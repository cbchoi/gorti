package federate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func TestGoSDKServedCallbackRecoveryInventory(t *testing.T) {
	fields := (&rtiv1.FederateEvent{}).ProtoReflect().Descriptor().Oneofs().ByName("event").Fields()
	if fields.Len() != 43 {
		t.Fatalf("protocol inventory changed: %d oneofs, expected 42 served plus internal barrier", fields.Len())
	}
	for _, scalar := range []string{HLAfloat64Time, HLAinteger64Time} {
		served, data, controls := 0, 0, 0
		for i := 0; i < fields.Len(); i++ {
			field := fields.Get(i)
			if field.Name() == "local_validation_barrier" {
				continue
			}
			served++
			t.Run(scalar+"/"+string(field.Name()), func(t *testing.T) {
				f := runtimeTestFederate(t, scalar)
				f.objectClasses = map[uint64]uint64{80: 19}
				wire := &rtiv1.FederateEvent{CallbackReceipt: bytes.Repeat([]byte{byte(i + 1)}, 32)}
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
						body.Set(fd, protoreflect.ValueOfUint64(27))
					case "federate_handle", "owning_federate":
						body.Set(fd, protoreflect.ValueOfUint64(f.CurrentHandle()))
					case "logical_time_value":
						body.Set(fd, protoreflect.ValueOfMessage(recoveryGrant(t, scalar).GetGrant().LogicalTimeValue.ProtoReflect()))
					}
				}
				source := f.translate(wire)
				if source == nil {
					t.Fatal("served source body dropped")
				}
				if _, invalid := source.(CallbackProtocolError); invalid {
					t.Fatal(source)
				}
				info, ok := runtimeInfo(source)
				identity := sha256.Sum256(wire.CallbackReceipt)
				if !ok || info.identity != hex.EncodeToString(identity[:]) || !bytes.Equal(info.receipt, wire.CallbackReceipt) {
					t.Fatal("source identity or receipt lost")
				}
				if runtimeDataCallback(wire) {
					data++
				} else {
					controls++
				}
				recovered := proto.Clone(wire).(*rtiv1.FederateEvent)
				recovered.CallbackReceipt = bytes.Repeat([]byte{99}, 32)
				recovered.CallbackRecovery = &rtiv1.CallbackRecovery{InvocationIdentity: identity[:]}
				target := runtimeTestFederate(t, scalar)
				target.objectClasses = map[uint64]uint64{80: 19}
				notification := target.translate(recovered)
				if notification == nil {
					t.Fatal("served pending recovery body dropped")
				}
				if _, invalid := notification.(CallbackProtocolError); invalid {
					t.Fatal(notification)
				}
				info, ok = runtimeInfo(notification)
				if !ok || !info.recovered || info.outcomeOnly || info.identity != hex.EncodeToString(identity[:]) {
					t.Fatal("pending recovery metadata lost")
				}
				state := runtimeTestState(t, target)
				if runtimeDataCallback(wire) {
					if len(state.PendingData) != 1 || len(state.PendingControls) != 0 {
						t.Fatal("data metadata misclassified")
					}
				} else if len(state.PendingControls) != 1 || len(state.PendingData) != 0 {
					t.Fatal("control metadata misclassified")
				}
				target.retainCallbackOutcome(notification, true, "", false)
				recovered.CallbackRecovery.OutcomeOnly = true
				carrier := target.translate(recovered)
				if _, ok := carrier.(callbackOutcomeRecovery); !ok {
					t.Fatal("outcome-only exposed a served body")
				}
				if _, err := target.recoveredOutcome(carrier); err != nil {
					t.Fatal(err)
				}
				marker := &rtiv1.FederateEvent{CallbackRecovery: &rtiv1.CallbackRecovery{InvocationIdentity: identity[:], OutcomeOnly: true, Completed: true}}
				for repeat := 0; repeat < 2; repeat++ {
					if target.translate(marker) != nil {
						t.Fatal("completed marker exposed an event")
					}
				}
				if len(runtimeTestState(t, target).PendingOutcomes) != 0 {
					t.Fatal("completed marker did not remove finished outcome")
				}
			})
		}
		if served != 42 || data != 4 || controls != 38 {
			t.Fatalf("inventory served=%d data=%d controls=%d", served, data, controls)
		}
	}
}

func TestGoSDKRestoreCapabilityStagingReview(t *testing.T) {
	f := runtimeTestFederate(t, HLAfloat64Time)
	encoded, err := f.snapshotRuntime()
	if err != nil {
		t.Fatal(err)
	}
	var envelope sdkRuntimeEnvelope
	if err := json.Unmarshal(encoded, &envelope); err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{
		`{"callback_recovery":"server-v3","pending_outcomes":[]}`,
		`{"callback_recovery":"server-v2","pending_outcomes":[],"unsupported_sdk_capability":true}`,
		`{"callback_recovery":"server-v2"}`,
	} {
		envelope.State = json.RawMessage(state)
		envelope.Checksum, _ = runtimeChecksum(envelope)
		unsupported, _ := json.Marshal(envelope)
		event, healthy := f.prepareRestoredCallback(context.Background(), InitiateFederateRestore{Label: "unsupported", ClientState: unsupported})
		if healthy {
			t.Fatal("unsupported runtime became target-ready")
		}
		if _, ok := event.(RestoreStateError); !ok {
			t.Fatal("staging failure not surfaced")
		}
		if f.runtimeStaged != nil || f.runtimeEpoch != 0 || f.CurrentHandle() != 17 {
			t.Fatal("failed capability staging changed target runtime")
		}
	}
	if _, healthy := f.prepareRestoredCallback(context.Background(), InitiateFederateRestore{Label: "supported", ClientState: encoded}); !healthy || f.runtimeStaged == nil {
		t.Fatal("supported v2 runtime failed to stage")
	}
}
