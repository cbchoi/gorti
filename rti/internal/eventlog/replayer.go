package eventlog

import (
	"context"
	"errors"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// ErrReplayDivergence indicates the replayed log produced a different
// byte sequence than the source log. This is a fatal determinism failure;
// the replayer surfaces it so callers can capture and bisect.
var ErrReplayDivergence = errors.New("eventlog: replay diverged from source")

// Replayer drives a fresh RTI through events from a source log and
// produces a new log; the new log MUST be byte-identical to the source.
//
// Replayer is a verification harness, not a runtime component. It exists
// so the determinism contract (NFR-DET-2) can be tested mechanically.
type Replayer struct {
	source   core.EventLogReader
	federation core.FederationStore
	objects  core.ObjectRegistry
	// internal state declared by Agent A in implementation
}

// ReplayerOptions bundles Replayer dependencies. All MUST be non-nil.
type ReplayerOptions struct {
	// Source is the event log to replay.
	Source core.EventLogReader

	// Federation is the lifecycle store events are routed to.
	Federation core.FederationStore

	// Objects is the object registry to drive Update/Send through.
	Objects core.ObjectRegistry

	// CapturingSink receives the new log produced during replay; the
	// replayer compares it byte-by-byte with the source after Close.
	// Pass a bytes.Buffer in tests.
	CapturingSink *Writer
}

// NewReplayer constructs a Replayer. Validates options.
func NewReplayer(opts ReplayerOptions) (*Replayer, error) {
	return &Replayer{
		source:     opts.Source,
		federation: opts.Federation,
		objects:    opts.Objects,
	}, ErrNotImplemented
}

// Replay consumes every event from the source, dispatches it through the
// live RTI components (Federation + Objects), and verifies the captured
// output matches the source byte-for-byte.
//
// Returns ErrReplayDivergence if the captured bytes differ. Returns the
// underlying error for I/O failures.
func (r *Replayer) Replay(ctx context.Context) error {
	_ = ctx
	return ErrNotImplemented
}
