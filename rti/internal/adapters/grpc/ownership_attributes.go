package grpc

import (
	"context"
	"fmt"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// Mapping finishes its object reads before returning. Manager
// entrypoints acquire their own object lease; never retain one across that call.
type ownershipAttributeMapper interface {
	MapKnownAttributesToRegistered(context.Context, core.FederationName, core.FederateHandle, core.ObjectHandle, []core.AttributeHandle) ([]core.AttributeHandle, error)
}

type ownershipAttributeRequest interface {
	GetFederationName() string
	GetFederateHandle() uint64
	GetObjectHandle() uint64
	GetAttributeHandles() []uint64
}

func (s *ownershipService) mapOwnershipAttributes(ctx context.Context, req ownershipAttributeRequest) ([]core.AttributeHandle, error) {
	attrs := attrHandles(req.GetAttributeHandles())
	if mapper, ok := s.objects.(ownershipAttributeMapper); ok {
		return mapper.MapKnownAttributesToRegistered(ctx, core.FederationName(req.GetFederationName()), core.FederateHandle(req.GetFederateHandle()), core.ObjectHandle(req.GetObjectHandle()), attrs)
	}
	return attrs, nil
}

// Capture values before mutation so a committed subset remains in the original
// request's coordinates even if object knowledge changes during the operation.
func captureOwnershipResultMapping(known []uint64, registered []core.AttributeHandle) (map[core.AttributeHandle]uint64, error) {
	if len(known) != len(registered) {
		return nil, fmt.Errorf("ownership attribute mapping length mismatch: known=%d registered=%d", len(known), len(registered))
	}
	toKnown := make(map[core.AttributeHandle]uint64, len(known))
	toRegistered := make(map[uint64]core.AttributeHandle, len(known))
	for i, attr := range registered {
		if previous, ok := toKnown[attr]; ok && previous != known[i] {
			return nil, fmt.Errorf("ownership attribute mapping is ambiguous for registered attribute %d", attr)
		}
		if previous, ok := toRegistered[known[i]]; ok && previous != attr {
			return nil, fmt.Errorf("ownership attribute mapping is ambiguous for known attribute %d", known[i])
		}
		toKnown[attr], toRegistered[known[i]] = known[i], attr
	}
	return toKnown, nil
}
