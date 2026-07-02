// TimeService gRPC handler — translates rti.v1.TimeService RPCs into
// calls on rti/internal/time.Manager (M21 TASK-202).
//
// Owner: M21 W2A. Composed via Options.Time → newTimeService (server.go).
// The proto + manager were both frozen at M0 / M3 / M7; this file is
// the wire bridge added by M21 to expose the in-process implementation
// cross-process. See docs/M21_DISPATCH_PLAN.md §2.1, §2.3.
//
// Test cases live in time_test.go (TASK-203).

package grpc

import (
	"context"
	"math"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/cbchoi/gorti/rti/internal/core"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
)

// timeService is the concrete TimeServiceServer impl. Embeds
// UnimplementedTimeServiceServer for forward compatibility.
type timeService struct {
	rtiv1.UnimplementedTimeServiceServer
	mgr core.TimeManager
}

// newTimeService constructs the wrapper. mgr MUST be non-nil; callers
// (server.go) gate the call on Options.Time != nil.
func newTimeService(mgr core.TimeManager) *timeService {
	if mgr == nil {
		panic("newTimeService: mgr must not be nil")
	}
	return &timeService{mgr: mgr}
}

// --- Regulation / constrained controls ---

func (s *timeService) EnableTimeRegulation(
	ctx context.Context, req *rtiv1.EnableRegulationRequest,
) (*rtiv1.Empty, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "nil request")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	if err := s.mgr.EnableRegulation(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
		core.LogicalTime(req.GetLookahead()),
	); err != nil {
		return nil, errToStatus(ctx, err)
	}
	return &rtiv1.Empty{}, nil
}

func (s *timeService) DisableTimeRegulation(
	ctx context.Context, req *rtiv1.DisableRegulationRequest,
) (*rtiv1.Empty, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "nil request")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	if err := s.mgr.DisableRegulation(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
	); err != nil {
		return nil, errToStatus(ctx, err)
	}
	return &rtiv1.Empty{}, nil
}

func (s *timeService) EnableTimeConstrained(
	ctx context.Context, req *rtiv1.EnableConstrainedRequest,
) (*rtiv1.Empty, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "nil request")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	if err := s.mgr.EnableConstrained(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
	); err != nil {
		return nil, errToStatus(ctx, err)
	}
	return &rtiv1.Empty{}, nil
}

func (s *timeService) DisableTimeConstrained(
	ctx context.Context, req *rtiv1.DisableConstrainedRequest,
) (*rtiv1.Empty, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "nil request")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	if err := s.mgr.DisableConstrained(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
	); err != nil {
		return nil, errToStatus(ctx, err)
	}
	return &rtiv1.Empty{}, nil
}

// --- Advance primitives ---

func (s *timeService) NextMessageRequest(
	ctx context.Context, req *rtiv1.NERRequest,
) (*rtiv1.Empty, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "nil request")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	if err := s.mgr.NextMessageRequest(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
		core.LogicalTime(req.GetLogicalTime()),
	); err != nil {
		return nil, errToStatus(ctx, err)
	}
	return &rtiv1.Empty{}, nil
}

func (s *timeService) NextMessageRequestAvailable(
	ctx context.Context, req *rtiv1.NMRARequest,
) (*rtiv1.Empty, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "nil request")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	if err := s.mgr.NextMessageRequestAvailable(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
		core.LogicalTime(req.GetLogicalTime()),
	); err != nil {
		return nil, errToStatus(ctx, err)
	}
	return &rtiv1.Empty{}, nil
}

func (s *timeService) TimeAdvanceRequest(
	ctx context.Context, req *rtiv1.TARRequest,
) (*rtiv1.Empty, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "nil request")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	if err := s.mgr.TimeAdvanceRequest(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
		core.LogicalTime(req.GetLogicalTime()),
	); err != nil {
		return nil, errToStatus(ctx, err)
	}
	return &rtiv1.Empty{}, nil
}

func (s *timeService) TimeAdvanceRequestAvailable(
	ctx context.Context, req *rtiv1.TARARequest,
) (*rtiv1.Empty, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "nil request")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	if err := s.mgr.TimeAdvanceRequestAvailable(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
		core.LogicalTime(req.GetLogicalTime()),
	); err != nil {
		return nil, errToStatus(ctx, err)
	}
	return &rtiv1.Empty{}, nil
}

func (s *timeService) FlushQueueRequest(
	ctx context.Context, req *rtiv1.FQRRequest,
) (*rtiv1.Empty, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "nil request")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	if err := s.mgr.FlushQueueRequest(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
		core.LogicalTime(req.GetLogicalTime()),
	); err != nil {
		return nil, errToStatus(ctx, err)
	}
	return &rtiv1.Empty{}, nil
}

// --- Lookahead modification ---

func (s *timeService) ModifyLookahead(
	ctx context.Context, req *rtiv1.ModifyLookaheadRequest,
) (*rtiv1.Empty, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "nil request")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	if err := s.mgr.ModifyLookahead(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
		core.LogicalTime(req.GetLookahead()),
	); err != nil {
		return nil, errToStatus(ctx, err)
	}
	return &rtiv1.Empty{}, nil
}

// --- Queries (synchronous reads) ---

// snapshotFor returns the federate's TimeFederateState by walking the
// federation-wide snapshot. Helper because TimeManager exposes only
// per-federation Snapshot, not per-federate.
func (s *timeService) snapshotFor(
	fed core.FederationName, h core.FederateHandle,
) (core.TimeFederateState, bool) {
	snap := s.mgr.Snapshot(fed)
	for i := range snap.Federates {
		if snap.Federates[i].Handle == h {
			return snap.Federates[i], true
		}
	}
	return core.TimeFederateState{}, false
}

func (s *timeService) QueryLogicalTime(
	_ context.Context, req *rtiv1.QueryFederateTimeRequest,
) (*rtiv1.QueryFederateTimeResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "nil request")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	st, _ := s.snapshotFor(
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
	)
	// Federate-not-present yields zero-value TimeFederateState — which
	// has CurrentTime = 0. Match the manager's convention: federation
	// start time is 0; absent federate is indistinguishable from a
	// joined-but-not-yet-advanced federate. Per plan §2.3.2, NotFound
	// is NOT surfaced from the time path.
	return &rtiv1.QueryFederateTimeResponse{
		LogicalTime: float64(st.CurrentTime),
	}, nil
}

func (s *timeService) QueryLookahead(
	ctx context.Context, req *rtiv1.QueryFederateTimeRequest,
) (*rtiv1.QueryLookaheadResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "nil request")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	st, ok := s.snapshotFor(
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
	)
	// Per plan §2.3.1 row "TASK-203.14" + plan AC §3.6: post-disable
	// (and not-yet-enabled) federates surface ErrTimeRegulationNotEnabled
	// rather than a silent 0.0 — lookahead is meaningful only while
	// regulating.
	if !ok || !st.Regulating {
		return nil, errToStatus(ctx, core.ErrTimeNotRegulating)
	}
	return &rtiv1.QueryLookaheadResponse{
		Lookahead: float64(st.Lookahead),
	}, nil
}

func (s *timeService) QueryLBTS(
	_ context.Context, req *rtiv1.QueryLBTSRequest,
) (*rtiv1.QueryLBTSResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "nil request")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	snap := s.mgr.Snapshot(core.FederationName(req.GetFederationName()))
	lbts := float64(snap.LBTS)
	// Translate +Inf sentinel (no regulators) → finite=false, lbts=0
	// per plan §2.2 / TASK-203.12.
	if math.IsInf(lbts, 1) {
		return &rtiv1.QueryLBTSResponse{Lbts: 0, Finite: false}, nil
	}
	return &rtiv1.QueryLBTSResponse{Lbts: lbts, Finite: true}, nil
}

// --- M20.1: queryGALT + queryLITS (§8.19 / §8.20) ---

// QueryGALT computes the Greatest Available Logical Time for the
// calling federate: min(currentTime + lookahead) over all OTHER
// regulating federates. When no other federate is regulating the
// response is finite=false, galt=0 (caller treats as +Inf).
func (s *timeService) QueryGALT(
	_ context.Context, req *rtiv1.QueryFederateTimeRequest,
) (*rtiv1.QueryGALTResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "nil request")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	snap := s.mgr.Snapshot(core.FederationName(req.GetFederationName()))
	selfHandle := core.FederateHandle(req.GetFederateHandle())
	var (
		minVal float64
		seen   bool
	)
	for i := range snap.Federates {
		f := &snap.Federates[i]
		if f.Handle == selfHandle {
			continue
		}
		if !f.Regulating {
			continue
		}
		v := float64(f.CurrentTime) + float64(f.Lookahead)
		if !seen || v < minVal {
			minVal = v
			seen = true
		}
	}
	if !seen {
		return &rtiv1.QueryGALTResponse{Galt: 0, Finite: false}, nil
	}
	return &rtiv1.QueryGALTResponse{Galt: minVal, Finite: true}, nil
}

// QueryLITS asks the time manager for the smallest buffered TSO
// timestamp for the calling federate. When async delivery is ON OR
// the buffer is empty, the response is finite=false.
func (s *timeService) QueryLITS(
	_ context.Context, req *rtiv1.QueryFederateTimeRequest,
) (*rtiv1.QueryLITSResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "nil request")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	lits, has := s.mgr.QueryLITS(
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
	)
	if !has {
		return &rtiv1.QueryLITSResponse{Lits: 0, Finite: false}, nil
	}
	return &rtiv1.QueryLITSResponse{Lits: float64(lits), Finite: true}, nil
}

// --- M22 W2: Asynchronous-delivery toggle ---

func (s *timeService) EnableAsynchronousDelivery(
	ctx context.Context, req *rtiv1.EnableAsynchronousDeliveryRequest,
) (*rtiv1.Empty, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "nil request")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	if err := s.mgr.EnableAsynchronousDelivery(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
	); err != nil {
		return nil, errToStatus(ctx, err)
	}
	return &rtiv1.Empty{}, nil
}

func (s *timeService) DisableAsynchronousDelivery(
	ctx context.Context, req *rtiv1.DisableAsynchronousDeliveryRequest,
) (*rtiv1.Empty, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "nil request")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	if err := s.mgr.DisableAsynchronousDelivery(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
	); err != nil {
		return nil, errToStatus(ctx, err)
	}
	return &rtiv1.Empty{}, nil
}
