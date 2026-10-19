package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	grpcadapter "github.com/cbchoi/gorti/rti/internal/adapters/grpc"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
	savepoint "github.com/cbchoi/gorti/rti/internal/hla/save_restore"
	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func TestFederateFault20261004ReportAndSaveAdmissionAtomic(t *testing.T) {
	for _, capacity := range []int{1, 2} {
		t.Run(fmt.Sprintf("Float64/capacity-%d", capacity), func(t *testing.T) {
			faultAdmissionScenario[core.LogicalTime](t, capacity)
		})
		t.Run(fmt.Sprintf("Integer64/capacity-%d", capacity), func(t *testing.T) {
			faultAdmissionScenario[int64](t, capacity)
		})
	}
}

func faultAdmissionScenario[T core.LogicalTimeScalar](t *testing.T, capacity int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	r, err := newRTID(rtidConfig{EnableInteger64: true, GenerationEpoch: 241, SaveDir: t.TempDir(),
		OutboxEventCapacity: capacity, OutboxBatchSize: 1, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- r.grpcS.Serve(listener) }()
	conn, err := grpcgo.NewClient(listener.Addr().String(), grpcgo.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		r.sessions.Close()
		_ = conn.Close()
		r.grpcS.Stop()
		_ = listener.Close()
		<-done
		for _, fed := range r.fedMgr.Snapshot() {
			_ = r.momMgr.FederationDestroyed(context.Background(), fed.Name)
		}
		_ = r.plugins.Close()
	})
	const fed = "fault-admission"
	created, err := rtiv1.NewFederationServiceClient(conn).CreateFederation(ctx, &rtiv1.CreateFederationRequest{
		WireVersion: 1, FederationName: fed, LogicalTimeImplementationName: core.ScalarRepresentation[T]().String(),
		FomModules: []*rtiv1.FOMModule{momCompletionReviewModule(t, "Item")}})
	if err != nil {
		t.Fatal(err)
	}
	lost := momCompletionReviewJoin(ctx, t, conn, fed, "lost", "", created.FederationGeneration)
	survivor := momCompletionReviewJoin(ctx, t, conn, fed, "survivor", "", created.FederationGeneration)
	channels := make(map[uint64]<-chan []core.OutboundEvent)
	for _, h := range []uint64{lost.FederateHandle, survivor.FederateHandle} {
		ch, release, err := r.outbox.Subscribe(ctx, fed, core.FederateHandle(h))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = release() })
		channels[h] = ch
	}
	model, err := r.foms.Get(ctx, fed)
	if err != nil {
		t.Fatal(err)
	}
	class, ok := model.LookupInteractionClass(faultReportClass)
	if !ok {
		t.Fatal("standard MIM lost report missing")
	}
	_, err = rtiv1.NewDeclarationServiceClient(conn).SubscribeInteractionClass(ctx, &rtiv1.SubInterRequest{
		WireVersion: 1, FederationName: fed, FederateHandle: survivor.FederateHandle, InteractionClassHandle: uint64(class)})
	if err != nil {
		t.Fatal(err)
	}
	saves := rtiv1.NewSavepointServiceClient(conn)
	_, err = saves.RequestFederationSave(ctx, &rtiv1.RequestFederationSaveRequest{
		WireVersion: 1, FederationName: fed, FederateHandle: survivor.FederateHandle, Label: "bounded-fault"})
	if err != nil {
		t.Fatal(err)
	}
	for h, ch := range channels {
		events := drainScheduledEvents(ch)
		if len(events) != 1 {
			t.Fatalf("member %d initiation count=%d", h, len(events))
		}
		event, err := grpcadapter.EncodeOutboundEvent(events[0])
		if err != nil || event.GetSaveInitiate() == nil {
			t.Fatalf("member %d not instructed: %v %v", h, event, err)
		}
	}
	id, err := r.fedMgr.MemberIdentity(fed, core.FederateHandle(lost.FederateHandle))
	if err != nil {
		t.Fatal(err)
	}
	// Exercise the production loss hook under the same admission as the session
	// timer, without a concurrent stream draining the capacity being tested.
	loss := func() error {
		var release func()
		var err error
		if core.ScalarRepresentation[T]() == core.ScalarRepresentation[int64]() {
			release, err = r.integerSave.AcquireLoss(fed)
		} else {
			release, err = r.saveMgr.(*savepoint.Manager).AcquireLoss(fed)
		}
		if err != nil {
			return err
		}
		defer release()
		return r.fedMgr.AutomaticResignFederation(ctx, id, func() bool { return true })
	}
	ch := channels[survivor.FederateHandle]
	for attempt := 0; attempt < 2; attempt++ {
		// Capacity two with one occupied slot must fail just like capacity one.
		var filler *mom.InteractionEvent
		if capacity == 2 {
			filler = &mom.InteractionEvent{Sequence: uint64(attempt + 1), Class: class}
			if err := r.outbox.Send(ctx, fed, core.FederateHandle(survivor.FederateHandle), filler); err != nil {
				t.Fatal(err)
			}
		}
		if err := loss(); !errors.Is(err, core.ErrFederateOverflow) {
			t.Fatalf("attempt %d loss error=%v, want capacity rejection", attempt, err)
		}
		if err := r.fedMgr.ValidateMember(fed, id.Handle); err != nil {
			t.Fatal("rejected fault removed membership", err)
		}
		state, err := saves.QuerySaveState(ctx, &rtiv1.QuerySaveStateRequest{WireVersion: 1, FederationName: fed, Label: "bounded-fault"})
		if err != nil || state.State != rtiv1.SaveState_SAVE_STATE_INITIATED || state.ActiveLabel != "bounded-fault" {
			t.Fatalf("rejected fault changed save: %v %v", state, err)
		}
		for _, event := range drainScheduledEvents(ch) {
			if event != filler {
				t.Errorf("attempt %d published partial fault before whole admission: %T", attempt, event)
			}
		}
	}
	if capacity == 1 {
		return // Two callbacks cannot fit; no partial publication is permitted.
	}
	if err := loss(); err != nil {
		t.Fatal("retry after freeing capacity failed", err)
	}
	if err := r.fedMgr.ValidateMember(fed, id.Handle); !errors.Is(err, core.ErrFederateNotJoined) {
		t.Fatalf("successful fault did not remove lost member: %v", err)
	}
	reports, failures := 0, 0
	for _, event := range drainScheduledEvents(ch) {
		encoded, err := grpcadapter.EncodeOutboundEvent(event)
		if err != nil {
			t.Fatal(err)
		}
		if report := encoded.GetReceive(); report != nil && report.InteractionClassHandle == uint64(class) {
			reports++
		}
		if failure := encoded.GetSaveFailed(); failure != nil {
			failures++
			if failure.Label != "bounded-fault" || failure.Reason == nil || *failure.Reason != int32(savepoint.RTIDetectedFailureDuringSave) {
				t.Fatalf("wrong save fault cause: %v", failure)
			}
		}
	}
	if reports != 1 || failures != 1 {
		t.Fatalf("successful retry reports=%d outcomes=%d, want exactly one each", reports, failures)
	}
	state, err := saves.QuerySaveState(ctx, &rtiv1.QuerySaveStateRequest{WireVersion: 1, FederationName: fed, Label: "bounded-fault"})
	if err != nil || state.State != rtiv1.SaveState_SAVE_STATE_NOT_SAVED || state.ActiveLabel != "" {
		t.Fatalf("successful fault left save active: %v %v", state, err)
	}
}
