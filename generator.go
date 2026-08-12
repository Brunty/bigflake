package bigflake

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"net"
	"sync"
	"time"
)

// Generator mints IDs. Create one with New and share it: all of its methods are
// safe for concurrent use by multiple goroutines.
type Generator struct {
	worker uint64

	mu       sync.Mutex
	lastMs   uint64
	sequence uint64
}

// config collects options while New builds a Generator.
type config struct {
	worker    uint64
	workerSet bool
}

// Option configures a Generator. See WithWorker.
type Option func(*config) error

// WithWorker sets an explicit 48-bit worker ID instead of deriving one from a
// MAC address. Use it where MACs are missing, unstable or duplicated, as they
// often are in containers, and give each concurrent generator its own value so
// their IDs cannot collide.
func WithWorker(worker uint64) Option {
	return func(c *config) error {
		if worker > maxWorker {
			return fmt.Errorf("bigflake: worker %d out of range [0, %d]", worker, maxWorker)
		}
		c.worker = worker
		c.workerSet = true
		return nil
	}
}

// New returns a Generator. With no options the worker ID comes from the host's
// MAC address, which is globally unique and so needs no coordination between
// machines; if no MAC is available it falls back to 48 random bits.
func New(opts ...Option) (*Generator, error) {
	var cfg config
	for _, opt := range opts {
		if err := opt(&cfg); err != nil {
			return nil, err
		}
	}

	worker := cfg.worker
	if !cfg.workerSet {
		w, err := deriveWorker()
		if err != nil {
			return nil, err
		}
		worker = w
	}
	return &Generator{worker: worker}, nil
}

// MustNew is like New but panics if the Generator cannot be built, which suits
// package-level initialisation where there is nothing useful to do with the
// error anyway.
func MustNew(opts ...Option) *Generator {
	g, err := New(opts...)
	if err != nil {
		panic(err)
	}
	return g
}

// Worker returns the 48-bit worker ID this Generator stamps into every ID.
func (g *Generator) Worker() uint64 { return g.worker }

// Generate mints an ID carrying prefix, which must be 1 to 32 lower-case letters
// and digits. An invalid prefix returns an error wrapping ErrInvalidPrefix; if
// the prefix is fixed, bind it once with Type and avoid the check entirely.
func (g *Generator) Generate(prefix string) (ID, error) {
	if err := validatePrefix(prefix); err != nil {
		return ID{}, err
	}
	hi, lo := g.next()
	return ID{prefix: prefix, hi: hi, lo: lo}, nil
}

// MustGenerate is like Generate but panics on an invalid prefix.
func (g *Generator) MustGenerate(prefix string) ID {
	id, err := g.Generate(prefix)
	if err != nil {
		panic(err)
	}
	return id
}

// next returns the two words of the next ID. When a millisecond's 65536
// sequence numbers run out it spins until the clock moves on, so IDs stay
// unique rather than repeating.
func (g *Generator) next() (hi, lo uint64) {
	g.mu.Lock()
	defer g.mu.Unlock()

	now := uint64(time.Now().UnixMilli())
	switch {
	case now > g.lastMs:
		g.lastMs = now
		g.sequence = 0
	default:
		// Covers now == lastMs and a clock that has stepped backwards: in both
		// cases keep counting against lastMs, which never goes down, so IDs stay
		// monotonic across an NTP correction.
		g.sequence++
		if g.sequence > maxSequence {
			g.lastMs = waitUntilAfter(g.lastMs)
			g.sequence = 0
		}
	}
	return g.lastMs, g.worker<<16 | g.sequence
}

// waitUntilAfter blocks until the wall clock passes ms and returns the new time.
func waitUntilAfter(ms uint64) uint64 {
	for {
		now := uint64(time.Now().UnixMilli())
		if now > ms {
			return now
		}
		time.Sleep(time.Millisecond)
	}
}

// deriveWorker returns the first usable MAC address as a 48-bit number, falling
// back to random bits when the host has no physical interface (as in some
// containers and sandboxes).
func deriveWorker() (uint64, error) {
	ifaces, err := net.Interfaces()
	if err == nil {
		for _, iface := range ifaces {
			if iface.Flags&net.FlagLoopback != 0 || len(iface.HardwareAddr) != 6 {
				continue
			}
			var w uint64
			for _, b := range iface.HardwareAddr {
				w = w<<8 | uint64(b)
			}
			if w != 0 {
				return w, nil
			}
		}
	}

	var b [8]byte
	if _, err := rand.Read(b[2:]); err != nil {
		return 0, fmt.Errorf("bigflake: no MAC address and no randomness available: %w", err)
	}
	return binary.BigEndian.Uint64(b[:]) & maxWorker, nil
}
