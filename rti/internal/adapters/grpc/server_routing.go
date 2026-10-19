package grpc

import (
	"context"
	"errors"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TypedBackends contains scalar-dependent services. Federation, declarations,
// ownership, DDM, synchronization and delivery remain shared dependencies.
type TypedBackends[T core.LogicalTimeScalar] struct {
	Objects   core.ObjectRegistryOf[T]
	Time      core.TimeManagerOf[T]
	Savepoint core.SavepointCoordinatorOf[T]
}

var (
	ErrTimeImplementationsRequired = errors.New("transport/grpc: dual backends require federation time implementation lookup")
	ErrInteger64TimeRequired       = errors.New("transport/grpc: Integer64.Time is required")
)

type federationTimeRouter struct {
	factories     core.FederationTimeImplementation
	resolveCaller func(context.Context) (core.FederationName, core.FederateHandle, func(), error)
}

func (r *federationTimeRouter) integer(ctx context.Context, req any) (bool, error) {
	scoped, ok := req.(federationScopedRequest)
	if !ok || scoped.GetFederationName() == "" {
		return false, status.Error(codes.InvalidArgument, "federation_name is required for typed service routing")
	}
	return r.integerFederation(ctx, core.FederationName(scoped.GetFederationName()))
}

func (r *federationTimeRouter) integerFederation(ctx context.Context, federation core.FederationName) (bool, error) {
	if r.factories == nil {
		return false, status.Error(codes.FailedPrecondition, "federation time implementation lookup is unavailable")
	}
	name, found := r.factories.LogicalTimeImplementationFor(federation)
	if !found {
		return false, errToStatus(ctx, core.ErrFederationNotFound)
	}
	switch name {
	case float64TimeFactory:
		return false, nil
	case integer64TimeFactory:
		return true, nil
	default:
		return false, status.Errorf(codes.FailedPrecondition, "unsupported federation time implementation %q", name)
	}
}

func routeUnary[S any](ctx context.Context, router *federationTimeRouter, req any, floats, integers S) (S, error) {
	var empty S
	integer, err := router.integer(ctx, req)
	if err != nil {
		return empty, err
	}
	selected := floats
	if integer {
		selected = integers
	}
	if any(selected) == nil {
		return empty, status.Error(codes.Unimplemented, "service is not configured for the federation time implementation")
	}
	return selected, nil
}

func (s *Server) installFederationRouting(opts Options) error {
	if opts.Integer64.Objects == nil {
		return ErrObjectsRequired
	}
	if opts.Integer64.Time == nil {
		return ErrInteger64TimeRequired
	}
	factories := opts.TimeImplementations
	if factories == nil {
		factories, _ = opts.Federations.(core.FederationTimeImplementation)
	}
	if factories == nil {
		return ErrTimeImplementationsRequired
	}
	router := &federationTimeRouter{factories: factories}
	if opts.MOMObserver != nil {
		router.resolveCaller = opts.MOMObserver.opts.ResolveCaller
	}
	s.timeRouter = router
	s.objectBackend = &routedObjectService{router: router, floats: s.objectBackend, integers: newObjectServiceOf[int64](opts.Integer64.Objects, Options{Sessions: opts.Sessions, Outbox: opts.Outbox})}
	s.timeService = &routedTimeService{router: router, floats: s.timeService, integers: newTimeServiceOf[int64](opts.Integer64.Time)}
	if opts.DDM != nil {
		integer := newDDMServiceOf[int64](opts.DDM, opts.Integer64.Objects)
		integer.momSubscriptions = newMOMSubscriptionGuard(opts.FOMs, opts.MOM)
		if opts.Sessions != nil {
			integer.callerIdentity = opts.Sessions.CurrentCallerIdentity
		}
		s.ddmService = &routedDDMService{router: router, floats: s.ddmService, integers: integer}
	}
	if opts.Savepoint != nil || opts.Integer64.Savepoint != nil {
		var integer rtiv1.SavepointServiceServer
		if opts.Integer64.Savepoint != nil {
			integer = newSavepointServiceOf[int64](opts.Integer64.Savepoint)
		}
		s.savepointService = &routedSavepointService{router: router, floats: s.savepointService, integers: integer}
	}
	if opts.Ownership != nil {
		s.ownershipBackend = &routedOwnershipService{router: router, floats: s.ownershipService, integers: newOwnershipService(opts.Ownership, opts.Integer64.Objects)}
	}
	if s.supportService != nil {
		integer := newSupportService(opts.FOMs)
		if instances, ok := opts.Integer64.Objects.(core.ObjectInstanceQuery); ok {
			integer = newSupportServiceWithInstances(opts.FOMs, instances, opts.DDM)
		}
		integer.callbackReceipts = opts.CallbackReceipts
		integer.normalizationMembers = opts.NormalizationMembers
		if opts.Sessions != nil {
			integer.callerIdentity = opts.Sessions.CurrentCallerIdentity
		}
		s.supportBackend = &routedSupportService{router: router, floats: s.supportService, integers: integer, shared: s.supportService}
	}
	return nil
}

// The optimized streams are Float64-only. Check decoded identity before the
// existing stream handler can admit or apply an operation, even without custom
// server interceptors. The shared callback stream is not restricted.
type timeRoutingRegistrar struct {
	grpc.ServiceRegistrar
	router *federationTimeRouter
}

func (r *timeRoutingRegistrar) RegisterService(desc *grpc.ServiceDesc, impl any) {
	if desc.ServiceName != "rti.v1.LocalLRCService" && desc.ServiceName != "rti.v1.ConfirmedObjectService" && desc.ServiceName != "rti.v1.InteractionStreamService" {
		r.ServiceRegistrar.RegisterService(desc, impl)
		return
	}
	cloned := *desc
	cloned.Streams = append([]grpc.StreamDesc(nil), desc.Streams...)
	for i := range cloned.Streams {
		original := cloned.Streams[i].Handler
		cloned.Streams[i].Handler = func(srv any, stream grpc.ServerStream) error {
			return original(srv, &floatOnlyServerStream{ServerStream: stream, router: r.router})
		}
	}
	cloned.Methods = append([]grpc.MethodDesc(nil), desc.Methods...)
	for i := range cloned.Methods {
		original := cloned.Methods[i].Handler
		cloned.Methods[i].Handler = func(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
			checked := func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
				call := func(ctx context.Context, req any) (any, error) {
					if err := r.router.requireFloat(ctx, req); err != nil {
						return nil, err
					}
					return handler(ctx, req)
				}
				if interceptor != nil {
					return interceptor(ctx, req, info, call)
				}
				return call(ctx, req)
			}
			return original(srv, ctx, dec, checked)
		}
	}
	r.ServiceRegistrar.RegisterService(&cloned, impl)
}

func (r *federationTimeRouter) requireFloat(ctx context.Context, req any) error {
	integer, err := r.integer(ctx, req)
	if err != nil {
		return err
	}
	if integer {
		return status.Error(codes.Unimplemented, "Integer64 federation requires unary object transport")
	}
	return nil
}

type floatOnlyServerStream struct {
	grpc.ServerStream
	router *federationTimeRouter
}

func (s *floatOnlyServerStream) RecvMsg(message any) error {
	if err := s.ServerStream.RecvMsg(message); err != nil {
		return err
	}
	scoped := message
	switch request := message.(type) {
	case *rtiv1.LocalLRCRequest:
		if request.GetOpen() == nil {
			return nil
		}
		scoped = request.GetOpen()
	case *rtiv1.ConfirmedObjectRequest:
		requestScope, ok := confirmedObjectScope(request)
		if !ok {
			return nil
		}
		scoped = requestScope
	case *rtiv1.SendInteractionRequest:
		if request.GetWireVersion() == 0 && request.GetFederationName() == "" && request.GetFederateHandle() == 0 {
			return nil
		}
	}
	return s.router.requireFloat(s.Context(), scoped)
}

const (
	float64TimeFactory   = "HLAfloat64Time"
	integer64TimeFactory = "HLAinteger64Time"
)
