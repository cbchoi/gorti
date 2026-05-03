package m3spec

import (
	"testing"
)

// TestSpec_M3_Replay_TimeAdvanceEventsByteIdentical is the M3 milestone
// gate test (TASK-049). Contract: an event log captured from a
// time-managed federation (regulation enable, NER request, NER grant,
// stall detection if any) replays through a fresh Manager and the new
// log is byte-identical to the source.
//
// This is the M3-shaped instance of the NFR-DET-2 guarantee already
// proven for M2 (rti/spec/M2/replay_test.go::TestSpec_M2_Replay_ByteIdentical).
//
// SCAFFOLD: this test is intentionally skipped until the
// examples/go-timed harness lands (per TASK-046). Agent A wires the
// real test by:
//
//  1. Building an example log via examples/go-timed with FakeClock and
//     a fixed seed.
//  2. Replaying through eventlog.Replayer with the timepkg.Manager as
//     the dispatch target (M3 events: TimeAdvanceRequested,
//     TimeAdvanceGranted, FederationHalted).
//  3. Comparing sha256(source) == sha256(captured).
//
// Implements: FR-EVT-3, NFR-DET-2; M3 exit criterion.
func TestSpec_M3_Replay_TimeAdvanceEventsByteIdentical(t *testing.T) {
	t.Skip("scaffolded; Agent A turns this into a real test once examples/go-timed (TASK-046) and the time-event replay path land")
}

// TestSpec_M3_Replay_StallEventReplays: a captured FederationHalted
// (cause: stall) record replays through a fresh Manager and emerges
// in the captured log byte-identical. This proves stall detection is
// not wall-clock-dependent at replay time — the recorded event is the
// authoritative source of truth.
//
// Implements: FR-EVT-3, FR-TM-6, NFR-DET-2.
func TestSpec_M3_Replay_StallEventReplays(t *testing.T) {
	t.Skip("scaffolded; depends on stall replay path landing (TASK-045 + replayer wiring)")
}
