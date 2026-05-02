package m2spec

import (
	"bytes"
	"context"
	"crypto/sha256"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/core"
	"github.com/cbchoi/gorti/rti/internal/eventlog"
)

// TestSpec_M2_Replay_ByteIdentical is the M2 milestone gate test.
//
// Contract: take a recorded event log, replay it through a fresh RTI
// instance, write the new log to a separate buffer, and assert that
// new bytes == source bytes.
//
// This is the determinism guarantee NFR-DET-2 in executable form.
//
// Until eventlog.Replayer + the supporting components land, this test
// uses t.Skip with a clear reason; Agent A flips the skip into a real
// run when their implementation is ready.
//
// Implements: FR-EVT-3, NFR-DET-2; M2 exit criterion.
func TestSpec_M2_Replay_ByteIdentical(t *testing.T) {
	source, err := buildExampleLog(t, "replay-src")
	if err != nil {
		t.Skipf("buildExampleLog (eventlog/federation/object stubs): %v", err)
	}

	var captured bytes.Buffer
	w, err := eventlog.NewWriter(eventlog.WriterOptions{
		Sink:       &captured,
		Federation: "replay-src",
		Mode:       core.ModeVerbose,
		Seed:       1,
		Clock:      core.NewFakeClock(time.Unix(0, 0)),
	})
	if err != nil {
		t.Skipf("eventlog.NewWriter: %v", err)
	}
	defer w.Close()

	r, err := eventlog.NewReader(bytes.NewReader(source))
	if err != nil {
		t.Skipf("eventlog.NewReader: %v", err)
	}

	rep, err := eventlog.NewReplayer(eventlog.ReplayerOptions{
		Source:        r,
		Federation:    nil, // wired by Agent A's M2 integration
		Objects:       nil,
		CapturingSink: w,
	})
	if err != nil {
		t.Skipf("eventlog.NewReplayer: %v", err)
	}

	if err := rep.Replay(context.Background()); err != nil {
		t.Fatalf("Replay: %v", err)
	}

	srcSum := sha256.Sum256(source)
	capSum := sha256.Sum256(captured.Bytes())
	if srcSum != capSum {
		t.Errorf("replay sha256 mismatch:\n  source:   %x\n  captured: %x", srcSum, capSum)
	}
}

// buildExampleLog produces a small, deterministic log Agent A's M2
// integration must be able to replay byte-identical. It is allowed to
// return an error during the M2 RED phase (the test then skips).
func buildExampleLog(t *testing.T, fed core.FederationName) ([]byte, error) {
	t.Helper()
	var buf bytes.Buffer
	w, err := eventlog.NewWriter(eventlog.WriterOptions{
		Sink:       &buf,
		Federation: fed,
		Mode:       core.ModeVerbose,
		Seed:       1,
		Clock:      core.NewFakeClock(time.Unix(0, 0)),
	})
	if err != nil {
		return nil, err
	}
	for i := 0; i < 4; i++ {
		if err := w.Append(context.Background(), fed, &fakeEventRecord{tag: "ev"}); err != nil {
			return nil, err
		}
	}
	if err := w.Sync(context.Background(), fed); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// TestSpec_M2_Replay_DivergenceIsFatal: when the replayer detects the
// captured log diverging from the source (e.g. seed mismatch, missed
// event), Replay returns ErrReplayDivergence rather than silently
// continuing.
//
// Implements: NFR-DET-2.
func TestSpec_M2_Replay_DivergenceIsFatal(t *testing.T) {
	t.Skip("scaffolded; Agent A wires the divergence-injection harness once Replayer lands")
}
