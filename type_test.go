package bigflake

import (
	"errors"
	"testing"
)

func TestTypeMintsWithItsPrefix(t *testing.T) {
	g, _ := New(WithWorker(5))

	users, err := g.Type("user")
	if err != nil {
		t.Fatalf("Type: %v", err)
	}
	if users.Prefix() != "user" {
		t.Errorf("Prefix() = %q, want %q", users.Prefix(), "user")
	}

	id := users.New()
	if id.Prefix() != "user" {
		t.Errorf("New().Prefix() = %q, want %q", id.Prefix(), "user")
	}
	if id.Worker() != 5 {
		t.Errorf("New().Worker() = %d, want 5", id.Worker())
	}
}

func TestTypeRejectsAnInvalidPrefixUpFront(t *testing.T) {
	g, _ := New(WithWorker(1))
	if _, err := g.Type("User ID"); !errors.Is(err, ErrInvalidPrefix) {
		t.Errorf("error = %v, want ErrInvalidPrefix", err)
	}
}

func TestTypeAndGeneratorShareOneSequence(t *testing.T) {
	// Mixing the two ways of minting must not produce the same ID twice.
	g, _ := New(WithWorker(1))
	users := g.MustType("user")

	seen := make(map[ID]struct{}, 2000)
	for range 1000 {
		for _, id := range []ID{users.New(), g.MustGenerate("user")} {
			if _, dup := seen[id]; dup {
				t.Fatalf("duplicate ID %v", id)
			}
			seen[id] = struct{}{}
		}
	}
}

func TestTypeParseRejectsAnotherPrefix(t *testing.T) {
	g, _ := New(WithWorker(1))
	users := g.MustType("user")
	product := g.MustType("product").New()

	if _, err := users.Parse(product.String()); !errors.Is(err, ErrInvalidID) {
		t.Errorf("error = %v, want ErrInvalidID", err)
	}

	id := users.New()
	got, err := users.Parse(id.String())
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got != id {
		t.Errorf("Parse = %v, want %v", got, id)
	}
}

func TestTypeReattachesItsPrefixToStoredValues(t *testing.T) {
	g, _ := New(WithWorker(1))
	users := g.MustType("user")
	id := users.New()

	fromBytes, err := users.FromBytes(id.Bytes())
	if err != nil {
		t.Fatalf("FromBytes: %v", err)
	}
	if fromBytes != id {
		t.Errorf("FromBytes = %v, want %v", fromBytes, id)
	}

	hi, lo := id.Uint128()
	if got := users.FromUint128(hi, lo); got != id {
		t.Errorf("FromUint128 = %v, want %v", got, id)
	}
}

func TestMustTypePanicsOnBadPrefix(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("MustType did not panic")
		}
	}()
	g, _ := New(WithWorker(1))
	g.MustType("")
}
