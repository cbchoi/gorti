package grpc

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"

	"github.com/cbchoi/gorti/rti/internal/adapters/auth/session"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

type ServiceObservation struct {
	Federation                           core.FederationName
	Federate                             core.FederateHandle
	Service                              string
	Success                              bool
	SuppliedArguments, ReturnedArguments []string
	Exception                            string
}
type MOMObserverOptions struct {
	Observe          func(context.Context, ServiceObservation) error
	OnError          func(error)
	ResolveCaller    func(context.Context) (core.FederationName, core.FederateHandle, func(), error)
	CaptureArguments func(core.FederationName, core.FederateHandle) bool
}
type MOMObserver struct{ opts MOMObserverOptions }

type resignObservationKey struct{}
type resignObservation struct {
	owner     *MOMObserver
	request   *rtiv1.ResignFederationRequest
	completed atomic.Bool
}

func NewMOMObserver(opts MOMObserverOptions) *MOMObserver { return &MOMObserver{opts: opts} }
func (o *MOMObserver) UnaryInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, next grpc.UnaryHandler) (any, error) {
		var resign *resignObservation
		if o != nil && o.opts.Observe != nil && info.FullMethod == rtiv1.FederationService_ResignFederation_FullMethodName {
			if request, ok := req.(*rtiv1.ResignFederationRequest); ok && request != nil {
				resign = &resignObservation{owner: o, request: request}
				ctx = context.WithValue(ctx, resignObservationKey{}, resign)
			}
		}
		resp, err := next(ctx, req)
		if resign == nil || !resign.completed.Load() {
			o.Observe(ctx, info.FullMethod, req, resp, err)
		}
		return resp, err
	}
}

// ObserveResignOutcome runs under the federation's exclusive lifecycle lease,
// after its roster mutation but before MOM state and subscriptions are retired.
// Calls rejected before that point are observed by the ordinary interceptor.
func (o *MOMObserver) ObserveResignOutcome(ctx context.Context, fed core.FederationName, h core.FederateHandle, action core.ResignAction, outcome error) {
	state, _ := ctx.Value(resignObservationKey{}).(*resignObservation)
	if state == nil || state.owner != o || state.request.GetFederationName() != string(fed) || h == 0 || !state.completed.CompareAndSwap(false, true) {
		return
	}
	// A restore between authentication and exclusive admission may rebind the
	// handle. Attribute the actual retired membership, not the old request handle.
	request := &rtiv1.ResignFederationRequest{WireVersion: state.request.GetWireVersion(), FederationName: string(fed), FederateHandle: uint64(h), Action: coreResignActionToProto(action)}
	o.Observe(ctx, rtiv1.FederationService_ResignFederation_FullMethodName, request, &rtiv1.Empty{}, errToStatus(ctx, outcome))
}

// Observe records a completed service without changing its result. Unary calls
// reach it through the interceptor; streaming operations call it once at their
// inner service return, never at an acknowledgement or channel-delivery boundary.
func (o *MOMObserver) Observe(ctx context.Context, method string, request, response any, serviceErr error) {
	if o == nil || o.opts.Observe == nil {
		return
	}
	service := momRPCService(method, request)
	if service == "" {
		return
	}
	fed, h := o.observationIdentity(ctx, method, service, request, response, serviceErr)
	if (fed == "" || h == 0) && o.opts.ResolveCaller != nil {
		var err error
		var release func()
		fed, h, release, err = o.opts.ResolveCaller(ctx)
		if release != nil {
			defer release()
		}
		if err != nil {
			o.failed(err)
			return
		}
	}
	// Unjoined operations have no HLAfederate subject to report against.
	if fed == "" || h == 0 {
		return
	}
	o.recordServiceObservation(ctx, fed, h, service, request, response, serviceErr)
}

func (o *MOMObserver) failed(err error) {
	if o.opts.OnError != nil {
		o.opts.OnError(err)
	}
}

func momRPCIdentity(request any) (core.FederationName, core.FederateHandle) {
	var fed core.FederationName
	var h core.FederateHandle
	if r, ok := request.(interface{ GetFederationName() string }); ok {
		fed = core.FederationName(r.GetFederationName())
	}
	if r, ok := request.(interface{ GetFederateHandle() uint64 }); ok {
		h = core.FederateHandle(r.GetFederateHandle())
	}
	return fed, h
}

func momRPCService(method string, request any) string {
	// Activation completes an already observed preparation, not another HLA service.
	if method == rtiv1.ObjectService_ActivateTransportationTypeChange_FullMethodName {
		return ""
	}
	parts := strings.Split(method, "/")
	if len(parts) != 3 {
		return ""
	}
	switch parts[1] {
	case "rti.v1.FederationService", "rti.v1.DeclarationService", "rti.v1.ObjectService", "rti.v1.OwnershipService", "rti.v1.TimeService", "rti.v1.DDMService", "rti.v1.SupportService", "rti.v1.SavepointService", "rti.v1.SyncService":
	default:
		return ""
	}
	name := parts[2]
	if alias, ok := momServiceAliases[name]; ok {
		name = alias
	} else if name != "" {
		name = strings.ToLower(name[:1]) + name[1:]
	}
	if req, ok := request.(*rtiv1.SetAdvisorySwitchRequest); ok {
		prefix := "disable"
		if req.GetEnabled() {
			prefix = "enable"
		}
		switch req.GetSwitchKind() {
		case 1:
			name = prefix + "ObjectClassRelevanceAdvisorySwitch"
		case 2:
			name = prefix + "AttributeRelevanceAdvisorySwitch"
		case 3:
			name = prefix + "AttributeScopeAdvisorySwitch"
		case 4:
			name = prefix + "InteractionRelevanceAdvisorySwitch"
		default:
			return ""
		}
	}
	if _, known := mom.ServiceGroup(name); !known {
		return ""
	}
	return name
}

var momServiceAliases = map[string]string{
	"LocalDeleteObjectInstanceFenced": "localDeleteObjectInstance",
	"CreateFederation":                "createFederationExecution", "DestroyFederation": "destroyFederationExecution", "JoinFederation": "joinFederationExecution", "ResignFederation": "resignFederationExecution", "ListFederations": "listFederationExecutions",
	"RequestClassAttributeValueUpdate": "requestAttributeValueUpdate", "ChangeAttributeTransportationType": "requestAttributeTransportationTypeChange", "ChangeInteractionTransportationType": "requestInteractionTransportationTypeChange",
	"PrepareAttributeTransportationTypeChange": "requestAttributeTransportationTypeChange", "PrepareInteractionTransportationTypeChange": "requestInteractionTransportationTypeChange",
	"AttributeOwnershipDivestitureIfWantedWithResult": "attributeOwnershipDivestitureIfWanted", "QueryBounds": "getRangeBounds", "LookupDimension": "getDimensionHandle",
	"GetTransportationType": "getTransportationTypeHandle", "GetTransportationName": "getTransportationTypeName", "QuerySaveState": "queryFederationSaveStatus", "QueryRestoreState": "queryFederationRestoreStatus",
}

// Each actual wire argument is rendered independently in descriptor order.
// Routing, authentication, protocol bookkeeping and absent optional values are
// excluded. No default return arguments are fabricated for failed/void calls.
func momArguments(value any) []string {
	message, ok := value.(proto.Message)
	if !ok || message == nil || !message.ProtoReflect().IsValid() {
		return nil
	}
	data, err := (protojson.MarshalOptions{UseProtoNames: true, EmitDefaultValues: true}).Marshal(message)
	if err != nil {
		return nil
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil {
		return nil
	}
	var args []string
	descriptor := message.ProtoReflect().Descriptor().Fields()
	for i := 0; i < descriptor.Len(); i++ {
		field := descriptor.Get(i)
		name := string(field.Name())
		if momPrivateArgument(name) {
			continue
		}
		if field.HasPresence() && !message.ProtoReflect().Has(field) {
			continue
		}
		if encoded, ok := fields[name]; ok {
			args = append(args, name+"="+string(momRedactJSON(encoded)))
		}
	}
	return args
}

func momRedactJSON(encoded json.RawMessage) json.RawMessage {
	var value any
	if json.Unmarshal(encoded, &value) != nil {
		return nil
	}
	var redact func(any)
	redact = func(v any) {
		switch v := v.(type) {
		case map[string]any:
			for name, child := range v {
				if momSensitiveArgument(name) {
					delete(v, name)
				} else {
					redact(child)
				}
			}
		case []any:
			for _, child := range v {
				redact(child)
			}
		}
	}
	redact(value)
	result, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return result
}

func momPrivateArgument(name string) bool {
	switch name {
	case "wire_version", "federation_name", "federate_handle", "expected_federation_generation", "federation_generation", "fom_revision", "fom_digest", "sequence", "seq", "callback_epoch", "expected_callback_epoch":
		return true
	}
	return momSensitiveArgument(name)
}

func momSensitiveArgument(name string) bool {
	name = strings.ToLower(name)
	return name == "client_state" || strings.Contains(name, "token") || strings.Contains(name, "receipt") || strings.Contains(name, "ticket")
}

// CallbackObservation uses only the server-held envelope associated with an
// authenticated receipt. In particular the client cannot supply a class handle.
func CallbackObservation(fed core.FederationName, h core.FederateHandle, event *rtiv1.FederateEvent, success bool, exception string) (ServiceObservation, bool) {
	service := callbackServiceName(event)
	if service == "" {
		return ServiceObservation{}, false
	}
	m := event.ProtoReflect()
	field := m.WhichOneof(m.Descriptor().Oneofs().ByName("event"))
	var arguments []string
	if field != nil && field.Kind() == protoreflect.MessageKind {
		arguments = momArguments(m.Get(field).Message().Interface())
	}
	return ServiceObservation{Federation: fed, Federate: h, Service: service, Success: success, Exception: exception, SuppliedArguments: arguments}, true
}
func (o *MOMObserver) observationIdentity(ctx context.Context, method, service string, request, response any, serviceErr error) (core.FederationName, core.FederateHandle) {
	fed, h := momRPCIdentity(request)
	if method == rtiv1.FederationService_ResignFederation_FullMethodName {
		state, _ := ctx.Value(resignObservationKey{}).(*resignObservation)
		if state != nil && state.owner == o && !state.completed.Load() {
			md, _ := metadata.FromIncomingContext(ctx)
			if len(md.Get(session.MetadataKey)) != 0 {
				// A rejected signed request may name another member. Only the
				// completed lifecycle hook can bypass authenticated resolution.
				fed, h = "", 0
			}
		}
	}
	if service == "joinFederationExecution" && serviceErr == nil {
		if joined, ok := response.(*rtiv1.JoinFederationResponse); ok {
			h = core.FederateHandle(joined.GetFederateHandle())
		}
	}
	return fed, h
}
func (o *MOMObserver) recordServiceObservation(ctx context.Context, fed core.FederationName, h core.FederateHandle, service string, request, response any, serviceErr error) {
	inv := ServiceObservation{Federation: fed, Federate: h, Service: service, Success: serviceErr == nil}
	capture := o.opts.CaptureArguments == nil || o.opts.CaptureArguments(fed, h)
	if capture {
		inv.SuppliedArguments = momArguments(request)
	}
	if serviceErr != nil {
		inv.Exception = serviceErr.Error()
	} else if capture {
		inv.ReturnedArguments = momArguments(response)
	}
	if err := o.opts.Observe(ctx, inv); err != nil {
		o.failed(err)
	}
}
