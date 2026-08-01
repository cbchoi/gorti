// M25 Phase B — SupportService spec tests.
//
// Verifies handle / name / dimension / order / transport lookups
// against a FOM with one object class (with two attributes), one
// interaction class (with a parameter), and one dimension. The
// SupportService is constructed via grpcsvc.NewSupportServiceForTest
// so the spec test does not need to drive the full Server compose.

package m25spec

import (
	"context"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/cbchoi/gorti/rti/internal/core"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
	grpcsvc "github.com/cbchoi/gorti/rti/internal/transport/grpc"
	"github.com/cbchoi/gorti/rti/pkg/fom/model"
)

const fedName = "support-test"

// fomHandle mirrors the rtid/foms.go fomHandle, copied here so the
// spec test does not depend on the cmd/rtid package. Identical
// behavior; production wiring uses the cmd/rtid one.
type fomHandle struct{ fom *model.FOM }

func (h *fomHandle) IsValid() bool { return h != nil && h.fom != nil }

func (h *fomHandle) LookupObjectClass(name string) (core.ObjectClassHandle, bool) {
	for i, oc := range h.fom.ObjectClasses() {
		if oc.Name == name {
			return core.ObjectClassHandle(i + 1), true
		}
	}
	return core.InvalidObjectClassHandle, false
}

func (h *fomHandle) LookupInteractionClass(name string) (core.InteractionClassHandle, bool) {
	for i, ic := range h.fom.InteractionClasses() {
		if ic.Name == name {
			return core.InteractionClassHandle(i + 1), true
		}
	}
	return core.InvalidInteractionClassHandle, false
}

func (h *fomHandle) LookupAttribute(cls core.ObjectClassHandle, name string) (core.AttributeHandle, bool) {
	classes := h.fom.ObjectClasses()
	idx := int(cls) - 1
	if idx < 0 || idx >= len(classes) {
		return core.InvalidAttributeHandle, false
	}
	for i, a := range classes[idx].Attributes {
		if a.Name == name {
			return core.AttributeHandle(i + 1), true
		}
	}
	return core.InvalidAttributeHandle, false
}

func (h *fomHandle) LookupParameter(cls core.InteractionClassHandle, name string) (core.ParameterHandle, bool) {
	classes := h.fom.InteractionClasses()
	idx := int(cls) - 1
	if idx < 0 || idx >= len(classes) {
		return core.InvalidParameterHandle, false
	}
	for i, p := range classes[idx].Parameters {
		if p.Name == name {
			return core.ParameterHandle(i + 1), true
		}
	}
	return core.InvalidParameterHandle, false
}

// FOMHandleNameLookup methods.
func (h *fomHandle) ObjectClassName(cls core.ObjectClassHandle) (string, bool) {
	classes := h.fom.ObjectClasses()
	idx := int(cls) - 1
	if idx < 0 || idx >= len(classes) {
		return "", false
	}
	return classes[idx].Name, true
}

func (h *fomHandle) InteractionClassName(cls core.InteractionClassHandle) (string, bool) {
	classes := h.fom.InteractionClasses()
	idx := int(cls) - 1
	if idx < 0 || idx >= len(classes) {
		return "", false
	}
	return classes[idx].Name, true
}

func (h *fomHandle) AttributeName(cls core.ObjectClassHandle, a core.AttributeHandle) (string, bool) {
	classes := h.fom.ObjectClasses()
	cidx := int(cls) - 1
	if cidx < 0 || cidx >= len(classes) {
		return "", false
	}
	aidx := int(a) - 1
	if aidx < 0 || aidx >= len(classes[cidx].Attributes) {
		return "", false
	}
	return classes[cidx].Attributes[aidx].Name, true
}

func (h *fomHandle) ParameterName(cls core.InteractionClassHandle, p core.ParameterHandle) (string, bool) {
	classes := h.fom.InteractionClasses()
	cidx := int(cls) - 1
	if cidx < 0 || cidx >= len(classes) {
		return "", false
	}
	pidx := int(p) - 1
	if pidx < 0 || pidx >= len(classes[cidx].Parameters) {
		return "", false
	}
	return classes[cidx].Parameters[pidx].Name, true
}

func (h *fomHandle) LookupDimension(name string) (core.DimensionHandle, bool) {
	for i, d := range h.fom.Dimensions() {
		if d.Name == name {
			return core.DimensionHandle(i + 1), true
		}
	}
	return core.InvalidDimensionHandle, false
}

func (h *fomHandle) DimensionName(dh core.DimensionHandle) (string, bool) {
	dims := h.fom.Dimensions()
	idx := int(dh) - 1
	if idx < 0 || idx >= len(dims) {
		return "", false
	}
	return dims[idx].Name, true
}

func (h *fomHandle) DimensionUpperBound(dh core.DimensionHandle) (uint64, bool) {
	dims := h.fom.Dimensions()
	idx := int(dh) - 1
	if idx < 0 || idx >= len(dims) {
		return 0, false
	}
	return dims[idx].UpperBound, true
}

// stubRepo serves a single pre-loaded handle.
type stubRepo struct{ h core.FOMHandle }

func (r *stubRepo) Load(_ context.Context, _ []core.FOMModule) (core.FOMHandle, error) {
	return r.h, nil
}
func (r *stubRepo) Get(_ context.Context, fed core.FederationName) (core.FOMHandle, error) {
	if fed != fedName {
		return nil, core.ErrFederationNotFound
	}
	return r.h, nil
}

func newSupport(t *testing.T) rtiv1.SupportServiceServer {
	t.Helper()
	fm := model.NewFOMWithDimensions(
		[]model.ObjectClass{{
			Name: "Vehicle",
			Attributes: []model.Attribute{
				{Name: "Position", DataType: "HLAfloat64BE"},
				{Name: "Velocity", DataType: "HLAfloat64BE"},
			},
		}},
		[]model.InteractionClass{{
			Name: "Honk",
			Parameters: []model.Parameter{
				{Name: "Volume", DataType: "HLAinteger32BE"},
			},
		}},
		nil,
		[]model.Dimension{{Name: "X", UpperBound: 1000}},
	)
	return grpcsvc.NewSupportServiceForTest(&stubRepo{h: &fomHandle{fom: fm}})
}

func wire1() rtiv1.WireVersion { return rtiv1.WireVersion_WIRE_VERSION_V1 }

func TestSpec_M25_SupportService_ObjectClassHandleRoundTrip(t *testing.T) {
	s := newSupport(t)
	ctx := context.Background()
	h, err := s.GetObjectClassHandle(ctx, &rtiv1.GetObjectClassHandleRequest{
		WireVersion: wire1(), FederationName: fedName, ClassName: "Vehicle",
	})
	if err != nil {
		t.Fatalf("GetObjectClassHandle: %v", err)
	}
	if h.GetClassHandle() != 1 {
		t.Errorf("ClassHandle = %d; want 1", h.GetClassHandle())
	}
	n, err := s.GetObjectClassName(ctx, &rtiv1.GetObjectClassNameRequest{
		WireVersion: wire1(), FederationName: fedName, ClassHandle: h.GetClassHandle(),
	})
	if err != nil {
		t.Fatalf("GetObjectClassName: %v", err)
	}
	if n.GetClassName() != "Vehicle" {
		t.Errorf("ClassName = %q; want %q", n.GetClassName(), "Vehicle")
	}
}

func TestSpec_M25_SupportService_AttributeHandleRoundTrip(t *testing.T) {
	s := newSupport(t)
	ctx := context.Background()
	h, err := s.GetAttributeHandle(ctx, &rtiv1.GetAttributeHandleRequest{
		WireVersion: wire1(), FederationName: fedName, ClassHandle: 1, AttributeName: "Velocity",
	})
	if err != nil {
		t.Fatalf("GetAttributeHandle: %v", err)
	}
	if h.GetAttributeHandle() != 2 {
		t.Errorf("Velocity AttributeHandle = %d; want 2", h.GetAttributeHandle())
	}
	n, err := s.GetAttributeName(ctx, &rtiv1.GetAttributeNameRequest{
		WireVersion: wire1(), FederationName: fedName, ClassHandle: 1, AttributeHandle: 2,
	})
	if err != nil {
		t.Fatalf("GetAttributeName: %v", err)
	}
	if n.GetAttributeName() != "Velocity" {
		t.Errorf("AttributeName = %q; want Velocity", n.GetAttributeName())
	}
}

func TestSpec_M25_SupportService_InteractionAndParameterRoundTrip(t *testing.T) {
	s := newSupport(t)
	ctx := context.Background()
	ih, err := s.GetInteractionClassHandle(ctx, &rtiv1.GetInteractionClassHandleRequest{
		WireVersion: wire1(), FederationName: fedName, ClassName: "Honk",
	})
	if err != nil {
		t.Fatalf("GetInteractionClassHandle: %v", err)
	}
	if ih.GetClassHandle() != 1 {
		t.Errorf("Honk class handle = %d; want 1", ih.GetClassHandle())
	}
	ph, err := s.GetParameterHandle(ctx, &rtiv1.GetParameterHandleRequest{
		WireVersion: wire1(), FederationName: fedName, ClassHandle: 1, ParameterName: "Volume",
	})
	if err != nil {
		t.Fatalf("GetParameterHandle: %v", err)
	}
	if ph.GetParameterHandle() != 1 {
		t.Errorf("Volume parameter handle = %d; want 1", ph.GetParameterHandle())
	}
	pn, err := s.GetParameterName(ctx, &rtiv1.GetParameterNameRequest{
		WireVersion: wire1(), FederationName: fedName, ClassHandle: 1, ParameterHandle: 1,
	})
	if err != nil {
		t.Fatalf("GetParameterName: %v", err)
	}
	if pn.GetParameterName() != "Volume" {
		t.Errorf("ParameterName = %q; want Volume", pn.GetParameterName())
	}
}

func TestSpec_M25_SupportService_DimensionLookup(t *testing.T) {
	s := newSupport(t)
	ctx := context.Background()
	dh, err := s.GetDimensionHandle(ctx, &rtiv1.GetDimensionHandleRequest{
		WireVersion: wire1(), FederationName: fedName, DimensionName: "X",
	})
	if err != nil {
		t.Fatalf("GetDimensionHandle: %v", err)
	}
	if dh.GetDimensionHandle() != 1 {
		t.Errorf("X dimension handle = %d; want 1", dh.GetDimensionHandle())
	}
	ub, err := s.GetDimensionUpperBound(ctx, &rtiv1.GetDimensionUpperBoundRequest{
		WireVersion: wire1(), FederationName: fedName, DimensionHandle: 1,
	})
	if err != nil {
		t.Fatalf("GetDimensionUpperBound: %v", err)
	}
	if ub.GetUpperBound() != 1000 {
		t.Errorf("upper bound = %d; want 1000", ub.GetUpperBound())
	}
}

func TestSpec_M25_SupportService_OrderEnum(t *testing.T) {
	s := newSupport(t)
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		typ  uint32
	}{
		{"Receive", grpcsvc.OrderTypeReceive},
		{"TimeStamp", grpcsvc.OrderTypeTimeStamp},
	} {
		out, err := s.GetOrderType(ctx, &rtiv1.GetOrderTypeRequest{
			WireVersion: wire1(), OrderName: tc.name,
		})
		if err != nil {
			t.Fatalf("GetOrderType(%q): %v", tc.name, err)
		}
		if out.GetOrderType() != tc.typ {
			t.Errorf("GetOrderType(%q) = %d; want %d", tc.name, out.GetOrderType(), tc.typ)
		}
		back, err := s.GetOrderName(ctx, &rtiv1.GetOrderNameRequest{
			WireVersion: wire1(), OrderType: tc.typ,
		})
		if err != nil {
			t.Fatalf("GetOrderName(%d): %v", tc.typ, err)
		}
		if back.GetOrderName() != tc.name {
			t.Errorf("GetOrderName(%d) = %q; want %q", tc.typ, back.GetOrderName(), tc.name)
		}
	}
}

func TestSpec_M25_SupportService_TransportEnum(t *testing.T) {
	s := newSupport(t)
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		typ  uint32
	}{
		{"HLAreliable", grpcsvc.TransportReliable},
		{"HLAbestEffort", grpcsvc.TransportBestEffort},
	} {
		out, err := s.GetTransportationType(ctx, &rtiv1.GetTransportationTypeRequest{
			WireVersion: wire1(), TransportationName: tc.name,
		})
		if err != nil {
			t.Fatalf("GetTransportationType(%q): %v", tc.name, err)
		}
		if out.GetTransportationType() != tc.typ {
			t.Errorf("GetTransportationType(%q) = %d; want %d", tc.name, out.GetTransportationType(), tc.typ)
		}
	}
}

func TestSpec_M25_SupportService_MissingNameReturnsNotFound(t *testing.T) {
	s := newSupport(t)
	ctx := context.Background()
	_, err := s.GetObjectClassHandle(ctx, &rtiv1.GetObjectClassHandleRequest{
		WireVersion: wire1(), FederationName: fedName, ClassName: "NoSuchClass",
	})
	if err == nil {
		t.Fatal("GetObjectClassHandle(NoSuchClass): nil error; want NotFound")
	}
	if st, _ := status.FromError(err); st.Code() != codes.NotFound {
		t.Errorf("error code = %v; want NotFound (got %v)", st.Code(), err)
	}
}

func TestSpec_M25_SupportService_UnknownFederationReturnsNotFound(t *testing.T) {
	s := newSupport(t)
	ctx := context.Background()
	_, err := s.GetObjectClassHandle(ctx, &rtiv1.GetObjectClassHandleRequest{
		WireVersion: wire1(), FederationName: "not-a-federation", ClassName: "Vehicle",
	})
	if err == nil {
		t.Fatal("unknown federation: nil error; want NotFound")
	}
	if st, _ := status.FromError(err); st.Code() != codes.NotFound {
		t.Errorf("error code = %v; want NotFound", st.Code())
	}
}

func TestSpec_M25_SupportService_WireVersionMismatchRejected(t *testing.T) {
	s := newSupport(t)
	ctx := context.Background()
	_, err := s.GetObjectClassHandle(ctx, &rtiv1.GetObjectClassHandleRequest{
		WireVersion: rtiv1.WireVersion_WIRE_VERSION_UNSPECIFIED, FederationName: fedName, ClassName: "Vehicle",
	})
	if err == nil {
		t.Fatal("wire version mismatch: nil error; want FailedPrecondition")
	}
	if st, _ := status.FromError(err); st.Code() != codes.FailedPrecondition {
		t.Errorf("error code = %v; want FailedPrecondition", st.Code())
	}
}
