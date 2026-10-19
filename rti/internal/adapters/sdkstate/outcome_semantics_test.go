package sdkstate

import (
	"context"
	"encoding/hex"
	"strings"
	"testing"

	savepoint "github.com/cbchoi/gorti/rti/internal/hla/save_restore"
)

func TestRecoveryOutcomeResultSemanticsBeforePersistenceOrAllocation(t *testing.T) {
	id := strings.Repeat("ab", 32)
	identity, _ := hex.DecodeString(id)
	for _, language := range []string{"go", "python", "java"} {
		for _, scalar := range []string{"HLAfloat64Time", "HLAinteger64Time"} {
			for _, result := range []struct {
				name, fields string
				valid        bool
			}{
				{"success", `"success":true,"exception":""`, true},
				{"failure", `"success":false,"exception":"application failure"`, true},
				{"success-with-exception", `"success":true,"exception":"application failure"`, false},
				{"failure-without-exception", `"success":false,"exception":""`, false},
			} {
				t.Run(language+"/"+scalar+"/"+result.name, func(t *testing.T) {
					e := recoveryEnvelope(t, language, scalar, `[{"identity":"`+id+`",`+result.fields+`}]`)
					m := recoveryManifest(t, e)
					_, err := Decode(m.FederateClientStates[0])
					if (err == nil) != result.valid {
						t.Fatalf("decode result semantics: %v", err)
					}
					rows := []savepoint.CallbackRecoveryRequirement{{Recipient: m.Federates[0], InvocationIdentity: identity, Phase: "outcome"}}
					if err := ReconcileManifestVersionOf(context.Background(), m, 2, rows); (err == nil) != result.valid {
						t.Fatalf("source preflight result semantics: %v", err)
					}
				})
			}
		}
	}
}
