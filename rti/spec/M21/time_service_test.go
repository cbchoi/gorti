// Scaffold owned by TASK-214 (M21) — see docs/M21_DISPATCH_PLAN.md §6.
//
// Binds the M21 acceptance criteria §3 to executable assertions, Go side.
// Cross-language counterparts in pysdk/tests/spec/m21/.

package m21spec

import "testing"

// 209.1 — TimeService is registered. RPCs return non-Unimplemented.
func TestACTimeServiceRegistered(t *testing.T) {
	t.Skip("TODO: TASK-214 — binds AC §3.2")
}

// 209.2 — EnableTimeRegulation/Constrained round-trip.
func TestACEnableRoundTrip(t *testing.T) {
	t.Skip("TODO: TASK-214 — binds AC §3 row covering enable/disable")
}

// 209.3 — All 5 advance primitives produce grants on the wire.
func TestACAllPrimitivesProduceGrants(t *testing.T) {
	t.Skip("TODO: TASK-214 — binds AC §3.3; subtests for NER/NMRA/TAR/TARA/FQR")
}

// 209.4 — Error mapping correct (every row of §2.3.1).
func TestACErrorMapping(t *testing.T) {
	t.Skip("TODO: TASK-214 — binds AC §3.4")
}

// 209.5 — examples/go-timed runs cleanly.
func TestACGoTimedExample(t *testing.T) {
	t.Skip("TODO: TASK-214 — binds AC §3.7; invokes examples/go-timed runner")
}

// 209.6 — Stream conversion handles time.TimeAdvanceGrant.
func TestACStreamConversion(t *testing.T) {
	t.Skip("TODO: TASK-214 — binds AC §3.11")
}

// 209.7 — Manager.ModifyLookahead exists and works.
func TestACModifyLookahead(t *testing.T) {
	t.Skip("TODO: TASK-214 — binds AC §3.12")
}

// 209.8 — Federate scaffold landed (rti/pkg/federate compiles + smoke).
func TestACFederateScaffold(t *testing.T) {
	t.Skip("TODO: TASK-214 — binds AC §3.13")
}

// 209.9 — Stall → FederationHalted on the wire.
func TestACStallHaltedWire(t *testing.T) {
	t.Skip("TODO: TASK-214 — binds AC §3.14")
}
