package object

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type batchNameRelease interface {
	ReleaseMultipleObjectInstanceNames(context.Context, core.FederationName, core.FederateHandle, []string) error
}

func TestReleaseMultipleNamesAtomicValidation(t *testing.T) {
	for _, bad := range []string{"missing", testForeign, "registered", ""} {
		t.Run(bad, func(t *testing.T) {
			r, _, _ := newTestRegistry(t, nil)
			if _, err := r.reservations.ReserveBatch("fed", 1, []string{"a", "b"}); err != nil {
				t.Fatal(err)
			}
			if _, err := r.reservations.Reserve("fed", 2, testForeign); err != nil {
				t.Fatal(err)
			}
			r.reservations.MarkRegistered("fed", "registered")
			api, ok := any(r).(batchNameRelease)
			if !ok {
				t.Fatal("atomic multiple-name release API missing")
			}
			before := r.ExportState("fed")
			err := api.ReleaseMultipleObjectInstanceNames(context.Background(), "fed", 1, []string{"a", bad, "b"})
			want := core.ErrObjectInstanceNameNotReserved
			if bad == testForeign {
				want = core.ErrObjectInstanceNameReservedByOther
			}
			if !errors.Is(err, want) {
				t.Fatalf("release=%v want%v", err, want)
			}
			if !reflect.DeepEqual(before, r.ExportState("fed")) {
				t.Fatal("failed batch released an earlier valid name")
			}
		})
	}
}

func TestReleaseMultipleNamesSuccessScopeAndConcurrentWinner(t *testing.T) {
	r, _, _ := newTestRegistry(t, nil)
	api, ok := any(r).(batchNameRelease)
	if !ok {
		t.Fatal("atomic multiple-name release API missing")
	}
	if _, err := r.reservations.ReserveBatch("fed", 1, []string{"a", "b"}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.reservations.Reserve(testOther, 1, "a"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.reservations.Reserve("fed", 2, testForeign); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- api.ReleaseMultipleObjectInstanceNames(context.Background(), "fed", 1, []string{"a", "b", "a"})
		}()
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		} else if !errors.Is(err, core.ErrObjectInstanceNameNotReserved) {
			t.Fatal(err)
		}
	}
	if success != 1 {
		t.Fatalf("successful competing releases=%d", success)
	}
	if len(r.ExportState("fed").Reservations) != 1 || len(r.ExportState(testOther).Reservations) != 1 {
		t.Fatal("batch crossed reservation ownership or federation")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := api.ReleaseMultipleObjectInstanceNames(ctx, "fed", 2, []string{testForeign}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled release=%v", err)
	}
	if err := api.ReleaseMultipleObjectInstanceNames(context.Background(), "fed", 0, nil); !errors.Is(err, core.ErrFederateNotJoined) {
		t.Fatalf("zero holder=%v", err)
	}
}
