package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strconv"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/adapters/savebundle"
	"github.com/cbchoi/gorti/rti/internal/adapters/sdkstate"
	"github.com/cbchoi/gorti/rti/internal/application/composition"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/federation"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
	savepoint "github.com/cbchoi/gorti/rti/internal/hla/save_restore"
)

type typedRebindingLedgerOf[T core.LogicalTimeScalar] struct {
	savebundle.RetractionsOf[T]
	data []byte
	err  error
}

func (l typedRebindingLedgerOf[T]) Marshal(core.FederationName) ([]byte, error) {
	return bytes.Clone(l.data), l.err
}

func typedRebindingJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func typedRebindingFixtureOf[T core.LogicalTimeScalar](t *testing.T, at T) (*federation.Manager, savebundle.RestoreGraphOf[T], savepoint.ManifestOf[T], core.FOMHandle) {
	t.Helper()
	ctx := context.Background()
	const fed core.FederationName = "typed-rebind"
	sourceRepo := composition.NewFOMRepository()
	model, err := sourceRepo.Load(ctx, []core.FOMModule{{Path: "saved.xml", XML: []byte(`<objectModel><objects><objectClass><name>HLAobjectRoot</name><objectClass><name>Vehicle</name><attribute><name>Position</name></attribute></objectClass></objectClass></objects></objectModel>`)}})
	if err != nil {
		t.Fatal(err)
	}
	sourceRepo.RememberFor(fed, model)
	fomBytes, err := (savebundle.FOM{Repository: sourceRepo}).Marshal(fed)
	if err != nil {
		t.Fatal(err)
	}
	targetRepo := composition.NewFOMRepository()
	empty, err := targetRepo.Load(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	targetRepo.RememberFor(fed, empty)
	manager, err := federation.New(federation.Options{Clock: core.NewRealClock(), FOMs: targetRepo, GenerationEpoch: 9})
	if err != nil {
		t.Fatal(err)
	}
	// Membership is time-free. These fixtures do not activate an Integer64 server
	// selector; they exercise the typed restore hook below the composition boundary.
	if err := manager.CreateFederation(ctx, core.CreateFederationRequest{Name: fed}); err != nil {
		t.Fatal(err)
	}
	current := map[string]core.FederateHandle{}
	for _, name := range []string{"receiver", "sender"} {
		h, err := manager.JoinFederation(ctx, core.JoinFederationRequest{Federation: fed, FederateName: name})
		if err != nil {
			t.Fatal(err)
		}
		current[name] = h
	}
	const sender core.FederateHandle = 7<<32 | 1
	const receiver core.FederateHandle = 7<<32 | 2
	const historical core.FederateHandle = 7<<32 | 3
	cls, _ := model.LookupObjectClass("HLAobjectRoot.Vehicle")
	integer := core.ScalarRepresentation[T]().String() == integer64TimeName
	timeVersion, objectVersion, ledgerVersion := uint32(1), uint32(1), uint32(2)
	if integer {
		timeVersion, objectVersion, ledgerVersion = 6, 5, 3
	}
	objectWire := map[string]any{"version": objectVersion, "state": &object.RegistryStateOf[T]{LastObjectHandle: 1, Objects: []object.InstanceState{{Handle: 1, Class: cls, Name: "saved-object", Registrant: sender}}, Deletions: []object.DeletionStateOf[T]{{Object: 1, Producer: sender, Timestamp: at, RetractionHandle: 7}}}}
	timeWire := map[string]any{"version": timeVersion, "halted": false, "stall_timeout_ns": int64(1000000000), "federates": []any{map[string]any{"handle": sender, "has_regulation_state": true, "constrained": true, "has_advance_state": true, "lookahead": T(0), "current_time": at - 1, "requested_time": T(0), "queue": []any{map[string]any{"destination": sender, "timestamp": at, "sender": sender, "retraction_handle": uint64(7), "event": map[string]any{"kind": "delete_marker", "object": uint64(1), "object_class": cls, "logical_time": at, "metadata": &core.ObjectCallbackMetadata{Producer: sender, RetractionHandle: 7, SentOrder: core.OrderTimestamp, ReceivedOrder: core.OrderTimestamp}}}}}}}
	ledger := func(producers []any) []byte {
		wire := map[string]any{"version": ledgerVersion, "history": map[string]any{"complete": true, "producers": producers}}
		if integer {
			wire["logical_time_implementation_name"] = integer64TimeName
		}
		return typedRebindingJSON(t, wire)
	}
	producer := func(h core.FederateHandle, id uint64) any {
		return map[string]any{"producer": h, "accepted": []any{map[string]any{"first": id, "last": id}}, "messages": []any{map[string]any{"id": id, "timestamp": at}}}
	}
	if integer {
		objectWire["logical_time_implementation_name"] = integer64TimeName
		timeWire["logical_time_implementation_name"] = integer64TimeName
	}
	gen := uint64(7)
	manifest := savepoint.ManifestOf[T]{Version: 2, Federation: fed, FederationGeneration: &gen, LogicalTimeImplementationName: core.ScalarRepresentation[T]().String(), Label: testSavedLabel, SaveTime: &at, Federates: []core.FederateHandle{sender, receiver}, FederateNames: []string{"sender", "receiver"}, FederateTypes: []string{"model", "model"}, FederateClientStates: make([][]byte, 2), ManagerSnapshots: map[string][]byte{"fom": fomBytes, "object": typedRebindingJSON(t, objectWire), "time": typedRebindingJSON(t, timeWire), "ownership": nil, "declaration": []byte(`{"version":3}`), "ddm": nil, "sync": nil, "mom": nil, "retractions": ledger([]any{producer(sender, 7), producer(historical, 31)})}}
	var privateTime any = map[string]any{"float64_value": at}
	if integer {
		privateTime = map[string]any{"integer64_value": strconv.FormatInt(int64(at), 10)}
	}
	for i, handle := range manifest.Federates {
		state := typedRebindingJSON(t, map[string]any{
			"callback_epoch": uint64(3), "callback_recovery": serverRunMode,
			"known_objects":    []any{map[string]any{"object_handle": uint64(1), "class_handle": cls}},
			"pending_removals": []any{map[string]any{"object_handle": uint64(1), "logical_time_value": privateTime}},
			// SDK-only producer is absent from the server graph reference census.
			"deleted_objects": []any{map[string]any{"object_handle": uint64(2), "class_handle": cls,
				"sender_federate": uint64(7<<32 | 4), "retraction_handle": uint64(1<<53 | 1), "originated": false}},
		})
		manifest.FederateClientStates[i], err = sdkstate.Encode(sdkstate.Envelope{
			Language: "go", Scalar: manifest.LogicalTimeImplementationName, Generation: gen, Self: uint64(handle), State: state,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	graph := savebundle.RestoreGraphOf[T]{FOMs: targetRepo, Snapshots: map[string]core.CheckpointSnapshotter{"fom": savebundle.FOM{Repository: targetRepo}, "retractions": typedRebindingLedgerOf[T]{data: ledger([]any{producer(current["sender"], 23)})}}}
	return manager, graph, manifest, model
}

func checkTypedRebindingOf[T core.LogicalTimeScalar](t *testing.T, at T) {
	t.Helper()
	manager, graph, saved, model := typedRebindingFixtureOf(t, at)
	original := typedRebindingJSON(t, saved)
	members := manager.ListMembers(saved.Federation)
	beforeFOM, err := graph.FOMs.Get(context.Background(), saved.Federation)
	if err != nil {
		t.Fatal(err)
	}
	options := savepoint.OptionsOf[T]{RebindRestore: newRestoreRebindingOf[T](manager, graph)}
	result, err := options.RebindRestore(context.Background(), saved.Federation, 9, saved)
	if err != nil {
		t.Fatal(err)
	}
	if result.SaveTime == nil || *result.SaveTime != at || result.FederationGeneration == nil || *result.FederationGeneration != 9 {
		t.Fatalf("typed manifest changed time/generation: %+v", result)
	}
	if !bytes.Equal(original, typedRebindingJSON(t, saved)) {
		t.Fatal("saved manifest mutated")
	}
	if !reflect.DeepEqual(manager.ListMembers(saved.Federation), members) {
		t.Fatal("rebind published membership")
	}
	afterFOM, err := graph.FOMs.Get(context.Background(), saved.Federation)
	if err != nil || beforeFOM != afterFOM {
		t.Fatal("rebind published candidate FOM")
	}
	if err := graph.Validate(saved.Federation, core.CheckpointManifestOf[T](result), model); err != nil {
		t.Fatalf("invalid rebound graph: %v", err)
	}
	if !reflect.DeepEqual(result.FederateTypes, saved.FederateTypes) {
		t.Fatal("participant types changed")
	}
	for i, state := range result.FederateClientStates {
		runtime, err := sdkstate.Decode(state)
		if err != nil || runtime.Generation != 9 || runtime.Self != uint64(result.Federates[i]) || runtime.Scalar != saved.LogicalTimeImplementationName {
			t.Fatalf("private runtime target not rebound: %+v err=%v", runtime, err)
		}
		var private struct {
			Deleted []struct {
				Sender     uint64 `json:"sender_federate"`
				Retraction uint64 `json:"retraction_handle"`
				Object     uint64 `json:"object_handle"`
				Class      uint64 `json:"class_handle"`
			} `json:"deleted_objects"`
		}
		if err := json.Unmarshal(runtime.State, &private); err != nil {
			t.Fatal(err)
		}
		if len(private.Deleted) != 1 || private.Deleted[0].Sender>>32 != 9 || private.Deleted[0].Retraction != 1<<53|1 || private.Deleted[0].Object != 2 {
			t.Fatalf("SDK-only producer/retraction/object rebinding: %+v", private)
		}
	}
	for _, h := range result.Federates {
		if uint64(h)>>32 != 9 {
			t.Fatal("saved source handle survived")
		}
		for _, member := range members {
			if h == member.Handle {
				t.Fatal("fresh identity reused current handle")
			}
		}
	}
	var state struct{ State *object.RegistryStateOf[T] }
	if err := json.Unmarshal(result.ManagerSnapshots["object"], &state); err != nil {
		t.Fatal(err)
	}
	if state.State.Deletions[0].Timestamp != at || state.State.Objects[0].Registrant != result.Federates[0] {
		t.Fatal("deletion timestamp or owner identity changed")
	}
	var timeWire struct {
		Federates []struct {
			CurrentTime T `json:"current_time"`
			Queue       []struct {
				Timestamp T `json:"timestamp"`
				Event     struct {
					LogicalTime *T `json:"logical_time"`
				} `json:"event"`
			} `json:"queue"`
		} `json:"federates"`
	}
	if err := json.Unmarshal(result.ManagerSnapshots["time"], &timeWire); err != nil {
		t.Fatal(err)
	}
	if len(timeWire.Federates) != 1 || timeWire.Federates[0].CurrentTime != at-1 || len(timeWire.Federates[0].Queue) != 1 || timeWire.Federates[0].Queue[0].Timestamp != at || timeWire.Federates[0].Queue[0].Event.LogicalTime == nil || *timeWire.Federates[0].Queue[0].Event.LogicalTime != at {
		t.Fatal("queued exact timestamp changed")
	}
	var ledger struct {
		History struct {
			Producers []struct {
				Producer core.FederateHandle
				Accepted []struct{ First, Last uint64 }
				Messages []struct{ Timestamp T }
			}
		}
	}
	if err := json.Unmarshal(result.ManagerSnapshots["retractions"], &ledger); err != nil {
		t.Fatal(err)
	}
	seen := map[uint64]bool{}
	for _, producer := range ledger.History.Producers {
		if uint64(producer.Producer)>>32 != 9 {
			t.Fatal("historical/current producer not rebound")
		}
		for _, message := range producer.Messages {
			if message.Timestamp != at {
				t.Fatal("ledger timestamp changed")
			}
		}
		for _, rangeEntry := range producer.Accepted {
			for _, id := range []uint64{7, 23, 31} {
				if rangeEntry.First <= id && id <= rangeEntry.Last {
					seen[id] = true
					if id == 23 && producer.Producer != result.Federates[0] {
						t.Fatal("current session mapped by join order instead of saved name")
					}
				}
			}
		}
	}
	if len(seen) != 3 {
		t.Fatalf("session/historical ledger lost: %v", seen)
	}
}

func TestRestoreRebindingInteger64ExactManifest(t *testing.T) {
	for _, at := range []int64{1<<53 + 1, 1<<53 + 7, math.MaxInt64} {
		t.Run(strconv.FormatInt(at, 10), func(t *testing.T) { checkTypedRebindingOf(t, at) })
	}
}

func TestRestoreRebindingFloatCompatibility(t *testing.T) {
	checkTypedRebindingOf(t, core.LogicalTime(3.5))
	manager, graph, saved, model := typedRebindingFixtureOf(t, core.LogicalTime(10))
	result, err := newRestoreRebinding(manager, graph)(context.Background(), saved.Federation, 9, saved)
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.Validate(saved.Federation, core.CheckpointManifest(result), model); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreRebindingTypedFailureDoesNotPublish(t *testing.T) {
	for _, mode := range []string{testCancelledLabel, "wrong-factory", "corrupt-fom", "corrupt-graph", "wrong-generation", "current-ledger", "unknown-runtime"} {
		t.Run(mode, func(t *testing.T) {
			manager, graph, saved, _ := typedRebindingFixtureOf(t, int64(1<<53+7))
			ctx := context.Background()
			generation := uint64(9)
			var want error
			switch mode {
			case testCancelledLabel:
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
				want = context.Canceled
			case "wrong-factory":
				saved.LogicalTimeImplementationName = float64TimeName
			case "corrupt-fom":
				saved.ManagerSnapshots["fom"] = []byte(`{`)
			case "corrupt-graph":
				saved.ManagerSnapshots["time"] = []byte(`{`)
			case "wrong-generation":
				generation = 10
				want = core.ErrFederationGenerationMismatch
			case "current-ledger":
				graph.Snapshots["retractions"] = typedRebindingLedgerOf[int64]{err: errors.New("ledger failure")}
			case "unknown-runtime":
				saved.FederateClientStates[1] = []byte{255, 0}
				want = savepoint.ErrSaveBundleIncompatible
			}
			members := manager.ListMembers(saved.Federation)
			fom, _ := graph.FOMs.Get(ctx, saved.Federation)
			if _, err := newRestoreRebindingOf[int64](manager, graph)(ctx, saved.Federation, generation, saved); err == nil || want != nil && !errors.Is(err, want) {
				t.Fatalf("failed validation accepted or error changed: %v", err)
			}
			if !reflect.DeepEqual(manager.ListMembers(saved.Federation), members) {
				t.Fatal("failure published roster")
			}
			if mode == "unknown-runtime" {
				fresh, err := manager.JoinFederation(ctx, core.JoinFederationRequest{Federation: saved.Federation, FederateName: "allocation-probe"})
				if err != nil || uint64(fresh) != 9<<32|3 {
					t.Fatalf("unknown SDK state consumed restore handles: %d err=%v", fresh, err)
				}
			}
			if mode != testCancelledLabel {
				after, err := graph.FOMs.Get(context.Background(), saved.Federation)
				if err != nil || after != fom {
					t.Fatal("failure published FOM")
				}
			}
		})
	}
}
