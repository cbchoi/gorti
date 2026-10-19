package federate

import (
	"bytes"
	"context"
	"fmt"
	"slices"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"google.golang.org/protobuf/proto"
)

// TransportationChangeConfirmed is delivered through DispatchEvent, which activates
// the prepared operation before calling the application with its achieved values.
type TransportationChangeConfirmed struct {
	CallbackInfo
	ObjectHandle           uint64
	AttributeHandles       []uint64
	InteractionClassHandle uint64
	TransportationType     int32
	prepared               *rtiv1.TransportationChange
}

func (TransportationChangeConfirmed) isFederateEvent() {}

func transportationEvent(info CallbackInfo, change *rtiv1.TransportationChange) TransportationChangeConfirmed {
	return TransportationChangeConfirmed{CallbackInfo: info, ObjectHandle: change.ObjectHandle,
		AttributeHandles: slices.Clone(change.AttributeHandles), InteractionClassHandle: change.InteractionClassHandle,
		TransportationType: int32(change.TransportType), prepared: proto.Clone(change).(*rtiv1.TransportationChange)}
}

func (f *Federate) activateTransportationCallback(ctx context.Context, event Event) (Event, error) {
	confirmation, ok := event.(TransportationChangeConfirmed)
	if !ok {
		return event, nil
	}
	p := confirmation.prepared
	if err := f.validateTransportationConfirmation(confirmation); err != nil {
		return nil, err
	}
	r, err := f.conn.obj.ActivateTransportationTypeChange(f.sessionContext(ctx), &rtiv1.ActivateTransportationTypeChangeRequest{
		WireVersion: 1, FederationName: f.federationName, FederateHandle: confirmation.federate, Ticket: slices.Clone(p.Ticket),
		ExpectedFederationGeneration: p.FederationGeneration, ExpectedCallbackEpoch: p.CallbackEpoch,
	})
	if err != nil {
		return nil, wrapStatusErr(err)
	}
	if r == nil || !bytes.Equal(r.Ticket, p.Ticket) || r.FederationGeneration != p.FederationGeneration || r.CallbackEpoch != p.CallbackEpoch ||
		r.ObjectHandle != p.ObjectHandle || r.InteractionClassHandle != p.InteractionClassHandle || r.TransportType != p.TransportType {
		return nil, fmt.Errorf("federate: mismatched transportation activation")
	}
	seen := map[uint64]bool{}
	for _, attribute := range r.AttributeHandles {
		if seen[attribute] || !slices.Contains(p.AttributeHandles, attribute) {
			return nil, fmt.Errorf("federate: activation added an unprepared attribute")
		}
		seen[attribute] = true
	}
	return transportationEvent(confirmation.CallbackInfo, r), nil
}

func (f *Federate) rollbackGenericAdmission(event Event) {
	key := genericReceipt(event)
	f.genericCallbackMu.Lock()
	if entry := f.genericCallbacks[key]; entry != nil && !entry.complete {
		delete(f.genericCallbacks, key)
	}
	f.genericCallbackMu.Unlock()
}

func (f *Federate) validateTransportationConfirmation(confirmation TransportationChangeConfirmed) error {
	p := confirmation.prepared
	if p == nil || len(p.Ticket) != 32 || p.FederationGeneration != f.federationGeneration || p.CallbackEpoch != f.CallbackEpoch() || confirmation.federate != f.CurrentHandle() ||
		(p.ObjectHandle == 0) == (p.InteractionClassHandle == 0) || (p.ObjectHandle == 0 && len(p.AttributeHandles) != 0) || (p.TransportType != 1 && p.TransportType != 2) {
		return fmt.Errorf("federate: invalid or retired transportation confirmation")
	}
	return nil
}
