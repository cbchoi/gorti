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
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type timeService = timeServiceOf[core.LogicalTime]

func newTimeService(mgr core.TimeManager) *timeService { return newTimeServiceOf(mgr) }

// timeService is the concrete TimeServiceServer impl. Embeds
// UnimplementedTimeServiceServer for forward compatibility.
type timeServiceOf[T core.LogicalTimeScalar] struct {
	rtiv1.UnimplementedTimeServiceServer
	mgr core.TimeManagerOf[T]
}

// newTimeService constructs the wrapper. mgr MUST be non-nil; callers
// (server.go) gate the call on Options.Time != nil.
func newTimeServiceOf[T core.LogicalTimeScalar](mgr core.TimeManagerOf[T]) *timeServiceOf[T] {
	if mgr == nil {
		panic("newTimeService: mgr must not be nil")
	}
	return &timeServiceOf[T]{mgr: mgr}
}

// --- Regulation / constrained controls ---

func (s *timeServiceOf[T]) EnableTimeRegulation(
	ctx context.Context, req *rtiv1.EnableRegulationRequest,
) (*rtiv1.Empty, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "nil request")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	lookahead, err := decodeScalarInterval[T](req.GetLookahead(), req.GetLookaheadValue())
	if err != nil {
		return nil, errToStatus(ctx, fmt.Errorf("%w: %v", core.ErrTimeInvalidLookahead, err))
	}
	if err := s.mgr.EnableRegulation(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()), lookahead,
	); err != nil {
		return nil, errToStatus(ctx, err)
	}
	return &rtiv1.Empty{}, nil
}

func (s *timeServiceOf[T]) DisableTimeRegulation(
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

func (s *timeServiceOf[T]) EnableTimeConstrained(
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

func (s *timeServiceOf[T]) DisableTimeConstrained(
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

func (s *timeServiceOf[T]) NextMessageRequest(
	ctx context.Context, req *rtiv1.NERRequest,
) (*rtiv1.Empty, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "nil request")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	timestamp, err := decodeScalarTime[T](req.GetLogicalTime(), req.GetLogicalTimeValue())
	if err != nil {
		return nil, errToStatus(ctx, fmt.Errorf("%w: %v", core.ErrTimeInvalidLogicalTime, err))
	}
	if err := s.mgr.NextMessageRequest(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()), timestamp,
	); err != nil {
		return nil, errToStatus(ctx, err)
	}
	return &rtiv1.Empty{}, nil
}

func (s *timeServiceOf[T]) NextMessageRequestAvailable(
	ctx context.Context, req *rtiv1.NMRARequest,
) (*rtiv1.Empty, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "nil request")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	timestamp, err := decodeScalarTime[T](req.GetLogicalTime(), req.GetLogicalTimeValue())
	if err != nil {
		return nil, errToStatus(ctx, fmt.Errorf("%w: %v", core.ErrTimeInvalidLogicalTime, err))
	}
	if err := s.mgr.NextMessageRequestAvailable(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()), timestamp,
	); err != nil {
		return nil, errToStatus(ctx, err)
	}
	return &rtiv1.Empty{}, nil
}

func (s *timeServiceOf[T]) TimeAdvanceRequest(
	ctx context.Context, req *rtiv1.TARRequest,
) (*rtiv1.Empty, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "nil request")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	timestamp, err := decodeScalarTime[T](req.GetLogicalTime(), req.GetLogicalTimeValue())
	if err != nil {
		return nil, errToStatus(ctx, fmt.Errorf("%w: %v", core.ErrTimeInvalidLogicalTime, err))
	}
	if err := s.mgr.TimeAdvanceRequest(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()), timestamp,
	); err != nil {
		return nil, errToStatus(ctx, err)
	}
	return &rtiv1.Empty{}, nil
}

func (s *timeServiceOf[T]) TimeAdvanceRequestAvailable(
	ctx context.Context, req *rtiv1.TARARequest,
) (*rtiv1.Empty, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "nil request")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	timestamp, err := decodeScalarTime[T](req.GetLogicalTime(), req.GetLogicalTimeValue())
	if err != nil {
		return nil, errToStatus(ctx, fmt.Errorf("%w: %v", core.ErrTimeInvalidLogicalTime, err))
	}
	if err := s.mgr.TimeAdvanceRequestAvailable(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()), timestamp,
	); err != nil {
		return nil, errToStatus(ctx, err)
	}
	return &rtiv1.Empty{}, nil
}

func (s *timeServiceOf[T]) FlushQueueRequest(
	ctx context.Context, req *rtiv1.FQRRequest,
) (*rtiv1.Empty, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "nil request")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	timestamp, err := decodeScalarTime[T](req.GetLogicalTime(), req.GetLogicalTimeValue())
	if err != nil {
		return nil, errToStatus(ctx, fmt.Errorf("%w: %v", core.ErrTimeInvalidLogicalTime, err))
	}
	if err := s.mgr.FlushQueueRequest(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()), timestamp,
	); err != nil {
		return nil, errToStatus(ctx, err)
	}
	return &rtiv1.Empty{}, nil
}

// --- Lookahead modification ---

func (s *timeServiceOf[T]) ModifyLookahead(
	ctx context.Context, req *rtiv1.ModifyLookaheadRequest,
) (*rtiv1.Empty, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "nil request")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	lookahead, err := decodeScalarInterval[T](req.GetLookahead(), req.GetLookaheadValue())
	if err != nil {
		return nil, errToStatus(ctx, fmt.Errorf("%w: %v", core.ErrTimeInvalidLookahead, err))
	}
	if err := s.mgr.ModifyLookahead(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()), lookahead,
	); err != nil {
		return nil, errToStatus(ctx, err)
	}
	return &rtiv1.Empty{}, nil
}

// --- Queries (synchronous reads) ---

// snapshotFor returns the federate's TimeFederateState by walking the
// federation-wide snapshot. Helper because TimeManager exposes only
// per-federation Snapshot, not per-federate.
func (s *timeServiceOf[T]) snapshotFor(
	fed core.FederationName, h core.FederateHandle,
) (core.TimeFederateStateOf[T], bool) {
	snap := s.mgr.Snapshot(fed)
	for i := range snap.Federates {
		if snap.Federates[i].Handle == h {
			return snap.Federates[i], true
		}
	}
	return core.TimeFederateStateOf[T]{}, false
}

func (s *timeServiceOf[T]) QueryLogicalTime(
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
	legacy, exact, err := encodeScalarTime(st.CurrentTime)
	if err != nil {
		return nil, err
	}
	return &rtiv1.QueryFederateTimeResponse{LogicalTime: legacy, LogicalTimeValue: exact}, nil
}

func (s *timeServiceOf[T]) QueryLookahead(
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
	legacy, exact, err := encodeScalarInterval(st.Lookahead)
	if err != nil {
		return nil, err
	}
	return &rtiv1.QueryLookaheadResponse{Lookahead: legacy, LookaheadValue: exact}, nil
}

func (s *timeServiceOf[T]) QueryLBTS(
	_ context.Context, req *rtiv1.QueryLBTSRequest,
) (*rtiv1.QueryLBTSResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "nil request")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	snap := s.mgr.Snapshot(core.FederationName(req.GetFederationName()))
	// Translate +Inf sentinel (no regulators) → finite=false, lbts=0
	// per plan §2.2 / TASK-203.12.
	if snap.LBTSUnbounded || scalarUnbounded(snap.LBTS) {
		return &rtiv1.QueryLBTSResponse{Lbts: 0, Finite: false}, nil
	}
	legacy, exact, err := encodeScalarTime(snap.LBTS)
	if err != nil {
		return nil, err
	}
	return &rtiv1.QueryLBTSResponse{Lbts: legacy, LbtsValue: exact, Finite: true}, nil
}

// Query services: IEEE 1516.1-2010 8.16 (GALT) and 8.18 (LITS).

// QueryGALT delegates the request and queue-dependent bound to time management.
func (s *timeServiceOf[T]) QueryGALT(
	_ context.Context, req *rtiv1.QueryFederateTimeRequest,
) (*rtiv1.QueryGALTResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "nil request")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	galt, defined := s.mgr.QueryGALT(
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
	)
	if bounded, ok := s.mgr.(interface {
		QueryGALTBound(core.FederationName, core.FederateHandle) (core.TimeBoundOf[T], bool)
	}); ok {
		bound, available := bounded.QueryGALTBound(core.FederationName(req.GetFederationName()), core.FederateHandle(req.GetFederateHandle()))
		galt, defined = bound.Limit, available && !bound.Unbounded
	}
	defined = defined && !scalarUnbounded(galt)
	if !defined {
		return &rtiv1.QueryGALTResponse{Galt: 0, Finite: false}, nil
	}
	legacy, exact, err := encodeScalarTime(galt)
	if err != nil {
		return nil, err
	}
	return &rtiv1.QueryGALTResponse{Galt: legacy, GaltValue: exact, Finite: true}, nil
}

// QueryLITS reports the minimum of GALT and pending incoming timestamps.
func (s *timeServiceOf[T]) QueryLITS(
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
	if bounded, ok := s.mgr.(interface {
		QueryLITSBound(core.FederationName, core.FederateHandle) (core.TimeBoundOf[T], bool)
	}); ok {
		bound, available := bounded.QueryLITSBound(core.FederationName(req.GetFederationName()), core.FederateHandle(req.GetFederateHandle()))
		lits, has = bound.Limit, available && !bound.Unbounded
	}
	has = has && !scalarUnbounded(lits)
	if !has {
		return &rtiv1.QueryLITSResponse{Lits: 0, Finite: false}, nil
	}
	legacy, exact, err := encodeScalarTime(lits)
	if err != nil {
		return nil, err
	}
	return &rtiv1.QueryLITSResponse{Lits: legacy, LitsValue: exact, Finite: true}, nil
}

// --- M22 W2: Asynchronous-delivery toggle ---

func (s *timeServiceOf[T]) EnableAsynchronousDelivery(
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

func (s *timeServiceOf[T]) DisableAsynchronousDelivery(
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
