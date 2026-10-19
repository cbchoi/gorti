package grpc

import (
	"bytes"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
)

// Descriptor coverage uses a declared projection; live authority is verified
// separately by actual FOM/transportation and production TCP tests.
func transportationCheckpointInventoryProjection(event *rtiv1.FederateEvent) func(core.FederationName) object.TransportationCheckpointSnapshot {
	change := event.GetTransportationChangeConfirmed()
	if change == nil {
		return nil
	}
	change.Ticket = bytes.Repeat([]byte{0x39}, 32)
	change.TransportType = rtiv1.TransportationType_TRANSPORTATION_TYPE_RELIABLE
	change.FederationGeneration = 2
	change.CallbackEpoch = 3
	return func(fed core.FederationName) object.TransportationCheckpointSnapshot {
		return object.TransportationCheckpointSnapshot{string(change.Ticket): {
			Scope:   core.TransportationScope{Member: core.MemberIdentity{Federation: fed, Handle: 1, Generation: 2, Incarnation: 1}, CallbackEpoch: 3},
			Binding: object.TransportationCheckpointBinding{Interaction: core.InteractionClassHandle(change.InteractionClassHandle), Transportation: core.TransportTypeReliable},
		}}
	}
}
