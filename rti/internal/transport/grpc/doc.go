// Package grpc binds the proto/rti/v1 services to the in-process
// implementations in rti/internal/{federation,declaration,object,time}.
//
// Owner: Agent A. Stubs in this package are part of the M2 contract.
//
// # Composition shape
//
// One Server type composes references to all four core services:
//
//	srv := grpc.NewServer(grpc.Options{
//	    Federations:  fedMgr,
//	    Declarations: declMgr,
//	    Objects:      objReg,
//	    Time:         timeMgr,  // M3
//	})
//	srv.Register(grpcServer)  // attaches all four service handlers
//
// Each service handler is a thin file (federation.go, declaration.go,
// object.go, stream.go) that translates proto request/response into
// core.* calls. No business logic lives here.
//
// # Test pattern (per docs/TDD.md §7.5)
//
// Handler tests use small inline fakes of core.FederationStore etc. —
// not mocking frameworks. Each handler test asserts:
//
//   1. Happy path produces the expected proto response.
//   2. Each documented error code is reachable from a defined input.
//   3. Idempotency where defined (e.g. resign of already-resigned).
//
// Integration tests under tests/spec/M2/grpc_test.go drive the real
// composed server.
package grpc
