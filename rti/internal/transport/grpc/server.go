package grpc

import (
	"errors"

	"github.com/cbchoi/gorti/rti/internal/core"
	"github.com/cbchoi/gorti/rti/internal/declaration"
)

// ErrNotImplemented is returned by stub methods until Agent A implements them.
var ErrNotImplemented = errors.New("transport/grpc: not implemented (Agent A M2 deliverable)")

// Server bundles the four core services into one gRPC server. Service
// handlers (federation.go, declaration.go, object.go, stream.go) hold
// references via this struct; tests construct a Server with stub
// implementations of each interface.
type Server struct {
	opts Options
}

// Options bundles Server dependencies. All MUST be non-nil except Time
// (M3 deliverable; nil at M2 means time RPCs return Unimplemented).
type Options struct {
	// Federations handles FederationService RPCs.
	Federations core.FederationStore

	// Declarations handles DeclarationService RPCs. Concrete type, not
	// interface, because declaration manager has no abstraction layer
	// (per docs/idd.md §3 — pure local component).
	Declarations *declaration.Manager

	// Objects handles ObjectService + StreamService data-plane RPCs.
	Objects core.ObjectRegistry

	// Time handles TimeService RPCs. May be nil at M2; nil → all time
	// RPCs return codes.Unimplemented.
	Time core.TimeManager

	// Outbox is referenced by the StreamService to register per-federate
	// outbound channels.
	Outbox core.Outbox
}

// NewServer constructs a Server. Validates that all required Options
// fields are non-nil.
func NewServer(opts Options) (*Server, error) {
	return &Server{opts: opts}, ErrNotImplemented
}

// Register attaches all four service handlers to the given gRPC server.
// Typically called once at process start from cmd/rtid.
//
// The signature is deliberately untyped (any) at the M2 contract layer
// to avoid pulling generated proto types into this stub file. Agent A
// substitutes the concrete grpc.ServiceRegistrar type during impl.
func (s *Server) Register(grpcServer any) error {
	_ = grpcServer
	return ErrNotImplemented
}
