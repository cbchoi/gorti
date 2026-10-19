package grpc

import (
	"context"
	"slices"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type ownershipKnownRecorder struct {
	core.OwnershipCoordinator
	available bool
	checks    int
	commands  []core.OwnershipCommand
	execute   func(context.Context, core.OwnershipCommand) ([]core.AttributeHandle, error)
}

func (m *ownershipKnownRecorder) KnownOwnershipAvailable() bool {
	m.checks++
	return m.available
}

func (m *ownershipKnownRecorder) ExecuteKnownOwnership(ctx context.Context, command core.OwnershipCommand) ([]core.AttributeHandle, error) {
	captured := command
	captured.Attributes, captured.Tag = slices.Clone(command.Attributes), slices.Clone(command.Tag)
	m.commands = append(m.commands, captured)
	if m.execute != nil {
		return m.execute(ctx, command)
	}
	return nil, nil
}

func TestOwnershipAtomicAllMutationPaths(t *testing.T) {
	for _, tc := range []struct {
		method    string
		operation core.OwnershipOperation
		tagged    bool
	}{
		{"unconditional", core.OwnershipUnconditionalDivest, false},
		{"negotiated", core.OwnershipNegotiatedDivest, true},
		{"two-phase", core.OwnershipNegotiatedDivestTwoPhase, true},
		{ownershipConfirmMethod, core.OwnershipConfirmDivestiture, false},
		{"confirm-tagged", core.OwnershipConfirmDivestiture, true},
		{"acquire", core.OwnershipAcquire, true},
		{"available", core.OwnershipAcquireIfAvailable, true},
		{"cancel-divest", core.OwnershipCancelDivest, false},
		{"cancel-acquire", core.OwnershipCancelAcquire, false},
		{"wanted", core.OwnershipDivestIfWanted, false},
		{ownershipWantedResultMethod, core.OwnershipDivestIfWanted, false},
		{"release-denied", core.OwnershipReleaseDenied, false},
	} {
		t.Run(tc.method, func(t *testing.T) {
			objects := &ownershipMappedObjects{err: core.ErrObjectHandleInvalid}
			legacy := &ownershipMappingRecorder{objects: objects}
			manager := &ownershipKnownRecorder{OwnershipCoordinator: legacy, available: true}
			manager.execute = func(_ context.Context, command core.OwnershipCommand) ([]core.AttributeHandle, error) {
				if !objects.mu.TryLock() {
					t.Fatal("object lease held across atomic manager call")
				}
				objects.mu.Unlock()
				command.Attributes[0] = 999
				if len(command.Tag) > 0 {
					command.Tag[0] = 99
				}
				if command.Operation == core.OwnershipDivestIfWanted {
					return []core.AttributeHandle{11, 12}, nil
				}
				return nil, nil
			}
			input := []uint64{12, 11, 12}
			result, err := ownershipMappedCall(newOwnershipService(manager, objects), tc.method, input)
			if err != nil {
				t.Fatal(err)
			}
			if manager.checks != 1 || len(manager.commands) != 1 || legacy.called != "" || objects.inputs != 0 || objects.outputs != 0 {
				t.Fatalf("atomic dispatch: checks=%d commands=%d legacy=%q mapping=%d/%d", manager.checks, len(manager.commands), legacy.called, objects.inputs, objects.outputs)
			}
			command := manager.commands[0]
			if command.Operation != tc.operation || command.Federation != "known" || command.Federate != 2 || command.Object != 10 || !slices.Equal(command.Attributes, []core.AttributeHandle{12, 11, 12}) {
				t.Fatalf("wrong atomic command: %+v", command)
			}
			var wantTag []byte
			if tc.tagged {
				wantTag = []byte{0, 255, 7}
			}
			if !slices.Equal(command.Tag, wantTag) || !slices.Equal(input, []uint64{12, 11, 12}) {
				t.Fatal("tag or request attributes changed")
			}
			if tc.method == ownershipWantedResultMethod && !slices.Equal(result, []uint64{11, 12}) {
				t.Fatalf("known result changed: %v", result)
			}
		})
	}
}

func TestOwnershipAtomicUnavailableUsesLegacyFallback(t *testing.T) {
	for method, want := range map[string]string{
		"unconditional": "unconditional", "negotiated": "negotiated", "two-phase": "negotiated",
		"acquire": "acquire", "available": "acquire", "cancel-divest": "cancel-divest", "cancel-acquire": "cancel-acquire", "wanted": "wanted",
	} {
		t.Run(method, func(t *testing.T) {
			objects := &ownershipMappedObjects{}
			legacy := &ownershipMappingRecorder{objects: objects}
			manager := &ownershipKnownRecorder{OwnershipCoordinator: legacy}
			_, err := ownershipMappedCall(newOwnershipService(manager, objects), method, []uint64{11})
			if err != nil || manager.checks != 1 || len(manager.commands) != 0 || legacy.called != want || !slices.Equal(legacy.attrs, []core.AttributeHandle{101}) || objects.inputs != 1 {
				t.Fatalf("fallback changed: error=%v checks=%d commands=%d legacy=%q attrs=%v maps=%d", err, manager.checks, len(manager.commands), legacy.called, legacy.attrs, objects.inputs)
			}
		})
	}
	for _, method := range []string{ownershipConfirmMethod, "confirm-tagged", ownershipWantedResultMethod, "release-denied"} {
		t.Run(method, func(t *testing.T) {
			objects := &ownershipMappedObjects{}
			manager := &ownershipKnownRecorder{OwnershipCoordinator: &ownershipMappingRecorder{}}
			_, err := ownershipMappedCall(newOwnershipService(manager, objects), method, []uint64{11})
			if status.Code(err) != codes.Unimplemented || manager.checks != 1 || len(manager.commands) != 0 {
				t.Fatalf("fallback capability changed: %v", err)
			}
		})
	}
}

type ownershipAtomicTransport struct{ trailer metadata.MD }

func (*ownershipAtomicTransport) Method() string               { return "ownership" }
func (*ownershipAtomicTransport) SetHeader(metadata.MD) error  { return nil }
func (*ownershipAtomicTransport) SendHeader(metadata.MD) error { return nil }
func (s *ownershipAtomicTransport) SetTrailer(md metadata.MD) error {
	s.trailer = metadata.Join(s.trailer, md)
	return nil
}

func TestOwnershipAtomicErrorMetadataAndContext(t *testing.T) {
	for _, tc := range []struct {
		acquisition bool
		err         error
		name        string
	}{
		{false, core.ErrOwnershipNotInTransfer, "AttributeDivestitureWasNotRequested"},
		{true, core.ErrOwnershipNotInTransfer, "AttributeAcquisitionWasNotRequested"},
		{true, core.ErrFederateOwnsAttributes, "AttributeAlreadyOwned"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stream := &ownershipAtomicTransport{}
			ctx := grpcgo.NewContextWithServerTransportStream(context.Background(), stream)
			objects := &ownershipMappedObjects{err: core.ErrObjectHandleInvalid}
			manager := &ownershipKnownRecorder{available: true, execute: func(got context.Context, _ core.OwnershipCommand) ([]core.AttributeHandle, error) {
				if got != ctx {
					t.Fatal("RPC context lost")
				}
				return nil, tc.err
			}}
			s := newOwnershipService(manager, objects)
			var err error
			if tc.acquisition {
				_, err = s.CancelAttributeOwnershipAcquisition(ctx, &rtiv1.CancelAcquireRequest{WireVersion: 1})
			} else {
				_, err = s.CancelNegotiatedAttributeOwnershipDivestiture(ctx, &rtiv1.CancelDivestRequest{WireVersion: 1})
			}
			if status.Code(err) != codes.FailedPrecondition || !slices.Equal(stream.trailer.Get(specExceptionTrailerKey), []string{tc.name}) || objects.inputs != 0 {
				t.Fatalf("atomic cancellation: %v trailers=%v", err, stream.trailer)
			}
		})
	}
}

func TestOwnershipAtomicDivestResultIntegrity(t *testing.T) {
	for _, tc := range []struct {
		name   string
		result []core.AttributeHandle
		err    error
		code   codes.Code
	}{
		{"empty", nil, nil, codes.OK},
		{"known", []core.AttributeHandle{11}, nil, codes.OK},
		{"canonical", []core.AttributeHandle{101}, nil, codes.Internal},
		{"unrequested", []core.AttributeHandle{12}, nil, codes.Internal},
		{"manager-error", []core.AttributeHandle{11}, core.ErrAttributeNotOwned, codes.PermissionDenied},
	} {
		t.Run(tc.name, func(t *testing.T) {
			manager := &ownershipKnownRecorder{available: true, execute: func(context.Context, core.OwnershipCommand) ([]core.AttributeHandle, error) { return tc.result, tc.err }}
			result, err := ownershipMappedCall(newOwnershipService(manager), ownershipWantedResultMethod, []uint64{11})
			if status.Code(err) != tc.code || (err != nil && result != nil) {
				t.Fatalf("result=%v error=%v", result, err)
			}
		})
	}
}

func TestOwnershipAtomicValidationPrecedesDispatch(t *testing.T) {
	manager := &ownershipKnownRecorder{available: true}
	objects := &ownershipMappedObjects{}
	s := newOwnershipService(manager, objects)
	for _, nilRequest := range []bool{true, false} {
		for _, call := range []func() error{
			func() error {
				req := &rtiv1.UnconditionalDivestRequest{WireVersion: 999}
				if nilRequest {
					req = nil
				}
				_, err := s.UnconditionalAttributeOwnershipDivestiture(context.Background(), req)
				return err
			},
			func() error {
				req := &rtiv1.NegotiatedDivestRequest{WireVersion: 999}
				if nilRequest {
					req = nil
				}
				_, err := s.NegotiatedAttributeOwnershipDivestiture(context.Background(), req)
				return err
			},
			func() error {
				req := &rtiv1.ConfirmDivestitureRequest{WireVersion: 999}
				if nilRequest {
					req = nil
				}
				_, err := s.ConfirmDivestiture(context.Background(), req)
				return err
			},
			func() error {
				req := &rtiv1.AcquireRequest{WireVersion: 999}
				if nilRequest {
					req = nil
				}
				_, err := s.AttributeOwnershipAcquisition(context.Background(), req)
				return err
			},
			func() error {
				req := &rtiv1.CancelDivestRequest{WireVersion: 999}
				if nilRequest {
					req = nil
				}
				_, err := s.CancelNegotiatedAttributeOwnershipDivestiture(context.Background(), req)
				return err
			},
			func() error {
				req := &rtiv1.CancelAcquireRequest{WireVersion: 999}
				if nilRequest {
					req = nil
				}
				_, err := s.CancelAttributeOwnershipAcquisition(context.Background(), req)
				return err
			},
			func() error {
				req := &rtiv1.DivestIfWantedRequest{WireVersion: 999}
				if nilRequest {
					req = nil
				}
				_, err := s.AttributeOwnershipDivestitureIfWanted(context.Background(), req)
				return err
			},
			func() error {
				req := &rtiv1.DivestIfWantedRequest{WireVersion: 999}
				if nilRequest {
					req = nil
				}
				_, err := s.AttributeOwnershipDivestitureIfWantedWithResult(context.Background(), req)
				return err
			},
			func() error {
				req := &rtiv1.ReleaseDeniedRequest{WireVersion: 999}
				if nilRequest {
					req = nil
				}
				_, err := s.AttributeOwnershipReleaseDenied(context.Background(), req)
				return err
			},
		} {
			want := codes.FailedPrecondition
			if nilRequest {
				want = codes.InvalidArgument
			}
			if err := call(); status.Code(err) != want {
				t.Fatalf("validation changed: nil=%v error=%v", nilRequest, err)
			}
		}
	}
	if manager.checks != 0 || len(manager.commands) != 0 || objects.inputs != 0 {
		t.Fatal("invalid request dispatched")
	}
}
