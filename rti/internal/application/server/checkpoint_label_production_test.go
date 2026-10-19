package server

import (
	"context"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/adapters/auth/session"
	grpcadapter "github.com/cbchoi/gorti/rti/internal/adapters/grpc"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	savepoint "github.com/cbchoi/gorti/rti/internal/hla/save_restore"
	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// These manager-composition fixtures consume raw outbox batches rather than a
// stream. Their collector is the callback body and reports both actual phases.
func invokeProductionRestoreFixtureEvent(ctx context.Context, t *testing.T, conn *grpcgo.ClientConn, runtime *rtid, fed core.FederationName, h core.FederateHandle, event core.OutboundEvent) *rtiv1.FederateEvent {
	t.Helper()
	wire, err := grpcadapter.EncodeOutboundEvent(event)
	if err != nil {
		t.Fatal(err)
	}
	if recovery := wire.GetCallbackRecovery(); recovery != nil {
		if recovery.Completed {
			return wire
		}
		if recovery.OutcomeOnly {
			t.Fatal("raw fixture cannot invent a saved SDK callback outcome")
		}
	} else if terminal := wire.GetRestoreCompleted(); terminal != nil {
		h = core.FederateHandle(terminal.GetFederateHandle())
	}
	if len(wire.CallbackReceipt) == 0 {
		return wire
	}
	identity, err := runtime.fedMgr.MemberIdentity(fed, h)
	if err != nil {
		t.Fatal(err)
	}
	token, err := session.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	runtime.sessions.Register(identity, token)
	signed := metadata.AppendToOutgoingContext(ctx, session.MetadataKey, string(token))
	for _, entry := range []bool{true, false} {
		_, err = rtiv1.NewSupportServiceClient(conn).ReportCallbackInvocation(signed, &rtiv1.ReportCallbackInvocationRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: uint64(h), CallbackReceipt: wire.CallbackReceipt, InvocationEntry: entry, Success: true})
		if err != nil {
			t.Fatalf("fixture callback phase entry=%v: %v", entry, err)
		}
	}
	return wire
}

func TestProductionLabelOnlyRestoreValidationBothFamilies(t *testing.T) {
	for _, mode := range []string{"valid", testReuseLabel, "fdd", "factory", "census", testPrivateLabel} {
		t.Run("Float64/"+mode, func(t *testing.T) { productionLabelRestoreContract[core.LogicalTime](t, mode) })
		t.Run("Integer64/"+mode, func(t *testing.T) { productionLabelRestoreContract[int64](t, mode) })
	}
}

func productionLabelRestoreContract[T core.LogicalTimeScalar](t *testing.T, mode string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	dir := t.TempDir()
	const fed = "label-restore"
	const label = "committed"
	source, conn, stop := integerProductionServer(t, dir, 211)
	factory := core.ScalarRepresentation[T]().String()
	created, err := rtiv1.NewFederationServiceClient(conn).CreateFederation(ctx, &rtiv1.CreateFederationRequest{WireVersion: 1, FederationName: fed, LogicalTimeImplementationName: factory, FomModules: []*rtiv1.FOMModule{momCompletionReviewModule(t, "Item")}})
	if err != nil {
		t.Fatal(err)
	}
	var old []uint64
	sourceChannels := map[uint64]<-chan []core.OutboundEvent{}
	for _, name := range []string{"a", "b"} {
		joined := momCompletionReviewJoin(ctx, t, conn, fed, name, "", created.FederationGeneration)
		old = append(old, joined.FederateHandle)
		channel, release, err := source.outbox.Subscribe(ctx, fed, core.FederateHandle(joined.FederateHandle))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = release() })
		sourceChannels[joined.FederateHandle] = channel
		drainScheduledEvents(channel)
	}
	instanceCount := func(runtime *rtid) uint32 {
		if factory == integer64TimeName {
			return runtime.integerObjects.Snapshot(fed).InstanceCount
		}
		return runtime.objReg.Snapshot(fed).InstanceCount
	}
	var register func()
	if mode == testReuseLabel {
		model, err := source.foms.Get(ctx, fed)
		if err != nil {
			t.Fatal(err)
		}
		class, ok := model.LookupObjectClass("HLAobjectRoot.Item.Leaf")
		if !ok {
			t.Fatal("missing reuse-test class")
		}
		attribute, ok := model.LookupAttribute(class, "Local")
		if !ok {
			t.Fatal("missing reuse-test attribute")
		}
		if _, err = rtiv1.NewDeclarationServiceClient(conn).PublishObjectClassAttributes(ctx, &rtiv1.PubObjAttrsRequest{WireVersion: 1, FederationName: fed, FederateHandle: old[0], ObjectClassHandle: uint64(class), AttributeHandles: []uint64{uint64(attribute)}}); err != nil {
			t.Fatal(err)
		}
		register = func() {
			t.Helper()
			if _, err := rtiv1.NewObjectServiceClient(conn).RegisterObjectInstance(ctx, &rtiv1.RegisterObjectRequest{WireVersion: 1, FederationName: fed, FederateHandle: old[0], ObjectClassHandle: uint64(class)}); err != nil {
				t.Fatal(err)
			}
		}
		register()
	}
	savedInstanceCount := instanceCount(source)
	saves := rtiv1.NewSavepointServiceClient(conn)
	if _, err = saves.RequestFederationSave(ctx, &rtiv1.RequestFederationSaveRequest{WireVersion: 1, FederationName: fed, FederateHandle: old[0], Label: label}); err != nil {
		t.Fatal(err)
	}
	for i, h := range old {
		r := &rtiv1.FederateSaveResponseRequest{WireVersion: 1, FederationName: fed, FederateHandle: h}
		if _, err = saves.FederateSaveBegun(ctx, r); err != nil {
			t.Fatal(err)
		}
		if mode == testPrivateLabel && i == 0 {
			r.ClientState = []byte("unrecognized-sdk-private-state")
		}
		if _, err = saves.FederateSaveComplete(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	for h, channel := range sourceChannels {
		saved := false
		notSaved := false
		for _, event := range drainScheduledEvents(channel) {
			wire := invokeProductionRestoreFixtureEvent(ctx, t, conn, source, fed, core.FederateHandle(h), event)
			if e := wire.GetSaveCompleted(); e != nil && e.Label == label {
				saved = true
			}
			if e := wire.GetSaveFailed(); e != nil && e.Label == label && e.GetReason() == int32(savepoint.RTIUnableToSave) {
				notSaved = true
			}
		}
		if mode == testPrivateLabel {
			if saved || !notSaved {
				t.Fatal("unrecognized private runtime was not rejected before source save persistence")
			}
			continue
		}
		if !saved {
			t.Fatal("source did not commit actual federationSaved")
		}
	}
	if mode == testPrivateLabel {
		return
	}
	if mode == testReuseLabel {
		register()
		if instanceCount(source) != savedInstanceCount+1 {
			t.Fatal("second save lacks a distinguishable live-state change")
		}
		if _, err = saves.RequestFederationSave(ctx, &rtiv1.RequestFederationSaveRequest{WireVersion: 1, FederationName: fed, FederateHandle: old[0], Label: label}); err != nil {
			t.Fatalf("reused label must not invent a request exception: %v", err)
		}
		for _, h := range old {
			r := &rtiv1.FederateSaveResponseRequest{WireVersion: 1, FederationName: fed, FederateHandle: h}
			if _, err = saves.FederateSaveBegun(ctx, r); err != nil {
				t.Fatal(err)
			}
			if _, err = saves.FederateSaveComplete(ctx, r); err != nil {
				t.Fatal(err)
			}
		}
		for h, channel := range sourceChannels {
			notSaved := false
			for _, event := range drainScheduledEvents(channel) {
				wire := invokeProductionRestoreFixtureEvent(ctx, t, conn, source, fed, core.FederateHandle(h), event)
				if wire.GetSaveCompleted() != nil {
					t.Fatal("immutable storage falsely committed a reused label")
				}
				if e := wire.GetSaveFailed(); e != nil && e.GetReason() == int32(savepoint.RTIUnableToSave) {
					notSaved = true
				}
			}
			if !notSaved {
				t.Fatal("reused label did not report the existing persistence failure")
			}
		}
	}
	stop()
	target, targetConn, _ := integerProductionServer(t, dir, 221)
	targetFactory, class := factory, "Item"
	if mode == "fdd" {
		class = "ChangedItem"
	}
	if mode == "factory" {
		targetFactory = integer64TimeName
		if factory == targetFactory {
			targetFactory = float64TimeName
		}
	}
	recreated, err := rtiv1.NewFederationServiceClient(targetConn).CreateFederation(ctx, &rtiv1.CreateFederationRequest{WireVersion: 1, FederationName: fed, LogicalTimeImplementationName: targetFactory, FomModules: []*rtiv1.FOMModule{momCompletionReviewModule(t, class)}})
	if err != nil {
		t.Fatal(err)
	}
	names := []string{"b", "a"}
	if mode == "census" {
		names = names[:1]
	}
	var current []uint64
	channels := map[uint64]<-chan []core.OutboundEvent{}
	for _, name := range names {
		joined := momCompletionReviewJoin(ctx, t, targetConn, fed, name, "", recreated.FederationGeneration)
		current = append(current, joined.FederateHandle)
		channel, release, err := target.outbox.Subscribe(ctx, fed, core.FederateHandle(joined.FederateHandle))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = release() })
		channels[joined.FederateHandle] = channel
		drainScheduledEvents(channel)
	}
	digest, _ := target.foms.DigestFor(fed)
	restore := rtiv1.NewSavepointServiceClient(targetConn)
	_, err = restore.RequestFederationRestore(ctx, &rtiv1.RequestFederationRestoreRequest{WireVersion: 1, FederationName: fed, FederateHandle: current[0], Label: label})
	if mode != "valid" && mode != testReuseLabel {
		if err == nil {
			t.Fatal("incompatible recreated restore accepted")
		}
		if after, _ := target.foms.DigestFor(fed); after != digest {
			t.Fatal("failed restore changed FDD")
		}
		members := target.fedMgr.ListMembers(fed)
		if len(members) != len(current) {
			t.Fatal("failed restore changed census")
		}
		for _, member := range members {
			if uint64(member.Handle)>>32 != 221 {
				t.Fatal("failed restore rebound live identity")
			}
		}
		for h, channel := range channels {
			for _, event := range drainScheduledEvents(channel) {
				wire := invokeProductionRestoreFixtureEvent(ctx, t, targetConn, target, fed, core.FederateHandle(h), event)
				if wire.GetRestoreInitiate() != nil {
					t.Fatal("incompatible restore published initiation")
				}
				if wire.GetRestoreCompleted() != nil {
					t.Fatal("failed restore committed")
				}
			}
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range current {
		for _, event := range drainScheduledEvents(channels[h]) {
			invokeProductionRestoreFixtureEvent(ctx, t, targetConn, target, fed, core.FederateHandle(h), event)
		}
		if _, err = restore.FederateRestoreComplete(ctx, &rtiv1.FederateRestoreResponseRequest{WireVersion: 1, FederationName: fed, FederateHandle: h}); err != nil {
			t.Fatal(err)
		}
	}
	for h, channel := range channels {
		restored := false
		reporting := core.FederateHandle(h)
		for _, event := range drainScheduledEvents(channel) {
			encoded, encodeErr := grpcadapter.EncodeOutboundEvent(event)
			if encodeErr != nil {
				t.Fatal(encodeErr)
			}
			if e := encoded.GetRestoreCompleted(); e != nil && encoded.GetCallbackRecovery() == nil {
				reporting = core.FederateHandle(e.GetFederateHandle())
			}
			wire := invokeProductionRestoreFixtureEvent(ctx, t, targetConn, target, fed, reporting, event)
			if e := wire.GetRestoreCompleted(); e != nil && wire.GetCallbackRecovery() == nil {
				if e.GetFederateHandle() == 0 || e.GetFederateHandle()>>32 != 221 {
					t.Fatal("restore did not use recreated execution identities")
				}
				restored = true
			}
		}
		if !restored {
			t.Fatal("label-only restore lacks actual federationRestored")
		}
	}
	if mode == testReuseLabel && instanceCount(target) != savedInstanceCount {
		t.Fatalf("reused label did not restore the original committed state: got %d want %d", instanceCount(target), savedInstanceCount)
	}
}
