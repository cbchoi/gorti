// W2: value-type membership lease addendum.
//
// FederationMembershipGuard.AcquireMember returns a bound release
// closure, which costs one heap allocation per acquisition. The
// LocalLRC Exchange hot path acquires once per frame (up to 256 ops);
// this optional interface lets the transport take the same shared
// operation lease as a plain value instead, so the per-call closure
// allocation disappears. Semantics are IDENTICAL to AcquireMember:
// resign and destroy take the exclusive side of the operation gate and
// therefore wait for every outstanding lease before membership changes
// or teardown runs.

package core

import "sync"

// MemberLease is a shared federation operation lease held from
// membership validation through the protected service mutation(s).
// It is a small VALUE type: copying it is cheap and acquiring one does
// not allocate. The zero MemberLease is valid and Release on it is a
// no-op, so validator-only membership implementations can hand out
// zero leases.
//
// Release MUST be called exactly once per successfully acquired lease.
type MemberLease struct {
	operations *sync.RWMutex
}

// NewMemberLease wraps an operations gate whose shared (RLock) side the
// caller already holds. Release gives that shared hold back.
func NewMemberLease(operations *sync.RWMutex) MemberLease {
	return MemberLease{operations: operations}
}

// Release returns the shared operation hold. No-op on the zero value.
func (l MemberLease) Release() {
	if l.operations != nil {
		l.operations.RUnlock()
	}
}

// FederationMembershipLeaser is the allocation-free variant of
// FederationMembershipGuard. AcquireMemberLease has exactly the
// validation and fencing semantics of AcquireMember but returns the
// lease as a value instead of a bound release closure.
type FederationMembershipLeaser interface {
	FederationMembershipGuard
	AcquireMemberLease(fed FederationName, h FederateHandle) (MemberLease, error)
}
