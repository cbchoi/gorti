package federate

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/adapters/delivery"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// S1 5.3.3(f), 6.16.3(g), 7.1 and 12.6: a received terminal callback
// resolves acquisition-pending eligibility at invocation, not after its body.
// This tests the real manager and SDK via a test RPC boundary, not full RTID.
func TestOwnershipEntryReviewReentrantServicesResolveTerminalGuard(t *testing.T) {
	for _, operation := range []string{"unpublish", "local-delete"} {
		t.Run(operation, func(t *testing.T) {
			ctx := context.Background()
			out := delivery.NewWithBatch(16, 1, 0)
			queue, release, err := out.Subscribe(ctx, testFederationName, 7)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := release(); err != nil {
					t.Errorf("release subscription: %v", err)
				}
			}()
			manager, err := ownership.New(ownership.Options{Outbox: out})
			if err != nil {
				t.Fatal(err)
			}
			scope := core.OwnershipCallbackScope{Federation: testFederationName, Recipient: 7, FederationGeneration: 4, CallbackEpoch: 1}
			if err := manager.EnableOwnershipCallbackReceipts(ctx, scope); err != nil {
				t.Fatal(err)
			}
			manager.RegisterInitialOwnership(testFederationName, 1, 80, []core.AttributeHandle{2})
			if err := manager.AcquireIfAvailable(ctx, testFederationName, 7, 80, []core.AttributeHandle{2}, nil); err != nil {
				t.Fatal(err)
			}
			var terminal *ownership.CallbackEvent
			select {
			case batch := <-queue:
				for _, event := range batch {
					if value, ok := event.(*ownership.CallbackEvent); ok && value.Kind == ownership.CallbackUnavailable {
						terminal = value
					}
				}
			default:
			}
			if terminal == nil {
				t.Fatal("real manager did not emit Unavailable")
			}
			receipt, finish, err := manager.BeginOwnershipCallbackDelivery(ctx, scope, terminal.CallbackID)
			if err != nil {
				t.Fatal(err)
			}
			finish(true)
			if !manager.HasAcquisitionObligation(testFederationName, 7, 80, []core.AttributeHandle{2}) {
				t.Fatal("transport delivery prematurely removed guard")
			}
			reports := 0
			f := remainingServicesFederate(t, func(ctx context.Context, req any, info *grpc.UnaryServerInfo, _ grpc.UnaryHandler) (any, error) {
				if strings.HasSuffix(info.FullMethod, "ReportOwnershipCallbackInvocation") {
					reports++
					wire := req.(*rtiv1.ReportOwnershipCallbackInvocationRequest).Receipt
					err := manager.ReportOwnershipCallbackInvocation(ctx, scope, core.OwnershipCallbackReceipt{FederationGeneration: wire.FederationGeneration, CallbackEpoch: wire.CallbackEpoch, CallbackID: wire.CallbackId, DeliveryAttempt: wire.DeliveryAttempt})
					return &rtiv1.Empty{}, err
				}
				if manager.HasAcquisitionObligation(testFederationName, 7, 80, []core.AttributeHandle{2}) {
					return nil, status.Error(codes.FailedPrecondition, "terminal callback has not been invoked")
				}
				if strings.HasSuffix(info.FullMethod, "LocalDeleteObjectInstanceFenced") {
					return &rtiv1.LocalDeleteObjectInstanceFence{RetiredKnowledgeEpoch: 10, FederationGeneration: 4, CallbackEpoch: 1}, nil
				}
				return &rtiv1.Empty{}, nil
			})
			f.federationGeneration, f.ownershipReceiptsEnabled = 4, true
			invoke := func() error {
				if operation == "unpublish" {
					return f.UnpublishObjectClassAttributeSetByHandle(ctx, 9, []uint64{2})
				}
				return f.LocalDeleteObjectInstance(ctx, 80)
			}
			if err := invoke(); err == nil {
				t.Fatal("not-yet-invoked terminal guard was bypassed")
			}
			wire := &rtiv1.FederateEvent{OwnershipReceipt: &rtiv1.OwnershipCallbackReceipt{FederationGeneration: receipt.FederationGeneration, CallbackEpoch: receipt.CallbackEpoch, CallbackId: receipt.CallbackID, DeliveryAttempt: receipt.DeliveryAttempt}, Event: &rtiv1.FederateEvent_OwnershipUnavailable{OwnershipUnavailable: &rtiv1.AttributeOwnershipUnavailable{ObjectHandle: 80, AttributeHandles: []uint64{2}}}}
			callbackErr, reportErr := f.DispatchEvent(ctx, f.translate(wire), func(Event) error {
				if reports != 0 {
					t.Fatal("unexpected pre-body report in this SDK path")
				}
				return invoke()
			})
			// This boundary carries an ownership receipt, not an optional generic
			// MOM receipt. Missing observation evidence is separate from the
			// normative service result and mandatory ownership acknowledgment.
			if callbackErr != nil || reportErr != nil && !errors.Is(reportErr, ErrCallbackReceiptUnavailable) || reports != 1 {
				t.Fatalf("reentrant service callback=%v report=%v reports=%d", callbackErr, reportErr, reports)
			}
			if manager.HasAcquisitionObligation(testFederationName, 7, 80, []core.AttributeHandle{2}) {
				t.Fatal("received terminal callback still guards later operations")
			}
		})
	}
}
