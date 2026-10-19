package federate

import (
	"context"
	"errors"
	"reflect"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func deletionCacheFederate() *Federate {
	return &Federate{federateHandle: 7, objectClasses: map[uint64]uint64{80: 19}, ownedObjectAttributes: map[uint64]map[uint64]struct{}{80: {2: {}, 7: {}}}}
}

func deletionCacheRemove(sender, id uint64, sent, received rtiv1.OrderType) *rtiv1.FederateEvent {
	at := 10.0
	return &rtiv1.FederateEvent{Event: &rtiv1.FederateEvent_Remove{Remove: &rtiv1.RemoveObjectInstance{
		ObjectHandle: 80, LogicalTime: &at,
		Metadata: &rtiv1.ObjectCallbackMetadata{ProducingFederate: &sender, MessageRetractionHandle: &id, SentOrder: &sent, ReceivedOrder: &received},
	}}}
}

func deletionCacheRetract(sender, id uint64) *rtiv1.FederateEvent {
	return &rtiv1.FederateEvent{Event: &rtiv1.FederateEvent_RetractionRequested{RetractionRequested: &rtiv1.RequestRetraction{SenderFederate: sender, MessageRetractionHandle: id}}}
}

// Sections 8.21-8.22 reconstitute a legally retracted deletion. Received RO
// does not remove retractability when the original sent order was TSO.
func TestRetractionCallbackRestoresDeletedObjectKnowledge(t *testing.T) {
	for _, received := range []rtiv1.OrderType{rtiv1.OrderType_ORDER_TYPE_TIMESTAMP, rtiv1.OrderType_ORDER_TYPE_RECEIVE} {
		t.Run(received.String(), func(t *testing.T) {
			f := deletionCacheFederate()
			if received == rtiv1.OrderType_ORDER_TYPE_TIMESTAMP {
				f.conn = &Connection{tm: deletionCacheTimeClient{}}
				if err := f.FlushQueueRequest(context.Background(), 20); err != nil {
					t.Fatal(err)
				}
			}
			f.translate(deletionCacheRemove(3, 501, rtiv1.OrderType_ORDER_TYPE_TIMESTAMP, received))
			if _, known := f.objectClassFor(80); known != (received == rtiv1.OrderType_ORDER_TYPE_TIMESTAMP) {
				t.Fatal("RO/FQR removal has incorrect immediate object knowledge")
			}
			event := f.translate(deletionCacheRetract(3, 501))
			if _, ok := event.(RequestRetraction); !ok {
				t.Fatalf("callback lost: %T", event)
			}
			if class, known := f.objectClassFor(80); !known || class != 19 {
				t.Fatal("legal deletion retraction did not restore the original object class")
			}
			if !reflect.DeepEqual(f.ownedObjectAttributes[80], map[uint64]struct{}{2: {}, 7: {}}) {
				t.Fatal("deletion retraction did not restore known ownership")
			}
			f.updateOwnedAttributes(80, []uint64{2}, false)
			f.translate(deletionCacheRetract(3, 501))
			if _, owned := f.ownedObjectAttributes[80][2]; owned {
				t.Fatal("duplicate retraction replayed stale ownership")
			}
			f.translate(&rtiv1.FederateEvent{Event: &rtiv1.FederateEvent_Grant{Grant: &rtiv1.TimeAdvanceGrant{LogicalTime: 10}}})
			if _, known := f.objectClassFor(80); !known {
				t.Fatal("retracted speculative deletion still fired at its old timestamp")
			}
		})
	}
}

func TestRetractionCallbackDoesNotInventObjectKnowledge(t *testing.T) {
	for _, scenario := range []string{"sent-ro", "missing-metadata", "unknown-object", "wrong-sender", "wrong-id", "local-forget", "rediscovered", "ordinary-tso", "permanent-after-speculative"} {
		t.Run(scenario, func(t *testing.T) {
			f := deletionCacheFederate()
			remove := deletionCacheRemove(3, 501, rtiv1.OrderType_ORDER_TYPE_TIMESTAMP, rtiv1.OrderType_ORDER_TYPE_RECEIVE)
			sender, id := uint64(3), uint64(501)
			switch scenario {
			case "sent-ro":
				order := rtiv1.OrderType_ORDER_TYPE_RECEIVE
				remove.GetRemove().Metadata.SentOrder = &order
			case "missing-metadata":
				remove.GetRemove().Metadata = nil
			case "unknown-object":
				f.forgetObjectClass(80)
			case "wrong-sender":
				sender++
			case "wrong-id":
				id++
			case "ordinary-tso":
				order := rtiv1.OrderType_ORDER_TYPE_TIMESTAMP
				remove.GetRemove().Metadata.ReceivedOrder = &order
			}
			f.translate(remove)
			if scenario == "local-forget" {
				f.forgetObjectClass(80)
			}
			if scenario == "permanent-after-speculative" {
				f.translate(deletionCacheRemove(3, 0, rtiv1.OrderType_ORDER_TYPE_RECEIVE, rtiv1.OrderType_ORDER_TYPE_RECEIVE))
			}
			if scenario == "rediscovered" {
				f.rememberRegisteredObject(80, 27)
			}
			f.translate(deletionCacheRetract(sender, id))
			if class, known := f.objectClassFor(80); scenario == "rediscovered" {
				if !known || class != 27 || len(f.ownedObjectAttributes[80]) != 0 {
					t.Fatal("old deletion overwrote rediscovered state")
				}
			} else if known {
				t.Fatal("retraction fabricated an object from unqualified history")
			}
		})
	}
}

type deletionCacheObjectClient struct {
	rtiv1.ObjectServiceClient
	onDelete  func(*rtiv1.DeleteObjectInstanceRequest) error
	onRetract func(*rtiv1.RetractRequest) error
}

func (c deletionCacheObjectClient) DeleteObjectInstance(_ context.Context, req *rtiv1.DeleteObjectInstanceRequest, _ ...grpc.CallOption) (*rtiv1.Empty, error) {
	if c.onDelete != nil {
		return &rtiv1.Empty{}, c.onDelete(req)
	}
	return &rtiv1.Empty{}, nil
}

func (c deletionCacheObjectClient) Retract(_ context.Context, req *rtiv1.RetractRequest, _ ...grpc.CallOption) (*rtiv1.Empty, error) {
	if c.onRetract != nil {
		return &rtiv1.Empty{}, c.onRetract(req)
	}
	return &rtiv1.Empty{}, nil
}

func TestSenderRetractionRestoresOnlySuccessfulTimestampedDeletion(t *testing.T) {
	for _, scenario := range []string{"tso-success", "ro-error", "too-late", "delete-error", "unknown-object", testNoDesignator} {
		t.Run(scenario, func(t *testing.T) {
			f := deletionCacheFederate()
			at := 10.0
			ts, id := &at, uint64(501)
			if scenario == "ro-error" {
				ts = nil
			}
			if scenario == testNoDesignator {
				id = 0
			}
			if scenario == "unknown-object" {
				f.forgetObjectClass(80)
			}
			f.conn = &Connection{obj: deletionCacheObjectClient{
				onDelete: func(*rtiv1.DeleteObjectInstanceRequest) error {
					if scenario == "delete-error" {
						return status.Error(codes.PermissionDenied, "not owned")
					}
					return nil
				},
				onRetract: func(*rtiv1.RetractRequest) error {
					if scenario == "ro-error" || scenario == testNoDesignator {
						return status.Error(codes.InvalidArgument, "message retraction designator is invalid for this sender")
					}
					if scenario == "too-late" {
						return status.Error(codes.FailedPrecondition, "object deletion has already been delivered")
					}
					return nil
				},
			}}
			err := f.DeleteObjectInstanceWithOptions(context.Background(), 80, ts, MessageOptions{RetractionHandle: id})
			if scenario == "delete-error" {
				if err == nil {
					t.Fatal("delete error hidden")
				}
				if _, known := f.objectClassFor(80); !known {
					t.Fatal("failed deletion erased object knowledge")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, known := f.objectClassFor(80); known {
				t.Fatal("delete was not reflected in local knowledge")
			}
			err = f.Retract(context.Background(), id)
			if scenario == "too-late" && !errors.Is(err, ErrMessageCanNoLongerBeRetracted) {
				t.Fatalf("wrong late error %v", err)
			}
			class, known := f.objectClassFor(80)
			if scenario == "tso-success" {
				if err != nil || !known || class != 19 || len(f.ownedObjectAttributes[80]) != 2 {
					t.Fatal("successful originating retraction did not restore known object state")
				}
			} else if known {
				t.Fatal("failed or unqualified originating retraction revived object knowledge")
			}
		})
	}
}

type deletionCacheTimeClient struct {
	rtiv1.TimeServiceClient
	onFlush      func() error
	disableError error
}

func (c deletionCacheTimeClient) FlushQueueRequest(context.Context, *rtiv1.FQRRequest, ...grpc.CallOption) (*rtiv1.Empty, error) {
	if c.onFlush != nil {
		return &rtiv1.Empty{}, c.onFlush()
	}
	return &rtiv1.Empty{}, nil
}

func (c deletionCacheTimeClient) DisableTimeConstrained(context.Context, *rtiv1.DisableConstrainedRequest, ...grpc.CallOption) (*rtiv1.Empty, error) {
	return &rtiv1.Empty{}, c.disableError
}

func TestFQRDeletionKnowledgeRetirement(t *testing.T) {
	for _, terminal := range []string{"grant-before", "grant-equal", "grant-after", "disable", testDisableFailed, testNoDesignator} {
		t.Run(terminal, func(t *testing.T) {
			f := deletionCacheFederate()
			client := deletionCacheTimeClient{}
			if terminal == testDisableFailed {
				client.disableError = status.Error(codes.FailedPrecondition, "save in progress")
			}
			f.conn = &Connection{tm: client}
			if err := f.FlushQueueRequest(context.Background(), 20); err != nil {
				t.Fatal(err)
			}
			remove := deletionCacheRemove(3, 501, rtiv1.OrderType_ORDER_TYPE_TIMESTAMP, rtiv1.OrderType_ORDER_TYPE_TIMESTAMP)
			if terminal == testNoDesignator {
				remove.GetRemove().Metadata.MessageRetractionHandle = nil
			}
			f.translate(remove)
			if _, known := f.objectClassFor(80); !known {
				t.Fatal("FQR remove forgot object before its timestamp")
			}
			at := 10.0
			if terminal == "grant-before" {
				at = 9
			}
			if terminal == "grant-after" {
				at = 11
			}
			if terminal == "disable" || terminal == testDisableFailed {
				err := f.DisableTimeConstrained(context.Background())
				if (err != nil) != (terminal == testDisableFailed) {
					t.Fatalf("disable error=%v", err)
				}
			} else {
				f.translate(&rtiv1.FederateEvent{Event: &rtiv1.FederateEvent_Grant{Grant: &rtiv1.TimeAdvanceGrant{LogicalTime: at}}})
			}
			wantKnown := terminal == "grant-before" || terminal == testDisableFailed
			if _, known := f.objectClassFor(80); known != wantKnown {
				t.Fatalf("%s: known=%v want=%v", terminal, known, wantKnown)
			}
			f.translate(deletionCacheRetract(3, 501))
			if _, known := f.objectClassFor(80); known != (wantKnown || terminal == "disable") {
				t.Fatal("retraction restored an expired/untracked deletion or failed to restore a still retractable deletion")
			}
		})
	}
}

func TestFQRCallbackBeforeRPCResponseKeepsSpeculativeKnowledge(t *testing.T) {
	f := deletionCacheFederate()
	f.conn = &Connection{tm: deletionCacheTimeClient{onFlush: func() error {
		f.translate(deletionCacheRemove(3, 501, rtiv1.OrderType_ORDER_TYPE_TIMESTAMP, rtiv1.OrderType_ORDER_TYPE_TIMESTAMP))
		if _, known := f.objectClassFor(80); !known {
			t.Fatal("FQR mode was not registered before RPC")
		}
		f.translate(&rtiv1.FederateEvent{Event: &rtiv1.FederateEvent_Grant{Grant: &rtiv1.TimeAdvanceGrant{LogicalTime: 1}}})
		return nil
	}}}
	if err := f.FlushQueueRequest(context.Background(), 20); err != nil {
		t.Fatal(err)
	}
	if len(f.timeAdvances) != 0 {
		t.Fatal("RPC acceptance reinstalled an already completed FQR")
	}
	if _, known := f.objectClassFor(80); !known {
		t.Fatal("early grant erased future deletion knowledge")
	}
}

func TestRetractionKnowledgeClearedByRestore(t *testing.T) {
	f := deletionCacheFederate()
	tables, err := tablesFromFOMView(authoritativeView(1))
	if err != nil {
		t.Fatal(err)
	}
	f.handles = tables
	f.conn = &Connection{fed: fomViewClient{get: func(context.Context, *rtiv1.GetFOMViewRequest) (*rtiv1.FOMView, error) {
		return authoritativeView(1), nil
	}}}
	f.translate(deletionCacheRemove(3, 501, rtiv1.OrderType_ORDER_TYPE_TIMESTAMP, rtiv1.OrderType_ORDER_TYPE_RECEIVE))
	if len(f.deletedObjects) != 1 {
		t.Fatal("missing pre-restore retraction history")
	}
	if err := f.applyRestoredState(context.Background(), FederationRestored{Label: "legacy"}); err != nil {
		t.Fatal(err)
	}
	if len(f.deletedObjects) != 0 {
		t.Fatal("restore retained unsaved deletion history")
	}
	f.translate(deletionCacheRetract(3, 501))
	if _, known := f.objectClassFor(80); known {
		t.Fatal("pre-restore callback resurrected stale object")
	}
	f.rememberRegisteredObject(80, 27)
	f.translate(deletionCacheRetract(3, 501))
	if class, _ := f.objectClassFor(80); class != 27 {
		t.Fatal("old designator aliased a restored handle")
	}
}

func TestRetractionKnowledgeClearedByResign(t *testing.T) {
	_, _, f := timeFixture(t)
	f.rememberRegisteredObject(80, 19)
	f.translate(deletionCacheRemove(3, 501, rtiv1.OrderType_ORDER_TYPE_TIMESTAMP, rtiv1.OrderType_ORDER_TYPE_RECEIVE))
	if len(f.deletedObjects) != 1 {
		t.Fatal("missing pre-resign retraction history")
	}
	if err := f.Resign(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(f.deletedObjects) != 0 {
		t.Fatal("resignation retained deletion history")
	}
	f.translate(deletionCacheRetract(3, 501))
	if _, known := f.objectClassFor(80); known {
		t.Fatal("callback after resignation revived object")
	}
}

func TestDeletionAcknowledgmentDoesNotMutateRestoredIdentity(t *testing.T) {
	for _, operation := range []string{"delete", "retract"} {
		t.Run(operation, func(t *testing.T) {
			f := deletionCacheFederate()
			tables, err := tablesFromFOMView(authoritativeView(1))
			if err != nil {
				t.Fatal(err)
			}
			f.handles = tables
			applyRestore := func() {
				handle, epoch := uint64(9), uint64(1)
				if err := f.applyRestoredState(context.Background(), FederationRestored{FederateHandle: &handle, CallbackEpoch: &epoch}); err != nil {
					t.Fatal(err)
				}
				f.rememberRegisteredObject(80, 27)
			}
			f.conn = &Connection{
				fed: fomViewClient{get: func(context.Context, *rtiv1.GetFOMViewRequest) (*rtiv1.FOMView, error) {
					return authoritativeView(1), nil
				}},
				obj: deletionCacheObjectClient{
					onDelete: func(*rtiv1.DeleteObjectInstanceRequest) error {
						if operation == "delete" {
							applyRestore()
						}
						return nil
					},
					onRetract: func(*rtiv1.RetractRequest) error {
						if operation == "retract" {
							applyRestore()
						}
						return nil
					},
				},
			}
			at := 10.0
			if err := f.DeleteObjectInstanceWithOptions(context.Background(), 80, &at, MessageOptions{RetractionHandle: 501}); err != nil {
				t.Fatal(err)
			}
			if operation == "retract" {
				if err := f.Retract(context.Background(), 501); err != nil {
					t.Fatal(err)
				}
			}
			if class, known := f.objectClassFor(80); !known || class != 27 || len(f.deletedObjects) != 0 {
				t.Fatal("old acknowledgment mutated restored object/session")
			}
		})
	}
}
