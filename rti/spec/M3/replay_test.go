package m3spec

import (
	"bytes"
	"context"
	"crypto/sha256"
	"testing"
	stdtime "time"

	"github.com/cbchoi/gorti/rti/internal/core"
	"github.com/cbchoi/gorti/rti/internal/eventlog"
	timepkg "github.com/cbchoi/gorti/rti/internal/time"
)

// TestSpec_M3_Replay_TimeAdvanceEventsByteIdentical is the M3 milestone
// gate test (TASK-049). Contract: an event log captured from a
// time-managed federation (regulation enable, NER request, NER grant)
// replays through eventlog.NewReplayer and the captured stream is
// byte-identical to the source.
//
// This is the M3-shaped instance of the NFR-DET-2 guarantee already
// proven for M2 (rti/spec/M2/replay_test.go::TestSpec_M2_Replay_ByteIdentical).
//
// The harness: build a small, deterministic source log directly off
// the time package (Manager + permissive log + 3 regulating federates
// running NER) writing into a *bytes.Buffer, then replay it through
// eventlog.NewReplayer into a fresh CapturingSink and assert sha256
// equality of the body region.
//
// The example/go-timed/replay_test.go covers the production-equivalent
// path through the rtid binary; this spec test exercises the same
// invariant at the package boundary so package-level breakage is
// detected even when the example shim isn't run.
//
// Implements: FR-EVT-3, NFR-DET-2; M3 exit criterion.
func TestSpec_M3_Replay_TimeAdvanceEventsByteIdentical(t *testing.T) {
	const fed core.FederationName = "m3-replay"

	source, err := buildTimedExampleLog(fed, false /*withStall*/)
	if err != nil {
		t.Fatalf("buildTimedExampleLog: %v", err)
	}
	if len(source) <= eventlog.HeaderSize {
		t.Fatalf("source log has no body (len %d <= header %d)", len(source), eventlog.HeaderSize)
	}

	captured, err := replayLog(source, fed)
	if err != nil {
		t.Fatalf("replayLog: %v", err)
	}

	srcBody := source[eventlog.HeaderSize:]
	capBody := captured[eventlog.HeaderSize:]
	if !bytes.Equal(srcBody, capBody) {
		srcSum := sha256.Sum256(srcBody)
		capSum := sha256.Sum256(capBody)
		t.Errorf("M3 replay body sha256 mismatch:\n  source:   %x  (%d bytes)\n  captured: %x  (%d bytes)",
			srcSum, len(srcBody), capSum, len(capBody))
	}
}

// TestSpec_M3_Replay_StallEventReplays: a captured FederationHalted
// (cause: stall) record replays through eventlog.NewReplayer and the
// captured stream is byte-identical to the source. This proves stall
// detection's recorded event is the authoritative source of truth at
// replay time — the replayer does NOT recompute halt detection from
// wall clocks.
//
// Implements: FR-EVT-3, FR-TM-6, NFR-DET-2.
func TestSpec_M3_Replay_StallEventReplays(t *testing.T) {
	const fed core.FederationName = "m3-replay-stall"

	source, err := buildTimedExampleLog(fed, true /*withStall*/)
	if err != nil {
		t.Fatalf("buildTimedExampleLog: %v", err)
	}
	captured, err := replayLog(source, fed)
	if err != nil {
		t.Fatalf("replayLog: %v", err)
	}
	srcBody := source[eventlog.HeaderSize:]
	capBody := captured[eventlog.HeaderSize:]
	if !bytes.Equal(srcBody, capBody) {
		srcSum := sha256.Sum256(srcBody)
		capSum := sha256.Sum256(capBody)
		t.Errorf("M3 stall-replay body sha256 mismatch:\n  source:   %x\n  captured: %x", srcSum, capSum)
	}
}

// buildTimedExampleLog drives a fresh time.Manager through a small
// deterministic NER schedule (3 regulating federates, lookaheads
// {1.0, 2.0, 0.5}, 5 ticks) and returns the resulting on-disk log
// bytes. When withStall is true it advances the FakeClock past the
// stall timeout and runs CheckStalls to append a FederationHalted
// record.
//
// Mirrors the example/go-timed runner's behaviour but stays inside
// the spec package so the replay test doesn't depend on cmd/rtid.
func buildTimedExampleLog(fed core.FederationName, withStall bool) ([]byte, error) {
	var buf bytes.Buffer
	clk := core.NewFakeClock(stdtime.Unix(0, 0))
	w, err := eventlog.NewWriter(eventlog.WriterOptions{
		Sink:       &buf,
		Federation: fed,
		Mode:       core.ModeVerbose,
		Seed:       1,
		Clock:      clk,
	})
	if err != nil {
		return nil, err
	}
	defer func() { _ = w.Close() }()

	outbox := newFakeOutbox()
	mgr, err := timepkg.New(timepkg.Options{
		Clock:        clk,
		Outbox:       outbox,
		EventLog:     w, // Writer satisfies core.EventLog
		StallTimeout: 5 * stdtime.Second,
	})
	if err != nil {
		return nil, err
	}

	ctx := context.Background()
	lookaheads := []core.LogicalTime{1.0, 2.0, 0.5}
	for i, look := range lookaheads {
		if err := mgr.EnableRegulation(ctx, fed, core.FederateHandle(uint64(i+1)), look); err != nil {
			return nil, err
		}
	}

	if withStall {
		// Skip federate 2's NER so federate 1's request stays
		// stuck pending past the stall timeout.
		if err := mgr.NextMessageRequest(ctx, fed, 1, core.LogicalTime(10.0)); err != nil {
			return nil, err
		}
		if err := mgr.NextMessageRequest(ctx, fed, 3, core.LogicalTime(10.0)); err != nil {
			return nil, err
		}
		clk.Advance(10 * stdtime.Second)
		_ = mgr.CheckStalls(ctx)
	} else {
		// Standard NER schedule: 5 ticks, each federate advances by
		// max(step, lookahead). Skip duplicate-NER errors (forced
		// grants leave a federate pending until peers catch up).
		currentTimes := make([]core.LogicalTime, len(lookaheads))
		for tick := 0; tick < 5; tick++ {
			for i, look := range lookaheads {
				h := core.FederateHandle(uint64(i + 1))
				delta := core.LogicalTime(1.0)
				if look > delta {
					delta = look
				}
				target := currentTimes[i] + delta
				if err := mgr.NextMessageRequest(ctx, fed, h, target); err == nil {
					currentTimes[i] = target
				}
			}
		}
	}
	if err := w.Sync(ctx, fed); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// replayLog drives source through eventlog.NewReplayer with a fresh
// CapturingSink and returns the captured byte stream.
//
// The captured Writer pins its Clock to the source header's
// CreatedAtNs so the captured header is byte-equal to the source
// (mirrors rtid's runReplayFromFile contract).
func replayLog(source []byte, fed core.FederationName) ([]byte, error) {
	rdr, err := eventlog.NewReader(bytes.NewReader(source))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rdr.Close() }()
	hdr := rdr.Header()

	var capturedBuf bytes.Buffer
	captureClock := core.NewFakeClock(stdtime.Unix(0, int64(hdr.CreatedAtNs))) //nolint:gosec
	w, err := eventlog.NewWriter(eventlog.WriterOptions{
		Sink:       &capturedBuf,
		Federation: fed,
		Mode:       hdr.Mode,
		Seed:       hdr.Seed,
		Clock:      captureClock,
	})
	if err != nil {
		return nil, err
	}
	defer func() { _ = w.Close() }()

	rep, err := eventlog.NewReplayer(eventlog.ReplayerOptions{
		Source:         rdr,
		CapturingSink:  w,
		CapturedBuffer: &capturedBuf,
	})
	if err != nil {
		return nil, err
	}
	if err := rep.Replay(context.Background()); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return capturedBuf.Bytes(), nil
}
