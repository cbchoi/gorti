package core

import "context"

// TimeManager governs HLA time management.
// Cut 1 supports NER only; TAR added in cut 2 (see docs/srs.md FR-TM-2).
//
// Grants are emitted via Outbox.
type TimeManager interface {
	EnableRegulation(ctx context.Context, fed FederationName, h FederateHandle, lookahead LogicalTime) error
	DisableRegulation(ctx context.Context, fed FederationName, h FederateHandle) error

	EnableConstrained(ctx context.Context, fed FederationName, h FederateHandle) error
	DisableConstrained(ctx context.Context, fed FederationName, h FederateHandle) error

	NextMessageRequest(ctx context.Context, fed FederationName, h FederateHandle, t LogicalTime) error
}
