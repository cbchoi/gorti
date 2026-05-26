// Package replication — event-log replication interface (M16.1 demo).
//
// In a production multi-node deployment, every committed event must
// be mirrored to a standby node so the federation can be promoted
// without losing state. The interface here pins the contract;
// the demo cut ships only NoopReplicator. M16 cut-3 replaces with
// a Raft-backed implementation that satisfies the quorum-commit
// contract.
//
// Wiring (M16.1 deferred to M16.3 spec test):
//   eventlog.MultiplexWriter consults Replicator BEFORE the local
//   Append, so a failure to replicate fails the commit. The demo
//   NoopReplicator never fails, preserving pre-M16 behavior.

package replication

import (
	"context"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// Replicator mirrors a committed event to a standby. Returns nil
// on success; non-nil aborts the local commit.
//
// Demo-cut implementations must be safe to call concurrently from
// the per-federation event-append paths.
type Replicator interface {
	Replicate(ctx context.Context, fed core.FederationName, evt core.EventRecord) error
}

// NoopReplicator satisfies the interface with a no-op. Used as the
// default in single-node deployments and as a stand-in until M16
// cut-3 lands the Raft-backed implementation.
type NoopReplicator struct{}

// Replicate accepts every event and returns nil. M16.1 demo.
func (NoopReplicator) Replicate(_ context.Context, _ core.FederationName, _ core.EventRecord) error {
	return nil
}

// Compile-time interface assertion.
var _ Replicator = NoopReplicator{}
