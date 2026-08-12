package bigflake

import (
	"errors"
	"fmt"
	"math/big"
	"math/bits"
	"strings"
	"time"
)

const (
	// maxWorker is 2^48-1, the largest worker ID the 48-bit field holds.
	maxWorker = uint64(1)<<48 - 1
	// maxSequence is 2^16-1, the largest per-millisecond sequence.
	maxSequence = uint64(1)<<16 - 1

	// encodedLen is the width of the encoded body. 62^22 > 2^128 > 62^21, so 22
	// characters are the fewest that always fit. Bodies are padded to that width
	// rather than trimmed, which is what keeps IDs sortable as strings.
	encodedLen = 22

	// base is the encoding radix.
	base = 62

	// maxPrefixLen keeps prefixes short enough to stay readable in logs and URLs.
	maxPrefixLen = 32

	// sep divides the prefix from the encoded body.
	sep = '_'
)

// base62, ordered digits then upper case then lower case. That order is the
// point: it matches ASCII, so comparing two fixed-width encoded bodies as
// strings gives the same answer as comparing the numbers they represent.
const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// decodeMap maps a byte to its base62 value, or -1 if it is not a valid
// character. Case is significant: "a" and "A" are different digits.
var decodeMap = func() [256]int8 {
	var m [256]int8
	for i := range m {
		m[i] = -1
	}
	for v, c := range []byte(alphabet) {
		m[c] = int8(v)
	}
	return m
}()

var (
	// ErrInvalidPrefix reports a prefix that is empty, too long, or contains
	// something other than lower-case letters and digits.
	ErrInvalidPrefix = errors.New("bigflake: invalid prefix")
	// ErrInvalidID reports a string or byte slice that is not a valid ID.
	ErrInvalidID = errors.New("bigflake: invalid id")
)

// ID is a 128-bit flake with a type prefix. The zero ID is not valid; obtain one
// from a Generator, a Type, or by parsing. IDs are comparable and safe to copy.
type ID struct {
	prefix string
	// hi holds the timestamp; lo holds worker<<16 | sequence. Keeping the two
	// halves as words rather than a formatted string means Generate does no
	// allocation - encoding is deferred to String.
	hi, lo uint64
}

// Parts is the decomposed content of an ID, as returned by ID.Decompose.
type Parts struct {
	Time     time.Time
	Worker   uint64
	Sequence uint64
}

// Prefix returns the type prefix, for example "user".
func (id ID) Prefix() string { return id.prefix }

// Timestamp returns the raw millisecond timestamp.
func (id ID) Timestamp() uint64 { return id.hi }

// Time returns when the ID was minted, in UTC.
func (id ID) Time() time.Time {
	return time.UnixMilli(int64(id.hi)).UTC()
}

// Worker returns the 48-bit worker ID that minted the ID.
func (id ID) Worker() uint64 { return (id.lo >> 16) & maxWorker }

// Sequence returns the per-millisecond sequence number.
func (id ID) Sequence() uint64 { return id.lo & maxSequence }

// Decompose returns the timestamp, worker and sequence together, for when you
// want all three without three calls.
func (id ID) Decompose() Parts {
	return Parts{Time: id.Time(), Worker: id.Worker(), Sequence: id.Sequence()}
}

// Uint128 returns the ID's numeric value as its high and low 64-bit words. This
// is the cheapest numeric form: no allocation, and the pair round-trips through
// FromUint128 or two BIGINT columns.
func (id ID) Uint128() (hi, lo uint64) { return id.hi, id.lo }

// Int returns the ID's numeric value as a big.Int. A 128-bit value does not fit
// any built-in integer type, so use Uint128 unless you need arithmetic or a
// decimal rendering.
func (id ID) Int() *big.Int {
	n := new(big.Int).SetUint64(id.hi)
	n.Lsh(n, 64)
	return n.Or(n, new(big.Int).SetUint64(id.lo))
}

// Bytes returns the 128-bit value as 16 big-endian bytes, ready for a BINARY(16)
// or bytea column. The prefix is not included: store that in the schema, not the
// row.
func (id ID) Bytes() []byte {
	b := make([]byte, 16)
	for i := range 8 {
		b[i] = byte(id.hi >> (56 - 8*i))
		b[8+i] = byte(id.lo >> (56 - 8*i))
	}
	return b
}

// String returns the full ID: the prefix, an underscore, and 22 characters of
// base62.
func (id ID) String() string {
	buf := make([]byte, 0, len(id.prefix)+1+encodedLen)
	buf = append(buf, id.prefix...)
	buf = append(buf, sep)
	return string(id.encode(buf))
}

// encode appends the base62 body. Digits fall out of the division least
// significant first, so they are written into a fixed 22-byte window back to
// front; running the loop the full 22 times regardless of how few digits the
// value needs is what left-pads small values with "0".
func (id ID) encode(buf []byte) []byte {
	var body [encodedLen]byte
	hi, lo := id.hi, id.lo
	for i := encodedLen - 1; i >= 0; i-- {
		var rem uint64
		hi, lo, rem = divmod(hi, lo, base)
		body[i] = alphabet[rem]
	}
	return append(buf, body[:]...)
}

// divmod divides the 128-bit value in hi:lo by d, returning the quotient in two
// words and the remainder. Dividing the top word first leaves a remainder below
// d, which is the precondition bits.Div64 needs to avoid overflowing (and
// panicking) on the bottom half.
func divmod(hi, lo, d uint64) (qhi, qlo, rem uint64) {
	qhi, rem = hi/d, hi%d
	qlo, rem = bits.Div64(rem, lo, d)
	return qhi, qlo, rem
}

// MarshalText implements encoding.TextMarshaler, so IDs encode as strings in
// JSON and anything else built on text marshalling.
func (id ID) MarshalText() ([]byte, error) {
	return []byte(id.String()), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
func (id *ID) UnmarshalText(text []byte) error {
	parsed, err := Parse(string(text))
	if err != nil {
		return err
	}
	*id = parsed
	return nil
}

// Parse reads an ID of the form "prefix_body". IDs are case-sensitive: base62
// uses both cases as distinct digits, so "user_aB..." and "user_Ab..." are
// different IDs, and a prefix that is not lower case is rejected outright.
func Parse(s string) (ID, error) {
	i := strings.LastIndexByte(s, sep)
	if i < 0 {
		return ID{}, fmt.Errorf("%w: %q has no %q separator", ErrInvalidID, s, string(sep))
	}

	prefix := s[:i]
	if err := validatePrefix(prefix); err != nil {
		return ID{}, err
	}

	body := s[i+1:]
	if len(body) != encodedLen {
		return ID{}, fmt.Errorf("%w: body is %d characters, want %d", ErrInvalidID, len(body), encodedLen)
	}

	var hi, lo uint64
	for i := 0; i < len(body); i++ {
		v := decodeMap[body[i]]
		if v < 0 {
			return ID{}, fmt.Errorf("%w: %q is not a base62 character", ErrInvalidID, string(body[i]))
		}
		var ok bool
		if hi, lo, ok = mulAdd(hi, lo, base, uint64(v)); !ok {
			return ID{}, fmt.Errorf("%w: %q overflows 128 bits", ErrInvalidID, body)
		}
	}
	return ID{prefix: prefix, hi: hi, lo: lo}, nil
}

// mulAdd returns hi:lo*m + add as a 128-bit value, reporting whether it fitted.
// 22 base62 digits can address more than 2^128, so the overflow check is what
// stops an out-of-range string decoding to a wrapped-around ID.
func mulAdd(hi, lo, m, add uint64) (rhi, rlo uint64, ok bool) {
	// Anything the top word carries above 64 bits is already out of range.
	over, rhi := bits.Mul64(hi, m)
	if over != 0 {
		return 0, 0, false
	}
	carry, rlo := bits.Mul64(lo, m)
	rhi, c := bits.Add64(rhi, carry, 0)
	if c != 0 {
		return 0, 0, false
	}
	rlo, c = bits.Add64(rlo, add, 0)
	rhi, c = bits.Add64(rhi, c, 0)
	if c != 0 {
		return 0, 0, false
	}
	return rhi, rlo, true
}

// MustParse is like Parse but panics on an invalid ID. Use it for constants in
// tests and package-level variables, not for untrusted input.
func MustParse(s string) ID {
	id, err := Parse(s)
	if err != nil {
		panic(err)
	}
	return id
}

// FromBytes rebuilds an ID from a prefix and 16 big-endian bytes, the inverse of
// ID.Bytes.
func FromBytes(prefix string, b []byte) (ID, error) {
	if err := validatePrefix(prefix); err != nil {
		return ID{}, err
	}
	if len(b) != 16 {
		return ID{}, fmt.Errorf("%w: %d bytes, want 16", ErrInvalidID, len(b))
	}
	var hi, lo uint64
	for i := range 8 {
		hi = hi<<8 | uint64(b[i])
		lo = lo<<8 | uint64(b[8+i])
	}
	return ID{prefix: prefix, hi: hi, lo: lo}, nil
}

// FromUint128 rebuilds an ID from a prefix and the two words returned by
// ID.Uint128.
func FromUint128(prefix string, hi, lo uint64) (ID, error) {
	if err := validatePrefix(prefix); err != nil {
		return ID{}, err
	}
	return ID{prefix: prefix, hi: hi, lo: lo}, nil
}

// validatePrefix accepts 1 to maxPrefixLen lower-case letters and digits.
// Underscores are excluded because the separator has to stay unambiguous.
func validatePrefix(prefix string) error {
	if prefix == "" {
		return fmt.Errorf("%w: prefix is empty", ErrInvalidPrefix)
	}
	if len(prefix) > maxPrefixLen {
		return fmt.Errorf("%w: %q is %d characters, limit is %d", ErrInvalidPrefix, prefix, len(prefix), maxPrefixLen)
	}
	for i := 0; i < len(prefix); i++ {
		c := prefix[i]
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') {
			return fmt.Errorf("%w: %q contains %q, want lower-case letters and digits", ErrInvalidPrefix, prefix, string(c))
		}
	}
	return nil
}
