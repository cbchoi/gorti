package federate

import (
	"context"
	"errors"
	"net"
	"reflect"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

func remainingServicesFederate(t *testing.T, handler grpc.UnaryServerInterceptor) *Federate {
	t.Helper()
	l := bufconn.Listen(1 << 20)
	s := grpc.NewServer(grpc.UnaryInterceptor(handler))
	rtiv1.RegisterOwnershipServiceServer(s, &rtiv1.UnimplementedOwnershipServiceServer{})
	rtiv1.RegisterMomServiceServer(s, &rtiv1.UnimplementedMomServiceServer{})
	rtiv1.RegisterObjectServiceServer(s, &rtiv1.UnimplementedObjectServiceServer{})
	rtiv1.RegisterDeclarationServiceServer(s, &rtiv1.UnimplementedDeclarationServiceServer{})
	rtiv1.RegisterSavepointServiceServer(s, &rtiv1.UnimplementedSavepointServiceServer{})
	rtiv1.RegisterSupportServiceServer(s, &rtiv1.UnimplementedSupportServiceServer{})
	rtiv1.RegisterFederationServiceServer(s, &rtiv1.UnimplementedFederationServiceServer{})
	rtiv1.RegisterTimeServiceServer(s, &rtiv1.UnimplementedTimeServiceServer{})
	rtiv1.RegisterDDMServiceServer(s, &rtiv1.UnimplementedDDMServiceServer{})
	rtiv1.RegisterOwnershipCallbackServiceServer(s, &rtiv1.UnimplementedOwnershipCallbackServiceServer{})
	go func() { _ = s.Serve(l) }()
	t.Cleanup(func() { s.Stop(); _ = l.Close() })
	cc, err := grpc.NewClient("passthrough:///remaining-services", grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return l.DialContext(ctx) }), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cc.Close() })
	f := &Federate{conn: WrapGRPCClientConn(cc), federationName: testFederationName, federateHandle: 3}
	f.restoredIdentity.Store(&federateIdentity{handle: 7, epoch: 1})
	return f
}

type ownershipBindings interface {
	UnconditionalAttributeOwnershipDivestiture(context.Context, uint64, []uint64) error
	NegotiatedAttributeOwnershipDivestiture(context.Context, uint64, []uint64, []byte) error
	AttributeOwnershipAcquisition(context.Context, uint64, []uint64, []byte) error
	AttributeOwnershipAcquisitionIfAvailable(context.Context, uint64, []uint64) error
	CancelNegotiatedAttributeOwnershipDivestiture(context.Context, uint64, []uint64) error
	CancelAttributeOwnershipAcquisition(context.Context, uint64, []uint64) error
	AttributeOwnershipDivestitureIfWanted(context.Context, uint64, []uint64) ([]uint64, error)
	ConfirmDivestiture(context.Context, uint64, []uint64, []byte) error
	AttributeOwnershipReleaseDenied(context.Context, uint64, []uint64) error
	QueryAttributeOwnership(context.Context, uint64, uint64) (uint64, bool, error)
	IsAttributeOwnedByFederate(context.Context, uint64, uint64) (bool, error)
}

func TestGoOwnershipBindingsPreserveAtomicResultsAndCaller(t *testing.T) {
	calls := 0
	f := remainingServicesFederate(t, func(_ context.Context, req any, _ *grpc.UnaryServerInfo, _ grpc.UnaryHandler) (any, error) {
		calls++
		identity, ok := req.(interface {
			GetFederationName() string
			GetFederateHandle() uint64
			GetObjectHandle() uint64
		})
		if !ok || identity.GetFederationName() != testFederationName || identity.GetFederateHandle() != 7 || identity.GetObjectHandle() != 80 {
			t.Errorf("caller/object mismatch: %v", req)
		}
		switch r := req.(type) {
		case *rtiv1.NegotiatedDivestRequest:
			if !r.TwoPhase || string(r.Tag) != testUserTag {
				t.Errorf("negotiated contract: %v", r)
			}
		case *rtiv1.ConfirmDivestitureRequest:
			if string(r.Tag) != testUserTag {
				t.Error("confirm tag lost")
			}
		case *rtiv1.DivestIfWantedRequest:
			return &rtiv1.DivestIfWantedResponse{DivestedAttributeHandles: []uint64{2}}, nil
		case *rtiv1.QueryOwnershipRequest:
			return &rtiv1.QueryOwnershipResponse{OwnerFederateHandle: 9, Owned: true}, nil
		case *rtiv1.IsOwnedRequest:
			return &rtiv1.IsOwnedResponse{Owned: false}, nil
		}
		return &rtiv1.Empty{}, nil
	})
	api, ok := any(f).(ownershipBindings)
	if !ok {
		t.Fatal("Go SDK omits existing ownership services")
	}
	f.objectClasses = map[uint64]uint64{80: 19}
	f.ownedObjectAttributes = map[uint64]map[uint64]struct{}{80: {2: {}, 7: {}}}
	ctx := context.Background()
	divested, err := api.AttributeOwnershipDivestitureIfWanted(ctx, 80, []uint64{2, 7})
	if err != nil || !reflect.DeepEqual(divested, []uint64{2}) {
		t.Fatalf("atomic result: %v %v", divested, err)
	}
	if _, ok := f.ownedObjectAttributes[80][7]; !ok {
		t.Fatal("removed untransferred ownership")
	}
	if _, ok := f.ownedObjectAttributes[80][2]; ok {
		t.Fatal("retained divested ownership")
	}
	for _, call := range []func() error{
		func() error { return api.UnconditionalAttributeOwnershipDivestiture(ctx, 80, []uint64{7}) },
		func() error {
			return api.NegotiatedAttributeOwnershipDivestiture(ctx, 80, []uint64{2}, []byte(testUserTag))
		},
		func() error { return api.AttributeOwnershipAcquisition(ctx, 80, []uint64{2}, []byte(testUserTag)) },
		func() error { return api.AttributeOwnershipAcquisitionIfAvailable(ctx, 80, []uint64{2}) },
		func() error { return api.CancelNegotiatedAttributeOwnershipDivestiture(ctx, 80, []uint64{2}) },
		func() error { return api.CancelAttributeOwnershipAcquisition(ctx, 80, []uint64{2}) },
		func() error { return api.ConfirmDivestiture(ctx, 80, []uint64{2}, []byte(testUserTag)) },
		func() error { return api.AttributeOwnershipReleaseDenied(ctx, 80, []uint64{2}) },
	} {
		if err := call(); err != nil {
			t.Fatal(err)
		}
	}
	owner, owned, err := api.QueryAttributeOwnership(ctx, 80, 2)
	if err != nil || !owned || owner != 9 {
		t.Fatalf("query %d %v %v", owner, owned, err)
	}
	if owned, err := api.IsAttributeOwnedByFederate(ctx, 80, 2); err != nil || owned {
		t.Fatalf("is owned %v %v", owned, err)
	}
	if calls != 11 {
		t.Fatalf("made %d RPCs; atomic divest must not use follow-up queries", calls)
	}
}

func TestGoMOMBindingsPreservePresenceAndTargets(t *testing.T) {
	f := remainingServicesFederate(t, func(_ context.Context, req any, _ *grpc.UnaryServerInfo, _ grpc.UnaryHandler) (any, error) {
		switch r := req.(type) {
		case *rtiv1.QueryFederationAttributesRequest:
			return &rtiv1.QueryFederationAttributesResponse{FederationName: testFederationName, FederateHandles: []uint64{7, 9}, FomModuleNames: []string{"model"}}, nil
		case *rtiv1.QueryFederateAttributesRequest:
			if r.FederateHandle != 9 {
				t.Errorf("MOM target overwritten with caller: %d", r.FederateHandle)
			}
			return &rtiv1.QueryFederateAttributesResponse{Found: true, FederateHandle: 9, LogicalTime: &rtiv1.LogicalTime{Value: 0}, UpdatesSent: 5}, nil
		case *rtiv1.EnumerateMomInstancesRequest:
			return &rtiv1.EnumerateMomInstancesResponse{Instances: []*rtiv1.MomInstance{{ClassName: "HLAfederation", InstanceName: testFederationName}}}, nil
		}
		t.Errorf("unexpected request %T", req)
		return &rtiv1.Empty{}, nil
	})
	api, ok := any(f).(interface {
		QueryFederationAttributes(context.Context) (MOMFederationAttributes, error)
		QueryFederateAttributes(context.Context, uint64) (MOMFederateAttributes, error)
		EnumerateMOMInstances(context.Context) ([]MOMInstance, error)
	})
	if !ok {
		t.Fatal("Go SDK omits existing MOM query services")
	}
	ctx := context.Background()
	fed, err := api.QueryFederationAttributes(ctx)
	if err != nil || len(fed.FederateHandles) != 2 || fed.FOMModuleNames[0] != "model" {
		t.Fatalf("federation %+v %v", fed, err)
	}
	member, err := api.QueryFederateAttributes(ctx, 9)
	if err != nil || !member.Found || member.LogicalTime == nil || *member.LogicalTime != 0 || member.Lookahead != nil || member.UpdatesSent != 5 {
		t.Fatalf("member %+v %v", member, err)
	}
	instances, err := api.EnumerateMOMInstances(ctx)
	if err != nil || len(instances) != 1 || instances[0].FederateHandle != 0 {
		t.Fatalf("instances %+v %v", instances, err)
	}
}

func TestGoAtomicNameReleaseAndExplicitRestoreGeneration(t *testing.T) {
	count := 0
	f := remainingServicesFederate(t, func(_ context.Context, req any, info *grpc.UnaryServerInfo, _ grpc.UnaryHandler) (any, error) {
		count++
		switch r := req.(type) {
		case *rtiv1.ReleaseMultipleObjectInstanceNamesRequest:
			if r.FederateHandle != 7 || !reflect.DeepEqual(r.ObjectNames, []string{"one", "two"}) {
				t.Errorf("release %v", r)
			}
		case *rtiv1.RequestFederationRestoreRequest:
			if r.FederateHandle != 7 || r.Label != "saved" || r.SavedFederationGeneration == nil || *r.SavedFederationGeneration != 0 {
				t.Errorf("explicit source generation lost: %v", r)
			}
		default:
			t.Errorf("unexpected RPC %s", info.FullMethod)
		}
		return &rtiv1.Empty{}, nil
	})
	api, ok := any(f).(interface {
		ReleaseMultipleObjectInstanceNames(context.Context, []string) error
		RequestFederationRestoreFromGeneration(context.Context, string, uint64) error
	})
	if !ok {
		t.Fatal("Go SDK omits atomic multiple release or explicit restore source generation")
	}
	if err := api.ReleaseMultipleObjectInstanceNames(context.Background(), []string{"one", "two"}); err != nil {
		t.Fatal(err)
	}
	if err := api.RequestFederationRestoreFromGeneration(context.Background(), "saved", 0); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("made %d RPCs; multiple release must not loop", count)
	}
}

func TestGoCallbackReportingIsExplicitAndNeverReplays(t *testing.T) {
	reports := 0
	entries := 0
	f := remainingServicesFederate(t, func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, _ grpc.UnaryHandler) (any, error) {
		r := req.(*rtiv1.ReportCallbackInvocationRequest)
		md, _ := metadata.FromIncomingContext(ctx)
		if r.InvocationEntry {
			entries++
			if r.FederateHandle != 7 || string(r.CallbackReceipt) != testCallbackReceipt || !r.Success || r.Exception != "" || len(md.Get("gorti-session-token-bin")) != 1 {
				t.Errorf("callback entry mismatch: %v metadata=%v", r, md)
			}
			return &rtiv1.Empty{}, nil
		}
		reports++
		if r.FederateHandle != 7 || string(r.CallbackReceipt) != testCallbackReceipt || r.Success || r.Exception != testApplicationFailure || len(md.Get("gorti-session-token-bin")) != 1 {
			t.Errorf("callback observation mismatch: %v metadata=%v", r, md)
		}
		return nil, status.Error(codes.Unavailable, "observation unavailable")
	})
	f.sessionToken = []byte("token")
	e := f.translate(&rtiv1.FederateEvent{CallbackReceipt: []byte(testCallbackReceipt), Event: &rtiv1.FederateEvent_Grant{Grant: &rtiv1.TimeAdvanceGrant{LogicalTime: 1}}})
	f.callbackEntrySupported = true
	if reports != 0 {
		t.Fatal("reported callback on translation")
	}
	invocations := 0
	applicationErr := errors.New(testApplicationFailure)
	callbackErr, reportErr := f.DispatchEvent(context.Background(), e, func(Event) error { invocations++; return applicationErr })
	if !errors.Is(callbackErr, applicationErr) || reportErr == nil || reports != 1 || invocations != 1 || entries != 1 {
		t.Fatalf("dispatch callback=%v report=%v reports=%d calls=%d", callbackErr, reportErr, reports, invocations)
	}
	if err := f.ReportCallbackInvocation(context.Background(), TimeAdvanceGrant{}, true, ""); !errors.Is(err, ErrCallbackReceiptUnavailable) {
		t.Fatalf("missing receipt: %v", err)
	}
	if reports != 1 {
		t.Fatal("missing receipt fabricated report")
	}
}

func TestGoSecureDirectiveAndNormalizationBindings(t *testing.T) {
	f := remainingServicesFederate(t, func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, _ grpc.UnaryHandler) (any, error) {
		md, _ := metadata.FromIncomingContext(ctx)
		if len(md.Get("gorti-session-token-bin")) != 1 {
			t.Error("session token missing")
		}
		switch r := req.(type) {
		case *rtiv1.SetAutomaticResignDirectiveRequest:
			if r.FederateHandle != 7 || r.Action != rtiv1.ResignAction_RESIGN_ACTION_NO_ACTION {
				t.Errorf("set directive %v", r)
			}
			return &rtiv1.Empty{}, nil
		case *rtiv1.GetAutomaticResignDirectiveRequest:
			return &rtiv1.GetAutomaticResignDirectiveResponse{Action: rtiv1.ResignAction_RESIGN_ACTION_NO_ACTION}, nil
		case *rtiv1.NormalizeFederateHandleRequest:
			if r.TargetHandle != ^uint64(0)-1 {
				t.Error("normalization truncated handle")
			}
			return &rtiv1.NormalizeFederateHandleResponse{Value: r.TargetHandle}, nil
		case *rtiv1.NormalizeServiceGroupRequest:
			return &rtiv1.NormalizeServiceGroupResponse{Value: uint64(r.ServiceGroup)}, nil
		}
		t.Errorf("unexpected request %T", req)
		return &rtiv1.Empty{}, nil
	})
	f.sessionToken = []byte("session")
	ctx := context.Background()
	if err := f.SetAutomaticResignDirective(ctx, ResignActionNoAction); err != nil {
		t.Fatal(err)
	}
	if a, err := f.GetAutomaticResignDirective(ctx); err != nil || a != ResignActionNoAction {
		t.Fatalf("directive %d %v", a, err)
	}
	if h, err := f.NormalizeFederateHandle(ctx, ^uint64(0)-1); err != nil || h != ^uint64(0)-1 {
		t.Fatalf("normalization %d %v", h, err)
	}
	if g, err := f.NormalizeServiceGroup(ctx, 6); err != nil || g != 6 {
		t.Fatalf("group %d %v", g, err)
	}
}

func TestGoRemoteSupportLookupsPreserveWireResults(t *testing.T) {
	f := remainingServicesFederate(t, func(_ context.Context, req any, _ *grpc.UnaryServerInfo, _ grpc.UnaryHandler) (any, error) {
		switch r := req.(type) {
		case *rtiv1.GetOrderTypeRequest:
			if r.OrderName != "TimeStamp" {
				t.Errorf("order request %v", r)
			}
			return &rtiv1.GetOrderTypeResponse{OrderType: 2}, nil
		case *rtiv1.GetDimensionUpperBoundRequest:
			return &rtiv1.GetDimensionUpperBoundResponse{UpperBound: ^uint64(0)}, nil
		case *rtiv1.GetAttributeHandleRequest:
			if r.ClassHandle != 19 || r.AttributeName != "Inherited" {
				t.Errorf("attribute request %v", r)
			}
			return &rtiv1.GetAttributeHandleResponse{AttributeHandle: 7}, nil
		}
		t.Errorf("unexpected request %T", req)
		return &rtiv1.Empty{}, nil
	})
	api, ok := any(f).(interface {
		GetOrderType(context.Context, string) (OrderType, error)
		GetDimensionUpperBound(context.Context, uint64) (uint64, error)
		GetAttributeHandle(context.Context, uint64, string) (uint64, error)
	})
	if !ok {
		t.Fatal("Go SDK omits remote support lookup bindings")
	}
	ctx := context.Background()
	if order, err := api.GetOrderType(ctx, "TimeStamp"); err != nil || order != OrderTimestamp {
		t.Fatalf("order %v %v", order, err)
	}
	if upper, err := api.GetDimensionUpperBound(ctx, 9); err != nil || upper != ^uint64(0) {
		t.Fatalf("dimension bound %d %v", upper, err)
	}
	if attr, err := api.GetAttributeHandle(ctx, 19, "Inherited"); err != nil || attr != 7 {
		t.Fatalf("attribute %d %v", attr, err)
	}
}

func TestGoMOMRawAttributesPreserveUnavailableAndCopies(t *testing.T) {
	f := remainingServicesFederate(t, func(_ context.Context, req any, _ *grpc.UnaryServerInfo, _ grpc.UnaryHandler) (any, error) {
		switch req.(type) {
		case *rtiv1.QueryFederateAttributesRequest:
			return &rtiv1.QueryFederateAttributesResponse{Found: true, AttributeValues: map[string][]byte{"HLAreflectionsReceived": {0, 0, 0, 2}}, UnavailableAttributes: map[string]string{"HLAinteractionsReceived": "callback receipts outstanding"}}, nil
		case *rtiv1.QueryFederationAttributesRequest:
			return &rtiv1.QueryFederationAttributesResponse{AttributeValues: map[string][]byte{"HLAfederationName": {1, 2}}, UnavailableAttributes: map[string]string{"HLAlastSaveTime": "no completed save"}}, nil
		}
		return &rtiv1.Empty{}, nil
	})
	member, err := f.QueryFederateAttributes(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(member.AttributeValues["HLAreflectionsReceived"], []byte{0, 0, 0, 2}) || member.UnavailableAttributes["HLAinteractionsReceived"] == "" {
		t.Fatalf("raw measurement lost: %+v", member)
	}
	if _, present := member.AttributeValues["HLAinteractionsReceived"]; present {
		t.Fatal("unavailable measurement fabricated")
	}
	fed, err := f.QueryFederationAttributes(context.Background())
	if err != nil || len(fed.AttributeValues) != 1 || len(fed.UnavailableAttributes) != 1 {
		t.Fatalf("federation maps: %+v %v", fed, err)
	}
	input := map[string][]byte{"value": {1, 2}}
	cloned := cloneMOMAttributeValues(input)
	input["value"][0] = 9
	if cloned["value"][0] != 1 {
		t.Fatal("MOM bytes alias source")
	}
}

func TestGoRemainingDeclarationAndTransportationBindings(t *testing.T) {
	calls := 0
	f := remainingServicesFederate(t, func(_ context.Context, req any, _ *grpc.UnaryServerInfo, _ grpc.UnaryHandler) (any, error) {
		calls++
		switch r := req.(type) {
		case *rtiv1.QueryAttributeTransportationTypeRequest:
			if r.ObjectHandle != 80 || r.AttributeHandle != 7 {
				t.Errorf("queryattribute %v", r)
			}
			return &rtiv1.TransportationTypeResponse{TransportType: rtiv1.TransportationType_TRANSPORTATION_TYPE_RELIABLE}, nil
		case *rtiv1.QueryInteractionTransportationTypeRequest:
			if r.TargetFederateHandle != 9 || r.FederateHandle != 7 {
				t.Errorf("query target/caller %v", r)
			}
			return &rtiv1.TransportationTypeResponse{TransportType: rtiv1.TransportationType_TRANSPORTATION_TYPE_BEST_EFFORT}, nil
		}
		return &rtiv1.Empty{}, nil
	})
	f.handles, _ = tablesFromFOMView(authoritativeView(1))
	f.rememberPublishedObjectAttributes(19, []uint64{2, 7})
	f.rememberPublishedInteraction(27)
	ctx := context.Background()
	for _, call := range []func() error{
		func() error { return f.UnpublishObjectClassAttributes(ctx, "Z", []string{"Own"}) },
		func() error { return f.UnsubscribeObjectClassAttributes(ctx, "Z", []string{"Inherited"}) },
		func() error { return f.UnpublishInteractionClass(ctx, "Event") },
		func() error { return f.UnsubscribeInteractionClass(ctx, "Event") },
		func() error {
			return f.RequestAttributeTransportationTypeChange(ctx, 80, []uint64{7}, TransportType(1))
		},
		func() error { return f.RequestInteractionTransportationTypeChange(ctx, 27, TransportType(2)) },
		func() error { return f.ReserveMultipleObjectInstanceNames(ctx, []string{"one", "two"}) },
		func() error { return f.ReleaseObjectInstanceName(ctx, "one") },
	} {
		if err := call(); err != nil {
			t.Fatal(err)
		}
	}
	if _, ok := f.publishedObjectAttributes[19][2]; ok {
		t.Fatal("unpublished attribute remains locally published")
	}
	if _, ok := f.publishedObjectAttributes[19][7]; !ok {
		t.Fatal("unpublish removed another attribute")
	}
	if _, ok := f.publishedInteractions[27]; ok {
		t.Fatal("unpublished interaction remains locally published")
	}
	if value, err := f.QueryAttributeTransportationType(ctx, 80, 7); err != nil || value != 1 {
		t.Fatalf("attribute transport %v %v", value, err)
	}
	if value, err := f.QueryInteractionTransportationType(ctx, 9, 27); err != nil || value != 2 {
		t.Fatalf("interaction transport %v %v", value, err)
	}
	if calls != 10 {
		t.Fatalf("RPC count %d", calls)
	}
}

const (
	testApplicationFailure = "application failure"
	testVehicleClass       = "Vehicle"
	testCallbackReceipt    = "receipt"
	testUserTag            = "tag"
	testDisableFailed      = "disable-failed"
	testRetainedMetadata   = "retained"
	testFederationName     = "fed"
	testNoDesignator       = "no-designator"
)
