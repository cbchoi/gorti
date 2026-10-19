package federate

import (
	"context"
	"fmt"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
)

func (f *Federate) ReleaseMultipleObjectInstanceNames(ctx context.Context, names []string) error {
	_, err := f.conn.obj.ReleaseMultipleObjectInstanceNames(f.sessionContext(ctx), &rtiv1.ReleaseMultipleObjectInstanceNamesRequest{WireVersion: 1, FederationName: f.federationName, FederateHandle: f.CurrentHandle(), ObjectNames: append([]string(nil), names...)})
	return wrapStatusErr(err)
}

func (f *Federate) ReserveMultipleObjectInstanceNames(ctx context.Context, names []string) error {
	_, err := f.conn.obj.ReserveMultipleObjectInstanceNames(f.sessionContext(ctx), &rtiv1.ReserveMultipleObjectInstanceNamesRequest{WireVersion: 1, FederationName: f.federationName, FederateHandle: f.CurrentHandle(), ObjectNames: append([]string(nil), names...)})
	return wrapStatusErr(err)
}

func (f *Federate) ReleaseObjectInstanceName(ctx context.Context, name string) error {
	_, err := f.conn.obj.ReleaseObjectInstanceName(f.sessionContext(ctx), &rtiv1.ReleaseObjectInstanceNameRequest{WireVersion: 1, FederationName: f.federationName, FederateHandle: f.CurrentHandle(), ObjectName: name})
	return wrapStatusErr(err)
}

// RequestFederationRestoreFromGeneration explicitly selects the saved execution;
// even generation zero remains present on the wire, never interpreted as omit.
func (f *Federate) RequestFederationRestoreFromGeneration(ctx context.Context, label string, generation uint64) error {
	_, err := f.conn.savepoint.RequestFederationRestore(f.sessionContext(ctx), &rtiv1.RequestFederationRestoreRequest{WireVersion: 1, FederationName: f.federationName, FederateHandle: f.CurrentHandle(), Label: label, SavedFederationGeneration: &generation})
	return wrapStatusErr(err)
}

// UnpublishObjectClass removes every publication of the named class.
func (f *Federate) UnpublishObjectClass(ctx context.Context, class string) error {
	c, _, err := f.resolveObjectAttributes(class, nil)
	if err != nil {
		return err
	}
	return f.UnpublishObjectClassByHandle(ctx, c)
}

func (f *Federate) UnpublishObjectClassByHandle(ctx context.Context, class uint64) error {
	return f.unpublishObjectClass(ctx, class, nil, nil)
}

// UnpublishObjectClassAttributeSet removes only the supplied publications.
// Both nil and empty sets are explicit no-ops, not whole-class removal.
func (f *Federate) UnpublishObjectClassAttributeSet(ctx context.Context, class string, attrs []string) error {
	c, a, err := f.resolveObjectAttributes(class, attrs)
	if err != nil {
		return err
	}
	return f.UnpublishObjectClassAttributeSetByHandle(ctx, c, a)
}

func (f *Federate) UnpublishObjectClassAttributeSetByHandle(ctx context.Context, class uint64, attrs []uint64) error {
	present := true
	return f.unpublishObjectClass(ctx, class, attrs, &present)
}

// UnpublishObjectClassAttributes retains the legacy empty-means-whole behavior.
// Use UnpublishObjectClassAttributeSet to represent an explicit empty set.
func (f *Federate) UnpublishObjectClassAttributes(ctx context.Context, class string, attrs []string) error {
	c, a, err := f.resolveObjectAttributes(class, attrs)
	if err != nil {
		return err
	}
	return f.UnpublishObjectClassAttributesByHandle(ctx, c, a)
}
func (f *Federate) UnpublishObjectClassAttributesByHandle(ctx context.Context, class uint64, attrs []uint64) error {
	return f.unpublishObjectClass(ctx, class, attrs, nil)
}

func (f *Federate) unpublishObjectClass(ctx context.Context, class uint64, attrs []uint64, present *bool) error {
	if err := f.FlushOwnershipCallbackReceipts(ctx); err != nil {
		return err
	}
	_, err := f.conn.decl.UnpublishObjectClassAttributes(f.sessionContext(ctx), &rtiv1.UnpubObjAttrsRequest{WireVersion: 1, FederationName: f.federationName, FederateHandle: f.CurrentHandle(), ObjectClassHandle: class, AttributeHandles: append([]uint64(nil), attrs...), AttributeSetPresent: present})
	if err == nil {
		f.mu.Lock()
		if present == nil && len(attrs) == 0 {
			delete(f.publishedObjectAttributes, class)
		} else {
			for _, a := range attrs {
				delete(f.publishedObjectAttributes[class], a)
			}
		}
		f.mu.Unlock()
	}
	return wrapStatusErr(err)
}

// UnsubscribeObjectClass removes every subscription of the named class.
func (f *Federate) UnsubscribeObjectClass(ctx context.Context, class string) error {
	c, _, err := f.resolveObjectAttributes(class, nil)
	if err != nil {
		return err
	}
	return f.UnsubscribeObjectClassByHandle(ctx, c)
}

func (f *Federate) UnsubscribeObjectClassByHandle(ctx context.Context, class uint64) error {
	return f.unsubscribeObjectClass(ctx, class, nil, nil)
}

// UnsubscribeObjectClassAttributeSet removes only the supplied subscriptions.
// Nil and empty sets are explicit no-ops, not whole-class removal.
func (f *Federate) UnsubscribeObjectClassAttributeSet(ctx context.Context, class string, attrs []string) error {
	c, a, err := f.resolveObjectAttributes(class, attrs)
	if err != nil {
		return err
	}
	return f.UnsubscribeObjectClassAttributeSetByHandle(ctx, c, a)
}

func (f *Federate) UnsubscribeObjectClassAttributeSetByHandle(ctx context.Context, class uint64, attrs []uint64) error {
	present := true
	return f.unsubscribeObjectClass(ctx, class, attrs, &present)
}

// UnsubscribeObjectClassAttributes retains the legacy empty-means-whole behavior.
// Use UnsubscribeObjectClassAttributeSet to represent an explicit empty set.
func (f *Federate) UnsubscribeObjectClassAttributes(ctx context.Context, class string, attrs []string) error {
	c, a, err := f.resolveObjectAttributes(class, attrs)
	if err != nil {
		return err
	}
	return f.UnsubscribeObjectClassAttributesByHandle(ctx, c, a)
}
func (f *Federate) UnsubscribeObjectClassAttributesByHandle(ctx context.Context, class uint64, attrs []uint64) error {
	return f.unsubscribeObjectClass(ctx, class, attrs, nil)
}

func (f *Federate) unsubscribeObjectClass(ctx context.Context, class uint64, attrs []uint64, present *bool) error {
	_, err := f.conn.decl.UnsubscribeObjectClassAttributes(f.sessionContext(ctx), &rtiv1.UnsubObjAttrsRequest{WireVersion: 1, FederationName: f.federationName, FederateHandle: f.CurrentHandle(), ObjectClassHandle: class, AttributeHandles: append([]uint64(nil), attrs...), AttributeSetPresent: present})
	return wrapStatusErr(err)
}
func (f *Federate) UnpublishInteractionClass(ctx context.Context, class string) error {
	c, ok := f.handles.interactionHandle(class)
	if !ok {
		return fmt.Errorf("%w: %s", ErrInteractionClassNotDefined, class)
	}
	return f.UnpublishInteractionClassByHandle(ctx, c)
}
func (f *Federate) UnpublishInteractionClassByHandle(ctx context.Context, class uint64) error {
	_, err := f.conn.decl.UnpublishInteractionClass(f.sessionContext(ctx), &rtiv1.UnpubInterRequest{WireVersion: 1, FederationName: f.federationName, FederateHandle: f.CurrentHandle(), InteractionClassHandle: class})
	if err == nil {
		f.mu.Lock()
		delete(f.publishedInteractions, class)
		f.mu.Unlock()
	}
	return wrapStatusErr(err)
}
func (f *Federate) UnsubscribeInteractionClass(ctx context.Context, class string) error {
	c, ok := f.handles.interactionHandle(class)
	if !ok {
		return fmt.Errorf("%w: %s", ErrInteractionClassNotDefined, class)
	}
	return f.UnsubscribeInteractionClassByHandle(ctx, c)
}
func (f *Federate) UnsubscribeInteractionClassByHandle(ctx context.Context, class uint64) error {
	_, err := f.conn.decl.UnsubscribeInteractionClass(f.sessionContext(ctx), &rtiv1.UnsubInterRequest{WireVersion: 1, FederationName: f.federationName, FederateHandle: f.CurrentHandle(), InteractionClassHandle: class})
	return wrapStatusErr(err)
}

func (f *Federate) RequestAttributeTransportationTypeChange(ctx context.Context, obj uint64, attrs []uint64, transport TransportType) error {
	_, err := f.conn.obj.RequestAttributeTransportationTypeChange(f.sessionContext(ctx), &rtiv1.ChangeAttributeTransportRequest{WireVersion: 1, FederationName: f.federationName, FederateHandle: f.CurrentHandle(), ObjectHandle: obj, AttributeHandles: append([]uint64(nil), attrs...), TransportType: rtiv1.TransportationType(transport)})
	return wrapStatusErr(err)
}
func (f *Federate) RequestInteractionTransportationTypeChange(ctx context.Context, class uint64, transport TransportType) error {
	_, err := f.conn.obj.RequestInteractionTransportationTypeChange(f.sessionContext(ctx), &rtiv1.ChangeInteractionTransportRequest{WireVersion: 1, FederationName: f.federationName, FederateHandle: f.CurrentHandle(), InteractionClassHandle: class, TransportType: rtiv1.TransportationType(transport)})
	return wrapStatusErr(err)
}
func (f *Federate) QueryAttributeTransportationType(ctx context.Context, obj, attr uint64) (TransportType, error) {
	r, err := f.conn.obj.QueryAttributeTransportationType(f.sessionContext(ctx), &rtiv1.QueryAttributeTransportationTypeRequest{WireVersion: 1, FederationName: f.federationName, FederateHandle: f.CurrentHandle(), ObjectHandle: obj, AttributeHandle: attr})
	if err != nil {
		return 0, wrapStatusErr(err)
	}
	// Preserve the low-octet representation used by the public transport type.
	return TransportType(wireEnumOctet(int32(r.GetTransportType()))), nil
}
func (f *Federate) QueryInteractionTransportationType(ctx context.Context, target, class uint64) (TransportType, error) {
	r, err := f.conn.obj.QueryInteractionTransportationType(f.sessionContext(ctx), &rtiv1.QueryInteractionTransportationTypeRequest{WireVersion: 1, FederationName: f.federationName, FederateHandle: f.CurrentHandle(), TargetFederateHandle: target, InteractionClassHandle: class})
	if err != nil {
		return 0, wrapStatusErr(err)
	}
	// Preserve the low-octet representation used by the public transport type.
	return TransportType(wireEnumOctet(int32(r.GetTransportType()))), nil
}

func (f *Federate) NormalizeFederateHandle(ctx context.Context, target uint64) (uint64, error) {
	r, err := rtiv1.NewSupportServiceClient(f.conn.rpcConnection()).NormalizeFederateHandle(f.sessionContext(ctx), &rtiv1.NormalizeFederateHandleRequest{WireVersion: 1, FederationName: f.federationName, FederateHandle: f.CurrentHandle(), TargetHandle: target})
	if err != nil {
		return 0, wrapStatusErr(err)
	}
	return r.GetValue(), nil
}
func (f *Federate) NormalizeServiceGroup(ctx context.Context, group uint32) (uint64, error) {
	r, err := rtiv1.NewSupportServiceClient(f.conn.rpcConnection()).NormalizeServiceGroup(f.sessionContext(ctx), &rtiv1.NormalizeServiceGroupRequest{WireVersion: 1, FederationName: f.federationName, FederateHandle: f.CurrentHandle(), ServiceGroup: group})
	if err != nil {
		return 0, wrapStatusErr(err)
	}
	return r.GetValue(), nil
}
