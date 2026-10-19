package federation_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/federation"
)

func TestRestoreIdentityUnderExistingMemberLease(t *testing.T) {
	ctx := context.Background()
	m, err := federation.New(validOptions())
	if err != nil {
		t.Fatal(err)
	}
	if err := m.CreateFederation(ctx, core.CreateFederationRequest{Name: "f"}); err != nil {
		t.Fatal(err)
	}
	saved, err := m.JoinFederation(ctx, core.JoinFederationRequest{Federation: "f", FederateName: "a"})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.ResignFederation(ctx, "f", saved, core.ResignActionNoAction); err != nil {
		t.Fatal(err)
	}
	current, err := m.JoinFederation(ctx, core.JoinFederationRequest{Federation: "f", FederateName: "a", FederateType: "live-type"})
	if err != nil {
		t.Fatal(err)
	}
	generation, _ := m.GenerationFor("f")
	release, err := m.AcquireMember("f", current)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	done := make(chan error, 1)
	go func() {
		r, err := m.PrepareRestoreIdentity("f", generation, []federation.RestoreIdentity{{Name: "a", CurrentHandle: current, SavedHandle: saved}})
		if err == nil {
			err = r.Commit()
			r.Release()
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("restore tried to upgrade a held member lease")
	}
	if err := m.ValidateMember("f", saved); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(m.ValidateMember("f", current), core.ErrFederateNotJoined) {
		t.Fatal("old handle remains joined")
	}
}

func TestRestoreIdentityRejectsInvalidGenerationAndBijection(t *testing.T) {
	ctx := context.Background()
	m, err := federation.New(validOptions())
	if err != nil {
		t.Fatal(err)
	}
	if err := m.CreateFederation(ctx, core.CreateFederationRequest{Name: "f"}); err != nil {
		t.Fatal(err)
	}
	h, err := m.JoinFederation(ctx, core.JoinFederationRequest{Federation: "f", FederateName: "a"})
	if err != nil {
		t.Fatal(err)
	}
	generation, _ := m.GenerationFor("f")
	for _, test := range []struct {
		name       string
		generation uint64
		identities []federation.RestoreIdentity
	}{
		{"stale-generation", generation + 1, []federation.RestoreIdentity{{Name: "a", CurrentHandle: h, SavedHandle: h}}},
		{"wrong-name", generation, []federation.RestoreIdentity{{Name: "b", CurrentHandle: h, SavedHandle: h}}},
		{"cross-generation", generation, []federation.RestoreIdentity{{Name: "a", CurrentHandle: h, SavedHandle: core.FederateHandle((generation+1)<<32 | 1)}}},
		{"unallocated", generation, []federation.RestoreIdentity{{Name: "a", CurrentHandle: h, SavedHandle: h + 1}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			r, err := m.PrepareRestoreIdentity("f", test.generation, test.identities)
			if err == nil {
				r.Release()
				t.Fatal("invalid identity accepted")
			}
			if err := m.ValidateMember("f", h); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRestoreIdentityInstallsDetachedSavedModuleBaseline(t *testing.T) {
	ctx := context.Background()
	m, err := federation.New(validOptions())
	if err != nil {
		t.Fatal(err)
	}
	if err := m.CreateFederation(ctx, core.CreateFederationRequest{Name: "f"}); err != nil {
		t.Fatal(err)
	}
	h, err := m.JoinFederation(ctx, core.JoinFederationRequest{Federation: "f", FederateName: "a"})
	if err != nil {
		t.Fatal(err)
	}
	generation, _ := m.GenerationFor("f")
	modules := []core.FOMModule{{Path: "saved.xml", XML: []byte("saved")}}
	r, err := m.PrepareRestoreIdentity("f", generation, []federation.RestoreIdentity{{Name: "a", CurrentHandle: h, SavedHandle: h, SavedModules: modules, ModulesKnown: true}})
	if err != nil {
		t.Fatal(err)
	}
	modules[0].XML[0] = 'X'
	if err := r.Commit(); err != nil {
		t.Fatal(err)
	}
	got, err := m.JoinedModules(ctx, "f", h)
	if err != nil || len(got) != 1 || string(got[0].XML) != "saved" {
		t.Fatalf("saved module baseline: %+v %v", got, err)
	}
	r, err = m.PrepareRestoreIdentity("f", generation, []federation.RestoreIdentity{{Name: "a", CurrentHandle: h, SavedHandle: h, ModulesKnown: true}})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Commit(); err != nil {
		t.Fatal(err)
	}
	got, err = m.JoinedModules(ctx, "f", h)
	if err != nil || len(got) != 0 {
		t.Fatalf("known empty baseline: %+v %v", got, err)
	}
}
