package server

import (
	"bytes"
	"context"
	"testing"
	"time"

	grpcadapter "github.com/cbchoi/gorti/rti/internal/adapters/grpc"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	savepoint "github.com/cbchoi/gorti/rti/internal/hla/save_restore"
	syncdomain "github.com/cbchoi/gorti/rti/internal/hla/synchronization"
	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

func TestApplicableFM13To21SynchronizationRecipientsAndMembership(t *testing.T) {
	srv, conn, _ := integerProductionServer(t, t.TempDir(), 42)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	const fed = "fm-sync-conditions"
	generation := momCompletionReviewCreate(ctx, t, conn, fed, "Item")
	var members []uint64
	channels := map[uint64]<-chan []core.OutboundEvent{}
	for _, name := range []string{"one", "two"} {
		joined := momCompletionReviewJoin(ctx, t, conn, fed, name, "", generation)
		h := joined.FederateHandle
		members = append(members, h)
		channel, release, err := srv.outbox.Subscribe(ctx, fed, core.FederateHandle(h))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = release() })
		channels[h] = channel
	}
	syncs := rtiv1.NewSyncServiceClient(conn)
	for _, bad := range []struct {
		name   string
		handle uint64
	}{{"absent-execution", members[0]}, {fed, members[1] + 100000}} {
		var md metadata.MD
		_, err := syncs.SynchronizationPointAchieved(ctx, &rtiv1.AchieveSyncPointRequest{WireVersion: 1, FederationName: bad.name, FederateHandle: bad.handle, Label: testReadyLabel}, grpcgo.Trailer(&md))
		if err == nil {
			t.Fatal("nonmember/absent execution accepted")
		}
		if bad.name == fed {
			got := md.Get("rti-spec-exception")
			if len(got) != 1 || got[0] != testNotExecutionMember {
				t.Fatalf("nonmember error=%v trailers=%v", err, md)
			}
		}
	}
	tag := []byte{0, 1, 255}
	if _, err := syncs.RegisterFederationSynchronizationPoint(ctx, &rtiv1.RegisterSyncPointRequest{WireVersion: 1, FederationName: fed, FederateHandle: members[0], Label: testReadyLabel, Tag: tag}); err != nil {
		t.Fatal(err)
	}
	for h, channel := range channels {
		announced := 0
		for _, event := range drainScheduledEvents(channel) {
			wire, err := grpcadapter.EncodeOutboundEvent(event)
			if err != nil {
				t.Fatal(err)
			}
			if wire.GetSyncSynchronized() != nil {
				t.Fatal("terminal callback before achievements")
			}
			if announcement := wire.GetSyncAnnounced(); announcement != nil {
				if announcement.Label != testReadyLabel || !bytes.Equal(announcement.Tag, tag) {
					t.Fatalf("announcement=%v", announcement)
				}
				if err := srv.fedMgr.ValidateMember(fed, core.FederateHandle(h)); err != nil {
					t.Fatal(err)
				}
				if err := srv.syncMgr.(*syncdomain.Manager).ObserveSyncCallbackEntry(ctx, fed, core.FederateHandle(h), core.SyncCallbackAnnounced, testReadyLabel, wire.Seq); err != nil {
					t.Fatal(err)
				}
				announced++
			}
		}
		if announced != 1 {
			t.Fatalf("recipient %d announcement count=%d", h, announced)
		}
	}
	for i, h := range members {
		if _, err := syncs.SynchronizationPointAchieved(ctx, &rtiv1.AchieveSyncPointRequest{WireVersion: 1, FederationName: fed, FederateHandle: h, Label: testReadyLabel}); err != nil {
			t.Fatal(err)
		}
		for recipient, channel := range channels {
			terminal := 0
			for _, event := range drainScheduledEvents(channel) {
				wire, err := grpcadapter.EncodeOutboundEvent(event)
				if err != nil {
					t.Fatal(err)
				}
				if completed := wire.GetSyncSynchronized(); completed != nil {
					if completed.Label != testReadyLabel || len(completed.FailedToSync) != 0 {
						t.Fatalf("terminal=%v", completed)
					}
					if err := srv.fedMgr.ValidateMember(fed, core.FederateHandle(recipient)); err != nil {
						t.Fatal(err)
					}
					terminal++
				}
			}
			if terminal != i {
				t.Fatalf("after achievement %d recipient %d terminal count=%d", i, recipient, terminal)
			}
		}
	}
}

func TestApplicableFM13To21SaveLifecycleAndRestoreExclusion(t *testing.T) {
	srv, conn, _ := integerProductionServer(t, t.TempDir(), 41)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	const fed = "fm-save-conditions"
	generation := momCompletionReviewCreate(ctx, t, conn, fed, "Item")
	joined := momCompletionReviewJoin(ctx, t, conn, fed, "member", "", generation)
	h := joined.FederateHandle
	if err := srv.fedMgr.ValidateMember(core.FederationName(fed), core.FederateHandle(h)); err != nil {
		t.Fatal(err)
	}
	channel, release, err := srv.outbox.Subscribe(ctx, fed, core.FederateHandle(h))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = release() }()
	saves, syncs := rtiv1.NewSavepointServiceClient(conn), rtiv1.NewSyncServiceClient(conn)
	response := &rtiv1.FederateSaveResponseRequest{WireVersion: 1, FederationName: fed, FederateHandle: h}
	request := func(label string) error {
		_, err := saves.RequestFederationSave(ctx, &rtiv1.RequestFederationSaveRequest{WireVersion: 1, FederationName: fed, FederateHandle: h, Label: label})
		return err
	}
	wantException := func(t *testing.T, want string, invoke func(*metadata.MD) error) {
		t.Helper()
		var trailers metadata.MD
		err := invoke(&trailers)
		got := trailers.Get("rti-spec-exception")
		if err == nil || len(got) != 1 || got[0] != want {
			t.Fatalf("want %s, got %v trailers=%v", want, err, trailers)
		}
	}
	wantException(t, "SaveNotInitiated", func(md *metadata.MD) error {
		_, err := saves.FederateSaveBegun(ctx, response, grpcgo.Trailer(md))
		return err
	})
	wantException(t, "SaveNotInProgress", func(md *metadata.MD) error {
		_, err := saves.AbortFederationSave(ctx, &rtiv1.AbortFederationSaveRequest{WireVersion: 1, FederationName: fed, FederateHandle: &h}, grpcgo.Trailer(md))
		return err
	})
	if err := request(testSavedLabel); err != nil {
		t.Fatal(err)
	}
	initiated := 0
	for _, event := range drainScheduledEvents(channel) {
		wire, err := grpcadapter.EncodeOutboundEvent(event)
		if err != nil {
			t.Fatal(err)
		}
		if save := wire.GetSaveInitiate(); save != nil {
			if save.Label != testSavedLabel || save.SaveTime != nil {
				t.Fatalf("initiate=%v", save)
			}
			initiated++
		}
	}
	if initiated != 1 {
		t.Fatalf("initiation count=%d", initiated)
	}
	wantException(t, "SaveInProgress", func(md *metadata.MD) error {
		_, err := syncs.SynchronizationPointAchieved(ctx, &rtiv1.AchieveSyncPointRequest{WireVersion: 1, FederationName: fed, FederateHandle: h, Label: "blocked"}, grpcgo.Trailer(md))
		return err
	})
	wantException(t, "SaveInProgress", func(md *metadata.MD) error {
		_, err := syncs.RegisterFederationSynchronizationPoint(ctx, &rtiv1.RegisterSyncPointRequest{WireVersion: 1, FederationName: fed, FederateHandle: h, Label: "blocked"}, grpcgo.Trailer(md))
		return err
	})
	if len(drainScheduledEvents(channel)) != 0 {
		t.Fatal("rejected synchronization requests produced callbacks during save")
	}
	wantException(t, "FederateHasNotBegunSave", func(md *metadata.MD) error {
		_, err := saves.FederateSaveComplete(ctx, response, grpcgo.Trailer(md))
		return err
	})
	if _, err := saves.FederateSaveBegun(ctx, response); err != nil {
		t.Fatal(err)
	}
	progress, err := saves.QuerySaveState(ctx, &rtiv1.QuerySaveStateRequest{WireVersion: 1, FederationName: fed})
	if err != nil || len(progress.GetFederateStatuses()) != 1 || progress.FederateStatuses[0].Status != rtiv1.FederateSaveProgress_FEDERATE_SAVE_PROGRESS_SAVING {
		t.Fatalf("begun progress=%v err=%v", progress, err)
	}
	if _, err := saves.FederateSaveComplete(ctx, response); err != nil {
		t.Fatal(err)
	}
	saved := 0
	for _, event := range drainScheduledEvents(channel) {
		wire, err := grpcadapter.EncodeOutboundEvent(event)
		if err != nil {
			t.Fatal(err)
		}
		if completed := wire.GetSaveCompleted(); completed != nil {
			if completed.Label != testSavedLabel {
				t.Fatalf("completed=%v", completed)
			}
			saved++
		}
	}
	if saved != 1 {
		t.Fatalf("saved callbacks=%d", saved)
	}
	if _, err := saves.RequestFederationRestore(ctx, &rtiv1.RequestFederationRestoreRequest{WireVersion: 1, FederationName: fed, FederateHandle: h, Label: testSavedLabel}); err != nil {
		t.Fatal(err)
	}
	for name, invoke := range map[string]func(*metadata.MD) error{
		"request": func(md *metadata.MD) error {
			_, err := saves.RequestFederationSave(ctx, &rtiv1.RequestFederationSaveRequest{WireVersion: 1, FederationName: fed, FederateHandle: h, Label: "blocked"}, grpcgo.Trailer(md))
			return err
		},
		"begun": func(md *metadata.MD) error {
			_, err := saves.FederateSaveBegun(ctx, response, grpcgo.Trailer(md))
			return err
		},
		"complete": func(md *metadata.MD) error {
			_, err := saves.FederateSaveComplete(ctx, response, grpcgo.Trailer(md))
			return err
		},
		"not-complete": func(md *metadata.MD) error {
			_, err := saves.FederateSaveNotComplete(ctx, response, grpcgo.Trailer(md))
			return err
		},
		"achieve": func(md *metadata.MD) error {
			_, err := syncs.SynchronizationPointAchieved(ctx, &rtiv1.AchieveSyncPointRequest{WireVersion: 1, FederationName: fed, FederateHandle: h, Label: "blocked"}, grpcgo.Trailer(md))
			return err
		},
		"register": func(md *metadata.MD) error {
			_, err := syncs.RegisterFederationSynchronizationPoint(ctx, &rtiv1.RegisterSyncPointRequest{WireVersion: 1, FederationName: fed, FederateHandle: h, Label: "blocked"}, grpcgo.Trailer(md))
			return err
		},
	} {
		t.Run("restore-rejects-"+name, func(t *testing.T) { wantException(t, "RestoreInProgress", invoke) })
	}
	for _, event := range drainScheduledEvents(channel) {
		wire, err := grpcadapter.EncodeOutboundEvent(event)
		if err != nil {
			t.Fatal(err)
		}
		if wire.GetSaveInitiate() != nil || wire.GetSaveCompleted() != nil || wire.GetSyncAnnounced() != nil || wire.GetSyncSynchronized() != nil {
			t.Fatalf("forbidden callback while restoring: %v", wire)
		}
	}
	if _, err := saves.AbortFederationRestore(ctx, &rtiv1.AbortFederationRestoreRequest{WireVersion: 1, FederationName: fed, FederateHandle: &h}); err != nil {
		t.Fatal(err)
	}
	if err := request("abort-me"); err != nil {
		t.Fatal(err)
	}
	if _, err := saves.AbortFederationSave(ctx, &rtiv1.AbortFederationSaveRequest{WireVersion: 1, FederationName: fed, FederateHandle: &h}); err != nil {
		t.Fatal(err)
	}
	if got := srv.saveMgr.QuerySaveState(fed, "abort-me"); got != savepoint.StateNotSaved {
		t.Fatalf("abort state=%v", got)
	}
}
