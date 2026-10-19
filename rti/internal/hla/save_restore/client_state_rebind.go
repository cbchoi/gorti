package savepoint

import (
	"context"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// ClientStateValidationRequest contains only persisted source identities.
// State is detached. Validation must not reserve handles or publish state.
type ClientStateValidationRequest struct {
	Federation                    core.FederationName
	Label                         string
	SourceGeneration              uint64
	SourceHandle                  core.FederateHandle
	LogicalTimeImplementationName string
	State                         []byte
}

// ClientStateValidator fully validates a recognized SDK checkpoint format and
// its source identity before any target identity allocation is attempted.
type ClientStateValidator func(context.Context, ClientStateValidationRequest) error

// ClientStateRebindRequest describes one participant's unpublished translation.
// State and FederateHandles are detached copies that the adapter may modify.
// TargetHandle is the identity installed at commit, not the consenting session's
// current handle. All entries in FederateHandles map saved to installed handles.
type ClientStateRebindRequest struct {
	Federation                    core.FederationName
	Label                         string
	SourceGeneration              uint64
	TargetGeneration              uint64
	SourceHandle                  core.FederateHandle
	TargetHandle                  core.FederateHandle
	LogicalTimeImplementationName string
	FederateHandles               map[core.FederateHandle]core.FederateHandle
	State                         []byte
	// ReboundState is the graph adapter's candidate runtime image. It may still
	// be source-scoped, or already translated using its complete identity map.
	ReboundState []byte
}

// ClientStateRebinder owns SDK format decoding, reference translation and
// re-encoding (including integrity checks). Success must return nonempty state;
// dropping saved queues or identities is not a valid translation. The domain
// copies the result before retaining it and enforces checkpoint size limits.
type ClientStateRebinder func(context.Context, ClientStateRebindRequest) ([]byte, error)
