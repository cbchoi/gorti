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
	ErrTimeRegulationPending          = errors.New("federate: request for time-regulation pending")
	ErrTimeConstrainedPending         = errors.New("federate: request for time-constrained pending")
	ErrInvalidMessageRetractionHandle = errors.New("federate: invalid message retraction handle")
	ErrMessageCanNoLongerBeRetracted  = errors.New("federate: message can no longer be retracted")

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

	// M23 — object-management additions.
	ErrObjectNotOwned       = errors.New("federate: object not owned by this federate")
	ErrObjectAlreadyDeleted = errors.New("federate: object instance already deleted")
)

// SDK-foundation errors (not from TimeService).
var (
	// ErrCouldNotCreateLogicalTimeFactory means the requested or selected
	// implementation cannot be used by the SDK/server. No lossy fallback is made.
	ErrCouldNotCreateLogicalTimeFactory = errors.New("federate: could not create logical time factory")

	// ErrRTIInternal corresponds to RTIinternalError, including contradictory
	// successful server responses whose committed effects must not be forgotten.
	ErrRTIInternal = errors.New("federate: RTIinternalError")

	// ErrNotJoined — federate has not joined a federation (or has resigned).
	ErrNotJoined = errors.New("federate: not joined to any federation")

	// ErrAlreadyJoined — federate is already joined to a federation.
	// Subsequent JoinFederation calls return this.
	ErrAlreadyJoined = errors.New("federate: already joined to a federation")

	// ErrLocalLRCUnavailable means the server or connection does not expose
	// the optional receive-order LocalLRC fast path.
	ErrLocalLRCUnavailable = errors.New("federate: LocalLRC fast path unavailable")

	// ErrLocalLRCClosed means the bounded queue no longer accepts work.
	ErrLocalLRCClosed = errors.New("federate: LocalLRC queue is closed")

	// ErrLocalLRCIndeterminate means locally accepted work lost its stream
	// before a cumulative server ACK established its final result.
	ErrLocalLRCIndeterminate = errors.New("federate: LocalLRC result indeterminate")

	// ErrLocalLRCSequenceNotAdmitted means AwaitLocalLRC was called for a
	// sequence that this LocalLRC has not returned from a queue operation.
	ErrLocalLRCSequenceNotAdmitted = errors.New("federate: LocalLRC sequence was not admitted")

	// Immediate LocalLRC admission errors mirror the corresponding RTI
	// service preconditions before work enters the asynchronous queue.
	ErrObjectInstanceNotKnown         = errors.New("federate: object instance not known")
	ErrAttributeNotDefined            = errors.New("federate: attribute not defined for object class")
	ErrAttributeNotOwned              = errors.New("federate: attribute not owned")
	ErrInteractionClassNotDefined     = errors.New("federate: interaction class not defined")
	ErrInteractionParameterNotDefined = errors.New("federate: interaction parameter not defined")
	ErrInteractionClassNotPublished   = errors.New("federate: interaction class not published")
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
	for _, mapping := range statusErrorMappings {
		if contains(msg, mapping.message) {
			return mapping.err
		}
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

// The first matching detail wins, preserving the original status precedence.
var statusErrorMappings = []struct {
	message string
	err     error
}{
	{"could not create logical time factory", ErrCouldNotCreateLogicalTimeFactory},
	{"request for time-regulation pending", ErrTimeRegulationPending},
	{"request for time-constrained pending", ErrTimeConstrainedPending},
	{"message retraction designator is invalid", ErrInvalidMessageRetractionHandle},
	{"object deletion has already been delivered", ErrMessageCanNoLongerBeRetracted},
	{"already time-regulating", ErrTimeRegulationAlreadyEnabled},
	{"not time-regulating", ErrTimeRegulationNotEnabled},
	{"already time-constrained", ErrTimeConstrainedAlreadyEnabled},
	{"not time-constrained", ErrTimeConstrainedNotEnabled},
	{"lookahead must be", ErrInvalidLookahead},
	{"requested time", ErrLogicalTimeAlreadyPassed},
	{"outstanding advance", ErrTimeAdvancingState},
	{"federation halted", ErrFederationHalted},
	{"already enabled asynchronous", ErrTimeAlreadyAsynchronous},
	{"not enabled asynchronous", ErrTimeNotAsynchronous},
	{"object not owned", ErrObjectNotOwned},
	{"object instance already deleted", ErrObjectAlreadyDeleted},
	{"object not found", ErrObjectInstanceNotKnown},
	{"attribute not found", ErrAttributeNotDefined},
	{"attribute not owned", ErrAttributeNotOwned},
	{"interaction class not found", ErrInteractionClassNotDefined},
	{"interaction parameter not found", ErrInteractionParameterNotDefined},
	{"interaction class not published", ErrInteractionClassNotPublished},
}
