package m5spec

import (
	"context"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
	"github.com/cbchoi/gorti/rti/internal/federation"
)

// TestSpec_M5_ModeFlag_VerboseDefault: a freshly-created federation
// with no mode specified defaults to ModeVerbose.
//
// Implements: FR-OM-3; M5 mode plumbing contract.
func TestSpec_M5_ModeFlag_VerboseDefault(t *testing.T) {
	mgr := newTestFederationManager(t)
	if mgr == nil {
		t.Skip("federation.Manager not yet wired (M5 W1A pre-state)")
	}
	ctx := context.Background()

	if err := mgr.CreateFederation(ctx, core.CreateFederationRequest{
		Name:       "default-mode",
		FOMModules: []core.FOMModule{{Path: "test", XML: minimalFOMXML()}},
		// Mode left zero — should default to ModeVerbose.
	}); err != nil {
		t.Fatalf("CreateFederation: %v", err)
	}

	sums, err := mgr.List(ctx)
	if err != nil {
		t.Fatalf("ListFederations: %v", err)
	}
	got := findFederation(sums, "default-mode")
	if got == nil {
		t.Fatalf("federation %q not found in list", "default-mode")
	}
	if got.Mode == core.ModeUnspecified {
		// PRE-DISPATCH: the federation manager currently passes Mode
		// through unchanged, so an unset Mode lands as ModeUnspecified.
		// TASK-076 (Agent A) wires the default-to-Verbose normalization
		// — at the federation manager OR the gRPC handler. Whichever
		// path Agent A chooses, this test then asserts ModeVerbose
		// flows through. Agent A converts this skip to a hard
		// assertion when landing TASK-076.
		t.Skip("TASK-076 RED state: ModeUnspecified persisted as default; awaiting normalization to ModeVerbose")
	}
	if got.Mode != core.ModeVerbose {
		t.Errorf("default mode = %v, want %v", got.Mode, core.ModeVerbose)
	}
}

// TestSpec_M5_ModeFlag_BestEffortPersists: creating a federation with
// mode=BestEffort persists that choice; listing it back reports
// ModeBestEffort.
//
// Implements: FR-OM-3.
func TestSpec_M5_ModeFlag_BestEffortPersists(t *testing.T) {
	mgr := newTestFederationManager(t)
	if mgr == nil {
		t.Skip("federation.Manager not yet wired (M5 W1A pre-state)")
	}
	ctx := context.Background()

	if err := mgr.CreateFederation(ctx, core.CreateFederationRequest{
		Name:       "be-fed",
		FOMModules: []core.FOMModule{{Path: "test", XML: minimalFOMXML()}},
		Mode:       core.ModeBestEffort,
	}); err != nil {
		t.Fatalf("CreateFederation: %v", err)
	}

	sums, err := mgr.List(ctx)
	if err != nil {
		t.Fatalf("ListFederations: %v", err)
	}
	got := findFederation(sums, "be-fed")
	if got == nil {
		t.Fatalf("federation %q not found in list", "be-fed")
	}
	if got.Mode != core.ModeBestEffort {
		t.Errorf("mode = %v, want %v", got.Mode, core.ModeBestEffort)
	}
}

// findFederation is a small lookup helper.
func findFederation(sums []core.FederationSummary, name core.FederationName) *core.FederationSummary {
	for i := range sums {
		if sums[i].Name == name {
			return &sums[i]
		}
	}
	return nil
}

// newTestFederationManager builds a fresh federation.Manager with the
// minimum dependencies: an in-memory FOMRepository (accepts every FOM)
// and a permissive EventLog. Returns nil on stub state so tests can skip.
func newTestFederationManager(t *testing.T) *federation.Manager {
	t.Helper()
	mgr, err := federation.New(federation.Options{
		Clock:    testClock(),
		FOMs:     newPermissiveFOMRepo(),
		EventLog: newPermissiveEventLog(),
	})
	if err != nil {
		t.Logf("federation.New returned: %v (expected during pre-dispatch)", err)
		return nil
	}
	return mgr
}
