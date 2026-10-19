package server

import (
	"context"
	"fmt"
	"slices"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
)

type localValidationFOMSource interface {
	FOMView(context.Context, core.FederationName) (core.FOMView, error)
}
type localValidationDeclarationSource interface {
	ExportState(core.FederationName) *declaration.ManagerState
}
type localValidationObjectSource interface {
	ExportState(core.FederationName) *object.RegistryState
}
type localValidationOwnershipSource interface {
	ExportState(core.FederationName) *ownership.ManagerState
}
type localValidationOutbox interface {
	core.ReservableOutbox
	CallbackEpoch(core.FederationName, core.FederateHandle) (uint64, error)
}

type localValidationSource struct {
	members      core.MemberSnapshotLeaser
	foms         localValidationFOMSource
	declarations localValidationDeclarationSource
	objects      localValidationObjectSource
	owners       localValidationOwnershipSource
	outbox       localValidationOutbox
	authenticate func(context.Context, core.FederationName, core.FederateHandle) error
}

func newLocalValidationSource(members core.MemberSnapshotLeaser, foms localValidationFOMSource, declarations localValidationDeclarationSource, objects localValidationObjectSource, owners localValidationOwnershipSource, outbox localValidationOutbox, authenticate func(context.Context, core.FederationName, core.FederateHandle) error) core.LocalValidationStateSource {
	return &localValidationSource{members, foms, declarations, objects, owners, outbox, authenticate}
}

func (s *localValidationSource) LocalValidationState(ctx context.Context, fed core.FederationName, h core.FederateHandle, generation, epoch, snapshotID uint64) (core.LocalValidationState, error) {
	var empty core.LocalValidationState
	if err := s.validateComposition(snapshotID); err != nil {
		return empty, err
	}
	release, err := s.members.AcquireMemberSnapshot(ctx, fed, h, generation)
	if err != nil {
		return empty, err
	}
	defer release()
	// Authentication must be inside the lease: restore may rebind a handle while
	// an earlier transport-level check waits for operation admission.
	if err := s.authenticate(ctx, fed, h); err != nil {
		return empty, err
	}
	actualEpoch, err := s.outbox.CallbackEpoch(fed, h)
	if err != nil {
		return empty, err
	}
	if actualEpoch != epoch {
		return empty, fmt.Errorf("local validation: callback epoch changed")
	}
	view, err := s.foms.FOMView(ctx, fed)
	if err != nil {
		return empty, err
	}
	state, err := buildLocalValidationState(h, view, s.declarations.ExportState(fed), s.objects.ExportState(fed), s.owners.ExportState(fed))
	if err != nil {
		return empty, err
	}
	state.FederationGeneration, state.CallbackEpoch, state.SnapshotID = generation, epoch, snapshotID
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	// Domain snapshot locks are all released before outbox admission. Every
	// relevant producer retains operation admission through callback enqueue.
	reservation, err := s.outbox.Reserve(ctx, fed, []core.OutboxDelivery{{Recipient: h, Event: core.LocalValidationBarrierEvent{SnapshotID: snapshotID, Federate: h, CallbackEpoch: epoch}}})
	if err != nil {
		return empty, err
	}
	defer reservation.Release()
	if err := reservation.Commit(); err != nil {
		return empty, err
	}
	return state, nil
}

func buildLocalValidationState(h core.FederateHandle, view core.FOMView, declarations *declaration.ManagerState, objects *object.RegistryState, owners *ownership.ManagerState) (core.LocalValidationState, error) {
	state := core.LocalValidationState{Federate: h, FOM: view}
	if view.Revision == 0 || declarations == nil || objects == nil {
		return core.LocalValidationState{}, localValidationError("missing state")
	}
	classes := make(map[core.ObjectClassHandle]map[core.AttributeHandle]bool)
	for _, c := range view.ObjectClasses {
		attrs := make(map[core.AttributeHandle]bool)
		for _, a := range c.Attributes {
			attrs[a.Handle] = true
		}
		classes[c.Handle] = attrs
	}
	interactions := make(map[core.InteractionClassHandle]bool)
	for _, c := range view.InteractionClasses {
		interactions[c.Handle] = true
	}
	if err := appendLocalPublications(&state, declarations, classes, interactions); err != nil {
		return core.LocalValidationState{}, err
	}
	instances, err := localKnownInstances(h, objects, classes)
	if err != nil {
		return core.LocalValidationState{}, err
	}
	if err := appendLocalOwnership(&state, instances, owners, classes); err != nil {
		return core.LocalValidationState{}, err
	}
	return state, nil
}

func (s *localValidationSource) validateComposition(snapshotID uint64) error {
	if snapshotID == 0 {
		return fmt.Errorf("local validation: nonzero snapshot ID required")
	}
	if s.members == nil || s.foms == nil || s.declarations == nil || s.objects == nil || s.owners == nil || s.outbox == nil || s.authenticate == nil {
		return fmt.Errorf("local validation: complete authenticated snapshot composition required")
	}
	return nil
}

func localValidationError(reason string) error {
	return fmt.Errorf("local validation: inconsistent authoritative state: %s", reason)
}

func appendLocalPublications(state *core.LocalValidationState, declarations *declaration.ManagerState, classes map[core.ObjectClassHandle]map[core.AttributeHandle]bool, interactions map[core.InteractionClassHandle]bool) error {
	h := state.Federate
	publications := make(map[core.ObjectClassHandle][]core.AttributeHandle)
	for _, p := range declarations.ObjectPublications {
		if !slices.Contains(p.Federates, h) {
			continue
		}
		if !classes[p.Class][p.Attribute] {
			return localValidationError("publication references undefined attribute")
		}
		publications[p.Class] = append(publications[p.Class], p.Attribute)
	}
	for c, attrs := range publications {
		slices.Sort(attrs)
		state.Publications = append(state.Publications, core.LocalObjectPublication{Class: c, Attributes: slices.Compact(attrs)})
	}
	slices.SortFunc(state.Publications, func(a, b core.LocalObjectPublication) int {
		if a.Class < b.Class {
			return -1
		}
		if a.Class > b.Class {
			return 1
		}
		return 0
	})
	for _, p := range declarations.InteractionPublications {
		if slices.Contains(p.Federates, h) {
			if !interactions[p.Class] {
				return localValidationError("publication references undefined interaction")
			}
			state.PublishedInteractions = append(state.PublishedInteractions, p.Class)
		}
	}
	slices.Sort(state.PublishedInteractions)
	state.PublishedInteractions = slices.Compact(state.PublishedInteractions)
	return nil
}

func localKnownInstances(h core.FederateHandle, objects *object.RegistryState, classes map[core.ObjectClassHandle]map[core.AttributeHandle]bool) (map[core.ObjectHandle]core.LocalKnownObject, error) {
	known := make(map[core.ObjectHandle]bool)
	for _, d := range objects.Discoveries {
		if d.Federate == h {
			known[d.Object] = true
		}
	}
	instances := make(map[core.ObjectHandle]core.LocalKnownObject)
	for _, obj := range objects.Objects {
		if known[obj.Handle] {
			if obj.Handle == 0 || classes[obj.Class] == nil {
				return nil, localValidationError("known object references undefined class")
			}
			instances[obj.Handle] = core.LocalKnownObject{Object: obj.Handle, Class: obj.Class}
		}
	}
	for obj := range known {
		if _, ok := instances[obj]; !ok {
			return nil, localValidationError("knowledge references missing object")
		}
	}
	return instances, nil
}

func appendLocalOwnership(state *core.LocalValidationState, instances map[core.ObjectHandle]core.LocalKnownObject, owners *ownership.ManagerState, classes map[core.ObjectClassHandle]map[core.AttributeHandle]bool) error {
	h := state.Federate
	if owners != nil {
		for _, owner := range owners.Owners {
			if owner.Owner != h {
				continue
			}
			obj, ok := instances[owner.Object]
			if !ok || !classes[obj.Class][owner.Attribute] {
				return localValidationError("ownership references unknown object or attribute")
			}
			obj.Owned = append(obj.Owned, owner.Attribute)
			instances[owner.Object] = obj
		}
	}
	for _, obj := range instances {
		slices.Sort(obj.Owned)
		obj.Owned = slices.Compact(obj.Owned)
		state.Objects = append(state.Objects, obj)
	}
	slices.SortFunc(state.Objects, func(a, b core.LocalKnownObject) int {
		if a.Object < b.Object {
			return -1
		}
		if a.Object > b.Object {
			return 1
		}
		return 0
	})
	return nil
}
