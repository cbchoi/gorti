// Scaffold owned by TASK-205½ (M21) — typed errors per
// docs/M21_DISPATCH_PLAN.md §2.3.1.
//
// SCAFFOLD CONTRACT: each error variable here corresponds to one row of
// §2.3.1's Manager error → wire mapping table. The implementer wires
// the gRPC detail-string parsing in TASK-205½.

package federate

import "errors"

// Time-management typed errors. The SDK constructs these from gRPC status
// detail strings returned by rtid's TimeServiceServer.
var (
	// ErrTimeRegulationAlreadyEnabled — detail string: time_regulation_already_enabled.
	// Maps to core.ErrTimeAlreadyRegulating server-side.
	ErrTimeRegulationAlreadyEnabled = errors.New("federate: time regulation already enabled")

	// ErrTimeRegulationNotEnabled — detail string: time_regulation_not_enabled.
	// Maps to core.ErrTimeNotRegulating server-side.
	ErrTimeRegulationNotEnabled = errors.New("federate: time regulation not enabled")

	// ErrTimeConstrainedAlreadyEnabled — detail string: time_constrained_already_enabled.
	ErrTimeConstrainedAlreadyEnabled = errors.New("federate: time constrained already enabled")

	// ErrTimeConstrainedNotEnabled — detail string: time_constrained_not_enabled.
	ErrTimeConstrainedNotEnabled = errors.New("federate: time constrained not enabled")

	// ErrInvalidLookahead — detail string: invalid_lookahead.
	// Lookahead < 0 or NaN/±Inf.
	ErrInvalidLookahead = errors.New("federate: invalid lookahead (must be >= 0 and finite)")

	// ErrLogicalTimeAlreadyPassed — detail string: logical_time_already_passed.
	// Maps to core.ErrTimeRequestInPast server-side. Note the manager fires
	// this when t < currentTime + lookahead, not strictly t < currentTime.
	ErrLogicalTimeAlreadyPassed = errors.New("federate: requested logical time is not greater than current+lookahead")

	// ErrTimeAdvancingState — detail string: in_time_advancing_state.
	// Maps to time.ErrDuplicateNER server-side; raised when the federate
	// has an outstanding advance primitive and tries to issue another.
	ErrTimeAdvancingState = errors.New("federate: another advance primitive is already pending")

	// ErrFederationHalted — detail string: federation_halted.
	// Maps to core.ErrFederationHalted; the federation is in terminal halted state.
	ErrFederationHalted = errors.New("federate: federation is halted")

	// ErrTimeAlreadyAsynchronous — M22 — async delivery already enabled.
	ErrTimeAlreadyAsynchronous = errors.New("federate: asynchronous delivery already enabled")

	// ErrTimeNotAsynchronous — M22 — async delivery already disabled.
	ErrTimeNotAsynchronous = errors.New("federate: asynchronous delivery is not enabled")
)

// SDK-foundation errors (not from TimeService).
var (
	// ErrNotJoined — federate has not joined a federation (or has resigned).
	ErrNotJoined = errors.New("federate: not joined to any federation")

	// ErrAlreadyJoined — federate is already joined to a federation.
	// Subsequent JoinFederation calls return this.
	ErrAlreadyJoined = errors.New("federate: already joined to a federation")
)

// wrapStatusErr inspects a gRPC status error and returns a typed
// federate error when the detail string matches a known mapping
// (per docs/M21_DISPATCH_PLAN.md §2.3.1). Falls through to the raw
// error for codes we don't translate.
//
// The translation is detail-string based rather than gRPC-code based
// because multiple HLA errors share the same code (e.g. 4 errors
// share FailedPrecondition). The detail string is the manager's
// sentinel.Error() text — see errs.go's status.Error(... err.Error()).
func wrapStatusErr(err error) error {
	if err == nil {
		return nil
	}
	msg := errorString(err)
	switch {
	case contains(msg, "already time-regulating"):
		return ErrTimeRegulationAlreadyEnabled
	case contains(msg, "not time-regulating"):
		return ErrTimeRegulationNotEnabled
	case contains(msg, "already time-constrained"):
		return ErrTimeConstrainedAlreadyEnabled
	case contains(msg, "not time-constrained"):
		return ErrTimeConstrainedNotEnabled
	case contains(msg, "lookahead must be"):
		return ErrInvalidLookahead
	case contains(msg, "requested time"):
		return ErrLogicalTimeAlreadyPassed
	case contains(msg, "outstanding advance"):
		return ErrTimeAdvancingState
	case contains(msg, "federation halted"):
		return ErrFederationHalted
	case contains(msg, "already enabled asynchronous"):
		return ErrTimeAlreadyAsynchronous
	case contains(msg, "not enabled asynchronous"):
		return ErrTimeNotAsynchronous
	}
	return err
}

func contains(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(s, substr string) int {
	// Tiny substring scan — avoids importing "strings" in the
	// hot-path error wrapper. n*m worst case but inputs here
	// are short fixed sentinel strings.
	if len(substr) == 0 {
		return 0
	}
	if len(s) < len(substr) {
		return -1
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
