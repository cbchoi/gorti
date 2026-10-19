package savepoint

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestClientStatePersistedAndReturnedOnlyToItsParticipant(t *testing.T) {
	f := newScheduleFixture(t)
	ctx := context.Background()
	if err := f.m.RequestFederationSave(ctx, "fed", "runtime", nil); err != nil {
		t.Fatal(err)
	}
	want := map[core.FederateHandle][]byte{
		1: []byte("first-runtime\x00\xff"),
		2: []byte("second-runtime\xff\x00"),
	}
	state := bytes.Clone(want[1])
	for _, h := range f.members {
		if err := f.m.FederateSaveBegun(ctx, "fed", h); err != nil {
			t.Fatal(err)
		}
		value := state
		if h == 2 {
			value = bytes.Clone(want[h])
		}
		if err := f.m.FederateSaveCompleteWithClientState(ctx, "fed", h, value); err != nil {
			t.Fatal(err)
		}
		if h == 1 {
			state[0] = 'X'
		}
	}
	manifest, err := f.m.LoadManifest("fed", "runtime")
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Federates) != len(want) || len(manifest.FederateClientStates) != len(want) {
		t.Fatalf("runtime state not persisted as a detached snapshot: %q", manifest.FederateClientStates)
	}
	persisted := make(map[core.FederateHandle]bool, len(want))
	for i, h := range manifest.Federates {
		expected, allowed := want[h]
		if !allowed || persisted[h] || !bytes.Equal(manifest.FederateClientStates[i], expected) {
			t.Fatalf("persisted snapshot owner=%d bytes=%x, want %x; allowed=%t duplicate=%t", h, manifest.FederateClientStates[i], expected, allowed, persisted[h])
		}
		persisted[h] = true
	}
	// Reconstruct the manager: no process-local checkpoint map may be required.
	f.m, err = New(Options{Outbox: f.out, BundleStore: f.store, Members: func(core.FederationName) []core.FederateHandle { return f.members }})
	if err != nil {
		t.Fatal(err)
	}
	beforeRestore := len(f.out.Sent())
	if err = f.m.RequestFederationRestore(ctx, "fed", "runtime"); err != nil {
		t.Fatal(err)
	}
	seen := make(map[core.FederateHandle]int, len(want))
	for _, sent := range f.out.Sent()[beforeRestore:] {
		expected, allowed := want[sent.Federate]
		if sent.Federation != "fed" || !allowed {
			t.Fatalf("restore delivery to an unexpected recipient: %+v", sent)
		}
		if event, ok := sent.Event.(*InitiateRestoreEvent); ok {
			if seen[sent.Federate] != 0 {
				t.Fatalf("duplicate runtime snapshot delivery to participant %d", sent.Federate)
			}
			if event.Label() != "runtime" || event.FederateHandle != sent.Federate || !bytes.Equal(event.ClientState, expected) {
				t.Fatalf("runtime snapshot recipient=%d saved handle=%d label=%q bytes=%x, want %x", sent.Federate, event.FederateHandle, event.Label(), event.ClientState, expected)
			}
			seen[sent.Federate]++
		}
	}
	for h := range want {
		if seen[h] != 1 {
			t.Fatalf("participant %d runtime snapshot count=%d, want 1", h, seen[h])
		}
	}
}

func TestClientStateInvalidCompleteDoesNotConsumeSaveResponse(t *testing.T) {
	f := newScheduleFixture(t)
	ctx := context.Background()
	if err := f.m.RequestFederationSave(ctx, "fed", "bounded", nil); err != nil {
		t.Fatal(err)
	}
	if err := f.m.FederateSaveCompleteWithClientState(ctx, "fed", 1, []byte("before-begun")); !errors.Is(err, core.ErrFederateNotInSave) {
		t.Fatalf("before begun: %v", err)
	}
	if err := f.m.FederateSaveBegun(ctx, "fed", 1); err != nil {
		t.Fatal(err)
	}
	if err := f.m.FederateSaveCompleteWithClientState(ctx, "fed", 1, make([]byte, MaxClientStateBytes+1)); err == nil {
		t.Fatal("oversized runtime state accepted")
	}
	if err := f.m.FederateSaveCompleteWithClientState(ctx, "fed", 1, []byte("valid")); err != nil {
		t.Fatal(err)
	}
	if err := f.m.FederateSaveCompleteWithClientState(ctx, "fed", 1, []byte("overwrite")); !errors.Is(err, core.ErrFederateNotInSave) {
		t.Fatalf("duplicate complete: %v", err)
	}
	if err := f.m.FederateSaveBegun(ctx, "fed", 2); err != nil {
		t.Fatal(err)
	}
	if err := f.m.FederateSaveComplete(ctx, "fed", 2); err != nil {
		t.Fatal(err)
	}
	manifest, err := f.m.LoadManifest("fed", "bounded")
	if err != nil {
		t.Fatal(err)
	}
	if string(manifest.FederateClientStates[0]) != "valid" || len(manifest.FederateClientStates[1]) != 0 {
		t.Fatalf("unexpected runtime state: %q", manifest.FederateClientStates)
	}
}

func TestClientStateManifestShapeAndLimits(t *testing.T) {
	for _, manifest := range []Manifest{
		{Federates: []core.FederateHandle{1, 2}, FederateClientStates: [][]byte{{1}}},
		{Federates: []core.FederateHandle{1}, FederateClientStates: [][]byte{make([]byte, MaxClientStateBytes+1)}},
	} {
		if !errors.Is(validateClientStates(manifest), ErrSaveBundleIncompatible) {
			t.Fatal("invalid runtime checkpoint accepted")
		}
	}
	if err := validateClientStates(Manifest{Federates: []core.FederateHandle{1}}); err != nil {
		t.Fatalf("legacy checkpoint rejected: %v", err)
	}
}

func TestClientStateCrossGenerationDoesNotReuseExecutionScopedHandles(t *testing.T) {
	m := &Manager{}
	manifest := Manifest{Federates: []core.FederateHandle{1}, FederateClientStates: [][]byte{[]byte("private-runtime")}}
	err := m.prepareCrossGeneration(context.Background(), "fed", 2, "", &manifest, &activeRestore{})
	if !errors.Is(err, ErrSaveBundleIncompatible) {
		t.Fatalf("opaque state silently reused across generations: %v", err)
	}
}
