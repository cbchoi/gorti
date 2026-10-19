package sdkstate

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	savepoint "github.com/cbchoi/gorti/rti/internal/hla/save_restore"
)

func localNotificationState(t *testing.T, language, scalar, kind, scope, payload string) Envelope {
	t.Helper()
	e := recoveryEnvelope(t, language, scalar, "[]")
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(e.State, &fields)
	fields["pending_local_notifications"] = json.RawMessage(`[{"local_id":"` + strings.Repeat("ab", 32) + `","order":9007199254740993,"scope":"` + scope + `","kind":"` + kind + `","payload":` + payload + `}]`)
	var err error
	e.State, err = json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestLocalNotificationsAllowlistBothFamiliesAndFederateOnlyRebind(t *testing.T) {
	for _, test := range []struct{ kind, scope, payload string }{
		{"informAttributeOwnership", "member", `{"object_handle":4294967298,"attribute_handle":4294967298,"owner_federate":4294967298}`},
		{"attributeIsNotOwned", "member", `{"object_handle":4294967298,"attribute_handle":4294967298}`},
		{"attributeIsOwnedByRTI", "member", `{"object_handle":4294967298,"attribute_handle":4294967298}`},
		{"confirmAttributeOwnershipAcquisitionCancellation", "member", `{"object_handle":4294967298,"attribute_handles":[4294967298]}`},
		{"confirmAttributeTransportationTypeChange", "member", `{"object_handle":4294967298,"attribute_handles":[4294967298],"transport_type":2}`},
		{"confirmInteractionTransportationTypeChange", "member", `{"interaction_class_handle":4294967298,"transport_type":1}`},
		{"reportAttributeTransportationType", "member", `{"object_handle":4294967298,"attribute_handle":4294967298,"transport_type":1}`},
		{"reportInteractionTransportationType", "member", `{"federate_handle":4294967298,"interaction_class_handle":4294967298,"transport_type":2}`},
		{"federationSaveStatusResponse", "member", `{"state":1,"active_label":"historical-save","statuses":[{"federate_handle":4294967298,"status":1}]}`},
		{"federationRestoreStatusResponse", "member", `{"state":4,"active_label":"historical-restore","statuses":[{"pre_restore_handle":4294967298,"post_restore_handle":30064771153,"status":6}]}`},
		{"reportFederationExecutions", "connection", `{"executions":[{"name":"other","logical_time_implementation_name":"HLAinteger64Time","mode":2,"federates_joined":3,"federation_generation":18446744073709551615}]}`},
	} {
		for _, language := range []string{"python", "java"} {
			if language == "python" && (test.kind == "informAttributeOwnership" || test.kind == "attributeIsNotOwned" || test.kind == "attributeIsOwnedByRTI" || test.kind == "confirmAttributeOwnershipAcquisitionCancellation") {
				continue
			}
			for _, scalar := range []string{"HLAfloat64Time", "HLAinteger64Time"} {
				t.Run(language+"/"+scalar+"/"+test.kind, func(t *testing.T) {
					e := localNotificationState(t, language, scalar, test.kind, test.scope, test.payload)
					encoded, _ := Encode(e)
					if _, err := Decode(encoded); err != nil {
						t.Fatal(err)
					}
					// Local records must not acquire source ledger obligations.
					if err := ReconcileManifestVersionOf(context.Background(), recoveryManifest(t, e), 2, nil); err != nil {
						t.Fatal(err)
					}
					refs, err := References(encoded)
					if err != nil {
						t.Fatal(err)
					}
					for _, ref := range refs {
						if ref == 30064771153 {
							t.Fatal("historical saved status handle entered allocation census")
						}
					}
					out, err := Rebind(context.Background(), savepoint.ClientStateRebindRequest{Federation: "fed", Label: "saved", SourceGeneration: 1, TargetGeneration: 2, SourceHandle: core.FederateHandle(e.Self), TargetHandle: 2<<32 | 9, LogicalTimeImplementationName: scalar, State: encoded, FederateHandles: map[core.FederateHandle]core.FederateHandle{1<<32 | 1: 2<<32 | 9, 1<<32 | 2: 2<<32 | 10}})
					if err != nil {
						t.Fatal(err)
					}
					target, err := Decode(out)
					if err != nil {
						t.Fatal(err)
					}
					var fields map[string]json.RawMessage
					_ = json.Unmarshal(target.State, &fields)
					locals := fields["pending_local_notifications"]
					if !bytes.Contains(locals, []byte(`"order":9007199254740993`)) || !bytes.Contains(locals, []byte(strings.Repeat("ab", 32))) {
						t.Fatal("local identity/order changed")
					}
					if strings.Contains(test.payload, "post_restore_handle") && !bytes.Contains(locals, []byte(`"post_restore_handle":30064771153`)) {
						t.Fatal("historical saved handle changed")
					}
					if strings.Contains(test.payload, "object_handle") && !bytes.Contains(locals, []byte(`"object_handle":4294967298`)) {
						t.Fatal("object mistaken for federate")
					}
					if strings.Contains(test.payload, "interaction_class_handle") && !bytes.Contains(locals, []byte(`"interaction_class_handle":4294967298`)) {
						t.Fatal("class mistaken for federate")
					}
					if test.kind == "informAttributeOwnership" && !bytes.Contains(locals, []byte(`"owner_federate":8589934602`)) {
						t.Fatal("owner not rebound")
					}
					if test.kind == "federationRestoreStatusResponse" && !bytes.Contains(locals, []byte(`"pre_restore_handle":8589934602`)) {
						t.Fatal("live pre-restore reference not rebound")
					}
					if test.scope == "connection" && !bytes.Contains(locals, []byte("18446744073709551615")) {
						t.Fatal("foreign discovery generation changed")
					}
				})
			}
		}
	}
}

func TestLocalNotificationsStrictUnknownCapabilitiesAndInvalidValues(t *testing.T) {
	base := localNotificationState(t, "python", "HLAinteger64Time", "reportInteractionTransportationType", "member", `{"federate_handle":4294967298,"interaction_class_handle":21,"transport_type":1}`)
	for _, change := range []struct{ name, old, new string }{
		{"unknown-kind", "reportInteractionTransportationType", "activateTransportationTicket"},
		{"wrong-scope", `"scope":"member"`, `"scope":"connection"`},
		{"receipt", `"payload":{`, `"payload":{"callback_receipt":"source-capability",`},
		{"ticket", `"payload":{`, `"payload":{"prepared_ticket":7,`},
		{"row-cap", `"local_id":`, `"ownership_receipt":{},"local_id":`},
		{"null-order", `"order":9007199254740993`, `"order":null`},
		{"zero-order", `"order":9007199254740993`, `"order":0`},
		{"bad-id", strings.Repeat("ab", 32), strings.Repeat("AB", 32)},
		{"foreign-live", `"federate_handle":4294967298`, `"federate_handle":8589934594`},
		{"zero-live", `"federate_handle":4294967298`, `"federate_handle":0`},
		{"mixed-number", `"federate_handle":4294967298`, `"federate_handle":"4294967298"`},
		{"unknown-transport", `"transport_type":1`, `"transport_type":3`},
		{"case-field", `"federate_handle":`, `"Federate_handle":`},
		{"missing-field", `"transport_type":1`, `"unrecognized":1`},
		{"legacy-authority", `"server-v2"`, `"server"`},
	} {
		t.Run(change.name, func(t *testing.T) {
			e := base
			e.State = bytes.Replace(e.State, []byte(change.old), []byte(change.new), 1)
			encoded, _ := Encode(e)
			if _, err := Decode(encoded); err == nil {
				t.Fatal("invalid local record accepted")
			}
		})
	}
	for _, payload := range []string{
		`{"statuses":null}`, `{"statuses":[{"pre_restore_handle":4294967298,"post_restore_handle":null,"status":1}]}`, `{"state":null,"statuses":[]}`, `{"statuses":[{"pre_restore_handle":4294967298,"post_restore_handle":0,"status":7}]}`, `{"statuses":[{"pre_restore_handle":4294967298,"post_restore_handle":0,"status":1},{"pre_restore_handle":4294967298,"post_restore_handle":0,"status":2}]}`,
	} {
		e := localNotificationState(t, "java", "HLAfloat64Time", "federationRestoreStatusResponse", "member", payload)
		encoded, _ := Encode(e)
		if _, err := Decode(encoded); err == nil {
			t.Fatalf("invalid vector accepted: %s", payload)
		}
	}
}

func TestLocalNotificationsRejectUnsupportedLanguageReaderBeforeAllocation(t *testing.T) {
	for _, scalar := range []string{"HLAfloat64Time", "HLAinteger64Time"} {
		for _, language := range []string{"go", "python"} {
			e := localNotificationState(t, language, scalar, "attributeIsNotOwned", "member", `{"object_handle":10,"attribute_handle":20}`)
			encoded, _ := Encode(e)
			if _, err := Decode(encoded); err == nil {
				t.Fatalf("unsupported %s reader notification admitted", language)
			}
			if _, err := References(encoded); err == nil {
				t.Fatalf("unsupported %s reader reached reference allocation", language)
			}
		}
	}
	e := recoveryEnvelope(t, "go", "HLAinteger64Time", "[]")
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(e.State, &fields)
	fields["pending_local_notifications"] = json.RawMessage(`[]`)
	e.State, _ = json.Marshal(fields)
	encoded, _ := Encode(e)
	if _, err := Decode(encoded); err == nil {
		t.Fatal("Go reader cannot stage even an explicit empty local notification field")
	}
}

func TestLocalNotificationsStrictDiscoveryAndHistoricalStatus(t *testing.T) {
	for _, scalar := range []string{"HLAfloat64Time", "HLAinteger64Time"} {
		for _, payload := range []string{
			`{"executions":[{"name":"","logical_time_implementation_name":"HLAfloat64Time"}]}`,
			`{"executions":[{"name":"other","logical_time_implementation_name":""}]}`,
			`{"executions":[{"name":"other","logical_time_implementation_name":"HLAfloat64Time"},{"name":"other","logical_time_implementation_name":"HLAinteger64Time"}]}`,
			`{"executions":[{"name":"other","logical_time_implementation_name":"HLAfloat64Time","mode":3}]}`,
			`{"executions":[{"name":"other","logical_time_implementation_name":"HLAfloat64Time","federation_generation":null}]}`,
			`{"executions":[{"name":"other","logical_time_implementation_name":"HLAfloat64Time","callback_receipt":"forbidden"}]}`,
			`{"executions":[{"name":"other","name":"duplicate-key","logical_time_implementation_name":"HLAfloat64Time"}]}`,
		} {
			e := localNotificationState(t, "java", scalar, "reportFederationExecutions", "connection", payload)
			encoded, _ := Encode(e)
			if _, err := Decode(encoded); err == nil {
				t.Fatalf("invalid discovery DTO accepted: %s", payload)
			}
		}
		e := localNotificationState(t, "java", scalar, "federationRestoreStatusResponse", "member", `{"statuses":[{"pre_restore_handle":4294967299,"post_restore_handle":30064771072,"status":1}]}`)
		encoded, _ := Encode(e)
		if _, err := Decode(encoded); err == nil {
			t.Fatal("historical post designator with zero low word accepted")
		}
		e.State = bytes.Replace(e.State, []byte(`"post_restore_handle":30064771072`), []byte(`"post_restore_handle":30064771073`), 1)
		encoded, _ = Encode(e)
		refs, err := References(encoded)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, ref := range refs {
			if ref == 30064771073 {
				t.Fatal("historical status identity entered census")
			}
			found = found || ref == 4294967299
		}
		if !found {
			t.Fatal("retired same-source-generation pre identity missing from census")
		}
		request := savepoint.ClientStateRebindRequest{Federation: "fed", Label: "saved", SourceGeneration: 1, TargetGeneration: 2, SourceHandle: core.FederateHandle(e.Self), TargetHandle: 2<<32 | 9, LogicalTimeImplementationName: scalar, State: encoded, FederateHandles: map[core.FederateHandle]core.FederateHandle{1<<32 | 1: 2<<32 | 9, 1<<32 | 2: 2<<32 | 10}}
		if _, err := Rebind(context.Background(), request); err == nil {
			t.Fatal("roster-only projection silently dropped retired status identity")
		}
		request.FederateHandles[1<<32|3] = 2<<32 | 11
		output, err := Rebind(context.Background(), request)
		if err != nil {
			t.Fatal(err)
		}
		target, _ := Decode(output)
		if !bytes.Contains(target.State, []byte(`"pre_restore_handle":8589934603`)) || !bytes.Contains(target.State, []byte(`"post_restore_handle":30064771073`)) {
			t.Fatal("typed retired/historical identity policy not preserved")
		}
	}
}

func TestLocalNotificationsMultiplicityOrderAndMissingMapping(t *testing.T) {
	e := localNotificationState(t, "java", "HLAinteger64Time", "informAttributeOwnership", "member", `{"object_handle":10,"attribute_handle":20,"owner_federate":4294967298}`)
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(e.State, &fields)
	var rows []map[string]json.RawMessage
	_ = json.Unmarshal(fields["pending_local_notifications"], &rows)
	second := map[string]json.RawMessage{}
	for key, value := range rows[0] {
		second[key] = bytes.Clone(value)
	}
	second["local_id"] = json.RawMessage(`"` + strings.Repeat("cd", 32) + `"`)
	second["order"] = json.RawMessage(`9007199254740994`)
	rows = append(rows, second)
	fields["pending_local_notifications"], _ = json.Marshal(rows)
	e.State, _ = json.Marshal(fields)
	encoded, _ := Encode(e)
	if _, err := Decode(encoded); err != nil {
		t.Fatal(err)
	}
	if _, err := Rebind(context.Background(), savepoint.ClientStateRebindRequest{Federation: "fed", Label: "saved", SourceGeneration: 1, TargetGeneration: 2, SourceHandle: core.FederateHandle(e.Self), TargetHandle: 2<<32 | 9, LogicalTimeImplementationName: e.Scalar, State: encoded, FederateHandles: map[core.FederateHandle]core.FederateHandle{1<<32 | 1: 2<<32 | 9}}); err == nil {
		t.Fatal("SDK-only owner missing from complete allocation map accepted")
	}
	for _, key := range []string{"local_id", "order"} {
		second["local_id"] = json.RawMessage(`"` + strings.Repeat("cd", 32) + `"`)
		second["order"] = json.RawMessage(`9007199254740994`)
		second[key] = rows[0][key]
		fields["pending_local_notifications"], _ = json.Marshal(rows)
		e.State, _ = json.Marshal(fields)
		encoded, _ = Encode(e)
		if _, err := Decode(encoded); err == nil {
			t.Fatalf("duplicate %s accepted", key)
		}
	}
}

func TestLocalNotificationsOptionalFieldAndPreparedCandidateConservation(t *testing.T) {
	e := localNotificationState(t, "python", "HLAinteger64Time", "federationRestoreStatusResponse", "member", `{"statuses":[{"pre_restore_handle":4294967298,"post_restore_handle":0,"status":1}]}`)
	encoded, _ := Encode(e)
	request := savepoint.ClientStateRebindRequest{Federation: "fed", Label: "saved", SourceGeneration: 1, TargetGeneration: 2, SourceHandle: core.FederateHandle(e.Self), TargetHandle: 2<<32 | 9, LogicalTimeImplementationName: e.Scalar, State: encoded, FederateHandles: map[core.FederateHandle]core.FederateHandle{1<<32 | 1: 2<<32 | 9, 1<<32 | 2: 2<<32 | 10}}
	targetBytes, err := Rebind(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	request.ReboundState = targetBytes
	request.FederateHandles = nil
	twice, err := Rebind(context.Background(), request)
	if err != nil || !bytes.Equal(twice, targetBytes) {
		t.Fatalf("prepared local DTO translated twice: %v", err)
	}
	target, _ := Decode(targetBytes)
	target.State = bytes.Replace(target.State, []byte(`"post_restore_handle":0`), []byte(`"post_restore_handle":99`), 1)
	request.ReboundState, _ = Encode(target)
	if _, err := Rebind(context.Background(), request); err == nil {
		t.Fatal("prepared candidate changed historical saved query result")
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(e.State, &fields)
	for _, raw := range []json.RawMessage{json.RawMessage(`null`), json.RawMessage(`[{"local_id":"00","order":1,"scope":"member","kind":"reportAttributeTransportationType","payload":{}}]`)} {
		fields["pending_local_notifications"] = raw
		e.State, _ = json.Marshal(fields)
		encoded, _ = Encode(e)
		if _, err := Decode(encoded); err == nil {
			t.Fatal("invalid optional local array accepted")
		}
	}
	fields["pending_local_notifications"] = json.RawMessage(`[]`)
	e.State, _ = json.Marshal(fields)
	encoded, _ = Encode(e)
	if _, err := Decode(encoded); err != nil {
		t.Fatal(err)
	}
	delete(fields, "pending_local_notifications")
	e.State, _ = json.Marshal(fields)
	encoded, _ = Encode(e)
	if _, err := Decode(encoded); err != nil {
		t.Fatal(err)
	}
}
