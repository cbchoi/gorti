package ownership_test

import (
	"context"
	"reflect"
	"sync"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

type reservation struct {
	once   sync.Once
	commit func()
}

func (r *reservation) Commit() error { r.once.Do(r.commit); return nil }
func (r *reservation) Release()      { r.once.Do(func() {}) }
func (r *recorder) Reserve(ctx context.Context, fed core.FederationName, deliveries []core.OutboxDelivery) (core.OutboxReservation, error) {
	return &reservation{commit: func() {
		for _, d := range deliveries {
			_ = r.Send(ctx, fed, d.Recipient, d.Event)
		}
	}}, nil
}

func TestReleaseDeniedRPCValidatesWithoutSideEffects(t *testing.T) {
	for _, scenario := range []string{unknownObjectName, "not-discovered", undefinedAttributeName, "not-owner", "not-member"} {
		t.Run(scenario, func(t *testing.T) {
			f := setup(t)
			ctx := context.Background()
			must(t, f.own.Acquire(ctx, "fed", 2, f.obj, []core.AttributeHandle{f.y}, nil))
			req := &rtiv1.ReleaseDeniedRequest{WireVersion: 1, FederationName: "fed", FederateHandle: 1, ObjectHandle: uint64(f.obj), AttributeHandles: []uint64{uint64(f.y)}}
			want := ""
			switch scenario {
			case unknownObjectName:
				req.ObjectHandle = 999
				want = objectNotKnownErrorName
			case "not-discovered":
				req.FederateHandle = 3
				want = objectNotKnownErrorName
			case undefinedAttributeName:
				req.AttributeHandles = append(req.AttributeHandles, 999)
				want = attributeNotDefinedErrorName
			case "not-owner":
				req.AttributeHandles = append(req.AttributeHandles, uint64(f.x))
				want = "AttributeNotOwned"
			case "not-member":
				req.FederateHandle = 77
				want = notExecutionMemberErrorName
			}
			before, callbacks := f.own.ExportState("fed"), f.out.snapshot()
			var trailer metadata.MD
			_, err := f.client.AttributeOwnershipReleaseDenied(ctx, req, grpc.Trailer(&trailer))
			if err == nil || !reflect.DeepEqual(trailer.Get("rti-spec-exception"), []string{want}) {
				t.Fatalf("error=%v trailer=%v want=%s", err, trailer, want)
			}
			if !reflect.DeepEqual(before, f.own.ExportState("fed")) || !reflect.DeepEqual(callbacks, f.out.snapshot()) {
				t.Fatal("rejected denial caused side effects")
			}
		})
	}
}

func TestReleaseDeniedRPCNotifiesAcquirerAndRetainsOwnership(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	must(t, f.own.Acquire(ctx, "fed", 2, f.obj, []core.AttributeHandle{f.y}, nil))
	// The owner need not remain a publisher to reject a pending acquisition.
	must(t, f.decl.UnpublishObjectClassAttributes(ctx, "fed", 1, f.cls, nil))
	before := len(f.out.snapshot())
	_, err := f.client.AttributeOwnershipReleaseDenied(ctx, &rtiv1.ReleaseDeniedRequest{WireVersion: 1, FederationName: "fed", FederateHandle: 1, ObjectHandle: uint64(f.obj), AttributeHandles: []uint64{uint64(f.y)}})
	must(t, err)
	if !f.own.IsOwnedBy("fed", 1, f.obj, f.y) || len(f.own.ExportState("fed").PendingAcquires) != 0 {
		t.Fatal("denial did not retain owner and terminate acquisition")
	}
	callbacks := f.out.snapshot()[before:]
	if len(callbacks) != 1 {
		t.Fatalf("callbacks=%d", len(callbacks))
	}
	e, ok := callbacks[0].event.(*ownership.CallbackEvent)
	if !ok || callbacks[0].to != 2 || e.Kind != ownership.CallbackUnavailable || !reflect.DeepEqual(e.Attributes, []core.AttributeHandle{f.y}) {
		t.Fatalf("callback=%+v", callbacks[0])
	}
}
