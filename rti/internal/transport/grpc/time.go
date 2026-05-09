// Scaffold owned by TASK-202 (M21) — see docs/M21_DISPATCH_PLAN.md §2.1, §2.3.
//
// Wraps *time.Manager and serves the rti.v1.TimeService RPCs. Fully
// implementing this requires TASK-201's regen of TimeServiceServer
// to expose all 12 methods (NER, NMRA, TAR, TARA, FQR, ModifyLookahead,
// 4 enable/disable, 3 query). Until regen lands, this file declares
// the timeService struct and its constructor; the per-RPC bodies are
// added by the W2A sub-agent after regen so the file always compiles.
//
// SCAFFOLD CONTRACT: do not add per-RPC method bodies here BEFORE
// TASK-201 lands — partial implementations risk breaking the build
// when regen completes.

package grpc

import (
	"github.com/cbchoi/gorti/rti/internal/time"
)

// timeService wraps a *time.Manager and serves the rti.v1.TimeService
// gRPC surface. Construction is conditional in server.go: when
// Options.Time is set (typed core.TimeManager today; see W2A note in
// plan §5), the server registers timeService.
type timeService struct {
	// rtiv1.UnimplementedTimeServiceServer is embedded by W2A
	// post-regen so unimplemented methods return Unimplemented
	// rather than nil-deref.
	mgr *time.Manager //nolint:unused // wired by TASK-202
}

// newTimeService constructs the wrapper. mgr MUST be non-nil; callers
// gate the call (server.go) on Options.Time != nil.
func newTimeService(mgr *time.Manager) *timeService {
	if mgr == nil {
		panic("newTimeService: mgr must not be nil")
	}
	return &timeService{mgr: mgr}
}

// Per-RPC handlers (TASK-202 + post-TASK-201 regen):
//
//   - EnableTimeRegulation(ctx, req) (*rtiv1.Empty, error)
//   - DisableTimeRegulation(ctx, req) (*rtiv1.Empty, error)
//   - EnableTimeConstrained(ctx, req) (*rtiv1.Empty, error)
//   - DisableTimeConstrained(ctx, req) (*rtiv1.Empty, error)
//   - NextMessageRequest(ctx, req) (*rtiv1.Empty, error)
//   - NextMessageRequestAvailable(ctx, req) (*rtiv1.Empty, error)
//   - TimeAdvanceRequest(ctx, req) (*rtiv1.Empty, error)
//   - TimeAdvanceRequestAvailable(ctx, req) (*rtiv1.Empty, error)
//   - FlushQueueRequest(ctx, req) (*rtiv1.Empty, error)
//   - ModifyLookahead(ctx, req) (*rtiv1.Empty, error)
//   - QueryLogicalTime(ctx, req) (*QueryFederateTimeResponse, error)
//   - QueryLookahead(ctx, req) (*QueryLookaheadResponse, error)
//   - QueryLBTS(ctx, req) (*QueryLBTSResponse, error)
//
// Each translates: validate request fields → translate to manager call →
// translate manager error to gRPC status (per §2.3.1) → return Empty
// or query response.
//
// The W2A sub-agent adds these handlers post-regen. Until then,
// TimeServiceServer is the 5-method cut-1 interface and embedding
// rtiv1.UnimplementedTimeServiceServer covers it. Adding the handlers
// before regen would create dead code that gets shadowed.
