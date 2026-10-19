package server

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/adapters/savebundle"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	syncpkg "github.com/cbchoi/gorti/rti/internal/hla/synchronization"
	"github.com/cbchoi/gorti/rti/pkg/federate"
	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

type productionSyncMember struct {
	h       uint64
	ctx     context.Context
	receive func() (*rtiv1.FederateEvent, error)
}

// This exercises the strict manager checkpoint adapter through fresh production
// TCP roots. The separate restart test covers the whole save/restore coordinator.
func TestSynchronizationProductionTCPStrictCheckpointReplay(t *testing.T) {
	for _, implementation := range []string{float64TimeName, integer64TimeName} {
		t.Run(implementation, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			const fed = "sync-checkpoint"
			create := func(conn *grpcgo.ClientConn) uint64 {
				t.Helper()
				response, err := rtiv1.NewFederationServiceClient(conn).CreateFederation(ctx, &rtiv1.CreateFederationRequest{
					WireVersion: 1, FederationName: fed, LogicalTimeImplementationName: implementation,
					FomModules: []*rtiv1.FOMModule{momCompletionReviewModule(t, "Item")},
				})
				if err != nil {
					t.Fatal(err)
				}
				return response.FederationGeneration
			}
			source, sourceConn, stopSource := integerProductionServer(t, t.TempDir(), 221)
			generation := create(sourceConn)
			a := productionSyncJoin(ctx, t, sourceConn, fed, "a", generation, true)
			b := productionSyncJoin(ctx, t, sourceConn, fed, "b", generation, true)
			_, err := rtiv1.NewSyncServiceClient(sourceConn).RegisterFederationSynchronizationPoint(a.ctx,
				&rtiv1.RegisterSyncPointRequest{WireVersion: 1, FederationName: fed, FederateHandle: a.h, Label: "p", Tag: []byte("saved-tag")})
			if err != nil {
				t.Fatal(err)
			}
			success := productionSyncReceive(t, a, func(e *rtiv1.FederateEvent) bool { return e.GetSyncRegistrationSucceeded() != nil })
			announcementA := productionSyncReceive(t, a, func(e *rtiv1.FederateEvent) bool { return e.GetSyncAnnounced() != nil })
			announcementB := productionSyncReceive(t, b, func(e *rtiv1.FederateEvent) bool { return e.GetSyncAnnounced() != nil })
			sourceManager := source.syncMgr.(*syncpkg.Manager)
			before := sourceManager.ExportState(fed)
			if len(before.Obligations) != 3 || len(before.Pending) != 0 {
				t.Fatal("fixture did not retain all admitted uninvoked callbacks")
			}
			image, err := (savebundle.StrictSynchronization{Manager: sourceManager}).Marshal(fed)
			if err != nil {
				t.Fatal(err)
			}
			stopSource()
			target, targetConn, _ := integerProductionServer(t, t.TempDir(), 222)
			generation = create(targetConn)
			// Reverse join order and change generation to forbid ordinal reuse.
			newB := productionSyncJoin(ctx, t, targetConn, fed, "b", generation, true)
			newA := productionSyncJoin(ctx, t, targetConn, fed, "a", generation, true)
			mapping := map[core.FederateHandle]core.FederateHandle{core.FederateHandle(a.h): core.FederateHandle(newA.h), core.FederateHandle(b.h): core.FederateHandle(newB.h)}
			targetManager := target.syncMgr.(*syncpkg.Manager)
			codec := savebundle.StrictSynchronization{Manager: targetManager}
			image, err = codec.RewriteFederates(image, func(h core.FederateHandle) (core.FederateHandle, error) {
				if rewritten, ok := mapping[h]; ok {
					return rewritten, nil
				}
				return 0, fmt.Errorf("unexpected saved federate %d", h)
			})
			if err != nil {
				t.Fatal(err)
			}
			commit, err := codec.PrepareRestore(fed, image)
			if err != nil {
				t.Fatal(err)
			}
			commit()
			if restored := targetManager.ExportState(fed); len(restored.Pending) != 3 || restored.NextEpoch != before.NextEpoch || restored.NextSequence != before.NextSequence {
				t.Fatal("strict restore lost callback obligations or identities")
			}
			if err := target.synchronizationLifecycle.Restored(ctx, fed); err != nil {
				t.Fatal(err)
			}
			replayedSuccess := productionSyncReceive(t, newA, func(e *rtiv1.FederateEvent) bool { return e.GetSyncRegistrationSucceeded() != nil })
			replayedA := productionSyncReceive(t, newA, func(e *rtiv1.FederateEvent) bool { return e.GetSyncAnnounced() != nil })
			replayedB := productionSyncReceive(t, newB, func(e *rtiv1.FederateEvent) bool { return e.GetSyncAnnounced() != nil })
			if replayedSuccess.Seq != success.Seq || replayedA.Seq != announcementA.Seq || replayedB.Seq != announcementB.Seq || string(replayedA.GetSyncAnnounced().Tag) != "saved-tag" {
				t.Fatal("restore changed callback kind, sequence or payload")
			}
			productionSyncReport(t, targetConn, fed, newA, replayedSuccess, true)
			productionSyncReport(t, targetConn, fed, newA, replayedSuccess, false)
			client := rtiv1.NewSyncServiceClient(targetConn)
			for i, member := range []productionSyncMember{newA, newB} {
				announcement := []*rtiv1.FederateEvent{replayedA, replayedB}[i]
				request := &rtiv1.AchieveSyncPointRequest{WireVersion: 1, FederationName: fed, FederateHandle: member.h, Label: "p"}
				if _, err := client.SynchronizationPointAchieved(member.ctx, request); err == nil {
					t.Fatal("restored transport callback fabricated invocation entry")
				}
				productionSyncReport(t, targetConn, fed, member, announcement, true)
				if _, err := client.SynchronizationPointAchieved(member.ctx, request); err != nil {
					t.Fatal("restored reentrant achievement rejected", err)
				}
				productionSyncReport(t, targetConn, fed, member, announcement, false)
			}
			for _, member := range []productionSyncMember{newA, newB} {
				terminal := productionSyncReceive(t, member, func(e *rtiv1.FederateEvent) bool { return e.GetSyncSynchronized() != nil })
				productionSyncReport(t, targetConn, fed, member, terminal, true)
				if targetManager.QueryState(fed, "p") != syncpkg.StateAchieved {
					t.Fatal("restored label retired before callback completion")
				}
				productionSyncReport(t, targetConn, fed, member, terminal, false)
			}
			if targetManager.QueryState(fed, "p") != syncpkg.StateUnknown || len(targetManager.ExportState(fed).Obligations) != 0 {
				t.Fatal("restored callbacks did not finish their durable obligations")
			}
		})
	}
}

func TestSynchronizationProductionTCPGoSDKReentrantCallbacks(t *testing.T) {
	for _, implementation := range []string{float64TimeName, integer64TimeName} {
		t.Run(implementation, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			srv, transport, _ := integerProductionServer(t, t.TempDir(), 211)
			connection, err := federate.Connect(ctx, transport.Target())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = connection.Close() })
			other, err := federate.Connect(ctx, transport.Target())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = other.Close() })
			module := momCompletionReviewModule(t, "Item")
			spec := federate.FederationSpec{Name: "sync-sdk", LogicalTimeImplementationName: implementation, FOMModules: []federate.FOMModule{{Path: module.Path, XML: module.Xml}}}
			if _, err := connection.CreateFederation(ctx, spec); err != nil {
				t.Fatal(err)
			}
			a, err := connection.JoinFederation(ctx, spec, "a")
			if err != nil {
				t.Fatal(err)
			}
			b, err := other.JoinFederation(ctx, spec, "b")
			if err != nil {
				t.Fatal(err)
			}
			if !a.CallbackInvocationEntrySupported() || !b.CallbackInvocationEntrySupported() {
				t.Fatal("production join did not advertise mandatory callback entry support")
			}
			manager := srv.syncMgr.(*syncpkg.Manager)
			next := func(participant *federate.Federate, match func(federate.Event) bool) federate.Event {
				t.Helper()
				for {
					select {
					case event, open := <-participant.Events():
						if !open {
							t.Fatal("SDK callback stream closed")
						}
						if match(event) {
							carrier, ok := event.(interface{ CallbackReceipt() []byte })
							if !ok || len(carrier.CallbackReceipt()) != 32 {
								t.Fatal("SDK lost mandatory synchronization receipt")
							}
							return event
						}
						if callbackErr, reportErr := participant.DispatchEvent(ctx, event, func(federate.Event) error { return nil }); callbackErr != nil || reportErr != nil {
							t.Fatal(callbackErr, reportErr)
						}
					case <-ctx.Done():
						t.Fatal("SDK callback deadline", ctx.Err())
					}
				}
			}
			if err := a.RegisterFederationSynchronizationPoint(ctx, "p", []byte("tag"), nil); err != nil {
				t.Fatal(err)
			}
			announcements := []federate.Event{
				next(a, func(e federate.Event) bool { _, ok := e.(federate.SynchronizationPointAnnounced); return ok }),
				next(b, func(e federate.Event) bool { _, ok := e.(federate.SynchronizationPointAnnounced); return ok }),
			}
			if err := a.SynchronizationPointAchieved(ctx, "p", true); err == nil {
				t.Fatal("Go SDK transport alone authorized achievement")
			}
			for i, participant := range []*federate.Federate{a, b} {
				calls := 0
				body := func(event federate.Event) error {
					calls++
					announcement := event.(federate.SynchronizationPointAnnounced)
					for _, inv := range manager.ExportState("sync-sdk").Points[0].Invocations {
						if uint64(inv.Recipient) == participant.CurrentHandle() && (!inv.AnnounceEntered || inv.AnnounceInvoked) {
							t.Fatal("SDK did not report entry before handler body")
						}
					}
					return participant.SynchronizationPointAchieved(ctx, announcement.Label, true)
				}
				for repeat := 0; repeat < 2; repeat++ {
					if callbackErr, reportErr := participant.DispatchEvent(ctx, announcements[i], body); callbackErr != nil || reportErr != nil {
						t.Fatal(callbackErr, reportErr)
					}
				}
				if calls != 1 {
					t.Fatal("Go SDK duplicate ticket reinvoked handler")
				}
			}
			for _, participant := range []*federate.Federate{a, b} {
				terminal := next(participant, func(e federate.Event) bool { _, ok := e.(federate.FederationSynchronized); return ok })
				callbackErr, reportErr := participant.DispatchEvent(ctx, terminal, func(federate.Event) error {
					if manager.QueryState("sync-sdk", "p") != syncpkg.StateAchieved {
						t.Fatal("label retired before handler completion")
					}
					return errors.New("test ambassador failure")
				})
				if callbackErr == nil || reportErr != nil {
					t.Fatal("throwing callback failed to report actual completion", callbackErr, reportErr)
				}
			}
			if manager.QueryState("sync-sdk", "p") != syncpkg.StateUnknown || len(manager.ExportState("sync-sdk").Obligations) != 0 {
				t.Fatal("Go SDK completed callbacks left synchronization obligations")
			}
		})
	}
}

func productionSyncJoin(ctx context.Context, t *testing.T, conn *grpcgo.ClientConn, fed, name string, generation uint64, batches bool) productionSyncMember {
	t.Helper()
	joined := momCompletionReviewJoin(ctx, t, conn, fed, name, "", generation)
	signed := metadata.AppendToOutgoingContext(ctx, "gorti-session-token-bin", string(joined.SessionToken))
	request := &rtiv1.EventsRequest{WireVersion: 1, FederationName: fed, FederateHandle: joined.FederateHandle}
	member := productionSyncMember{h: joined.FederateHandle, ctx: signed}
	client := rtiv1.NewStreamServiceClient(conn)
	if batches {
		stream, err := client.EventBatches(signed, request)
		if err != nil {
			t.Fatal(err)
		}
		var buffered []*rtiv1.FederateEvent
		member.receive = func() (*rtiv1.FederateEvent, error) {
			for len(buffered) == 0 {
				batch, err := stream.Recv()
				if err != nil {
					return nil, err
				}
				buffered = batch.Events
			}
			event := buffered[0]
			buffered = buffered[1:]
			return event, nil
		}
	} else {
		stream, err := client.Events(signed, request)
		if err != nil {
			t.Fatal(err)
		}
		member.receive = stream.Recv
	}
	return member
}

func productionSyncReceive(t *testing.T, member productionSyncMember, match func(*rtiv1.FederateEvent) bool) *rtiv1.FederateEvent {
	t.Helper()
	for {
		event, err := member.receive()
		if err != nil {
			t.Fatal("production callback stream", err)
		}
		if match(event) {
			if len(event.CallbackReceipt) != 32 {
				t.Fatal("mandatory synchronization callback has no opaque receipt")
			}
			return event
		}
	}
}

func productionSyncReport(t *testing.T, conn *grpcgo.ClientConn, fed string, member productionSyncMember, event *rtiv1.FederateEvent, entry bool) {
	t.Helper()
	_, err := rtiv1.NewSupportServiceClient(conn).ReportCallbackInvocation(member.ctx, &rtiv1.ReportCallbackInvocationRequest{
		WireVersion: 1, FederationName: fed, FederateHandle: member.h, CallbackReceipt: event.CallbackReceipt, Success: true, InvocationEntry: entry,
	})
	if err != nil {
		t.Fatal("authenticated production callback phase", entry, err)
	}
}

func TestSynchronizationProductionTCPInvocationLifecycle(t *testing.T) {
	for _, implementation := range []string{float64TimeName, integer64TimeName} {
		for _, batches := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/batches=%t", implementation, batches), func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()
				srv, conn, _ := integerProductionServer(t, t.TempDir(), 201)
				fed := "production-sync"
				federation := rtiv1.NewFederationServiceClient(conn)
				created, err := federation.CreateFederation(ctx, &rtiv1.CreateFederationRequest{WireVersion: 1, FederationName: fed, LogicalTimeImplementationName: implementation, FomModules: []*rtiv1.FOMModule{momCompletionReviewModule(t, "Item")}})
				if err != nil {
					t.Fatal(err)
				}
				a := productionSyncJoin(ctx, t, conn, fed, "a", created.FederationGeneration, batches)
				b := productionSyncJoin(ctx, t, conn, fed, "b", created.FederationGeneration, batches)
				client := rtiv1.NewSyncServiceClient(conn)
				register := func(label string, required []uint64) {
					t.Helper()
					_, err := client.RegisterFederationSynchronizationPoint(a.ctx, &rtiv1.RegisterSyncPointRequest{WireVersion: 1, FederationName: fed, FederateHandle: a.h, Label: label, RequiredFederates: required})
					if err != nil {
						t.Fatal("register", err)
					}
				}
				achieve := func(member productionSyncMember, label string) error {
					_, err := client.SynchronizationPointAchieved(member.ctx, &rtiv1.AchieveSyncPointRequest{WireVersion: 1, FederationName: fed, FederateHandle: member.h, Label: label})
					return err
				}
				manager := srv.syncMgr.(*syncpkg.Manager)
				if !manager.RequiresInvocationReceipts() {
					t.Fatal("root did not construct strict synchronization")
				}
				register("all", []uint64{})
				success := productionSyncReceive(t, a, func(e *rtiv1.FederateEvent) bool { return e.GetSyncRegistrationSucceeded() != nil })
				productionSyncReport(t, conn, fed, a, success, true)
				productionSyncReport(t, conn, fed, a, success, false)
				announcedA := productionSyncReceive(t, a, func(e *rtiv1.FederateEvent) bool { return e.GetSyncAnnounced() != nil })
				announcedB := productionSyncReceive(t, b, func(e *rtiv1.FederateEvent) bool { return e.GetSyncAnnounced() != nil })
				if err := achieve(a, "all"); err == nil {
					t.Fatal("transport delivery alone authorized achievement")
				}
				productionSyncReport(t, conn, fed, a, announcedA, true)
				if err := achieve(a, "all"); err != nil {
					t.Fatal("reentrant achievement before announcement completion rejected", err)
				}
				register("all", nil)
				failure := productionSyncReceive(t, a, func(e *rtiv1.FederateEvent) bool { return e.GetSyncRegistrationFailed() != nil })
				productionSyncReport(t, conn, fed, a, failure, true)
				productionSyncReport(t, conn, fed, a, failure, false)
				c := productionSyncJoin(ctx, t, conn, fed, "late", created.FederationGeneration, batches)
				productionSyncReceive(t, c, func(e *rtiv1.FederateEvent) bool { return e.GetSyncAnnounced() != nil })
				productionSyncReport(t, conn, fed, b, announcedB, true)
				if err := achieve(b, "all"); err != nil {
					t.Fatal(err)
				}
				if manager.QueryState(core.FederationName(fed), "all") != syncpkg.StateAnnounced {
					t.Fatal("late all-mode member did not delay completion")
				}
				if err := achieve(c, "all"); err == nil {
					t.Fatal("late never-entered member achieved")
				}
				_, err = federation.ResignFederation(c.ctx, &rtiv1.ResignFederationRequest{WireVersion: 1, FederationName: fed, FederateHandle: c.h, Action: rtiv1.ResignAction_RESIGN_ACTION_NO_ACTION})
				if err != nil {
					t.Fatal("resign hook", err)
				}
				terminalA := productionSyncReceive(t, a, func(e *rtiv1.FederateEvent) bool { return e.GetSyncSynchronized() != nil })
				terminalB := productionSyncReceive(t, b, func(e *rtiv1.FederateEvent) bool { return e.GetSyncSynchronized() != nil })
				productionSyncReport(t, conn, fed, a, terminalA, true)
				if manager.QueryState(core.FederationName(fed), "all") != syncpkg.StateAchieved {
					t.Fatal("terminal entry/send retired label")
				}
				_, err = rtiv1.NewSupportServiceClient(conn).ReportCallbackInvocation(b.ctx, &rtiv1.ReportCallbackInvocationRequest{WireVersion: 1, FederationName: fed, FederateHandle: b.h, CallbackReceipt: terminalB.CallbackReceipt, Success: true})
				if err == nil {
					t.Fatal("mandatory completion before entry accepted")
				}
				productionSyncReport(t, conn, fed, b, terminalB, true)
				productionSyncReport(t, conn, fed, b, terminalB, false)
				if manager.QueryState(core.FederationName(fed), "all") != syncpkg.StateAchieved {
					t.Fatal("one completion retired required fanout")
				}
				productionSyncReport(t, conn, fed, a, terminalA, false)
				if manager.QueryState(core.FederationName(fed), "all") != syncpkg.StateUnknown {
					t.Fatal("completed callback fanout did not retire label")
				}
				register("all", nil)
				productionSyncReport(t, conn, fed, a, terminalA, false)
				if manager.QueryState(core.FederationName(fed), "all") != syncpkg.StateAnnounced {
					t.Fatal("stale ticket retired reused label")
				}
				productionSyncReport(t, conn, fed, a, announcedA, false)
				productionSyncReport(t, conn, fed, b, announcedB, false)
				register("subset", []uint64{a.h})
				d := productionSyncJoin(ctx, t, conn, fed, "outside-subset", created.FederationGeneration, batches)
				if err := achieve(d, "subset"); err == nil {
					t.Fatal("member outside explicit subset achieved")
				}
				for _, point := range manager.Snapshot(core.FederationName(fed)) {
					if point.Label == "subset" && (len(point.RequiredHandles) != 1 || uint64(point.RequiredHandles[0]) != a.h) {
						t.Fatal("explicit subset expanded on join")
					}
				}
				_, err = client.SynchronizationPointAchieved(a.ctx, &rtiv1.AchieveSyncPointRequest{WireVersion: 1, FederationName: fed, FederateHandle: ^uint64(0) - 1, Label: "all"})
				if err == nil {
					t.Fatal("nonmember was accepted")
				}
			})
		}
	}
}
