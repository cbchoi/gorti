package sdkstate

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	savepoint "github.com/cbchoi/gorti/rti/internal/hla/save_restore"
)

// This opt-in exchange consumes production SDK writers, then emits target
// images for an independent check by each SDK's own runtime reader.
func TestLocalSDKGeneratedFixtureExchange(t *testing.T) {
	directories := os.Getenv("GORTI_SDKSTATE_LOCAL_FIXTURES")
	if directories == "" {
		return
	}
	for _, directory := range strings.Split(directories, ";") {
		entries, err := os.ReadDir(directory)
		if err != nil {
			t.Fatal(err)
		}
		families := map[string]bool{}
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
				if families[e.Scalar] {
					t.Fatal("duplicate scalar fixture")
				}
				families[e.Scalar] = true
				locals, present, err := localNotifications(e.State)
				if err != nil || !present {
					t.Fatalf("missing actual SDK local notification inventory: %v", err)
				}
				kinds := map[string]bool{}
				payloads := map[string]bool{}
				duplicatePayload := false
				for _, row := range locals {
					kinds[row.Kind] = true
					key := row.Kind + string(row.Payload)
					duplicatePayload = duplicatePayload || payloads[key]
					payloads[key] = true
				}
				want := []string{"reportFederationExecutions", "reportAttributeTransportationType", "reportInteractionTransportationType", "federationSaveStatusResponse", "federationRestoreStatusResponse"}
				switch e.Language {
				case "python":
					want = append(want, "confirmAttributeTransportationTypeChange", "confirmInteractionTransportationTypeChange")
				case "java":
					want = append(want, "informAttributeOwnership", "attributeIsNotOwned", "attributeIsOwnedByRTI", "confirmAttributeOwnershipAcquisitionCancellation")
				default:
					t.Fatal("fixture must originate from an implemented local-producing SDK")
				}
				for _, kind := range want {
					if !kinds[kind] {
						t.Fatalf("production fixture lacks native kind %s", kind)
					}
				}
				if len(kinds) != len(want) || !duplicatePayload || len(locals) != len(want)+1 {
					t.Fatal("production fixture inventory/multiplicity differs from native profile")
				}
				// No matching server ledger is invented for completed local RPC results.
				if err := ReconcileManifestVersionOf(context.Background(), recoveryManifest(t, e), 2, nil); err != nil {
					t.Fatal(err)
				}
				refs, err := References(input)
				if err != nil {
					t.Fatal(err)
				}
				mapping := map[core.FederateHandle]core.FederateHandle{core.FederateHandle(e.Self): 9<<32 | 41}
				next := uint64(42)
				for _, ref := range refs {
					if uint64(ref)>>32 != e.Generation {
						t.Fatal("historical/foreign reference entered allocation census")
					}
					if _, exists := mapping[ref]; !exists {
						mapping[ref] = core.FederateHandle(9<<32 | next)
						next++
					}
				}
				request := savepoint.ClientStateRebindRequest{Federation: "fed", Label: "saved", SourceGeneration: e.Generation, SourceHandle: core.FederateHandle(e.Self), TargetGeneration: 9, TargetHandle: 9<<32 | 41, LogicalTimeImplementationName: e.Scalar, State: input, FederateHandles: mapping}
				output, err := Rebind(context.Background(), request)
				if err != nil {
					t.Fatal(err)
				}
				target, err := Decode(output)
				if err != nil {
					t.Fatal(err)
				}
				translated, _, err := localNotifications(target.State)
				if err != nil || len(translated) != len(locals) {
					t.Fatalf("target local inventory: %v", err)
				}
				for i, source := range locals {
					actual := translated[i]
					if source.ID != actual.ID || source.Order != actual.Order || source.Scope != actual.Scope || source.Kind != actual.Kind {
						t.Fatal("identity/order/multiplicity changed")
					}
					expected, err := decodeLocalPayload(source, e)
					if err != nil {
						t.Fatal(err)
					}
					_ = visitLocalFederates(expected, func(value *uint64) error { *value = uint64(mapping[core.FederateHandle(*value)]); return nil })
					got, err := decodeLocalPayload(actual, target)
					if err != nil {
						t.Fatal(err)
					}
					wantPayload, _ := json.Marshal(expected)
					gotPayload, _ := json.Marshal(got)
					if !bytes.Equal(wantPayload, gotPayload) {
						t.Fatalf("non-federate result data changed: %s", source.Kind)
					}
				}
				request.ReboundState, request.FederateHandles = output, nil
				again, err := Rebind(context.Background(), request)
				if err != nil || !bytes.Equal(output, again) {
					t.Fatalf("prepared target translated twice: %v", err)
				}
				if err := os.WriteFile(filepath.Join(directory, "central-"+entry.Name()), output, 0600); err != nil {
					t.Fatal(err)
				}
			})
		}
		if inputs != 2 || !families["HLAfloat64Time"] || !families["HLAinteger64Time"] {
			t.Fatalf("expected both production scalar fixtures in %s", directory)
		}
	}
}
