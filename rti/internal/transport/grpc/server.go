package grpc

import (
	"errors"
	"fmt"

	"google.golang.org/grpc"

	"github.com/cbchoi/gorti/rti/internal/core"
	"github.com/cbchoi/gorti/rti/internal/declaration"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
)

// ErrNotImplemented is returned by stub methods until the real handler
// lands. The federation handler set is implemented in this branch
// (TASK-034); declaration/object/stream remain stubs in this file and
// are owned by W3B/W3C respectively.
var ErrNotImplemented = errors.New("transport/grpc: not implemented (Agent A M2 deliverable)")

// Required-options sentinels for NewServer. Each Options field that must
// be non-nil at M2 has a dedicated sentinel so callers can match
// programmatically instead of string-sniffing.
var (
	ErrFederationsRequired  = errors.New("transport/grpc: Options.Federations is required")
	ErrDeclarationsRequired = errors.New("transport/grpc: Options.Declarations is required")
	ErrObjectsRequired      = errors.New("transport/grpc: Options.Objects is required")
	ErrOutboxRequired       = errors.New("transport/grpc: Options.Outbox is required")
)

// Server bundles the four core services into one gRPC server. Service
// handlers (federation.go, declaration.go, object.go, stream.go) hold
// references via this struct; tests construct a Server with stub
// implementations of each interface.
type Server struct {
	fedService    *federationService
	declService   *declarationService
	objService    *objectService
	streamService *streamService
	timeService   rtiv1.TimeServiceServer
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
// fields are non-nil. Time is intentionally optional at M2.
func NewServer(opts Options) (*Server, error) {
	if opts.Federations == nil {
		return nil, ErrFederationsRequired
	}
	if opts.Declarations == nil {
		return nil, ErrDeclarationsRequired
	}
	if opts.Objects == nil {
		return nil, ErrObjectsRequired
	}
	if opts.Outbox == nil {
		return nil, ErrOutboxRequired
	}
	return &Server{
		fedService:    newFederationService(opts.Federations),
		declService:   newDeclarationService(opts.Declarations),
		objService:    newObjectService(opts.Objects),
		streamService: newStreamService(opts.Outbox),
		timeService:   nil, // M3 — Time RPCs return Unimplemented when nil.
	}, nil
}

// Register attaches the service handlers to the given gRPC server. The
// argument is typed as `any` at the contract layer (matches doc.go
// frozen-shape) and asserted to grpc.ServiceRegistrar at runtime.
//
// In this branch only the FederationService is wired; declaration,
// object, and stream services are stub shells (W3B/W3C own the real
// constructors and gRPC handler interface implementations). Their
// register calls are gated by interface assertions — when W3B/W3C land
// real implementations, the assertion succeeds and the call wires up.
func (s *Server) Register(grpcServer any) error {
	gs, ok := grpcServer.(grpc.ServiceRegistrar)
	if !ok {
		return fmt.Errorf("transport/grpc: Register: want grpc.ServiceRegistrar, got %T", grpcServer)
	}

	rtiv1.RegisterFederationServiceServer(gs, s.fedService)

	// Sibling-service registrations are gated until W3B/W3C land real
	// handlers. Once the stub structs implement rtiv1.XxxServiceServer
	// (per the generated interfaces), the type assertions succeed and
	// the services attach. Until then, callers can still serve the
	// federation service in this branch without a build break.
	if impl, ok := any(s.declService).(rtiv1.DeclarationServiceServer); ok && impl != nil {
		rtiv1.RegisterDeclarationServiceServer(gs, impl)
	}
	if impl, ok := any(s.objService).(rtiv1.ObjectServiceServer); ok && impl != nil {
		rtiv1.RegisterObjectServiceServer(gs, impl)
	}
	if impl, ok := any(s.streamService).(rtiv1.StreamServiceServer); ok && impl != nil {
		rtiv1.RegisterStreamServiceServer(gs, impl)
	}
	if s.timeService != nil {
		rtiv1.RegisterTimeServiceServer(gs, s.timeService)
	}
	return nil
}

// declarationService, objectService, streamService — real implementations
// land in declaration.go (W3B), object.go and stream.go (W3C).
