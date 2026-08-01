package research_test

import (
	"strings"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
	"github.com/cbchoi/gorti/rti/internal/ownership"
	"github.com/cbchoi/gorti/rti/internal/research"
	timepkg "github.com/cbchoi/gorti/rti/internal/time"
)

// fakeLBTS is a minimal LBTSStrategy used by tests. Lives in the
// _test.go file so it never leaks into the production registry.
type fakeLBTS struct {
	name string
	det  bool
}

func (fakeLBTS) LBTS(_ []timepkg.RegulatingFederate) core.LogicalTime { return 0 }
func (f fakeLBTS) Name() string                                       { return f.name }
func (f fakeLBTS) DeterminismPreserving() bool                        { return f.det }

type fakeGrant struct {
	name string
	det  bool
}

func (fakeGrant) DecideGrant(_ timepkg.GrantContext) timepkg.GrantDecision {
	return timepkg.GrantDecision{}
}
func (f fakeGrant) Name() string                { return f.name }
func (f fakeGrant) DeterminismPreserving() bool { return f.det }

type fakeNegotiation struct {
	name string
	det  bool
}

func (fakeNegotiation) SelectAcquirer(_ ownership.SelectAcquirerContext) core.FederateHandle {
	return core.InvalidFederateHandle
}
func (f fakeNegotiation) Name() string                { return f.name }
func (f fakeNegotiation) DeterminismPreserving() bool { return f.det }

func TestDefaultRegistryHasDefaultImpls(t *testing.T) {
	t.Parallel()

	r := research.Default()
	if got, ok := r.LookupLBTS("default"); !ok || got == nil {
		t.Fatalf("LookupLBTS(default): want non-nil impl, got ok=%v impl=%v", ok, got)
	}
	if got, ok := r.LookupGrant("default"); !ok || got == nil {
		t.Fatalf("LookupGrant(default): want non-nil impl, got ok=%v impl=%v", ok, got)
	}
	if got, ok := r.LookupNegotiation("default"); !ok || got == nil {
		t.Fatalf("LookupNegotiation(default): want non-nil impl, got ok=%v impl=%v", ok, got)
	}
}

func TestRegistryRoundTripLBTS(t *testing.T) {
	t.Parallel()

	r := research.NewRegistry()
	want := fakeLBTS{name: "alt", det: false}
	if err := r.RegisterLBTS("alt", want); err != nil {
		t.Fatalf("RegisterLBTS: unexpected err %v", err)
	}
	got, ok := r.LookupLBTS("alt")
	if !ok {
		t.Fatalf("LookupLBTS(alt): want ok=true, got false")
	}
	if got.Name() != "alt" || got.DeterminismPreserving() != false {
		t.Fatalf("LookupLBTS(alt): roundtrip mismatch got name=%s det=%v",
			got.Name(), got.DeterminismPreserving())
	}
}

func TestRegistryRoundTripGrant(t *testing.T) {
	t.Parallel()

	r := research.NewRegistry()
	if err := r.RegisterGrant("alt", fakeGrant{name: "alt", det: true}); err != nil {
		t.Fatalf("RegisterGrant: unexpected err %v", err)
	}
	got, ok := r.LookupGrant("alt")
	if !ok || got.Name() != "alt" {
		t.Fatalf("LookupGrant(alt): roundtrip mismatch ok=%v got=%v", ok, got)
	}
}

func TestRegistryRoundTripNegotiation(t *testing.T) {
	t.Parallel()

	r := research.NewRegistry()
	if err := r.RegisterNegotiation("alt", fakeNegotiation{name: "alt", det: true}); err != nil {
		t.Fatalf("RegisterNegotiation: unexpected err %v", err)
	}
	got, ok := r.LookupNegotiation("alt")
	if !ok || got.Name() != "alt" {
		t.Fatalf("LookupNegotiation(alt): roundtrip mismatch ok=%v got=%v", ok, got)
	}
}

func TestRegistryDuplicateNameRejected(t *testing.T) {
	t.Parallel()

	r := research.Default() // already has "default" in every category
	cases := []struct {
		name string
		err  error
	}{
		{"lbts", r.RegisterLBTS("default", fakeLBTS{name: "default"})},
		{"grant", r.RegisterGrant("default", fakeGrant{name: "default"})},
		{"negotiation", r.RegisterNegotiation("default", fakeNegotiation{name: "default"})},
	}
	for _, tc := range cases {
		if tc.err == nil {
			t.Errorf("Register%s(default): want duplicate-name error, got nil", tc.name)
			continue
		}
		if !strings.Contains(tc.err.Error(), "already registered") {
			t.Errorf("Register%s(default): err message missing 'already registered': %v", tc.name, tc.err)
		}
	}
}

func TestRegistryEmptyNameRejected(t *testing.T) {
	t.Parallel()

	r := research.NewRegistry()
	if err := r.RegisterLBTS("", fakeLBTS{}); err == nil {
		t.Errorf("RegisterLBTS(empty name): want error, got nil")
	}
	if err := r.RegisterGrant("", fakeGrant{}); err == nil {
		t.Errorf("RegisterGrant(empty name): want error, got nil")
	}
	if err := r.RegisterNegotiation("", fakeNegotiation{}); err == nil {
		t.Errorf("RegisterNegotiation(empty name): want error, got nil")
	}
}

func TestRegistryNilImplRejected(t *testing.T) {
	t.Parallel()

	r := research.NewRegistry()
	if err := r.RegisterLBTS("alt", nil); err == nil {
		t.Errorf("RegisterLBTS(nil): want error, got nil")
	}
	if err := r.RegisterGrant("alt", nil); err == nil {
		t.Errorf("RegisterGrant(nil): want error, got nil")
	}
	if err := r.RegisterNegotiation("alt", nil); err == nil {
		t.Errorf("RegisterNegotiation(nil): want error, got nil")
	}
}

func TestRegistryMissingNameLookup(t *testing.T) {
	t.Parallel()

	r := research.NewRegistry()
	if _, ok := r.LookupLBTS("missing"); ok {
		t.Errorf("LookupLBTS(missing): want ok=false, got true")
	}
	if _, ok := r.LookupGrant("missing"); ok {
		t.Errorf("LookupGrant(missing): want ok=false, got true")
	}
	if _, ok := r.LookupNegotiation("missing"); ok {
		t.Errorf("LookupNegotiation(missing): want ok=false, got true")
	}
}
