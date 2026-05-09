// Scaffold owned by TASK-206 (M21) — see docs/M21_DISPATCH_PLAN.md §2.7.1.
//
// The Federate methods below mirror the gRPC TimeService surface
// extended by TASK-201. Bodies are TODO panics; W3A fills them in
// after W2½ ships the foundation in federate.go.

package federate

import "context"

// EnableTimeRegulation enables time regulation with the supplied lookahead.
// lookahead MUST be >= 0 and finite. Returns ErrTimeRegulationAlreadyEnabled
// if already regulating, ErrInvalidLookahead on bad input.
func (f *Federate) EnableTimeRegulation(ctx context.Context, lookahead float64) error {
	_ = ctx
	_ = lookahead
	panic("TODO: TASK-206 — implement EnableTimeRegulation")
}

// DisableTimeRegulation. Returns ErrTimeRegulationNotEnabled if not regulating.
func (f *Federate) DisableTimeRegulation(ctx context.Context) error {
	_ = ctx
	panic("TODO: TASK-206 — implement DisableTimeRegulation")
}

// EnableTimeConstrained enables constrained mode.
func (f *Federate) EnableTimeConstrained(ctx context.Context) error {
	_ = ctx
	panic("TODO: TASK-206 — implement EnableTimeConstrained")
}

// DisableTimeConstrained.
func (f *Federate) DisableTimeConstrained(ctx context.Context) error {
	_ = ctx
	panic("TODO: TASK-206 — implement DisableTimeConstrained")
}

// ModifyLookahead updates the current lookahead without round-tripping
// through Disable + Enable. Wraps Manager.ModifyLookahead added by TASK-202b.
func (f *Federate) ModifyLookahead(ctx context.Context, lookahead float64) error {
	_ = ctx
	_ = lookahead
	panic("TODO: TASK-206 — implement ModifyLookahead")
}

// NextMessageRequest issues NER(t). Grant arrives later on Events().
// Returns ErrTimeAdvancingState if another advance primitive is already pending.
func (f *Federate) NextMessageRequest(ctx context.Context, t float64) error {
	_ = ctx
	_ = t
	panic("TODO: TASK-206 — implement NextMessageRequest")
}

// NextMessageRequestAvailable issues NMRA(t). NMRA's grant gate is
// inclusive (LBTS >= t), differing from NER's strict (LBTS > t).
func (f *Federate) NextMessageRequestAvailable(ctx context.Context, t float64) error {
	_ = ctx
	_ = t
	panic("TODO: TASK-206 — implement NextMessageRequestAvailable")
}

// TimeAdvanceRequest issues TAR(t). Grant fires only when LBTS >= t
// (vs NER's "first message OR t" semantics).
func (f *Federate) TimeAdvanceRequest(ctx context.Context, t float64) error {
	_ = ctx
	_ = t
	panic("TODO: TASK-206 — implement TimeAdvanceRequest")
}

// TimeAdvanceRequestAvailable issues TARA(t). Available + advance-to combined.
func (f *Federate) TimeAdvanceRequestAvailable(ctx context.Context, t float64) error {
	_ = ctx
	_ = t
	panic("TODO: TASK-206 — implement TimeAdvanceRequestAvailable")
}

// FlushQueueRequest issues FQR(t). Queued events are delivered via Events()
// before the grant arrives; cut-1 simplification grants at LBTS, not requested t.
func (f *Federate) FlushQueueRequest(ctx context.Context, t float64) error {
	_ = ctx
	_ = t
	panic("TODO: TASK-206 — implement FlushQueueRequest")
}

// QueryLogicalTime returns the federate's current logical time.
func (f *Federate) QueryLogicalTime(ctx context.Context) (float64, error) {
	_ = ctx
	panic("TODO: TASK-206 — implement QueryLogicalTime")
}

// QueryLookahead returns the federate's current lookahead.
// Returns (0, ErrTimeRegulationNotEnabled) if the federate has been
// disabled (post-disable lookahead is not meaningful — see plan AC §3 / 203.14).
func (f *Federate) QueryLookahead(ctx context.Context) (float64, error) {
	_ = ctx
	panic("TODO: TASK-206 — implement QueryLookahead")
}

// QueryLBTS returns the federation-wide Lower Bound on Time Stamp.
// finite is false when no federate is regulating; lbts is then 0
// (the manager returns +Inf internally; SDK translates).
func (f *Federate) QueryLBTS(ctx context.Context) (lbts float64, finite bool, err error) {
	_ = ctx
	panic("TODO: TASK-206 — implement QueryLBTS")
}
