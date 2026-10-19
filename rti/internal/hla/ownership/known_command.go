package ownership

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type ownershipPreparation func(context.Context, core.FederationName, core.FederateHandle, core.ObjectHandle, []core.AttributeHandle) ([]core.AttributeHandle, func(), error)

func canonicalPreparation(prepare func(context.Context, core.FederationName, core.FederateHandle, core.ObjectHandle, []core.AttributeHandle) (func(), error)) ownershipPreparation {
	return func(ctx context.Context, fed core.FederationName, caller core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) ([]core.AttributeHandle, func(), error) {
		if prepare == nil {
			return attrs, func() {}, nil
		}
		release, err := prepare(ctx, fed, caller, obj, attrs)
		if err != nil {
			return nil, nil, err
		}
		if release == nil {
			return nil, nil, errors.New("ownership: preparation returned no release")
		}
		return attrs, sync.OnceFunc(release), nil
	}
}

var _ core.KnownOwnershipCoordinator = (*Manager)(nil)

func (m *Manager) KnownOwnershipAvailable() bool {
	return m.opts.PrepareKnownAcquisition != nil && m.opts.PrepareKnownOperation != nil
}

// ExecuteKnownOwnership shares the canonical state machine, but performs the
// caller-to-registered mapping under the same object pin as the mutation.
func (m *Manager) ExecuteKnownOwnership(ctx context.Context, c core.OwnershipCommand) ([]core.AttributeHandle, error) {
	if !m.KnownOwnershipAvailable() {
		return nil, errors.New("ownership: known-coordinate preparation is unavailable")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.Attributes = slices.Clone(c.Attributes)
	c.Tag = slices.Clone(c.Tag)
	port := m.opts.PrepareKnownOperation
	if c.Operation == core.OwnershipAcquire || c.Operation == core.OwnershipAcquireIfAvailable {
		port = m.opts.PrepareKnownAcquisition
	}
	var reverse map[core.AttributeHandle]core.AttributeHandle
	prepare := knownCoordinatePreparation(port, &reverse)
	var err error
	switch c.Operation {
	case core.OwnershipUnconditionalDivest:
		err = m.unconditionalDivest(ctx, c.Federation, c.Federate, c.Object, c.Attributes, prepare)
	case core.OwnershipNegotiatedDivest, core.OwnershipNegotiatedDivestTwoPhase:
		err = m.negotiatedDivest(ctx, c.Federation, c.Federate, c.Object, c.Attributes, c.Tag, c.Operation == core.OwnershipNegotiatedDivestTwoPhase, prepare)
	case core.OwnershipConfirmDivestiture:
		err = m.confirmDivestiture(ctx, c.Federation, c.Federate, c.Object, c.Attributes, c.Tag, prepare)
	case core.OwnershipAcquire, core.OwnershipAcquireIfAvailable:
		err = m.acquire(ctx, c.Federation, c.Federate, c.Object, c.Attributes, c.Tag, c.Operation == core.OwnershipAcquireIfAvailable, prepare)
	case core.OwnershipCancelDivest:
		err = m.cancelDivest(ctx, c.Federation, c.Federate, c.Object, c.Attributes, prepare)
	case core.OwnershipCancelAcquire:
		err = m.cancelAcquire(ctx, c.Federation, c.Federate, c.Object, c.Attributes, prepare)
	case core.OwnershipDivestIfWanted:
		var result []core.AttributeHandle
		result, err = m.divestIfWanted(ctx, c.Federation, c.Federate, c.Object, c.Attributes, prepare)
		if err != nil {
			return nil, err
		}
		return knownOwnershipResult(result, reverse)
	case core.OwnershipReleaseDenied:
		err = m.releaseDenied(ctx, c.Federation, c.Federate, c.Object, c.Attributes, prepare)
	default:
		err = fmt.Errorf("ownership: unknown operation %d", c.Operation)
	}
	return nil, err
}
func knownCoordinatePreparation(port ownershipPreparation, reverse *map[core.AttributeHandle]core.AttributeHandle) ownershipPreparation {
	return func(ctx context.Context, fed core.FederationName, caller core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) ([]core.AttributeHandle, func(), error) {
		mapped, release, err := port(ctx, fed, caller, obj, slices.Clone(attrs))
		if err != nil {
			if release != nil {
				release()
			}
			return nil, nil, err
		}
		if release == nil {
			return nil, nil, errors.New("ownership: known-coordinate preparation returned no release")
		}
		release = sync.OnceFunc(release)
		invalid := func() ([]core.AttributeHandle, func(), error) {
			release()
			return nil, nil, errors.New("ownership: invalid known-coordinate mapping")
		}
		if len(mapped) != len(attrs) {
			return invalid()
		}
		*reverse = make(map[core.AttributeHandle]core.AttributeHandle, len(attrs))
		forward := make(map[core.AttributeHandle]core.AttributeHandle, len(attrs))
		for i, known := range attrs {
			registered := mapped[i]
			if previous, ok := (*reverse)[registered]; ok && previous != known {
				return invalid()
			}
			if previous, ok := forward[known]; ok && previous != registered {
				return invalid()
			}
			(*reverse)[registered], forward[known] = known, registered
		}
		return slices.Clone(mapped), release, nil
	}
}
func knownOwnershipResult(result []core.AttributeHandle, reverse map[core.AttributeHandle]core.AttributeHandle) ([]core.AttributeHandle, error) {
	known := make([]core.AttributeHandle, len(result))
	for i, attr := range result {
		var ok bool
		known[i], ok = reverse[attr]
		if !ok {
			return nil, fmt.Errorf("ownership: result attribute %d was not prepared", attr)
		}
	}
	return known, nil
}
