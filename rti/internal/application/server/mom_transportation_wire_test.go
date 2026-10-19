package server

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/adapters/auth/session"
	"github.com/cbchoi/gorti/rti/internal/adapters/delivery"
	grpcadapter "github.com/cbchoi/gorti/rti/internal/adapters/grpc"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/pkg/encoding"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestMOMTransportationWireConfirmationRequiresTargetActivationAndReceipt(t *testing.T) {
	for _, service := range []string{momServiceRequestAttributeTransportationTypeChange, momServiceRequestInteractionTransportationTypeChange} {
		t.Run(service, func(t *testing.T) {
			queue := delivery.NewWithBatch(8, 1, 0)
			var receipts *grpcadapter.CallbackReceipts
			var invocations atomic.Int32
			r := newMOMExecutionRPC(t, service, func(options *grpcadapter.Options, admission *momServiceAdmissionDependencies) {
				var err error
				receipts, err = grpcadapter.NewCallbackReceipts(grpcadapter.CallbackReceiptOptions{
					Authenticate: admission.Authenticate,
					ValidateRequiredRecipient: func(_ context.Context, fed core.FederationName, h core.FederateHandle) error {
						_, err := admission.Federations.MemberIdentity(fed, h)
						return err
					},
					ObserveEntry: func(context.Context, core.FederationName, core.FederateHandle, *rtiv1.FederateEvent) error {
						return nil
					},
					ObserveRequired: func(context.Context, core.FederationName, core.FederateHandle, *rtiv1.FederateEvent, bool, string) error {
						invocations.Add(1)
						return nil
					},
					Observe: func(context.Context, core.FederationName, core.FederateHandle, *rtiv1.FederateEvent, bool, string) error {
						return nil
					},
				})
				if err != nil {
					t.Fatal(err)
				}
				options.Outbox, options.FOMs, options.CallbackReceipts = queue, admission.FOMs, receipts
			})
			const fed = "catalog-provider"
			queue.Bind(fed, r.f.sender)
			queue.Bind(fed, r.f.target)
			deps := r.f.executor.(*momServiceExecutor).deps
			confirmation, err := newMOMTransportationConfirmation(momTransportationConfirmationDependencies{Federations: deps.Federations, Float: deps.FloatObjects, Integer: deps.IntegerObjects, CallbackEpoch: queue.CallbackEpoch, Outbox: grpcadapter.NewMandatoryCallbackOutbox(queue, func() *grpcadapter.CallbackReceipts { return receipts })})
			if err != nil {
				t.Fatal(err)
			}
			deps.Admission, deps.TransportationConfirmed = admittedMOMServiceExecution, confirmation
			if err := configureMOMServiceExecution(r.f.dispatcher, deps); err != nil {
				t.Fatal(err)
			}
			transport, err := (encoding.HLAunicodeString{}).Encode(bestEffortTransportationName)
			if err != nil {
				t.Fatal(err)
			}
			params := map[string][]byte{"HLAtransportation": transport}
			if service == momServiceRequestAttributeTransportationTypeChange {
				params["HLAobjectInstance"] = catalogRPCHandle(uint64(r.f.command.Object))
				params["HLAattributeList"] = append(catalogRPCInt(1), catalogRPCHandle(uint64(r.f.command.Attributes[0]))...)
				if err := r.f.deps.Objects.RequestAttributeTransportationTypeChange(context.Background(), fed, r.f.target, r.f.command.Object, r.f.command.Attributes, core.TransportTypeReliable); err != nil {
					t.Fatal(err)
				}
			} else {
				params["HLAinteractionClass"] = catalogRPCHandle(uint64(r.f.command.InteractionClass))
			}
			req := r.request(t, service, r.f.target, params)
			r.publish(t, req)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			auth := metadata.AppendToOutgoingContext(ctx, session.MetadataKey, string(r.targetToken))
			stream, err := rtiv1.NewStreamServiceClient(r.conn).EventBatches(auth, &rtiv1.EventsRequest{WireVersion: 1, FederationName: fed, FederateHandle: uint64(r.f.target)})
			if err != nil {
				t.Fatal(err)
			}
			if ready, err := stream.Recv(); err != nil || !ready.Ready {
				t.Fatalf("ready=%v error=%v", ready, err)
			}
			if err := r.send(req, r.senderToken); err != nil {
				t.Fatal("actual MOM transportation service:", err)
			}
			batch, err := stream.Recv()
			if err != nil || len(batch.GetEvents()) != 1 {
				t.Fatalf("callback batch=%v error=%v", batch, err)
			}
			event := batch.Events[0]
			change := event.GetTransportationChangeConfirmed()
			if change == nil || len(change.Ticket) != 32 || len(event.CallbackReceipt) != 32 {
				t.Fatal("actual wire confirmation or mandatory receipt missing")
			}
			query := func() core.TransportType {
				t.Helper()
				var value core.TransportType
				var err error
				if change.ObjectHandle != 0 {
					value, err = r.f.deps.Objects.QueryAttributeTransportationType(ctx, fed, r.f.target, r.f.command.Object, r.f.command.Attributes[0])
				} else {
					value, err = r.f.deps.Objects.QueryInteractionTransportationType(ctx, fed, r.f.sender, r.f.target, r.f.command.InteractionClass)
				}
				if err != nil {
					t.Fatal(err)
				}
				return value
			}
			if query() != core.TransportTypeReliable {
				t.Fatal("wire delivery prematurely changed transportation")
			}
			activation := &rtiv1.ActivateTransportationTypeChangeRequest{WireVersion: 1, FederationName: fed, FederateHandle: uint64(r.f.target), Ticket: change.Ticket, ExpectedFederationGeneration: change.FederationGeneration, ExpectedCallbackEpoch: change.CallbackEpoch}
			foreign := metadata.AppendToOutgoingContext(ctx, session.MetadataKey, string(r.senderToken))
			if _, err := r.client.ActivateTransportationTypeChange(foreign, activation); status.Code(err) != codes.PermissionDenied {
				t.Fatalf("managing token impersonated target: %v", err)
			}
			if query() != core.TransportTypeReliable {
				t.Fatal("foreign activation mutated transport")
			}
			report := &rtiv1.ReportCallbackInvocationRequest{WireVersion: 1, FederationName: fed, FederateHandle: uint64(r.f.target), CallbackReceipt: event.CallbackReceipt, InvocationEntry: true, Success: true}
			if _, err := rtiv1.NewSupportServiceClient(r.conn).ReportCallbackInvocation(auth, report); err != nil {
				t.Fatal(err)
			}
			if _, err := r.client.ActivateTransportationTypeChange(auth, activation); err != nil {
				t.Fatal("actual target callback entry activation:", err)
			}
			if query() != core.TransportTypeBestEffort {
				t.Fatal("callback entry did not activate transport")
			}
			report.InvocationEntry, report.Success = false, true
			for attempt := 0; attempt < 2; attempt++ {
				if _, err := rtiv1.NewSupportServiceClient(r.conn).ReportCallbackInvocation(auth, report); err != nil {
					t.Fatal(err)
				}
			}
			if invocations.Load() != 1 {
				t.Fatal("mandatory invocation completion repeated")
			}
		})
	}
}
