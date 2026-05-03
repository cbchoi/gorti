package m12spec

import (
	"testing"
)

// TestSpec_M12_SyncService_GRPCRoundTrip: SyncService.RegisterFederationSynchronizationPoint
// + SynchronizationPointAchieved invokable via real gRPC.
//
// SCAFFOLD: Agent A wires the round-trip test in M12 W1 by extending
// the existing transport/grpc test helpers (server_test.go) to also
// register SyncService + dialing a client via grpc.Dial.
//
// Implements: M12 — sync gRPC exposure.
func TestSpec_M12_SyncService_GRPCRoundTrip(t *testing.T) {
	t.Skip("Agent A wires SyncService gRPC handler + round-trip test in M12 W1")
}

// TestSpec_M12_OwnershipService_GRPCRoundTrip: NegotiatedDivest +
// Acquire flow over real gRPC.
//
// SCAFFOLD.
//
// Implements: M12 — ownership gRPC exposure.
func TestSpec_M12_OwnershipService_GRPCRoundTrip(t *testing.T) {
	t.Skip("Agent A wires OwnershipService gRPC handler + round-trip test in M12 W1")
}

// TestSpec_M12_DDMService_GRPCRoundTrip: CreateRegion + SetRangeBounds
// + CommitRegionModifications + QueryBounds over real gRPC.
//
// SCAFFOLD.
//
// Implements: M12 — DDM gRPC exposure.
func TestSpec_M12_DDMService_GRPCRoundTrip(t *testing.T) {
	t.Skip("Agent A wires DDMService gRPC handler + round-trip test in M12 W1")
}

// TestSpec_M12_SavepointService_GRPCRoundTrip: RequestFederationSave
// → FederateSaveComplete aggregation over real gRPC.
//
// SCAFFOLD.
//
// Implements: M12 — savepoint gRPC exposure.
func TestSpec_M12_SavepointService_GRPCRoundTrip(t *testing.T) {
	t.Skip("Agent A wires SavepointService gRPC handler + round-trip test in M12 W1")
}

// TestSpec_M12_AllServicesRegistered: rtid's grpc.Server registers all
// 8 services (the 4 cut-1 + the 4 new cut-3 ones for sync/ownership/
// DDM/savepoint). Asserted by inspecting server.GetServiceInfo().
//
// SCAFFOLD.
//
// Implements: M12 — proto + gRPC handler completeness.
func TestSpec_M12_AllServicesRegistered(t *testing.T) {
	t.Skip("Agent A wires the service-registration check in M12 W1; expect 8 registered services post-M12")
}
