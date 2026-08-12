package bigflake

import (
	"errors"
	"sync"
	"testing"
)

func TestNewDerivesAWorkerFromTheHost(t *testing.T) {
	g, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if g.Worker() > maxWorker {
		t.Errorf("Worker() = %d, which does not fit 48 bits", g.Worker())
	}
	if g.Worker() == 0 {
		t.Error("Worker() = 0, want a MAC address or random bits")
	}
}

func TestWithWorkerIsUsedVerbatim(t *testing.T) {
	const worker = uint64(0x0123456789AB)

	g, err := New(WithWorker(worker))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if g.Worker() != worker {
		t.Errorf("Worker() = %d, want %d", g.Worker(), worker)
	}
	if got := g.MustGenerate("user").Worker(); got != worker {
		t.Errorf("id.Worker() = %d, want %d", got, worker)
	}
}

func TestWithWorkerRejectsValuesOver48Bits(t *testing.T) {
	if _, err := New(WithWorker(maxWorker + 1)); err == nil {
		t.Error("New accepted a worker that does not fit 48 bits")
	}
}

func TestGenerateRejectsInvalidPrefixes(t *testing.T) {
	g, _ := New(WithWorker(1))

	for _, prefix := range []string{"", "User", "user_id", "user-id", "user id", "üser", string(make([]byte, 33))} {
		if _, err := g.Generate(prefix); !errors.Is(err, ErrInvalidPrefix) {
			t.Errorf("Generate(%q) error = %v, want ErrInvalidPrefix", prefix, err)
		}
	}
}

func TestGenerateAcceptsLettersAndDigits(t *testing.T) {
	g, _ := New(WithWorker(1))

	for _, prefix := range []string{"user", "u", "team2", "0", "abcdefghijklmnopqrstuvwxyz012345"} {
		if _, err := g.Generate(prefix); err != nil {
			t.Errorf("Generate(%q): %v", prefix, err)
		}
	}
}

func TestIDsAreUniqueUnderConcurrency(t *testing.T) {
	const (
		goroutines = 16
		per        = 2000
	)

	g, _ := New(WithWorker(1))
	ids := make([]ID, 0, goroutines*per)

	var mu sync.Mutex
	var wg sync.WaitGroup
	for range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			local := make([]ID, per)
			for i := range local {
				local[i] = g.MustGenerate("user")
			}
			mu.Lock()
			ids = append(ids, local...)
			mu.Unlock()
		}()
	}
	wg.Wait()

	seen := make(map[ID]struct{}, len(ids))
	for _, id := range ids {
		if _, dup := seen[id]; dup {
			t.Fatalf("duplicate ID %v", id)
		}
		seen[id] = struct{}{}
	}
}

func TestSequenceExhaustionWaitsForTheNextMillisecond(t *testing.T) {
	g, _ := New(WithWorker(1))

	// One more than a millisecond holds, so the generator has to roll over.
	first := g.MustGenerate("user")
	var last ID
	for range maxSequence + 2 {
		last = g.MustGenerate("user")
	}

	if last.Timestamp() <= first.Timestamp() {
		t.Errorf("timestamp did not advance past the full millisecond: %d then %d", first.Timestamp(), last.Timestamp())
	}
}

func TestClockGoingBackwardsStillYieldsRisingIDs(t *testing.T) {
	g, _ := New(WithWorker(1))

	first := g.MustGenerate("user")
	// Simulate an NTP correction dragging the clock into the future's past.
	g.mu.Lock()
	g.lastMs += 10_000
	g.mu.Unlock()

	second := g.MustGenerate("user")
	third := g.MustGenerate("user")

	if !(first.String() < second.String() && second.String() < third.String()) {
		t.Errorf("IDs stopped rising across a clock step: %v, %v, %v", first, second, third)
	}
}

func TestMustGeneratePanicsOnBadPrefix(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("MustGenerate did not panic")
		}
	}()
	g, _ := New(WithWorker(1))
	g.MustGenerate("NOPE")
}

func TestMustNewReturnsAUsableGenerator(t *testing.T) {
	if id := MustNew(WithWorker(3)).MustGenerate("user"); id.Worker() != 3 {
		t.Errorf("Worker() = %d, want 3", id.Worker())
	}
}

func TestMustNewPanicsOnABadOption(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("MustNew did not panic")
		}
	}()
	MustNew(WithWorker(maxWorker + 1))
}
