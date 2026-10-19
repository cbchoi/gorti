package server

import (
	"context"
	"errors"
	"testing"
	"time"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestRestoreTransitionProductionChangedHandles(t *testing.T) {
	srv, conn, saved := scheduledServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := rtiv1.NewSavepointServiceClient(conn)
	const fed = core.FederationName("scheduled")
	sourceChannels := make(map[core.FederateHandle]<-chan []core.OutboundEvent)
	for _, h := range saved {
		channel, release, err := srv.outbox.Subscribe(ctx, fed, h)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = release() })
		sourceChannels[h] = channel
		for _, event := range drainScheduledEvents(channel) {
			invokeProductionRestoreFixtureEvent(ctx, t, conn, srv, fed, h, event)
		}
	}
	if _, err := client.RequestFederationSave(ctx, &rtiv1.RequestFederationSaveRequest{WireVersion: rtiv1.WireVersion_WIRE_VERSION_V1, FederationName: string(fed), FederateHandle: uint64(saved[0]), Label: "identity"}); err != nil {
		t.Fatal(err)
	}
	for _, h := range saved {
		for _, event := range drainScheduledEvents(sourceChannels[h]) {
			invokeProductionRestoreFixtureEvent(ctx, t, conn, srv, fed, h, event)
		}
		req := &rtiv1.FederateSaveResponseRequest{WireVersion: rtiv1.WireVersion_WIRE_VERSION_V1, FederationName: string(fed), FederateHandle: uint64(h)}
		if _, err := client.FederateSaveBegun(ctx, req); err != nil {
			t.Fatal(err)
		}
		if _, err := client.FederateSaveComplete(ctx, req); err != nil {
			t.Fatal(err)
		}
	}
	for _, h := range saved {
		if err := srv.fedMgr.ResignFederation(ctx, fed, h, core.ResignActionNoAction); err != nil {
			t.Fatal(err)
		}
	}
	current := make([]core.FederateHandle, 2)
	channels := make([]<-chan []core.OutboundEvent, 2)
	cancels := make([]func() error, 2)
	for i, name := range []string{"first", "second"} {
		h, err := srv.fedMgr.JoinFederation(ctx, core.JoinFederationRequest{Federation: fed, FederateName: name})
		if err != nil {
			t.Fatal(err)
		}
		current[i] = h
		channels[i], cancels[i], err = srv.outbox.Subscribe(ctx, fed, h)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = cancels[i]() })
	}
	if _, err := client.RequestFederationRestore(ctx, &rtiv1.RequestFederationRestoreRequest{WireVersion: rtiv1.WireVersion_WIRE_VERSION_V1, FederationName: string(fed), FederateHandle: uint64(current[0]), Label: "identity"}); err != nil {
		t.Fatalf("same-generation name rebinding rejected: %v", err)
	}
	for i, ch := range channels {
		found := false
		for _, event := range drainScheduledEvents(ch) {
			wire := invokeProductionRestoreFixtureEvent(ctx, t, conn, srv, fed, current[i], event)
			if init := wire.GetRestoreInitiate(); init != nil && wire.GetCallbackRecovery() == nil {
				found = core.FederateHandle(init.FederateHandle) == saved[i]
			}
		}
		if !found {
			t.Fatalf("saved identity not announced for %d", current[i])
		}
		// This belongs to the pre-restore callback epoch and must be purged.
		if err := srv.outbox.Send(ctx, fed, current[i], &fakeOutboundEvent{}); err != nil {
			t.Fatal(err)
		}
	}
	for _, h := range current {
		if _, err := client.FederateRestoreComplete(ctx, &rtiv1.FederateRestoreResponseRequest{WireVersion: rtiv1.WireVersion_WIRE_VERSION_V1, FederationName: string(fed), FederateHandle: uint64(h)}); err != nil {
			t.Fatal(err)
		}
	}
	for i, ch := range channels {
		events := drainScheduledEvents(ch)
		var restored *rtiv1.FederationRestored
		for _, event := range events {
			wire := invokeProductionRestoreFixtureEvent(ctx, t, conn, srv, fed, saved[i], event)
			if recovery := wire.GetCallbackRecovery(); recovery != nil && recovery.Completed {
				continue
			}
			if wire.GetRestoreCompleted() == nil || restored != nil {
				t.Fatalf("old epoch callbacks survived: %+v", events)
			}
			restored = wire.GetRestoreCompleted()
		}
		if restored == nil || core.FederateHandle(restored.GetFederateHandle()) != saved[i] || restored.GetCallbackEpoch() != 1 {
			t.Fatalf("terminal identity/epoch: %+v", restored)
		}
		if err := srv.fedMgr.ValidateMember(fed, saved[i]); err != nil {
			t.Fatal(err)
		}
		if modules, err := srv.fedMgr.JoinedModules(ctx, fed, saved[i]); err != nil || len(modules) != 0 {
			t.Fatalf("saved empty join-module baseline not restored: %+v %v", modules, err)
		}
		if !errors.Is(srv.fedMgr.ValidateMember(fed, current[i]), core.ErrFederateNotJoined) {
			t.Fatal("old handle remains joined")
		}
		if err := srv.outbox.Send(ctx, fed, saved[i], &fakeOutboundEvent{}); err != nil {
			t.Fatal(err)
		}
		if len(drainScheduledEvents(ch)) != 1 {
			t.Fatal("rekey detached the live stream")
		}
		if err := cancels[i](); err != nil {
			t.Fatal(err)
		}
		if !errors.Is(srv.outbox.Send(ctx, fed, saved[i], &fakeOutboundEvent{}), core.ErrOutboxUnavailable) {
			t.Fatal("old stream cancellation did not remove new key")
		}
	}
}
