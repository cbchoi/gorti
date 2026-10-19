package server

import (
	"bytes"
	"testing"

	grpcadapter "github.com/cbchoi/gorti/rti/internal/adapters/grpc"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
)

func TestOwnershipPublicationContinuesSolicitationProduction(t *testing.T) {
	for _, negotiated := range []bool{false, true} {
		name := "unconditional"
		if negotiated {
			name = "negotiated"
		}
		t.Run(name, func(t *testing.T) {
			const fed core.FederationName = "ownership-publication"
			ctx, srv, conn, _ := momCompletionReviewServer(t, fed)
			federations := rtiv1.NewFederationServiceClient(conn)
			created, err := federations.CreateFederation(ctx, &rtiv1.CreateFederationRequest{WireVersion: 1, FederationName: string(fed), FomModules: []*rtiv1.FOMModule{{Path: "ownership.xml", Xml: []byte(`<objectModel><objects><objectClass><name>HLAobjectRoot</name><objectClass><name>Vehicle</name><attribute><name>value</name></attribute><attribute><name>other</name></attribute></objectClass></objectClass></objects></objectModel>`)}}})
			if err != nil {
				t.Fatal(err)
			}
			owner := core.FederateHandle(momCompletionReviewJoin(ctx, t, conn, string(fed), "owner", "", created.FederationGeneration).FederateHandle)
			candidate := core.FederateHandle(momCompletionReviewJoin(ctx, t, conn, string(fed), "candidate", "", created.FederationGeneration).FederateHandle)
			unknown := core.FederateHandle(momCompletionReviewJoin(ctx, t, conn, string(fed), "unknown", "", created.FederationGeneration).FederateHandle)
			model, err := srv.foms.Get(ctx, fed)
			if err != nil {
				t.Fatal(err)
			}
			class, _ := model.LookupObjectClass("HLAobjectRoot.Vehicle")
			value, _ := model.LookupAttribute(class, "value")
			other, _ := model.LookupAttribute(class, "other")
			ownerQueue, ownerDone, err := srv.outbox.Subscribe(ctx, fed, owner)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := ownerDone(); err != nil {
					t.Error(err)
				}
			}()
			queue, done, err := srv.outbox.Subscribe(ctx, fed, candidate)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := done(); err != nil {
					t.Error(err)
				}
			}()
			unknownQueue, unknownDone, err := srv.outbox.Subscribe(ctx, fed, unknown)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := unknownDone(); err != nil {
					t.Error(err)
				}
			}()
			if err := srv.declMgr.PublishObjectClassAttributes(ctx, fed, owner, class, []core.AttributeHandle{value, other}); err != nil {
				t.Fatal(err)
			}
			if err := srv.declMgr.SubscribeObjectClassAttributes(ctx, fed, candidate, class, []core.AttributeHandle{value, other}); err != nil {
				t.Fatal(err)
			}
			obj, _, err := srv.objReg.Register(ctx, fed, owner, class, "")
			if err != nil {
				t.Fatal(err)
			}
			drainScheduledEvents(ownerQueue)
			drainScheduledEvents(queue)
			drainScheduledEvents(unknownQueue)
			owners := srv.ownMgr.(*ownership.Manager)
			var tag []byte
			if negotiated {
				tag = []byte{4, 0, 255}
				err = owners.NegotiatedDivestTwoPhase(ctx, fed, owner, obj, []core.AttributeHandle{value, other}, tag)
			} else {
				err = owners.UnconditionalDivest(ctx, fed, owner, obj, []core.AttributeHandle{value, other})
			}
			if err != nil {
				t.Fatal(err)
			}
			if assumptions := ownershipPublicationAssumptions(t, drainScheduledEvents(queue)); len(assumptions) != 0 {
				t.Fatal("nonpublisher solicited", assumptions)
			}
			publishFor := func(publisher core.FederateHandle, attrs ...core.AttributeHandle) {
				t.Helper()
				wire := make([]uint64, len(attrs))
				for i, attr := range attrs {
					wire[i] = uint64(attr)
				}
				_, err := rtiv1.NewDeclarationServiceClient(conn).PublishObjectClassAttributes(ctx, &rtiv1.PubObjAttrsRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: uint64(publisher), ObjectClassHandle: uint64(class), AttributeHandles: wire})
				if err != nil {
					t.Fatal(err)
				}
			}
			publishFor(unknown, value, other)
			if assumptions := ownershipPublicationAssumptions(t, drainScheduledEvents(unknownQueue)); len(assumptions) != 0 {
				t.Fatal("publisher that does not know the object was solicited", assumptions)
			}
			publish := func(attrs ...core.AttributeHandle) { publishFor(candidate, attrs...) }
			publish(value)
			assumptions := ownershipPublicationAssumptions(t, drainScheduledEvents(queue))
			if len(assumptions) != 1 || len(assumptions[0].AttributeHandles) != 1 || assumptions[0].AttributeHandles[0] != uint64(value) || !bytes.Equal(assumptions[0].Tag, tag) {
				t.Fatalf("new eligible publisher did not receive original offer: %+v", assumptions)
			}
			publish(value)
			if assumptions := ownershipPublicationAssumptions(t, drainScheduledEvents(queue)); len(assumptions) != 0 {
				t.Fatal("idempotent publication duplicated solicitation", assumptions)
			}
			if negotiated {
				if err := owners.CancelDivest(ctx, fed, owner, obj, []core.AttributeHandle{other}); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := owners.AcquireIfAvailable(ctx, fed, candidate, obj, []core.AttributeHandle{value}, nil); err != nil {
					t.Fatal(err)
				}
			}
			publish(other)
			assumptions = ownershipPublicationAssumptions(t, drainScheduledEvents(queue))
			if negotiated && len(assumptions) != 0 {
				t.Fatal("cancelled negotiated offer revived", assumptions)
			}
			if !negotiated && (len(assumptions) != 1 || len(assumptions[0].AttributeHandles) != 1 || assumptions[0].AttributeHandles[0] != uint64(other)) {
				t.Fatal("remaining unowned attribute was not independently solicited", assumptions)
			}
		})
	}
}

func ownershipPublicationAssumptions(t *testing.T, events []core.OutboundEvent) []*rtiv1.RequestAttributeOwnershipAssumption {
	t.Helper()
	var result []*rtiv1.RequestAttributeOwnershipAssumption
	for _, event := range events {
		if callback := ownershipReviewWireEvent(t, event).GetOwnershipAssumption(); callback != nil {
			result = append(result, callback)
		}
	}
	return result
}

func ownershipReviewWireEvent(t *testing.T, event core.OutboundEvent) *rtiv1.FederateEvent {
	t.Helper()
	wire, err := grpcadapter.EncodeOutboundEvent(event)
	if err != nil || wire == nil {
		t.Fatalf("encode ownership callback: event=%T wire=%v err=%v", event, wire, err)
	}
	return wire
}
