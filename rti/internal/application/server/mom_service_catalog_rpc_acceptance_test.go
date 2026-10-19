package server

import (
	"context"
	"encoding/binary"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/adapters/delivery"
	grpcadapter "github.com/cbchoi/gorti/rti/internal/adapters/grpc"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/pkg/encoding"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func momCatalogRPCArguments(t *testing.T, f *momExecutionFixture) map[string][]byte {
	t.Helper()
	c := f.command
	p := map[string][]byte{}
	handles := func() []byte {
		out := catalogRPCInt(uint32(len(c.Attributes)))
		for _, h := range c.Attributes {
			out = append(out, catalogRPCHandle(uint64(h))...)
		}
		return out
	}
	text := func(value string) []byte {
		out, err := (encoding.HLAunicodeString{}).Encode(value)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	switch c.Service {
	case momServiceResignFederationExecution:
		p["HLAresignAction"] = catalogRPCInt(6)
	case momServiceSynchronizationPointAchieved:
		p["HLAlabel"] = text(c.Label)
	case momServiceFederateSaveComplete, momServiceFederateRestoreComplete:
		p["HLAsuccessIndicator"] = catalogRPCInt(1)
	case momServicePublishObjectClassAttributes, momServiceUnpublishObjectClassAttributes, momServiceSubscribeObjectClassAttributes, momServiceUnsubscribeObjectClassAttributes:
		p["HLAobjectClass"], p["HLAattributeList"] = catalogRPCHandle(uint64(c.ObjectClass)), handles()
	case momServicePublishInteractionClass, momServiceUnpublishInteractionClass, momServiceSubscribeInteractionClass, momServiceUnsubscribeInteractionClass:
		p["HLAinteractionClass"] = catalogRPCHandle(uint64(c.InteractionClass))
	case momServiceDeleteObjectInstance:
		p["HLAobjectInstance"], p["HLAtag"] = catalogRPCHandle(uint64(c.Object)), catalogRPCInt(0)
	case momServiceLocalDeleteObjectInstance:
		p["HLAobjectInstance"] = catalogRPCHandle(uint64(c.Object))
	case momServiceRequestAttributeTransportationTypeChange:
		p["HLAobjectInstance"], p["HLAattributeList"], p["HLAtransportation"] = catalogRPCHandle(uint64(c.Object)), handles(), text(c.TransportationName)
	case momServiceRequestInteractionTransportationTypeChange:
		p["HLAinteractionClass"], p["HLAtransportation"] = catalogRPCHandle(uint64(c.InteractionClass)), text(c.TransportationName)
	case momServiceUnconditionalAttributeOwnershipDivestiture:
		p["HLAobjectInstance"], p["HLAattributeList"] = catalogRPCHandle(uint64(c.Object)), handles()
	case momServiceEnableTimeRegulation:
		p["HLAlookahead"] = catalogRPCHandle(binary.BigEndian.Uint64(c.Lookahead[:]))
	case momServiceModifyLookahead:
		p["HLAlookahead"] = catalogRPCHandle(binary.BigEndian.Uint64(c.Time[:]))
	case momServiceTimeAdvanceRequest, momServiceTimeAdvanceRequestAvailable, momServiceNextMessageRequest, momServiceNextMessageRequestAvailable, momServiceFlushQueueRequest:
		p["HLAtimeStamp"] = catalogRPCHandle(binary.BigEndian.Uint64(c.Time[:]))
	case momServiceChangeAttributeOrderType:
		p["HLAobjectInstance"], p["HLAattributeList"], p["HLAsendOrder"] = catalogRPCHandle(uint64(c.Object)), handles(), catalogRPCInt(uint32(c.Order)-1)
	case momServiceChangeInteractionOrderType:
		p["HLAinteractionClass"], p["HLAsendOrder"] = catalogRPCHandle(uint64(c.InteractionClass)), catalogRPCInt(uint32(c.Order)-1)
	case momServiceFederateSaveBegun, momServiceDisableTimeRegulation, momServiceEnableTimeConstrained, momServiceDisableTimeConstrained, momServiceEnableAsynchronousDelivery, momServiceDisableAsynchronousDelivery:
	default:
		t.Fatalf("uncovered MOM service: %s", c.Service)
	}
	return p
}

// This covers catalog decoding through a real gRPC server with seeded shared
// managers. Full application bootstrap is covered separately by production tests.
func TestMOMServiceCatalogAll32LeavesOverRPC(t *testing.T) {
	if len(momExecutionServices) != 32 {
		t.Fatal("catalog must cover all 32 standard leaves")
	}
	for _, service := range momExecutionServices {
		for _, invalid := range []bool{false, true} {
			name := service + "/valid"
			if invalid {
				name = service + "/unresolvedParameter"
			}
			t.Run(name, func(t *testing.T) {
				queue := delivery.NewWithBatch(8, 1, 0)
				r := newMOMExecutionRPC(t, service, func(opts *grpcadapter.Options, _ *momServiceAdmissionDependencies) { opts.Outbox = queue })
				const fed = "catalog-provider"
				queue.Bind(fed, r.f.target)
				deps := r.f.executor.(*momServiceExecutor).deps
				confirmation, err := newMOMTransportationConfirmation(momTransportationConfirmationDependencies{Federations: deps.Federations, Float: deps.FloatObjects, Integer: deps.IntegerObjects, CallbackEpoch: queue.CallbackEpoch, Outbox: queue})
				if err != nil {
					t.Fatal(err)
				}
				deps.TransportationConfirmed = confirmation
				if err := configureMOMServiceExecution(r.f.dispatcher, deps); err != nil {
					t.Fatal(err)
				}
				request := r.request(t, service, r.f.target, momCatalogRPCArguments(t, r.f))
				r.publish(t, request)
				beforeDecl, beforeOwners, beforeTime := r.f.deps.Declarations.ExportState(fed), r.f.deps.Ownership.ExportState(fed), r.f.time.Snapshot(fed)
				beforeMembers := r.f.deps.Federations.MembersOf(fed)
				beforeAsync := r.f.time.Metrics(fed, r.f.target).AsynchronousDelivery
				beforeClass := r.f.deps.Objects.ClassOf(fed, r.f.command.Object)
				_, _, beforeKnown := r.f.deps.Objects.LookupKnownObjectInstance(fed, r.f.target, r.f.command.Object)
				if invalid {
					request.Parameters[999999] = []byte{1}
				}
				err = r.send(request, r.senderToken)
				if invalid {
					if err == nil || status.Code(err) == codes.DeadlineExceeded {
						t.Fatalf("undefined parameter not rejected synchronously: %v", err)
					}
					if !reflect.DeepEqual(beforeDecl, r.f.deps.Declarations.ExportState(fed)) || !reflect.DeepEqual(beforeOwners, r.f.deps.Ownership.ExportState(fed)) || !reflect.DeepEqual(beforeTime, r.f.time.Snapshot(fed)) || !reflect.DeepEqual(beforeMembers, r.f.deps.Federations.MembersOf(fed)) {
						t.Fatal("invalid catalog RPC changed shared service state")
					}
					_, _, afterKnown := r.f.deps.Objects.LookupKnownObjectInstance(fed, r.f.target, r.f.command.Object)
					if beforeAsync != r.f.time.Metrics(fed, r.f.target).AsynchronousDelivery || beforeClass != r.f.deps.Objects.ClassOf(fed, r.f.command.Object) || beforeKnown != afterKnown {
						t.Fatal("invalid catalog RPC changed asynchronous delivery or object knowledge")
					}
					return
				}
				if err != nil {
					t.Fatal("standard service failed through real RPC:", err)
				}
				switch {
				case strings.Contains(service, "publish") || strings.Contains(service, "subscribe"):
					if reflect.DeepEqual(beforeDecl, r.f.deps.Declarations.ExportState(fed)) {
						t.Fatal("declaration service returned success without mutation")
					}
				case service == momServiceResignFederationExecution:
					if _, err := r.f.deps.Federations.MemberIdentity(fed, r.f.target); !errors.Is(err, core.ErrFederateNotJoined) {
						t.Fatal("resign did not retire target", err)
					}
				case service == momServiceRequestAttributeTransportationTypeChange || service == momServiceRequestInteractionTransportationTypeChange:
					stream, release, err := queue.Subscribe(context.Background(), fed, r.f.target)
					if err != nil {
						t.Fatal(err)
					}
					defer func() {
						if err := release(); err != nil {
							t.Error(err)
						}
					}()
					event := (<-stream)[0].(*momTransportationEvent).Inner()
					if event.Seq == 0 || len(event.GetTransportationChangeConfirmed().Ticket) != 32 {
						t.Fatal("transport request did not queue actual prepared confirmation")
					}
				case service == momServiceUnconditionalAttributeOwnershipDivestiture:
					if r.f.deps.Ownership.IsOwnedBy(fed, r.f.target, r.f.command.Object, r.f.command.Attributes[0]) {
						t.Fatal("divestiture retained ownership")
					}
				case service == momServiceDeleteObjectInstance:
					if r.f.deps.Objects.ClassOf(fed, r.f.command.Object) != 0 {
						t.Fatal("delete retained instance")
					}
				case service == momServiceLocalDeleteObjectInstance:
					if _, _, known := r.f.deps.Objects.LookupKnownObjectInstance(fed, r.f.target, r.f.command.Object); known {
						t.Fatal("local delete retained knowledge")
					}
				case service == momServiceEnableAsynchronousDelivery || service == momServiceDisableAsynchronousDelivery:
					if r.f.time.Metrics(fed, r.f.target).AsynchronousDelivery != (service == momServiceEnableAsynchronousDelivery) {
						t.Fatal("asynchronous delivery state did not change")
					}
				case strings.Contains(service, "Time") || strings.Contains(service, "Message") || strings.Contains(service, "Queue") || strings.Contains(service, "Asynchronous") || service == momServiceModifyLookahead:
					if reflect.DeepEqual(beforeTime, r.f.time.Snapshot(fed)) {
						t.Fatal("time service returned success without state transition")
					}
				}
			})
		}
	}
}
