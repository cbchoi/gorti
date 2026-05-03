package eventlog

import (
	"bytes"
	"context"
	"sync"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
)

// TestWriter_Append_ConcurrentSafe verifies that Writer.Append is safe to
// call from multiple goroutines on the same Writer instance.
//
// Background (M6 W1B / W2A finding): the production federation manager
// historically serialized Append on a single goroutine per federation,
// so the Writer's `nextSeq` increment and sink-Write pair raced under
// no mutex. The W2A perf harness tripped this by sending from many
// goroutines concurrently and worked around it by running with
// `EventLog: nil`. This test pins the new contract: Append is
// goroutine-safe, and the per-record seq numbers it assigns form the
// permutation 1..N*M with no gaps and no duplicates.
//
// The test uses *productionEvent (defined in proto_record_test.go) so
// the proto.Marshal branch and the unsafe-pointer-Seq write are both
// exercised. Run with `go test -race ./rti/internal/eventlog/...` to
// validate the fix.
func TestWriter_Append_ConcurrentSafe(t *testing.T) {
	const (
		goroutines = 8
		perG       = 256
	)

	var buf bytes.Buffer
	w := newWriterForTest(t, "concurrent", &buf)
	defer w.Close()

	var wg sync.WaitGroup
	seenMu := sync.Mutex{}
	seen := make([]uint64, 0, goroutines*perG)

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perG; i++ {
				evt := &productionEvent{Event: &rtiv1.Event{}}
				if err := w.Append(context.Background(), "concurrent", evt); err != nil {
					t.Errorf("Append: %v", err)
					return
				}
				seenMu.Lock()
				seen = append(seen, evt.Seq())
				seenMu.Unlock()
			}
		}()
	}
	wg.Wait()

	if got, want := len(seen), goroutines*perG; got != want {
		t.Fatalf("collected %d seqs, want %d", got, want)
	}

	// Each emitted seq MUST be in [1, N*M] and no value may repeat.
	occurrences := make(map[uint64]int, len(seen))
	for _, s := range seen {
		if s < 1 || s > uint64(goroutines*perG) {
			t.Errorf("seq %d out of range [1, %d]", s, goroutines*perG)
		}
		occurrences[s]++
	}
	for s, n := range occurrences {
		if n != 1 {
			t.Errorf("seq %d assigned %d times (want 1)", s, n)
		}
	}
	if len(occurrences) != goroutines*perG {
		t.Errorf("distinct seq count = %d, want %d", len(occurrences), goroutines*perG)
	}
}
