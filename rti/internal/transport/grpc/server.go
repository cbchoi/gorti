package grpc

import (
	"context"
	"errors"
	"fmt"

	"google.golang.org/grpc"

	"github.com/cbchoi/gorti/rti/internal/core"
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

// Server bundles the core services into one gRPC server. Service
// handlers (federation.go, declaration.go, object.go, stream.go,
// sync.go, ownership.go, ddm.go, savepoint.go) hold references via
// this struct; tests construct a Server with stub implementations of
// each interface.
type Server struct {
	fedService               *federationService
	declService              *declarationService
	objService               *objectService
	streamService            *streamService
	timeService              rtiv1.TimeServiceServer
	syncService              *syncService
	ownershipService         *ownershipService
	ddmService               *ddmService
	savepointService         *savepointService
	momService               *momService
	supportService           *supportService
	interactionStreamEnabled bool
	membership               core.FederationMembershipValidator
}

// Options bundles Server dependencies. All MUST be non-nil except Time
// (M3 deliverable; nil at M2 means time RPCs return Unimplemented).
type Options struct {
	// Federations handles FederationService RPCs.
	Federations core.FederationStore

	// Membership validates every request carrying a federation name and
	// federate handle against the current federation generation. Optional for
	// embedded legacy fixtures; production must wire federation.Manager.
	Membership core.FederationMembershipValidator

	// Declarations handles DeclarationService RPCs.
	//
	// Phase 1 research-platform refactor (docs/research-platform.md
	// §5.6): typed as core.DeclarationManagement so alternative
	// implementations may be wired here. The cut-2 docs/idd.md §3 note
	// that called declaration "pure local component, no abstraction
	// layer" has been revised in the same Phase 1 commit.
	Declarations core.DeclarationManagement

	// Objects handles ObjectService + StreamService data-plane RPCs.
	Objects core.ObjectRegistry

	// Time handles TimeService RPCs. May be nil at M2; nil → all time
	// RPCs return codes.Unimplemented.
	Time core.TimeManager

	// Outbox is referenced by the StreamService to register per-federate
	// outbound channels.
	Outbox core.Outbox

	// EnableInteractionStream enables the internal persistent interaction
	// transport. Leave false for embedded servers with unary-only interceptors.
	EnableInteractionStream bool

	// OnCreateFederationSuccess, when non-nil, is invoked after every
	// successful CreateFederation gRPC call with the federation name
	// and the FOM modules supplied by the client. The composition root
	// (rtid main) wires this to populate the FOM repository's
	// per-federation handle map (foms.RememberFor) so that
	// FOMRepoOrderLookup can resolve per-class declared order at
	// best-effort interaction-send time. Tests may leave this nil.
	OnCreateFederationSuccess func(ctx context.Context, name core.FederationName, modules []core.FOMModule)

	// OnDestroyFederationSuccess, when non-nil, is invoked after every
	// successful DestroyFederation gRPC call. M11 wires this to
	// MOM.FederationDestroyed so the HLAfederation MOM instance is
	// retired. Tests may leave this nil.
	OnDestroyFederationSuccess func(ctx context.Context, name core.FederationName)

	// Sync handles SyncService RPCs (M12 W1: cut-3 gRPC exposure of
	// the cut-2 sync.Manager). May be nil at construction time — when
	// nil, the SyncService is NOT registered on the gRPC server (the
	// service is simply absent from GetServiceInfo). Production wiring
	// in cmd/rtid passes a real *syncpkg.Manager.
	//
	// Phase 1 research-platform refactor (docs/research-platform.md
	// §5.1): typed as core.SyncCoordinator so alternative
	// implementations may be wired here.
	Sync core.SyncCoordinator

	// Ownership handles OwnershipService RPCs (M12 W1).
	//
	// Phase 1 research-platform refactor (docs/research-platform.md
	// §5.2): typed as core.OwnershipCoordinator so alternative
	// implementations may be wired here.
	Ownership core.OwnershipCoordinator

	// DDM handles DDMService RPCs (M12 W1).
	//
	// Phase 1 research-platform refactor (docs/research-platform.md
	// §5.4): typed as core.DataDistributionManagement so alternative
	// implementations may be wired here.
	DDM core.DataDistributionManagement

	// Savepoint handles SavepointService RPCs (M12 W1).
	//
	// Phase 1 research-platform refactor (docs/research-platform.md
	// §5.5): typed as core.SavepointCoordinator so alternative
	// implementations may be wired here.
	Savepoint core.SavepointCoordinator

	// MOM handles MomService RPCs (M12 W3: cut-3 gRPC exposure of the
	// cut-2 mom.Manager). May be nil at construction time — when nil,
	// MomService is NOT registered on the gRPC server (the service is
	// simply absent from GetServiceInfo). Production wiring in
	// cmd/rtid passes the federate-port-side ManagementObjectModel so
	// federates can introspect HLAfederation / HLAfederate state via
	// the standard MOM RPCs.
	//
	// Phase 1 research-platform refactor (docs/research-platform.md
	// §5.3): typed as core.ManagementObjectModel so alternative
	// implementations may be wired here.
	MOM core.ManagementObjectModel

	// DDSEnabled, DDSDefaultDomainID, and TransportLookup are M19
	// Phase 1a (docs/m19-dds-adapter.md §4.4). DDSEnabled gates
	// whether CreateFederation will accept TRANSPORT_MODE_DDS; in the
	// default CGo-free build it MUST stay false (a DDS request is
	// rejected with FailedPrecondition + a clear "not built with DDS
	// support" message). DDSDefaultDomainID is the domain ID stamped
	// into a DDS-mode federation at create time when the request
	// itself does not pin a domain. TransportLookup is the manager's
	// federation.Manager.TransportFor; nil leaves the join response
	// at UNSPECIFIED (collapses to GRPC at the wire layer).
	DDSEnabled         bool
	DDSDefaultDomainID int32
	TransportLookup    func(core.FederationName) (core.TransportMode, int32, bool)

	// FOMs is the federation-scoped FOM repository consumed by
	// SupportService (M25 Phase B — IEEE 1516.1-2010 §10.2 handle /
	// name / dimension / order / transport lookups). When nil, the
	// SupportService is NOT registered. Composition in cmd/rtid wires
	// the same fomRepository instance that the federation manager
	// already uses for Load + RememberFor.
	FOMs core.FOMRepository
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
	membership := opts.Membership
	if membership == nil {
		// The production federation manager implements membership fencing.
		// Adopt it automatically so embedded servers cannot accidentally emit
		// generation zero while the underlying store has advanced.
		membership, _ = opts.Federations.(core.FederationMembershipValidator)
	}
	fedSvc := newFederationService(opts.Federations)
	fedSvc.membership = membership
	fedSvc.onCreateFederationSuccess = opts.OnCreateFederationSuccess
	fedSvc.onDestroyFederationSuccess = opts.OnDestroyFederationSuccess
	fedSvc.ddsEnabled = opts.DDSEnabled
	fedSvc.ddsDefaultDomainID = opts.DDSDefaultDomainID
	fedSvc.transportLookup = opts.TransportLookup
	srv := &Server{
		fedService:               fedSvc,
		declService:              newDeclarationService(opts.Declarations),
		objService:               newObjectService(opts.Objects),
		streamService:            newStreamService(opts.Outbox),
		timeService:              nil, // composed below when opts.Time != nil (M21 TASK-204).
		interactionStreamEnabled: opts.EnableInteractionStream,
		membership:               membership,
	}
	// M21 TASK-204: TimeService is wired the same way as the cut-3
	// service-group entries below — register only when composed.
	if opts.Time != nil {
		srv.timeService = newTimeService(opts.Time)
	}
	// M12 W1: cut-3 gRPC services. Each is optional at construction
	// time so existing callers (older test harnesses, M3 / M4 cmd/rtid
	// snapshots) continue to compile + run; when nil the service is
	// simply not registered on the gRPC server.
	if opts.Sync != nil {
		srv.syncService = newSyncService(opts.Sync)
	}
	if opts.Ownership != nil {
		srv.ownershipService = newOwnershipService(opts.Ownership)
	}
	if opts.DDM != nil {
		srv.ddmService = newDDMService(opts.DDM, opts.Objects)
	}
	if opts.Savepoint != nil {
		srv.savepointService = newSavepointService(opts.Savepoint)
	}
	if opts.MOM != nil {
		srv.momService = newMomService(opts.MOM)
	}
	if opts.FOMs != nil {
		// M27 Phase C: if the composed Objects registry also satisfies
		// core.ObjectInstanceQuery (production *object.Registry does),
		// wire it through so GetObjectInstanceHandle / Name work. Test
		// fixtures that pass a stub ObjectRegistry get the FOM-only
		// SupportService and the two new RPCs return Unimplemented.
		if q, ok := opts.Objects.(core.ObjectInstanceQuery); ok {
			srv.supportService = newSupportServiceWithInstances(opts.FOMs, q)
		} else {
			srv.supportService = newSupportService(opts.FOMs)
		}
	}
	return srv, nil
}

type federateScopedRequest interface {
	GetFederationName() string
	GetFederateHandle() uint64
}

func (s *Server) validateMembership(ctx context.Context, request any) error {
	if s.membership == nil {
		return nil
	}
	scoped, ok := request.(federateScopedRequest)
	if !ok {
		return nil
	}
	return errToStatus(ctx, s.membership.ValidateMember(
		core.FederationName(scoped.GetFederationName()),
		core.FederateHandle(scoped.GetFederateHandle()),
	))
}

func (s *Server) acquireMembership(ctx context.Context, request any) (func(), error) {
	if s.membership == nil {
		return nil, nil
	}
	scoped, ok := request.(federateScopedRequest)
	if !ok {
		return nil, nil
	}
	if guard, ok := s.membership.(core.FederationMembershipGuard); ok {
		release, err := guard.AcquireMember(
			core.FederationName(scoped.GetFederationName()),
			core.FederateHandle(scoped.GetFederateHandle()),
		)
		return release, errToStatus(ctx, err)
	}
	return nil, s.validateMembership(ctx, request)
}

// UnaryMembershipInterceptor rejects stale, forged, and resigned handles
// before any service manager can create or mutate federation-scoped state.
func (s *Server) UnaryMembershipInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		// Resign takes the exclusive side of the same operation gate inside the
		// federation manager after validating the action and membership.
		if info.FullMethod == "/rti.v1.FederationService/ResignFederation" ||
			// This MOM field is the lookup target, not the calling federate. The
			// request has no caller handle and is read-only.
			info.FullMethod == "/rti.v1.MomService/QueryFederateAttributes" {
			return handler(ctx, req)
		}
		release, err := s.acquireMembership(ctx, req)
		if err != nil {
			return nil, err
		}
		if release != nil {
			defer release()
		}
		return handler(ctx, req)
	}
}

type membershipServerStream struct {
	grpc.ServerStream
	server          *Server
	interaction     bool
	handshakePassed bool
	currentRelease  func()
}

func (s *membershipServerStream) releaseCurrent() {
	if s.currentRelease != nil {
		s.currentRelease()
		s.currentRelease = nil
	}
}

func (s *membershipServerStream) RecvMsg(message any) error {
	// A protocol handler normally releases on its ACK SendMsg. If it advances
	// without one, the previous mutation is nevertheless complete at this next
	// receive boundary.
	s.releaseCurrent()
	if err := s.ServerStream.RecvMsg(message); err != nil {
		return err
	}
	if s.interaction && !s.handshakePassed {
		s.handshakePassed = true
		if scoped, ok := message.(federateScopedRequest); ok &&
			scoped.GetFederationName() == "" && scoped.GetFederateHandle() == 0 {
			return nil
		}
	}
	if !s.interaction {
		return s.server.validateMembership(s.Context(), message)
	}
	release, err := s.server.acquireMembership(s.Context(), message)
	if err != nil {
		return err
	}
	s.currentRelease = release
	return nil
}

func (s *membershipServerStream) SendMsg(message any) error {
	err := s.ServerStream.SendMsg(message)
	s.releaseCurrent()
	return err
}

// StreamMembershipInterceptor validates the initial Events request and every
// persistent interaction message. Only the protocol's empty capability
// handshake is exempt.
func (s *Server) StreamMembershipInterceptor() grpc.StreamServerInterceptor {
	return func(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		wrapped := &membershipServerStream{
			ServerStream: stream,
			server:       s,
			interaction:  info.FullMethod == interactionStreamMethod,
		}
		defer wrapped.releaseCurrent()
		return handler(srv, wrapped)
	}
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
		if s.interactionStreamEnabled {
			gs.RegisterService(&interactionStreamServiceDescription, &interactionStreamHandler{objects: s.objService})
		}
	}
	if impl, ok := any(s.streamService).(rtiv1.StreamServiceServer); ok && impl != nil {
		rtiv1.RegisterStreamServiceServer(gs, impl)
	}
	if s.timeService != nil {
		rtiv1.RegisterTimeServiceServer(gs, s.timeService)
	}
	// M12 W1: cut-3 services — register only if composed.
	if s.syncService != nil {
		rtiv1.RegisterSyncServiceServer(gs, s.syncService)
	}
	if s.ownershipService != nil {
		rtiv1.RegisterOwnershipServiceServer(gs, s.ownershipService)
	}
	if s.ddmService != nil {
		rtiv1.RegisterDDMServiceServer(gs, s.ddmService)
	}
	if s.savepointService != nil {
		rtiv1.RegisterSavepointServiceServer(gs, s.savepointService)
	}
	if s.momService != nil {
		rtiv1.RegisterMomServiceServer(gs, s.momService)
	}
	if s.supportService != nil {
		rtiv1.RegisterSupportServiceServer(gs, s.supportService)
	}
	return nil
}

// declarationService, objectService, streamService — real implementations
// land in declaration.go (W3B), object.go and stream.go (W3C).
