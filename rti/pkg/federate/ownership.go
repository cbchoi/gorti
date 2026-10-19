package federate

import (
	"context"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
)

func (f *Federate) ownershipClient() (rtiv1.OwnershipServiceClient, error) {
	if f == nil || f.conn == nil || f.conn.cc == nil || f.CurrentHandle() == 0 || f.localLRCClosing.Load() {
		return nil, ErrNotJoined
	}
	return rtiv1.NewOwnershipServiceClient(f.conn.rpcConnection()), nil
}

func (f *Federate) UnconditionalAttributeOwnershipDivestiture(ctx context.Context, obj uint64, attrs []uint64) error {
	c, err := f.ownershipClient()
	if err != nil {
		return err
	}
	_, err = c.UnconditionalAttributeOwnershipDivestiture(f.sessionContext(ctx), &rtiv1.UnconditionalDivestRequest{WireVersion: 1, FederationName: f.federationName, FederateHandle: f.CurrentHandle(), ObjectHandle: obj, AttributeHandles: append([]uint64(nil), attrs...)})
	if err == nil {
		f.updateOwnedAttributes(obj, attrs, false)
	}
	return wrapStatusErr(err)
}

// NegotiatedAttributeOwnershipDivestiture uses the two-phase protocol. Receipt
// of RequestDivestitureConfirmation does not transfer ownership; the caller
// must subsequently invoke ConfirmDivestiture.
func (f *Federate) NegotiatedAttributeOwnershipDivestiture(ctx context.Context, obj uint64, attrs []uint64, tag []byte) error {
	c, err := f.ownershipClient()
	if err != nil {
		return err
	}
	_, err = c.NegotiatedAttributeOwnershipDivestiture(f.sessionContext(ctx), &rtiv1.NegotiatedDivestRequest{WireVersion: 1, FederationName: f.federationName, FederateHandle: f.CurrentHandle(), ObjectHandle: obj, AttributeHandles: append([]uint64(nil), attrs...), Tag: append([]byte(nil), tag...), TwoPhase: true})
	return wrapStatusErr(err)
}

func (f *Federate) AttributeOwnershipAcquisition(ctx context.Context, obj uint64, attrs []uint64, tag []byte) error {
	return f.acquireOwnership(ctx, obj, attrs, tag, false)
}
func (f *Federate) AttributeOwnershipAcquisitionIfAvailable(ctx context.Context, obj uint64, attrs []uint64) error {
	return f.acquireOwnership(ctx, obj, attrs, nil, true)
}
func (f *Federate) acquireOwnership(ctx context.Context, obj uint64, attrs []uint64, tag []byte, available bool) error {
	c, err := f.ownershipClient()
	if err != nil {
		return err
	}
	_, err = c.AttributeOwnershipAcquisition(f.sessionContext(ctx), &rtiv1.AcquireRequest{WireVersion: 1, FederationName: f.federationName, FederateHandle: f.CurrentHandle(), ObjectHandle: obj, AttributeHandles: append([]uint64(nil), attrs...), Tag: append([]byte(nil), tag...), IfAvailable: available})
	// Success admits the request, not necessarily ownership. Only the callback
	// supplies the actual acquired subset.
	return wrapStatusErr(err)
}

func (f *Federate) CancelNegotiatedAttributeOwnershipDivestiture(ctx context.Context, obj uint64, attrs []uint64) error {
	c, err := f.ownershipClient()
	if err != nil {
		return err
	}
	_, err = c.CancelNegotiatedAttributeOwnershipDivestiture(f.sessionContext(ctx), &rtiv1.CancelDivestRequest{WireVersion: 1, FederationName: f.federationName, FederateHandle: f.CurrentHandle(), ObjectHandle: obj, AttributeHandles: append([]uint64(nil), attrs...)})
	return wrapStatusErr(err)
}

func (f *Federate) CancelAttributeOwnershipAcquisition(ctx context.Context, obj uint64, attrs []uint64) error {
	c, err := f.ownershipClient()
	if err != nil {
		return err
	}
	_, err = c.CancelAttributeOwnershipAcquisition(f.sessionContext(ctx), &rtiv1.CancelAcquireRequest{WireVersion: 1, FederationName: f.federationName, FederateHandle: f.CurrentHandle(), ObjectHandle: obj, AttributeHandles: append([]uint64(nil), attrs...)})
	return wrapStatusErr(err)
}

func (f *Federate) AttributeOwnershipDivestitureIfWanted(ctx context.Context, obj uint64, attrs []uint64) ([]uint64, error) {
	c, err := f.ownershipClient()
	if err != nil {
		return nil, err
	}
	resp, err := c.AttributeOwnershipDivestitureIfWantedWithResult(f.sessionContext(ctx), &rtiv1.DivestIfWantedRequest{WireVersion: 1, FederationName: f.federationName, FederateHandle: f.CurrentHandle(), ObjectHandle: obj, AttributeHandles: append([]uint64(nil), attrs...)})
	if err != nil {
		return nil, wrapStatusErr(err)
	}
	divested := append([]uint64(nil), resp.GetDivestedAttributeHandles()...)
	f.updateOwnedAttributes(obj, divested, false)
	return divested, nil
}

func (f *Federate) ConfirmDivestiture(ctx context.Context, obj uint64, attrs []uint64, tag []byte) error {
	c, err := f.ownershipClient()
	if err != nil {
		return err
	}
	_, err = c.ConfirmDivestiture(f.sessionContext(ctx), &rtiv1.ConfirmDivestitureRequest{WireVersion: 1, FederationName: f.federationName, FederateHandle: f.CurrentHandle(), ObjectHandle: obj, AttributeHandles: append([]uint64(nil), attrs...), Tag: append([]byte(nil), tag...)})
	if err == nil {
		f.updateOwnedAttributes(obj, attrs, false)
	}
	return wrapStatusErr(err)
}

func (f *Federate) AttributeOwnershipReleaseDenied(ctx context.Context, obj uint64, attrs []uint64) error {
	c, err := f.ownershipClient()
	if err != nil {
		return err
	}
	_, err = c.AttributeOwnershipReleaseDenied(f.sessionContext(ctx), &rtiv1.ReleaseDeniedRequest{WireVersion: 1, FederationName: f.federationName, FederateHandle: f.CurrentHandle(), ObjectHandle: obj, AttributeHandles: append([]uint64(nil), attrs...)})
	return wrapStatusErr(err)
}

func (f *Federate) QueryAttributeOwnership(ctx context.Context, obj, attr uint64) (uint64, bool, error) {
	c, err := f.ownershipClient()
	if err != nil {
		return 0, false, err
	}
	resp, err := c.QueryAttributeOwnership(f.sessionContext(ctx), &rtiv1.QueryOwnershipRequest{WireVersion: 1, FederationName: f.federationName, FederateHandle: f.CurrentHandle(), ObjectHandle: obj, AttributeHandle: attr})
	if err != nil {
		return 0, false, wrapStatusErr(err)
	}
	return resp.GetOwnerFederateHandle(), resp.GetOwned(), nil
}

func (f *Federate) IsAttributeOwnedByFederate(ctx context.Context, obj, attr uint64) (bool, error) {
	c, err := f.ownershipClient()
	if err != nil {
		return false, err
	}
	resp, err := c.IsAttributeOwnedByFederate(f.sessionContext(ctx), &rtiv1.IsOwnedRequest{WireVersion: 1, FederationName: f.federationName, FederateHandle: f.CurrentHandle(), ObjectHandle: obj, AttributeHandle: attr})
	if err != nil {
		return false, wrapStatusErr(err)
	}
	return resp.GetOwned(), nil
}

func (f *Federate) updateOwnedAttributes(obj uint64, attrs []uint64, owned bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, known := f.objectClasses[obj]; !known {
		return
	}
	if f.ownedObjectAttributes == nil {
		f.ownedObjectAttributes = make(map[uint64]map[uint64]struct{})
	}
	set := f.ownedObjectAttributes[obj]
	if set == nil && owned {
		set = make(map[uint64]struct{})
		f.ownedObjectAttributes[obj] = set
	}
	for _, attr := range attrs {
		if owned {
			set[attr] = struct{}{}
		} else {
			delete(set, attr)
		}
	}
}
