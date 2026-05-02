package eventlog

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// newMultiplexForTest builds a MultiplexWriter whose factory routes each
// federation's bytes into a per-federation *bytes.Buffer kept on the test
// helper for inspection. Returns the multiplexer and a getter for the
// captured buffers (so tests can assert byte content directly).
func newMultiplexForTest(t *testing.T) (*MultiplexWriter, map[core.FederationName]*bytes.Buffer) {
	t.Helper()
	bufs := map[core.FederationName]*bytes.Buffer{}
	factory := func(opts WriterOptions) (*Writer, error) {
		buf := &bytes.Buffer{}
		bufs[opts.Federation] = buf
		opts.Sink = buf
		return NewWriter(opts)
	}
	mux, err := NewMultiplexWriter(MultiplexOptions{
		Clock:   core.NewFakeClock(time.Unix(0, 0)),
		Mode:    core.ModeVerbose,
		Seed:    42,
		Factory: factory,
	})
	if err != nil {
		t.Fatalf("NewMultiplexWriter: %v", err)
	}
	return mux, bufs
}

// TestMultiplexWriter_LazyOpenPerFederation: first Append for a federation
// triggers a writer creation; second Append on a different federation
// triggers a second writer.
func TestMultiplexWriter_LazyOpenPerFederation(t *testing.T) {
	mux, bufs := newMultiplexForTest(t)
	defer func() { _ = mux.Close() }()

	if err := mux.Append(context.Background(), "alpha", &writerEvent{}); err != nil {
		t.Fatalf("Append alpha: %v", err)
	}
	if _, ok := bufs["alpha"]; !ok {
		t.Errorf("after Append alpha: no buffer created for alpha")
	}
	if _, ok := bufs["beta"]; ok {
		t.Errorf("after Append alpha: unexpected buffer created for beta")
	}

	if err := mux.Append(context.Background(), "beta", &writerEvent{}); err != nil {
		t.Fatalf("Append beta: %v", err)
	}
	if _, ok := bufs["beta"]; !ok {
		t.Errorf("after Append beta: no buffer created for beta")
	}
}

// TestMultiplexWriter_PerFederationSeqIsIndependent: monotonic seq is
// per-federation, not shared across federations.
func TestMultiplexWriter_PerFederationSeqIsIndependent(t *testing.T) {
	mux, _ := newMultiplexForTest(t)
	defer func() { _ = mux.Close() }()

	a1 := &writerEvent{}
	if err := mux.Append(context.Background(), "alpha", a1); err != nil {
		t.Fatalf("Append alpha #1: %v", err)
	}
	a2 := &writerEvent{}
	if err := mux.Append(context.Background(), "alpha", a2); err != nil {
		t.Fatalf("Append alpha #2: %v", err)
	}
	b1 := &writerEvent{}
	if err := mux.Append(context.Background(), "beta", b1); err != nil {
		t.Fatalf("Append beta #1: %v", err)
	}

	if a1.Seq() != 1 || a2.Seq() != 2 {
		t.Errorf("alpha seqs = %d,%d; want 1,2", a1.Seq(), a2.Seq())
	}
	if b1.Seq() != 1 {
		t.Errorf("beta seq = %d; want 1 (independent counter)", b1.Seq())
	}
}

// TestMultiplexWriter_SyncByFederation: Sync flushes the named federation
// only; unknown federation returns ErrFederationNotFound.
func TestMultiplexWriter_SyncByFederation(t *testing.T) {
	mux, _ := newMultiplexForTest(t)
	defer func() { _ = mux.Close() }()

	if err := mux.Append(context.Background(), "alpha", &writerEvent{}); err != nil {
		t.Fatalf("Append alpha: %v", err)
	}
	if err := mux.Sync(context.Background(), "alpha"); err != nil {
		t.Errorf("Sync alpha: %v", err)
	}
	if err := mux.Sync(context.Background(), "ghost"); !errors.Is(err, core.ErrFederationNotFound) {
		t.Errorf("Sync ghost: err = %v, want ErrFederationNotFound", err)
	}
}

// TestMultiplexWriter_CloseFlushesAll: Close releases every per-federation
// writer; a subsequent Append returns the closed-error.
func TestMultiplexWriter_CloseFlushesAll(t *testing.T) {
	mux, _ := newMultiplexForTest(t)
	if err := mux.Append(context.Background(), "alpha", &writerEvent{}); err != nil {
		t.Fatalf("Append alpha: %v", err)
	}
	if err := mux.Append(context.Background(), "beta", &writerEvent{}); err != nil {
		t.Fatalf("Append beta: %v", err)
	}
	if err := mux.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := mux.Append(context.Background(), "alpha", &writerEvent{}); err == nil {
		t.Errorf("Append after Close: got nil error, want closed-error")
	}
}

// TestMultiplexWriter_RejectsMissingClock: NewMultiplexWriter requires Clock.
func TestMultiplexWriter_RejectsMissingClock(t *testing.T) {
	_, err := NewMultiplexWriter(MultiplexOptions{
		Mode:    core.ModeVerbose,
		Factory: func(WriterOptions) (*Writer, error) { return nil, nil },
	})
	if err == nil {
		t.Errorf("NewMultiplexWriter with nil Clock returned nil error")
	}
}

// TestMultiplexWriter_DefaultFactoryWritesToDir: with no Factory supplied
// MultiplexWriter creates files under Dir and writes the federation header.
// The default factory MUST refuse if Dir is empty.
func TestMultiplexWriter_DefaultFactory_RequiresDir(t *testing.T) {
	_, err := NewMultiplexWriter(MultiplexOptions{
		Clock: core.NewFakeClock(time.Unix(0, 0)),
		Mode:  core.ModeVerbose,
	})
	if err == nil {
		t.Errorf("NewMultiplexWriter with no Factory and no Dir returned nil error")
	}
}

// TestMultiplexWriter_DefaultFactoryFiles: when Dir is supplied and no
// Factory, the multiplexer writes one .log file per federation, each with
// the standard header.
func TestMultiplexWriter_DefaultFactoryFiles(t *testing.T) {
	dir := t.TempDir()
	mux, err := NewMultiplexWriter(MultiplexOptions{
		Clock: core.NewFakeClock(time.Unix(0, 0)),
		Mode:  core.ModeVerbose,
		Dir:   dir,
	})
	if err != nil {
		t.Fatalf("NewMultiplexWriter: %v", err)
	}
	defer func() { _ = mux.Close() }()

	if err := mux.Append(context.Background(), "alpha", &writerEvent{}); err != nil {
		t.Fatalf("Append alpha: %v", err)
	}
	if err := mux.Sync(context.Background(), "alpha"); err != nil {
		t.Fatalf("Sync alpha: %v", err)
	}

	// Open the on-disk file via the standard Reader to confirm the header
	// + one record are present.
	rdr, err := mux.OpenReader(context.Background(), "alpha")
	if err != nil {
		t.Fatalf("OpenReader alpha: %v", err)
	}
	defer func() { _ = rdr.Close() }()
	if rdr.Header().Federation != "alpha" {
		t.Errorf("Header().Federation = %q, want alpha", rdr.Header().Federation)
	}
	if _, err := rdr.Next(context.Background()); err != nil {
		t.Errorf("Next: %v", err)
	}
	if _, err := rdr.Next(context.Background()); err != io.EOF {
		t.Errorf("Next at end: err = %v, want io.EOF", err)
	}
}

// writerEvent is a minimal record satisfying core.EventRecord with a
// settable seq via reflection (matches the writer's reflection contract).
type writerEvent struct {
	seq uint64 //nolint:unused // assigned via reflection in writer.assignSeq
}

func (e *writerEvent) Seq() uint64 { return e.seq }
