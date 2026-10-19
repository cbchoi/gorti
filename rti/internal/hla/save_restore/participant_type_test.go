package savepoint

import (
	"context"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestRestoreParticipantTypeMatchesSavedCensus(t *testing.T) {
	for _, test := range []struct {
		name  string
		saved []string
		live  string
		fail  bool
	}{
		{"same", []string{"producer"}, "producer", false},
		{"different", []string{"producer"}, "consumer", true},
		{"missing-live-type", []string{"producer"}, "", true},
		{"malformed-vector", []string{"producer", "extra"}, "producer", true},
		{"legacy-unknown", nil, "producer", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			m := &Manager{opts: Options{Members: func(core.FederationName) []core.FederateHandle { return []core.FederateHandle{1} },
				Roster: func(core.FederationName) []core.FederationMember {
					return []core.FederationMember{{Handle: 1, Name: "one", FederateType: test.live}}
				}}}
			_, err := m.restoreParticipants("fed", Manifest{Federates: []core.FederateHandle{1}, FederateNames: []string{"one"}, FederateTypes: test.saved})
			if test.fail != errors.Is(err, ErrSaveBundleIncompatible) {
				t.Fatalf("type census: %v", err)
			}
		})
	}
}

func TestSavePersistsParticipantTypes(t *testing.T) {
	f := newScheduleFixture(t)
	f.m.opts.Roster = func(core.FederationName) []core.FederationMember {
		return []core.FederationMember{{Handle: 1, Name: "first", FederateType: "producer"}, {Handle: 2, Name: "second", FederateType: "consumer"}}
	}
	ctx := context.Background()
	if err := f.m.RequestFederationSave(ctx, "fed", "types", nil); err != nil {
		t.Fatal(err)
	}
	for _, h := range f.members {
		if err := f.m.FederateSaveBegun(ctx, "fed", h); err != nil {
			t.Fatal(err)
		}
		if err := f.m.FederateSaveComplete(ctx, "fed", h); err != nil {
			t.Fatal(err)
		}
	}
	manifest, err := f.m.LoadManifest("fed", "types")
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.FederateTypes) != 2 || manifest.FederateTypes[0] != "producer" || manifest.FederateTypes[1] != "consumer" {
		t.Fatalf("types: %v", manifest.FederateTypes)
	}
}
