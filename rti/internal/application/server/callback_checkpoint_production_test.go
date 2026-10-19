package server

import (
	"bytes"
	"context"
	"encoding/binary"
	"testing"
	"time"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestProductionCallbackCheckpointFreshServerBothFamilies(t *testing.T) {
	for _, factory := range []string{float64TimeName, integer64TimeName} {
		for _, stage := range []string{testReadyLabel, "client-unentered", "client-entered"} {
			t.Run(factory+"/"+stage, func(t *testing.T) { productionCallbackCheckpoint(t, factory, stage) })
		}
	}
}

func productionCallbackCheckpoint(t *testing.T, factory, stage string) {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	dir := t.TempDir()
	const fed = "callback-checkpoint"
	type member struct {
		joined     *rtiv1.JoinFederationResponse
		generation uint64
		ctx        context.Context
		stream     rtiv1.StreamService_EventBatchesClient
		cancel     context.CancelFunc
		queued     []*rtiv1.FederateEvent
	}
	create := func(_ *rtid, conn *grpcgo.ClientConn, order []string) map[string]*member {
		t.Helper()
		federations := rtiv1.NewFederationServiceClient(conn)
		created, err := federations.CreateFederation(ctx, &rtiv1.CreateFederationRequest{WireVersion: 1, FederationName: fed, LogicalTimeImplementationName: factory, FomModules: []*rtiv1.FOMModule{{Path: "signal.xml", Xml: []byte(conveyedProductionFOM)}}})
		if err != nil {
			t.Fatal(err)
		}
		result := map[string]*member{}
		for _, name := range order {
			joined := momCompletionReviewJoin(ctx, t, conn, fed, name, "", created.FederationGeneration)
			result[name] = &member{joined: joined, generation: created.FederationGeneration, ctx: metadata.AppendToOutgoingContext(ctx, "gorti-session-token-bin", string(joined.SessionToken))}
		}
		return result
	}
	open := func(conn *grpcgo.ClientConn, m *member) {
		t.Helper()
		streamctx, cancel := context.WithCancel(m.ctx)
		m.cancel = cancel
		t.Cleanup(cancel)
		var err error
		m.stream, err = rtiv1.NewStreamServiceClient(conn).EventBatches(streamctx, &rtiv1.EventsRequest{WireVersion: 1, FederationName: fed, FederateHandle: m.joined.FederateHandle, ExpectedFederationGeneration: &m.generation})
		if err != nil {
			t.Fatal(err)
		}
		if ready, err := m.stream.Recv(); err != nil || !ready.GetReady() {
			t.Fatal("stream readiness", ready, err)
		}
	}
	next := func(m *member, match func(*rtiv1.FederateEvent) bool) *rtiv1.FederateEvent {
		t.Helper()
		for {
			if len(m.queued) == 0 {
				batch, err := m.stream.Recv()
				if err != nil {
					t.Fatal(err)
				}
				m.queued = append(m.queued, batch.Events...)
			}
			for len(m.queued) > 0 {
				event := m.queued[0]
				m.queued = m.queued[1:]
				if match(event) {
					return event
				}
			}
		}
	}
	source, conn, stop := integerProductionServer(t, dir, 331)
	members := create(source, conn, []string{"producer", "receiver"})
	producer, receiver := members["producer"], members["receiver"]
	savedReceiver := receiver.joined.FederateHandle
	model, err := source.foms.Get(ctx, fed)
	if err != nil {
		t.Fatal(err)
	}
	class, _ := model.LookupInteractionClass("Signal")
	param, _ := model.LookupParameter(class, "value")
	decl := rtiv1.NewDeclarationServiceClient(conn)
	if _, err = decl.PublishInteractionClass(producer.ctx, &rtiv1.PubInterRequest{WireVersion: 1, FederationName: fed, FederateHandle: producer.joined.FederateHandle, InteractionClassHandle: uint64(class)}); err != nil {
		t.Fatal(err)
	}
	if _, err = decl.SubscribeInteractionClass(receiver.ctx, &rtiv1.SubInterRequest{WireVersion: 1, FederationName: fed, FederateHandle: receiver.joined.FederateHandle, InteractionClassHandle: uint64(class)}); err != nil {
		t.Fatal(err)
	}
	if _, err = rtiv1.NewTimeServiceClient(conn).EnableAsynchronousDelivery(receiver.ctx, &rtiv1.EnableAsynchronousDeliveryRequest{WireVersion: 1, FederationName: fed, FederateHandle: receiver.joined.FederateHandle}); err != nil {
		t.Fatal(err)
	}
	if stage != testReadyLabel {
		open(conn, receiver)
	}
	for i := 0; i < 2; i++ {
		if _, err = rtiv1.NewObjectServiceClient(conn).SendInteraction(producer.ctx, &rtiv1.SendInteractionRequest{WireVersion: 1, FederationName: fed, FederateHandle: producer.joined.FederateHandle, InteractionClassHandle: uint64(class), Parameters: map[uint64][]byte{uint64(param): {0, 0, 0, 1}}}); err != nil {
			t.Fatal(err)
		}
	}
	var old []*rtiv1.FederateEvent
	if stage != testReadyLabel {
		for i := 0; i < 2; i++ {
			old = append(old, next(receiver, func(e *rtiv1.FederateEvent) bool { return e.GetReceive() != nil }))
		}
		if stage == "client-entered" {
			for _, event := range old {
				if _, err = rtiv1.NewSupportServiceClient(conn).ReportCallbackInvocation(receiver.ctx, &rtiv1.ReportCallbackInvocationRequest{WireVersion: 1, FederationName: fed, FederateHandle: receiver.joined.FederateHandle, CallbackReceipt: event.CallbackReceipt, InvocationEntry: true, Success: true}); err != nil {
					t.Fatal(err)
				}
				if _, err = rtiv1.NewSupportServiceClient(conn).ReportCallbackInvocation(receiver.ctx, &rtiv1.ReportCallbackInvocationRequest{WireVersion: 1, FederationName: fed, FederateHandle: receiver.joined.FederateHandle, CallbackReceipt: event.CallbackReceipt, Success: true}); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	saves := rtiv1.NewSavepointServiceClient(conn)
	if _, err = saves.RequestFederationSave(producer.ctx, &rtiv1.RequestFederationSaveRequest{WireVersion: 1, FederationName: fed, FederateHandle: producer.joined.FederateHandle, Label: testSavedLabel}); err != nil {
		t.Fatal(err)
	}
	for _, m := range []*member{producer, receiver} {
		req := &rtiv1.FederateSaveResponseRequest{WireVersion: 1, FederationName: fed, FederateHandle: m.joined.FederateHandle}
		if _, err = saves.FederateSaveBegun(m.ctx, req); err != nil {
			t.Fatal(err)
		}
		if _, err = saves.FederateSaveComplete(m.ctx, req); err != nil {
			t.Fatal(err)
		}
	}
	if receiver.cancel != nil {
		receiver.cancel()
	}
	stop()
	target, targetConn, _ := integerProductionServer(t, dir, 341)
	members = create(target, targetConn, []string{"receiver", "producer"})
	producer, receiver = members["producer"], members["receiver"]
	open(targetConn, receiver)
	open(targetConn, producer)
	restore := rtiv1.NewSavepointServiceClient(targetConn)
	if _, err = restore.RequestFederationRestore(producer.ctx, &rtiv1.RequestFederationRestoreRequest{WireVersion: 1, FederationName: fed, FederateHandle: producer.joined.FederateHandle, Label: testSavedLabel}); err != nil {
		t.Fatal(err)
	}
	for _, m := range []*member{producer, receiver} {
		init := next(m, func(e *rtiv1.FederateEvent) bool { return e.GetRestoreInitiate() != nil })
		if m == receiver && init.GetRestoreInitiate().FederateHandle != savedReceiver {
			t.Fatal("initiation did not preserve saved IEEE handle")
		}
		if _, err = restore.FederateRestoreComplete(m.ctx, &rtiv1.FederateRestoreResponseRequest{WireVersion: 1, FederationName: fed, FederateHandle: m.joined.FederateHandle}); err != nil {
			t.Fatal(err)
		}
	}
	terminal := next(receiver, func(e *rtiv1.FederateEvent) bool { return e.GetRestoreCompleted() != nil })
	receiver.joined.FederateHandle = terminal.GetRestoreCompleted().GetFederateHandle()
	if receiver.joined.FederateHandle>>32 != 341 {
		t.Fatal("terminal did not install fresh execution identity", terminal)
	}
	var want uint64 = 2
	if stage == "client-entered" {
		want = 0
	}
	pendingCount := func() uint64 {
		t.Helper()
		attrs, err := rtiv1.NewMomServiceClient(targetConn).QueryFederateAttributes(receiver.ctx, &rtiv1.QueryFederateAttributesRequest{WireVersion: 1, FederationName: fed, FederateHandle: receiver.joined.FederateHandle})
		if err != nil {
			t.Fatal(err)
		}
		value := attrs.AttributeValues["HLAROlength"]
		if len(value) != 4 || attrs.UnavailableAttributes["HLAROlength"] != "" {
			t.Fatal("restored queue coverage unavailable", attrs)
		}
		return uint64(binary.BigEndian.Uint32(value))
	}
	if count := pendingCount(); count != want {
		t.Fatalf("restored pending=%d want %d", count, want)
	}
	for i := uint64(0); i < want; i++ {
		event := next(receiver, func(e *rtiv1.FederateEvent) bool { return e.GetReceive() != nil })
		if !bytes.Equal(event.GetReceive().Parameters[uint64(param)], []byte{0, 0, 0, 1}) || len(event.CallbackReceipt) != 32 {
			t.Fatal("restored payload/ticket changed", event)
		}
		for _, prior := range old {
			if bytes.Equal(prior.CallbackReceipt, event.CallbackReceipt) {
				t.Fatal("old capability reused")
			}
		}
		if _, err = rtiv1.NewSupportServiceClient(targetConn).ReportCallbackInvocation(receiver.ctx, &rtiv1.ReportCallbackInvocationRequest{WireVersion: 1, FederationName: fed, FederateHandle: receiver.joined.FederateHandle, CallbackReceipt: event.CallbackReceipt, InvocationEntry: true, Success: true}); err != nil {
			t.Fatal(err)
		}
		if _, err = rtiv1.NewSupportServiceClient(targetConn).ReportCallbackInvocation(receiver.ctx, &rtiv1.ReportCallbackInvocationRequest{WireVersion: 1, FederationName: fed, FederateHandle: receiver.joined.FederateHandle, CallbackReceipt: event.CallbackReceipt, Success: true}); err != nil {
			t.Fatal(err)
		}
	}
	for _, prior := range old {
		_, err = rtiv1.NewSupportServiceClient(targetConn).ReportCallbackInvocation(receiver.ctx, &rtiv1.ReportCallbackInvocationRequest{WireVersion: 1, FederationName: fed, FederateHandle: receiver.joined.FederateHandle, CallbackReceipt: prior.CallbackReceipt, InvocationEntry: true, Success: true})
		if status.Code(err) != codes.FailedPrecondition {
			t.Fatal("old capability remains usable", err)
		}
	}
	if count := pendingCount(); count != 0 {
		t.Fatal("restored entry did not drain pending", count)
	}
	_ = target
}
