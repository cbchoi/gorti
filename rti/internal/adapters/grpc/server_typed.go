package grpc

import (
	"errors"
	"fmt"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// ErrUnsupportedTimeOptimization rejects Float64-only fast paths before any
// service is registered. Typed unary calls remain available with those flags off.
var ErrUnsupportedTimeOptimization = errors.New("transport/grpc: selected time representation does not support object stream optimization")

// NewServerOf binds every timestamped unary service to the same scalar backend.
// This does not change federation factory-selection policy or advertise support.
func NewServerOf[T core.LogicalTimeScalar](opts OptionsOf[T]) (*Server, error) {
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
	objectHandler := newObjectServiceOf[T](opts.Objects, Options{Sessions: opts.Sessions, Outbox: opts.Outbox})
	legacyObjectHandler, canonicalFloat := any(objectHandler).(*objectService)
	if !canonicalFloat && (opts.EnableInteractionStream || opts.EnableConfirmedObjectStream || opts.EnableLocalLRC) {
		return nil, fmt.Errorf("%w: representation %s requires typed unary object transport", ErrUnsupportedTimeOptimization, core.ScalarRepresentation[T]())
	}
	membership := opts.Membership
	if membership == nil {
		// The production federation manager implements membership fencing.
		// Adopt it automatically so embedded servers cannot accidentally emit
		// generation zero while the underlying store has advanced.
		membership, _ = opts.Federations.(core.FederationMembershipValidator)
	}
	fedSvc := newFederationService(opts.Federations)
	fedSvc.resolveTimeImplementation = opts.ResolveTimeImplementation
	fedSvc.sessions = opts.Sessions
	fedSvc.foms = opts.FOMs
	fedSvc.membership = membership
	fedSvc.onCreateFederationSuccess = opts.OnCreateFederationSuccess
	fedSvc.prepareFederation = opts.PrepareFederation
	fedSvc.onJoinFederationSuccess = opts.OnJoinFederationSuccess
	fedSvc.onDestroyFederationSuccess = opts.OnDestroyFederationSuccess
	fedSvc.ddsEnabled = opts.DDSEnabled
	fedSvc.ddsDefaultDomainID = opts.DDSDefaultDomainID
	fedSvc.transportLookup = opts.TransportLookup
	srv := &Server{
		fedService:               fedSvc,
		declService:              newDeclarationService(opts.Declarations),
		objService:               legacyObjectHandler,
		objectBackend:            objectHandler,
		streamService:            newStreamService(opts.Outbox, membership),
		timeService:              nil, // composed below when opts.Time != nil (M21 TASK-204).
		interactionStreamEnabled: opts.EnableInteractionStream,
		confirmedObjectEnabled:   opts.EnableConfirmedObjectStream,
		localLRCEnabled:          opts.EnableLocalLRC,
		membership:               membership,
		checkpointAdmission:      opts.CheckpointAdmission,
		localValidationState:     opts.LocalValidationState,
		momObserver:              opts.MOMObserver,
	}
	srv.streamService.sessions = opts.Sessions
	srv.declService.momSubscriptions = newMOMSubscriptionGuard(opts.FOMs, opts.MOM)
	srv.streamService.receipts = opts.CallbackReceipts
	srv.streamService.ownershipReceipts = opts.OwnershipReceipts
	if opts.OwnershipReceipts != nil {
		srv.ownershipCallbackService = NewOwnershipCallbackService(opts.OwnershipReceipts)
	}
	if canonicalFloat {
		srv.localLRCService = newLocalLRCService(srv)
		srv.confirmedObjectService = newConfirmedObjectService(srv)
	}
	configureTypedServices(srv, opts)
	configureTypedSupport(srv, opts)
	return srv, nil
}
func configureTypedServices[T core.LogicalTimeScalar](srv *Server, opts OptionsOf[T]) {
	// M21 TASK-204: TimeService is wired the same way as the cut-3
	// service-group entries below — register only when composed.
	if opts.Time != nil {
		srv.timeService = newTimeServiceOf[T](opts.Time)
	}
	// M12 W1: cut-3 gRPC services. Each is optional at construction
	// time so existing callers (older test harnesses, M3 / M4 cmd/rtid
	// snapshots) continue to compile + run; when nil the service is
	// simply not registered on the gRPC server.
	if opts.Sync != nil {
		srv.syncService = newSyncService(opts.Sync)
	}
	if opts.Ownership != nil {
		srv.ownershipService = newOwnershipService(opts.Ownership, opts.Objects)
	}
	if opts.DDM != nil {
		service := newDDMServiceOf[T](opts.DDM, opts.Objects)
		service.momSubscriptions = newMOMSubscriptionGuard(opts.FOMs, opts.MOM)
		if opts.Sessions != nil {
			service.callerIdentity = opts.Sessions.CurrentCallerIdentity
		}
		srv.ddmService = service
	}
	if opts.Savepoint != nil {
		srv.savepointService = newSavepointServiceOf[T](opts.Savepoint)
	}
	if opts.MOM != nil {
		srv.momService = newMomService(opts.MOM)
	}
}

func configureTypedSupport[T core.LogicalTimeScalar](srv *Server, opts OptionsOf[T]) {
	if opts.FOMs != nil {
		// M27 Phase C: if the composed Objects registry also satisfies
		// core.ObjectInstanceQuery (production *object.Registry does),
		// wire it through so GetObjectInstanceHandle / Name work. Test
		// fixtures that pass a stub ObjectRegistry get the FOM-only
		// SupportService and the two new RPCs return Unimplemented.
		if q, ok := opts.Objects.(core.ObjectInstanceQuery); ok {
			srv.supportService = newSupportServiceWithInstances(opts.FOMs, q, opts.DDM)
		} else {
			srv.supportService = newSupportService(opts.FOMs)
		}
	}
	if srv.supportService != nil {
		srv.supportService.callbackReceipts = opts.CallbackReceipts
		if opts.Sessions != nil {
			srv.supportService.callerIdentity = opts.Sessions.CurrentCallerIdentity
		}
		srv.supportService.normalizationMembers = opts.NormalizationMembers
		// Advertise only the registered, authenticated entry/completion path.
		if receipts := opts.CallbackReceipts; receipts != nil {
			srv.fedService.callbackInvocationEntrySupported = receipts.opts.Authenticate != nil &&
				receipts.opts.Observe != nil && receipts.opts.ObserveEntry != nil
		}
	}
}
