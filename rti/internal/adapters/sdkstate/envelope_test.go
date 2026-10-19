package sdkstate

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	savepoint "github.com/cbchoi/gorti/rti/internal/hla/save_restore"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sampleEnvelope(language, scalar string) Envelope {
	state := json.RawMessage(`{"callback_epoch":7,"known_objects":[{"object_handle":4294967297,"class_handle":4294967297,"owned_attributes":[4294967297]}],"deleted_objects":[{"object_handle":4294967297,"class_handle":4294967297,"sender_federate":4294967298,"retraction_handle":9007199254740993,"originated":false}],"callback_recovery":"server"}`)
	if language == checkpointPython {
		state = json.RawMessage(`{"callback_epoch":7,"logical_time":null,"object_knowledge":{"latest":[[4294967297,9007199254740993]],"retired":[]},"next_retraction_handle":9007199254740993,"used_retraction_handles":[9007199254740992],"pending_callbacks":[],"callback_recovery":"server"}`)
	}
	return Envelope{Language: language, Scalar: scalar, Generation: 1, Self: 1<<32 | 1, State: state}
}

func TestEnvelopeSharedRoundTripExactValuesAndFederateOnlyRewrite(t *testing.T) {
	for _, language := range []string{"go", checkpointPython} {
		for _, scalar := range []string{"HLAfloat64Time", "HLAinteger64Time"} {
			t.Run(language+"/"+scalar, func(t *testing.T) {
				e := sampleEnvelope(language, scalar)
				encoded, err := Encode(e)
				if err != nil {
					t.Fatal(err)
				}
				if err = Validate(context.Background(), savepoint.ClientStateValidationRequest{SourceGeneration: 1, SourceHandle: core.FederateHandle(e.Self), LogicalTimeImplementationName: scalar, State: encoded}); err != nil {
					t.Fatal(err)
				}
				out, err := Rebind(context.Background(), savepoint.ClientStateRebindRequest{SourceGeneration: 1, TargetGeneration: 2, SourceHandle: core.FederateHandle(e.Self), TargetHandle: 2<<32 | 9, LogicalTimeImplementationName: scalar, State: encoded, FederateHandles: map[core.FederateHandle]core.FederateHandle{1<<32 | 1: 2<<32 | 9, 1<<32 | 2: 2<<32 | 10}})
				if err != nil {
					t.Fatal(err)
				}
				rebound, err := Decode(out)
				if err != nil {
					t.Fatal(err)
				}
				if rebound.Generation != 2 || rebound.Self != 2<<32|9 || !bytes.Contains(rebound.State, []byte("9007199254740993")) {
					t.Fatalf("lost exact values: %s", out)
				}
				if language == "go" && (!bytes.Contains(rebound.State, []byte(`"object_handle":4294967297`)) || !bytes.Contains(rebound.State, []byte(`"class_handle":4294967297`)) || !bytes.Contains(rebound.State, []byte(`"sender_federate":8589934602`))) {
					t.Fatalf("rewrote non-federate identity or missed sender: %s", out)
				}
			})
		}
	}
}

func TestEnvelopeRejectsUnknownMalformedDuplicateAndMixedScalar(t *testing.T) {
	e := sampleEnvelope(checkpointPython, "HLAinteger64Time")
	good, err := Encode(e)
	if err != nil {
		t.Fatal(err)
	}
	for _, payload := range [][]byte{[]byte(`{"magic":"UNKNOWN"}`), append(bytes.Clone(good), []byte("{}")...), bytes.Replace(good, []byte(`"magic":"GRS1"`), []byte(`"magic":"GRS1","magic":"GRS1"`), 1), bytes.Replace(good, []byte(`"callback_epoch":7`), []byte(`"callback_epoch":8`), 1)} {
		if _, err := Decode(payload); err == nil {
			t.Fatalf("invalid checkpoint accepted: %s", payload)
		}
	}
	e.State = json.RawMessage(`{"callback_epoch":7,"logical_time":{"float64_value":1.5},"object_knowledge":{"latest":[],"retired":[]},"next_retraction_handle":null,"used_retraction_handles":[],"pending_callbacks":[],"callback_recovery":"server"}`)
	bad, err := Encode(e)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Decode(bad); err == nil {
		t.Fatal("mixed scalar accepted")
	}
}

func TestJavaEmittedFixtureBothScalars(t *testing.T) {
	// Actual Java PortableRuntimeCheckpointTest emitted this Integer64 fixture.
	fixture := []byte(`{"magic":"GRS1","version":1,"language":"java","scalar":"HLAinteger64Time","generation":7,"self":30064771080,"state":{"federation":"fed","label":"saved","known_objects":[{"object_handle":10,"class_handle":20,"name":"known"}],"pending_removals":[],"deleted_objects":[{"object_handle":11,"class_handle":20,"name":"removed","sender_federate":30064771080,"retraction_handle":3,"logical_time_value":{"integer64_value":"9007199254740993"},"expires_at_grant":true}],"cleared_retraction":1,"last_issued_retraction":3,"ddm_regions":[{"region_handle":30,"dimensions":[{"dimension_handle":40,"lower":9223372036854775808,"upper":18446744073709551615}]}],"synchronizations":["sync"],"time_policy_flags":17,"logical_time_value":{"integer64_value":"9007199254740993"},"pending_callbacks":[{"grant":{"logical_time_value":{"integer64_value":"9007199254740993"}}},{"callback_receipt":"b2Jzb2xldGUtdGlja2V0","discover":{"object_handle":"12","object_class_handle":"20","object_name":"data"}},{"callback_receipt":"b2xkLW1hcC10aWNrZXQ=","reflect":{"object_handle":"10","attributes":{"18446744073709551615":"AAEC"}}}]},"checksum":"6864941f8a00a312be701076dd98aa90dd3fba9725bef977ed844300c1540aa9"}`)
	e, err := Decode(fixture)
	if err != nil {
		t.Fatal(err)
	}
	for _, scalar := range []string{"HLAinteger64Time", "HLAfloat64Time"} {
		t.Run(scalar, func(t *testing.T) {
			current := e
			if scalar == "HLAfloat64Time" {
				current.Scalar = scalar
				current.State = bytes.ReplaceAll(current.State, []byte(`"integer64_value":"9007199254740993"`), []byte(`"float64_value":1.25`))
			}
			encoded, err := Encode(current)
			if err != nil {
				t.Fatal(err)
			}
			request := savepoint.ClientStateRebindRequest{Federation: "fed", Label: "saved", SourceGeneration: 7, SourceHandle: core.FederateHandle(current.Self), TargetGeneration: 8, TargetHandle: 8<<32 | 9, LogicalTimeImplementationName: scalar, State: encoded, FederateHandles: map[core.FederateHandle]core.FederateHandle{core.FederateHandle(current.Self): 8<<32 | 9}}
			rewritten, err := Rebind(context.Background(), request)
			if err != nil {
				t.Fatal(err)
			}
			request.ReboundState = rewritten
			request.FederateHandles = nil
			twice, err := Rebind(context.Background(), request)
			if err != nil || !bytes.Equal(twice, rewritten) {
				t.Fatalf("already prepared SDK image translated twice: %v", err)
			}
			target, err := Decode(rewritten)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(target.State, []byte("18446744073709551615")) || !bytes.Contains(target.State, []byte(`"sender_federate":34359738377`)) {
				t.Fatalf("lost DDM unsigned bound or sender: %s", rewritten)
			}
		})
	}
}

func TestPreparedCandidateCannotDropKnowledgeOrSequences(t *testing.T) {
	e := sampleEnvelope("go", "HLAinteger64Time")
	encoded, err := Encode(e)
	if err != nil {
		t.Fatal(err)
	}
	request := savepoint.ClientStateRebindRequest{SourceGeneration: 1, SourceHandle: core.FederateHandle(e.Self), TargetGeneration: 2, TargetHandle: 2<<32 | 9, State: encoded, FederateHandles: map[core.FederateHandle]core.FederateHandle{1<<32 | 1: 2<<32 | 9, 1<<32 | 2: 2<<32 | 10}}
	out, err := Rebind(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	target, err := Decode(out)
	if err != nil {
		t.Fatal(err)
	}
	target.State = bytes.Replace(target.State, []byte("9007199254740993"), []byte("9007199254740992"), 1)
	request.ReboundState, err = Encode(target)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Rebind(context.Background(), request); err == nil {
		t.Fatal("graph candidate silently changed SDK retraction sequence")
	}
}

// This optional campaign reads fixtures freshly emitted by the Java worker and
// writes central-adapter output for independent Java decoder verification.
func TestJavaGeneratedFixtureExchange(t *testing.T) {
	directory := os.Getenv("GORTI_JAVA_SDKSTATE_FIXTURES")
	if directory == "" {
		return
	}
	for _, scalar := range []string{"HLAfloat64Time", "HLAinteger64Time"} {
		t.Run(scalar, func(t *testing.T) {
			input, err := os.ReadFile(filepath.Join(directory, "java-"+scalar+".json"))
			if err != nil {
				t.Fatal(err)
			}
			e, err := Decode(input)
			if err != nil {
				t.Fatal(err)
			}
			output, err := Rebind(context.Background(), savepoint.ClientStateRebindRequest{Federation: "fed", Label: "saved", SourceGeneration: 7, SourceHandle: core.FederateHandle(e.Self), TargetGeneration: 9, TargetHandle: 9<<32 | 41, LogicalTimeImplementationName: scalar, State: input, FederateHandles: map[core.FederateHandle]core.FederateHandle{core.FederateHandle(e.Self): 9<<32 | 41}})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(directory, "central-java-"+scalar+".json"), output, 0600); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSDKPreflightRejectsMalformedRuntimeFields(t *testing.T) {
	for _, change := range []string{"duplicate-nested", "foreign-sender", "zero-object", "unknown-payload", "bad-pending-state", "unsupported-control-capability", "case-variant", "null-epoch", "missing-originated", "missing-integer-grant-time"} {
		t.Run(change, func(t *testing.T) {
			e := sampleEnvelope("go", "HLAinteger64Time")
			switch change {
			case "duplicate-nested":
				e.State = bytes.Replace(e.State, []byte(`"callback_epoch":7`), []byte(`"callback_epoch":7,"callback_epoch":7`), 1)
			case "foreign-sender":
				e.State = bytes.Replace(e.State, []byte(`"sender_federate":4294967298`), []byte(`"sender_federate":8589934594`), 1)
			case "zero-object":
				e.State = bytes.Replace(e.State, []byte(`"object_handle":4294967297`), []byte(`"object_handle":0`), 1)
			case "unknown-payload":
				e.State = bytes.Replace(e.State, []byte(`"callback_epoch":7`), []byte(`"unknown_format_field":7`), 1)
			case "bad-pending-state":
				e.State = bytes.Replace(e.State, []byte(`"callback_epoch":7`), []byte(`"callback_epoch":7,"pending_data":[{"invocation_state":"entered","callback_identity":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]`), 1)
			case "unsupported-control-capability":
				e.State = bytes.Replace(e.State, []byte(`"callback_epoch":7`), []byte(`"callback_epoch":7,"pending_callbacks":[{"callback_receipt":"b2xk","sync_announced":{"label":"saved"}}]`), 1)
			case "case-variant":
				e.State = bytes.Replace(e.State, []byte(`"deleted_objects"`), []byte(`"Deleted_objects"`), 1)
			case "null-epoch":
				e.State = bytes.Replace(e.State, []byte(`"callback_epoch":7`), []byte(`"callback_epoch":null`), 1)
			case "missing-originated":
				e.State = bytes.Replace(e.State, []byte(`,"originated":false`), nil, 1)
			case "missing-integer-grant-time":
				e.State = bytes.Replace(e.State, []byte(`"callback_epoch":7`), []byte(`"callback_epoch":7,"pending_callbacks":[{"grant":{}}]`), 1)
			}
			encoded, err := Encode(e)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = Decode(encoded); err == nil {
				t.Fatal("malformed runtime reached allocation boundary")
			}
		})
	}
}

func TestGoPythonGeneratedFixtureExchange(t *testing.T) {
	paths := os.Getenv("GORTI_SDKSTATE_CROSS_FIXTURES")
	if paths == "" {
		return
	}
	for _, directory := range strings.Split(paths, ";") {
		files, err := os.ReadDir(directory)
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		for _, file := range files {
			if file.IsDir() || !strings.HasSuffix(file.Name(), ".json") || (!strings.HasPrefix(file.Name(), "go-") && !strings.HasPrefix(file.Name(), "python-")) {
				continue
			}
			count++
			t.Run(file.Name(), func(t *testing.T) {
				input, err := os.ReadFile(filepath.Join(directory, file.Name()))
				if err != nil {
					t.Fatal(err)
				}
				e, err := Decode(input)
				if err != nil {
					t.Fatal(err)
				}
				refs, err := References(input)
				if err != nil {
					t.Fatal(err)
				}
				mapping := map[core.FederateHandle]core.FederateHandle{}
				for i, h := range refs {
					mapping[h] = core.FederateHandle(9<<32 | uint64(41+i))
				}
				mapping[core.FederateHandle(e.Self)] = 9<<32 | 41
				output, err := Rebind(context.Background(), savepoint.ClientStateRebindRequest{SourceGeneration: e.Generation, SourceHandle: core.FederateHandle(e.Self), TargetGeneration: 9, TargetHandle: 9<<32 | 41, LogicalTimeImplementationName: e.Scalar, State: input, FederateHandles: mapping})
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(directory, "central-"+file.Name()), output, 0600); err != nil {
					t.Fatal(err)
				}
			})
		}
		if count == 0 {
			t.Fatalf("no actual SDK fixtures in %s", directory)
		}
	}
}
