package server

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	stdgrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

func resignRPCServer(t *testing.T, capacities ...int) (*rtid, *stdgrpc.ClientConn, context.Context) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	capacity := 0
	if len(capacities) > 0 {
		capacity = capacities[0]
	}
	srv, err := newRTID(rtidConfig{EnableInteger64: true, GenerationEpoch: 1, SaveDir: t.TempDir(), OutboxEventCapacity: capacity, OutboxBatchSize: 1, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	listener := bufconn.Listen(1 << 20)
	done := make(chan error, 1)
	go func() { done <- srv.grpcS.Serve(listener) }()
	t.Cleanup(func() { srv.sessions.Close(); srv.grpcS.Stop(); _ = listener.Close(); <-done; _ = srv.plugins.Close() })
	conn, err := stdgrpc.NewClient("passthrough:///resign-deletion", stdgrpc.WithTransportCredentials(insecure.NewCredentials()), stdgrpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return srv, conn, ctx
}

func TestResignLastMemberAllDirectivesProductionRPC(t *testing.T) {
	for _, implementation := range []string{float64TimeName, integer64TimeName} {
		t.Run(implementation, func(t *testing.T) { testResignLastMemberAllDirectives(t, implementation) })
	}
}

func testResignLastMemberAllDirectives(t *testing.T, implementation string) {
	for _, action := range []rtiv1.ResignAction{
		rtiv1.ResignAction_RESIGN_ACTION_UNCONDITIONALLY_DIVEST_ATTRIBUTES,
		rtiv1.ResignAction_RESIGN_ACTION_DELETE_OBJECTS,
		rtiv1.ResignAction_RESIGN_ACTION_CANCEL_PENDING_OWNERSHIP,
		rtiv1.ResignAction_RESIGN_ACTION_DELETE_THEN_DIVEST,
		rtiv1.ResignAction_RESIGN_ACTION_CANCEL_THEN_DELETE,
		rtiv1.ResignAction_RESIGN_ACTION_NO_ACTION,
	} {
		t.Run(action.String(), func(t *testing.T) {
			srv, conn, ctx := resignRPCServer(t)
			fed := rtiv1.NewFederationServiceClient(conn)
			_, err := fed.CreateFederation(ctx, &rtiv1.CreateFederationRequest{WireVersion: 1, FederationName: "last", LogicalTimeImplementationName: implementation, FomModules: []*rtiv1.FOMModule{{Path: "last.xml", Xml: []byte(`<objectModel><objects><objectClass><name>HLAobjectRoot</name><objectClass><name>V</name><attribute><name>x</name></attribute></objectClass></objectClass></objects></objectModel>`)}}})
			if err != nil {
				t.Fatal(err)
			}
			generation, _ := srv.fedMgr.GenerationFor("last")
			joined, err := fed.JoinFederation(ctx, &rtiv1.JoinFederationRequest{WireVersion: 1, FederationName: "last", FederateName: "owner", ExpectedFederationGeneration: &generation})
			if err != nil {
				t.Fatal(err)
			}
			auth := metadata.AppendToOutgoingContext(ctx, "gorti-session-token-bin", string(joined.SessionToken))
			fom, _ := srv.foms.Get(ctx, "last")
			class, _ := fom.LookupObjectClass("HLAobjectRoot.V")
			attr, _ := fom.LookupAttribute(class, "x")
			decl := rtiv1.NewDeclarationServiceClient(conn)
			_, err = decl.PublishObjectClassAttributes(auth, &rtiv1.PubObjAttrsRequest{WireVersion: 1, FederationName: "last", FederateHandle: joined.FederateHandle, ObjectClassHandle: uint64(class), AttributeHandles: []uint64{uint64(attr)}})
			if err != nil {
				t.Fatal(err)
			}
			objects := rtiv1.NewObjectServiceClient(conn)
			_, err = objects.ReserveObjectInstanceName(auth, &rtiv1.ReserveObjectInstanceNameRequest{WireVersion: 1, FederationName: "last", FederateHandle: joined.FederateHandle, ObjectName: "reusable"})
			if err != nil {
				t.Fatal(err)
			}
			registered, err := objects.RegisterObjectInstance(auth, &rtiv1.RegisterObjectRequest{WireVersion: 1, FederationName: "last", FederateHandle: joined.FederateHandle, ObjectClassHandle: uint64(class), ObjectName: "reusable"})
			if err != nil {
				t.Fatal(err)
			}
			_, err = fed.ResignFederation(auth, &rtiv1.ResignFederationRequest{WireVersion: 1, FederationName: "last", FederateHandle: joined.FederateHandle, Action: action})
			if err != nil {
				t.Fatalf("last-member resign: %v", err)
			}
			classOf := srv.objReg.ClassOf
			if implementation == integer64TimeName {
				classOf = srv.integerObjects.ClassOf
			}
			if classOf("last", core.ObjectHandle(registered.ObjectHandle)) != 0 {
				t.Fatal("last-member resign retained object")
			}
			if _, exists := srv.fedMgr.GenerationFor("last"); !exists {
				t.Fatal("resign destroyed federation")
			}
			rejoined, err := fed.JoinFederation(ctx, &rtiv1.JoinFederationRequest{WireVersion: 1, FederationName: "last", FederateName: "owner", ExpectedFederationGeneration: &generation})
			if err != nil {
				t.Fatal(err)
			}
			auth = metadata.AppendToOutgoingContext(ctx, "gorti-session-token-bin", string(rejoined.SessionToken))
			_, err = decl.SubscribeObjectClassAttributes(auth, &rtiv1.SubObjAttrsRequest{WireVersion: 1, FederationName: "last", FederateHandle: rejoined.FederateHandle, ObjectClassHandle: uint64(class), AttributeHandles: []uint64{uint64(attr)}})
			if err != nil {
				t.Fatal(err)
			}
			if classOf("last", core.ObjectHandle(registered.ObjectHandle)) != 0 {
				t.Fatal("rejoin rediscovered a retired object")
			}
			assertNoRetiredDiscovery(auth, t, conn, "last", rejoined.FederateHandle, registered.ObjectHandle)
			_, err = decl.PublishObjectClassAttributes(auth, &rtiv1.PubObjAttrsRequest{WireVersion: 1, FederationName: "last", FederateHandle: rejoined.FederateHandle, ObjectClassHandle: uint64(class), AttributeHandles: []uint64{uint64(attr)}})
			if err != nil {
				t.Fatal(err)
			}
			reservationCtx, cancelReservation := context.WithCancel(auth)
			t.Cleanup(cancelReservation)
			reservationStream, err := rtiv1.NewStreamServiceClient(conn).EventBatches(reservationCtx, &rtiv1.EventsRequest{WireVersion: 1, FederationName: "last", FederateHandle: rejoined.FederateHandle})
			if err != nil {
				t.Fatal(err)
			}
			if ready, err := reservationStream.Recv(); err != nil || !ready.Ready {
				t.Fatalf("reservation stream: %v %v", ready, err)
			}
			_, err = objects.ReserveObjectInstanceName(auth, &rtiv1.ReserveObjectInstanceNameRequest{WireVersion: 1, FederationName: "last", FederateHandle: rejoined.FederateHandle, ObjectName: "reusable"})
			if err != nil {
				t.Fatal(err)
			}
			reserved := false
			for !reserved {
				batch, err := reservationStream.Recv()
				if err != nil {
					t.Fatal(err)
				}
				for _, event := range batch.Events {
					if event.GetReservationFailed().GetObjectName() == "reusable" {
						t.Fatal("resign retained the old name reservation")
					}
					reserved = reserved || event.GetReservationSucceeded().GetObjectName() == "reusable"
				}
			}
			_, err = objects.RegisterObjectInstance(auth, &rtiv1.RegisterObjectRequest{WireVersion: 1, FederationName: "last", FederateHandle: rejoined.FederateHandle, ObjectClassHandle: uint64(class), ObjectName: "reusable"})
			if err != nil {
				t.Fatalf("deleted name unavailable: %v", err)
			}
		})
	}
}

func assertNoRetiredDiscovery(ctx context.Context, t *testing.T, conn *stdgrpc.ClientConn, fed string, h, retired uint64) {
	t.Helper()
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stream, err := rtiv1.NewStreamServiceClient(conn).EventBatches(streamCtx, &rtiv1.EventsRequest{WireVersion: 1, FederationName: fed, FederateHandle: h})
	if err != nil {
		t.Fatal(err)
	}
	if ready, err := stream.Recv(); err != nil || !ready.Ready {
		t.Fatalf("stream ready: %v %v", ready, err)
	}
	// An ordered control callback is a deterministic drain boundary, not a sleep.
	_, err = rtiv1.NewTimeServiceClient(conn).EnableTimeConstrained(ctx, &rtiv1.EnableConstrainedRequest{WireVersion: 1, FederationName: fed, FederateHandle: h})
	if err != nil {
		t.Fatal(err)
	}
	for {
		batch, err := stream.Recv()
		if err != nil {
			t.Fatal(err)
		}
		for _, event := range batch.Events {
			if discover := event.GetDiscover(); discover != nil && discover.ObjectHandle == retired {
				t.Fatal("rejoin received discovery of retired object")
			}
			if event.GetTimeConstrainedEnabled() != nil {
				return
			}
		}
	}
}

func TestResignConcurrentJoinProductionAtomicOutcome(t *testing.T) {
	for _, implementation := range []string{float64TimeName, integer64TimeName} {
		t.Run(implementation, func(t *testing.T) {
			srv, conn, ctx := resignRPCServer(t)
			federations := rtiv1.NewFederationServiceClient(conn)
			for trial := 0; trial < 40; trial++ {
				fed := fmt.Sprintf("atomic-%d", trial)
				_, err := federations.CreateFederation(ctx, &rtiv1.CreateFederationRequest{WireVersion: 1, FederationName: fed, LogicalTimeImplementationName: implementation, FomModules: []*rtiv1.FOMModule{{Path: "atomic.xml", Xml: []byte(`<objectModel><objects><objectClass><name>HLAobjectRoot</name><objectClass><name>V</name><attribute><name>x</name></attribute></objectClass></objectClass></objects></objectModel>`)}}})
				if err != nil {
					t.Fatal(err)
				}
				generation, _ := srv.fedMgr.GenerationFor(core.FederationName(fed))
				owner, err := federations.JoinFederation(ctx, &rtiv1.JoinFederationRequest{WireVersion: 1, FederationName: fed, FederateName: "owner", ExpectedFederationGeneration: &generation})
				if err != nil {
					t.Fatal(err)
				}
				auth := metadata.AppendToOutgoingContext(ctx, "gorti-session-token-bin", string(owner.SessionToken))
				fom, _ := srv.foms.Get(ctx, core.FederationName(fed))
				cls, _ := fom.LookupObjectClass("HLAobjectRoot.V")
				attr, _ := fom.LookupAttribute(cls, "x")
				_, err = rtiv1.NewDeclarationServiceClient(conn).PublishObjectClassAttributes(auth, &rtiv1.PubObjAttrsRequest{WireVersion: 1, FederationName: fed, FederateHandle: owner.FederateHandle, ObjectClassHandle: uint64(cls), AttributeHandles: []uint64{uint64(attr)}})
				if err != nil {
					t.Fatal(err)
				}
				registered, err := rtiv1.NewObjectServiceClient(conn).RegisterObjectInstance(auth, &rtiv1.RegisterObjectRequest{WireVersion: 1, FederationName: fed, FederateHandle: owner.FederateHandle, ObjectClassHandle: uint64(cls)})
				if err != nil {
					t.Fatal(err)
				}
				start := make(chan struct{})
				joinResult := make(chan error, 1)
				resignResult := make(chan error, 1)
				go func() {
					<-start
					_, err := federations.JoinFederation(ctx, &rtiv1.JoinFederationRequest{WireVersion: 1, FederationName: fed, FederateName: "joiner", ExpectedFederationGeneration: &generation})
					joinResult <- err
				}()
				go func() {
					<-start
					_, err := federations.ResignFederation(auth, &rtiv1.ResignFederationRequest{WireVersion: 1, FederationName: fed, FederateHandle: owner.FederateHandle, Action: rtiv1.ResignAction_RESIGN_ACTION_NO_ACTION})
					resignResult <- err
				}()
				close(start)
				if err := <-joinResult; err != nil {
					t.Fatal(err)
				}
				resignErr := <-resignResult
				classOf := srv.objReg.ClassOf
				if implementation == integer64TimeName {
					classOf = srv.integerObjects.ClassOf
				}
				alive := classOf(core.FederationName(fed), core.ObjectHandle(registered.ObjectHandle)) != 0
				members := len(srv.fedMgr.MembersOf(core.FederationName(fed)))
				switch {
				case resignErr == nil:
					if alive || members != 1 {
						t.Fatalf("resign-first partial outcome: alive=%v members=%d", alive, members)
					}
				case status.Code(resignErr) == codes.FailedPrecondition:
					if !alive || members != 2 {
						t.Fatalf("join-first partial outcome: alive=%v members=%d", alive, members)
					}
				default:
					t.Fatal(resignErr)
				}
			}
		})
	}
}
