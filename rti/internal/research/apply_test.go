package research_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/research"
)

// buildRegistryWithFakeLBTS adds a fake non-preserving LBTS impl
// under name "fake-nondet" to a Default() registry. Returns the
// registry. Used by the determinism-gate tests.
func buildRegistryWithFakeLBTS(t *testing.T) *research.Registry {
	t.Helper()
	r := research.Default()
	if err := r.RegisterLBTS("fake-nondet", fakeLBTS{name: "fake-nondet", det: false}); err != nil {
		t.Fatalf("RegisterLBTS: %v", err)
	}
	return r
}

func TestApplyDefaultsResolveToDefaultImpls(t *testing.T) {
	t.Parallel()

	cfg := research.DefaultConfig()
	reg := research.Default()
	res, err := research.Apply(cfg, reg)
	if err != nil {
		t.Fatalf("Apply: unexpected err %v", err)
	}
	if res.Time.LBTS == nil || res.Time.LBTS.Name() != "default" {
		t.Errorf("Time.LBTS: want default, got %v", res.Time.LBTS)
	}
	if res.Time.Grant == nil || res.Time.Grant.Name() != "default" {
		t.Errorf("Time.Grant: want default, got %v", res.Time.Grant)
	}
	if res.Ownership.Negotiation == nil || res.Ownership.Negotiation.Name() != "default" {
		t.Errorf("Ownership.Negotiation: want default, got %v", res.Ownership.Negotiation)
	}
	if !res.AllPreserving() {
		t.Errorf("AllPreserving: want true with default impls, got false")
	}
}

func TestApplyMissingStrategyRejected(t *testing.T) {
	t.Parallel()

	cfg := research.DefaultConfig()
	cfg.Time.LBTS = "no-such-impl"
	_, err := research.Apply(cfg, research.Default())
	if err == nil {
		t.Fatalf("Apply: want error for missing impl, got nil")
	}
	if !strings.Contains(err.Error(), "no-such-impl") {
		t.Errorf("err: should name the missing impl, got %v", err)
	}
}

func TestApplyNilCfgRejected(t *testing.T) {
	t.Parallel()

	_, err := research.Apply(nil, research.Default())
	if err == nil {
		t.Fatalf("Apply(nil cfg): want error, got nil")
	}
}

func TestApplyNilRegRejected(t *testing.T) {
	t.Parallel()

	_, err := research.Apply(research.DefaultConfig(), nil)
	if err == nil {
		t.Fatalf("Apply(nil reg): want error, got nil")
	}
}

func TestApplyStrictRejectsNonPreservingStrategy(t *testing.T) {
	t.Parallel()

	reg := buildRegistryWithFakeLBTS(t)
	cfg := research.DefaultConfig()
	cfg.Determinism = research.DeterminismStrict
	cfg.Time.LBTS = "fake-nondet"

	_, err := research.Apply(cfg, reg)
	if err == nil {
		t.Fatalf("Apply(strict + non-preserving): want error, got nil")
	}
	var npe *research.NonPreservingError
	if !errors.As(err, &npe) {
		t.Fatalf("Apply(strict + non-preserving): want NonPreservingError, got %T: %v", err, err)
	}
	if npe.Category != research.CategoryTimeLBTS {
		t.Errorf("NonPreservingError.Category: want %s, got %s", research.CategoryTimeLBTS, npe.Category)
	}
	if npe.Name != "fake-nondet" {
		t.Errorf("NonPreservingError.Name: want fake-nondet, got %q", npe.Name)
	}
}

func TestApplyPerImplOptInToleratesNonPreserving(t *testing.T) {
	t.Parallel()

	reg := buildRegistryWithFakeLBTS(t)
	cfg := research.DefaultConfig()
	cfg.Determinism = research.DeterminismPerImplOptIn
	cfg.Time.LBTS = "fake-nondet"

	res, err := research.Apply(cfg, reg)
	if err != nil {
		t.Fatalf("Apply(per-impl-opt-in + non-preserving): unexpected err %v", err)
	}
	if res.AllPreserving() {
		t.Errorf("AllPreserving: want false with non-preserving impl, got true")
	}
}

func TestApplyOffToleratesNonPreserving(t *testing.T) {
	t.Parallel()

	reg := buildRegistryWithFakeLBTS(t)
	cfg := research.DefaultConfig()
	cfg.Determinism = research.DeterminismOff
	cfg.Time.LBTS = "fake-nondet"

	res, err := research.Apply(cfg, reg)
	if err != nil {
		t.Fatalf("Apply(off + non-preserving): unexpected err %v", err)
	}
	if res.AllPreserving() {
		t.Errorf("AllPreserving: want false with non-preserving impl, got true")
	}
	if res.Determinism != research.DeterminismOff {
		t.Errorf("Determinism: want off, got %v", res.Determinism)
	}
}

func TestApplyStrictRejectsNonPreservingGrant(t *testing.T) {
	t.Parallel()

	reg := research.Default()
	if err := reg.RegisterGrant("fake-nondet", fakeGrant{name: "fake-nondet", det: false}); err != nil {
		t.Fatalf("RegisterGrant: %v", err)
	}
	cfg := research.DefaultConfig()
	cfg.Determinism = research.DeterminismStrict
	cfg.Time.Grant = "fake-nondet"

	_, err := research.Apply(cfg, reg)
	var npe *research.NonPreservingError
	if !errors.As(err, &npe) {
		t.Fatalf("Apply: want NonPreservingError, got %T: %v", err, err)
	}
	if npe.Category != research.CategoryTimeGrant {
		t.Errorf("Category: want %s, got %s", research.CategoryTimeGrant, npe.Category)
	}
}

func TestApplyStrictRejectsNonPreservingNegotiation(t *testing.T) {
	t.Parallel()

	reg := research.Default()
	if err := reg.RegisterNegotiation("fake-nondet", fakeNegotiation{name: "fake-nondet", det: false}); err != nil {
		t.Fatalf("RegisterNegotiation: %v", err)
	}
	cfg := research.DefaultConfig()
	cfg.Determinism = research.DeterminismStrict
	cfg.Ownership.Negotiation = "fake-nondet"

	_, err := research.Apply(cfg, reg)
	var npe *research.NonPreservingError
	if !errors.As(err, &npe) {
		t.Fatalf("Apply: want NonPreservingError, got %T: %v", err, err)
	}
	if npe.Category != research.CategoryOwnershipNegotiation {
		t.Errorf("Category: want %s, got %s", research.CategoryOwnershipNegotiation, npe.Category)
	}
}

// TestApplyStrictRejectsRandomAcquirerAlt is the Phase-4 end-to-end
// integration check: the production-shipped, pre-registered
// "random-acquirer" alt MUST trip the strict-mode gate. Without this
// the gate is dead code in real deployments — every other apply test
// uses a test-local fake. The test cements the contract: any
// determinism-preserving=false alt added in Default() is guaranteed
// to be rejected when an operator opts into strict mode.
func TestApplyStrictRejectsRandomAcquirerAlt(t *testing.T) {
	t.Parallel()

	reg := research.Default()
	cfg := research.DefaultConfig()
	cfg.Determinism = research.DeterminismStrict
	cfg.Ownership.Negotiation = "random-acquirer"

	_, err := research.Apply(cfg, reg)
	if err == nil {
		t.Fatalf("Apply(strict + random-acquirer): want error, got nil")
	}
	var npe *research.NonPreservingError
	if !errors.As(err, &npe) {
		t.Fatalf("Apply(strict + random-acquirer): want NonPreservingError, got %T: %v", err, err)
	}
	if npe.Category != research.CategoryOwnershipNegotiation {
		t.Errorf("NonPreservingError.Category: want %s, got %s", research.CategoryOwnershipNegotiation, npe.Category)
	}
	if npe.Name != "random-acquirer" {
		t.Errorf("NonPreservingError.Name: want random-acquirer, got %q", npe.Name)
	}
}

// TestApplyPerImplOptInAllowsRandomAcquirerAlt: under per-impl-opt-in
// the random-acquirer alt is admitted (no error from Apply); the gate
// shifts to AllPreserving() == false, which the replay-test fixtures
// consult to skip-with-reason. This pins the second leg of the
// determinism contract.
func TestApplyPerImplOptInAllowsRandomAcquirerAlt(t *testing.T) {
	t.Parallel()

	reg := research.Default()
	cfg := research.DefaultConfig()
	cfg.Determinism = research.DeterminismPerImplOptIn
	cfg.Ownership.Negotiation = "random-acquirer"

	res, err := research.Apply(cfg, reg)
	if err != nil {
		t.Fatalf("Apply(per-impl-opt-in + random-acquirer): unexpected err %v", err)
	}
	if res.AllPreserving() {
		t.Errorf("AllPreserving: want false (random-acquirer is non-preserving), got true")
	}
	if res.Ownership.Negotiation == nil || res.Ownership.Negotiation.Name() != "random-acquirer" {
		t.Errorf("Ownership.Negotiation: want random-acquirer, got %v", res.Ownership.Negotiation)
	}
}
