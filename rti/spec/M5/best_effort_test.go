package m5spec

import (
	"testing"
)

// TestSpec_M5_BestEffort_RODelivery: in a federation with mode=BestEffort,
// updates to a best-effort attribute are delivered RO (Receive Order)
// rather than TSO (Time Stamp Order). Specifically: the OutboundEvent
// surfaced to the subscriber's outbox carries no timestamp.
//
// The wire-level invariant per docs/agent-a-rti-core.md §5.7: when the
// federation is in best-effort mode AND the attribute is declared
// best-effort in the FOM, the publisher's update bypasses the TSO queue
// and reaches subscribers immediately. The Outbox.Send call carries an
// OutboundEvent whose timestamp accessor returns nil (matching
// core.Outbox semantics: nil = RO; non-nil = TSO).
//
// SCAFFOLD: this test is a skip-scaffold pending Agent A's TASK-077
// implementation. The full integration requires:
//
//   1. A federation manager (M2) configured with mode=BestEffort.
//   2. A declaration manager that reads the FOM's per-attribute order
//      attribute (TimeStamp vs Receive).
//   3. An object registry (M2 W2A) that consults BOTH the federation
//      mode AND the attribute's declared order on the update path.
//
// Agent A unskips this test in TASK-077 by replacing the body with a
// real end-to-end scenario.
//
// Implements: FR-OM-3; M5 RO/TSO contract.
func TestSpec_M5_BestEffort_RODelivery(t *testing.T) {
	t.Skip("scaffolded; Agent A wires the real end-to-end test in TASK-077 (depends on M2 federation + declaration + object stack already on main)")
}

// TestSpec_M5_BestEffort_VerboseModeStillTSO: in a federation with
// mode=Verbose (the default), updates remain TSO regardless of the
// attribute's declared order. This catches a regression where TASK-077's
// best-effort path accidentally short-circuits TSO for ALL attributes.
//
// SCAFFOLD: same as above; Agent A turns this into a real test.
//
// Implements: FR-OM-3.
func TestSpec_M5_BestEffort_VerboseModeStillTSO(t *testing.T) {
	t.Skip("scaffolded; Agent A unskips after TASK-077 lands the verbose-mode regression check")
}
