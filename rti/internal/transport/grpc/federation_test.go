package grpc

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/cbchoi/gorti/rti/internal/core"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
)

// fakeFedStore is a configurable in-memory core.FederationStore for the
// federation handler tests. Each method records its call args so happy-path
// tests can assert proto -> core translation.
type fakeFedStore struct {
	createErr   error
	createCalls []core.CreateFederationRequest

	destroyErr   error
	destroyCalls []core.FederationName

	joinErr    error
	joinHandle core.FederateHandle
	joinCalls  []core.JoinFederationRequest

	resignErr   error
	resignCalls []resignCall

	listResp []core.FederationSummary
	listErr  error
}

type resignCall struct {
	Fed    core.FederationName
	Handle core.FederateHandle
	Action core.ResignAction
}

func (f *fakeFedStore) CreateFederation(_ context.Context, r core.CreateFederationRequest) error {
	f.createCalls = append(f.createCalls, r)
	return f.createErr
}
func (f *fakeFedStore) DestroyFederation(_ context.Context, n core.FederationName) error {
	f.destroyCalls = append(f.destroyCalls, n)
	return f.destroyErr
}
func (f *fakeFedStore) JoinFederation(_ context.Context, r core.JoinFederationRequest) (core.FederateHandle, error) {
	f.joinCalls = append(f.joinCalls, r)
	return f.joinHandle, f.joinErr
}
func (f *fakeFedStore) ResignFederation(_ context.Context, fed core.FederationName, h core.FederateHandle, a core.ResignAction) error {
	f.resignCalls = append(f.resignCalls, resignCall{fed, h, a})
	return f.resignErr
}
func (f *fakeFedStore) List(_ context.Context) ([]core.FederationSummary, error) {
	return f.listResp, f.listErr
}

func newFedSvc(s core.FederationStore) *federationService {
	return newFederationService(s)
}

func wireV1() rtiv1.WireVersion { return rtiv1.WireVersion_WIRE_VERSION_V1 }

// ===========================================================================
// CreateFederation
// ===========================================================================

func TestCreateFederation_Happy_TranslatesProtoToCoreAndEchoesSeed(t *testing.T) {
	fake := &fakeFedStore{}
	svc := newFedSvc(fake)

	req := &rtiv1.CreateFederationRequest{
		WireVersion:    wireV1(),
		FederationName: "alpha",
		FomModules: []*rtiv1.FOMModule{
			{Path: "f.xml", Xml: []byte("<a/>")},
		},
		Mode:                rtiv1.Mode_MODE_VERBOSE,
		StallTimeoutSeconds: 1.5,
		Seed:                42,
	}
	resp, err := svc.CreateFederation(context.Background(), req)
	if err != nil {
		t.Fatalf("CreateFederation: %v", err)
	}
	if resp == nil {
		t.Fatal("nil response")
	}
	if resp.GetEffectiveSeed() != 42 {
		t.Errorf("EffectiveSeed=%d, want 42", resp.GetEffectiveSeed())
	}
	if len(fake.createCalls) != 1 {
		t.Fatalf("createCalls=%d, want 1", len(fake.createCalls))
	}
	got := fake.createCalls[0]
	if got.Name != core.FederationName("alpha") {
		t.Errorf("Name=%q, want alpha", got.Name)
	}
	if got.Mode != core.ModeVerbose {
		t.Errorf("Mode=%d, want ModeVerbose", got.Mode)
	}
	if got.StallTimeout != 1500*time.Millisecond {
		t.Errorf("StallTimeout=%v, want 1.5s", got.StallTimeout)
	}
	if got.Seed != 42 {
		t.Errorf("Seed=%d, want 42", got.Seed)
	}
	if len(got.FOMModules) != 1 || got.FOMModules[0].Path != "f.xml" || string(got.FOMModules[0].XML) != "<a/>" {
		t.Errorf("FOMModules=%v, want one module {f.xml, <a/>}", got.FOMModules)
	}
}

func TestCreateFederation_NilRequest_ReturnsInvalidArgument(t *testing.T) {
	svc := newFedSvc(&fakeFedStore{})
	_, err := svc.CreateFederation(context.Background(), nil)
	if status.Code(err) != codes.InvalidArgument {
		t.Errorf("code=%v, want InvalidArgument; err=%v", status.Code(err), err)
	}
}

func TestCreateFederation_BadWireVersion_ReturnsFailedPrecondition(t *testing.T) {
	svc := newFedSvc(&fakeFedStore{})
	_, err := svc.CreateFederation(context.Background(), &rtiv1.CreateFederationRequest{
		WireVersion:    rtiv1.WireVersion_WIRE_VERSION_UNSPECIFIED,
		FederationName: "alpha",
	})
	if status.Code(err) != codes.FailedPrecondition {
		t.Errorf("code=%v, want FailedPrecondition; err=%v", status.Code(err), err)
	}
}

func TestCreateFederation_AlreadyExists_MapsToAlreadyExists(t *testing.T) {
	svc := newFedSvc(&fakeFedStore{createErr: core.ErrFederationAlreadyExists})
	_, err := svc.CreateFederation(context.Background(), &rtiv1.CreateFederationRequest{
		WireVersion:    wireV1(),
		FederationName: "alpha",
	})
	if status.Code(err) != codes.AlreadyExists {
		t.Errorf("code=%v, want AlreadyExists; err=%v", status.Code(err), err)
	}
}

func TestCreateFederation_InvalidName_MapsToInvalidArgument(t *testing.T) {
	svc := newFedSvc(&fakeFedStore{createErr: core.ErrFederationInvalidName})
	_, err := svc.CreateFederation(context.Background(), &rtiv1.CreateFederationRequest{
		WireVersion:    wireV1(),
		FederationName: "",
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Errorf("code=%v, want InvalidArgument; err=%v", status.Code(err), err)
	}
}

func TestCreateFederation_UnknownError_MapsToInternal(t *testing.T) {
	svc := newFedSvc(&fakeFedStore{createErr: errors.New("disk on fire")})
	_, err := svc.CreateFederation(context.Background(), &rtiv1.CreateFederationRequest{
		WireVersion:    wireV1(),
		FederationName: "alpha",
	})
	if status.Code(err) != codes.Internal {
		t.Errorf("code=%v, want Internal; err=%v", status.Code(err), err)
	}
}

func TestCreateFederation_BestEffortMode_TranslatesEnum(t *testing.T) {
	fake := &fakeFedStore{}
	svc := newFedSvc(fake)
	_, err := svc.CreateFederation(context.Background(), &rtiv1.CreateFederationRequest{
		WireVersion:    wireV1(),
		FederationName: "beta",
		Mode:           rtiv1.Mode_MODE_BEST_EFFORT,
	})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if fake.createCalls[0].Mode != core.ModeBestEffort {
		t.Errorf("Mode=%d, want ModeBestEffort", fake.createCalls[0].Mode)
	}
}

// ===========================================================================
// DestroyFederation
// ===========================================================================

func TestDestroyFederation_Happy_TranslatesAndReturnsEmpty(t *testing.T) {
	fake := &fakeFedStore{}
	svc := newFedSvc(fake)
	resp, err := svc.DestroyFederation(context.Background(), &rtiv1.DestroyFederationRequest{
		WireVersion:    wireV1(),
		FederationName: "alpha",
	})
	if err != nil {
		t.Fatalf("DestroyFederation: %v", err)
	}
	if resp == nil {
		t.Fatal("nil empty response")
	}
	if len(fake.destroyCalls) != 1 || fake.destroyCalls[0] != "alpha" {
		t.Errorf("destroyCalls=%v, want [alpha]", fake.destroyCalls)
	}
}

func TestDestroyFederation_NilRequest_ReturnsInvalidArgument(t *testing.T) {
	svc := newFedSvc(&fakeFedStore{})
	_, err := svc.DestroyFederation(context.Background(), nil)
	if status.Code(err) != codes.InvalidArgument {
		t.Errorf("code=%v, want InvalidArgument", status.Code(err))
	}
}

func TestDestroyFederation_NotFound_MapsToNotFound(t *testing.T) {
	svc := newFedSvc(&fakeFedStore{destroyErr: core.ErrFederationNotFound})
	_, err := svc.DestroyFederation(context.Background(), &rtiv1.DestroyFederationRequest{
		WireVersion:    wireV1(),
		FederationName: "ghost",
	})
	if status.Code(err) != codes.NotFound {
		t.Errorf("code=%v, want NotFound", status.Code(err))
	}
}

func TestDestroyFederation_HasFederates_MapsToFailedPrecondition(t *testing.T) {
	svc := newFedSvc(&fakeFedStore{destroyErr: core.ErrFederationHasFederatesJoined})
	_, err := svc.DestroyFederation(context.Background(), &rtiv1.DestroyFederationRequest{
		WireVersion:    wireV1(),
		FederationName: "alpha",
	})
	if status.Code(err) != codes.FailedPrecondition {
		t.Errorf("code=%v, want FailedPrecondition", status.Code(err))
	}
}

// ===========================================================================
// JoinFederation
// ===========================================================================

func TestJoinFederation_Happy_ReturnsAssignedHandle(t *testing.T) {
	fake := &fakeFedStore{joinHandle: 7}
	svc := newFedSvc(fake)
	resp, err := svc.JoinFederation(context.Background(), &rtiv1.JoinFederationRequest{
		WireVersion:    wireV1(),
		FederationName: "alpha",
		FederateName:   "fa",
	})
	if err != nil {
		t.Fatalf("JoinFederation: %v", err)
	}
	if resp.GetFederateHandle() != 7 {
		t.Errorf("FederateHandle=%d, want 7", resp.GetFederateHandle())
	}
	if len(fake.joinCalls) != 1 ||
		fake.joinCalls[0].Federation != "alpha" ||
		fake.joinCalls[0].FederateName != "fa" {
		t.Errorf("joinCalls=%v", fake.joinCalls)
	}
}

func TestJoinFederation_NilRequest_ReturnsInvalidArgument(t *testing.T) {
	svc := newFedSvc(&fakeFedStore{})
	_, err := svc.JoinFederation(context.Background(), nil)
	if status.Code(err) != codes.InvalidArgument {
		t.Errorf("code=%v, want InvalidArgument", status.Code(err))
	}
}

func TestJoinFederation_NotFound_MapsToNotFound(t *testing.T) {
	svc := newFedSvc(&fakeFedStore{joinErr: core.ErrFederationNotFound})
	_, err := svc.JoinFederation(context.Background(), &rtiv1.JoinFederationRequest{
		WireVersion:    wireV1(),
		FederationName: "ghost",
		FederateName:   "fa",
	})
	if status.Code(err) != codes.NotFound {
		t.Errorf("code=%v, want NotFound", status.Code(err))
	}
}

func TestJoinFederation_AlreadyJoined_MapsToAlreadyExists(t *testing.T) {
	svc := newFedSvc(&fakeFedStore{joinErr: core.ErrFederateAlreadyJoined})
	_, err := svc.JoinFederation(context.Background(), &rtiv1.JoinFederationRequest{
		WireVersion:    wireV1(),
		FederationName: "alpha",
		FederateName:   "fa",
	})
	if status.Code(err) != codes.AlreadyExists {
		t.Errorf("code=%v, want AlreadyExists", status.Code(err))
	}
}

// ===========================================================================
// ResignFederation
// ===========================================================================

func TestResignFederation_Happy_TranslatesActionAndReturnsEmpty(t *testing.T) {
	fake := &fakeFedStore{}
	svc := newFedSvc(fake)
	resp, err := svc.ResignFederation(context.Background(), &rtiv1.ResignFederationRequest{
		WireVersion:    wireV1(),
		FederationName: "alpha",
		FederateHandle: 3,
		Action:         rtiv1.ResignAction_RESIGN_ACTION_UNCONDITIONALLY_DIVEST_ATTRIBUTES,
	})
	if err != nil {
		t.Fatalf("ResignFederation: %v", err)
	}
	if resp == nil {
		t.Fatal("nil empty response")
	}
	if len(fake.resignCalls) != 1 {
		t.Fatalf("resignCalls=%d, want 1", len(fake.resignCalls))
	}
	c := fake.resignCalls[0]
	if c.Fed != "alpha" || c.Handle != 3 || c.Action != core.ResignActionUnconditionallyDivestAttributes {
		t.Errorf("resignCall=%+v, want {alpha 3 UnconditionallyDivest}", c)
	}
}

func TestResignFederation_NilRequest_ReturnsInvalidArgument(t *testing.T) {
	svc := newFedSvc(&fakeFedStore{})
	_, err := svc.ResignFederation(context.Background(), nil)
	if status.Code(err) != codes.InvalidArgument {
		t.Errorf("code=%v, want InvalidArgument", status.Code(err))
	}
}

func TestResignFederation_NotJoined_MapsToFailedPrecondition(t *testing.T) {
	svc := newFedSvc(&fakeFedStore{resignErr: core.ErrFederateNotJoined})
	_, err := svc.ResignFederation(context.Background(), &rtiv1.ResignFederationRequest{
		WireVersion:    wireV1(),
		FederationName: "alpha",
		FederateHandle: 3,
		Action:         rtiv1.ResignAction_RESIGN_ACTION_UNCONDITIONALLY_DIVEST_ATTRIBUTES,
	})
	if status.Code(err) != codes.FailedPrecondition {
		t.Errorf("code=%v, want FailedPrecondition", status.Code(err))
	}
}

func TestResignFederation_FederationNotFound_MapsToNotFound(t *testing.T) {
	svc := newFedSvc(&fakeFedStore{resignErr: core.ErrFederationNotFound})
	_, err := svc.ResignFederation(context.Background(), &rtiv1.ResignFederationRequest{
		WireVersion:    wireV1(),
		FederationName: "ghost",
		FederateHandle: 3,
		Action:         rtiv1.ResignAction_RESIGN_ACTION_UNCONDITIONALLY_DIVEST_ATTRIBUTES,
	})
	if status.Code(err) != codes.NotFound {
		t.Errorf("code=%v, want NotFound", status.Code(err))
	}
}

// ===========================================================================
// ListFederations
// ===========================================================================

func TestListFederations_Happy_TranslatesEntries(t *testing.T) {
	fake := &fakeFedStore{listResp: []core.FederationSummary{
		{Name: "alpha", Mode: core.ModeVerbose, FederatesJoined: 2},
		{Name: "beta", Mode: core.ModeBestEffort, FederatesJoined: 0},
	}}
	svc := newFedSvc(fake)
	resp, err := svc.ListFederations(context.Background(), &rtiv1.ListFederationsRequest{
		WireVersion: wireV1(),
	})
	if err != nil {
		t.Fatalf("ListFederations: %v", err)
	}
	if got, want := len(resp.GetFederations()), 2; got != want {
		t.Fatalf("len(federations)=%d, want %d", got, want)
	}
	a := resp.GetFederations()[0]
	if a.GetName() != "alpha" || a.GetMode() != rtiv1.Mode_MODE_VERBOSE || a.GetFederatesJoined() != 2 {
		t.Errorf("federations[0]=%+v, want {alpha VERBOSE 2}", a)
	}
	b := resp.GetFederations()[1]
	if b.GetName() != "beta" || b.GetMode() != rtiv1.Mode_MODE_BEST_EFFORT || b.GetFederatesJoined() != 0 {
		t.Errorf("federations[1]=%+v, want {beta BEST_EFFORT 0}", b)
	}
}

func TestListFederations_NilRequest_ReturnsInvalidArgument(t *testing.T) {
	svc := newFedSvc(&fakeFedStore{})
	_, err := svc.ListFederations(context.Background(), nil)
	if status.Code(err) != codes.InvalidArgument {
		t.Errorf("code=%v, want InvalidArgument", status.Code(err))
	}
}

func TestListFederations_StoreError_MapsToInternal(t *testing.T) {
	svc := newFedSvc(&fakeFedStore{listErr: errors.New("io failure")})
	_, err := svc.ListFederations(context.Background(), &rtiv1.ListFederationsRequest{
		WireVersion: wireV1(),
	})
	if status.Code(err) != codes.Internal {
		t.Errorf("code=%v, want Internal", status.Code(err))
	}
}

func TestListFederations_EmptyList_ReturnsEmptyResponse(t *testing.T) {
	svc := newFedSvc(&fakeFedStore{listResp: nil})
	resp, err := svc.ListFederations(context.Background(), &rtiv1.ListFederationsRequest{
		WireVersion: wireV1(),
	})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if len(resp.GetFederations()) != 0 {
		t.Errorf("len=%d, want 0", len(resp.GetFederations()))
	}
}

// ===========================================================================
// errToStatus + WireVersion edge cases shared by all RPCs
// ===========================================================================

func TestWireVersionMismatch_MapsToFailedPrecondition(t *testing.T) {
	svc := newFedSvc(&fakeFedStore{createErr: core.ErrWireVersionMismatch})
	_, err := svc.CreateFederation(context.Background(), &rtiv1.CreateFederationRequest{
		WireVersion:    wireV1(),
		FederationName: "alpha",
	})
	if status.Code(err) != codes.FailedPrecondition {
		t.Errorf("code=%v, want FailedPrecondition", status.Code(err))
	}
}

func TestWrappedSentinel_StillMapsCorrectly(t *testing.T) {
	wrapped := errors.New("federation \"alpha\": " + core.ErrFederationNotFound.Error())
	// Bare new error (no Is/wrap chain) maps to Internal — only true wrap chains map.
	svc := newFedSvc(&fakeFedStore{destroyErr: wrapped})
	_, err := svc.DestroyFederation(context.Background(), &rtiv1.DestroyFederationRequest{
		WireVersion:    wireV1(),
		FederationName: "alpha",
	})
	if status.Code(err) != codes.Internal {
		t.Errorf("bare-string error should map to Internal; got %v", status.Code(err))
	}

	// Properly wrapped sentinel must propagate through errors.Is.
	svc2 := newFedSvc(&fakeFedStore{destroyErr: wrap("federation %q destroy", core.ErrFederationNotFound, "alpha")})
	_, err = svc2.DestroyFederation(context.Background(), &rtiv1.DestroyFederationRequest{
		WireVersion:    wireV1(),
		FederationName: "alpha",
	})
	if status.Code(err) != codes.NotFound {
		t.Errorf("wrapped sentinel should map to NotFound; got %v", status.Code(err))
	}
}

// wrap is a tiny helper that produces an error wrapping `cause` with %w
// so the test can assert errors.Is propagation through errToStatus.
func wrap(format string, cause error, args ...any) error {
	return errWrap{msg: format, cause: cause, args: args}
}

type errWrap struct {
	msg   string
	cause error
	args  []any
}

func (e errWrap) Error() string { return e.msg }
func (e errWrap) Unwrap() error { return e.cause }
