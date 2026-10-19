package savepoint

import "github.com/cbchoi/gorti/rti/internal/hla/core"

// CallbackRecoveryOutcome is a finished body result, never an inferred success.
// Empty Exception is valid for success; a failed body retains its exact exception.
type CallbackRecoveryOutcome struct {
	Success   bool
	Exception string
}

// CallbackRecoveryRequirement is projected by the authoritative checkpoint
// decoder, not parsed in the save/restore domain. InvocationIdentity is a stable
// 32-byte correlation key, not authorization. Phase is pending, outcome or
// completed; completed rows must include their authoritative Outcome.
type CallbackRecoveryRequirement struct {
	Recipient          core.FederateHandle
	InvocationIdentity []byte
	Phase              string
	Outcome            *CallbackRecoveryOutcome
}
