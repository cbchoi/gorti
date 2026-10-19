package grpc

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cbchoi/gorti/rti/internal/adapters/auth/session"
	"google.golang.org/grpc"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// ErrNotImplemented is returned by stub methods until the real handler
// lands. The federation handler set is implemented in this branch
// (TASK-034); declaration/object/stream remain stubs in this file and
// are owned by W3B/W3C respectively.
var ErrNotImplemented = errors.New("transport/grpc: not implemented ( M2 deliverable)")

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
	objectBackend            rtiv1.ObjectServiceServer
	streamService            *streamService
	timeService              rtiv1.TimeServiceServer
	syncService              *syncService
	ownershipService         *ownershipService
	ownershipBackend         rtiv1.OwnershipServiceServer
	ownershipCallbackService rtiv1.OwnershipCallbackServiceServer
	ddmService               rtiv1.DDMServiceServer
	savepointService         rtiv1.SavepointServiceServer
	momService               *momService
	supportService           *supportService
	supportBackend           rtiv1.SupportServiceServer
	timeRouter               *federationTimeRouter
	interactionStreamEnabled bool
	confirmedObjectEnabled   bool
	localLRCEnabled          bool
	localLRCService          *localLRCService
	confirmedObjectService   *confirmedObjectService
	membership               core.FederationMembershipValidator
	checkpointAdmission      CheckpointAdmission
	localValidationState     core.LocalValidationStateSource
	momObserver              *MOMObserver
}

// Options bundles Server dependencies. All MUST be non-nil except Time
// (M3 deliverable; nil at M2 means time RPCs return Unimplemented).
type Options = OptionsOf[core.LogicalTime]

type OptionsOf[T core.LogicalTimeScalar] struct {
	// Integer64 adds a second authoritative unary backend to legacy NewServer.
	// TimeImplementations resolves the immutable factory selected by federation.
	Integer64           *TypedBackends[int64]
	TimeImplementations core.FederationTimeImplementation
	// ResolveTimeImplementation is supplied by composition when additional
	// complete backends are available. Nil preserves the legacy factory policy.
	ResolveTimeImplementation func(string) (string, error)
	Sessions                  *session.Manager
	LocalValidationState      core.LocalValidationStateSource
	CallbackReceipts          *CallbackReceipts
	OwnershipReceipts         *OwnershipCallbackReceipts
	MOMObserver               *MOMObserver
	NormalizationMembers      func(core.FederationName) []core.FederateHandle
	// Federations handles FederationService RPCs.
	Federations core.FederationStore

	// Membership validates every request carrying a federation name and
	// federate handle against the current federation generation. Optional for
	// embedded legacy fixtures; production must wire federation.Manager.
	Membership core.FederationMembershipValidator

	// CheckpointAdmission is optional when Save/Restore is disabled.
	CheckpointAdmission CheckpointAdmission

	// Declarations handles DeclarationService RPCs.
	//
	// Phase 1 research-platform refactor (docs/research-platform.md
	// §5.6): typed as core.DeclarationManagement so alternative
	// implementations may be wired here. The cut-2 docs/idd.md §3 note
	// that called declaration "pure local component, no abstraction
	// layer" has been revised in the same Phase 1 commit.
	Declarations core.DeclarationManagement

	// Objects handles ObjectService + StreamService data-plane RPCs.
	Objects core.ObjectRegistryOf[T]

	// Time handles TimeService RPCs. May be nil at M2; nil → all time
	// RPCs return codes.Unimplemented.
	Time core.TimeManagerOf[T]

	// Outbox is referenced by the StreamService to register per-federate
	// outbound channels.
	Outbox core.Outbox

	// EnableInteractionStream enables the internal persistent interaction
	// transport. Leave false for embedded servers with unary-only interceptors.
	EnableInteractionStream bool

	// EnableConfirmedObjectStream enables the ordered, synchronous
	// UpdateAttributeValues/SendInteraction stream used by the Go SDK. Unary
	// ObjectService RPCs remain registered for older and authenticated clients.
	EnableConfirmedObjectStream bool

	// EnableLocalLRC enables the ordered receive-order Object Management
	// pipeline used by the Go SDK's bounded in-process LocalLRC.
	EnableLocalLRC bool

	// OnCreateFederationSuccess, when non-nil, is invoked after every
	// successful CreateFederation gRPC call with the federation name
	// and the FOM modules supplied by the client. The composition root
	// (rtid main) wires this to populate the FOM repository's
	// per-federation handle map (foms.RememberFor) so that
	// FOMRepoOrderLookup can resolve per-class declared order at
	// best-effort interaction-send time. Tests may leave this nil.
	OnCreateFederationSuccess func(ctx context.Context, name core.FederationName, modules []core.FOMModule)
	// PrepareFederation runs before publishing FOM/MOM state under exclusive
	// creation admission. Failure aborts the new execution.
	PrepareFederation       func(context.Context, core.FederationName) error
	OnJoinFederationSuccess func(context.Context, core.FederationName, core.FederateHandle)

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
	Savepoint core.SavepointCoordinatorOf[T]

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
	srv, err := NewServerOf[core.LogicalTime](opts)
	if err != nil || opts.Integer64 == nil {
		return srv, err
	}
	if err := srv.installFederationRouting(opts); err != nil {
		return nil, err
	}
	return srv, nil
}

type federateScopedRequest interface {
	GetFederationName() string
	GetFederateHandle() uint64
}

func (s *Server) validateMembership(ctx context.Context, request any) error {
	if legacyGlobalObjectLookup(request) {
		return nil
	}
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
	if legacyGlobalObjectLookup(request) {
		return nil, nil
	}
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
		// Mandatory receipts own one checked scope lease, including checkpoint
		// observation admission. Reacquiring it here would invert or recurse locks.
		if strings.HasPrefix(info.FullMethod, "/rti.v1.OwnershipCallbackService/") {
			return handler(ctx, req)
		}
		releaseAdmission, err := acquireCheckpointAdmission(ctx, s.checkpointAdmission, req, info.FullMethod)
		if err != nil {
			return nil, err
		}
		if releaseAdmission != nil {
			defer releaseAdmission()
			if scoped, ok := req.(federationScopedRequest); ok {
				// Handleless observations and rejected signed resigns resolve a
				// caller and must reuse this lease rather than reacquiring it.
				member, hasMember := req.(federateScopedRequest)
				if !hasMember || member.GetFederateHandle() == 0 || scoped.GetFederationName() == "" || info.FullMethod == rtiv1.FederationService_ResignFederation_FullMethodName {
					ctx = context.WithValue(ctx, checkpointAdmissionContextKey{}, core.FederationName(scoped.GetFederationName()))
				}
			}
		}
		// Resign takes the exclusive side of the same operation gate inside the
		// federation manager after validating the action and membership.
		if info.FullMethod == "/rti.v1.FederationService/ResignFederation" ||
			info.FullMethod == "/rti.v1.LocalLRCService/GetLocalValidationState" ||
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
	server                  *Server
	interaction             bool
	confirmedObject         bool
	handshakePassed         bool
	currentRelease          func()
	currentAdmissionRelease func()
	currentValidated        bool
}

type confirmedObjectMembershipValidatedKey struct{}

func (s *membershipServerStream) Context() context.Context {
	ctx := s.ServerStream.Context()
	if s.confirmedObject && s.currentValidated {
		return context.WithValue(ctx, confirmedObjectMembershipValidatedKey{}, true)
	}
	return ctx
}

func (s *membershipServerStream) releaseCurrent() {
	if s.currentRelease != nil {
		s.currentRelease()
		s.currentRelease = nil
	}
	if s.currentAdmissionRelease != nil {
		s.currentAdmissionRelease()
		s.currentAdmissionRelease = nil
	}
	s.currentValidated = false
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
	if s.confirmedObject {
		scoped, ok := confirmedObjectScope(message)
		if !ok {
			// The protocol handler reports missing or malformed operations.
			return nil
		}
		if err := s.acquireOperation(scoped); err != nil {
			return err
		}
		s.currentValidated = true
		return nil
	}
	if !s.interaction {
		return s.server.validateMembership(s.Context(), message)
	}
	return s.acquireOperation(message)
}

func confirmedObjectScope(message any) (federateScopedRequest, bool) {
	request, ok := message.(*rtiv1.ConfirmedObjectRequest)
	if !ok || request == nil {
		return nil, false
	}
	if update := request.GetAttributeUpdate(); update != nil {
		return update, true
	}
	if interaction := request.GetInteraction(); interaction != nil {
		return interaction, true
	}
	if open := request.GetOpen(); open != nil {
		return open, true
	}
	return nil, false
}

func (s *membershipServerStream) SendMsg(message any) error {
	// Mutation and callback admission have finished. Network flow control must
	// not hold either lease while checkpoint or lifecycle work is waiting.
	s.releaseCurrent()
	return s.ServerStream.SendMsg(message)
}

// StreamMembershipInterceptor validates the initial Events request and every
// persistent interaction message. Only the protocol's empty capability
// handshake is exempt.
func (s *Server) StreamMembershipInterceptor() grpc.StreamServerInterceptor {
	return func(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		wrapped := &membershipServerStream{
			ServerStream:    stream,
			server:          s,
			interaction:     info.FullMethod == interactionStreamMethod,
			confirmedObject: info.FullMethod == rtiv1.ConfirmedObjectService_Exchange_FullMethodName,
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
	if s.timeRouter != nil {
		gs = &timeRoutingRegistrar{ServiceRegistrar: gs, router: s.timeRouter}
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
	objectHandler := s.objectBackend
	if objectHandler == nil && s.objService != nil {
		objectHandler = s.objService
	}
	if objectHandler != nil {
		rtiv1.RegisterObjectServiceServer(gs, objectHandler)
		if s.confirmedObjectEnabled && s.confirmedObjectService != nil {
			rtiv1.RegisterConfirmedObjectServiceServer(gs, s.confirmedObjectService)
		}
		if s.interactionStreamEnabled && s.objService != nil {
			gs.RegisterService(&interactionStreamServiceDescription, &interactionStreamHandler{objects: s.objService, observer: s.momObserver})
		}
	}
	if s.localLRCEnabled && s.localLRCService != nil {
		rtiv1.RegisterLocalLRCServiceServer(gs, s.localLRCService)
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
	if s.ownershipBackend != nil {
		rtiv1.RegisterOwnershipServiceServer(gs, s.ownershipBackend)
	} else if s.ownershipService != nil {
		rtiv1.RegisterOwnershipServiceServer(gs, s.ownershipService)
	}
	if s.ownershipCallbackService != nil {
		rtiv1.RegisterOwnershipCallbackServiceServer(gs, s.ownershipCallbackService)
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
	if s.supportBackend != nil {
		rtiv1.RegisterSupportServiceServer(gs, s.supportBackend)
		registerCallbackReportService(gs, s.supportBackend)
	} else if s.supportService != nil {
		rtiv1.RegisterSupportServiceServer(gs, s.supportService)
		registerCallbackReportService(gs, s.supportService)
	}
	return nil
}

// declarationService, objectService, streamService — real implementations
// land in declaration.go (W3B), object.go and stream.go (W3C).
