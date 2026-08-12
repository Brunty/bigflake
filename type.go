package bigflake

import "fmt"

// Type is a Generator bound to one pre-validated prefix. Because the prefix is
// checked when the Type is created, Type.New cannot fail.
//
// Create one with Generator.Type and keep it per entity kind, typically in a
// package-level variable. A Type draws on its Generator's shared sequence, so
// IDs minted through a Type and straight from the Generator never collide. A
// Type is safe for concurrent use by multiple goroutines.
type Type struct {
	g      *Generator
	prefix string
}

// Type returns a minter bound to prefix. The prefix is validated here, so the
// returned Type's New method never fails. An invalid prefix returns an error
// wrapping ErrInvalidPrefix.
func (g *Generator) Type(prefix string) (*Type, error) {
	if err := validatePrefix(prefix); err != nil {
		return nil, err
	}
	return &Type{g: g, prefix: prefix}, nil
}

// MustType is like Type but panics on an invalid prefix, which suits
// package-level setup with a constant prefix.
func (g *Generator) MustType(prefix string) *Type {
	t, err := g.Type(prefix)
	if err != nil {
		panic(err)
	}
	return t
}

// Prefix returns the prefix this Type is bound to.
func (t *Type) Prefix() string { return t.prefix }

// New mints an ID carrying the bound prefix.
func (t *Type) New() ID {
	hi, lo := t.g.next()
	return ID{prefix: t.prefix, hi: hi, lo: lo}
}

// FromBytes rebuilds an ID from 16 big-endian bytes, re-attaching the bound
// prefix: the usual way to turn a BINARY(16) column back into an ID.
func (t *Type) FromBytes(b []byte) (ID, error) {
	return FromBytes(t.prefix, b)
}

// FromUint128 rebuilds an ID from its two 64-bit words, re-attaching the bound
// prefix.
func (t *Type) FromUint128(hi, lo uint64) ID {
	return ID{prefix: t.prefix, hi: hi, lo: lo}
}

// Parse behaves like the package-level Parse but also requires the prefix to
// match this Type's, so a "product_..." string cannot be read as a user ID.
func (t *Type) Parse(s string) (ID, error) {
	id, err := Parse(s)
	if err != nil {
		return ID{}, err
	}
	if id.prefix != t.prefix {
		return ID{}, fmt.Errorf("%w: prefix %q, want %q", ErrInvalidID, id.prefix, t.prefix)
	}
	return id, nil
}
