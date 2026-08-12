// Package bigflake generates 128-bit, k-sortable, prefixed IDs.
//
// The layout follows the BigFlake variant of the Snowflake scheme:
//
//	64 bits  timestamp  milliseconds since the Unix epoch
//	48 bits  worker     a MAC address by default
//	16 bits  sequence   0 - 65535 IDs per worker per millisecond
//
// A 64-bit millisecond timestamp removes the wraparound the original 64-bit
// design hits in 2089, and a 48-bit worker holds a MAC address, so separate
// machines need no central node-number assignment to keep from colliding.
//
// IDs render as a prefix, an underscore, and 22 characters of base62. The
// encoding is fixed width and ordered, so sorting the strings sorts by age. It
// spends both letter cases as distinct digits to reach 62 symbols, which makes
// IDs case-sensitive: store them somewhere that compares bytes, not somewhere
// that folds case.
//
//	g, err := bigflake.New()
//	id, err := g.Generate("user")
//	id.String() // "user_0000B9JIhkWR2rgYhGjXBQ"
package bigflake
