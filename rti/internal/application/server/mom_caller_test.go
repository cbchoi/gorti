package server

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/adapters/auth/session"
	grpcadapter "github.com/cbchoi/gorti/rti/internal/adapters/grpc"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/federation"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
	"google.golang.org/grpc/metadata"
)

func TestMOMAuthenticatedHandlelessServiceProduction(t *testing.T) {
	const fed core.FederationName = "caller-observation"
	ctx, srv, conn, _ := momCompletionReviewServer(t, fed)
	generation := momCompletionReviewCreate(ctx, t, conn, string(fed), "Parent")
	a := momCompletionReviewJoin(ctx, t, conn, string(fed), "caller", "", generation)
	b := momCompletionReviewJoin(ctx, t, conn, string(fed), "observer", "", generation)
	fom, err := srv.foms.Get(ctx, fed)
	if err != nil {
		t.Fatal(err)
	}
	class, ok := fom.LookupInteractionClass(mom.ClassReportServiceInvocation)
	if !ok {
		t.Fatal("missing service-report class")
	}
	if err := srv.declMgr.SubscribeInteractionClass(ctx, fed, core.FederateHandle(b.FederateHandle), class); err != nil {
		t.Fatal(err)
	}
	channel, release, err := srv.outbox.Subscribe(ctx, fed, core.FederateHandle(b.FederateHandle))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = release() })
	srv.momMgr.(*mom.Manager).SetServiceReporting(fed, core.FederateHandle(a.FederateHandle), true)
	serviceParam, _ := fom.LookupParameter(class, "HLAservice")
	callerParam, _ := fom.LookupParameter(class, "HLAfederate")
	client := rtiv1.NewSupportServiceClient(conn)
	request := &rtiv1.GetOrderTypeRequest{WireVersion: 1, OrderName: "Receive"}
	assertReports := func(want int) {
		t.Helper()
		count := 0
		for _, event := range drainScheduledEvents(channel) {
			wire, err := grpcadapter.EncodeOutboundEvent(event)
			if err != nil {
				t.Fatal(err)
			}
			received := wire.GetReceive()
			if received == nil || received.InteractionClassHandle != uint64(class) {
				continue
			}
			if name := catalogRPCString(t, received.Parameters[uint64(serviceParam)]); name != "getOrderType" {
				t.Fatalf("unexpected service %q", name)
			}
			if !bytes.Equal(received.Parameters[uint64(callerParam)], catalogRPCHandle(a.FederateHandle)) {
				t.Fatal("wrong report subject")
			}
			count++
		}
		if count != want {
			t.Fatalf("handleless service reports=%d, want %d", count, want)
		}
	}
	if _, err := client.GetOrderType(ctx, request); err != nil {
		t.Fatal(err)
	}
	assertReports(0)
	signed := metadata.AppendToOutgoingContext(ctx, "gorti-session-token-bin", string(a.SessionToken))
	if _, err := client.GetOrderType(signed, request); err != nil {
		t.Fatal(err)
	}
	assertReports(1)
	// A token that no longer belongs to a live session cannot manufacture a caller.
	srv.sessions.ClearFederate(fed, core.FederateHandle(a.FederateHandle))
	if _, err := client.GetOrderType(signed, request); err != nil {
		t.Fatal(err)
	}
	assertReports(0)
}

func TestMOMAuthenticatedCheckpointQueryDoesNotReenterAdmission(t *testing.T) {
	const fed core.FederationName = "caller-checkpoint"
	ctx, _, conn, _ := momCompletionReviewServer(t, fed)
	generation := momCompletionReviewCreate(ctx, t, conn, string(fed), "Parent")
	joined := momCompletionReviewJoin(ctx, t, conn, string(fed), "caller", "", generation)
	signed, cancel := context.WithTimeout(metadata.AppendToOutgoingContext(ctx, session.MetadataKey, string(joined.SessionToken)), time.Second)
	defer cancel()
	client := rtiv1.NewSavepointServiceClient(conn)
	if _, err := client.QuerySaveState(signed, &rtiv1.QuerySaveStateRequest{WireVersion: 1, FederationName: string(fed)}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.QueryRestoreState(signed, &rtiv1.QueryRestoreStateRequest{WireVersion: 1, FederationName: string(fed)}); err != nil {
		t.Fatal(err)
	}
}

func TestMOMCallerFollowsRestoreAndRejectsReplacedIdentity(t *testing.T) {
	const fed core.FederationName = "caller-rebound"
	ctx, srv, conn, _ := momCompletionReviewServer(t, fed)
	generation := momCompletionReviewCreate(ctx, t, conn, string(fed), "Parent")
	old := momCompletionReviewJoin(ctx, t, conn, string(fed), "member", "", generation)
	oldID, err := srv.fedMgr.MemberIdentity(fed, core.FederateHandle(old.FederateHandle))
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.fedMgr.ResignFederation(ctx, fed, oldID.Handle, core.ResignActionNoAction); err != nil {
		t.Fatal(err)
	}
	joined := momCompletionReviewJoin(ctx, t, conn, string(fed), "member", "", generation)
	reservation, err := srv.fedMgr.PrepareRestoreIdentity(fed, generation, []federation.RestoreIdentity{{Name: "member", CurrentHandle: core.FederateHandle(joined.FederateHandle), SavedHandle: oldID.Handle}})
	if err != nil {
		t.Fatal(err)
	}
	if err := reservation.Commit(); err != nil {
		reservation.Release()
		t.Fatal(err)
	}
	reservation.Release()
	acquired, released := 0, 0
	resolve := newMOMCallerResolver(srv.sessions, srv.fedMgr, func(core.FederationName) func() {
		acquired++
		return func() { released++ }
	})
	signed := metadata.NewIncomingContext(ctx, metadata.Pairs(session.MetadataKey, string(joined.SessionToken)))
	actualFed, actualHandle, release, err := resolve(signed)
	if err != nil || actualFed != fed || actualHandle != oldID.Handle || release == nil {
		t.Fatalf("resolved=%s/%d release=%v err=%v", actualFed, actualHandle, release != nil, err)
	}
	if acquired != 1 || released != 0 {
		t.Fatal("lease not retained through observation")
	}
	release()
	if released != 1 {
		t.Fatal("lease not released")
	}
	// Even a stale entry cannot authenticate a membership with the same name
	// and numeric handle but a different incarnation.
	srv.sessions.Register(oldID, old.SessionToken)
	stale := metadata.NewIncomingContext(ctx, metadata.Pairs(session.MetadataKey, string(old.SessionToken)))
	_, _, release, err = resolve(stale)
	if err == nil || release != nil || acquired != released {
		t.Fatalf("replaced membership accepted or lease leaked: %v", err)
	}
	srv.sessions.Register(core.MemberIdentity{Federation: fed, Generation: generation + 1, Name: "member", Handle: oldID.Handle, Incarnation: oldID.Incarnation}, old.SessionToken)
	_, _, release, err = resolve(stale)
	if err == nil || release != nil || acquired != released {
		t.Fatalf("wrong generation accepted or lease leaked: %v", err)
	}
	canceled, cancel := context.WithCancel(signed)
	cancel()
	_, _, release, err = resolve(canceled)
	if err == nil || release != nil || acquired != released {
		t.Fatalf("canceled observation accepted or lease leaked: %v", err)
	}
}
