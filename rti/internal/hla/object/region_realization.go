package object

import (
	"context"
	"slices"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type interactionSnapshotRouting interface {
	InteractionDeliverySnapshot(context.Context, core.FederationName, core.FederateHandle, core.InteractionClassHandle, []core.DDMRegionHandleCore) ([]core.FederateHandle, []core.ConveyedRegionCopy, bool, error)
}

type objectSnapshotRouting interface {
	ObjectDeliverySnapshots(context.Context, core.FederationName, core.ObjectHandle, core.ObjectClassHandle, []core.AttributeHandle, map[core.FederateHandle]core.ObjectClassHandle) (map[core.AttributeHandle]core.AttributeRegionDeliverySnapshot, error)
}

type conveyedInteractionRouting interface {
	InteractionSubscribersForConveyedRegions(context.Context, core.FederationName, core.InteractionClassHandle, []core.ConveyedRegionCopy) ([]core.FederateHandle, error)
}

type conveyedAttributeRouting interface {
	SubscribersForConveyedUpdate(context.Context, core.FederationName, core.ObjectClassHandle, core.AttributeHandle, []core.ConveyedRegionCopy) ([]core.FederateHandle, error)
}

func (r *RegistryOf[T]) snapshotReflectRecipients(ctx context.Context, fed core.FederationName, inst *objectInstance, attrs []core.AttributeHandle, classes map[core.FederateHandle]core.ObjectClassHandle) ([]core.FederateHandle, map[core.AttributeHandle][]core.FederateHandle, map[core.AttributeHandle]core.AttributeRegionDeliverySnapshot, error) {
	port, ok := r.opts.RegistrationRegions.(objectSnapshotRouting)
	if !ok {
		subs, perAttr := r.subscribersForReflectClasses(ctx, fed, inst, attrs, classes)
		return subs, perAttr, nil, nil
	}
	snapshots, err := port.ObjectDeliverySnapshots(ctx, fed, inst.handle, inst.cls, attrs, classes)
	if err != nil {
		return nil, nil, nil, err
	}
	perAttr := make(map[core.AttributeHandle][]core.FederateHandle, len(attrs))
	var recipients []core.FederateHandle
	for recipient, known := range classes {
		matched := false
		for _, actual := range attrs {
			mapped, defined := r.attributeAtClass(ctx, fed, inst.cls, known, actual)
			if !defined {
				continue
			}
			snapshot := snapshots[actual]
			ordinary := snapshot.OverlapsDefault && slices.Contains(r.objectSubscribers(ctx, fed, known, []core.AttributeHandle{mapped}), recipient)
			if ordinary || slices.Contains(snapshot.Subscribers, recipient) {
				perAttr[actual] = append(perAttr[actual], recipient)
				matched = true
			}
		}
		if matched {
			recipients = append(recipients, recipient)
		}
	}
	for _, subscribers := range perAttr {
		slices.Sort(subscribers)
	}
	slices.Sort(recipients)
	return recipients, perAttr, snapshots, nil
}

func appendConveyedCopies(copies, extra []core.ConveyedRegionCopy) []core.ConveyedRegionCopy {
	for _, regionCopy := range extra {
		if !slices.ContainsFunc(copies, func(value core.ConveyedRegionCopy) bool { return value.Designator == regionCopy.Designator }) {
			copies = append(copies, regionCopy)
		}
	}
	return copies
}

func conveyedOverlapsDefault(copies []core.ConveyedRegionCopy) bool {
	for _, regionCopy := range copies {
		dimensions := regionCopy.Realization.DimensionRanges()
		if len(dimensions) == 0 {
			continue
		}
		nonempty := true
		for _, dimension := range dimensions {
			if dimension.Bounds.Lower >= dimension.Bounds.Upper {
				nonempty = false
				break
			}
		}
		if nonempty {
			return true
		}
	}
	return false
}
