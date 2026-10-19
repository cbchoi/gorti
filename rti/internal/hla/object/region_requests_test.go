package object

import (
	"context"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestUpdateRequestsPreserveDeliveryErrors(t *testing.T) {
	reg, decl, _ := newTestRegistry(t, nil)
	ctx := context.Background()
	if err := decl.SubscribeObjectClassAttributes(ctx, "fed", 3, 1, []core.AttributeHandle{1}); err != nil {
		t.Fatal(err)
	}
	var first core.ObjectHandle
	for _, owner := range []core.FederateHandle{1, 2} {
		if err := decl.PublishObjectClassAttributes(ctx, "fed", owner, 1, []core.AttributeHandle{1}); err != nil {
			t.Fatal(err)
		}
		obj, _, err := reg.Register(ctx, "fed", owner, 1, "")
		if err != nil {
			t.Fatal(err)
		}
		if first == 0 {
			first = obj
		}
	}
	failure := errors.New("outbox unavailable")
	out := &failingUpdateOutbox{err: failure}
	reg.opts.Outbox = out
	if err := reg.RequestClassAttributeValueUpdate(ctx, "fed", 3, 1, []core.AttributeHandle{1}, []byte{0, 255}); !errors.Is(err, failure) {
		t.Fatalf("class request hid delivery failure: %v", err)
	}
	if out.sendCalls != 2 {
		t.Fatalf("class request attempted %d owners, want 2", out.sendCalls)
	}
	if err := reg.RequestAttributeValueUpdate(ctx, "fed", 3, first, []core.AttributeHandle{1}, nil); !errors.Is(err, failure) {
		t.Fatalf("instance request hid delivery failure: %v", err)
	}
	before := out.sendCalls
	if err := reg.RequestAttributeValueUpdate(ctx, "fed", 3, first, []core.AttributeHandle{1, 0}, nil); !errors.Is(err, core.ErrAttributeNotFound) || out.sendCalls != before {
		t.Fatalf("invalid request had effects: error=%v sends=%d", err, out.sendCalls-before)
	}
}
