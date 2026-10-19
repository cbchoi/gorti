package delivery

import (
	"context"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	savepoint "github.com/cbchoi/gorti/rti/internal/hla/save_restore"
)

func TestRestoreEpochFloorIsFreshAndDetached(t *testing.T) {
	m := NewWithBatch(2, 1, 0)
	ch, closeStream, err := m.Subscribe(context.Background(), "f", 7)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestSubscription(t, closeStream)
	deliveries := []RestoreDelivery{{CurrentHandle: 7, SavedHandle: 3, Event: &savepoint.RestoredEvent{}}}
	reservation, epochs, err := m.PrepareRestoreWithEpochFloor(context.Background(), "f", deliveries, map[core.FederateHandle]uint64{7: 19})
	if err != nil || epochs[7] != 20 {
		t.Fatalf("saved epoch floor: %v %v", epochs, err)
	}
	epochs[7] = 1
	if err := reservation.Commit(); err != nil {
		t.Fatal(err)
	}
	<-ch
	if actual, err := m.CallbackEpoch("f", 3); err != nil || actual != 20 {
		t.Fatalf("caller altered reserved epoch: %d %v", actual, err)
	}
	reservation, epochs, err = m.PrepareRestoreWithEpochFloor(context.Background(), "f", []RestoreDelivery{{CurrentHandle: 3, SavedHandle: 3, Event: &savepoint.RestoredEvent{}}}, map[core.FederateHandle]uint64{3: 1})
	if err != nil || epochs[3] != 21 {
		t.Fatalf("live epoch floor: %v %v", epochs, err)
	}
	reservation.Release()
	if actual, _ := m.CallbackEpoch("f", 3); actual != 20 {
		t.Fatalf("release consumed epoch: %d", actual)
	}
}

func TestRestoreEpochFloorRejectsOverflowAndUnknownRecipient(t *testing.T) {
	for name, floors := range map[string]map[core.FederateHandle]uint64{"exhausted": {1: ^uint64(0)}, "unknown": {2: 1}} {
		t.Run(name, func(t *testing.T) {
			m := NewWithBatch(2, 1, 0)
			m.Bind("f", 1)
			r, _, err := m.PrepareRestoreWithEpochFloor(context.Background(), "f", []RestoreDelivery{{CurrentHandle: 1, SavedHandle: 3, Event: &savepoint.RestoredEvent{}}}, floors)
			if err == nil {
				r.Release()
				t.Fatal("invalid floor accepted")
			}
			if epoch, err := m.CallbackEpoch("f", 1); err != nil || epoch != 0 {
				t.Fatalf("failed preparation changed recipient: %d %v", epoch, err)
			}
		})
	}
}
