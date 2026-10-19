package server

import (
	"context"

	grpcadapter "github.com/cbchoi/gorti/rti/internal/adapters/grpc"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/federation"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
)

// Only the production observer composition opts in: untimed, nonregional data
// has no sync/checkpoint/DDM transition beyond MOM accounting. Other callbacks
// and custom observer compositions continue through the single-report path.
func newDataCallbackReportBatch(manager *mom.Manager, members *federation.Manager, save saveObservationAdmission, authenticate func(context.Context, core.FederationName, core.FederateHandle) error) *grpcadapter.DataCallbackReportBatch {
	if manager == nil {
		return nil
	}
	return &grpcadapter.DataCallbackReportBatch{
		Acquire: func(ctx context.Context, fed core.FederationName, h core.FederateHandle) (func(), error) {
			checkpointRelease := func() {}
			if save != nil {
				checkpointRelease = save.AcquireObservation(fed)
			}
			memberRelease, err := members.AcquireMember(fed, h)
			if err != nil {
				checkpointRelease()
				return nil, err
			}
			release := func() { memberRelease(); checkpointRelease() }
			if err = ctx.Err(); err == nil {
				err = authenticate(ctx, fed, h)
			}
			if err != nil {
				release()
				return nil, err
			}
			return release, nil
		},
		Apply: func(fed core.FederationName, h core.FederateHandle, events []*rtiv1.FederateEvent) bool {
			entries := make([]mom.DataCallbackEntry, len(events))
			reports := make([]mom.DataCallbackReport, len(events))
			for i, event := range events {
				if event == nil {
					continue
				}
				entry := &entries[i]
				var metadata *rtiv1.ObjectCallbackMetadata
				if data := event.GetReflect(); data != nil {
					entry.Service, entry.Object, entry.ObjectClass = "reflectAttributeValues", core.ObjectHandle(data.ObjectHandle), core.ObjectClassHandle(data.ObjectClassHandle)
					metadata = data.Metadata
				} else {
					data := event.GetReceive()
					entry.Service, entry.InteractionClass = "receiveInteraction", core.InteractionClassHandle(data.InteractionClassHandle)
					metadata = data.Metadata
				}
				entry.Transportation = core.TransportTypeReliable
				if metadata.GetTransportationType() == rtiv1.TransportationType_TRANSPORTATION_TYPE_BEST_EFFORT {
					entry.Transportation = core.TransportTypeBestEffort
				}
				reports[i].Entry = entry
			}
			// Accounting failures are retained as unavailable MOM coverage, never
			// used to reject callback admission or reopen a completed receipt.
			applied, _ := manager.TryRecordDataCallbackReports(fed, h, reports)
			return applied
		},
	}
}
