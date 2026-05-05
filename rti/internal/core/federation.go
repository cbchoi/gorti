package core

import (
	"context"
	"time"
)

// FOMModule represents one FOM XML module submitted at federation create.
type FOMModule struct {
	Path string // optional, for diagnostics
	XML  []byte
}

// CreateFederationRequest is the input to FederationStore.CreateFederation.
type CreateFederationRequest struct {
	Name         FederationName
	FOMModules   []FOMModule
	Mode         Mode
	StallTimeout time.Duration // 0 = use server default (60s)
	Seed         uint64        // 0 = derive from name + creation time
}

// JoinFederationRequest is the input to FederationStore.JoinFederation.
type JoinFederationRequest struct {
	Federation    FederationName
	FederateName  string
}

// FederationSummary is what ListFederations returns per federation.
type FederationSummary struct {
	Name             FederationName
	Mode             Mode
	FederatesJoined  uint32
}

// FederationStore is the entry point for federation lifecycle services.
// Implementations serialize per-federation state mutations.
type FederationStore interface {
	CreateFederation(ctx context.Context, req CreateFederationRequest) error
	DestroyFederation(ctx context.Context, name FederationName) error

	JoinFederation(ctx context.Context, req JoinFederationRequest) (FederateHandle, error)
	ResignFederation(ctx context.Context, fed FederationName, h FederateHandle, action ResignAction) error

	List(ctx context.Context) ([]FederationSummary, error)

	// --- Read-only introspection (rtid-TUI Phase 1) ----------------------

	// Snapshot returns the federation roster (mode + per-federate
	// handles + names) for the AdminService handler. The federations
	// slice is sorted by name; each FederationRoster.Federates slice
	// is sorted by handle. Returns an empty slice when no federations
	// are active.
	Snapshot() []FederationRoster
}

// FederateInfo is one (handle, name) entry on a FederationRoster.
type FederateInfo struct {
	Handle FederateHandle
	Name   string
}

// FederationRoster is one federation's roster snapshot.
type FederationRoster struct {
	Name      FederationName
	Mode      Mode
	Federates []FederateInfo
}
