package federate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	codec "github.com/cbchoi/gorti/rti/internal/adapters/protobuf"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/pkg/logicaltime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func TestCallbackRecoveryFinishedOutcomeExport(t *testing.T) {
	for _, scalar := range []string{HLAfloat64Time, HLAinteger64Time} {
		t.Run(scalar, func(t *testing.T) {
			f := remainingServicesFederate(t, func(_ context.Context, request any, _ *grpc.UnaryServerInfo, _ grpc.UnaryHandler) (any, error) {
				if !request.(*rtiv1.ReportCallbackInvocationRequest).InvocationEntry {
					return nil, status.Error(codes.Unavailable, "outcome unavailable")
				}
				return &rtiv1.Empty{}, nil
			})
			f.logicalTimeImplementationName = scalar
			f.callbackEntrySupported = true
			receipt := bytes.Repeat([]byte{3}, 32)
			event := f.translate(&rtiv1.FederateEvent{CallbackReceipt: receipt, Event: &rtiv1.FederateEvent_SaveInitiate{SaveInitiate: &rtiv1.InitiateFederateSave{Label: "finished"}}})
			appErr, reportErr := f.DispatchEvent(context.Background(), event, func(Event) error {
				if _, err := f.snapshotRuntime(); err == nil {
					t.Error("running control handler was exportable")
				}
				return errors.New("known application failure")
			})
			if appErr == nil || reportErr == nil {
				t.Fatalf("dispatch %v/%v", appErr, reportErr)
			}
			encoded, err := f.snapshotRuntime()
			if err != nil {
				t.Fatal(err)
			}
			var envelope sdkRuntimeEnvelope
			if err := json.Unmarshal(encoded, &envelope); err != nil {
				t.Fatal(err)
			}
			var state struct {
				Recovery string `json:"callback_recovery"`
				Outcomes []struct {
					Identity  string `json:"identity"`
					Success   bool   `json:"success"`
					Exception string `json:"exception"`
				} `json:"pending_outcomes"`
			}
			_ = json.Unmarshal(envelope.State, &state)
			identity := sha256.Sum256(receipt)
			if state.Recovery != runtimeRecoveryServerV2 || len(state.Outcomes) != 1 || state.Outcomes[0].Identity != hex.EncodeToString(identity[:]) || state.Outcomes[0].Success || state.Outcomes[0].Exception != "known application failure" {
				t.Fatalf("finished outcome not preserved: %s", envelope.State)
			}
		})
	}
}

func recoveryGrant(t *testing.T, scalar string) *rtiv1.FederateEvent {
	t.Helper()
	at, _ := logicaltime.Float64(math.Nextafter(2, 3))
	if scalar == HLAinteger64Time {
		at, _ = logicaltime.Integer64(1<<53 + 1)
	}
	representation, err := logicaltime.ParseRepresentation(scalar)
	if err != nil {
		t.Fatal(err)
	}
	value, err := codec.EncodeLogicalTime(representation, at)
	if err != nil {
		t.Fatal(err)
	}
	return &rtiv1.FederateEvent{CallbackReceipt: bytes.Repeat([]byte{11}, 32), Event: &rtiv1.FederateEvent_Grant{Grant: &rtiv1.TimeAdvanceGrant{LogicalTimeValue: value}}}
}

func TestCallbackRecoveryOutcomeOnlyRetryAndRepeatedRestore(t *testing.T) {
	for _, scalar := range []string{HLAfloat64Time, HLAinteger64Time} {
		for _, success := range []bool{true, false} {
			t.Run(scalar+"/"+map[bool]string{true: "success", false: "failure"}[success], func(t *testing.T) {
				var reports atomic.Int32
				var reject atomic.Bool
				reject.Store(true)
				f := remainingServicesFederate(t, func(_ context.Context, request any, _ *grpc.UnaryServerInfo, _ grpc.UnaryHandler) (any, error) {
					r := request.(*rtiv1.ReportCallbackInvocationRequest)
					if r.InvocationEntry {
						t.Error("outcome recovery sent Entry")
					}
					if r.Success != success || (!success && r.Exception != "original failure") {
						t.Error("saved result changed")
					}
					reports.Add(1)
					if reject.Load() {
						return nil, status.Error(codes.Unavailable, "retry")
					}
					return &rtiv1.Empty{}, nil
				})
				base := runtimeTestFederate(t, scalar)
				f.handles, f.logicalTimeImplementationName = base.handles, scalar
				f.conn.fed = base.conn.fed
				f.eventCh = make(chan Event, 16)
				f.federationGeneration = 1
				f.callbackEntrySupported = true
				wire := recoveryGrant(t, scalar)
				identity := sha256.Sum256(wire.CallbackReceipt)
				detail := ""
				if !success {
					detail = "original failure"
				}
				state := sdkRuntimeState{Generation: 1, Self: 29, Epoch: 2, Scalar: scalar, Recovery: runtimeRecoveryServerV2, Objects: []sdkRuntimeObject{}, PendingOutcomes: []sdkRuntimeOutcome{{Identity: hex.EncodeToString(identity[:]), Success: success, Exception: detail}}}
				encoded, err := encodeRuntimeState(state)
				if err != nil {
					t.Fatal(err)
				}
				if err := f.stageRuntimeRestore(InitiateFederateRestore{ClientState: encoded}); err != nil {
					t.Fatal(err)
				}
				h, e := state.Self, state.Epoch
				if err := f.applyRestoredState(context.Background(), FederationRestored{FederateHandle: &h, CallbackEpoch: &e}); err != nil {
					t.Fatal(err)
				}
				f.timeAdvances = []*sdkAdvanceRequest{{flush: true}}
				wire.CallbackReceipt = bytes.Repeat([]byte{12}, 32)
				wire.CallbackRecovery = &rtiv1.CallbackRecovery{InvocationIdentity: identity[:], OutcomeOnly: true}
				event := f.translate(wire)
				if len(f.timeAdvances) != 1 {
					t.Fatal("outcome carrier applied grant side effects")
				}
				body := func(Event) error { t.Error("outcome-only invoked body"); return nil }
				if err := f.ReportCallbackEntry(context.Background(), event); err == nil {
					t.Error("outcome-only Entry accepted")
				}
				if a, r := f.DispatchEvent(context.Background(), event, body); a != nil || r == nil {
					t.Fatalf("retry setup %v/%v", a, r)
				}
				state = runtimeTestState(t, f)
				if len(state.PendingOutcomes) != 1 || state.PendingOutcomes[0].Identity != hex.EncodeToString(identity[:]) || len(state.PendingControls) != 0 {
					t.Fatal("stable outcome lost on repeat save")
				}
				reject.Store(false)
				if a, r := f.DispatchEvent(context.Background(), event, body); a != nil || r != nil {
					t.Fatalf("retry %v/%v", a, r)
				}
				if a, r := f.DispatchEvent(context.Background(), event, body); a != nil || r != nil {
					t.Fatalf("duplicate %v/%v", a, r)
				}
				if reports.Load() != 2 {
					t.Fatalf("same carrier reported %d times", reports.Load())
				}
				state = runtimeTestState(t, f)
				state.Self, state.Epoch = 31, 3
				encoded, _ = encodeRuntimeState(state)
				if err := f.stageRuntimeRestore(InitiateFederateRestore{ClientState: encoded}); err != nil {
					t.Fatal(err)
				}
				h, e = state.Self, state.Epoch
				if err := f.applyRestoredState(context.Background(), FederationRestored{FederateHandle: &h, CallbackEpoch: &e}); err != nil {
					t.Fatal(err)
				}
				if _, err := f.DispatchEvent(context.Background(), event, body); err == nil {
					t.Fatal("old outcome carrier survived restore")
				}
				wire.CallbackReceipt = bytes.Repeat([]byte{13}, 32)
				event = f.translate(wire)
				if a, r := f.DispatchEvent(context.Background(), event, body); a != nil || r != nil {
					t.Fatalf("second restore %v/%v", a, r)
				}
				if reports.Load() != 3 {
					t.Fatal("fresh receipt not redeemed")
				}
				marker := &rtiv1.FederateEvent{CallbackRecovery: &rtiv1.CallbackRecovery{InvocationIdentity: identity[:], OutcomeOnly: true, Completed: true}}
				for i := 0; i < 2; i++ {
					if got := f.translate(marker); got != nil {
						t.Fatal("completed marker exposed body")
					}
				}
				if len(runtimeTestState(t, f).PendingOutcomes) != 0 || reports.Load() != 3 {
					t.Fatal("completed marker not reconciled without RPC")
				}
			})
		}
	}
}

func TestCallbackRecoveryPendingGrantStableIdentity(t *testing.T) {
	for _, scalar := range []string{HLAfloat64Time, HLAinteger64Time} {
		t.Run(scalar, func(t *testing.T) {
			f := runtimeTestFederate(t, scalar)
			wire := recoveryGrant(t, scalar)
			identity := sha256.Sum256(wire.CallbackReceipt)
			wire.CallbackReceipt = bytes.Repeat([]byte{14}, 32)
			wire.CallbackRecovery = &rtiv1.CallbackRecovery{InvocationIdentity: identity[:]}
			f.timeAdvances = []*sdkAdvanceRequest{{flush: true}}
			event := f.translate(wire)
			grant, ok := event.(TimeAdvanceGrant)
			expected, err := codec.DecodeLogicalTime(f.timeRepresentation(), wire.GetGrant().LogicalTimeValue)
			if err != nil || !ok || grant.TimeValue != expected {
				t.Fatalf("exact grant missing: %#v", event)
			}
			if len(f.timeAdvances) != 1 {
				t.Fatal("pending replay reapplied post-receive grant state")
			}
			state := runtimeTestState(t, f)
			if len(state.PendingControls) != 1 || state.PendingControls[0].Identity != hex.EncodeToString(identity[:]) {
				t.Fatal("restored pending identity was rehashed from fresh receipt")
			}
		})
	}
}

func TestCallbackRecoveryHistoricLifecycleNotificationOnly(t *testing.T) {
	f := runtimeTestFederate(t, HLAfloat64Time)
	h, e := uint64(999), uint64(888)
	identity := sha256.Sum256([]byte("historic"))
	wire := &rtiv1.FederateEvent{CallbackReceipt: bytes.Repeat([]byte{21}, 32), CallbackRecovery: &rtiv1.CallbackRecovery{InvocationIdentity: identity[:]}, Event: &rtiv1.FederateEvent_RestoreCompleted{RestoreCompleted: &rtiv1.FederationRestored{Label: "historic", FederateHandle: &h, CallbackEpoch: &e}}}
	event := f.translate(wire)
	state := runtimeTestState(t, f)
	state.Self = 29
	encoded, _ := encodeRuntimeState(state)
	if err := f.stageRuntimeRestore(InitiateFederateRestore{ClientState: encoded}); err != nil {
		t.Fatal(err)
	}
	staged := f.runtimeStaged
	prepared, healthy := f.prepareRestoredCallback(context.Background(), event)
	if !healthy || f.CurrentHandle() != 17 || f.runtimeStaged != staged || prepared.(FederationRestored).CallbackInfo.federate != 17 {
		t.Fatal("historic restore terminal rebound live SDK identity")
	}
	f.eventCh <- FederationSaved{Label: "must remain"}
	if !f.publishRuntimeEvent(context.Background(), prepared) || len(f.eventCh) != 2 {
		t.Fatal("historic terminal purged live queue")
	}
}

func TestCallbackRecoveryUnknownAndMalformedFailClosed(t *testing.T) {
	f := runtimeTestFederate(t, HLAfloat64Time)
	wire := recoveryGrant(t, HLAfloat64Time)
	identity := sha256.Sum256(wire.CallbackReceipt)
	wire.CallbackRecovery = &rtiv1.CallbackRecovery{InvocationIdentity: identity[:], OutcomeOnly: true}
	event := f.translate(wire)
	if _, err := f.DispatchEvent(context.Background(), event, func(Event) error { t.Error("unknown outcome body invoked"); return nil }); err == nil {
		t.Fatal("unknown outcome reported")
	}
	if _, err := f.snapshotRuntime(); err == nil {
		t.Fatal("unknown recovery outcome checkpointed")
	}
	before := len(f.runtimePending)
	for _, recovery := range []*rtiv1.CallbackRecovery{{InvocationIdentity: []byte{1}, OutcomeOnly: true}, {InvocationIdentity: identity[:], Completed: true}, {InvocationIdentity: identity[:], Completed: true, OutcomeOnly: true}} {
		wire.CallbackRecovery = recovery
		if _, ok := f.translate(wire).(CallbackProtocolError); !ok {
			t.Fatal("malformed recovery carrier accepted")
		}
		if len(f.runtimePending) != before {
			t.Fatal("malformed recovery mutated queue")
		}
	}
}

func TestCallbackRecoveryV2Fixtures(t *testing.T) {
	for _, scalar := range []string{HLAfloat64Time, HLAinteger64Time} {
		t.Run(scalar, func(t *testing.T) {
			f := runtimeTestFederate(t, scalar)
			f.federationGeneration = 7
			f.federateHandle = 7<<32 | 8
			f.translate(recoveryGrant(t, scalar))
			outcomeIdentity := sha256.Sum256([]byte("fixture-finished-outcome"))
			state := runtimeTestState(t, f)
			state.PendingOutcomes = append(state.PendingOutcomes, sdkRuntimeOutcome{Identity: hex.EncodeToString(outcomeIdentity[:]), Success: false, Exception: "fixture callback failure"})
			encoded, err := encodeRuntimeState(state)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.decodeRuntime(encoded); err != nil {
				t.Fatal(err)
			}
			if folder := os.Getenv("GORTI_GO_RUNTIME_V2_FIXTURES"); folder != "" {
				if err := os.MkdirAll(folder, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(folder, "go-v2-"+scalar+".json"), encoded, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if folder := os.Getenv("GORTI_GO_RUNTIME_V2_REBOUND_FIXTURES"); folder != "" {
				rebound, err := os.ReadFile(filepath.Join(folder, "central-go-v2-"+scalar+".json"))
				if err != nil {
					t.Fatal(err)
				}
				target := runtimeTestFederate(t, scalar)
				target.federationGeneration = 9
				prepared, err := target.decodeRuntime(rebound)
				if err != nil {
					t.Fatal(err)
				}
				if prepared.state.Self != 9<<32|41 || len(prepared.state.PendingControls) != 1 || prepared.state.PendingControls[0] != state.PendingControls[0] || len(prepared.state.PendingOutcomes) != 1 || prepared.state.PendingOutcomes[0] != state.PendingOutcomes[0] {
					t.Fatal("central rebind changed control identity or known failed result")
				}
				t.Logf("independent central fixture decode PASS scalar=%s generation=%d self=%d", scalar, prepared.state.Generation, prepared.state.Self)
			}
		})
	}
}

func TestCallbackRecoveryFinishedOutcomeIsImmutable(t *testing.T) {
	var calls atomic.Int32
	f := remainingServicesFederate(t, func(context.Context, any, *grpc.UnaryServerInfo, grpc.UnaryHandler) (any, error) {
		calls.Add(1)
		return &rtiv1.Empty{}, nil
	})
	event := f.translate(recoveryGrant(t, HLAfloat64Time))
	if err := f.ReportCallbackInvocation(context.Background(), event, false, "original failure"); err != nil {
		t.Fatal(err)
	}
	if err := f.ReportCallbackInvocation(context.Background(), event, true, ""); err == nil {
		t.Fatal("known finished result overwritten by guessed success")
	}
	if calls.Load() != 1 {
		t.Fatal("conflicting outcome reached server")
	}
}

func TestCallbackRecoveryV2InvalidStateAtomicAndLegacy(t *testing.T) {
	f := runtimeTestFederate(t, HLAfloat64Time)
	valid, err := f.snapshotRuntime()
	if err != nil {
		t.Fatal(err)
	}
	var envelope sdkRuntimeEnvelope
	_ = json.Unmarshal(valid, &envelope)
	identity := hex.EncodeToString(bytes.Repeat([]byte{10}, 32))
	cases := map[string]string{
		"missing":              `{"callback_recovery":"server-v2"}`,
		"null":                 `{"callback_recovery":"server-v2","pending_outcomes":null}`,
		"success-missing":      `{"callback_recovery":"server-v2","pending_outcomes":[{"identity":"` + identity + `","exception":"failure"}]}`,
		"exception-missing":    `{"callback_recovery":"server-v2","pending_outcomes":[{"identity":"` + identity + `","success":true}]}`,
		"invalid-result":       `{"callback_recovery":"server-v2","pending_outcomes":[{"identity":"` + identity + `","success":false,"exception":""}]}`,
		"unknown-result-field": `{"callback_recovery":"server-v2","pending_outcomes":[{"identity":"` + identity + `","success":true,"exception":"","inflight":true}]}`,
		"upper-identity":       `{"callback_recovery":"server-v2","pending_outcomes":[{"identity":"` + strings.ToUpper(hex.EncodeToString(bytes.Repeat([]byte{0xab}, 32))) + `","success":true,"exception":""}]}`,
		"duplicate-phase":      `{"callback_recovery":"server-v2","pending_controls":[{"invocation_state":"unentered","callback_identity":"` + identity + `"}],"pending_outcomes":[{"identity":"` + identity + `","success":true,"exception":""}]}`,
		"legacy-outcome":       `{"callback_recovery":"server","pending_outcomes":[{"identity":"` + identity + `","success":true,"exception":""}]}`,
		"local-v2-body":        `{"callback_recovery":"server-v2","pending_outcomes":[],"pending_callbacks":[{"grant":{"logical_time":1}}]}`,
	}
	for name, state := range cases {
		t.Run(name, func(t *testing.T) {
			envelope.State = json.RawMessage(state)
			envelope.Checksum, _ = runtimeChecksum(envelope)
			encoded, _ := json.Marshal(envelope)
			if err := f.stageRuntimeRestore(InitiateFederateRestore{ClientState: encoded}); err == nil {
				t.Fatal("invalid v2 state accepted before terminal")
			}
			if f.runtimeStaged != nil || f.runtimeEpoch != 0 || f.CurrentHandle() != 17 {
				t.Fatal("failed staging mutated runtime")
			}
		})
	}
	envelope.State = json.RawMessage(`{"callback_recovery":"server","known_objects":[]}`)
	envelope.Checksum, _ = runtimeChecksum(envelope)
	legacy, _ := json.Marshal(envelope)
	if _, err := f.decodeRuntime(legacy); err != nil {
		t.Fatalf("legacy v1 rejected: %v", err)
	}
}

func TestCallbackRecoveryOldInFlightReportCannotRepopulateRestoredLedger(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	f := remainingServicesFederate(t, func(context.Context, any, *grpc.UnaryServerInfo, grpc.UnaryHandler) (any, error) {
		close(entered)
		<-release
		return &rtiv1.Empty{}, nil
	})
	base := runtimeTestFederate(t, HLAfloat64Time)
	f.conn.fed, f.handles = base.conn.fed, base.handles
	f.logicalTimeImplementationName = HLAfloat64Time
	event := f.translate(recoveryGrant(t, HLAfloat64Time))
	done := make(chan error, 1)
	go func() { done <- f.ReportCallbackInvocation(context.Background(), event, true, "") }()
	<-entered
	if _, err := f.snapshotRuntime(); err != nil {
		t.Errorf("known finished report in flight was not exportable: %v", err)
	}
	h, e := uint64(29), uint64(2)
	err := f.applyRestoredState(context.Background(), FederationRestored{FederateHandle: &h, CallbackEpoch: &e})
	close(release)
	if err != nil {
		t.Fatal(err)
	}
	if err := <-done; err == nil {
		t.Fatal("old report crossed restored epoch")
	}
	if len(f.genericCallbacks) != 0 || len(f.runtimePending) != 0 {
		t.Fatal("old report repopulated restored ledger")
	}
}

func TestCallbackRecoveryAllStreamControlInventory(t *testing.T) {
	fields := (&rtiv1.FederateEvent{}).ProtoReflect().Descriptor().Oneofs().ByName("event").Fields()
	for _, scalar := range []string{HLAfloat64Time, HLAinteger64Time} {
		for i := 0; i < fields.Len(); i++ {
			field := fields.Get(i)
			switch field.Name() {
			case "discover", "reflect", "remove", "receive", "local_validation_barrier":
				continue
			}
			t.Run(scalar+"/"+string(field.Name()), func(t *testing.T) {
				var entries, outcomes, activations atomic.Int32
				var transportation *rtiv1.TransportationChange
				f := remainingServicesFederate(t, func(_ context.Context, request any, _ *grpc.UnaryServerInfo, _ grpc.UnaryHandler) (any, error) {
					switch request := request.(type) {
					case *rtiv1.ReportCallbackInvocationRequest:
						if request.InvocationEntry {
							entries.Add(1)
						} else {
							outcomes.Add(1)
						}
						return &rtiv1.Empty{}, nil
					case *rtiv1.ActivateTransportationTypeChangeRequest:
						activations.Add(1)
						if request.ExpectedFederationGeneration != transportation.FederationGeneration || request.ExpectedCallbackEpoch != transportation.CallbackEpoch || !bytes.Equal(request.Ticket, transportation.Ticket) {
							t.Error("transport recovery activation lost authoritative ticket scope")
						}
						return proto.Clone(transportation), nil
					default:
						t.Errorf("unexpected recovery RPC %T", request)
						return nil, status.Error(codes.Internal, "unexpected RPC")
					}
				})
				base := runtimeTestFederate(t, scalar)
				f.handles, f.logicalTimeImplementationName = base.handles, scalar
				f.federationGeneration, f.callbackEntrySupported = 1, true
				f.objectClasses = map[uint64]uint64{80: 19}
				f.timeAdvances = []*sdkAdvanceRequest{{flush: true}}
				identity := sha256.Sum256([]byte(scalar + "/" + string(field.Name())))
				wire := &rtiv1.FederateEvent{CallbackReceipt: bytes.Repeat([]byte{31}, 32), CallbackRecovery: &rtiv1.CallbackRecovery{InvocationIdentity: identity[:]}}
				body := wire.ProtoReflect().Mutable(field).Message()
				bodyFields := body.Descriptor().Fields()
				for j := 0; j < bodyFields.Len(); j++ {
					fd := bodyFields.Get(j)
					switch fd.Name() {
					case "object_handle":
						body.Set(fd, protoreflect.ValueOfUint64(80))
					case "object_class_handle":
						body.Set(fd, protoreflect.ValueOfUint64(19))
					case "interaction_class_handle":
						if field.Name() != "transportation_change_confirmed" {
							body.Set(fd, protoreflect.ValueOfUint64(27))
						}
					case "federate_handle", "owning_federate":
						body.Set(fd, protoreflect.ValueOfUint64(f.CurrentHandle()))
					case "federation_generation":
						body.Set(fd, protoreflect.ValueOfUint64(1))
					case "callback_epoch":
						body.Set(fd, protoreflect.ValueOfUint64(f.CallbackEpoch()))
					case "ticket":
						body.Set(fd, protoreflect.ValueOfBytes(bytes.Repeat([]byte{32}, 32)))
					case "transport_type":
						body.Set(fd, protoreflect.ValueOfEnum(1))
					case "logical_time_value":
						body.Set(fd, protoreflect.ValueOfMessage(recoveryGrant(t, scalar).GetGrant().LogicalTimeValue.ProtoReflect()))
					}
				}
				transportation = wire.GetTransportationChangeConfirmed()
				event := f.translate(wire)
				if event == nil {
					t.Fatal("control inventory callback not reconstructed")
				}
				if _, invalid := event.(CallbackProtocolError); invalid {
					t.Fatal(event)
				}
				if prepared, healthy := f.prepareRestoredCallback(context.Background(), event); !healthy || prepared == nil || f.runtimeStaged != nil || f.CurrentHandle() != 7 {
					t.Fatal("historic control changed lifecycle state")
				}
				if len(f.timeAdvances) != 1 {
					t.Fatal("recovered control reapplied time side effects")
				}
				state := runtimeTestState(t, f)
				if len(state.PendingControls) != 1 || state.PendingControls[0].Identity != hex.EncodeToString(identity[:]) {
					t.Fatal("pending control not portable identity metadata")
				}
				bodies := 0
				for retry := 0; retry < 2; retry++ {
					if a, r := f.DispatchEvent(context.Background(), event, func(Event) error { bodies++; return nil }); a != nil || r != nil {
						t.Fatalf("pending control dispatch %v/%v", a, r)
					}
				}
				if bodies != 1 || entries.Load() != 1 || outcomes.Load() != 1 {
					t.Fatal("pending control did not dispatch/report once")
				}
				wire.CallbackReceipt = bytes.Repeat([]byte{33}, 32)
				wire.CallbackRecovery.OutcomeOnly = true
				if transportation != nil {
					transportation.Ticket = nil
					transportation.FederationGeneration = 0
					transportation.CallbackEpoch = 0
				}
				event = f.translate(wire)
				if a, r := f.DispatchEvent(context.Background(), event, func(Event) error { t.Error("finished control body replayed"); return nil }); a != nil || r != nil {
					t.Fatalf("outcome-only control %v/%v", a, r)
				}
				if entries.Load() != 1 || outcomes.Load() != 2 {
					t.Fatal("outcome-only control sent Entry or failed to report")
				}
				if transportation != nil && activations.Load() != 1 {
					t.Fatal("transport activation was bypassed or repeated by outcome-only")
				}
			})
		}
	}
}

func TestCallbackRecoveryPendingControlMetadata(t *testing.T) {
	f := runtimeTestFederate(t, HLAfloat64Time)
	receipt := bytes.Repeat([]byte{9}, 32)
	f.translate(&rtiv1.FederateEvent{CallbackReceipt: receipt, Event: &rtiv1.FederateEvent_Grant{Grant: &rtiv1.TimeAdvanceGrant{LogicalTime: 1}}})
	encoded, err := f.snapshotRuntime()
	if err != nil {
		t.Fatal(err)
	}
	var envelope sdkRuntimeEnvelope
	_ = json.Unmarshal(encoded, &envelope)
	var state map[string]json.RawMessage
	_ = json.Unmarshal(envelope.State, &state)
	var pending []sdkRuntimePendingData
	_ = json.Unmarshal(state["pending_controls"], &pending)
	identity := sha256.Sum256(receipt)
	if len(pending) != 1 || pending[0].Identity != hex.EncodeToString(identity[:]) || pending[0].InvocationState != runtimeInvocationUnentered || len(state["pending_callbacks"]) != 0 || string(state["pending_outcomes"]) != "[]" {
		t.Fatalf("pending control authority leaked or lost: %s", envelope.State)
	}
}
