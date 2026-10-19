package sdkstate

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	savepoint "github.com/cbchoi/gorti/rti/internal/hla/save_restore"
)

func recoveryEnvelope(t *testing.T, language, scalar string, outcomes string) Envelope {
	t.Helper()
	e := sampleEnvelope(language, scalar)
	if language == "java" {
		time := `{"float64_value":1.25}`
		if scalar == "HLAinteger64Time" {
			time = `{"integer64_value":"9007199254740993"}`
		}
		e.State = json.RawMessage(`{"federation":"fed","label":"saved","known_objects":[],"pending_removals":[],"deleted_objects":[],"cleared_retraction":0,"last_issued_retraction":0,"ddm_regions":[],"synchronizations":[],"time_policy_flags":0,"logical_time_value":` + time + `,"pending_callbacks":[]}`)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(e.State, &fields); err != nil {
		t.Fatal(err)
	}
	fields["callback_recovery"] = json.RawMessage(`"server-v2"`)
	fields["pending_outcomes"] = json.RawMessage(outcomes)
	var err error
	e.State, err = json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func recoveryManifest(t *testing.T, e Envelope) savepoint.ManifestOf[float64] {
	t.Helper()
	encoded, err := Encode(e)
	if err != nil {
		t.Fatal(err)
	}
	generation := e.Generation
	return savepoint.ManifestOf[float64]{Federation: "fed", Label: "saved", FederationGeneration: &generation, LogicalTimeImplementationName: e.Scalar, Federates: []core.FederateHandle{core.FederateHandle(e.Self)}, FederateClientStates: [][]byte{encoded}}
}

func TestRecoveryKnownOutcomesSchemaAndRebind(t *testing.T) {
	id := strings.Repeat("ab", 32)
	for _, language := range []string{"go", "python", "java"} {
		for _, scalar := range []string{"HLAfloat64Time", "HLAinteger64Time"} {
			t.Run(language+"/"+scalar, func(t *testing.T) {
				e := recoveryEnvelope(t, language, scalar, `[{"identity":"`+id+`","success":false,"exception":"application failed"}]`)
				m := recoveryManifest(t, e)
				if _, err := Decode(m.FederateClientStates[0]); err != nil {
					t.Fatal(err)
				}
				out, err := Rebind(context.Background(), savepoint.ClientStateRebindRequest{Federation: "fed", Label: "saved", SourceGeneration: 1, TargetGeneration: 2, SourceHandle: core.FederateHandle(e.Self), TargetHandle: 2<<32 | 9, LogicalTimeImplementationName: scalar, State: m.FederateClientStates[0], FederateHandles: map[core.FederateHandle]core.FederateHandle{1<<32 | 1: 2<<32 | 9, 1<<32 | 2: 2<<32 | 10}})
				if err != nil {
					t.Fatal(err)
				}
				target, err := Decode(out)
				if err != nil {
					t.Fatal(err)
				}
				var fields map[string]json.RawMessage
				_ = json.Unmarshal(target.State, &fields)
				if string(fields["pending_outcomes"]) != `[{"identity":"`+id+`","success":false,"exception":"application failed"}]` {
					t.Fatalf("outcome changed: %s", fields["pending_outcomes"])
				}
			})
		}
	}
}

func TestRecoveryReconciliationFailClosed(t *testing.T) {
	id := strings.Repeat("ab", 32)
	identity, _ := hex.DecodeString(id)
	for _, language := range []string{"go", "python", "java"} {
		for _, phase := range []string{"pending", "outcome", "completed"} {
			t.Run(language+"/"+phase, func(t *testing.T) {
				outcomes := `[{"identity":"` + id + `","success":false,"exception":"failed"}]`
				if phase == "pending" {
					outcomes = "[]"
				}
				m := recoveryManifest(t, recoveryEnvelope(t, language, "HLAfloat64Time", outcomes))
				r := savepoint.CallbackRecoveryRequirement{Recipient: m.Federates[0], InvocationIdentity: identity, Phase: phase}
				if phase == "completed" {
					r.Outcome = &savepoint.CallbackRecoveryOutcome{Success: false, Exception: "failed"}
				}
				if err := ReconcileManifestVersionOf(context.Background(), m, 2, []savepoint.CallbackRecoveryRequirement{r}); err != nil {
					t.Fatal(err)
				}
				if phase != "pending" {
					if err := ReconcileManifestVersionOf(context.Background(), m, 2, nil); err == nil {
						t.Fatal("orphan SDK result accepted")
					}
					missing := recoveryManifest(t, recoveryEnvelope(t, language, "HLAfloat64Time", "[]"))
					if err := ReconcileManifestVersionOf(context.Background(), missing, 2, []savepoint.CallbackRecoveryRequirement{r}); (err == nil) != (phase == "completed") {
						t.Fatalf("missing finished result phase %s: %v", phase, err)
					}
					r.Outcome = &savepoint.CallbackRecoveryOutcome{Success: true}
					if err := ReconcileManifestVersionOf(context.Background(), m, 2, []savepoint.CallbackRecoveryRequirement{r}); err == nil {
						t.Fatal("conflicting result accepted")
					}
				} else {
					finished := recoveryManifest(t, recoveryEnvelope(t, language, "HLAfloat64Time", `[{"identity":"`+id+`","success":true,"exception":""}]`))
					if err := ReconcileManifestVersionOf(context.Background(), finished, 2, []savepoint.CallbackRecoveryRequirement{r}); err == nil {
						t.Fatal("finished body scheduled for reinvocation")
					}
				}
				r.Recipient++
				if err := ReconcileManifestVersionOf(context.Background(), m, 2, []savepoint.CallbackRecoveryRequirement{r}); err == nil {
					t.Fatal("foreign recipient accepted")
				}
			})
		}
	}
}

func TestRecoveryRejectsInvalidOrUnknownResultsAndLegacyNegotiation(t *testing.T) {
	id := strings.Repeat("ab", 32)
	for _, outcomes := range []string{`null`, `[{"identity":"` + id + `","success":null,"exception":""}]`, `[{"identity":"` + id + `","exception":""}]`, `[{"identity":"` + strings.ToUpper(id) + `","success":true,"exception":""}]`, `[{"identity":"` + id + `","success":true,"exception":"","running":true}]`, `[{"identity":"` + id + `","success":true,"exception":""},{"identity":"` + id + `","success":false,"exception":""}]`} {
		e := recoveryEnvelope(t, "go", "HLAfloat64Time", outcomes)
		encoded, _ := Encode(e)
		if _, err := Decode(encoded); err == nil {
			t.Fatalf("invalid result accepted: %s", outcomes)
		}
	}
	m := recoveryManifest(t, sampleEnvelope("go", "HLAfloat64Time"))
	if err := ReconcileManifestVersionOf(context.Background(), m, 1, nil); err != nil {
		t.Fatal(err)
	}
	if err := ReconcileManifestVersionOf(context.Background(), m, 2, nil); err != nil {
		t.Fatalf("empty v2 ledger rejected legacy SDK state: %v", err)
	}
	identity, _ := hex.DecodeString(id)
	for _, phase := range []string{"pending", "completed"} {
		row := savepoint.CallbackRecoveryRequirement{Recipient: m.Federates[0], InvocationIdentity: identity, Phase: phase}
		if err := ReconcileManifestVersionOf(context.Background(), m, 2, []savepoint.CallbackRecoveryRequirement{row}); err != nil {
			t.Fatal(err)
		}
		m.FederateClientStates = nil
		if err := ReconcileManifestVersionOf(context.Background(), m, 2, []savepoint.CallbackRecoveryRequirement{row}); err != nil {
			t.Fatal(err)
		}
	}
	row := savepoint.CallbackRecoveryRequirement{Recipient: m.Federates[0], InvocationIdentity: identity, Phase: "outcome"}
	if err := ReconcileManifestVersionOf(context.Background(), m, 2, []savepoint.CallbackRecoveryRequirement{row}); err == nil {
		t.Fatal("raw legacy client admitted outcome-only carrier")
	}
	if err := ReconcileManifestVersionOf(context.Background(), m, 3, nil); err == nil {
		t.Fatal("unknown ledger version accepted")
	}
}
