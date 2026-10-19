package server

import (
	"context"
	grpcadapter "github.com/cbchoi/gorti/rti/internal/adapters/grpc"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	savepoint "github.com/cbchoi/gorti/rti/internal/hla/save_restore"
	hlatime "github.com/cbchoi/gorti/rti/internal/hla/time"
	stdgrpc "google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

func scheduledServer(t *testing.T) (*rtid, *stdgrpc.ClientConn, []core.FederateHandle) {
	t.Helper()
	srv, err := newRTID(rtidConfig{SaveDir: t.TempDir(), OutboxBatchSize: 1, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	listener := bufconn.Listen(1 << 20)
	done := make(chan error, 1)
	go func() { done <- srv.grpcS.Serve(listener) }()
	t.Cleanup(func() { srv.grpcS.Stop(); _ = listener.Close(); <-done; _ = srv.plugins.Close() })
	conn, err := stdgrpc.NewClient("passthrough:///scheduled-checkpoint", stdgrpc.WithTransportCredentials(insecure.NewCredentials()), stdgrpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	ctx := context.Background()
	if _, err := rtiv1.NewFederationServiceClient(conn).CreateFederation(ctx, &rtiv1.CreateFederationRequest{WireVersion: rtiv1.WireVersion_WIRE_VERSION_V1, FederationName: "scheduled", Mode: rtiv1.Mode_MODE_VERBOSE}); err != nil {
		t.Fatal(err)
	}
	var members []core.FederateHandle
	for _, name := range []string{"first", "second"} {
		h, err := srv.fedMgr.JoinFederation(ctx, core.JoinFederationRequest{Federation: "scheduled", FederateName: name})
		if err != nil {
			t.Fatal(err)
		}
		members = append(members, h)
		if err := srv.timeMgr.EnableRegulation(ctx, "scheduled", h, 1); err != nil {
			t.Fatal(err)
		}
		if err := srv.timeMgr.EnableConstrained(ctx, "scheduled", h); err != nil {
			t.Fatal(err)
		}
	}
	return srv, conn, members
}

func TestScheduledCheckpointProductionRPCProgressAndCompletion(t *testing.T) {
	srv, conn, members := scheduledServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	saves, times := rtiv1.NewSavepointServiceClient(conn), rtiv1.NewTimeServiceClient(conn)
	channels := make(map[core.FederateHandle]<-chan []core.OutboundEvent)
	for _, h := range members {
		channel, release, err := srv.outbox.Subscribe(ctx, "scheduled", h)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = release() })
		channels[h] = channel
		awaitScheduledTimeEnabled(ctx, t, channel, 1)
	}
	target := float64(10)
	_, err := saves.RequestFederationSave(ctx, &rtiv1.RequestFederationSaveRequest{WireVersion: rtiv1.WireVersion_WIRE_VERSION_V1, FederationName: "scheduled", FederateHandle: uint64(members[0]), Label: "deferred", SaveTime: &target})
	if err != nil {
		t.Fatal(err)
	}
	for _, channel := range channels {
		if len(drainScheduledEvents(channel)) != 0 {
			t.Fatal("callbacks before scheduled time progression")
		}
	}
	mgr := srv.saveMgr.(*savepoint.Manager)
	_, statuses, err := mgr.QueryFederationSaveStatus("scheduled")
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range statuses {
		if state.Status != savepoint.NoSaveInProgress {
			t.Fatalf("early initiation: %+v", statuses)
		}
	}
	for i, h := range members {
		_, err := times.TimeAdvanceRequest(ctx, &rtiv1.TARRequest{WireVersion: rtiv1.WireVersion_WIRE_VERSION_V1, FederationName: "scheduled", FederateHandle: uint64(h), LogicalTime: target})
		if err != nil {
			t.Fatal(err)
		}
		_, statuses, err = mgr.QueryFederationSaveStatus("scheduled")
		if err != nil {
			t.Fatal(err)
		}
		want := savepoint.NoSaveInProgress
		if i == len(members)-1 {
			want = savepoint.FederateInstructedToSave
		}
		for _, state := range statuses {
			if state.Status != want {
				t.Fatalf("after peer %d: %+v, want %v", i, statuses, want)
			}
		}
	}
	for _, channel := range channels {
		events := drainScheduledEvents(channel)
		grant, initiation := -1, -1
		for i, evt := range events {
			wire, err := grpcadapter.EncodeOutboundEvent(evt)
			if err != nil {
				t.Fatal(err)
			}
			if wire.GetGrant() != nil && wire.GetGrant().LogicalTime == 10 {
				grant = i
			}
			if save := wire.GetSaveInitiate(); save != nil {
				if initiation != -1 || save.SaveTime == nil || *save.SaveTime != 10 {
					t.Fatal("invalid/duplicate scheduled callback")
				}
				initiation = i
			}
		}
		if grant >= 0 || initiation < 0 {
			t.Fatalf("scheduled save must precede the strict boundary grant: grant=%d initiate=%d", grant, initiation)
		}
	}
	for _, h := range members {
		req := &rtiv1.FederateSaveResponseRequest{WireVersion: rtiv1.WireVersion_WIRE_VERSION_V1, FederationName: "scheduled", FederateHandle: uint64(h)}
		if _, err := saves.FederateSaveBegun(ctx, req); err != nil {
			t.Fatal(err)
		}
		if _, err := saves.FederateSaveComplete(ctx, req); err != nil {
			t.Fatal(err)
		}
	}
	if mgr.QuerySaveState("scheduled", "deferred") != savepoint.StateSaved {
		t.Fatal("real production snapshots were not saved")
	}
	for _, channel := range channels {
		saved, grant := -1, -1
		for i, event := range drainScheduledEvents(channel) {
			wire, err := grpcadapter.EncodeOutboundEvent(event)
			if err != nil {
				t.Fatal(err)
			}
			if wire.GetSaveCompleted() != nil {
				saved = i
			}
			if tag := wire.GetGrant(); tag != nil {
				if grant != -1 || tag.LogicalTime != 10 {
					t.Fatalf("invalid/duplicate resumed strict grant: %+v", tag)
				}
				grant = i
			}
		}
		if saved < 0 || grant <= saved {
			t.Fatalf("strict boundary grant did not follow save completion: saved=%d grant=%d", saved, grant)
		}
	}
	for _, h := range members {
		if _, err := times.TimeAdvanceRequest(ctx, &rtiv1.TARRequest{WireVersion: rtiv1.WireVersion_WIRE_VERSION_V1, FederationName: "scheduled", FederateHandle: uint64(h), LogicalTime: 20}); err != nil {
			t.Fatal(err)
		}
	}
}

func drainScheduledEvents(channel <-chan []core.OutboundEvent) []core.OutboundEvent {
	var result []core.OutboundEvent
	for {
		select {
		case batch, ok := <-channel:
			if !ok {
				return result
			}
			result = append(result, batch...)
		default:
			return result
		}
	}
}

func awaitScheduledTimeEnabled(ctx context.Context, t *testing.T, channel <-chan []core.OutboundEvent, regulationCallbacks int) {
	t.Helper()
	regulation, constrained := 0, 0
	for regulation < regulationCallbacks || constrained < 1 {
		select {
		case batch, ok := <-channel:
			if !ok {
				t.Fatal("callback stream closed before time enable completion")
			}
			for _, event := range batch {
				wire, err := grpcadapter.EncodeOutboundEvent(event)
				if err != nil {
					t.Fatal(err)
				}
				switch {
				case wire.GetTimeRegulationEnabled() != nil:
					enabled := wire.GetTimeRegulationEnabled()
					regulation++
					if enabled.LogicalTime != 0 || regulation > regulationCallbacks {
						t.Fatalf("unexpected regulation callback: %+v count=%d", enabled, regulation)
					}
				case wire.GetTimeConstrainedEnabled() != nil:
					enabled := wire.GetTimeConstrainedEnabled()
					constrained++
					if enabled.LogicalTime != 0 || constrained > 1 {
						t.Fatalf("unexpected constraint callback: %+v count=%d", enabled, constrained)
					}
				default:
					t.Fatalf("unexpected callback during time enable: %T", event)
				}
			}
		case <-ctx.Done():
			t.Fatalf("time enable callbacks incomplete: regulation=%d constrained=%d: %v", regulation, constrained, ctx.Err())
		}
	}
}

func TestScheduledSaveCrossingRequestsResumeProduction(t *testing.T) {
	for _, scenario := range []struct {
		name             string
		zero, decreasing bool
	}{{name: "positive"}, {name: "zero", zero: true}, {name: "decreasing-to-zero", decreasing: true}} {
		t.Run(scenario.name, func(t *testing.T) {
			srv, conn, members := scheduledServer(t)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			channels := make(map[core.FederateHandle]<-chan []core.OutboundEvent)
			for _, h := range members {
				if scenario.zero {
					if err := srv.timeMgr.DisableRegulation(ctx, "scheduled", h); err != nil {
						t.Fatal(err)
					}
					if err := srv.timeMgr.EnableRegulation(ctx, "scheduled", h, 0); err != nil {
						t.Fatal(err)
					}
				}
				if scenario.decreasing {
					if err := srv.timeMgr.ModifyLookahead(ctx, "scheduled", h, 0); err != nil {
						t.Fatal(err)
					}
				}
				ch, release, err := srv.outbox.Subscribe(ctx, "scheduled", h)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = release() })
				channels[h] = ch
				regulationCallbacks := 1
				if scenario.zero {
					regulationCallbacks++
				}
				awaitScheduledTimeEnabled(ctx, t, ch, regulationCallbacks)
			}
			saves, times := rtiv1.NewSavepointServiceClient(conn), rtiv1.NewTimeServiceClient(conn)
			target := float64(10)
			if _, err := saves.RequestFederationSave(ctx, &rtiv1.RequestFederationSaveRequest{WireVersion: rtiv1.WireVersion_WIRE_VERSION_V1, FederationName: "scheduled", FederateHandle: uint64(members[0]), Label: "crossing", SaveTime: &target}); err != nil {
				t.Fatal(err)
			}
			for _, h := range members {
				if _, err := times.TimeAdvanceRequestAvailable(ctx, &rtiv1.TARARequest{WireVersion: rtiv1.WireVersion_WIRE_VERSION_V1, FederationName: "scheduled", FederateHandle: uint64(h), LogicalTime: 20}); err != nil {
					t.Fatalf("crossing request rejected: %v", err)
				}
			}
			for h, ch := range channels {
				initiated := 0
				for _, evt := range drainScheduledEvents(ch) {
					wire, err := grpcadapter.EncodeOutboundEvent(evt)
					if err != nil {
						t.Fatal(err)
					}
					if wire.GetGrant() != nil {
						t.Fatalf("early TAG for %d", h)
					}
					if wire.GetSaveInitiate() != nil {
						initiated++
					}
				}
				if initiated != 1 {
					t.Fatalf("initiation count %d for %d", initiated, h)
				}
			}
			for _, h := range members {
				req := &rtiv1.FederateSaveResponseRequest{WireVersion: rtiv1.WireVersion_WIRE_VERSION_V1, FederationName: "scheduled", FederateHandle: uint64(h)}
				if _, err := saves.FederateSaveBegun(ctx, req); err != nil {
					t.Fatal(err)
				}
				if _, err := saves.FederateSaveComplete(ctx, req); err != nil {
					t.Fatal(err)
				}
			}
			for h, ch := range channels {
				saved, grants := false, 0
				for _, evt := range drainScheduledEvents(ch) {
					wire, err := grpcadapter.EncodeOutboundEvent(evt)
					if err != nil {
						t.Fatal(err)
					}
					if wire.GetSaveCompleted() != nil {
						saved = true
					}
					if grant := wire.GetGrant(); grant != nil {
						if !saved || grant.LogicalTime != 20 {
							t.Fatalf("wrong resumed order/time for %d: saved=%v time=%v", h, saved, grant.LogicalTime)
						}
						grants++
					}
				}
				if grants != 1 {
					t.Fatalf("original request did not resume for %d: %d grants", h, grants)
				}
				if scenario.zero || scenario.decreasing {
					if actual := srv.timeMgr.(*hlatime.Manager).Metrics("scheduled", h).Lookahead; actual == nil || *actual != 0 {
						t.Fatalf("lookahead reduction did not complete for %d", h)
					}
				}
			}
			// The checkpoint itself contains the still-pending original request.
			// Restoring it must resume after the terminal identity/epoch callback.
			if _, err := saves.RequestFederationRestore(ctx, &rtiv1.RequestFederationRestoreRequest{WireVersion: rtiv1.WireVersion_WIRE_VERSION_V1, FederationName: "scheduled", FederateHandle: uint64(members[0]), Label: "crossing"}); err != nil {
				t.Fatal(err)
			}
			for _, ch := range channels {
				drainScheduledEvents(ch)
			}
			for _, h := range members {
				if _, err := saves.FederateRestoreComplete(ctx, &rtiv1.FederateRestoreResponseRequest{WireVersion: rtiv1.WireVersion_WIRE_VERSION_V1, FederationName: "scheduled", FederateHandle: uint64(h)}); err != nil {
					t.Fatal(err)
				}
			}
			for h, ch := range channels {
				restored, grants := false, 0
				for _, evt := range drainScheduledEvents(ch) {
					wire, err := grpcadapter.EncodeOutboundEvent(evt)
					if err != nil {
						t.Fatal(err)
					}
					if wire.GetRestoreCompleted() != nil {
						restored = true
					}
					if grant := wire.GetGrant(); grant != nil {
						if !restored || grant.LogicalTime != 20 {
							t.Fatalf("restore resumed before terminal/wrong time for %d", h)
						}
						grants++
					}
				}
				if grants != 1 {
					t.Fatalf("restored pending request did not resume for %d", h)
				}
				if scenario.zero || scenario.decreasing {
					if actual := srv.timeMgr.(*hlatime.Manager).Metrics("scheduled", h).Lookahead; actual == nil || *actual != 0 {
						t.Fatalf("restored lookahead target was lost for %d", h)
					}
				}
			}
		})
	}
}

func TestScheduledCheckpointProductionRPCResignCancels(t *testing.T) {
	srv, conn, members := scheduledServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	target := float64(10)
	saves := rtiv1.NewSavepointServiceClient(conn)
	if _, err := saves.RequestFederationSave(ctx, &rtiv1.RequestFederationSaveRequest{WireVersion: rtiv1.WireVersion_WIRE_VERSION_V1, FederationName: "scheduled", FederateHandle: uint64(members[0]), Label: "resign", SaveTime: &target}); err != nil {
		t.Fatal(err)
	}
	_, err := rtiv1.NewFederationServiceClient(conn).ResignFederation(ctx, &rtiv1.ResignFederationRequest{WireVersion: rtiv1.WireVersion_WIRE_VERSION_V1, FederationName: "scheduled", FederateHandle: uint64(members[1]), Action: rtiv1.ResignAction_RESIGN_ACTION_NO_ACTION})
	if err != nil {
		t.Fatal(err)
	}
	if srv.saveMgr.QuerySaveState("scheduled", "resign") != savepoint.StateNotSaved {
		t.Fatal("RPC lifecycle release did not cancel stale roster")
	}
	if _, err := rtiv1.NewTimeServiceClient(conn).TimeAdvanceRequest(ctx, &rtiv1.TARRequest{WireVersion: rtiv1.WireVersion_WIRE_VERSION_V1, FederationName: "scheduled", FederateHandle: uint64(members[0]), LogicalTime: 20}); err != nil {
		t.Fatalf("cancelled ceiling remains: %v", err)
	}
}

func TestScheduledCheckpointProductionCallbackFailureReleasesCeiling(t *testing.T) {
	srv, conn, members := scheduledServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	channels := make(map[core.FederateHandle]<-chan []core.OutboundEvent)
	for _, h := range members {
		channel, release, err := srv.outbox.Subscribe(ctx, "scheduled", h)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = release() })
		channels[h] = channel
		awaitScheduledTimeEnabled(ctx, t, channel, 1)
	}
	saves, times := rtiv1.NewSavepointServiceClient(conn), rtiv1.NewTimeServiceClient(conn)
	target := float64(10)
	if _, err := saves.RequestFederationSave(ctx, &rtiv1.RequestFederationSaveRequest{WireVersion: rtiv1.WireVersion_WIRE_VERSION_V1, FederationName: "scheduled", FederateHandle: uint64(members[0]), Label: "callback-failure", SaveTime: &target}); err != nil {
		t.Fatal(err)
	}
	// A strict boundary grant follows the save, so initiation is the first
	// callback to admit. Fill that recipient's queue completely.
	for i := 0; i < cap(channels[members[0]]); i++ {
		if err := srv.outbox.Send(ctx, "scheduled", members[0], &fakeOutboundEvent{}); err != nil {
			t.Fatal(err)
		}
	}
	for _, h := range members {
		if _, err := times.TimeAdvanceRequest(ctx, &rtiv1.TARRequest{WireVersion: rtiv1.WireVersion_WIRE_VERSION_V1, FederationName: "scheduled", FederateHandle: uint64(h), LogicalTime: 10}); err != nil {
			t.Fatal(err)
		}
	}
	mgr := srv.saveMgr.(*savepoint.Manager)
	if mgr.QuerySaveState("scheduled", "callback-failure") != savepoint.StateNotSaved {
		t.Fatal("callback overflow did not cancel the checkpoint")
	}
	failed := false
	for _, channel := range channels {
		for _, event := range drainScheduledEvents(channel) {
			if _, filler := event.(*fakeOutboundEvent); filler {
				continue
			}
			wire, err := grpcadapter.EncodeOutboundEvent(event)
			if err != nil {
				t.Fatal(err)
			}
			if wire.GetSaveInitiate() != nil {
				t.Fatal("initiation delivered after first recipient admission failed")
			}
			if outcome := wire.GetSaveFailed(); outcome != nil {
				failed = failed || outcome.Reason != nil && outcome.GetReason() == int32(savepoint.RTIUnableToSave)
			}
		}
	}
	if !failed {
		t.Fatal("reachable participant did not receive failure callback")
	}
	if err := mgr.ProgressScheduledSave(ctx, "scheduled"); err != nil {
		t.Fatalf("pending boundary grants did not resume after queue recovery: %v", err)
	}
	if mgr.HasPendingScheduledSave("scheduled") {
		t.Fatal("queue recovery retained checkpoint progression work")
	}
	for _, h := range members {
		if got := srv.timeMgr.(*hlatime.Manager).Metrics("scheduled", h); got.Advancing || got.CurrentTime != 10 {
			t.Fatalf("failed save retained the accepted boundary advance for %d: %+v", h, got)
		}
	}
	for _, h := range members {
		if _, err := times.TimeAdvanceRequest(ctx, &rtiv1.TARRequest{WireVersion: rtiv1.WireVersion_WIRE_VERSION_V1, FederationName: "scheduled", FederateHandle: uint64(h), LogicalTime: 20}); err != nil {
			t.Fatalf("failed save retained ceiling: %v", err)
		}
	}
}
