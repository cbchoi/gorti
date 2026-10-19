package object

import (
	"context"
	"slices"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

var _ core.QueuedMessageFilter = (*Registry)(nil)

// FilterQueuedMessage rechecks interest when a queued RO message becomes
// eligible. It uses retained class and region identities, not a live object:
// an accepted RO deletion may already have retired the instance.
func (r *RegistryOf[T]) FilterQueuedMessage(ctx context.Context, fed core.FederationName, recipient core.FederateHandle, event core.OutboundEvent) core.OutboundEvent {
	if filtered, handled := r.FilterMOMAttributePointMessage(ctx, fed, recipient, event); handled {
		return filtered
	}
	callback, ok := event.(*CallbackEventOf[T])
	if !ok || callback == nil {
		return event
	}
	if callback.Kind != CallbackReflect && callback.Kind != CallbackReceive {
		// Removal follows prior discovery, including after unsubscription.
		return event
	}
	callback = r.projectDelayedReflection(ctx, fed, recipient, callback, false)
	if callback == nil {
		return nil
	}
	if callback.Kind == CallbackReceive {
		return r.filterQueuedInteraction(ctx, fed, recipient, callback)
	}
	return r.filterQueuedReflection(ctx, fed, recipient, callback)
}

func (r *RegistryOf[T]) filterQueuedReflection(ctx context.Context, fed core.FederationName, recipient core.FederateHandle, callback *CallbackEventOf[T]) core.OutboundEvent {
	// ObjectClass is the class conveyed to this recipient, not the registered
	// class. A later ancestor subscription must not change that historical view.
	cls := callback.ObjectClass
	var regions []DDMRegionHandle
	if callback.Metadata != nil {
		for _, region := range callback.Metadata.Regions {
			regions = append(regions, DDMRegionHandle(region))
		}
	}
	attributes := make(map[uint64][]byte, len(callback.Attributes))
	for handle, value := range callback.Attributes {
		interested := r.queuedAttributeInterested(ctx, fed, recipient, callback, cls, regions, core.AttributeHandle(handle))
		if interested {
			attributes[handle] = value
		}
	}
	if len(attributes) == 0 {
		return nil
	}
	// Payload bytes and metadata remain immutable. Copy the envelope and map
	// so a failed outbox admission can retry against the unfiltered source.
	filtered := *callback
	filtered.Attributes = attributes
	if callback.Metadata != nil && callback.Metadata.AttributeConveyedRegions != nil {
		metadata := *callback.Metadata
		metadata.ConveyedRegions = make([]core.ConveyedRegionCopy, 0)
		metadata.AttributeConveyedRegions = make(map[core.AttributeHandle][]core.ConveyedRegionCopy)
		for handle := range attributes {
			attr := core.AttributeHandle(handle)
			if copies, ok := callback.Metadata.AttributeConveyedRegions[attr]; ok {
				metadata.AttributeConveyedRegions[attr] = slices.Clone(copies)
				metadata.ConveyedRegions = appendConveyedCopies(metadata.ConveyedRegions, copies)
			}
		}
		filtered.Metadata = &metadata
	}
	return &filtered
}

func (r *RegistryOf[T]) filterQueuedInteraction(ctx context.Context, fed core.FederationName, recipient core.FederateHandle, callback *CallbackEventOf[T]) core.OutboundEvent {
	model, _ := r.opts.FOMs.Get(ctx, fed)
	hierarchy, _ := model.(core.FOMClassAncestry)
	classes := []core.InteractionClassHandle{callback.InteractionClass}
	if hierarchy != nil {
		classes = append(classes, hierarchy.InteractionClassAncestors(callback.InteractionClass)...)
	}
	for _, cls := range classes {
		var subs []core.FederateHandle
		switch {
		case callback.Metadata != nil && callback.Metadata.ConveyedRegions != nil:
			if conveyedOverlapsDefault(callback.Metadata.ConveyedRegions) {
				subs = r.ordinaryInteractionSubscribers(ctx, fed, cls)
			}
		case callback.Metadata == nil || len(callback.Metadata.Regions) == 0:
			subs = r.interactionSubscribers(ctx, fed, cls)
		case r.interactionRegionsOverlapDefault(ctx, fed, callback.InteractionClass, callback.Metadata.Regions):
			subs = r.ordinaryInteractionSubscribers(ctx, fed, cls)
		}
		if slices.Contains(subs, recipient) {
			return r.projectDelayedInteraction(ctx, fed, callback, cls)
		}
	}
	return r.filterQueuedRegionalInteraction(ctx, fed, recipient, callback)
}

func (r *RegistryOf[T]) filterQueuedRegionalInteraction(ctx context.Context, fed core.FederationName, recipient core.FederateHandle, callback *CallbackEventOf[T]) core.OutboundEvent {
	if callback.Metadata != nil && callback.Metadata.ConveyedRegions != nil {
		if routing, ok := r.opts.RegistrationRegions.(conveyedInteractionRouting); ok {
			subs, err := routing.InteractionSubscribersForConveyedRegions(ctx, fed, callback.InteractionClass, callback.Metadata.ConveyedRegions)
			if err == nil && slices.Contains(subs, recipient) {
				return r.projectDelayedInteraction(ctx, fed, callback, callback.InteractionClass)
			}
		}
		return nil
	}
	if routing, ok := r.opts.RegistrationRegions.(core.RegionRouting); ok && callback.Metadata != nil && len(callback.Metadata.Regions) > 0 {
		subs, err := routing.InteractionSubscribersWithRegions(ctx, fed, callback.Metadata.Producer, callback.InteractionClass, callback.Metadata.Regions)
		if err == nil && slices.Contains(subs, recipient) {
			return callback
		}
	}
	return nil
}

func (r *RegistryOf[T]) queuedAttributeInterested(ctx context.Context, fed core.FederationName, recipient core.FederateHandle, callback *CallbackEventOf[T], cls core.ObjectClassHandle, regions []DDMRegionHandle, attr core.AttributeHandle) bool {
	var copies []core.ConveyedRegionCopy
	copied := false
	if callback.Metadata != nil && callback.Metadata.ConveyedRegions != nil {
		copies = callback.Metadata.ConveyedRegions
		copied = true
		if callback.Metadata.AttributeConveyedRegions != nil {
			copies, copied = callback.Metadata.AttributeConveyedRegions[attr]
		}
	}
	defaultOverlap := r.publisherRegionsOverlapDefault(fed, cls, attr, regions)
	if copied {
		defaultOverlap = conveyedOverlapsDefault(copies)
	}
	// In a mixed regional/default callback, attributes without a regional
	// entry were accepted using the default update region.
	if callback.Metadata != nil && callback.Metadata.AttributeConveyedRegions != nil && !copied {
		defaultOverlap = true
	}
	interested := slices.Contains(r.objectSubscribers(ctx, fed, cls, []core.AttributeHandle{attr}), recipient) && defaultOverlap
	if interested {
		return true
	}
	return r.queuedRegionalAttributeInterested(ctx, fed, recipient, cls, attr, copies, copied, regions)
}

func (r *RegistryOf[T]) queuedRegionalAttributeInterested(ctx context.Context, fed core.FederationName, recipient core.FederateHandle, cls core.ObjectClassHandle, attr core.AttributeHandle, copies []core.ConveyedRegionCopy, copied bool, regions []DDMRegionHandle) bool {
	interested := false
	if copied {
		if routing, ok := r.opts.RegistrationRegions.(conveyedAttributeRouting); ok {
			subs, err := routing.SubscribersForConveyedUpdate(ctx, fed, cls, attr, copies)
			interested = err == nil && slices.Contains(subs, recipient)
		} else if r.opts.DDM != nil {
			// Compatibility adapters can match readonly designators, never
			// mutable source handles. Production uses portable values above.
			copiedRegions := make([]DDMRegionHandle, len(copies))
			for i, regionCopy := range copies {
				copiedRegions[i] = DDMRegionHandle(regionCopy.Designator)
			}
			interested = slices.Contains(r.opts.DDM.SubscribersForUpdate(fed, cls, attr, copiedRegions), recipient)
		}
	} else if r.opts.DDM != nil {
		var subs []core.FederateHandle
		if len(regions) == 0 {
			subs = r.opts.DDM.RegionSubscribersFor(fed, cls, attr)
		} else {
			subs = r.opts.DDM.SubscribersForUpdate(fed, cls, attr, regions)
		}
		interested = slices.Contains(subs, recipient)
	}
	return interested
}
