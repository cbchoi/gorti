// TASK-205½ (M21) — Publish/Subscribe declaration RPCs.

package federate

import (
	"context"
	"fmt"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
)

// PublishObjectClassAttributes declares publication of the named attributes
// on an object class. Class and attribute names are resolved from the FOM
// supplied at join time.
func (f *Federate) PublishObjectClassAttributes(
	ctx context.Context, className string, attributeNames []string,
) error {
	classHandle, attributeHandles, err := f.resolveObjectAttributes(className, attributeNames)
	if err != nil {
		return err
	}
	_, err = f.conn.decl.PublishObjectClassAttributes(ctx, &rtiv1.PubObjAttrsRequest{
		WireVersion:       rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName:    f.federationName,
		FederateHandle:    f.CurrentHandle(),
		ObjectClassHandle: classHandle,
		AttributeHandles:  append([]uint64(nil), attributeHandles...),
	})
	if err != nil {
		return wrapStatusErr(err)
	}
	f.rememberPublishedObjectAttributes(classHandle, attributeHandles)
	return nil
}

// SubscribeObjectClassAttributes declares subscription to the named
// attributes on an object class.
func (f *Federate) SubscribeObjectClassAttributes(
	ctx context.Context, className string, attributeNames []string,
) error {
	return f.SubscribeObjectClassAttributesWithOptions(ctx, className, attributeNames, SubscriptionOptions{Active: true})
}

// SubscriptionOptions carries an explicit active/passive mode and FOM rate name.
// A zero value requests a passive subscription with no named rate.
type SubscriptionOptions struct {
	Active               bool
	UpdateRateDesignator string
}

func (f *Federate) SubscribeObjectClassAttributesWithOptions(ctx context.Context, className string, attributeNames []string, options SubscriptionOptions) error {
	classHandle, attributeHandles, err := f.resolveObjectAttributes(className, attributeNames)
	if err != nil {
		return err
	}
	_, err = f.conn.decl.SubscribeObjectClassAttributes(ctx, &rtiv1.SubObjAttrsRequest{
		WireVersion:          rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName:       f.federationName,
		FederateHandle:       f.CurrentHandle(),
		ObjectClassHandle:    classHandle,
		AttributeHandles:     append([]uint64(nil), attributeHandles...),
		Active:               &options.Active,
		UpdateRateDesignator: options.UpdateRateDesignator,
	})
	return wrapStatusErr(err)
}

func (f *Federate) resolveObjectAttributes(className string, attributeNames []string) (uint64, []uint64, error) {
	classHandle, ok := f.handles.objectClassHandle(className)
	if !ok {
		return 0, nil, fmt.Errorf("federate: object class %q not in FOM", className)
	}
	attributeHandles := make([]uint64, len(attributeNames))
	for i, name := range attributeNames {
		handle, found := f.handles.attributeHandle(className, name)
		if !found {
			return 0, nil, fmt.Errorf("federate: attribute %q not in object class %q", name, className)
		}
		attributeHandles[i] = handle
	}
	return classHandle, attributeHandles, nil
}

// PublishInteractionClass declares this federate publishes the named
// interaction class. The class name is resolved to a wire handle via
// the FOM tables built at JoinFederation. Returns ErrInteractionClassNotFound
// if the class is not in the FOM.
func (f *Federate) PublishInteractionClass(ctx context.Context, className string) error {
	h, ok := f.handles.interactionHandle(className)
	if !ok {
		return fmt.Errorf("federate: interaction class %q not in FOM", className)
	}
	_, err := f.conn.decl.PublishInteractionClass(ctx, &rtiv1.PubInterRequest{
		WireVersion:            rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName:         f.federationName,
		FederateHandle:         f.CurrentHandle(),
		InteractionClassHandle: h,
	})
	if err != nil {
		return wrapStatusErr(err)
	}
	f.rememberPublishedInteraction(h)
	return nil
}

// SubscribeInteractionClass declares this federate subscribes to the
// named interaction class.
func (f *Federate) SubscribeInteractionClass(ctx context.Context, className string) error {
	return f.SubscribeInteractionClassWithOptions(ctx, className, true)
}

func (f *Federate) SubscribeInteractionClassWithOptions(ctx context.Context, className string, active bool) error {
	h, ok := f.handles.interactionHandle(className)
	if !ok {
		return fmt.Errorf("federate: interaction class %q not in FOM", className)
	}
	_, err := f.conn.decl.SubscribeInteractionClass(ctx, &rtiv1.SubInterRequest{
		WireVersion:            rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName:         f.federationName,
		FederateHandle:         f.CurrentHandle(),
		InteractionClassHandle: h,
		Active:                 &active,
	})
	return wrapStatusErr(err)
}

type AdvisorySwitchKind uint8

const (
	AdvisoryObjectClassRelevance AdvisorySwitchKind = iota + 1
	AdvisoryAttributeRelevance
	AdvisoryAttributeScope
	AdvisoryInteractionRelevance
)

func (f *Federate) SetAdvisorySwitch(ctx context.Context, kind AdvisorySwitchKind, enabled bool) error {
	if kind < AdvisoryObjectClassRelevance || kind > AdvisoryInteractionRelevance {
		return fmt.Errorf("federate: invalid advisory switch kind %d", kind)
	}
	_, err := f.conn.decl.SetAdvisorySwitch(ctx, &rtiv1.SetAdvisorySwitchRequest{WireVersion: rtiv1.WireVersion_WIRE_VERSION_V1, FederationName: f.federationName, FederateHandle: f.CurrentHandle(), SwitchKind: rtiv1.AdvisorySwitchKind(kind), Enabled: enabled})
	return wrapStatusErr(err)
}
