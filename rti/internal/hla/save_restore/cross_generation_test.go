package savepoint

import (
	"context"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestExplicitSourceRestorePinsTargetProvenance(t *testing.T) {
	for _, change := range []string{"none", "generation", "fom", "roster"} {
		t.Run(change, func(t *testing.T) {
			ctx := context.Background()
			generation := uint64(1)
			digest := testFOMDigest
			handle := core.FederateHandle(1<<32 | 1)
			fom := &transitionSnapshot{live: testSaved, digest: testFOMDigest}
			state := &transitionSnapshot{live: testSaved}
			opts := strictOptions(newMemStore(), &generation, nil)
			opts.Outbox = &transitionOutbox{}
			opts.FOMSHA256 = func(core.FederationName) (string, bool) { return digest, true }
			opts.Members = func(core.FederationName) []core.FederateHandle { return []core.FederateHandle{handle} }
			opts.Roster = func(core.FederationName) []core.FederationMember {
				return []core.FederationMember{{Handle: handle, Name: "one"}}
			}
			opts.RequiredSnapshots = []string{"fom", "state"}
			opts.ManagerSnapshots = map[string]ManagerSnapshotter{"fom": fom, "state": state}
			translated := core.FederateHandle(2<<32 | 9)
			rebindCalls := 0
			opts.RebindRestore = func(_ context.Context, _ core.FederationName, target uint64, manifest Manifest) (Manifest, error) {
				rebindCalls++
				if target != 2 || *manifest.FederationGeneration != 1 {
					t.Fatal("source/target conflated")
				}
				manifest.FederationGeneration = &target
				manifest.Federates = []core.FederateHandle{translated}
				return manifest, nil
			}
			opts.ValidateRestoreSnapshots = func(core.FederationName, Manifest, core.FOMHandle) error { return nil }
			opts.PrepareRestoreTransition = func(_ context.Context, _ core.FederationName, participants []RestoreParticipant, _ []core.OutboxDelivery) (core.OutboxReservation, error) {
				if participants[0].CurrentHandle != handle || participants[0].SavedHandle != translated {
					t.Fatal("wrong installed identity")
				}
				return &transitionReservation{commit: func() { handle = translated }}, nil
			}
			m, err := New(opts)
			if err != nil {
				t.Fatal(err)
			}
			if err = m.RequestFederationSave(ctx, "fed", "checkpoint", nil); err != nil {
				t.Fatal(err)
			}
			if err = m.FederateSaveBegun(ctx, "fed", handle); err != nil {
				t.Fatal(err)
			}
			if err = m.FederateSaveComplete(ctx, "fed", handle); err != nil {
				t.Fatal(err)
			}
			generation, handle, digest = 2, core.FederateHandle(2<<32|1), "current-target"
			state.live = testLive
			if err = m.RequestFederationRestoreBy(ctx, "fed", handle, "checkpoint"); !errors.Is(err, ErrSaveBundleNotFound) {
				t.Fatalf("default crossed generation: %v", err)
			}
			if err = m.RequestFederationRestoreFrom(ctx, "fed", handle, "checkpoint", 1); err != nil {
				t.Fatalf("explicit prior execution restore: %v", err)
			}
			if rebindCalls != 1 || state.live != testLive {
				t.Fatal("bad prepare/publication ordering")
			}
			responding := handle
			switch change {
			case "generation":
				generation++
			case "fom":
				digest = testChanged
			case "roster":
				handle++
			}
			err = m.FederateRestoreComplete(ctx, "fed", responding)
			if change == "none" {
				if err != nil || state.live != testSaved || handle != translated {
					t.Fatalf("commit state=%s handle=%d err=%v", state.live, handle, err)
				}
			} else if err == nil || state.live != testLive {
				t.Fatalf("changed target committed: %v", err)
			}
		})
	}
}

func TestSaveRejectsInconsistentStateGraphBeforePublication(t *testing.T) {
	ctx := context.Background()
	store := newMemStore()
	m, err := New(Options{Outbox: &transitionOutbox{}, BundleStore: store, Members: func(core.FederationName) []core.FederateHandle { return []core.FederateHandle{1} }, RequiredSnapshots: []string{"state"}, ManagerSnapshots: map[string]ManagerSnapshotter{"state": &transitionSnapshot{live: "inconsistent"}}, ValidateRestoreSnapshots: func(core.FederationName, Manifest, core.FOMHandle) error {
		return errors.New("ownership references missing object")
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err = m.RequestFederationSave(ctx, "fed", "invalid", nil); err != nil {
		t.Fatal(err)
	}
	if err = m.FederateSaveBegun(ctx, "fed", 1); err != nil {
		t.Fatal(err)
	}
	if err = m.FederateSaveComplete(ctx, "fed", 1); err != nil {
		t.Fatal(err)
	}
	if store.Exists("fed", "invalid") || m.QuerySaveState("fed", "invalid") != StateNotSaved {
		t.Fatal("inconsistent cross-manager graph published as a successful checkpoint")
	}
}

const testLive = "live"
