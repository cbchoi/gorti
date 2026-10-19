package server

import (
	"context"
	"fmt"
	"slices"
	"strings"

	grpcadapter "github.com/cbchoi/gorti/rti/internal/adapters/grpc"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/federation"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type momServiceAdmissionDependencies struct {
	Dispatcher   *mom.Dispatcher
	FOMs         core.FOMRepository
	Federations  *federation.Manager
	Declarations core.DeclarationManagement
	Authenticate func(context.Context, core.FederationName, core.FederateHandle) error
	Checkpoint   grpcadapter.CheckpointAdmission
}

type momServiceAdmissionKey struct{}
type momServiceAdmissionIdentity struct {
	federation     core.FederationName
	sender         core.FederateHandle
	service        string
	checkpointHeld bool
}

// Install this wrapper IN PLACE OF the ordinary membership interceptor, not
// before a second copy of it. Only FOM-defined HLAservice leaf descendants use
// the special path; all other RPCs retain normal admission. Stream paths require
// equivalent classification before acquiring their per-message ordinary lease.
func newMOMServiceAdmissionInterceptor(ordinary grpc.UnaryServerInterceptor, deps momServiceAdmissionDependencies) (grpc.UnaryServerInterceptor, error) {
	if ordinary == nil || deps.Dispatcher == nil || deps.FOMs == nil || deps.Federations == nil || deps.Declarations == nil || deps.Authenticate == nil {
		return nil, fmt.Errorf("MOM service admission dependencies incomplete")
	}
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		request, ok := req.(*rtiv1.SendInteractionRequest)
		if !ok || request == nil || info.FullMethod != rtiv1.ObjectService_SendInteraction_FullMethodName || request.WireVersion != 1 {
			return ordinary(ctx, req, info, handler)
		}
		leaf, service := momRequestServiceLeaf(ctx, deps, request)
		if !service {
			return ordinary(ctx, req, info, handler)
		}
		ctx, release, err := acquireMOMRequestAdmission(ctx, deps, request, leaf)
		if err != nil {
			return nil, err
		}
		defer release()
		return handler(ctx, req)
	}, nil
}

func momRequestServiceLeaf(ctx context.Context, deps momServiceAdmissionDependencies, request *rtiv1.SendInteractionRequest) (string, bool) {
	if request == nil || request.WireVersion != 1 {
		return "", false
	}
	fom, err := deps.FOMs.Get(ctx, core.FederationName(request.FederationName))
	if err != nil {
		return "", false
	}
	names, ok := fom.(core.FOMHandleNameLookup)
	if !ok {
		return "", false
	}
	class, defined := names.InteractionClassName(core.InteractionClassHandle(request.InteractionClassHandle))
	if !defined {
		return "", false
	}
	leaf, found := deps.Dispatcher.ServiceLeaf(class)
	if !found {
		return "", false
	}
	if strings.TrimPrefix(class, "HLAinteractionRoot.") != leaf {
		hierarchy, ok := fom.(core.FOMClassAncestry)
		leafHandle, defined := fom.LookupInteractionClass(leaf)
		if !ok || !defined || !slices.Contains(hierarchy.InteractionClassAncestors(core.InteractionClassHandle(request.InteractionClassHandle)), leafHandle) {
			return "", false
		}
	}
	return leaf, true
}

func acquireMOMRequestAdmission(ctx context.Context, deps momServiceAdmissionDependencies, request *rtiv1.SendInteractionRequest, leaf string) (context.Context, func(), error) {
	fed, sender := core.FederationName(request.FederationName), core.FederateHandle(request.FederateHandle)
	if err := deps.Authenticate(ctx, fed, sender); err != nil {
		return ctx, nil, err
	}
	checkpointRelease := func() {}
	var err error
	if deps.Checkpoint != nil {
		switch leaf {
		case mom.ServiceClassPrefix + "HLAfederateSaveBegun", mom.ServiceClassPrefix + "HLAfederateSaveComplete", mom.ServiceClassPrefix + "HLAfederateRestoreComplete":
			checkpointRelease = deps.Checkpoint.AcquireCheckpoint(fed)
		default:
			checkpointRelease, err = deps.Checkpoint.AcquireService(fed)
		}
		if err != nil {
			return ctx, nil, status.Error(codes.FailedPrecondition, err.Error())
		}
	}
	memberRelease := func() {}
	fail := func(err error) (context.Context, func(), error) {
		memberRelease()
		checkpointRelease()
		return ctx, nil, err
	}
	if leaf != mom.ServiceClassPrefix+"HLAresignFederationExecution" {
		memberRelease, err = deps.Federations.AcquireMember(fed, sender)
		if err != nil {
			memberRelease = func() {}
			return fail(status.Error(codes.FailedPrecondition, err.Error()))
		}
	}
	// Reauthenticate after admission: restore may have rebound handles while
	// the initial token check was waiting for the checkpoint or lifecycle lock.
	if err := deps.Authenticate(ctx, fed, sender); err != nil {
		return fail(err)
	}
	if !slices.Contains(deps.Declarations.InteractionPublishersFor(ctx, fed, core.InteractionClassHandle(request.InteractionClassHandle)), sender) {
		return fail(status.Error(codes.FailedPrecondition, core.ErrInteractionClassNotPublished.Error()))
	}
	ctx = context.WithValue(ctx, momServiceAdmissionKey{}, momServiceAdmissionIdentity{federation: fed, sender: sender, service: strings.TrimPrefix(leaf, mom.ServiceClassPrefix), checkpointHeld: deps.Checkpoint != nil})
	return ctx, func() { memberRelease(); checkpointRelease() }, nil
}

func heldMOMCheckpoint(ctx context.Context, fed core.FederationName) bool {
	id, ok := ctx.Value(momServiceAdmissionKey{}).(momServiceAdmissionIdentity)
	return ok && id.federation == fed && id.checkpointHeld
}

// This executor lease is a checked reuse of interceptor admission, not target
// impersonation. Direct calls without authenticated transport admission fail.
func admittedMOMServiceExecution(ctx context.Context, fed core.FederationName, sender, target core.FederateHandle, service string) (func(), error) {
	id, ok := ctx.Value(momServiceAdmissionKey{}).(momServiceAdmissionIdentity)
	if !ok || id.federation != fed || id.sender != sender || serviceNameForAdmission(id.service) != service || target == 0 {
		return nil, status.Error(codes.Unauthenticated, "MOM service requires authenticated managing-federate admission")
	}
	return func() {}, nil
}

func serviceNameForAdmission(leaf string) string {
	name := strings.TrimPrefix(leaf, "HLA")
	if name == "" {
		return ""
	}
	return strings.ToLower(name[:1]) + name[1:]
}
