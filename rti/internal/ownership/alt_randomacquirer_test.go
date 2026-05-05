package ownership

import (
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// TestRandomAcquirer_NameAndDeterminismFlag pins the registry key and
// the (non-)determinism claim. If either changes the docs and the
// registry pre-registration in research.Default() must be updated in
// lockstep. Critically: this is the ONE alt in Phase 4 that returns
// false for DeterminismPreserving — flipping it accidentally would
// silently disable the strict-mode rejection-path test.
func TestRandomAcquirer_NameAndDeterminismFlag(t *testing.T) {
	s := RandomAcquirerNegotiationStrategy()
	if got := s.Name(); got != "random-acquirer" {
		t.Errorf("Name(): got %q, want %q", got, "random-acquirer")
	}
	if s.DeterminismPreserving() {
		t.Errorf("DeterminismPreserving(): got true; the random-acquirer alt is the canonical non-preserving illustration and MUST report false")
	}
}

// TestRandomAcquirer_EmptyCandidates_ReturnsInvalidHandle mirrors the
// default's "no transfer" behavior on an empty candidate list.
func TestRandomAcquirer_EmptyCandidates_ReturnsInvalidHandle(t *testing.T) {
	s := RandomAcquirerNegotiationStrategy()
	got := s.SelectAcquirer(SelectAcquirerContext{Candidates: nil})
	if got != core.InvalidFederateHandle {
		t.Errorf("SelectAcquirer(empty): got %v, want InvalidFederateHandle", got)
	}
}

// TestRandomAcquirer_PicksFromCandidateSet verifies the alt always
// returns a handle from the candidate slice (never out-of-band),
// across many invocations. Pure correctness: the random pick must
// stay inside the legal candidate set even though it is non-deterministic.
func TestRandomAcquirer_PicksFromCandidateSet(t *testing.T) {
	s := RandomAcquirerNegotiationStrategy()
	cands := []core.FederateHandle{1, 2, 3, 4, 5}
	for i := 0; i < 200; i++ {
		got := s.SelectAcquirer(SelectAcquirerContext{Candidates: cands})
		found := false
		for _, c := range cands {
			if c == got {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("iter %d: SelectAcquirer returned %v not in candidate set %v", i, got, cands)
		}
	}
}

// TestRandomAcquirer_DiffersFromDefault_OverManyTrials is the
// pedagogical heart: the default ALWAYS picks Candidates[0]; the alt
// picks a different element at least sometimes. Over 200 trials with a
// 5-candidate set the probability of every pick coinciding with index
// 0 is (1/5)^200 ≈ 1.6e-140 — effectively zero. If this test ever
// fails the rng has been frozen or the alt has been stubbed out.
func TestRandomAcquirer_DiffersFromDefault_OverManyTrials(t *testing.T) {
	def := DefaultNegotiationStrategy()
	alt := RandomAcquirerNegotiationStrategy()
	cands := []core.FederateHandle{1, 2, 3, 4, 5}

	// The default returns Candidates[0] every call.
	if got := def.SelectAcquirer(SelectAcquirerContext{Candidates: cands}); got != cands[0] {
		t.Fatalf("default picked %v, want Candidates[0]=%v (precondition for the differs test)", got, cands[0])
	}

	saw := map[core.FederateHandle]int{}
	for i := 0; i < 200; i++ {
		saw[alt.SelectAcquirer(SelectAcquirerContext{Candidates: cands})]++
	}
	if len(saw) < 2 {
		t.Errorf("alt: only saw %d distinct picks across 200 trials (saw=%v); the alt has no random spread", len(saw), saw)
	}
}

// TestRandomAcquirer_SingletonCandidate_AlwaysPicksIt: a degenerate
// but important case — when the candidate set is a singleton (the
// PhaseAcquire call-site), the alt MUST return that one handle so the
// transfer fires. (PhaseAcquire's contract is "the just-arrived
// caller wins" and any random alt that fails to honor singletons
// breaks Acquire-vs-pendingDivest.)
func TestRandomAcquirer_SingletonCandidate_AlwaysPicksIt(t *testing.T) {
	s := RandomAcquirerNegotiationStrategy()
	cands := []core.FederateHandle{42}
	for i := 0; i < 50; i++ {
		got := s.SelectAcquirer(SelectAcquirerContext{Candidates: cands})
		if got != core.FederateHandle(42) {
			t.Fatalf("iter %d: singleton pick: got %v, want 42", i, got)
		}
	}
}
