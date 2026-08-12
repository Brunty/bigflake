package bigflake

import (
	"encoding/json"
	"errors"
	"math"
	"math/big"
	"strings"
	"testing"
	"time"
)

func TestIDRoundTripsThroughItsString(t *testing.T) {
	g, err := New(WithWorker(0xAABBCCDDEEFF))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	id, err := g.Generate("user")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	got, err := Parse(id.String())
	if err != nil {
		t.Fatalf("Parse(%q): %v", id.String(), err)
	}
	if got != id {
		t.Errorf("round trip changed the ID: got %v, want %v", got, id)
	}
}

func TestIDStringHasPrefixAndFixedWidthBody(t *testing.T) {
	id := MustParse("user_" + strings.Repeat("0", encodedLen))

	if !strings.HasPrefix(id.String(), "user_") {
		t.Errorf("String() = %q, want a \"user_\" prefix", id.String())
	}
	body := strings.TrimPrefix(id.String(), "user_")
	if len(body) != encodedLen {
		t.Errorf("body %q is %d characters, want %d", body, len(body), encodedLen)
	}
}

func TestMaxValueRoundTrips(t *testing.T) {
	// All ones is the widest 128-bit value and sits one below the overflow
	// boundary, so it exercises both the longest encode and the tightest decode.
	id, err := FromUint128("user", math.MaxUint64, math.MaxUint64)
	if err != nil {
		t.Fatalf("FromUint128: %v", err)
	}

	got, err := Parse(id.String())
	if err != nil {
		t.Fatalf("Parse(%q): %v", id.String(), err)
	}
	if got != id {
		t.Errorf("round trip changed the ID: got %v, want %v", got, id)
	}
}

func TestParseIsCaseSensitive(t *testing.T) {
	g, _ := New(WithWorker(1234))
	id := g.MustGenerate("user")
	canonical := id.String()

	got, err := Parse(canonical)
	if err != nil {
		t.Fatalf("Parse(%q): %v", canonical, err)
	}
	if got != id {
		t.Errorf("Parse(%q) = %v, want %v", canonical, got, id)
	}

	// base62 spends both cases as distinct digits, so re-casing the body names a
	// different number - it must not quietly resolve back to the same ID.
	for _, s := range []string{strings.ToLower(canonical), strings.ToUpper(canonical)} {
		if s == canonical {
			continue
		}
		switch other, err := Parse(s); {
		case err != nil:
			// Re-casing the prefix is rejected outright, which is also correct.
		case other == id:
			t.Errorf("Parse(%q) returned the same ID as %q, want case to matter", s, canonical)
		}
	}
}

func TestParseRejectsANonLowerCasePrefix(t *testing.T) {
	g, _ := New(WithWorker(1))
	body := strings.TrimPrefix(g.MustGenerate("user").String(), "user")

	if _, err := Parse("USER" + body); !errors.Is(err, ErrInvalidPrefix) {
		t.Errorf("error = %v, want ErrInvalidPrefix", err)
	}
}

func TestParseRejectsMalformedIDs(t *testing.T) {
	tests := map[string]struct {
		in   string
		want error
	}{
		"no separator":     {"user" + strings.Repeat("0", encodedLen), ErrInvalidID},
		"body too short":   {"user_0000", ErrInvalidID},
		"body too long":    {"user_" + strings.Repeat("0", encodedLen+1), ErrInvalidID},
		"invalid char":     {"user_" + strings.Repeat("0", encodedLen-1) + "-", ErrInvalidID},
		"overflows 128":    {"user_8" + strings.Repeat("0", encodedLen-1), ErrInvalidID},
		"empty prefix":     {"_" + strings.Repeat("0", encodedLen), ErrInvalidPrefix},
		"prefix with dash": {"my-type_" + strings.Repeat("0", encodedLen), ErrInvalidPrefix},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse(tc.in); !errors.Is(err, tc.want) {
				t.Errorf("Parse(%q) error = %v, want %v", tc.in, err, tc.want)
			}
		})
	}
}

func TestIDExposesItsParts(t *testing.T) {
	const (
		worker = uint64(0x0000AABBCCDD)
		seq    = uint64(42)
	)
	ts := time.Date(2026, 8, 12, 10, 30, 0, 0, time.UTC)

	id, err := FromUint128("user", uint64(ts.UnixMilli()), worker<<16|seq)
	if err != nil {
		t.Fatalf("FromUint128: %v", err)
	}

	if got := id.Prefix(); got != "user" {
		t.Errorf("Prefix() = %q, want %q", got, "user")
	}
	if got := id.Time(); !got.Equal(ts) {
		t.Errorf("Time() = %v, want %v", got, ts)
	}
	if got := id.Worker(); got != worker {
		t.Errorf("Worker() = %d, want %d", got, worker)
	}
	if got := id.Sequence(); got != seq {
		t.Errorf("Sequence() = %d, want %d", got, seq)
	}

	parts := id.Decompose()
	if !parts.Time.Equal(ts) || parts.Worker != worker || parts.Sequence != seq {
		t.Errorf("Decompose() = %+v, want time %v, worker %d, sequence %d", parts, ts, worker, seq)
	}
}

func TestTimeIsUTC(t *testing.T) {
	g, _ := New(WithWorker(1))
	if loc := g.MustGenerate("user").Time().Location(); loc != time.UTC {
		t.Errorf("Time() location = %v, want UTC", loc)
	}
}

func TestIDRoundTripsThroughBytes(t *testing.T) {
	g, _ := New(WithWorker(maxWorker))
	id := g.MustGenerate("order")

	b := id.Bytes()
	if len(b) != 16 {
		t.Fatalf("Bytes() is %d bytes, want 16", len(b))
	}

	got, err := FromBytes("order", b)
	if err != nil {
		t.Fatalf("FromBytes: %v", err)
	}
	if got != id {
		t.Errorf("FromBytes(Bytes()) = %v, want %v", got, id)
	}
}

func TestFromBytesRejectsWrongLength(t *testing.T) {
	if _, err := FromBytes("user", make([]byte, 15)); !errors.Is(err, ErrInvalidID) {
		t.Errorf("error = %v, want ErrInvalidID", err)
	}
}

func TestIDRoundTripsThroughUint128(t *testing.T) {
	g, _ := New(WithWorker(7))
	id := g.MustGenerate("user")

	hi, lo := id.Uint128()
	got, err := FromUint128("user", hi, lo)
	if err != nil {
		t.Fatalf("FromUint128: %v", err)
	}
	if got != id {
		t.Errorf("FromUint128(Uint128()) = %v, want %v", got, id)
	}
}

func TestIntMatchesTheTwoWords(t *testing.T) {
	id, err := FromUint128("user", 0x0123456789ABCDEF, 0xFEDCBA9876543210)
	if err != nil {
		t.Fatalf("FromUint128: %v", err)
	}

	want, ok := new(big.Int).SetString("0123456789ABCDEFFEDCBA9876543210", 16)
	if !ok {
		t.Fatal("could not build the expected big.Int")
	}
	if got := id.Int(); got.Cmp(want) != 0 {
		t.Errorf("Int() = %s, want %s", got, want)
	}
}

func TestStringsSortInTimeOrder(t *testing.T) {
	// The whole point of a k-sortable ID: sorting the strings sorts by age.
	g, _ := New(WithWorker(12345))
	prev := g.MustGenerate("user").String()
	for range 1000 {
		next := g.MustGenerate("user").String()
		if next <= prev {
			t.Fatalf("IDs are out of order: %q came after %q", next, prev)
		}
		prev = next
	}
}

func TestIDMarshalsAsJSONString(t *testing.T) {
	g, _ := New(WithWorker(99))
	id := g.MustGenerate("user")

	b, err := json.Marshal(id)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if want := `"` + id.String() + `"`; string(b) != want {
		t.Errorf("Marshal = %s, want %s", b, want)
	}

	var got ID
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got != id {
		t.Errorf("Unmarshal = %v, want %v", got, id)
	}
}

func TestMustParsePanicsOnBadInput(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("MustParse did not panic")
		}
	}()
	MustParse("nope")
}

func BenchmarkGenerate(b *testing.B) {
	g, _ := New(WithWorker(1))
	for b.Loop() {
		_, _ = g.Generate("user")
	}
}

// BenchmarkGenerateParallel measures the cost with every goroutine sharing one
// Generator, so the lock is contended - the shape most callers actually have.
// Compare it against BenchmarkGenerate to see what the contention costs.
func BenchmarkGenerateParallel(b *testing.B) {
	g, _ := New(WithWorker(1))
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = g.Generate("user")
		}
	})
}

func BenchmarkParse(b *testing.B) {
	g, _ := New(WithWorker(1))
	s := g.MustGenerate("user").String()
	b.ResetTimer()
	for b.Loop() {
		_, _ = Parse(s)
	}
}

func BenchmarkString(b *testing.B) {
	g, _ := New(WithWorker(1))
	id := g.MustGenerate("user")
	b.ResetTimer()
	for b.Loop() {
		_ = id.String()
	}
}
