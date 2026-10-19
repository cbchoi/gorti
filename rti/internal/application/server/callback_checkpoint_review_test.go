package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/adapters/delivery"
	grpcadapter "github.com/cbchoi/gorti/rti/internal/adapters/grpc"
	wire "github.com/cbchoi/gorti/rti/internal/adapters/protobuf"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	ddmdomain "github.com/cbchoi/gorti/rti/internal/hla/ddm"
	savepoint "github.com/cbchoi/gorti/rti/internal/hla/save_restore"
	"github.com/cbchoi/gorti/rti/pkg/logicaltime"
	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

type checkpointReviewReservation struct {
	commit  func() error
	release func()
}

func (r checkpointReviewReservation) Commit() error { return r.commit() }
func (r checkpointReviewReservation) Release() {
	if r.release != nil {
		r.release()
	}
}

func checkpointReviewReplay(t *testing.T) (*grpcadapter.CallbackReceipts, map[core.FederateHandle][]core.OutboundEvent, *grpcadapter.CallbackReplayReservation, []byte) {
	t.Helper()
	r, err := grpcadapter.NewCallbackReceipts(grpcadapter.CallbackReceiptOptions{
		Authenticate: func(context.Context, core.FederationName, core.FederateHandle) error { return nil },
		Observe: func(context.Context, core.FederationName, core.FederateHandle, *rtiv1.FederateEvent, bool, string) error {
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	order := rtiv1.OrderType_ORDER_TYPE_RECEIVE
	event := &rtiv1.FederateEvent{Seq: 5, Event: &rtiv1.FederateEvent_Receive{Receive: &rtiv1.ReceiveInteraction{InteractionClassHandle: 1, Metadata: &rtiv1.ObjectCallbackMetadata{ReceivedOrder: &order}}}}
	old, sent, err := r.PrepareRequired(context.Background(), "f", 7, event)
	if err != nil {
		t.Fatal(err)
	}
	sent(true)
	payload, err := (proto.MarshalOptions{Deterministic: true}).Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(map[string]any{"version": 1, "deliveries": []map[string]any{{"recipient": 1, "order": 1, "event": payload}}})
	if err != nil {
		t.Fatal(err)
	}
	pending, replay, err := r.PrepareCheckpointReplay(context.Background(), "f", data, []core.RestoreParticipant{{CurrentHandle: 7, SavedHandle: 1}})
	if err != nil {
		t.Fatal(err)
	}
	return r, pending, replay, old
}

func TestCallbackCheckpointReviewNewTicketAdmittedBeforePublication(t *testing.T) {
	r, pending, replay, old := checkpointReviewReplay(t)
	fresh, err := grpcadapter.EncodeOutboundEvent(pending[1][0])
	if err != nil || len(fresh.CallbackReceipt) != 32 || bytes.Equal(old, fresh.CallbackReceipt) {
		t.Fatal("fresh ticket preparation", err)
	}
	published := 0
	inner := checkpointReviewReservation{commit: func() error {
		counts, err := r.PendingDataCounts(context.Background(), "f", 1)
		if err != nil || counts.RO != 1 {
			t.Fatalf("ticket not admitted at publication boundary: %+v %v", counts, err)
		}
		published++
		return nil
	}}
	transition := &callbackReplayTransition{transition: r.PreserveRestoreTransition("f", testSavedLabel, []core.RestoreParticipant{{CurrentHandle: 7, SavedHandle: 1}}, inner), replay: replay}
	for i := 0; i < 2; i++ {
		if err := transition.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	transition.Release()
	if published != 1 {
		t.Fatal("duplicate publication")
	}
	if err := r.ReportEntry(context.Background(), "f", 1, old); status.Code(err) != codes.FailedPrecondition {
		t.Fatal("old data capability survived restore", err)
	}
	if err := r.ReportEntry(context.Background(), "f", 1, fresh.CallbackReceipt); err != nil {
		t.Fatal("fresh ticket expired during restore", err)
	}
	if err := r.Report(context.Background(), "f", 1, fresh.CallbackReceipt, true, ""); err != nil {
		t.Fatal(err)
	}
}

func TestCallbackCheckpointReviewFailedPublicationRollsBackFreshTicketsOnly(t *testing.T) {
	r, pending, replay, old := checkpointReviewReplay(t)
	fresh, err := grpcadapter.EncodeOutboundEvent(pending[1][0])
	if err != nil {
		t.Fatal(err)
	}
	want := errors.New("queue publication rejected")
	commits, releases := 0, 0
	inner := checkpointReviewReservation{commit: func() error { commits++; return want }, release: func() { releases++ }}
	transition := &callbackReplayTransition{transition: r.PreserveRestoreTransition("f", testSavedLabel, []core.RestoreParticipant{{CurrentHandle: 7, SavedHandle: 1}}, inner), replay: replay}
	for i := 0; i < 2; i++ {
		if err := transition.Commit(); !errors.Is(err, want) {
			t.Fatalf("commit lost failure: %v", err)
		}
	}
	transition.Release()
	if commits != 1 || releases != 1 {
		t.Fatalf("rollback calls: commit=%d release=%d", commits, releases)
	}
	if err := r.ReportEntry(context.Background(), "f", 1, fresh.CallbackReceipt); status.Code(err) != codes.FailedPrecondition {
		t.Fatal("rejected fresh receipt remains usable", err)
	}
	if err := r.ReportEntry(context.Background(), "f", 7, old); err != nil {
		t.Fatal("failed publication expired live data", err)
	}
	if err := r.Report(context.Background(), "f", 7, old, true, ""); err != nil {
		t.Fatal(err)
	}
}

func TestCallbackCheckpointReviewOneRecipientCapacityFailureReleasesMembershipAndAllQueues(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	srv, conn, _ := integerProductionServer(t, t.TempDir(), 801)
	const fed = "checkpoint-capacity-review"
	created, err := rtiv1.NewFederationServiceClient(conn).CreateFederation(ctx, &rtiv1.CreateFederationRequest{WireVersion: 1, FederationName: fed})
	if err != nil {
		t.Fatal(err)
	}
	var participants []savepoint.RestoreParticipant
	var terminal []core.OutboxDelivery
	raw := delivery.NewWithBatch(2, 1, 0)
	channels := map[core.FederateHandle]<-chan []core.OutboundEvent{}
	for _, name := range []string{"first", "second"} {
		joined := momCompletionReviewJoin(ctx, t, conn, fed, name, "", created.FederationGeneration)
		h := core.FederateHandle(joined.FederateHandle)
		participants = append(participants, savepoint.RestoreParticipant{Name: name, CurrentHandle: h, SavedHandle: h})
		terminal = append(terminal, core.OutboxDelivery{Recipient: h, Event: &savepoint.RestoredEvent{}})
		ch, closeStream, err := raw.Subscribe(ctx, fed, h)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = closeStream() })
		channels[h] = ch
	}
	old := &savepoint.RestoredEvent{}
	for h := range channels {
		if err := raw.Send(ctx, fed, h, old); err != nil {
			t.Fatal(err)
		}
	}
	pending := map[core.FederateHandle][]core.OutboundEvent{
		participants[0].SavedHandle: {&savepoint.RestoredEvent{}},
		participants[1].SavedHandle: {&savepoint.RestoredEvent{}, &savepoint.RestoredEvent{}},
	}
	reservation, err := newRestoreTransitionWithPending(srv.fedMgr, raw, nil, pending)(ctx, fed, participants, terminal)
	if err == nil {
		reservation.Release()
		t.Fatal("one-recipient overflow admitted")
	}
	if !errors.Is(err, core.ErrFederateOverflow) {
		t.Fatal(err)
	}
	for _, participant := range participants {
		h := participant.CurrentHandle
		if err := srv.fedMgr.ValidateMember(fed, h); err != nil {
			t.Fatal("membership lock/state not released", err)
		}
		if (<-channels[h])[0] != old {
			t.Fatal("failed restore changed an old queue")
		}
		if epoch, err := raw.CallbackEpoch(fed, h); err != nil || epoch != 0 {
			t.Fatalf("failed restore changed epoch: %d %v", epoch, err)
		}
		if err := raw.Send(ctx, fed, h, old); err != nil {
			t.Fatal("recipient lock not released", err)
		}
	}
}

const checkpointReviewCopiedFOM = `<objectModel><interactions><interactionClass><name>HLAinteractionRoot</name><interactionClass><name>CheckpointSignal</name><dimensions><dimension>Axis</dimension></dimensions><parameter><name>payload</name><dataType>HLAinteger32BE</dataType></parameter></interactionClass></interactionClass></interactions><dimensions><dimension><name>Axis</name><upperBound>18446744073709551615</upperBound></dimension></dimensions></objectModel>`

type checkpointReviewMember struct {
	handle      uint64
	generation  uint64
	ctx         context.Context
	stream      rtiv1.StreamService_EventBatchesClient
	queued      []*rtiv1.FederateEvent
	closeStream context.CancelFunc
}

func checkpointReviewCreateMembers(ctx context.Context, t *testing.T, conn *grpcgo.ClientConn, fed, factory string, names []string) map[string]*checkpointReviewMember {
	t.Helper()
	client := rtiv1.NewFederationServiceClient(conn)
	created, err := client.CreateFederation(ctx, &rtiv1.CreateFederationRequest{WireVersion: 1, FederationName: fed, LogicalTimeImplementationName: factory, FomModules: []*rtiv1.FOMModule{{Path: "review-copied.xml", Xml: []byte(checkpointReviewCopiedFOM)}}})
	if err != nil {
		t.Fatal(err)
	}
	result := map[string]*checkpointReviewMember{}
	for _, name := range names {
		joined, err := client.JoinFederation(ctx, &rtiv1.JoinFederationRequest{WireVersion: 1, FederationName: fed, FederateName: name, ExpectedFederationGeneration: &created.FederationGeneration})
		if err != nil {
			t.Fatal(err)
		}
		result[name] = &checkpointReviewMember{handle: joined.FederateHandle, generation: created.FederationGeneration, ctx: metadata.AppendToOutgoingContext(ctx, "gorti-session-token-bin", string(joined.SessionToken))}
	}
	return result
}

func checkpointReviewOpen(t *testing.T, conn *grpcgo.ClientConn, fed string, member *checkpointReviewMember) {
	t.Helper()
	ctx, cancel := context.WithCancel(member.ctx)
	member.closeStream = cancel
	t.Cleanup(cancel)
	var err error
	member.stream, err = rtiv1.NewStreamServiceClient(conn).EventBatches(ctx, &rtiv1.EventsRequest{WireVersion: 1, FederationName: fed, FederateHandle: member.handle, ExpectedFederationGeneration: &member.generation})
	if err != nil {
		t.Fatal(err)
	}
	if ready, err := member.stream.Recv(); err != nil || !ready.GetReady() {
		t.Fatal("stream readiness", ready, err)
	}
}

func checkpointReviewNext(t *testing.T, member *checkpointReviewMember, match func(*rtiv1.FederateEvent) bool) *rtiv1.FederateEvent {
	t.Helper()
	for {
		if len(member.queued) == 0 {
			batch, err := member.stream.Recv()
			if err != nil {
				t.Fatal(err)
			}
			member.queued = append(member.queued, batch.Events...)
		}
		event := member.queued[0]
		member.queued = member.queued[1:]
		if match(event) {
			return event
		}
	}
}

func TestCallbackCheckpointReviewFreshServerCopiedPendingAndExactTSO(t *testing.T) {
	for _, factory := range []string{float64TimeName, integer64TimeName} {
		t.Run(factory, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
			defer cancel()
			const fed = "checkpoint-copied-review"
			root := t.TempDir()
			source, conn, stop := integerProductionServer(t, root, 811)
			members := checkpointReviewCreateMembers(ctx, t, conn, fed, factory, []string{"producer", "receiver"})
			producer, receiver := members["producer"], members["receiver"]
			checkpointReviewOpen(t, conn, fed, receiver)
			model, err := source.foms.Get(ctx, fed)
			if err != nil {
				t.Fatal(err)
			}
			class, _ := model.LookupInteractionClass("CheckpointSignal")
			param, _ := model.LookupParameter(class, "payload")
			dimension, _ := model.(core.FOMHandleNameLookup).LookupDimension("Axis")
			ddm := rtiv1.NewDDMServiceClient(conn)
			region := func(member *checkpointReviewMember) uint64 {
				t.Helper()
				r, err := ddm.CreateRegion(member.ctx, &rtiv1.CreateRegionRequest{WireVersion: 1, FederationName: fed, FederateHandle: member.handle, RoutingSpaceHandle: 1, DimensionHandles: []uint64{uint64(dimension)}})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := ddm.SetRangeBounds(member.ctx, &rtiv1.SetRangeBoundsRequest{WireVersion: 1, FederationName: fed, FederateHandle: member.handle, RegionHandle: r.RegionHandle, DimensionHandle: uint64(dimension), Bounds: &rtiv1.Range{Lower: 1<<53 + 1, Upper: ^uint64(0)}}); err != nil {
					t.Fatal(err)
				}
				if _, err := ddm.CommitRegionModifications(member.ctx, &rtiv1.CommitRegionRequest{WireVersion: 1, FederationName: fed, FederateHandle: member.handle, RegionHandles: []uint64{r.RegionHandle}}); err != nil {
					t.Fatal(err)
				}
				return r.RegionHandle
			}
			pub, sub := region(producer), region(receiver)
			if _, err := rtiv1.NewDeclarationServiceClient(conn).PublishInteractionClass(producer.ctx, &rtiv1.PubInterRequest{WireVersion: 1, FederationName: fed, FederateHandle: producer.handle, InteractionClassHandle: uint64(class)}); err != nil {
				t.Fatal(err)
			}
			if _, err := ddm.SubscribeInteractionClassWithRegions(receiver.ctx, &rtiv1.SubscribeICWithRegionsRequest{WireVersion: 1, FederationName: fed, FederateHandle: receiver.handle, InteractionClassHandle: uint64(class), RegionHandles: []uint64{sub}}); err != nil {
				t.Fatal(err)
			}
			times := rtiv1.NewTimeServiceClient(conn)
			const exactStamp int64 = 1<<53 + 1
			request := &rtiv1.SendInteractionWithRegionsRequest{WireVersion: 1, FederationName: fed, FederateHandle: producer.handle, InteractionClassHandle: uint64(class), Parameters: map[uint64][]byte{uint64(param): {0, 0, 0, 7}}, RegionHandles: []uint64{pub}, UserSuppliedTag: []byte{0, 255}}
			if factory == integer64TimeName {
				if _, err := times.EnableTimeConstrained(receiver.ctx, &rtiv1.EnableConstrainedRequest{WireVersion: 1, FederationName: fed, FederateHandle: receiver.handle}); err != nil {
					t.Fatal(err)
				}
				request.LogicalTimeValue = integerWireTime(t, exactStamp)
			} else {
				if _, err := times.EnableAsynchronousDelivery(receiver.ctx, &rtiv1.EnableAsynchronousDeliveryRequest{WireVersion: 1, FederationName: fed, FederateHandle: receiver.handle}); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := ddm.SendInteractionWithRegions(producer.ctx, request); err != nil {
				t.Fatal(err)
			}
			if factory == integer64TimeName {
				if _, err := times.TimeAdvanceRequest(receiver.ctx, &rtiv1.TARRequest{WireVersion: 1, FederationName: fed, FederateHandle: receiver.handle, LogicalTimeValue: integerWireTime(t, exactStamp)}); err != nil {
					t.Fatal(err)
				}
			}
			prior := checkpointReviewNext(t, receiver, func(e *rtiv1.FederateEvent) bool { return e.GetReceive() != nil })
			copies := prior.GetReceive().GetMetadata().GetReceivedRegions().GetRegionHandles()
			if len(copies) != 1 || copies[0] == pub || len(prior.CallbackReceipt) != 32 {
				t.Fatal("source pending copied callback incomplete", prior)
			}
			query := &rtiv1.QueryBoundsRequest{WireVersion: 1, FederationName: fed, RegionHandle: copies[0], DimensionHandle: uint64(dimension)}
			if _, err := ddm.QueryBounds(receiver.ctx, query); err == nil {
				t.Fatal("source copied lease active before entry")
			}
			saves := rtiv1.NewSavepointServiceClient(conn)
			if _, err := saves.RequestFederationSave(producer.ctx, &rtiv1.RequestFederationSaveRequest{WireVersion: 1, FederationName: fed, FederateHandle: producer.handle, Label: "copied-pending"}); err != nil {
				t.Fatal(err)
			}
			for _, member := range []*checkpointReviewMember{producer, receiver} {
				r := &rtiv1.FederateSaveResponseRequest{WireVersion: 1, FederationName: fed, FederateHandle: member.handle}
				if _, err := saves.FederateSaveBegun(member.ctx, r); err != nil {
					t.Fatal(err)
				}
				if _, err := saves.FederateSaveComplete(member.ctx, r); err != nil {
					t.Fatal(err)
				}
			}
			receiver.closeStream()
			stop()
			target, targetConn, _ := integerProductionServer(t, root, 821)
			members = checkpointReviewCreateMembers(ctx, t, targetConn, fed, factory, []string{"receiver", "producer"})
			producer, receiver = members["producer"], members["receiver"]
			for _, member := range []*checkpointReviewMember{receiver, producer} {
				checkpointReviewOpen(t, targetConn, fed, member)
			}
			restore := rtiv1.NewSavepointServiceClient(targetConn)
			if _, err := restore.RequestFederationRestore(producer.ctx, &rtiv1.RequestFederationRestoreRequest{WireVersion: 1, FederationName: fed, FederateHandle: producer.handle, Label: "copied-pending"}); err != nil {
				t.Fatal(err)
			}
			for _, member := range []*checkpointReviewMember{producer, receiver} {
				checkpointReviewNext(t, member, func(e *rtiv1.FederateEvent) bool { return e.GetRestoreInitiate() != nil })
				if _, err := restore.FederateRestoreComplete(member.ctx, &rtiv1.FederateRestoreResponseRequest{WireVersion: 1, FederationName: fed, FederateHandle: member.handle}); err != nil {
					t.Fatal(err)
				}
			}
			for _, member := range []*checkpointReviewMember{receiver, producer} {
				terminal := checkpointReviewNext(t, member, func(e *rtiv1.FederateEvent) bool { return e.GetRestoreCompleted() != nil })
				member.handle = terminal.GetRestoreCompleted().GetFederateHandle()
				if member.handle>>32 != 821 {
					t.Fatal("terminal did not install fresh generation")
				}
			}
			replayed := checkpointReviewNext(t, receiver, func(e *rtiv1.FederateEvent) bool { return e.GetReceive() != nil })
			if len(replayed.CallbackReceipt) != 32 || bytes.Equal(prior.CallbackReceipt, replayed.CallbackReceipt) || !bytes.Equal(replayed.GetReceive().Parameters[uint64(param)], []byte{0, 0, 0, 7}) || !bytes.Equal(replayed.GetReceive().UserSuppliedTag, []byte{0, 255}) {
				t.Fatal("fresh-server replay lost payload or reused capability")
			}
			if !bytes.Equal(mustCheckpointReviewProto(t, prior.GetReceive().Metadata.ReceivedRegions), mustCheckpointReviewProto(t, replayed.GetReceive().Metadata.ReceivedRegions)) {
				t.Fatal("copied designator changed across restore")
			}
			if factory == integer64TimeName {
				at, err := wire.DecodeOptionalLogicalTimePair(logicaltime.HLAinteger64Time, replayed.GetReceive().LogicalTime, replayed.GetReceive().LogicalTimeValue)
				if err != nil || at == nil {
					t.Fatal("restored exact timestamp missing", err)
				}
				value, err := at.Integer64()
				if err != nil || value != exactStamp || replayed.GetReceive().Metadata.GetReceivedOrder() != rtiv1.OrderType_ORDER_TYPE_TIMESTAMP {
					t.Fatalf("restored TSO rounded or downgraded: %d %v", value, err)
				}
			}
			targetDDM := rtiv1.NewDDMServiceClient(targetConn)
			if _, err := targetDDM.QueryBounds(receiver.ctx, query); err == nil {
				t.Fatal("restored copied lease active before callback entry")
			}
			if _, err := targetDDM.QueryBounds(producer.ctx, query); err == nil {
				t.Fatal("foreign restored recipient obtained copied lease")
			}
			support := rtiv1.NewSupportServiceClient(targetConn)
			report := func(ticket []byte, entry bool) error {
				_, err := support.ReportCallbackInvocation(receiver.ctx, &rtiv1.ReportCallbackInvocationRequest{WireVersion: 1, FederationName: fed, FederateHandle: receiver.handle, CallbackReceipt: ticket, InvocationEntry: entry, Success: true})
				return err
			}
			if err := report(prior.CallbackReceipt, true); status.Code(err) != codes.FailedPrecondition {
				t.Fatal("old data ticket accepted by fresh server", err)
			}
			if err := report(replayed.CallbackReceipt, true); err != nil {
				t.Fatal(err)
			}
			manager := target.ddmMgr.(*ddmdomain.Manager)
			if bounds, err := manager.QueryBoundsFor(fed, core.FederateHandle(receiver.handle), ddmdomain.RegionHandle(copies[0]), ddmdomain.DimensionHandle(dimension)); err != nil || bounds.Lower != 1<<53+1 || bounds.Upper != ^uint64(0) {
				t.Fatal("callback entry did not activate exact restored recipient lease", bounds, err)
			}
			outgoing, _ := metadata.FromOutgoingContext(receiver.ctx)
			identity, err := target.sessions.CallerIdentity(metadata.NewIncomingContext(ctx, outgoing.Copy()))
			if err != nil {
				t.Fatal("restored receiver session unavailable", err)
			}
			t.Logf("active copied lease=%d restored receiver=%d session handle=%d", copies[0], receiver.handle, identity.Handle)
			dimensions, err := support.GetDimensionHandleSet(receiver.ctx, &rtiv1.GetDimensionHandleSetRequest{WireVersion: 1, FederationName: fed, FederateHandle: receiver.handle, RegionHandle: copies[0]})
			if err != nil || len(dimensions.GetDimensionHandles()) != 1 || dimensions.GetDimensionHandles()[0] != uint64(dimension) {
				t.Error("authenticated copied-region dimension query did not resolve restored receiver", dimensions, err)
			}
			if _, err := targetDDM.QueryBounds(producer.ctx, query); err == nil {
				t.Error("foreign session obtained restored receiver lease after handle permutation")
			}
			bounds, err := targetDDM.QueryBounds(receiver.ctx, query)
			if err != nil || !bounds.GetFound() || bounds.GetBounds().Lower != 1<<53+1 || bounds.GetBounds().Upper != ^uint64(0) {
				t.Error("authenticated copied-region query did not resolve restored receiver", bounds, err)
			}
			if err := report(replayed.CallbackReceipt, false); err != nil {
				t.Fatal(err)
			}
			if _, err := manager.QueryBoundsFor(fed, core.FederateHandle(receiver.handle), ddmdomain.RegionHandle(copies[0]), ddmdomain.DimensionHandle(dimension)); !errors.Is(err, core.ErrRegionNotFound) {
				t.Fatal("callback outcome did not expire restored recipient lease", err)
			}
			if _, err := targetDDM.QueryBounds(receiver.ctx, query); err == nil {
				t.Fatal("completed restored callback retained copied-region authority")
			}
		})
	}
}

func mustCheckpointReviewProto(t *testing.T, message proto.Message) []byte {
	t.Helper()
	data, err := (proto.MarshalOptions{Deterministic: true}).Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
