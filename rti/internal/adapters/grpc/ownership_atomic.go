package grpc

import (
	"context"
	"slices"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Dispatch before handler-side mapping so the manager can pin known-class
// interpretation through mutation. Minimal coordinators retain the old path.
func (s *ownershipService) executeKnownOwnership(ctx context.Context, req ownershipAttributeRequest, operation core.OwnershipOperation, tag []byte) ([]core.AttributeHandle, bool, error) {
	manager, ok := s.mgr.(core.KnownOwnershipCoordinator)
	if !ok || !manager.KnownOwnershipAvailable() {
		return nil, false, nil
	}
	attrs := attrHandles(req.GetAttributeHandles())
	var requested map[core.AttributeHandle]struct{}
	if operation == core.OwnershipDivestIfWanted {
		requested = make(map[core.AttributeHandle]struct{}, len(attrs))
		for _, attr := range attrs {
			requested[attr] = struct{}{}
		}
	}
	result, err := manager.ExecuteKnownOwnership(ctx, core.OwnershipCommand{
		Operation:  operation,
		Federation: core.FederationName(req.GetFederationName()),
		Federate:   core.FederateHandle(req.GetFederateHandle()),
		Object:     core.ObjectHandle(req.GetObjectHandle()),
		Attributes: attrs,
		Tag:        slices.Clone(tag),
	})
	if err != nil {
		switch operation {
		case core.OwnershipCancelDivest:
			return nil, true, ownershipCancellationStatus(ctx, err, false)
		case core.OwnershipCancelAcquire:
			return nil, true, ownershipCancellationStatus(ctx, err, true)
		default:
			return nil, true, errToStatus(ctx, err)
		}
	}
	if operation == core.OwnershipDivestIfWanted {
		for _, attr := range result {
			if _, ok := requested[attr]; !ok {
				return nil, true, status.Errorf(codes.Internal, "known ownership divest result contains unrequested attribute %d", attr)
			}
		}
	}
	return result, true, nil
}
