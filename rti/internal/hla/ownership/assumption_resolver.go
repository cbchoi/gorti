package ownership

import (
	"context"
	"slices"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// NewPublisherResolver enumerates publishers, not subscribers. Knowledge and
// exact publication eligibility are validated by Manager.PrepareAcquisition.
// classOf may close over a registry initialized after the ownership manager.
func NewPublisherResolver(classOf func(core.FederationName, core.ObjectHandle) core.ObjectClassHandle, declarations core.DeclarationManagement, foms core.FOMRepository) SubscribersResolver {
	return func(ctx context.Context, fed core.FederationName, obj core.ObjectHandle, attrs []core.AttributeHandle) []core.FederateHandle {
		if classOf == nil || declarations == nil || foms == nil {
			return nil
		}
		class := classOf(fed, obj)
		if class == 0 {
			return nil
		}
		fom, err := foms.Get(ctx, fed)
		if err != nil || fom == nil {
			return nil
		}
		classes := []core.ObjectClassHandle{class}
		if ancestry, ok := fom.(core.FOMClassAncestry); ok {
			classes = append(classes, ancestry.ObjectClassAncestors(class)...)
		}
		slices.Sort(classes)
		classes = slices.Compact(classes)
		var candidates []core.FederateHandle
		for _, cls := range classes {
			for _, attr := range publicationAttributesAtClass(fom, class, cls, attrs) {
				candidates = append(candidates, declarations.PublishersFor(ctx, fed, cls, attr)...)
			}
		}
		slices.Sort(candidates)
		return slices.Compact(candidates)
	}
}

// NewPublicationAttributeResolver matches new publications against existing
// offers without assuming attribute handles are identical across classes.
func NewPublicationAttributeResolver(classOf func(core.FederationName, core.ObjectHandle) core.ObjectClassHandle, foms core.FOMRepository) func(context.Context, core.FederationName, core.ObjectHandle, core.ObjectClassHandle, []core.AttributeHandle) []core.AttributeHandle {
	return func(ctx context.Context, fed core.FederationName, obj core.ObjectHandle, cls core.ObjectClassHandle, attrs []core.AttributeHandle) []core.AttributeHandle {
		if classOf == nil || foms == nil {
			return nil
		}
		actual := classOf(fed, obj)
		if actual == 0 {
			return nil
		}
		fom, err := foms.Get(ctx, fed)
		if err != nil || fom == nil {
			return nil
		}
		return publicationAttributesAtClass(fom, cls, actual, attrs)
	}
}

func publicationAttributesAtClass(fom core.FOMHandle, from, to core.ObjectClassHandle, attrs []core.AttributeHandle) []core.AttributeHandle {
	if from == to {
		return slices.Clone(attrs)
	}
	names, ok := fom.(interface {
		AttributeName(core.ObjectClassHandle, core.AttributeHandle) (string, bool)
	})
	if !ok {
		return nil
	}
	var result []core.AttributeHandle
	for _, attr := range attrs {
		name, found := names.AttributeName(from, attr)
		if !found {
			continue
		}
		if mapped, found := fom.LookupAttribute(to, name); found {
			result = append(result, mapped)
		}
	}
	return result
}
