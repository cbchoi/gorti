package federate

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"

	codec "github.com/cbchoi/gorti/rti/internal/adapters/protobuf"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/pkg/logicaltime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func TestSDKRuntimeSaveCompleteNonempty(t *testing.T) {
	client := &captureSavepointClient{}
	f := &Federate{conn: &Connection{savepoint: client}, federateHandle: 17, federationGeneration: 1, objectClasses: map[uint64]uint64{42: 9}}
	if err := f.FederateSaveComplete(context.Background()); err != nil {
		t.Fatal(err)
	}
	state := client.request.(*rtiv1.FederateSaveResponseRequest).ClientState
	var envelope map[string]json.RawMessage
	if len(state) == 0 || json.Unmarshal(state, &envelope) != nil || len(envelope["state"]) == 0 || len(envelope["checksum"]) == 0 {
		t.Fatalf("nonempty versioned SDK runtime missing from saveComplete: %s", state)
	}
}

func runtimeTestFederate(t *testing.T, scalar string) *Federate {
	t.Helper()
	tables, err := tablesFromFOMView(authoritativeView(1))
	if err != nil {
		t.Fatal(err)
	}
	f := &Federate{federateHandle: 17, federationGeneration: 1, logicalTimeImplementationName: scalar, handles: tables, eventCh: make(chan Event, 16)}
	f.conn = &Connection{fed: fomViewClient{get: func(context.Context, *rtiv1.GetFOMViewRequest) (*rtiv1.FOMView, error) {
		return authoritativeView(1), nil
	}}}
	return f
}

func runtimeTestState(t *testing.T, f *Federate) sdkRuntimeState {
	t.Helper()
	encoded, err := f.snapshotRuntime()
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := f.decodeRuntime(encoded)
	if err != nil {
		t.Fatal(err)
	}
	return prepared.state
}

func TestSDKRuntimeExactKnowledgeAndRetractionRoundTrip(t *testing.T) {
	for _, scalar := range []string{HLAfloat64Time, HLAinteger64Time} {
		t.Run(scalar, func(t *testing.T) {
			f := runtimeTestFederate(t, scalar)
			at, _ := logicaltime.Float64(math.Nextafter(1, 2))
			if scalar == HLAinteger64Time {
				at, _ = logicaltime.Integer64(1<<53 + 1)
			}
			f.objectClasses = map[uint64]uint64{42: 9, 44: 9}
			f.ownedObjectAttributes = map[uint64]map[uint64]struct{}{42: {11: {}}}
			f.deletedObjects = map[deletionDesignator]deletedObjectKnowledge{{17, 77}: {object: 43, class: 9, owned: map[uint64]struct{}{11: {}}, originated: true}}
			f.pendingObjectRemovals = map[uint64]logicaltime.Time{42: at}
			f.pendingFinalObjectRemovals = map[uint64]struct{}{44: {}}
			f.timeAdvances = []*sdkAdvanceRequest{{flush: true}, {flush: false}}
			state := runtimeTestState(t, f)
			state.Self = 29
			state.Epoch = 2
			encoded, err := encodeRuntimeState(state)
			if err != nil {
				t.Fatal(err)
			}
			if err := f.stageRuntimeRestore(InitiateFederateRestore{FederateHandle: 17, ClientState: encoded}); err != nil {
				t.Fatal(err)
			}
			h, e := uint64(29), uint64(2)
			if err := f.applyRestoredState(context.Background(), FederationRestored{FederateHandle: &h, CallbackEpoch: &e}); err != nil {
				t.Fatal(err)
			}
			if f.objectClasses[42] != 9 || f.pendingObjectRemovals[42] != at || len(f.timeAdvances) != 2 || !f.timeAdvances[0].flush {
				t.Fatal("runtime knowledge/time changed")
			}
			if _, ok := f.pendingFinalObjectRemovals[44]; !ok {
				t.Fatal("final removal lost")
			}
			f.restoreDeletedObject(deletionDesignator{17, 77}, true)
			if f.objectClasses[43] != 9 {
				t.Fatal("runtime retraction knowledge lost")
			}
		})
	}
}

func TestSDKRuntimeUnreadDataDelegatesAuthoritativeReplay(t *testing.T) {
	f := runtimeTestFederate(t, HLAfloat64Time)
	wire := &rtiv1.FederateEvent{CallbackReceipt: []byte("server-owned"), Event: &rtiv1.FederateEvent_Remove{Remove: &rtiv1.RemoveObjectInstance{ObjectHandle: 42}}}
	event := f.translate(wire)
	f.eventCh <- event
	<-f.Events()
	discovery := f.translate(&rtiv1.FederateEvent{CallbackReceipt: []byte("discovery-owned"), Event: &rtiv1.FederateEvent_Discover{Discover: &rtiv1.DiscoverObjectInstance{ObjectHandle: 43, ObjectClassHandle: 19, ObjectName: "known"}}})
	f.eventCh <- discovery
	<-f.Events()
	wire.CallbackReceipt[0] = 'X'
	state := runtimeTestState(t, f)
	if len(state.Callbacks) != 0 || len(state.PendingData) != 2 || state.PendingData[0].InvocationState != runtimeInvocationUnentered || f.objectClasses[43] != 19 {
		t.Fatalf("channel read incorrectly treated as invocation: %+v", state)
	}
	encoded, _ := encodeRuntimeState(state)
	if bytes.Contains(encoded, []byte("server-owned")) {
		t.Fatal("saved reusable old data receipt")
	}
}

func TestSDKRuntimeInvalidRestoreIsAtomic(t *testing.T) {
	f := runtimeTestFederate(t, HLAfloat64Time)
	f.objectClasses = map[uint64]uint64{42: 9}
	valid, err := f.snapshotRuntime()
	if err != nil {
		t.Fatal(err)
	}
	var envelope sdkRuntimeEnvelope
	if err := json.Unmarshal(valid, &envelope); err != nil {
		t.Fatal(err)
	}
	cases := map[string][]byte{"opaque": []byte("legacy-private-unknown"), "trailing": append(append([]byte(nil), valid...), []byte(" {}")...)}
	for _, kind := range []string{"version", "scalar", "generation", "checksum", "object", "unknown", "callback", "duplicate-object", "duplicate-member", "mixed-time", "capability"} {
		e := envelope
		switch kind {
		case "version":
			e.Version = 99
		case "scalar":
			e.Scalar = HLAinteger64Time
		case "generation":
			e.Generation = 2
		case "checksum":
			e.Checksum = "bad"
		case "object":
			e.State = json.RawMessage(`{"callback_recovery":"server","known_objects":[{"object_handle":0,"class_handle":9}]}`)
		case "unknown":
			e.State = json.RawMessage(`{"callback_recovery":"server","unknown_runtime":true}`)
		case "callback":
			e.State = json.RawMessage(`{"callback_recovery":"server","pending_callbacks":[{"ready":{}}]}`)
		case "duplicate-object":
			e.State = json.RawMessage(`{"callback_recovery":"server","known_objects":[{"object_handle":42,"class_handle":9},{"object_handle":42,"class_handle":9}]}`)
		case "duplicate-member":
			e.State = json.RawMessage(`{"callback_recovery":"server","callback_epoch":1,"callback_epoch":2}`)
		case "mixed-time":
			e.State = json.RawMessage(`{"callback_recovery":"server","known_objects":[{"object_handle":42,"class_handle":9}],"pending_removals":[{"object_handle":42,"logical_time_value":{"integer64_value":"9007199254740993"}}]}`)
		case "capability":
			e.State = json.RawMessage(`{"callback_recovery":"server","pending_callbacks":[{"callback_receipt":"b2xk","remove":{"object_handle":"42"}}]}`)
		}
		if kind != "checksum" {
			e.Checksum, _ = runtimeChecksum(e)
		}
		cases[kind], _ = json.Marshal(e)
	}
	for name, encoded := range cases {
		t.Run(name, func(t *testing.T) {
			before := f.runtimeEpoch
			if err := f.stageRuntimeRestore(InitiateFederateRestore{ClientState: encoded}); err == nil {
				t.Fatal("invalid state accepted")
			}
			if f.CurrentHandle() != 17 || f.runtimeEpoch != before || !reflect.DeepEqual(f.objectClasses, map[uint64]uint64{42: 9}) || f.runtimeStaged != nil {
				t.Fatal("invalid state mutated runtime")
			}
		})
	}
}

func TestSDKRuntimeLegacyRestorePurgesAndFences(t *testing.T) {
	f := runtimeTestFederate(t, HLAfloat64Time)
	event := f.translate(&rtiv1.FederateEvent{Event: &rtiv1.FederateEvent_Remove{Remove: &rtiv1.RemoveObjectInstance{ObjectHandle: 42}}})
	f.eventCh <- event
	if err := f.stageRuntimeRestore(InitiateFederateRestore{}); err != nil {
		t.Fatal(err)
	}
	h, e := uint64(29), uint64(2)
	restored, healthy := f.prepareRestoredCallback(context.Background(), FederationRestored{FederateHandle: &h, CallbackEpoch: &e})
	if !healthy {
		t.Fatal(restored)
	}
	if !f.publishRuntimeEvent(context.Background(), restored) {
		t.Fatal("publish failed")
	}
	if _, ok := (<-f.Events()).(FederationRestored); !ok || len(f.eventCh) != 0 {
		t.Fatal("old queue not replaced at terminal")
	}
	invoked := false
	_, err := f.DispatchEvent(context.Background(), event, func(Event) error { invoked = true; return nil })
	if err == nil || invoked {
		t.Fatal("held old raw event invoked after restore")
	}
}

func TestSDKRuntimeCrossGenerationRequiresReboundEnvelope(t *testing.T) {
	source := runtimeTestFederate(t, HLAinteger64Time)
	source.objectClasses = map[uint64]uint64{1<<32 | 42: 9}
	encoded, _ := source.snapshotRuntime()
	target := runtimeTestFederate(t, HLAinteger64Time)
	target.federationGeneration = 2
	if err := target.stageRuntimeRestore(InitiateFederateRestore{ClientState: encoded}); err == nil {
		t.Fatal("untranslated source accepted")
	}
	state := runtimeTestState(t, source)
	state.Generation = 2
	state.Self = 2<<32 | 29
	state.Epoch = 3
	state.Objects[0].Object = 2<<32 | 42
	encoded, _ = encodeRuntimeState(state)
	if err := target.stageRuntimeRestore(InitiateFederateRestore{FederateHandle: 17, ClientState: encoded}); err != nil {
		t.Fatal(err)
	}
	h, e := state.Self, state.Epoch
	if err := target.applyRestoredState(context.Background(), FederationRestored{FederateHandle: &h, CallbackEpoch: &e}); err != nil {
		t.Fatal(err)
	}
	if target.objectClasses[2<<32|42] != 9 || target.CurrentHandle() != h {
		t.Fatal("translated knowledge missing")
	}
}

func TestSDKRuntimeConcurrentReceiveAndSnapshot(t *testing.T) {
	f := runtimeTestFederate(t, HLAfloat64Time)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			f.translate(&rtiv1.FederateEvent{CallbackReceipt: []byte{byte(i + 1)}, Event: &rtiv1.FederateEvent_Remove{Remove: &rtiv1.RemoveObjectInstance{ObjectHandle: uint64(i + 1)}}})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			if _, err := f.snapshotRuntime(); err != nil {
				t.Error(err)
			}
		}
	}()
	wg.Wait()
	if state := runtimeTestState(t, f); len(state.PendingData) != 50 {
		t.Fatalf("pending conservation=%d", len(state.PendingData))
	}
}

func TestSDKRuntimeReceiptlessPendingExactReplay(t *testing.T) {
	for _, scalar := range []string{HLAfloat64Time, HLAinteger64Time} {
		t.Run(scalar, func(t *testing.T) {
			f := runtimeTestFederate(t, scalar)
			at, _ := logicaltime.Float64(math.Nextafter(1, 2))
			if scalar == HLAinteger64Time {
				at, _ = logicaltime.Integer64(1<<53 + 1)
			}
			legacy, exact, err := codec.EncodeOptionalLogicalTimePair(f.timeRepresentation(), &at)
			if err != nil {
				t.Fatal(err)
			}
			wire := &rtiv1.FederateEvent{Event: &rtiv1.FederateEvent_Remove{Remove: &rtiv1.RemoveObjectInstance{ObjectHandle: 42, LogicalTime: legacy, LogicalTimeValue: exact}}}
			state := runtimeTestState(t, f)
			f.translate(wire)
			if _, err := f.snapshotRuntime(); err == nil || len(f.runtimePending) != 1 {
				t.Fatal("receiptless v2 export was not fail-closed")
			}
			state.Recovery, state.PendingOutcomes = "server", nil
			body, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(wire)
			if err != nil {
				t.Fatal(err)
			}
			state.Callbacks = []json.RawMessage{body}
			if len(state.Callbacks) != 1 {
				t.Fatal("receiptless unread data silently dropped")
			}
			var saved rtiv1.FederateEvent
			if err := protojson.Unmarshal(state.Callbacks[0], &saved); err != nil {
				t.Fatal(err)
			}
			if !proto.Equal(wire, &saved) {
				t.Fatal("pending payload changed")
			}
			state.Self = 29
			state.Epoch = 2
			encoded, _ := encodeRuntimeState(state)
			if err := f.stageRuntimeRestore(InitiateFederateRestore{ClientState: encoded}); err != nil {
				t.Fatal(err)
			}
			h, e := state.Self, state.Epoch
			restored, healthy := f.prepareRestoredCallback(context.Background(), FederationRestored{FederateHandle: &h, CallbackEpoch: &e})
			if !healthy {
				t.Fatal(restored)
			}
			f.publishRuntimeEvent(context.Background(), restored)
			<-f.Events()
			remove, ok := (<-f.Events()).(RemoveObjectInstance)
			if !ok || remove.TimestampValue == nil || *remove.TimestampValue != at {
				t.Fatal("exact legacy pending event not recovered")
			}
		})
	}
}

func TestSDKRuntimeCallbackEntryRetiresOnlyAfterAdmission(t *testing.T) {
	var fail atomic.Bool
	fail.Store(true)
	f := remainingServicesFederate(t, func(_ context.Context, request any, _ *grpc.UnaryServerInfo, _ grpc.UnaryHandler) (any, error) {
		r := request.(*rtiv1.ReportCallbackInvocationRequest)
		if r.InvocationEntry && fail.Load() {
			return nil, status.Error(codes.Unavailable, "entry unavailable")
		}
		return &rtiv1.Empty{}, nil
	})
	f.callbackEntrySupported = true
	event := f.translate(&rtiv1.FederateEvent{CallbackReceipt: []byte("data-receipt"), Event: &rtiv1.FederateEvent_Remove{Remove: &rtiv1.RemoveObjectInstance{ObjectHandle: 42}}})
	if err := f.ReportCallbackEntry(context.Background(), event); err == nil {
		t.Fatal("entry rejection hidden")
	}
	if len(f.runtimePending) != 1 {
		t.Fatal("entry failure dropped unread data")
	}
	fail.Store(false)
	if err := f.ReportCallbackEntry(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if len(f.runtimePending) != 0 {
		t.Fatal("explicit authenticated callback entry did not retire queue metadata")
	}
	if _, err := f.snapshotRuntime(); err == nil {
		t.Fatal("manual callback entry without outcome was checkpointed as settled")
	}
	if err := f.ReportCallbackInvocation(context.Background(), event, true, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := f.snapshotRuntime(); err != nil {
		t.Fatal(err)
	}
}

func TestSDKRuntimeDispatchRefusesCheckpointWhileInvokingAndUnresolved(t *testing.T) {
	var fail atomic.Bool
	fail.Store(true)
	f := remainingServicesFederate(t, func(_ context.Context, request any, _ *grpc.UnaryServerInfo, _ grpc.UnaryHandler) (any, error) {
		r := request.(*rtiv1.ReportCallbackInvocationRequest)
		if !r.InvocationEntry && fail.Load() {
			return nil, status.Error(codes.Unavailable, "outcome unavailable")
		}
		return &rtiv1.Empty{}, nil
	})
	f.callbackEntrySupported = true
	event := f.translate(&rtiv1.FederateEvent{CallbackReceipt: []byte("data-receipt"), Event: &rtiv1.FederateEvent_Remove{Remove: &rtiv1.RemoveObjectInstance{ObjectHandle: 42}}})
	invoked := 0
	callback := func(Event) error {
		invoked++
		if _, err := f.snapshotRuntime(); err == nil {
			t.Error("checkpoint accepted during callback body")
		}
		return nil
	}
	if a, r := f.DispatchEvent(context.Background(), event, callback); a != nil || r == nil {
		t.Fatalf("dispatch %v/%v", a, r)
	}
	if state := runtimeTestState(t, f); len(state.PendingOutcomes) != 1 || !state.PendingOutcomes[0].Success {
		t.Fatal("finished callback outcome not retained for recovery")
	}
	fail.Store(false)
	if a, r := f.DispatchEvent(context.Background(), event, callback); a != nil || r != nil {
		t.Fatalf("retry %v/%v", a, r)
	}
	if invoked != 1 {
		t.Fatal("outcome retry reinvoked application")
	}
	if _, err := f.snapshotRuntime(); err != nil {
		t.Fatal(err)
	}
}

func TestSDKRuntimeSaveFromInitiateCallback(t *testing.T) {
	f := remainingServicesFederate(t, func(context.Context, any, *grpc.UnaryServerInfo, grpc.UnaryHandler) (any, error) {
		return &rtiv1.Empty{}, nil
	})
	client := &captureSavepointClient{}
	f.conn.savepoint = client
	f.callbackEntrySupported = true
	event := f.translate(&rtiv1.FederateEvent{CallbackReceipt: []byte("save-receipt"), Event: &rtiv1.FederateEvent_SaveInitiate{SaveInitiate: &rtiv1.InitiateFederateSave{Label: "checkpoint"}}})
	if a, r := f.DispatchEvent(context.Background(), event, func(Event) error {
		if err := f.FederateSaveComplete(context.Background()); err == nil {
			t.Error("running save callback exported unknown result")
		}
		return nil
	}); a != nil || r != nil {
		t.Fatalf("save callback %v/%v", a, r)
	}
	if err := f.FederateSaveComplete(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(client.calls) != 1 {
		t.Fatal("saveComplete missing inside callback")
	}
}

func TestSDKRuntimeReceiptlessDispatchDeduplicates(t *testing.T) {
	f := runtimeTestFederate(t, HLAfloat64Time)
	event := f.translate(&rtiv1.FederateEvent{Event: &rtiv1.FederateEvent_Remove{Remove: &rtiv1.RemoveObjectInstance{ObjectHandle: 42}}})
	invoked := 0
	for i := 0; i < 2; i++ {
		_, _ = f.DispatchEvent(context.Background(), event, func(Event) error { invoked++; return nil })
	}
	if invoked != 1 {
		t.Fatalf("receiptless held callback invoked %d times", invoked)
	}
}

func TestSDKRuntimeConcurrentReceiptlessDispatchDeduplicates(t *testing.T) {
	f := runtimeTestFederate(t, HLAfloat64Time)
	event := f.translate(&rtiv1.FederateEvent{Event: &rtiv1.FederateEvent_Remove{Remove: &rtiv1.RemoveObjectInstance{ObjectHandle: 42}}})
	var invoked atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = f.DispatchEvent(context.Background(), event, func(Event) error { invoked.Add(1); return nil })
		}()
	}
	wg.Wait()
	if invoked.Load() != 1 {
		t.Fatalf("concurrent receiptless callback invoked %d times", invoked.Load())
	}
}

func TestSDKRuntimeUnsupportedCapabilityAndOversizeFailClosed(t *testing.T) {
	f := runtimeTestFederate(t, HLAfloat64Time)
	f.translate(&rtiv1.FederateEvent{OwnershipReceipt: &rtiv1.OwnershipCallbackReceipt{CallbackId: 1}, Event: &rtiv1.FederateEvent_Grant{Grant: &rtiv1.TimeAdvanceGrant{LogicalTime: 1}}})
	if _, err := f.snapshotRuntime(); err == nil {
		t.Fatal("opaque control capability exported as portable")
	}
	if len(f.runtimePending) != 1 {
		t.Fatal("failed export discarded pending callback")
	}
	if err := f.stageRuntimeRestore(InitiateFederateRestore{ClientState: make([]byte, sdkRuntimeLimit+1)}); err == nil {
		t.Fatal("oversized checkpoint accepted")
	}
	if len(f.runtimePending) != 1 || f.runtimeStaged != nil {
		t.Fatal("oversized decode changed queue")
	}
}

func TestSDKRuntimeTerminalMismatchIsAtomic(t *testing.T) {
	f := runtimeTestFederate(t, HLAfloat64Time)
	event := f.translate(&rtiv1.FederateEvent{CallbackReceipt: bytes.Repeat([]byte{5}, 32), Event: &rtiv1.FederateEvent_Remove{Remove: &rtiv1.RemoveObjectInstance{ObjectHandle: 42}}})
	f.eventCh <- event
	state := runtimeTestState(t, f)
	state.Self = 29
	encoded, _ := encodeRuntimeState(state)
	if err := f.stageRuntimeRestore(InitiateFederateRestore{ClientState: encoded}); err != nil {
		t.Fatal(err)
	}
	h, e := uint64(30), uint64(2)
	if err := f.applyRestoredState(context.Background(), FederationRestored{FederateHandle: &h, CallbackEpoch: &e}); err == nil {
		t.Fatal("mismatched terminal accepted")
	}
	if f.CurrentHandle() != 17 || f.runtimeEpoch != 0 || len(f.eventCh) != 1 || len(f.runtimePending) != 1 {
		t.Fatal("mismatched terminal mutated identity or queue")
	}
}

func TestSDKRuntimePortableCodecFixtures(t *testing.T) {
	for _, scalar := range []string{HLAfloat64Time, HLAinteger64Time} {
		t.Run(scalar, func(t *testing.T) {
			f := runtimeTestFederate(t, scalar)
			f.federationGeneration = 7
			f.federateHandle = 7<<32 | 8
			at, _ := logicaltime.Float64(2.5)
			if scalar == HLAinteger64Time {
				at, _ = logicaltime.Integer64(1<<53 + 1)
			}
			f.objectClasses = map[uint64]uint64{100: 19}
			f.ownedObjectAttributes = map[uint64]map[uint64]struct{}{100: {2: {}}}
			f.pendingObjectRemovals = map[uint64]logicaltime.Time{100: at}
			f.deletedObjects = map[deletionDesignator]deletedObjectKnowledge{{f.CurrentHandle(), 77}: {object: 101, class: 19, owned: map[uint64]struct{}{2: {}}, originated: true}}
			encoded, err := f.snapshotRuntime()
			if err != nil {
				t.Fatal(err)
			}
			prepared, err := f.decodeRuntime(encoded)
			if err != nil {
				t.Fatal(err)
			}
			if prepared.removals[100] != at || prepared.state.Deleted[0].Sender != f.CurrentHandle() {
				t.Fatal("production exported fixture changed state")
			}
			if directory := os.Getenv("GORTI_GO_RUNTIME_FIXTURES"); directory != "" {
				if err := os.MkdirAll(directory, 0700); err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(directory, "go-"+scalar+".json")
				if err := os.WriteFile(path, encoded, 0600); err != nil {
					t.Fatal(err)
				}
				t.Logf("production fixture: %s", path)
			}
			if directory := os.Getenv("GORTI_GO_RUNTIME_REBOUND_FIXTURES"); directory != "" {
				encoded, err := os.ReadFile(filepath.Join(directory, os.Getenv("GORTI_GO_RUNTIME_REBOUND_PREFIX")+"go-"+scalar+".json"))
				if err != nil {
					t.Fatal(err)
				}
				f.federationGeneration = 9
				prepared, err := f.decodeRuntime(encoded)
				if err != nil {
					t.Fatal(err)
				}
				if prepared.state.Self != 9<<32|41 || prepared.state.Deleted[0].Sender != 9<<32|41 || prepared.removals[100] != at || prepared.state.Objects[0].Object != 100 {
					t.Fatal("central codec rebinding altered state or omitted federate translation")
				}
				t.Log("independent SDK decode of central rebound fixture passed")
			}
		})
	}
}
