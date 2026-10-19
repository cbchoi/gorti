package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"testing"
	"time"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/adapters/sdkstate"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
	"github.com/cbchoi/gorti/rti/pkg/encoding"
	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// Authenticated raw TCP clients explicitly author known finished outcomes.
// This exercises source SAVE and fresh-server RESTORE, not SDK handler execution.
func TestTransportationCheckpointFreshTCPBothFamilies(t *testing.T) {
	for _, factory := range []string{float64TimeName, integer64TimeName} {
		for _, service := range []string{momServiceRequestAttributeTransportationTypeChange, momServiceRequestInteractionTransportationTypeChange} {
			for _, phase := range []string{testPendingLabel, "activated-unentered", testOutcomeLabel, "outcome-evicted"} {
				t.Run(factory+"/"+service+"/"+phase, func(t *testing.T) {
					ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
					defer cancel()
					dir := t.TempDir()
					const fed = "transportation-checkpoint-tcp"
					knownOutcome := phase == testOutcomeLabel || phase == "outcome-evicted"
					module := momCompletionReviewModule(t, "Item")
					module.Xml = bytes.ReplaceAll(module.Xml, []byte("<transportation>HLAbestEffort</transportation>"), []byte("<transportation>HLAreliable</transportation>"))
					create := func(conn *grpcgo.ClientConn) uint64 {
						t.Helper()
						response, err := rtiv1.NewFederationServiceClient(conn).CreateFederation(ctx, &rtiv1.CreateFederationRequest{WireVersion: 1, FederationName: fed, LogicalTimeImplementationName: factory, FomModules: []*rtiv1.FOMModule{module}})
						if err != nil {
							t.Fatal(err)
						}
						return response.FederationGeneration
					}
					source, conn, stop := integerProductionServer(t, dir, 571)
					generation := create(conn)
					manager := productionSyncJoin(ctx, t, conn, fed, "manager", generation, true)
					target := productionSyncJoin(ctx, t, conn, fed, "target", generation, true)
					model, err := source.foms.Get(ctx, fed)
					if err != nil {
						t.Fatal(err)
					}
					serviceClass, ok := model.LookupInteractionClass(mom.ServiceClassPrefix + "HLA" + service)
					if !ok {
						t.Fatal("MIM service absent")
					}
					decl, objects := rtiv1.NewDeclarationServiceClient(conn), rtiv1.NewObjectServiceClient(conn)
					if _, err := decl.PublishInteractionClass(manager.ctx, &rtiv1.PubInterRequest{WireVersion: 1, FederationName: fed, FederateHandle: manager.h, InteractionClassHandle: uint64(serviceClass)}); err != nil {
						t.Fatal(err)
					}
					params := map[uint64][]byte{}
					put := func(name string, value []byte) {
						h, ok := model.LookupParameter(serviceClass, name)
						if !ok {
							t.Fatal(name)
						}
						params[uint64(h)] = value
					}
					put("HLAfederate", catalogRPCHandle(target.h))
					var obj, attr, inter uint64
					if service == momServiceRequestAttributeTransportationTypeChange {
						class, ok := model.LookupObjectClass("Item")
						if !ok {
							t.Fatal("class absent")
						}
						attribute, ok := model.LookupAttribute(class, "Inherited")
						if !ok {
							t.Fatal("attribute absent")
						}
						attr = uint64(attribute)
						if _, err := decl.PublishObjectClassAttributes(target.ctx, &rtiv1.PubObjAttrsRequest{WireVersion: 1, FederationName: fed, FederateHandle: target.h, ObjectClassHandle: uint64(class), AttributeHandles: []uint64{attr}}); err != nil {
							t.Fatal(err)
						}
						registered, err := objects.RegisterObjectInstance(target.ctx, &rtiv1.RegisterObjectRequest{WireVersion: 1, FederationName: fed, FederateHandle: target.h, ObjectClassHandle: uint64(class)})
						if err != nil {
							t.Fatal(err)
						}
						obj = registered.ObjectHandle
						put("HLAobjectInstance", catalogRPCHandle(obj))
						put("HLAattributeList", append(catalogRPCInt(1), catalogRPCHandle(attr)...))
					} else {
						interaction, ok := model.LookupInteractionClass("Event")
						if !ok {
							t.Fatal("interaction absent")
						}
						inter = uint64(interaction)
						if _, err := decl.PublishInteractionClass(target.ctx, &rtiv1.PubInterRequest{WireVersion: 1, FederationName: fed, FederateHandle: target.h, InteractionClassHandle: inter}); err != nil {
							t.Fatal(err)
						}
						put("HLAinteractionClass", catalogRPCHandle(inter))
					}
					encoded, err := (encoding.HLAunicodeString{}).Encode(bestEffortTransportationName)
					if err != nil {
						t.Fatal(err)
					}
					put("HLAtransportation", encoded)
					if _, err := objects.SendInteraction(manager.ctx, &rtiv1.SendInteractionRequest{WireVersion: 1, FederationName: fed, FederateHandle: manager.h, InteractionClassHandle: uint64(serviceClass), Parameters: params}); err != nil {
						t.Fatal(err)
					}
					original := productionSyncReceive(t, target, func(e *rtiv1.FederateEvent) bool { return e.GetTransportationChangeConfirmed() != nil })
					old := original.GetTransportationChangeConfirmed()
					if len(old.Ticket) != 32 || len(original.CallbackReceipt) != 32 {
						t.Fatal("source authority absent")
					}
					identity := sha256.Sum256(original.CallbackReceipt)
					activate := func(client rtiv1.ObjectServiceClient, member productionSyncMember, change *rtiv1.TransportationChange) (*rtiv1.TransportationChange, error) {
						return client.ActivateTransportationTypeChange(member.ctx, &rtiv1.ActivateTransportationTypeChangeRequest{WireVersion: 1, FederationName: fed, FederateHandle: member.h, Ticket: change.Ticket, ExpectedFederationGeneration: change.FederationGeneration, ExpectedCallbackEpoch: change.CallbackEpoch})
					}
					query := func(client rtiv1.ObjectServiceClient, member productionSyncMember) rtiv1.TransportationType {
						t.Helper()
						var value *rtiv1.TransportationTypeResponse
						var err error
						if obj != 0 {
							value, err = client.QueryAttributeTransportationType(member.ctx, &rtiv1.QueryAttributeTransportationTypeRequest{WireVersion: 1, FederationName: fed, FederateHandle: member.h, ObjectHandle: obj, AttributeHandle: attr})
						} else {
							value, err = client.QueryInteractionTransportationType(member.ctx, &rtiv1.QueryInteractionTransportationTypeRequest{WireVersion: 1, FederationName: fed, FederateHandle: member.h, TargetFederateHandle: member.h, InteractionClassHandle: inter})
						}
						if err != nil {
							t.Fatal(err)
						}
						return value.TransportType
					}
					if query(objects, target) != rtiv1.TransportationType_TRANSPORTATION_TYPE_RELIABLE {
						t.Fatal("MOM delivery activated source transport")
					}
					if knownOutcome {
						productionSyncReport(t, conn, fed, target, original, true)
					}
					if phase != testPendingLabel {
						if _, err := activate(objects, target, old); err != nil {
							t.Fatal(err)
						}
						if query(objects, target) != rtiv1.TransportationType_TRANSPORTATION_TYPE_BEST_EFFORT {
							t.Fatal("source activation missing")
						}
						if phase == "activated-unentered" {
							if obj != 0 {
								_, err = objects.ChangeAttributeTransportationType(target.ctx, &rtiv1.ChangeAttributeTransportRequest{WireVersion: 1, FederationName: fed, FederateHandle: target.h, ObjectHandle: obj, AttributeHandles: []uint64{attr}, TransportType: rtiv1.TransportationType_TRANSPORTATION_TYPE_RELIABLE})
							} else {
								_, err = objects.ChangeInteractionTransportationType(target.ctx, &rtiv1.ChangeInteractionTransportRequest{WireVersion: 1, FederationName: fed, FederateHandle: target.h, InteractionClassHandle: inter, TransportType: rtiv1.TransportationType_TRANSPORTATION_TYPE_RELIABLE})
							}
							if err != nil {
								t.Fatal(err)
							}
						}
					}
					if phase == "outcome-evicted" {
						for i := 0; i < 256; i++ {
							var next *rtiv1.TransportationChange
							if obj != 0 {
								next, err = objects.PrepareAttributeTransportationTypeChange(target.ctx, &rtiv1.ChangeAttributeTransportRequest{WireVersion: 1, FederationName: fed, FederateHandle: target.h, ObjectHandle: obj, AttributeHandles: []uint64{attr}, TransportType: rtiv1.TransportationType_TRANSPORTATION_TYPE_RELIABLE})
							} else {
								next, err = objects.PrepareInteractionTransportationTypeChange(target.ctx, &rtiv1.ChangeInteractionTransportRequest{WireVersion: 1, FederationName: fed, FederateHandle: target.h, InteractionClassHandle: inter, TransportType: rtiv1.TransportationType_TRANSPORTATION_TYPE_RELIABLE})
							}
							if err != nil {
								t.Fatal(err)
							}
							if _, err := activate(objects, target, next); err != nil {
								t.Fatal(err)
							}
						}
						if _, err := activate(objects, target, old); status.Code(err) != codes.FailedPrecondition {
							t.Fatal("source activated cache did not evict", err)
						}
					}
					saves := rtiv1.NewSavepointServiceClient(conn)
					if _, err := saves.RequestFederationSave(manager.ctx, &rtiv1.RequestFederationSaveRequest{WireVersion: 1, FederationName: fed, FederateHandle: manager.h, Label: testSavedLabel}); err != nil {
						t.Fatal(err)
					}
					for _, member := range []productionSyncMember{manager, target} {
						init := productionSyncReceive(t, member, func(e *rtiv1.FederateEvent) bool { return e.GetSaveInitiate() != nil })
						productionSyncReport(t, conn, fed, member, init, true)
						productionSyncReport(t, conn, fed, member, init, false)
						epoch, err := source.outbox.CallbackEpoch(fed, core.FederateHandle(member.h))
						if err != nil {
							t.Fatal(err)
						}
						outcomes := []map[string]any{}
						if member.h == target.h && knownOutcome {
							outcomes = append(outcomes, map[string]any{"identity": hex.EncodeToString(identity[:]), "success": true, "exception": ""})
						}
						state, err := json.Marshal(map[string]any{"callback_epoch": epoch, "known_objects": []any{}, "callback_recovery": "server-v2", "pending_outcomes": outcomes})
						if err != nil {
							t.Fatal(err)
						}
						runtime, err := sdkstate.Encode(sdkstate.Envelope{Language: "go", Scalar: factory, Generation: generation, Self: member.h, State: state})
						if err != nil {
							t.Fatal(err)
						}
						response := &rtiv1.FederateSaveResponseRequest{WireVersion: 1, FederationName: fed, FederateHandle: member.h, ClientState: runtime}
						if _, err := saves.FederateSaveBegun(member.ctx, response); err != nil {
							t.Fatal(err)
						}
						if _, err := saves.FederateSaveComplete(member.ctx, response); err != nil {
							t.Fatal(err)
						}
					}
					stop()
					restored, restoredConn, _ := integerProductionServer(t, dir, 572)
					restoredGeneration := create(restoredConn)
					current := productionSyncJoin(ctx, t, restoredConn, fed, "target", restoredGeneration, true)
					currentManager := productionSyncJoin(ctx, t, restoredConn, fed, "manager", restoredGeneration, true)
					restore := rtiv1.NewSavepointServiceClient(restoredConn)
					if _, err := restore.RequestFederationRestore(currentManager.ctx, &rtiv1.RequestFederationRestoreRequest{WireVersion: 1, FederationName: fed, FederateHandle: currentManager.h, Label: testSavedLabel}); err != nil {
						t.Fatal(err)
					}
					for _, member := range []productionSyncMember{currentManager, current} {
						productionSyncReceive(t, member, func(e *rtiv1.FederateEvent) bool { return e.GetRestoreInitiate() != nil })
						if _, err := restore.FederateRestoreComplete(member.ctx, &rtiv1.FederateRestoreResponseRequest{WireVersion: 1, FederationName: fed, FederateHandle: member.h}); err != nil {
							t.Fatal(err)
						}
					}
					for {
						event, err := current.receive()
						if err != nil {
							t.Fatal(err)
						}
						if event.CallbackRecovery != nil {
							t.Fatal("recovery overtook terminal")
						}
						if terminal := event.GetRestoreCompleted(); terminal != nil {
							current.h = terminal.GetFederateHandle()
							break
						}
						if event.GetRestoreFailed() != nil {
							t.Fatal("restore failed", event)
						}
					}
					var replayed *rtiv1.FederateEvent
					for replayed == nil {
						event, err := current.receive()
						if err != nil {
							t.Fatal(err)
						}
						if bytes.Equal(event.GetCallbackRecovery().GetInvocationIdentity(), identity[:]) {
							replayed = event
						}
					}
					change := replayed.GetTransportationChangeConfirmed()
					if change == nil || change.ObjectHandle != old.ObjectHandle || change.InteractionClassHandle != old.InteractionClassHandle || !slices.Equal(change.AttributeHandles, old.AttributeHandles) || change.TransportType != old.TransportType || !bytes.Equal(replayed.CallbackRecovery.InvocationIdentity, identity[:]) || len(replayed.CallbackReceipt) != 32 || bytes.Equal(replayed.CallbackReceipt, original.CallbackReceipt) {
						t.Fatal("body or general authority changed incorrectly", replayed)
					}
					restoredObjects := rtiv1.NewObjectServiceClient(restoredConn)
					wantBefore := rtiv1.TransportationType_TRANSPORTATION_TYPE_RELIABLE
					if phase == testOutcomeLabel {
						wantBefore = rtiv1.TransportationType_TRANSPORTATION_TYPE_BEST_EFFORT
					}
					if query(restoredObjects, current) != wantBefore {
						t.Fatal("restore publication applied pending intent")
					}
					if knownOutcome {
						if !replayed.CallbackRecovery.OutcomeOnly || len(change.Ticket) != 0 || change.FederationGeneration != 0 || change.CallbackEpoch != 0 {
							t.Fatal("outcome retained activation authority")
						}
					} else {
						epoch, err := restored.outbox.CallbackEpoch(fed, core.FederateHandle(current.h))
						if err != nil {
							t.Fatal(err)
						}
						if replayed.CallbackRecovery.OutcomeOnly || len(change.Ticket) != 32 || bytes.Equal(change.Ticket, old.Ticket) || change.FederationGeneration != restoredGeneration || change.FederationGeneration == old.FederationGeneration || change.CallbackEpoch != epoch {
							t.Fatal("not fresh planned activation scope", change)
						}
						productionSyncReport(t, restoredConn, fed, current, replayed, true)
						activated, err := activate(restoredObjects, current, change)
						if err != nil {
							t.Fatal(err)
						}
						if activated.ObjectHandle != change.ObjectHandle || activated.InteractionClassHandle != change.InteractionClassHandle {
							t.Fatal("activation changed target")
						}
						want := rtiv1.TransportationType_TRANSPORTATION_TYPE_BEST_EFFORT
						if phase == "activated-unentered" {
							want = rtiv1.TransportationType_TRANSPORTATION_TYPE_RELIABLE
						}
						if query(restoredObjects, current) != want {
							t.Fatal("fresh activation missing or repeated source effect")
						}
					}
					productionSyncReport(t, restoredConn, fed, current, replayed, false)
					productionSyncReport(t, restoredConn, fed, current, replayed, false)
					if _, err := activate(restoredObjects, current, old); status.Code(err) != codes.FailedPrecondition {
						t.Fatal("old scoped ticket accepted", err)
					}
					if !knownOutcome {
						stale := proto.Clone(change).(*rtiv1.TransportationChange)
						stale.Ticket = old.Ticket
						if _, err := activate(restoredObjects, current, stale); status.Code(err) != codes.FailedPrecondition {
							t.Fatal("old ticket accepted in fresh scope", err)
						}
					}
				})
			}
		}
	}
}
