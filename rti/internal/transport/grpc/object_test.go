package grpc

import (
	"context"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// fedXName is reused across object handler tests; pulled out to a const
// to satisfy goconst.
const fedXName = "fedX"

// stubObjectRegistry records calls and returns canned answers, so handler
// tests assert request/response translation without a real registry.
type stubObjectRegistry struct {
	regHandle core.ObjectHandle
	regName   string
	regErr    error
	regCalls  []regCall

	updErr   error
	updCalls []updCall

	sendErr   error
	sendCalls []sendCall
}

type regCall struct {
	Federation core.FederationName
	Producer   core.FederateHandle
	Class      core.ObjectClassHandle
	Name       string
}

type updCall struct {
	Federation core.FederationName
	Producer   core.FederateHandle
	Object     core.ObjectHandle
	Attrs      map[core.AttributeHandle][]byte
	TS         *core.LogicalTime
}

type sendCall struct {
	Federation core.FederationName
	Producer   core.FederateHandle
	Class      core.InteractionClassHandle
	Params     map[core.ParameterHandle][]byte
	TS         *core.LogicalTime
}

func (s *stubObjectRegistry) Register(_ context.Context, fed core.FederationName, producer core.FederateHandle, cls core.ObjectClassHandle, name string) (core.ObjectHandle, string, error) {
	s.regCalls = append(s.regCalls, regCall{fed, producer, cls, name})
	if s.regErr != nil {
		return 0, "", s.regErr
	}
	return s.regHandle, s.regName, nil
}

func (s *stubObjectRegistry) UpdateAttributes(_ context.Context, fed core.FederationName, producer core.FederateHandle, obj core.ObjectHandle, attrs map[core.AttributeHandle][]byte, ts *core.LogicalTime) error {
	s.updCalls = append(s.updCalls, updCall{fed, producer, obj, attrs, ts})
	return s.updErr
}

func (s *stubObjectRegistry) SendInteraction(_ context.Context, fed core.FederationName, producer core.FederateHandle, cls core.InteractionClassHandle, params map[core.ParameterHandle][]byte, ts *core.LogicalTime) error {
	s.sendCalls = append(s.sendCalls, sendCall{fed, producer, cls, params, ts})
	return s.sendErr
}
func (s *stubObjectRegistry) Delete(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.ObjectHandle, _ *core.LogicalTime, _ []byte) error {
	return nil
}
func (s *stubObjectRegistry) LocalDelete(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.ObjectHandle) error {
	return nil
}
func (s *stubObjectRegistry) RequestAttributeValueUpdate(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.ObjectHandle, _ []core.AttributeHandle, _ []byte) error {
	return nil
}
func (s *stubObjectRegistry) RequestClassAttributeValueUpdate(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.ObjectClassHandle, _ []core.AttributeHandle, _ []byte) error {
	return nil
}
func (s *stubObjectRegistry) Snapshot(_ core.FederationName) core.ObjectSnapshot {
	return core.ObjectSnapshot{}
}

// ----------------------------------------------------------------------------
// RegisterObjectInstance
// ----------------------------------------------------------------------------

func TestObjectService_RegisterObjectInstance_Happy_ReturnsHandleAndName(t *testing.T) {
	stub := &stubObjectRegistry{regHandle: 42, regName: "HLAobj_1_42"}
	svc := newObjectService(stub)

	resp, err := svc.RegisterObjectInstance(context.Background(), &rtiv1.RegisterObjectRequest{
		WireVersion:       rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName:    fedXName,
		FederateHandle:    7,
		ObjectClassHandle: 3,
		ObjectName:        "",
	})
	if err != nil {
		t.Fatalf("RegisterObjectInstance: unexpected error: %v", err)
	}
	if resp.GetObjectHandle() != 42 {
		t.Errorf("ObjectHandle = %d, want 42", resp.GetObjectHandle())
	}
	if resp.GetObjectName() != "HLAobj_1_42" {
		t.Errorf("ObjectName = %q, want %q", resp.GetObjectName(), "HLAobj_1_42")
	}
	if len(stub.regCalls) != 1 {
		t.Fatalf("Register calls = %d, want 1", len(stub.regCalls))
	}
	got := stub.regCalls[0]
	if got.Federation != fedXName || got.Producer != 7 || got.Class != 3 || got.Name != "" {
		t.Errorf("Register call = %+v, want fedX/7/3/empty", got)
	}
}

func TestObjectService_RegisterObjectInstance_ErrorMapping(t *testing.T) {
	cases := []struct {
		name string
		err  error
		code codes.Code
	}{
		{"federation not found", core.ErrFederationNotFound, codes.NotFound},
		{"federate not joined", core.ErrFederateNotJoined, codes.FailedPrecondition},
		{"class not published", core.ErrObjectClassNotPublished, codes.FailedPrecondition},
		{"other error", errors.New("boom"), codes.Internal},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &stubObjectRegistry{regErr: tc.err}
			svc := newObjectService(stub)
			_, err := svc.RegisterObjectInstance(context.Background(), &rtiv1.RegisterObjectRequest{
				WireVersion:       rtiv1.WireVersion_WIRE_VERSION_V1,
				FederationName:    "fed",
				FederateHandle:    1,
				ObjectClassHandle: 1,
			})
			if err == nil {
				t.Fatal("want error, got nil")
			}
			st, ok := status.FromError(err)
			if !ok {
				t.Fatalf("err is not a status: %v", err)
			}
			if st.Code() != tc.code {
				t.Errorf("code = %v, want %v", st.Code(), tc.code)
			}
		})
	}
}

func TestObjectService_RegisterObjectInstance_RejectsBadWireVersion(t *testing.T) {
	stub := &stubObjectRegistry{}
	svc := newObjectService(stub)
	_, err := svc.RegisterObjectInstance(context.Background(), &rtiv1.RegisterObjectRequest{
		WireVersion:    rtiv1.WireVersion_WIRE_VERSION_UNSPECIFIED,
		FederationName: "fed",
		FederateHandle: 1,
	})
	if err == nil {
		t.Fatal("want error for unspecified wire version, got nil")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.FailedPrecondition {
		t.Errorf("code = %v, want FailedPrecondition", st.Code())
	}
	if len(stub.regCalls) != 0 {
		t.Errorf("registry called %d times despite bad wire version", len(stub.regCalls))
	}
}

// ----------------------------------------------------------------------------
// UpdateAttributeValues
// ----------------------------------------------------------------------------

func TestObjectService_UpdateAttributeValues_Happy_RO(t *testing.T) {
	stub := &stubObjectRegistry{}
	svc := newObjectService(stub)
	_, err := svc.UpdateAttributeValues(context.Background(), &rtiv1.UpdateAttributeValuesRequest{
		WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName: fedXName,
		FederateHandle: 7,
		ObjectHandle:   11,
		Attributes: map[uint64][]byte{
			1: []byte("a"),
			2: []byte("bb"),
		},
	})
	if err != nil {
		t.Fatalf("UpdateAttributeValues: unexpected error: %v", err)
	}
	if len(stub.updCalls) != 1 {
		t.Fatalf("Update calls = %d, want 1", len(stub.updCalls))
	}
	got := stub.updCalls[0]
	if got.Federation != fedXName || got.Producer != 7 || got.Object != 11 {
		t.Errorf("Update call = %+v", got)
	}
	if got.TS != nil {
		t.Errorf("TS = %v, want nil for RO", got.TS)
	}
	if len(got.Attrs) != 2 || string(got.Attrs[core.AttributeHandle(1)]) != "a" || string(got.Attrs[core.AttributeHandle(2)]) != "bb" {
		t.Errorf("Attrs = %v", got.Attrs)
	}
}

func TestObjectService_UpdateAttributeValues_Happy_TSO(t *testing.T) {
	stub := &stubObjectRegistry{}
	svc := newObjectService(stub)
	ts := 3.5
	_, err := svc.UpdateAttributeValues(context.Background(), &rtiv1.UpdateAttributeValuesRequest{
		WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName: fedXName,
		FederateHandle: 7,
		ObjectHandle:   11,
		Attributes:     map[uint64][]byte{1: []byte("v")},
		LogicalTime:    &ts,
	})
	if err != nil {
		t.Fatalf("UpdateAttributeValues: unexpected error: %v", err)
	}
	if len(stub.updCalls) != 1 {
		t.Fatalf("Update calls = %d, want 1", len(stub.updCalls))
	}
	got := stub.updCalls[0]
	if got.TS == nil {
		t.Fatal("TS is nil; want TSO")
	}
	if float64(*got.TS) != 3.5 {
		t.Errorf("TS = %v, want 3.5", *got.TS)
	}
}

func TestObjectService_UpdateAttributeValues_ErrorMapping(t *testing.T) {
	cases := []struct {
		name string
		err  error
		code codes.Code
	}{
		{"federation not found", core.ErrFederationNotFound, codes.NotFound},
		{"federate not joined", core.ErrFederateNotJoined, codes.FailedPrecondition},
		{"object not found", core.ErrObjectNotFound, codes.NotFound},
		{"attribute not owned", core.ErrAttributeNotOwned, codes.PermissionDenied},
		{"other", errors.New("kaboom"), codes.Internal},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &stubObjectRegistry{updErr: tc.err}
			svc := newObjectService(stub)
			_, err := svc.UpdateAttributeValues(context.Background(), &rtiv1.UpdateAttributeValuesRequest{
				WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
				FederationName: "fed",
				FederateHandle: 1,
				ObjectHandle:   1,
			})
			if err == nil {
				t.Fatal("want error, got nil")
			}
			st, _ := status.FromError(err)
			if st.Code() != tc.code {
				t.Errorf("code = %v, want %v", st.Code(), tc.code)
			}
		})
	}
}

func TestObjectService_UpdateAttributeValues_RejectsBadWireVersion(t *testing.T) {
	stub := &stubObjectRegistry{}
	svc := newObjectService(stub)
	_, err := svc.UpdateAttributeValues(context.Background(), &rtiv1.UpdateAttributeValuesRequest{
		WireVersion: rtiv1.WireVersion_WIRE_VERSION_UNSPECIFIED,
	})
	if err == nil {
		t.Fatal("want error for unspecified wire version")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.FailedPrecondition {
		t.Errorf("code = %v, want FailedPrecondition", st.Code())
	}
}

// ----------------------------------------------------------------------------
// SendInteraction
// ----------------------------------------------------------------------------

func TestObjectService_SendInteraction_Happy_RO(t *testing.T) {
	stub := &stubObjectRegistry{}
	svc := newObjectService(stub)
	_, err := svc.SendInteraction(context.Background(), &rtiv1.SendInteractionRequest{
		WireVersion:            rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName:         fedXName,
		FederateHandle:         7,
		InteractionClassHandle: 5,
		Parameters: map[uint64][]byte{
			1: []byte("p1"),
		},
	})
	if err != nil {
		t.Fatalf("SendInteraction: unexpected error: %v", err)
	}
	if len(stub.sendCalls) != 1 {
		t.Fatalf("Send calls = %d, want 1", len(stub.sendCalls))
	}
	got := stub.sendCalls[0]
	if got.Federation != fedXName || got.Producer != 7 || got.Class != 5 {
		t.Errorf("Send call = %+v", got)
	}
	if got.TS != nil {
		t.Errorf("TS = %v, want nil", got.TS)
	}
	if string(got.Params[core.ParameterHandle(1)]) != "p1" {
		t.Errorf("Params = %v", got.Params)
	}
}

func TestObjectService_SendInteraction_Happy_TSO(t *testing.T) {
	stub := &stubObjectRegistry{}
	svc := newObjectService(stub)
	ts := 9.25
	_, err := svc.SendInteraction(context.Background(), &rtiv1.SendInteractionRequest{
		WireVersion:            rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName:         fedXName,
		FederateHandle:         7,
		InteractionClassHandle: 5,
		LogicalTime:            &ts,
	})
	if err != nil {
		t.Fatalf("SendInteraction: unexpected error: %v", err)
	}
	if stub.sendCalls[0].TS == nil || float64(*stub.sendCalls[0].TS) != 9.25 {
		t.Errorf("TS = %v, want 9.25", stub.sendCalls[0].TS)
	}
}

func TestObjectService_SendInteraction_ErrorMapping(t *testing.T) {
	cases := []struct {
		name string
		err  error
		code codes.Code
	}{
		{"federation not found", core.ErrFederationNotFound, codes.NotFound},
		{"federate not joined", core.ErrFederateNotJoined, codes.FailedPrecondition},
		{"interaction class not published", core.ErrInteractionClassNotPublished, codes.FailedPrecondition},
		{"other", errors.New("boom"), codes.Internal},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &stubObjectRegistry{sendErr: tc.err}
			svc := newObjectService(stub)
			_, err := svc.SendInteraction(context.Background(), &rtiv1.SendInteractionRequest{
				WireVersion:            rtiv1.WireVersion_WIRE_VERSION_V1,
				FederationName:         "fed",
				FederateHandle:         1,
				InteractionClassHandle: 1,
			})
			if err == nil {
				t.Fatal("want error, got nil")
			}
			st, _ := status.FromError(err)
			if st.Code() != tc.code {
				t.Errorf("code = %v, want %v", st.Code(), tc.code)
			}
		})
	}
}

func TestObjectService_SendInteraction_RejectsBadWireVersion(t *testing.T) {
	stub := &stubObjectRegistry{}
	svc := newObjectService(stub)
	_, err := svc.SendInteraction(context.Background(), &rtiv1.SendInteractionRequest{
		WireVersion: rtiv1.WireVersion_WIRE_VERSION_UNSPECIFIED,
	})
	if err == nil {
		t.Fatal("want error for unspecified wire version")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.FailedPrecondition {
		t.Errorf("code = %v, want FailedPrecondition", st.Code())
	}
}
