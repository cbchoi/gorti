package time

import "github.com/cbchoi/gorti/rti/internal/core"

// checkLookahead enforces the HLA invariant for a regulating federate's
// NER request: the requested logical time t must satisfy
//
//	t >= currentTime + lookahead
//
// Any t below the floor would produce a TSO message in the federate's
// own past, which the rest of the federation cannot receive without
// breaking causality (see SRS §FR-TM-5).
//
// Returns core.ErrTimeRequestInPast on violation. A non-regulating
// federate has no lookahead obligation and MUST NOT be passed here;
// callers gate on the regulating flag first.
//
// Edge cases:
//   - lookahead == 0 reduces the rule to t >= currentTime, allowing
//     zero-lookahead federates (e.g. event-driven receivers) to NER to
//     their own current time without violating the invariant.
//   - Floating-point comparison is strict (no epsilon); per SRS
//     determinism rules, equality is granted.
func checkLookahead(currentTime, lookahead, requested core.LogicalTime) error {
	floor := float64(currentTime) + float64(lookahead)
	if float64(requested) < floor {
		return core.ErrTimeRequestInPast
	}
	return nil
}
