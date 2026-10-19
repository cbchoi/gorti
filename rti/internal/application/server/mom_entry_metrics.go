package server

import (
	"context"
	"fmt"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
)

func momEntryCountedData(event *rtiv1.FederateEvent) bool {
	return event != nil && (event.GetReflect() != nil || event.GetReceive() != nil)
}

func momDataCallbackEntryObserver(manager *mom.Manager, previous func(context.Context, core.FederationName, core.FederateHandle, *rtiv1.FederateEvent) error, onError ...func(error)) func(context.Context, core.FederationName, core.FederateHandle, *rtiv1.FederateEvent) error {
	return func(ctx context.Context, fed core.FederationName, h core.FederateHandle, event *rtiv1.FederateEvent) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if previous != nil {
			if err := previous(ctx, fed, h, event); err != nil {
				return err
			}
		}
		var entry mom.DataCallbackEntry
		data := momEntryCountedData(event)
		var accountingErr error
		if data {
			var metadata *rtiv1.ObjectCallbackMetadata
			if reflected := event.GetReflect(); reflected != nil {
				entry.Service, entry.Object, entry.ObjectClass = "reflectAttributeValues", core.ObjectHandle(reflected.GetObjectHandle()), core.ObjectClassHandle(reflected.GetObjectClassHandle())
				metadata = reflected.GetMetadata()
			} else {
				received := event.GetReceive()
				entry.Service, entry.InteractionClass = "receiveInteraction", core.InteractionClassHandle(received.GetInteractionClassHandle())
				metadata = received.GetMetadata()
			}
			if metadata == nil {
				accountingErr = fmt.Errorf("MOM: callback actual transportation unavailable or unsupported")
			} else {
				switch metadata.GetTransportationType() {
				case rtiv1.TransportationType_TRANSPORTATION_TYPE_RELIABLE:
					entry.Transportation = core.TransportTypeReliable
				case rtiv1.TransportationType_TRANSPORTATION_TYPE_BEST_EFFORT:
					entry.Transportation = core.TransportTypeBestEffort
				default:
					accountingErr = fmt.Errorf("MOM: callback actual transportation unavailable or unsupported")
				}
			}
			if manager == nil {
				accountingErr = fmt.Errorf("MOM: data ENTRY counter observer unavailable")
			}
			if accountingErr == nil {
				accountingErr = manager.RecordDataCallbackEntry(fed, h, entry)
			}
		}
		if accountingErr != nil {
			if manager != nil {
				manager.DataCallbackEntryUnavailable(fed, h, entry, accountingErr)
			}
			for _, notify := range onError {
				if notify != nil {
					notify(accountingErr)
				}
			}
		}
		return nil
	}
}
