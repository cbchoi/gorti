package object

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/adapters/delivery"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type discoveryLockOutbox struct {
	*delivery.MultiOutbox
	reserves      atomic.Int32
	secondReserve chan struct{}
}

func (o *discoveryLockOutbox) Reserve(ctx context.Context, fed core.FederationName, deliveries []core.OutboxDelivery) (core.OutboxReservation, error) {
	if o.reserves.Add(1) == 2 {
		close(o.secondReserve)
	}
	return o.MultiOutbox.Reserve(ctx, fed, deliveries)
}

type discoveryLockDDM struct {
	updateDDMFilter
	regionalObject core.ObjectHandle
}

func (d *discoveryLockDDM) HasObjectAssociations(_ core.FederationName, obj core.ObjectHandle) bool {
	return obj == d.regionalObject
}

func TestConcurrentRediscoveryAndScopedFanoutLockOrder(t *testing.T) {
	ctx := context.Background()
	out := &discoveryLockOutbox{MultiOutbox: delivery.NewWithBatch(64, 1, 0), secondReserve: make(chan struct{})}
	out.Bind("fed", 2)
	reg, decl, _ := newUpdateDeliveryRegistry(t, out)
	enableScopeAdvisories(t, decl, 2)
	ordinary := registerUpdateProducer(t, reg, decl)
	regional := registerUpdateProducer(t, reg, decl, 2)
	if err := decl.SubscribeObjectClassAttributes(ctx, "fed", 2, 7, []core.AttributeHandle{2}); err != nil {
		t.Fatal(err)
	}
	reg.opts.DDM = &discoveryLockDDM{
		updateDDMFilter: updateDDMFilter{subs: map[core.AttributeHandle][]core.FederateHandle{2: {2}}},
		regionalObject:  regional,
	}
	st := reg.stateFor("fed")
	first, err := reg.prepareFanoutReflect(ctx, "fed", st, 1, st.instances[ordinary], map[uint64][]byte{2: {1}}, []core.AttributeHandle{2}, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.discoveries) != 1 || first.scope == nil || len(first.scope.newlyIn[2]) != 1 {
		first.abort()
		t.Fatal("fixture must hold an ordinary rediscovery reservation")
	}
	secondDone := make(chan error, 1)
	go func() {
		second, err := reg.prepareFanoutReflect(ctx, "fed", st, 1, st.instances[regional], map[uint64][]byte{2: {2}}, []core.AttributeHandle{2}, nil, 0)
		if err == nil {
			defer second.abort()
			err = second.commit(ctx)
		}
		secondDone <- err
	}()
	select {
	case <-out.secondReserve:
	case <-time.After(time.Second):
		t.Fatal("scoped update did not reach its reservation")
	}
	// The first reservation owns the recipient lock. Scoped preparation must
	// release federation state before waiting for that same recipient.
	firstDone := make(chan error, 1)
	go func() {
		defer first.abort()
		firstDone <- first.commit(ctx)
	}()
	for _, done := range []<-chan error{firstDone, secondDone} {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(time.Second):
			t.Fatal("ordinary discovery commit and scoped preparation deadlocked on state/recipient lock order")
		}
	}
}
