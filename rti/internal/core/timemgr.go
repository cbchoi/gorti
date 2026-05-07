package core

import "context"

// TimeManager governs HLA time management per IEEE 1516.1-2010 §8.
//
// Cut 1 (MVP) — NER only.
// Cut 2 (M7+) — adds the other three time-advance primitives:
//   - TimeAdvanceRequest(t)             (TAR)   — §8.10
//   - TimeAdvanceRequestAvailable(t)    (TARA)  — §8.11 (allows grants AT t even with regulating peers exactly at t)
//   - FlushQueueRequest(t)              (FQR)   — §8.13 (cancels any TSO traffic ≤ t)
//   - NextMessageRequestAvailable(t)    (NMRA)  — §8.12 (NER variant that allows grants AT t)
//
// All four primitives share the LBTS computation and grant-emission
// machinery from cut 1. The semantic differences are:
//   - NER:   grant fires at t' = min(t, LBTS); fed advances to t'.
//   - NMRA:  same as NER but grant time may equal LBTS (allows other federates' messages at exactly LBTS).
//   - TAR:   grant fires at t' = min(t, LBTS); but federate must advance ALL the way to t' (no early grants).
//   - TARA:  same as TAR but grant time may equal LBTS.
//   - FQR:   request the RTI to flush the federate's TSO queue ≤ t; grant fires when queue is drained.
//
// Grants are emitted via Outbox.
type TimeManager interface {
	EnableRegulation(ctx context.Context, fed FederationName, h FederateHandle, lookahead LogicalTime) error
	DisableRegulation(ctx context.Context, fed FederationName, h FederateHandle) error

	EnableConstrained(ctx context.Context, fed FederationName, h FederateHandle) error
	DisableConstrained(ctx context.Context, fed FederationName, h FederateHandle) error

	// NextMessageRequest — IEEE 1516.1-2010 §8.10. Cut 1.
	NextMessageRequest(ctx context.Context, fed FederationName, h FederateHandle, t LogicalTime) error

	// NextMessageRequestAvailable — IEEE 1516.1-2010 §8.12. Cut 2 (M7).
	NextMessageRequestAvailable(ctx context.Context, fed FederationName, h FederateHandle, t LogicalTime) error

	// TimeAdvanceRequest — IEEE 1516.1-2010 §8.10. Cut 2 (M7).
	TimeAdvanceRequest(ctx context.Context, fed FederationName, h FederateHandle, t LogicalTime) error

	// TimeAdvanceRequestAvailable — IEEE 1516.1-2010 §8.11. Cut 2 (M7).
	TimeAdvanceRequestAvailable(ctx context.Context, fed FederationName, h FederateHandle, t LogicalTime) error

	// FlushQueueRequest — IEEE 1516.1-2010 §8.13. Cut 2 (M7).
	// Drains the federate's TSO queue up to t and emits a grant when complete.
	FlushQueueRequest(ctx context.Context, fed FederationName, h FederateHandle, t LogicalTime) error

	// --- Read-only introspection (rtid-TUI Phase 1) ----------------------

	// Snapshot returns the per-federation time-management view for
	// the AdminService handler. Read-only; cheap.
	Snapshot(fed FederationName) TimeSnapshot
}

// TimeFederateState bundles one federate's time-management snapshot.
// Phase 1 of the rtid-TUI plan: consumed by the AdminService handler
// to populate FederateSnapshot.{current_time, pending_request_time,
// lookahead, regulating, constrained}.
type TimeFederateState struct {
	Handle             FederateHandle
	CurrentTime        LogicalTime
	HasPendingRequest  bool
	PendingRequestTime LogicalTime
	Lookahead          LogicalTime
	Regulating         bool
	Constrained        bool
}

// TimeSnapshot is the federation-wide time view for the AdminService
// Snapshot RPC. The TUI's "Time advance" view (docs/rtid-tui.md §3.3)
// consumes the LBTS + per-federate detail.
type TimeSnapshot struct {
	// LBTS is the current Lower Bound on Time Stamp over the
	// regulating set. PositiveInfinity when no federate is regulating.
	LBTS LogicalTime

	// Federates carries every federate that has any time-management
	// state recorded for the federation, in handle-sorted order.
	Federates []TimeFederateState
}
