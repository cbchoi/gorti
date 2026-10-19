package server

import (
	"context"
	"encoding/binary"
	"io"
	"log/slog"
	"math"
	"net"
	"sync"
	"testing"
	"time"

	grpcadapter "github.com/cbchoi/gorti/rti/internal/adapters/grpc"
	wire "github.com/cbchoi/gorti/rti/internal/adapters/protobuf"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	savepoint "github.com/cbchoi/gorti/rti/internal/hla/save_restore"
	"github.com/cbchoi/gorti/rti/pkg/federate"
	"github.com/cbchoi/gorti/rti/pkg/logicaltime"
	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func integerProductionServer(t *testing.T, dir string, generation uint32) (*rtid, *grpcgo.ClientConn, func()) {
	t.Helper()
	runtime, err := newRTID(rtidConfig{EnableInteger64: true, SaveDir: dir, GenerationEpoch: generation, OutboxBatchSize: 1, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- runtime.grpcS.Serve(listener) }()
	conn, err := grpcgo.NewClient(listener.Addr().String(), grpcgo.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	stop := func() {
		once.Do(func() {
			_ = conn.Close()
			runtime.grpcS.Stop()
			_ = listener.Close()
			<-done
			runtime.sessions.Close()
			for _, roster := range runtime.fedMgr.Snapshot() {
				_ = runtime.momMgr.FederationDestroyed(context.Background(), roster.Name)
			}
			_ = runtime.plugins.Close()
		})
	}
	t.Cleanup(stop)
	return runtime, conn, stop
}

func integerWireTime(t *testing.T, value int64) *rtiv1.LogicalTimeValue {
	t.Helper()
	typed, err := core.ScalarTimeValue(value)
	if err != nil {
		t.Fatal(err)
	}
	_, exact, err := wire.EncodeLogicalTimePair(core.ScalarRepresentation[int64](), typed)
	if err != nil {
		t.Fatal(err)
	}
	return exact
}

func integerQueryTime(ctx context.Context, t *testing.T, conn *grpcgo.ClientConn, fed string, handle uint64) int64 {
	t.Helper()
	response, err := rtiv1.NewTimeServiceClient(conn).QueryLogicalTime(ctx, &rtiv1.QueryFederateTimeRequest{WireVersion: 1, FederationName: fed, FederateHandle: handle})
	if err != nil {
		t.Fatal(err)
	}
	exact, err := wire.DecodeLogicalTimePair(core.ScalarRepresentation[int64](), response.LogicalTime, response.LogicalTimeValue)
	if err != nil {
		t.Fatal(err)
	}
	value, err := exact.Integer64()
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestProductionIntegerCompositionIdleMOMExactTimeAndIsolation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	runtime, conn, _ := integerProductionServer(t, t.TempDir(), 123)
	federation := rtiv1.NewFederationServiceClient(conn)
	for _, name := range []string{integer64TimeName, float64TimeName} {
		created, err := federation.CreateFederation(ctx, &rtiv1.CreateFederationRequest{WireVersion: 1, FederationName: name, LogicalTimeImplementationName: name, FomModules: []*rtiv1.FOMModule{momCompletionReviewModule(t, "Item")}})
		if err != nil {
			t.Fatal(err)
		}
		joined := momCompletionReviewJoin(ctx, t, conn, name, "idle", "", created.FederationGeneration)
		if joined.LogicalTimeImplementationName != name {
			t.Fatal("join selected wrong backend")
		}
		if name == integer64TimeName {
			result, err := rtiv1.NewMomServiceClient(conn).QueryFederateAttributes(ctx, &rtiv1.QueryFederateAttributesRequest{WireVersion: 1, FederationName: name, FederateHandle: joined.FederateHandle})
			if err != nil {
				t.Fatal(err)
			}
			if result.LogicalTime.GetLogicalTimeValue() == nil || result.Lookahead.GetLogicalTimeValue() == nil {
				t.Fatal("idle Integer64 MOM uses Float64 zero")
			}
			if len(result.AttributeValues["HLAlogicalTime"]) != 12 || binary.BigEndian.Uint32(result.AttributeValues["HLAlogicalTime"]) != 8 || binary.BigEndian.Uint64(result.AttributeValues["HLAlogicalTime"][4:]) != 0 {
				t.Fatal("initial metric is not exact zero")
			}
			for _, target := range []int64{1<<53 + 1, 1<<53 + 2, math.MaxInt64} {
				_, err := rtiv1.NewTimeServiceClient(conn).TimeAdvanceRequest(ctx, &rtiv1.TARRequest{WireVersion: 1, FederationName: name, FederateHandle: joined.FederateHandle, LogicalTimeValue: integerWireTime(t, target)})
				if err != nil {
					t.Fatal(err)
				}
				if got := integerQueryTime(ctx, t, conn, name, joined.FederateHandle); got != target {
					t.Fatalf("time rounded: %d != %d", got, target)
				}
			}
			_, err = rtiv1.NewTimeServiceClient(conn).TimeAdvanceRequest(ctx, &rtiv1.TARRequest{WireVersion: 1, FederationName: name, FederateHandle: joined.FederateHandle, LogicalTime: float64(1<<53 + 3)})
			if status.Code(err) != codes.InvalidArgument {
				t.Fatalf("mixed time accepted: %v", err)
			}
			if integerQueryTime(ctx, t, conn, name, joined.FederateHandle) != math.MaxInt64 {
				t.Fatal("rejected request mutated time")
			}
			state := runtime.timeMgr.Snapshot(core.FederationName(name))
			if len(state.Federates) != 0 {
				t.Fatal("Integer64 execution leaked into Float time manager")
			}
			if runtime.objReg.Snapshot(core.FederationName(name)).InstanceCount != 0 {
				t.Fatal("Integer64 MOM instances leaked into Float registry")
			}
		} else {
			result, err := rtiv1.NewTimeServiceClient(conn).QueryLogicalTime(ctx, &rtiv1.QueryFederateTimeRequest{WireVersion: 1, FederationName: name, FederateHandle: joined.FederateHandle})
			if err != nil || result.LogicalTime != 0 {
				t.Fatalf("Float default changed: %v %v", result, err)
			}
		}
	}
}

func TestProductionIntegerTimedSaveRestartAndIdentityRebinding(t *testing.T) {
	productionIntegerTimedSaveRestart(t, false)
}

func TestProductionIntegerLabelOnlyRecreatedRestore(t *testing.T) {
	productionIntegerTimedSaveRestart(t, true)
}

func productionIntegerTimedSaveRestart(t *testing.T, labelOnly bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	const fed = "integer-restart"
	dir := t.TempDir()
	source, conn, stop := integerProductionServer(t, dir, 141)
	create := func(conn *grpcgo.ClientConn) *rtiv1.CreateFederationResponse {
		t.Helper()
		response, err := rtiv1.NewFederationServiceClient(conn).CreateFederation(ctx, &rtiv1.CreateFederationRequest{WireVersion: 1, FederationName: fed, LogicalTimeImplementationName: integer64TimeName, FomModules: []*rtiv1.FOMModule{momCompletionReviewModule(t, "Item")}})
		if err != nil {
			t.Fatal(err)
		}
		return response
	}
	created := create(conn)
	var handles []uint64
	for _, name := range []string{"a", "b"} {
		joined := momCompletionReviewJoin(ctx, t, conn, fed, name, "", created.FederationGeneration)
		handles = append(handles, joined.FederateHandle)
	}
	sourceChannels := make(map[uint64]<-chan []core.OutboundEvent, len(handles))
	for _, h := range handles {
		channel, release, err := source.outbox.Subscribe(ctx, fed, core.FederateHandle(h))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = release() })
		sourceChannels[h] = channel
	}
	saves, times := rtiv1.NewSavepointServiceClient(conn), rtiv1.NewTimeServiceClient(conn)
	interval, _ := logicaltime.Integer64Interval(1)
	_, ahead, err := wire.EncodeLogicalTimeIntervalPair(logicaltime.HLAinteger64Time, interval)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = times.EnableTimeRegulation(ctx, &rtiv1.EnableRegulationRequest{WireVersion: 1, FederationName: fed, FederateHandle: handles[0], LookaheadValue: ahead}); err != nil {
		t.Fatal(err)
	}
	for _, h := range handles {
		if _, err := times.EnableTimeConstrained(ctx, &rtiv1.EnableConstrainedRequest{WireVersion: 1, FederationName: fed, FederateHandle: h}); err != nil {
			t.Fatal(err)
		}
	}
	_, err = saves.RequestFederationSave(ctx, &rtiv1.RequestFederationSaveRequest{WireVersion: 1, FederationName: fed, FederateHandle: handles[0], Label: "exact-final", SaveTimeValue: integerWireTime(t, math.MaxInt64)})
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range handles {
		_, err := times.TimeAdvanceRequest(ctx, &rtiv1.TARRequest{WireVersion: 1, FederationName: fed, FederateHandle: h, LogicalTimeValue: integerWireTime(t, math.MaxInt64)})
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, h := range handles {
		request := &rtiv1.FederateSaveResponseRequest{WireVersion: 1, FederationName: fed, FederateHandle: h}
		if _, err = saves.FederateSaveBegun(ctx, request); err != nil {
			t.Fatal(err)
		}
		if _, err = saves.FederateSaveComplete(ctx, request); err != nil {
			t.Fatal(err)
		}
	}
	for _, h := range handles {
		saved := false
		var failures []savepoint.SaveFailureReason
		for _, event := range drainScheduledEvents(sourceChannels[h]) {
			wire := invokeProductionRestoreFixtureEvent(ctx, t, conn, source, fed, core.FederateHandle(h), event)
			if outcome := wire.GetSaveCompleted(); outcome != nil {
				saved = saved || outcome.Label == "exact-final"
			}
			if outcome := wire.GetSaveFailed(); outcome != nil && outcome.Label == "exact-final" {
				failures = append(failures, savepoint.SaveFailureReason(outcome.GetReason()))
			}
		}
		if !saved || len(failures) != 0 {
			label, statuses, statusErr := source.integerSave.QueryFederationSaveStatus(fed)
			t.Fatalf("federate %d did not receive federationSaved: state=%v failureReasons=%v statusLabel=%q statuses=%v statusError=%v", h, source.integerSave.QuerySaveState(fed, "exact-final"), failures, label, statuses, statusErr)
		}
	}
	manifest, err := source.integerSave.LoadManifest(fed, "exact-final")
	if err != nil {
		t.Fatal(err)
	}
	if manifest.SaveTime == nil || *manifest.SaveTime != math.MaxInt64 || manifest.LogicalTimeImplementationName != integer64TimeName {
		t.Fatal("checkpoint lost selected exact final time")
	}
	floatState := source.saveMgr.QuerySaveState(fed, "exact-final")
	if floatState == savepoint.StateSaved {
		t.Fatal("Integer save routed to Float coordinator")
	}
	savedMetric, err := rtiv1.NewMomServiceClient(conn).QueryFederationAttributes(ctx, &rtiv1.QueryFederationAttributesRequest{WireVersion: 1, FederationName: fed})
	if err != nil {
		t.Fatal(err)
	}
	savedPayload := savedMetric.AttributeValues["HLAlastSaveTime"]
	if len(savedPayload) != 12 || binary.BigEndian.Uint64(savedPayload[4:]) != math.MaxInt64 {
		t.Fatalf("MOM last-save time is not exact: %x", savedPayload)
	}
	stop()
	target, targetConn, _ := integerProductionServer(t, dir, 151)
	restarted := create(targetConn)
	var current []uint64
	for _, name := range []string{"b", "a"} {
		joined := momCompletionReviewJoin(ctx, t, targetConn, fed, name, "", restarted.FederationGeneration)
		current = append(current, joined.FederateHandle)
	}
	channels := map[uint64]<-chan []core.OutboundEvent{}
	for _, h := range current {
		channel, release, err := target.outbox.Subscribe(ctx, fed, core.FederateHandle(h))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = release() })
		channels[h] = channel
		drainScheduledEvents(channel)
	}
	restoredSaves := rtiv1.NewSavepointServiceClient(targetConn)
	restoreRequest := &rtiv1.RequestFederationRestoreRequest{WireVersion: 1, FederationName: fed, FederateHandle: current[0], Label: "exact-final"}
	if !labelOnly {
		restoreRequest.SavedFederationGeneration = &created.FederationGeneration
	}
	_, err = restoredSaves.RequestFederationRestore(ctx, restoreRequest)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range current {
		for _, event := range drainScheduledEvents(channels[h]) {
			invokeProductionRestoreFixtureEvent(ctx, t, targetConn, target, fed, core.FederateHandle(h), event)
		}
		_, err = restoredSaves.FederateRestoreComplete(ctx, &rtiv1.FederateRestoreResponseRequest{WireVersion: 1, FederationName: fed, FederateHandle: h})
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, h := range current {
		var fresh core.FederateHandle
		reporting := core.FederateHandle(h)
		for _, event := range drainScheduledEvents(channels[h]) {
			encoded, encodeErr := grpcadapter.EncodeOutboundEvent(event)
			if encodeErr != nil {
				t.Fatal(encodeErr)
			}
			if terminal := encoded.GetRestoreCompleted(); terminal != nil && encoded.GetCallbackRecovery() == nil {
				reporting = core.FederateHandle(terminal.GetFederateHandle())
			}
			wire := invokeProductionRestoreFixtureEvent(ctx, t, targetConn, target, fed, reporting, event)
			if terminal := wire.GetRestoreCompleted(); terminal != nil && wire.GetCallbackRecovery() == nil {
				fresh = core.FederateHandle(terminal.GetFederateHandle())
			}
		}
		if fresh == 0 || uint64(fresh)>>32 != 151 {
			t.Fatal("restore identity was not rebound")
		}
		if got := integerQueryTime(ctx, t, targetConn, fed, uint64(fresh)); got != math.MaxInt64 {
			t.Fatalf("restored time=%d", got)
		}
		metric, err := rtiv1.NewMomServiceClient(targetConn).QueryFederateAttributes(ctx, &rtiv1.QueryFederateAttributesRequest{WireVersion: 1, FederationName: fed, FederateHandle: uint64(fresh)})
		if err != nil {
			t.Fatal(err)
		}
		payload := metric.AttributeValues["HLAlogicalTime"]
		if len(payload) != 12 || binary.BigEndian.Uint64(payload[4:]) != math.MaxInt64 {
			t.Fatalf("restored MOM logical time is not exact: %x", payload)
		}
	}
}

func TestProductionIntegerTwoGoSDKFederatesExactGrants(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, conn, _ := integerProductionServer(t, t.TempDir(), 161)
	otherConn, err := grpcgo.NewClient(conn.Target(), grpcgo.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = otherConn.Close() })
	first, second := federate.WrapGRPCClientConn(conn), federate.WrapGRPCClientConn(otherConn)
	module := momCompletionReviewModule(t, "Item")
	spec := federate.FederationSpec{Name: "integer-sdk", LogicalTimeImplementationName: integer64TimeName, FOMModules: []federate.FOMModule{{Path: module.Path, XML: module.Xml}}}
	if _, err := first.CreateFederation(ctx, spec); err != nil {
		t.Fatal(err)
	}
	a, err := first.JoinFederation(ctx, spec, "a")
	if err != nil {
		t.Fatal(err)
	}
	b, err := second.JoinFederation(ctx, spec, "b")
	if err != nil {
		t.Fatal(err)
	}
	for _, participant := range []*federate.Federate{a, b} {
		if participant.LogicalTimeImplementationName() != spec.LogicalTimeImplementationName {
			t.Fatal("SDK selected wrong factory")
		}
		for _, target := range []int64{1<<53 + 1, 1<<53 + 2, math.MaxInt64} {
			exact, err := logicaltime.Integer64(target)
			if err != nil {
				t.Fatal(err)
			}
			if err = participant.TimeAdvanceRequestValue(ctx, exact); err != nil {
				t.Fatal(err)
			}
			granted := false
			for !granted {
				select {
				case event, ok := <-participant.Events():
					if !ok {
						t.Fatal("callback channel closed")
					}
					if malformed, ok := event.(federate.CallbackProtocolError); ok {
						t.Fatal(malformed)
					}
					if grant, ok := event.(federate.TimeAdvanceGrant); ok {
						value, err := grant.TimeValue.Integer64()
						if err != nil || value != target {
							t.Fatalf("SDK grant lost integer: %d %v", value, err)
						}
						granted = true
					}
				case <-ctx.Done():
					t.Fatal("no exact SDK grant")
				}
			}
			value, err := participant.QueryLogicalTimeValue(ctx)
			if err != nil {
				t.Fatal(err)
			}
			integer, err := value.Integer64()
			if err != nil || integer != target {
				t.Fatalf("SDK query=%d %v", integer, err)
			}
		}
	}
}

func TestProductionIntegerResignDeletesSelectedRegistryObjects(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	runtime, conn, _ := integerProductionServer(t, t.TempDir(), 171)
	const fed = "integer-resign"
	created, err := rtiv1.NewFederationServiceClient(conn).CreateFederation(ctx, &rtiv1.CreateFederationRequest{WireVersion: 1, FederationName: fed, LogicalTimeImplementationName: integer64TimeName, FomModules: []*rtiv1.FOMModule{momCompletionReviewModule(t, "Item")}})
	if err != nil {
		t.Fatal(err)
	}
	a := momCompletionReviewJoin(ctx, t, conn, fed, "owner", "", created.FederationGeneration)
	b := momCompletionReviewJoin(ctx, t, conn, fed, "observer", "", created.FederationGeneration)
	model, err := runtime.foms.Get(ctx, fed)
	if err != nil {
		t.Fatal(err)
	}
	class, ok := model.LookupObjectClass("HLAobjectRoot.Item")
	if !ok {
		t.Fatal("class missing")
	}
	attr, ok := model.LookupAttribute(class, "Inherited")
	if !ok {
		t.Fatal("attribute missing")
	}
	declarations := rtiv1.NewDeclarationServiceClient(conn)
	if _, err = declarations.PublishObjectClassAttributes(ctx, &rtiv1.PubObjAttrsRequest{WireVersion: 1, FederationName: fed, FederateHandle: a.FederateHandle, ObjectClassHandle: uint64(class), AttributeHandles: []uint64{uint64(attr)}}); err != nil {
		t.Fatal(err)
	}
	if _, err = declarations.SubscribeObjectClassAttributes(ctx, &rtiv1.SubObjAttrsRequest{WireVersion: 1, FederationName: fed, FederateHandle: b.FederateHandle, ObjectClassHandle: uint64(class), AttributeHandles: []uint64{uint64(attr)}}); err != nil {
		t.Fatal(err)
	}
	registered, err := rtiv1.NewObjectServiceClient(conn).RegisterObjectInstance(ctx, &rtiv1.RegisterObjectRequest{WireVersion: 1, FederationName: fed, FederateHandle: a.FederateHandle, ObjectClassHandle: uint64(class)})
	if err != nil {
		t.Fatal(err)
	}
	_, err = rtiv1.NewFederationServiceClient(conn).ResignFederation(ctx, &rtiv1.ResignFederationRequest{WireVersion: 1, FederationName: fed, FederateHandle: a.FederateHandle, Action: rtiv1.ResignAction_RESIGN_ACTION_DELETE_THEN_DIVEST})
	if err != nil {
		t.Fatal(err)
	}
	if runtime.integerObjects.ClassOf(fed, core.ObjectHandle(registered.ObjectHandle)) != 0 {
		t.Fatal("Integer object survived resignation through Float registry")
	}
	if _, _, known := runtime.integerObjects.LookupKnownObjectInstance(fed, core.FederateHandle(b.FederateHandle), core.ObjectHandle(registered.ObjectHandle)); known {
		t.Fatal("recipient retained removed Integer object knowledge")
	}
}

func TestProductionIntegerObjectTSOReflectionBeforeExactGrant(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	runtime, conn, _ := integerProductionServer(t, t.TempDir(), 181)
	const fed = "integer-reflection"
	created, err := rtiv1.NewFederationServiceClient(conn).CreateFederation(ctx, &rtiv1.CreateFederationRequest{WireVersion: 1, FederationName: fed, LogicalTimeImplementationName: integer64TimeName, FomModules: []*rtiv1.FOMModule{momCompletionReviewModule(t, "Item")}})
	if err != nil {
		t.Fatal(err)
	}
	a := momCompletionReviewJoin(ctx, t, conn, fed, "producer", "", created.FederationGeneration)
	b := momCompletionReviewJoin(ctx, t, conn, fed, "receiver", "", created.FederationGeneration)
	model, err := runtime.foms.Get(ctx, fed)
	if err != nil {
		t.Fatal(err)
	}
	class, ok := model.LookupObjectClass("HLAobjectRoot.Item.Leaf")
	if !ok {
		t.Fatal("class missing")
	}
	attr, ok := model.LookupAttribute(class, "Local")
	if !ok {
		t.Fatal("attribute missing")
	}
	declarations := rtiv1.NewDeclarationServiceClient(conn)
	if _, err = declarations.PublishObjectClassAttributes(ctx, &rtiv1.PubObjAttrsRequest{WireVersion: 1, FederationName: fed, FederateHandle: a.FederateHandle, ObjectClassHandle: uint64(class), AttributeHandles: []uint64{uint64(attr)}}); err != nil {
		t.Fatal(err)
	}
	if _, err = declarations.SubscribeObjectClassAttributes(ctx, &rtiv1.SubObjAttrsRequest{WireVersion: 1, FederationName: fed, FederateHandle: b.FederateHandle, ObjectClassHandle: uint64(class), AttributeHandles: []uint64{uint64(attr)}}); err != nil {
		t.Fatal(err)
	}
	signed := metadata.AppendToOutgoingContext(ctx, "gorti-session-token-bin", string(b.SessionToken))
	stream, err := rtiv1.NewStreamServiceClient(conn).EventBatches(signed, &rtiv1.EventsRequest{WireVersion: 1, FederationName: fed, FederateHandle: b.FederateHandle, ExpectedFederationGeneration: &created.FederationGeneration})
	if err != nil {
		t.Fatal(err)
	}
	if ready, err := stream.Recv(); err != nil || !ready.Ready {
		t.Fatalf("callback readiness: %v %v", ready, err)
	}
	objects, times := rtiv1.NewObjectServiceClient(conn), rtiv1.NewTimeServiceClient(conn)
	registered, err := objects.RegisterObjectInstance(ctx, &rtiv1.RegisterObjectRequest{WireVersion: 1, FederationName: fed, FederateHandle: a.FederateHandle, ObjectClassHandle: uint64(class)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = times.EnableTimeConstrained(ctx, &rtiv1.EnableConstrainedRequest{WireVersion: 1, FederationName: fed, FederateHandle: b.FederateHandle}); err != nil {
		t.Fatal(err)
	}
	interval, _ := logicaltime.Integer64Interval(1)
	_, ahead, err := wire.EncodeLogicalTimeIntervalPair(logicaltime.HLAinteger64Time, interval)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = times.EnableTimeRegulation(ctx, &rtiv1.EnableRegulationRequest{WireVersion: 1, FederationName: fed, FederateHandle: a.FederateHandle, LookaheadValue: ahead}); err != nil {
		t.Fatal(err)
	}
	const target = int64(1<<53 + 17)
	if _, err = objects.UpdateAttributeValues(ctx, &rtiv1.UpdateAttributeValuesRequest{WireVersion: 1, FederationName: fed, FederateHandle: a.FederateHandle, ObjectHandle: registered.ObjectHandle, Attributes: map[uint64][]byte{uint64(attr): []byte("exact")}, LogicalTimeValue: integerWireTime(t, target)}); err != nil {
		t.Fatal(err)
	}
	for _, h := range []uint64{a.FederateHandle, b.FederateHandle} {
		if _, err = times.TimeAdvanceRequest(ctx, &rtiv1.TARRequest{WireVersion: 1, FederationName: fed, FederateHandle: h, LogicalTimeValue: integerWireTime(t, target)}); err != nil {
			t.Fatal(err)
		}
	}
	reflected, granted := false, false
	for !granted {
		batch, err := stream.Recv()
		if err != nil {
			t.Fatal(err)
		}
		for _, event := range batch.Events {
			if reflection := event.GetReflect(); reflection != nil && reflection.ObjectHandle == registered.ObjectHandle {
				typed, err := wire.DecodeOptionalLogicalTimePair(logicaltime.HLAinteger64Time, reflection.LogicalTime, reflection.LogicalTimeValue)
				if err != nil || typed == nil {
					t.Fatalf("reflection time: %v", err)
				}
				value, err := typed.Integer64()
				if err != nil || value != target {
					t.Fatalf("reflection rounded: %d %v", value, err)
				}
				reflected = true
			}
			if grant := event.GetGrant(); grant != nil {
				if !reflected {
					t.Fatal("grant overtook queued reflection")
				}
				typed, err := wire.DecodeLogicalTimePair(logicaltime.HLAinteger64Time, grant.LogicalTime, grant.LogicalTimeValue)
				if err != nil {
					t.Fatal(err)
				}
				value, err := typed.Integer64()
				if err != nil || value != target {
					t.Fatalf("grant rounded: %d %v", value, err)
				}
				granted = true
			}
		}
	}
}
