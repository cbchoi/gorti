// OwnershipService gRPC handler — translates rti.v1.OwnershipService
// RPCs into calls on rti/internal/ownership.Manager.
//
// M12 W1 exposes the cut-2 ownership.Manager through gRPC.
//
// Composition: server.go wires an *ownershipService into the composed
// Server via newOwnershipService(*ownership.Manager).

package grpc

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// ownershipService is the concrete OwnershipServiceServer impl.
//
// Phase 1 of the research-platform refactor (docs/research-platform.md
// §5.2): the handler binds to core.OwnershipCoordinator instead of the
// concrete *ownership.Manager so alternative implementations can be
// wired in at the composition root.
type ownershipService struct {
	rtiv1.UnimplementedOwnershipServiceServer
	mgr core.OwnershipCoordinator
	// Ownership consumes only optional time-free mapping/query capabilities.
	objects any
}

func newOwnershipService(mgr core.OwnershipCoordinator, objects ...any) *ownershipService {
	s := &ownershipService{mgr: mgr}
	if len(objects) > 0 {
		s.objects = objects[0]
	}
	return s
}

// UnconditionalAttributeOwnershipDivestiture implements §7.2.
func (s *ownershipService) UnconditionalAttributeOwnershipDivestiture(
	ctx context.Context,
	req *rtiv1.UnconditionalDivestRequest,
) (*rtiv1.Empty, error) {
	if req == nil {
		return nil, nilRequest("UnconditionalAttributeOwnershipDivestiture")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	if _, handled, err := s.executeKnownOwnership(ctx, req, core.OwnershipUnconditionalDivest, nil); handled {
		if err != nil {
			return nil, err
		}
		return &rtiv1.Empty{}, nil
	}
	attrs, err := s.mapOwnershipAttributes(ctx, req)
	if err != nil {
		return nil, errToStatus(ctx, err)
	}
	if err := s.mgr.UnconditionalDivest(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
		core.ObjectHandle(req.GetObjectHandle()),
		attrs,
	); err != nil {
		return nil, errToStatus(ctx, err)
	}
	return &rtiv1.Empty{}, nil
}

// twoPhaseDivester is the optional §7.6-aware entrypoint pair the
// production *ownership.Manager exposes (M37). Duck-typed so
// core.OwnershipCoordinator keeps its frozen shape.
type twoPhaseDivester interface {
	NegotiatedDivestTwoPhase(
		ctx context.Context,
		fed core.FederationName,
		owner core.FederateHandle,
		obj core.ObjectHandle,
		attrs []core.AttributeHandle,
		tag []byte,
	) error
	ConfirmDivestiture(
		ctx context.Context,
		fed core.FederationName,
		owner core.FederateHandle,
		obj core.ObjectHandle,
		attrs []core.AttributeHandle,
	) error
}

// NegotiatedAttributeOwnershipDivestiture implements §7.3.
func (s *ownershipService) NegotiatedAttributeOwnershipDivestiture(
	ctx context.Context,
	req *rtiv1.NegotiatedDivestRequest,
) (*rtiv1.Empty, error) {
	if req == nil {
		return nil, nilRequest("NegotiatedAttributeOwnershipDivestiture")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	operation := core.OwnershipNegotiatedDivest
	if req.GetTwoPhase() {
		operation = core.OwnershipNegotiatedDivestTwoPhase
	}
	if _, handled, err := s.executeKnownOwnership(ctx, req, operation, req.GetTag()); handled {
		if err != nil {
			return nil, err
		}
		return &rtiv1.Empty{}, nil
	}
	attrs, err := s.mapOwnershipAttributes(ctx, req)
	if err != nil {
		return nil, errToStatus(ctx, err)
	}
	if tp, ok := s.mgr.(twoPhaseDivester); ok && req.GetTwoPhase() {
		err = tp.NegotiatedDivestTwoPhase(
			ctx,
			core.FederationName(req.GetFederationName()),
			core.FederateHandle(req.GetFederateHandle()),
			core.ObjectHandle(req.GetObjectHandle()),
			attrs,
			req.GetTag(),
		)
	} else {
		err = s.mgr.NegotiatedDivest(
			ctx,
			core.FederationName(req.GetFederationName()),
			core.FederateHandle(req.GetFederateHandle()),
			core.ObjectHandle(req.GetObjectHandle()),
			attrs,
			req.GetTag(),
		)
	}
	if err != nil {
		return nil, errToStatus(ctx, err)
	}
	return &rtiv1.Empty{}, nil
}

// ConfirmDivestiture implements §7.6 (M37).
func (s *ownershipService) ConfirmDivestiture(
	ctx context.Context,
	req *rtiv1.ConfirmDivestitureRequest,
) (*rtiv1.Empty, error) {
	if req == nil {
		return nil, nilRequest("ConfirmDivestiture")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	if _, handled, err := s.executeKnownOwnership(ctx, req, core.OwnershipConfirmDivestiture, req.GetTag()); handled {
		if err != nil {
			return nil, err
		}
		return &rtiv1.Empty{}, nil
	}
	attrs, err := s.mapOwnershipAttributes(ctx, req)
	if err != nil {
		return nil, errToStatus(ctx, err)
	}
	if tagged, ok := s.mgr.(interface {
		ConfirmDivestitureWithTag(context.Context, core.FederationName, core.FederateHandle, core.ObjectHandle, []core.AttributeHandle, []byte) error
	}); ok {
		if err := tagged.ConfirmDivestitureWithTag(ctx, core.FederationName(req.GetFederationName()), core.FederateHandle(req.GetFederateHandle()), core.ObjectHandle(req.GetObjectHandle()), attrs, req.GetTag()); err != nil {
			return nil, errToStatus(ctx, err)
		}
		return &rtiv1.Empty{}, nil
	}
	if len(req.GetTag()) != 0 {
		return nil, status.Error(codes.Unimplemented, "ownership coordinator does not support confirmation tags")
	}
	tp, ok := s.mgr.(twoPhaseDivester)
	if !ok {
		return nil, status.Error(codes.Unimplemented,
			"ownership coordinator does not support ConfirmDivestiture")
	}
	if err := tp.ConfirmDivestiture(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
		core.ObjectHandle(req.GetObjectHandle()),
		attrs,
	); err != nil {
		return nil, errToStatus(ctx, err)
	}
	return &rtiv1.Empty{}, nil
}

// AttributeOwnershipAcquisition implements §7.4.
// ifAvailableAcquirer is the optional §7.9 entrypoint the production
// *ownership.Manager exposes (M37): grant only the
// currently-available attributes, report the rest via §7.10
// AttributeOwnershipUnavailable, queue nothing. Duck-typed so
// core.OwnershipCoordinator keeps its frozen shape.
type ifAvailableAcquirer interface {
	AcquireIfAvailable(
		ctx context.Context,
		fed core.FederationName,
		acquirer core.FederateHandle,
		obj core.ObjectHandle,
		attrs []core.AttributeHandle,
		tag []byte,
	) error
}

func (s *ownershipService) AttributeOwnershipAcquisition(
	ctx context.Context,
	req *rtiv1.AcquireRequest,
) (*rtiv1.Empty, error) {
	if req == nil {
		return nil, nilRequest("AttributeOwnershipAcquisition")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	operation := core.OwnershipAcquire
	if req.GetIfAvailable() {
		operation = core.OwnershipAcquireIfAvailable
	}
	if _, handled, err := s.executeKnownOwnership(ctx, req, operation, req.GetTag()); handled {
		if err != nil {
			return nil, err
		}
		return &rtiv1.Empty{}, nil
	}
	attrs, err := s.mapOwnershipAttributes(ctx, req)
	if err != nil {
		return nil, errToStatus(ctx, err)
	}
	if ia, ok := s.mgr.(ifAvailableAcquirer); ok && req.GetIfAvailable() {
		err = ia.AcquireIfAvailable(
			ctx,
			core.FederationName(req.GetFederationName()),
			core.FederateHandle(req.GetFederateHandle()),
			core.ObjectHandle(req.GetObjectHandle()),
			attrs,
			req.GetTag(),
		)
	} else {
		err = s.mgr.Acquire(
			ctx,
			core.FederationName(req.GetFederationName()),
			core.FederateHandle(req.GetFederateHandle()),
			core.ObjectHandle(req.GetObjectHandle()),
			attrs,
			req.GetTag(),
		)
	}
	if err != nil {
		return nil, errToStatus(ctx, err)
	}
	return &rtiv1.Empty{}, nil
}

// CancelNegotiatedAttributeOwnershipDivestiture implements §7.5.
func (s *ownershipService) CancelNegotiatedAttributeOwnershipDivestiture(
	ctx context.Context,
	req *rtiv1.CancelDivestRequest,
) (*rtiv1.Empty, error) {
	if req == nil {
		return nil, nilRequest("CancelNegotiatedAttributeOwnershipDivestiture")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	if _, handled, err := s.executeKnownOwnership(ctx, req, core.OwnershipCancelDivest, nil); handled {
		if err != nil {
			return nil, err
		}
		return &rtiv1.Empty{}, nil
	}
	attrs, err := s.mapOwnershipAttributes(ctx, req)
	if err != nil {
		return nil, errToStatus(ctx, err)
	}
	if err := s.mgr.CancelDivest(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
		core.ObjectHandle(req.GetObjectHandle()),
		attrs,
	); err != nil {
		return nil, ownershipCancellationStatus(ctx, err, false)
	}
	return &rtiv1.Empty{}, nil
}

// CancelAttributeOwnershipAcquisition implements §7.6.
func (s *ownershipService) CancelAttributeOwnershipAcquisition(
	ctx context.Context,
	req *rtiv1.CancelAcquireRequest,
) (*rtiv1.Empty, error) {
	if req == nil {
		return nil, nilRequest("CancelAttributeOwnershipAcquisition")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	if _, handled, err := s.executeKnownOwnership(ctx, req, core.OwnershipCancelAcquire, nil); handled {
		if err != nil {
			return nil, err
		}
		return &rtiv1.Empty{}, nil
	}
	attrs, err := s.mapOwnershipAttributes(ctx, req)
	if err != nil {
		return nil, errToStatus(ctx, err)
	}
	if err := s.mgr.CancelAcquire(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
		core.ObjectHandle(req.GetObjectHandle()),
		attrs,
	); err != nil {
		return nil, ownershipCancellationStatus(ctx, err, true)
	}
	return &rtiv1.Empty{}, nil
}

// AttributeOwnershipDivestitureIfWanted implements §7.7.
func (s *ownershipService) AttributeOwnershipDivestitureIfWanted(
	ctx context.Context,
	req *rtiv1.DivestIfWantedRequest,
) (*rtiv1.Empty, error) {
	if req == nil {
		return nil, nilRequest("AttributeOwnershipDivestitureIfWanted")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	if _, handled, err := s.executeKnownOwnership(ctx, req, core.OwnershipDivestIfWanted, nil); handled {
		if err != nil {
			return nil, err
		}
		return &rtiv1.Empty{}, nil
	}
	attrs, err := s.mapOwnershipAttributes(ctx, req)
	if err != nil {
		return nil, errToStatus(ctx, err)
	}
	if err := s.mgr.DivestIfWanted(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
		core.ObjectHandle(req.GetObjectHandle()),
		attrs,
	); err != nil {
		return nil, errToStatus(ctx, err)
	}
	return &rtiv1.Empty{}, nil
}

func (s *ownershipService) AttributeOwnershipDivestitureIfWantedWithResult(ctx context.Context, req *rtiv1.DivestIfWantedRequest) (*rtiv1.DivestIfWantedResponse, error) {
	if req == nil {
		return nil, nilRequest("AttributeOwnershipDivestitureIfWantedWithResult")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	if divested, handled, err := s.executeKnownOwnership(ctx, req, core.OwnershipDivestIfWanted, nil); handled {
		if err != nil {
			return nil, err
		}
		resp := &rtiv1.DivestIfWantedResponse{}
		for _, attr := range divested {
			resp.DivestedAttributeHandles = append(resp.DivestedAttributeHandles, uint64(attr))
		}
		return resp, nil
	}
	mgr, ok := s.mgr.(interface {
		DivestIfWantedWithResult(context.Context, core.FederationName, core.FederateHandle, core.ObjectHandle, []core.AttributeHandle) ([]core.AttributeHandle, error)
	})
	if !ok {
		return nil, status.Error(codes.Unimplemented, "ownership coordinator does not support atomic divest results")
	}
	attrs, err := s.mapOwnershipAttributes(ctx, req)
	if err != nil {
		return nil, errToStatus(ctx, err)
	}
	resultMapping, err := captureOwnershipResultMapping(req.GetAttributeHandles(), attrs)
	if err != nil {
		return nil, errToStatus(ctx, err)
	}
	divested, err := mgr.DivestIfWantedWithResult(ctx, core.FederationName(req.GetFederationName()), core.FederateHandle(req.GetFederateHandle()), core.ObjectHandle(req.GetObjectHandle()), attrs)
	if err != nil {
		return nil, errToStatus(ctx, err)
	}
	resp := &rtiv1.DivestIfWantedResponse{}
	for _, attr := range divested {
		known, ok := resultMapping[attr]
		if !ok {
			return nil, status.Errorf(codes.Internal, "ownership divest result contains unrequested attribute %d", attr)
		}
		resp.DivestedAttributeHandles = append(resp.DivestedAttributeHandles, known)
	}
	return resp, nil
}
