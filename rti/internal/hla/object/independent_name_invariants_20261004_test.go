package object

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestIndependentNameReviewRegistrationPersistenceOrdersRelease(t *testing.T) {
	for _, successful := range []bool{false, true} {
		mode := "failed-write"
		if successful {
			mode = "committed-write"
		}
		t.Run(mode, func(t *testing.T) {
			s := newReservationStore()
			if ok, err := s.Reserve("fed", 1, "reserved"); !ok || err != nil {
				t.Fatal(ok, err)
			}
			entered, releasePersist := make(chan struct{}), make(chan struct{})
			registered, released := make(chan error, 1), make(chan error, 1)
			go func() {
				registered <- s.register("fed", 1, "reserved", true, func() error {
					close(entered)
					<-releasePersist
					if !successful {
						return io.ErrClosedPipe
					}
					return nil
				})
			}()
			select {
			case <-entered:
			case <-time.After(time.Second):
				close(releasePersist)
				t.Fatal("registration did not reach persistence")
			}
			go func() { released <- s.Release("fed", 1, "reserved") }()
			select {
			case err := <-released:
				close(releasePersist)
				<-registered
				t.Fatalf("release crossed unfinished registration transaction: %v", err)
			case <-time.After(20 * time.Millisecond):
			}
			close(releasePersist)
			select {
			case err := <-registered:
				if successful && err != nil || !successful && !errors.Is(err, io.ErrClosedPipe) {
					t.Fatalf("persistence outcome: %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("registration did not finish")
			}
			select {
			case err := <-released:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("release did not finish")
			}
			s.mu.Lock()
			_, hasReservation := s.reservations["fed"]["reserved"]
			_, hasRegistration := s.registered["fed"]["reserved"]
			s.mu.Unlock()
			if hasReservation || hasRegistration != successful {
				t.Fatalf("ordered final state: reservation=%t registration=%t success=%t", hasReservation, hasRegistration, successful)
			}
		})
	}
}

func TestIndependentNameReviewWrongHolderDoesNotPersistAndMOMNamesRemainInternal(t *testing.T) {
	s := newReservationStore()
	if ok, err := s.Reserve("fed", 1, "reserved"); !ok || err != nil {
		t.Fatal(ok, err)
	}
	writes := 0
	if err := s.register("fed", 2, "reserved", true, func() error { writes++; return nil }); !errors.Is(err, core.ErrObjectInstanceNameNotReserved) || writes != 0 {
		t.Fatalf("wrong holder reached persistence: %v writes=%d", err, writes)
	}
	r, declarations, _ := newTestRegistry(t, nil)
	ctx := context.Background()
	for _, holder := range []core.FederateHandle{1, internalProducer} {
		if err := declarations.PublishObjectClassAttributes(ctx, "fed", holder, 1, []core.AttributeHandle{1}); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := r.Register(ctx, "fed", 1, 1, "HLAmanager-federation"); !errors.Is(err, core.ErrObjectInstanceNameNotReserved) {
		t.Fatalf("public named registration bypassed reservation: %v", err)
	}
	if _, name, err := r.Register(ctx, "fed", internalProducer, 1, "HLAmanager-federation"); err != nil || name != "HLAmanager-federation" {
		t.Fatalf("RTI-owned named object rejected: %s %v", name, err)
	}
	if _, name, err := r.Register(ctx, "fed", 1, 1, ""); err != nil || name == "" {
		t.Fatalf("unnamed user object rejected: %s %v", name, err)
	}
}
