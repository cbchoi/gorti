package object

import (
	"context"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
	timepkg "github.com/cbchoi/gorti/rti/internal/hla/time"
)

func TestIEEE2010RetractSpeculativeDeletionRestoresOriginalOwnership(t *testing.T) {
	r, tm, out, obj := liveDeletionFixture(t, 2)
	ctx := context.Background()
	own, err := ownership.New(ownership.Options{Outbox: out})
	if err != nil {
		t.Fatal(err)
	}
	own.RegisterInitialOwnership("fed", 1, obj, []core.AttributeHandle{1})
	r.opts.Ownership = own
	r.opts.OnDelete = own.OnObjectDeleted
	timestamp := core.LogicalTime(10)
	if err := r.DeleteWithMetadata(ctx, "fed", 1, obj, &timestamp, core.ObjectMessageOptions{RetractionHandle: 401}); err != nil {
		t.Fatal(err)
	}
	if err := tm.FlushQueueRequest(ctx, "fed", 2, 20); err != nil {
		t.Fatal(err)
	}
	if r.stateFor("fed").instances[obj] == nil || !own.IsOwnedBy("fed", 1, obj, 1) {
		t.Fatal("speculative delivery retired authoritative object/ownership")
	}
	if err := own.UnconditionalDivest(ctx, "fed", 1, obj, []core.AttributeHandle{1}); err != nil {
		t.Fatal(err)
	}
	if err := own.Acquire(ctx, "fed", 2, obj, []core.AttributeHandle{1}, nil); err != nil {
		t.Fatal(err)
	}
	if !own.IsOwnedBy("fed", 2, obj, 1) {
		t.Fatal("positive control: ownership transfer did not occur")
	}
	out.records = nil
	out.reserveErr = errors.New("retraction callback queue full")
	if err := r.RetractWithError(ctx, "fed", 1, 401); !errors.Is(err, out.reserveErr) {
		t.Fatalf("callback failure=%v", err)
	}
	if !own.IsOwnedBy("fed", 2, obj, 1) || len(r.stateFor("fed").deletions) != 1 || len(out.records) != 0 {
		t.Fatal("failed callback admission changed deletion/ownership")
	}
	out.reserveErr = nil
	if err := r.RetractWithError(ctx, "fed", 1, 401); err != nil {
		t.Fatal(err)
	}
	if !own.IsOwnedBy("fed", 1, obj, 1) {
		t.Fatal("retraction did not reassume ownership at original Delete invocation")
	}
	if len(out.records) != 1 {
		t.Fatalf("callbacks=%v", out.records)
	}
	event, ok := out.records[0].Event.(*timepkg.RequestRetraction)
	if !ok || event.RetractionHandle != 401 || out.records[0].Federate != 2 {
		t.Fatalf("callback=%+v", out.records[0])
	}
	if err := tm.TimeAdvanceRequest(ctx, "fed", 1, 20); err != nil {
		t.Fatal(err)
	}
	if r.stateFor("fed").instances[obj] == nil {
		t.Fatal("canceled sender marker later deleted object")
	}
}

func TestIEEE2010RetractionDoesNotRestoreResignedOwner(t *testing.T) {
	r, tm, out, obj := liveDeletionFixture(t, 2)
	ctx := context.Background()
	own, err := ownership.New(ownership.Options{Outbox: out})
	if err != nil {
		t.Fatal(err)
	}
	own.RegisterInitialOwnership("fed", 1, obj, []core.AttributeHandle{1})
	own.RegisterInitialOwnership("fed", 2, obj, []core.AttributeHandle{2})
	r.opts.Ownership = own
	timestamp := core.LogicalTime(10)
	if err := r.DeleteWithMetadata(ctx, "fed", 1, obj, &timestamp, core.ObjectMessageOptions{RetractionHandle: 402}); err != nil {
		t.Fatal(err)
	}
	if err := own.UnconditionalDivest(ctx, "fed", 2, obj, []core.AttributeHandle{2}); err != nil {
		t.Fatal(err)
	}
	r.PrepareResignDeletions(ctx, "fed", 2, false).Commit()
	tm.OnFederateResign(ctx, "fed", 2)
	for _, entry := range r.ExportState("fed").Deletions[0].AttributeOwners {
		if entry.Owner == 2 {
			t.Fatal("snapshot retains resigned owner")
		}
	}
	if err := r.RetractWithError(ctx, "fed", 1, 402); err != nil {
		t.Fatal(err)
	}
	if own.IsOwnedBy("fed", 2, obj, 2) {
		t.Fatal("retraction resurrected resigned owner")
	}
}

func TestIEEE2010RemovalKnowledgeBoundary(t *testing.T) {
	for _, mode := range []string{testReceiveOrder, "flush-disable", "flush-grant", testSenderFirst, testSenderFirstResign} {
		t.Run(mode, func(t *testing.T) {
			r, tm, _, obj := liveDeletionFixture(t, 2)
			ctx := context.Background()
			if mode == testReceiveOrder {
				if err := tm.DisableConstrained(ctx, "fed", 2); err != nil {
					t.Fatal(err)
				}
			}
			timestamp := core.LogicalTime(10)
			if err := r.DeleteWithMetadata(ctx, "fed", 1, obj, &timestamp, core.ObjectMessageOptions{RetractionHandle: 501}); err != nil {
				t.Fatal(err)
			}
			if mode != testReceiveOrder && mode != testSenderFirst && mode != testSenderFirstResign {
				if err := tm.FlushQueueRequest(ctx, "fed", 2, 20); err != nil {
					t.Fatal(err)
				}
				if _, _, known := r.LookupKnownObjectInstance("fed", 2, obj); !known {
					t.Fatal("FQR removal forgot object before its timestamp")
				}
			}
			switch mode {
			case testReceiveOrder:
			case "flush-disable":
				if err := tm.DisableConstrained(ctx, "fed", 2); err != nil {
					t.Fatal(err)
				}
			case "flush-grant":
				if err := tm.TimeAdvanceRequest(ctx, "fed", 1, 9); err != nil {
					t.Fatal(err)
				}
				if err := tm.TimeAdvanceRequestAvailable(ctx, "fed", 2, 10); err != nil {
					t.Fatal(err)
				}
			case testSenderFirst, testSenderFirstResign:
				if err := tm.TimeAdvanceRequest(ctx, "fed", 1, 10); err != nil {
					t.Fatal(err)
				}
				if r.stateFor("fed").instances[obj] != nil {
					t.Fatal("authoritative object not retired")
				}
				if _, _, known := r.LookupKnownObjectInstance("fed", 2, obj); !known {
					t.Fatal("sender advancement prematurely forgot recipient object")
				}
				if err := r.UpdateAttributes(ctx, "fed", 1, obj, map[core.AttributeHandle][]byte{1: {1}}, nil); !errors.Is(err, core.ErrObjectNotFound) {
					t.Fatalf("query-only tombstone admitted update: %v", err)
				}
				if mode == testSenderFirstResign {
					r.PrepareResignDeletions(ctx, "fed", 2, false).Commit()
					tm.OnFederateResign(ctx, "fed", 2)
				} else if err := tm.TimeAdvanceRequest(ctx, "fed", 2, 10); err != nil {
					t.Fatal(err)
				}
			}
			if _, _, known := r.LookupKnownObjectInstance("fed", 2, obj); known {
				t.Fatal("object remains known after applicable removal boundary")
			}
			if (mode == testSenderFirst || mode == testSenderFirstResign) && len(r.ExportState("fed").Deletions) != 0 {
				t.Fatal("fully forgotten terminal object leaked a tombstone")
			}
			if mode == testReceiveOrder || mode == "flush-disable" {
				if err := r.RetractWithError(ctx, "fed", 1, 501); err != nil {
					t.Fatal(err)
				}
				if _, _, known := r.LookupKnownObjectInstance("fed", 2, obj); !known {
					t.Fatal("retraction failed to restore recipient knowledge")
				}
			}
		})
	}
}
