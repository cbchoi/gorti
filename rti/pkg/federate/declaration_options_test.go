package federate

import (
	"context"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
)

type declarationOptionsWire struct {
	grpc.ClientConnInterface
	requests []proto.Message
}

func (w *declarationOptionsWire) Invoke(_ context.Context, _ string, args, _ any, _ ...grpc.CallOption) error {
	w.requests = append(w.requests, proto.Clone(args.(proto.Message)))
	return nil
}

func TestDeclarationSDKExplicitOptionsWire(t *testing.T) {
	w := &declarationOptionsWire{}
	f := &Federate{conn: &Connection{decl: rtiv1.NewDeclarationServiceClient(w)}, federationName: "f", federateHandle: 3}
	api, ok := any(f).(interface {
		SubscribeInteractionClassWithOptions(context.Context, string, bool) error
	})
	if !ok {
		t.Fatal("SDK lacks explicit active option API")
	}
	// Unknown FOM class must fail before invoking a request.
	if err := api.SubscribeInteractionClassWithOptions(context.Background(), "missing", false); err == nil {
		t.Fatal("unknown class accepted")
	}
	if len(w.requests) != 0 {
		t.Fatal("invalid class sent request")
	}
	f.handles = &fomTables{objectByName: map[string]uint64{"Object": 10}, objectByHandle: map[uint64]string{10: "Object"}, attrByName: map[string]map[string]uint64{"Object": {"value": 1}}, interactionByName: map[string]uint64{"Interaction": 20}}
	if err := f.SubscribeObjectClassAttributesWithOptions(context.Background(), "Object", []string{"value"}, SubscriptionOptions{Active: false, UpdateRateDesignator: "slow"}); err != nil {
		t.Fatal(err)
	}
	request := w.requests[0].(*rtiv1.SubObjAttrsRequest)
	if request.Active == nil || request.GetActive() || request.UpdateRateDesignator != "slow" {
		t.Fatalf("SDK dropped object options: %v", request)
	}
	if err := api.SubscribeInteractionClassWithOptions(context.Background(), "Interaction", false); err != nil {
		t.Fatal(err)
	}
	interaction := w.requests[1].(*rtiv1.SubInterRequest)
	if interaction.Active == nil || interaction.GetActive() {
		t.Fatalf("SDK dropped passive interaction: %v", interaction)
	}
	if err := f.SetAdvisorySwitch(context.Background(), AdvisoryAttributeScope, false); err != nil {
		t.Fatal(err)
	}
	switchReq := w.requests[2].(*rtiv1.SetAdvisorySwitchRequest)
	if switchReq.SwitchKind != rtiv1.AdvisorySwitchKind_ADVISORY_SWITCH_KIND_ATTRIBUTE_SCOPE || switchReq.Enabled || switchReq.FederateHandle != 3 || switchReq.FederationName != "f" {
		t.Fatalf("switch request=%v", switchReq)
	}
}
