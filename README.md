# bigflake

128-bit, time-sortable, prefixed IDs for Go.

```go
g, err := bigflake.New()
id, err := g.Generate("user")

id.String() // "user_0000B9JIhkWR2rgYhGjXBQ"
```

The prefix tells you what an ID refers to the moment you see it in a log, a URL,
or a support ticket. The rest is a [Snowflake](https://en.wikipedia.org/wiki/Snowflake_ID)-style
flake, widened to 128 bits in the style of
[bigflake](https://github.com/stevedomin/bigflake).

## Install

```sh
go get github.com/brunty/bigflake
```

```go
import "github.com/brunty/bigflake"
```

## Layout

128 bits, most significant first, so ordering the numbers orders them by age:

| Bits | Field     | Range                                        |
| ---- | --------- | -------------------------------------------- |
| 64   | timestamp | milliseconds since the Unix epoch            |
| 48   | worker    | 0 – (2^48-1), a MAC address by default       |
| 16   | sequence  | 0 – 65535 IDs per worker per millisecond     |

A 64-bit millisecond timestamp reaches far past any horizon worth planning for,
so there is no epoch to configure and no 2089 wraparound like the original
64-bit design. A 48-bit worker is exactly the width of a MAC address, so two
machines get distinct IDs without anyone handing out node numbers.

That is 65,536 IDs per millisecond per worker — 65 million a second. When a
millisecond's sequence is exhausted the generator waits for the clock rather
than reusing a number.

## Encoding

IDs render as `prefix` + `_` + 22 characters of base62, over the alphabet
`0-9A-Za-z`:

- **Fixed width and lexicographically sortable.** The alphabet is in ASCII
  order and bodies are left-padded to 22 characters, so comparing two IDs as
  strings gives the same answer as comparing the numbers. Sorting them sorts by
  time, and a text index behaves like a timestamp index.
- **Compact.** 22 characters is the fewest that can carry 128 bits at this
  radix, against the 32 a hex rendering needs or the 36 of a UUID.
- **Case-sensitive.** Reaching 62 symbols costs both letter cases, so `aB` and
  `Ab` are different digits and `user_aB…` and `user_Ab…` are different IDs.

That last point has consequences worth being blunt about: these IDs cannot
survive anything that folds case. Do not put them in a case-insensitive database
column (see below), and do not expect a mistyped `l` for `I` to be forgiven.
`Parse` is strict in both halves — the prefix must be lower case, and the body
must match exactly.

Small values are left-padded with `0` rather than printed short, so every body
is exactly 22 characters. That uniformity is what makes the string comparison
above valid.

## Types

When every ID of a kind shares a prefix, bind it once. The prefix is validated
up front, so minting cannot fail:

```go
users, err := g.Type("user") // validates "user" once
if err != nil {
    log.Fatal(err)
}

id := users.New() // always "user_…", no error to handle
```

A `Type` also rebuilds IDs from stored values and parses with a prefix check:

```go
id, err := users.FromBytes(b)                             // re-attach "user" to a 16-byte column
id, err := users.Parse("user_0000B9JIhkWR2rgYhGjXBQ") // rejects other prefixes
```

`MustType` panics instead of returning an error, which suits package-level
variables:

```go
var UserID = bigflake.MustNew().MustType("user")
```

## Reading an ID

```go
id, err := bigflake.Parse("user_0000B9JIhkWR2rgYhGjXBQ")

id.Prefix()   // "user" — the type
id.Time()     // time.Time — when the ID was created (UTC)
id.Worker()   // the worker that minted it
id.Sequence() // the per-millisecond sequence

parts := id.Decompose() // Parts{Time, Worker, Sequence}
```

## Numbers and bytes

A 128-bit value does not fit any built-in Go integer, so there are two numeric
forms:

```go
hi, lo := id.Uint128() // two uint64 words — no allocation
n := id.Int()          // *big.Int, for arithmetic or a decimal rendering

b := id.Bytes()        // 16 big-endian bytes
```

Each round-trips back, with the prefix supplied separately:

```go
id, err := bigflake.FromUint128("user", hi, lo)
id, err := bigflake.FromBytes("user", b)
```

## Storing IDs in a database

128 bits no longer fits a `BIGINT`. In rough order of preference:

| Storage                        | Write               | Read                       |
| ------------------------------ | ------------------- | -------------------------- |
| `BINARY(16)` / `bytea` / `uuid`| `id.Bytes()`        | `users.FromBytes(b)`       |
| Two `BIGINT` columns           | `id.Uint128()`      | `users.FromUint128(hi, lo)`|
| `VARCHAR(55)`                  | `id.String()`       | `bigflake.Parse(s)`         |

The prefix belongs to the table, not the row — store the 16 bytes and let the
`Type` re-attach the prefix on read. Storing the string is the most readable
option and sorts correctly, at the cost of roughly double the space.

Size the column for the longest ID you will store: the body is always 22
characters, plus the underscore, plus the prefix, so `user_…` is 27 and the
maximum a 32-character prefix allows is 55.

> [!CAUTION]
> **If you store the string, the collation must be case-sensitive.** base62
> uses `a` and `A` as different digits, so under a case-insensitive collation —
> including `utf8mb4_0900_ai_ci`, the MySQL 8.0 default — two distinct IDs can
> compare as equal. That means spurious duplicate-key errors on a unique index,
> and lookups that match the wrong row. Use a binary or `_bin` collation
> (`ascii_bin` is the right fit: the content is ASCII, and the comparison is a
> byte compare). Postgres `text` is case-sensitive by default and needs nothing
> special.

`ID` implements `encoding.TextMarshaler` and `TextUnmarshaler`, so it encodes as
a plain string in JSON.

## Choosing a key type

### Relational: prefer the bytes, especially in MySQL

In InnoDB a secondary index does not store a row pointer — it stores the whole
primary key. A 28-byte string key instead of a 16-byte binary one therefore
costs the extra bytes again in *every* secondary index on the table, so four
secondary indexes pay the difference five times. Wider keys also fit fewer per
16KB page, which deepens the tree and, more importantly, means less of the index
fits in the buffer pool.

Two more MySQL traps if you do store the string:

- Declare the column `ascii`, not `utf8mb4`. The content is ASCII either way,
  but `utf8mb4` makes MySQL budget 4 bytes per character wherever it needs a
  fixed upper bound — sort buffers, temp tables, index key limits.
- Use a case-sensitive collation. This is a correctness requirement, not a
  performance preference — see the warning above.

Postgres is less sensitive: its indexes reference heap tuples rather than
repeating the primary key, so a text key costs less there than in MySQL. `uuid`
is a native 16-byte type and takes `Bytes()` directly.

What you keep either way is the ordering. Because IDs rise over time, inserts
land at the right-hand edge of the index and append, instead of splitting pages
across the whole tree the way random UUIDs do. That is the main reason to use
these over UUIDv4, and it survives whichever column type you pick.

### NoSQL: the string is fine, the partitioning is the question

Most document, key-value and column stores have no clustered-index penalty, so
the string form costs little: keys in a key-value store are byte strings
already, and DynamoDB counts binary attributes base64-encoded, which makes 16
bytes 24 characters against this encoding's 27 — a saving of three characters.
Rarely worth the lost readability.

The decision that does matter is how the store spreads keys:

- **Hashed keys** (a DynamoDB partition key, a Cassandra partition key) destroy
  the ordering, since the hash discards it. Use the ID as a sort/clustering key
  if you want time-ordered scans.
- **Ordered keys** (HBase, Bigtable, any ordered range scan) have the opposite
  problem: IDs that climb over time send every new write to the same region or
  tablet. Salting or hash-prefixing the key spreads the load back out, and
  gives up the ordering to do it.

## Workers

By default the worker ID is the first non-loopback MAC address on the host, and
48 random bits if there is none. In containers, MACs are often synthetic,
duplicated or reassigned, so set the worker explicitly where that matters:

```go
g, err := bigflake.New(bigflake.WithWorker(7))
```

Any two generators that could run at the same instant need different worker IDs;
that is the only rule uniqueness depends on.

## Clocks

The generator never emits an ID with a timestamp below the last one it issued,
so an NTP correction that steps the clock backwards costs some resolution but
never repeats an ID.

## Performance

On an M2 Pro (10 cores), `go test -bench . -benchmem -count=5`:

```
BenchmarkGenerate-10            27134637     44.2 ns/op     0 B/op    0 allocs/op
BenchmarkGenerateParallel-10     8077417    150.4 ns/op     0 B/op    0 allocs/op
BenchmarkParse-10               26386722     46.4 ns/op     0 B/op    0 allocs/op
BenchmarkString-10              12022737    100.4 ns/op    32 B/op    1 allocs/op
```

Generation allocates nothing: encoding happens only when you ask for the string,
and `Parse` allocates nothing at all.

`String` costs more than `Parse` because 62 is not a power of two. Encoding is
22 rounds of 128-bit division, while decoding is 22 multiply-and-adds, and
division is the more expensive instruction. A power-of-two radix could encode by
shifting rather than dividing, which measures about 2.5x faster here, but needs
more characters to carry the same 128 bits. Density won that trade.

`Generate` is one goroutine, so its lock is uncontended: ~22M IDs/sec.
`GenerateParallel` shares one generator across all 10 cores, and the lock then
dominates — about 6.6M IDs/sec in total, *below* the single-goroutine figure.
Minting IDs is short enough that the goroutines spend their time queueing for
the lock rather than working.

So one shared generator is fine into the millions per second, but it will not
scale up with cores. If a profile shows contention, giveb each worker its own
`Generator` with its own `WithWorker` ID: they then run without touching each
other, and distinct worker IDs are what keeps their output unique.

Neither benchmark reaches the 65,536-per-millisecond ceiling, so neither
measures the cost of waiting for the next millisecond.

## Errors

Errors wrap `bigflake.ErrInvalidPrefix` or `bigflake.ErrInvalidID`, so test them
with `errors.Is`. A prefix is 1–32 lower-case letters and digits.

## Licence

[MIT](LICENSE) © Matt Brunt
