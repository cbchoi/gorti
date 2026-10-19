package grpc

import (
	"context"
	"reflect"
	"slices"
	"sync"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ownershipMappedObjects struct {
	core.ObjectRegistry
	mu              sync.Mutex
	err             error
	projectErr      error
	mapOverride     func([]core.AttributeHandle) []core.AttributeHandle
	inputs, outputs int
}

func (o *ownershipMappedObjects) MapKnownAttributesToRegistered(_ context.Context, fed core.FederationName, caller core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) ([]core.AttributeHandle, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.inputs++
	if o.err != nil {
		return nil, o.err
	}
	if fed != "known" || caller != 2 || obj != 10 {
		return nil, core.ErrObjectHandleInvalid
	}
	if o.mapOverride != nil {
		return o.mapOverride(attrs), nil
	}
	result := make([]core.AttributeHandle, len(attrs))
	for i, attr := range attrs {
		switch attr {
		case 11:
			result[i] = 101
		case 12:
			result[i] = 102
		default:
			return nil, core.ErrAttributeNotFound
		}
	}
	return result, nil
}

func (o *ownershipMappedObjects) MapRegisteredAttributesToKnown(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.ObjectHandle, attrs []core.AttributeHandle) ([]core.AttributeHandle, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.outputs++
	if o.projectErr != nil {
		return nil, o.projectErr
	}
	result := make([]core.AttributeHandle, len(attrs))
	for i, attr := range attrs {
		switch attr {
		case 101:
			result[i] = 11
		case 102:
			result[i] = 12
		default:
			return nil, core.ErrAttributeNotFound
		}
	}
	return result, nil
}

type ownershipMappingRecorder struct {
	core.OwnershipCoordinator
	objects  *ownershipMappedObjects
	called   string
	attrs    []core.AttributeHandle
	tag      []byte
	err      error
	onDivest func([]core.AttributeHandle) ([]core.AttributeHandle, error)
}

func (m *ownershipMappingRecorder) record(name string, attrs []core.AttributeHandle, tag []byte) error {
	if m.objects != nil {
		if !m.objects.mu.TryLock() {
			panic("object mapping lease held across ownership manager call")
		}
		m.objects.mu.Unlock()
	}
	m.called, m.attrs, m.tag = name, slices.Clone(attrs), slices.Clone(tag)
	return m.err
}
func (m *ownershipMappingRecorder) UnconditionalDivest(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.ObjectHandle, attrs []core.AttributeHandle) error {
	return m.record("unconditional", attrs, nil)
}
func (m *ownershipMappingRecorder) NegotiatedDivest(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.ObjectHandle, attrs []core.AttributeHandle, tag []byte) error {
	return m.record("negotiated", attrs, tag)
}
func (m *ownershipMappingRecorder) NegotiatedDivestTwoPhase(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.ObjectHandle, attrs []core.AttributeHandle, tag []byte) error {
	return m.record("two-phase", attrs, tag)
}
func (m *ownershipMappingRecorder) ConfirmDivestiture(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.ObjectHandle, attrs []core.AttributeHandle) error {
	return m.record(ownershipConfirmMethod, attrs, nil)
}
func (m *ownershipMappingRecorder) ConfirmDivestitureWithTag(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.ObjectHandle, attrs []core.AttributeHandle, tag []byte) error {
	return m.record("confirm-tagged", attrs, tag)
}
func (m *ownershipMappingRecorder) Acquire(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.ObjectHandle, attrs []core.AttributeHandle, tag []byte) error {
	return m.record("acquire", attrs, tag)
}
func (m *ownershipMappingRecorder) AcquireIfAvailable(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.ObjectHandle, attrs []core.AttributeHandle, tag []byte) error {
	return m.record("available", attrs, tag)
}
func (m *ownershipMappingRecorder) CancelDivest(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.ObjectHandle, attrs []core.AttributeHandle) error {
	return m.record("cancel-divest", attrs, nil)
}
func (m *ownershipMappingRecorder) CancelAcquire(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.ObjectHandle, attrs []core.AttributeHandle) error {
	return m.record("cancel-acquire", attrs, nil)
}
func (m *ownershipMappingRecorder) DivestIfWanted(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.ObjectHandle, attrs []core.AttributeHandle) error {
	return m.record("wanted", attrs, nil)
}
func (m *ownershipMappingRecorder) DivestIfWantedWithResult(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.ObjectHandle, attrs []core.AttributeHandle) ([]core.AttributeHandle, error) {
	if err := m.record(ownershipWantedResultMethod, attrs, nil); err != nil {
		return nil, err
	}
	if m.onDivest != nil {
		return m.onDivest(attrs)
	}
	if len(attrs) == 0 {
		return nil, nil
	}
	return slices.Clone(attrs[:1]), nil
}
func (m *ownershipMappingRecorder) ReleaseDenied(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.ObjectHandle, attrs []core.AttributeHandle) error {
	return m.record("release-denied", attrs, nil)
}

// Hide the newer tagged method while retaining the historical optional pair.
type ownershipUntaggedCoordinator struct {
	core.OwnershipCoordinator
	recorder *ownershipMappingRecorder
}

func (m ownershipUntaggedCoordinator) NegotiatedDivestTwoPhase(ctx context.Context, fed core.FederationName, h core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle, tag []byte) error {
	return m.recorder.NegotiatedDivestTwoPhase(ctx, fed, h, obj, attrs, tag)
}
func (m ownershipUntaggedCoordinator) ConfirmDivestiture(ctx context.Context, fed core.FederationName, h core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) error {
	return m.recorder.ConfirmDivestiture(ctx, fed, h, obj, attrs)
}

func ownershipMappedCall(s *ownershipService, method string, attrs []uint64) ([]uint64, error) {
	ctx := context.Background()
	tag := []byte{0, 255, 7}
	var err error
	switch method {
	case "unconditional":
		_, err = s.UnconditionalAttributeOwnershipDivestiture(ctx, &rtiv1.UnconditionalDivestRequest{WireVersion: 1, FederationName: "known", FederateHandle: 2, ObjectHandle: 10, AttributeHandles: attrs})
	case "negotiated", "two-phase":
		_, err = s.NegotiatedAttributeOwnershipDivestiture(ctx, &rtiv1.NegotiatedDivestRequest{WireVersion: 1, FederationName: "known", FederateHandle: 2, ObjectHandle: 10, AttributeHandles: attrs, Tag: tag, TwoPhase: method == "two-phase"})
	case ownershipConfirmMethod, "confirm-tagged":
		if method == ownershipConfirmMethod {
			tag = nil
		}
		_, err = s.ConfirmDivestiture(ctx, &rtiv1.ConfirmDivestitureRequest{WireVersion: 1, FederationName: "known", FederateHandle: 2, ObjectHandle: 10, AttributeHandles: attrs, Tag: tag})
	case "acquire", "available":
		_, err = s.AttributeOwnershipAcquisition(ctx, &rtiv1.AcquireRequest{WireVersion: 1, FederationName: "known", FederateHandle: 2, ObjectHandle: 10, AttributeHandles: attrs, Tag: tag, IfAvailable: method == "available"})
	case "cancel-divest":
		_, err = s.CancelNegotiatedAttributeOwnershipDivestiture(ctx, &rtiv1.CancelDivestRequest{WireVersion: 1, FederationName: "known", FederateHandle: 2, ObjectHandle: 10, AttributeHandles: attrs})
	case "cancel-acquire":
		_, err = s.CancelAttributeOwnershipAcquisition(ctx, &rtiv1.CancelAcquireRequest{WireVersion: 1, FederationName: "known", FederateHandle: 2, ObjectHandle: 10, AttributeHandles: attrs})
	case "wanted":
		_, err = s.AttributeOwnershipDivestitureIfWanted(ctx, &rtiv1.DivestIfWantedRequest{WireVersion: 1, FederationName: "known", FederateHandle: 2, ObjectHandle: 10, AttributeHandles: attrs})
	case ownershipWantedResultMethod:
		var result *rtiv1.DivestIfWantedResponse
		result, err = s.AttributeOwnershipDivestitureIfWantedWithResult(ctx, &rtiv1.DivestIfWantedRequest{WireVersion: 1, FederationName: "known", FederateHandle: 2, ObjectHandle: 10, AttributeHandles: attrs})
		if result != nil {
			return result.DivestedAttributeHandles, err
		}
	case "release-denied":
		_, err = s.AttributeOwnershipReleaseDenied(ctx, &rtiv1.ReleaseDeniedRequest{WireVersion: 1, FederationName: "known", FederateHandle: 2, ObjectHandle: 10, AttributeHandles: attrs})
	default:
		panic(method)
	}
	return nil, err
}

func TestOwnershipKnownClassAllMutationPaths(t *testing.T) {
	for _, method := range []string{"unconditional", "negotiated", "two-phase", ownershipConfirmMethod, "confirm-tagged", "acquire", "available", "cancel-divest", "cancel-acquire", "wanted", ownershipWantedResultMethod, "release-denied"} {
		for _, mapped := range []bool{false, true} {
			t.Run(method+map[bool]string{false: "/legacy", true: "/mapped"}[mapped], func(t *testing.T) {
				objects := &ownershipMappedObjects{}
				recorder := &ownershipMappingRecorder{objects: objects}
				var manager core.OwnershipCoordinator = recorder
				if method == ownershipConfirmMethod {
					manager = ownershipUntaggedCoordinator{OwnershipCoordinator: recorder, recorder: recorder}
				}
				service := newOwnershipService(manager)
				if mapped {
					service.objects = objects
				} else {
					service.objects = struct{ core.ObjectRegistry }{}
				}
				input := []uint64{12, 11, 12}
				result, err := ownershipMappedCall(service, method, input)
				if err != nil {
					t.Fatal(err)
				}
				want := []core.AttributeHandle{12, 11, 12}
				if mapped {
					want = []core.AttributeHandle{102, 101, 102}
				}
				if recorder.called != method || !reflect.DeepEqual(recorder.attrs, want) {
					t.Fatalf("manager got %s %v, want %s %v", recorder.called, recorder.attrs, method, want)
				}
				if !reflect.DeepEqual(input, []uint64{12, 11, 12}) {
					t.Fatal("caller request mutated")
				}
				if mapped && objects.inputs != 1 {
					t.Fatalf("input mapped %d times", objects.inputs)
				}
				if method == ownershipWantedResultMethod && !reflect.DeepEqual(result, []uint64{12}) {
					t.Fatalf("canonical divest result leaked: %v", result)
				}
				if slices.Contains([]string{"negotiated", "two-phase", "confirm-tagged", "acquire", "available"}, method) && !reflect.DeepEqual(recorder.tag, []byte{0, 255, 7}) {
					t.Fatal("tag changed")
				}
				if !mapped {
					return
				}
				recorder.called = ""
				_, err = ownershipMappedCall(service, method, []uint64{11, 999})
				if err == nil || recorder.called != "" {
					t.Fatalf("mixed invalid set reached manager: %s, %v", recorder.called, err)
				}
			})
		}
	}
}

func TestOwnershipKnownClassMappingAndManagerErrors(t *testing.T) {
	objects := &ownershipMappedObjects{projectErr: core.ErrObjectHandleInvalid}
	recorder := &ownershipMappingRecorder{objects: objects}
	s := newOwnershipService(recorder, objects)
	result, err := ownershipMappedCall(s, ownershipWantedResultMethod, []uint64{11})
	if err != nil || !slices.Equal(result, []uint64{11}) || recorder.called != ownershipWantedResultMethod || objects.outputs != 0 {
		t.Fatalf("committed result required live projection: result=%v error=%v reads=%d", result, err, objects.outputs)
	}
	objects.outputs = 0
	recorder.err = core.ErrAttributeNotOwned
	_, err = ownershipMappedCall(s, ownershipWantedResultMethod, []uint64{11})
	if err == nil || objects.outputs != 0 {
		t.Fatal("failed mutation projected a success result")
	}
	objects.err = core.ErrObjectHandleInvalid
	recorder.called = ""
	_, err = ownershipMappedCall(s, "acquire", []uint64{11})
	if err == nil || recorder.called != "" {
		t.Fatal("unknown object reached manager")
	}
	_, err = s.AttributeOwnershipAcquisition(context.Background(), &rtiv1.AcquireRequest{WireVersion: 999})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("wire validation changed: %v", err)
	}
}

func TestOwnershipKnownClassDivestResultRetainsRequestCoordinates(t *testing.T) {
	for _, transition := range []string{"knowledge-changed", "knowledge-removed", "mapper-removed", "slices-mutated"} {
		t.Run(transition, func(t *testing.T) {
			objects := &ownershipMappedObjects{}
			recorder := &ownershipMappingRecorder{objects: objects}
			s := newOwnershipService(recorder, objects)
			input := []uint64{12, 11, 12}
			recorder.onDivest = func(attrs []core.AttributeHandle) ([]core.AttributeHandle, error) {
				result := []core.AttributeHandle{attrs[1], attrs[0]}
				switch transition {
				case "knowledge-changed":
					objects.mapOverride = slices.Clone[[]core.AttributeHandle]
					objects.projectErr = core.ErrAttributeNotFound
				case "knowledge-removed":
					objects.err, objects.projectErr = core.ErrObjectHandleInvalid, core.ErrObjectHandleInvalid
				case "mapper-removed":
					s.objects = nil
				case "slices-mutated":
					input[0], input[1] = 90, 91
					attrs[0], attrs[1] = 901, 902
				}
				return result, nil
			}
			result, err := ownershipMappedCall(s, ownershipWantedResultMethod, input)
			if err != nil || !slices.Equal(result, []uint64{11, 12}) {
				t.Fatalf("original request coordinates lost: result=%v error=%v", result, err)
			}
			if objects.inputs != 1 || objects.outputs != 0 {
				t.Fatalf("unexpected mapping reads: inputs=%d outputs=%d", objects.inputs, objects.outputs)
			}
		})
	}
}

func TestOwnershipKnownClassDivestResultRejectsAmbiguousMappingBeforeMutation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		input  []uint64
		mapped []core.AttributeHandle
	}{
		{"short", []uint64{11, 12}, []core.AttributeHandle{101}},
		{"long", []uint64{11}, []core.AttributeHandle{101, 102}},
		{"canonical-collision", []uint64{11, 12}, []core.AttributeHandle{101, 101}},
		{"known-collision", []uint64{11, 11}, []core.AttributeHandle{101, 102}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			objects := &ownershipMappedObjects{mapOverride: func([]core.AttributeHandle) []core.AttributeHandle { return slices.Clone(tc.mapped) }}
			recorder := &ownershipMappingRecorder{objects: objects}
			result, err := ownershipMappedCall(newOwnershipService(recorder, objects), ownershipWantedResultMethod, tc.input)
			if status.Code(err) != codes.Internal || result != nil || recorder.called != "" {
				t.Fatalf("invalid mapping reached mutation: result=%v error=%v called=%q", result, err, recorder.called)
			}
			if objects.inputs != 1 || objects.outputs != 0 {
				t.Fatalf("unexpected mapping reads: inputs=%d outputs=%d", objects.inputs, objects.outputs)
			}
		})
	}
}

func TestOwnershipKnownClassDivestResultSubsetIntegrity(t *testing.T) {
	for _, tc := range []struct {
		name      string
		input     []uint64
		committed []core.AttributeHandle
		want      []uint64
		code      codes.Code
	}{
		{"empty-subset", []uint64{11, 12}, nil, nil, codes.OK},
		{"empty-request", nil, nil, nil, codes.OK},
		{"repeated-request", []uint64{11, 11}, []core.AttributeHandle{101}, []uint64{11}, codes.OK},
		{"known-but-not-requested", []uint64{11}, []core.AttributeHandle{101, 102}, nil, codes.Internal},
		{"unknown", []uint64{11}, []core.AttributeHandle{999}, nil, codes.Internal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			objects := &ownershipMappedObjects{}
			recorder := &ownershipMappingRecorder{objects: objects, onDivest: func([]core.AttributeHandle) ([]core.AttributeHandle, error) { return tc.committed, nil }}
			result, err := ownershipMappedCall(newOwnershipService(recorder, objects), ownershipWantedResultMethod, tc.input)
			if status.Code(err) != tc.code || !slices.Equal(result, tc.want) || recorder.called != ownershipWantedResultMethod {
				t.Fatalf("subset integrity: result=%v error=%v called=%q", result, err, recorder.called)
			}
			if objects.inputs != 1 || objects.outputs != 0 {
				t.Fatalf("unexpected mapping reads: inputs=%d outputs=%d", objects.inputs, objects.outputs)
			}
		})
	}
}

func TestOwnershipKnownClassLegacyCoordinatorFallback(t *testing.T) {
	objects := &ownershipMappedObjects{}
	recorder := &ownershipMappingRecorder{objects: objects}
	service := newOwnershipService(struct{ core.OwnershipCoordinator }{recorder}, objects)
	for method, want := range map[string]string{"two-phase": "negotiated", "available": "acquire"} {
		if _, err := ownershipMappedCall(service, method, []uint64{11}); err != nil {
			t.Fatal(err)
		}
		if recorder.called != want || !reflect.DeepEqual(recorder.attrs, []core.AttributeHandle{101}) {
			t.Fatalf("legacy fallback got %s %v", recorder.called, recorder.attrs)
		}
	}
	for _, method := range []string{ownershipConfirmMethod, "confirm-tagged", ownershipWantedResultMethod, "release-denied"} {
		if _, err := ownershipMappedCall(service, method, []uint64{11}); status.Code(err) != codes.Unimplemented {
			t.Fatalf("legacy %s capability behavior changed: %v", method, err)
		}
	}
}

const (
	ownershipWantedResultMethod = "wanted-result"
	ownershipConfirmMethod      = "confirm"
)
