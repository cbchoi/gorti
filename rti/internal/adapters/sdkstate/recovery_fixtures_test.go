package sdkstate

import (
	"context"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	savepoint "github.com/cbchoi/gorti/rti/internal/hla/save_restore"
)

// This explicit campaign consumes actual SDK writers and returns central
// target images for independent SDK decoder checks, not synthetic fixtures.
func TestRecoverySDKGeneratedFixtureExchange(t *testing.T) {
	directories := os.Getenv("GORTI_SDKSTATE_V2_FIXTURES")
	if directories == "" {
		return
	}
	for _, directory := range strings.Split(directories, ";") {
		entries, err := os.ReadDir(directory)
		if err != nil {
			t.Fatal(err)
		}
		inputs := 0
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" || strings.HasPrefix(entry.Name(), "central-") {
				continue
			}
			inputs++
			t.Run(entry.Name(), func(t *testing.T) {
				input, err := os.ReadFile(filepath.Join(directory, entry.Name()))
				if err != nil {
					t.Fatal(err)
				}
				e, err := Decode(input)
				if err != nil {
					t.Fatal(err)
				}
				capability, outcomes := recoveryState(e)
				if capability != "server-v2" || len(outcomes) == 0 {
					t.Fatal("SDK fixture lacks v2 finished-result evidence")
				}
				manifest := recoveryManifest(t, e)
				var rows []savepoint.CallbackRecoveryRequirement
				seen := map[string]bool{}
				for _, outcome := range outcomes {
					identity, _ := hex.DecodeString(outcome.Identity)
					rows = append(rows, savepoint.CallbackRecoveryRequirement{Recipient: core.FederateHandle(e.Self), InvocationIdentity: identity, Phase: "outcome", Outcome: &savepoint.CallbackRecoveryOutcome{Success: outcome.Success, Exception: outcome.Exception}})
					seen[outcome.Identity] = true
				}
				queued, err := queuedRecoveryIdentities(e)
				if err != nil {
					t.Fatal(err)
				}
				for _, id := range queued {
					if seen[id] {
						continue
					}
					identity, _ := hex.DecodeString(id)
					rows = append(rows, savepoint.CallbackRecoveryRequirement{Recipient: core.FederateHandle(e.Self), InvocationIdentity: identity, Phase: "pending"})
				}
				if err := ReconcileManifestVersionOf(context.Background(), manifest, 2, rows); err != nil {
					t.Fatal(err)
				}
				refs, err := References(input)
				if err != nil {
					t.Fatal(err)
				}
				mapping := map[core.FederateHandle]core.FederateHandle{core.FederateHandle(e.Self): 9<<32 | 41}
				next := uint64(42)
				for _, ref := range refs {
					if _, present := mapping[ref]; !present {
						mapping[ref] = core.FederateHandle(9<<32 | next)
						next++
					}
				}
				output, err := Rebind(context.Background(), savepoint.ClientStateRebindRequest{Federation: "fed", Label: "saved", SourceGeneration: e.Generation, SourceHandle: core.FederateHandle(e.Self), TargetGeneration: 9, TargetHandle: 9<<32 | 41, LogicalTimeImplementationName: e.Scalar, State: input, FederateHandles: mapping})
				if err != nil {
					t.Fatal(err)
				}
				target, err := Decode(output)
				if err != nil {
					t.Fatal(err)
				}
				_, translated := recoveryState(target)
				if len(translated) != len(outcomes) {
					t.Fatal("SDK result ledger changed in translation")
				}
				for i := range outcomes {
					if outcomes[i] != translated[i] {
						t.Fatal("SDK result identity or outcome changed")
					}
				}
				if err := os.WriteFile(filepath.Join(directory, "central-"+entry.Name()), output, 0600); err != nil {
					t.Fatal(err)
				}
			})
		}
		if inputs != 2 {
			t.Fatalf("expected both scalar fixtures in %s, got %d", directory, inputs)
		}
	}
}
