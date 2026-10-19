package sdkstate

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	savepoint "github.com/cbchoi/gorti/rti/internal/hla/save_restore"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func withRecoveryCallbacks(t *testing.T, e Envelope, wire *rtiv1.FederateEvent) Envelope {
	t.Helper()
	callback, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(e.State, &fields)
	fields["pending_callbacks"] = append(append([]byte{'['}, callback...), ']')
	e.State, err = json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestRecoveryAllStreamCallbackBodiesAndExplicitFederateReferences(t *testing.T) {
	descriptor := new(rtiv1.FederateEvent).ProtoReflect().Descriptor().Oneofs().ByName("event")
	for _, language := range []string{"go", "python", "java"} {
		for _, scalar := range []string{"HLAfloat64Time", "HLAinteger64Time"} {
			for i := 0; i < descriptor.Fields().Len(); i++ {
				field := descriptor.Fields().Get(i)
				t.Run(language+"/"+scalar+"/"+string(field.Name()), func(t *testing.T) {
					e := recoveryEnvelope(t, language, scalar, "[]")
					wire := new(rtiv1.FederateEvent)
					wire.CallbackReceipt = make([]byte, 32)
					body := wire.ProtoReflect().Mutable(field).Message()
					for j := 0; j < body.Descriptor().Fields().Len(); j++ {
						f := body.Descriptor().Fields().Get(j)
						if isFederateField(f.Name()) {
							if f.IsList() {
								body.Mutable(f).List().Append(protoreflect.ValueOfUint64(e.Self))
							} else {
								body.Set(f, protoreflect.ValueOfUint64(e.Self))
							}
						}
						if f.Name() == "federate_handle" {
							body.Set(f, protoreflect.ValueOfUint64(7<<32|81))
						}
						if f.Kind() == protoreflect.MessageKind && f.Message().FullName() == "rti.v1.LogicalTimeValue" {
							value := body.Mutable(f).Message()
							if scalar == "HLAinteger64Time" {
								value.Set(value.Descriptor().Fields().ByName("integer64_value"), protoreflect.ValueOfInt64(9007199254740993))
							} else {
								value.Set(value.Descriptor().Fields().ByName("float64_value"), protoreflect.ValueOfFloat64(1.25))
							}
						}
					}
					e = withRecoveryCallbacks(t, e, wire)
					encoded, err := Encode(e)
					if err != nil {
						t.Fatal(err)
					}
					_, err = Decode(encoded)
					if field.Name() == "local_validation_barrier" || field.Name() == "halted" {
						if err == nil {
							t.Fatal("internal marker admitted as recoverable HLA body")
						}
						return
					}
					if err != nil {
						t.Fatal(err)
					}
					manifest := recoveryManifest(t, e)
					identity := sha256.Sum256(wire.CallbackReceipt)
					row := savepoint.CallbackRecoveryRequirement{Recipient: core.FederateHandle(e.Self), InvocationIdentity: identity[:], Phase: "pending"}
					if err := ReconcileManifestVersionOf(context.Background(), manifest, 2, []savepoint.CallbackRecoveryRequirement{row}); err != nil {
						t.Fatal(err)
					}
					if err := ReconcileManifestVersionOf(context.Background(), manifest, 2, nil); err == nil {
						t.Fatal("orphan queue identity accepted")
					}
					target, err := Rebind(context.Background(), savepoint.ClientStateRebindRequest{Federation: "fed", Label: "saved", SourceGeneration: 1, TargetGeneration: 2, SourceHandle: core.FederateHandle(e.Self), TargetHandle: 2<<32 | 9, LogicalTimeImplementationName: scalar, State: encoded, FederateHandles: map[core.FederateHandle]core.FederateHandle{core.FederateHandle(e.Self): 2<<32 | 9, 1<<32 | 2: 2<<32 | 10}})
					if err != nil {
						t.Fatal(err)
					}
					if _, err := Decode(target); err != nil {
						t.Fatal(err)
					}
				})
			}
		}
	}
}

func TestRecoveryCarrierValidationAndStableIdentity(t *testing.T) {
	id := sha256.Sum256([]byte("stable source receipt"))
	for _, test := range []struct {
		name  string
		wire  *rtiv1.FederateEvent
		valid bool
	}{
		{"completed", &rtiv1.FederateEvent{CallbackRecovery: &rtiv1.CallbackRecovery{InvocationIdentity: id[:], OutcomeOnly: true, Completed: true}}, true},
		{"completed-with-receipt", &rtiv1.FederateEvent{CallbackReceipt: make([]byte, 32), CallbackRecovery: &rtiv1.CallbackRecovery{InvocationIdentity: id[:], OutcomeOnly: true, Completed: true}}, false},
		{"completed-with-body", &rtiv1.FederateEvent{CallbackRecovery: &rtiv1.CallbackRecovery{InvocationIdentity: id[:], OutcomeOnly: true, Completed: true}, Event: &rtiv1.FederateEvent_RestoreBegun{RestoreBegun: &rtiv1.FederationRestoreBegun{}}}, false},
		{"completed-not-outcome-only", &rtiv1.FederateEvent{CallbackRecovery: &rtiv1.CallbackRecovery{InvocationIdentity: id[:], Completed: true}}, false},
		{"bad-identity", &rtiv1.FederateEvent{CallbackRecovery: &rtiv1.CallbackRecovery{InvocationIdentity: []byte{1}, OutcomeOnly: true, Completed: true}}, false},
		{"unknown-finished-result", &rtiv1.FederateEvent{CallbackReceipt: make([]byte, 32), CallbackRecovery: &rtiv1.CallbackRecovery{InvocationIdentity: id[:], OutcomeOnly: true}, Event: &rtiv1.FederateEvent_RestoreBegun{RestoreBegun: &rtiv1.FederationRestoreBegun{}}}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			e := withRecoveryCallbacks(t, recoveryEnvelope(t, "go", "HLAfloat64Time", "[]"), test.wire)
			encoded, _ := Encode(e)
			_, err := Decode(encoded)
			if (err == nil) != test.valid {
				t.Fatalf("valid=%v err=%v", test.valid, err)
			}
		})
	}
	wire := &rtiv1.FederateEvent{CallbackReceipt: make([]byte, 32), CallbackRecovery: &rtiv1.CallbackRecovery{InvocationIdentity: id[:]}}
	if stableCallbackIdentity(wire) == stableCallbackIdentity(&rtiv1.FederateEvent{CallbackReceipt: wire.CallbackReceipt}) {
		t.Fatal("restored identity rehashed from fresh receipt")
	}
}
