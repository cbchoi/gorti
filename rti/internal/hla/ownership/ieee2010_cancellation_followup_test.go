package ownership

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestIEEE2010CancellationValidatesWholeSet(t *testing.T) {
	for _, acquire := range []bool{false, true} {
		for _, reverse := range []bool{false, true} {
			for _, problem := range []string{"not-requested", "ownership", "undefined", "unknown-object"} {
				t.Run(fmtCancellationCase(acquire, reverse, problem), func(t *testing.T) {
					ctx := context.Background()
					journal := &permissiveLog{}
					m, out := newMgr(t, Options{EventLog: journal})
					m.RegisterInitialOwnership("fed", 1, 10, []core.AttributeHandle{1, 2})
					if err := m.Acquire(ctx, "fed", 2, 10, []core.AttributeHandle{1}, nil); err != nil {
						t.Fatal(err)
					}
					if err := m.NegotiatedDivestTwoPhase(ctx, "fed", 1, 10, []core.AttributeHandle{1}, nil); err != nil {
						t.Fatal(err)
					}
					caller := core.FederateHandle(1)
					call := m.CancelDivest
					if acquire {
						caller, call = 2, m.CancelAcquire
					}
					want := core.ErrOwnershipNotInTransfer
					var preparationError error
					switch problem {
					case "ownership":
						m.RegisterInitialOwnership("fed", 2, 10, []core.AttributeHandle{2})
						want = core.ErrAttributeNotOwned
						if acquire {
							want = core.ErrFederateOwnsAttributes
						}
					case "undefined":
						want, preparationError = core.ErrAttributeNotFound, core.ErrAttributeNotFound
					case "unknown-object":
						want, preparationError = core.ErrObjectHandleInvalid, core.ErrObjectHandleInvalid
					}
					prepared, released := 0, 0
					m.opts.PrepareDivestiture = func(context.Context, core.FederationName, core.FederateHandle, core.ObjectHandle, []core.AttributeHandle) (func(), error) {
						prepared++
						if preparationError != nil {
							return nil, preparationError
						}
						return func() { released++ }, nil
					}
					attrs := []core.AttributeHandle{1, 2}
					if reverse {
						attrs = []core.AttributeHandle{2, 1}
					}
					before, callbacks, logs := m.ExportState("fed"), len(out.Sent()), len(journal.records)
					if err := call(ctx, "fed", caller, 10, attrs); !errors.Is(err, want) {
						t.Errorf("error=%v, want %v", err, want)
					}
					if !reflect.DeepEqual(before, m.ExportState("fed")) || len(out.Sent()) != callbacks || len(journal.records) != logs {
						t.Error("rejected mixed set changed ownership, requests or callbacks")
					}
					wantReleased := 1
					if preparationError != nil {
						wantReleased = 0
					}
					if prepared != 1 || released != wantReleased {
						t.Errorf("prepared/released=%d/%d, want 1/%d", prepared, released, wantReleased)
					}
				})
			}
		}
	}
}

func fmtCancellationCase(acquire, reverse bool, problem string) string {
	name := "divest/"
	if acquire {
		name = "acquire/"
	}
	if reverse {
		name += "invalid-first/"
	}
	return name + problem
}

func TestIEEE2010CancellationReleasesObjectBeforeJournal(t *testing.T) {
	for _, acquire := range []bool{false, true} {
		ctx := context.Background()
		m, out := newMgr(t, Options{})
		m.RegisterInitialOwnership("fed", 1, 10, []core.AttributeHandle{1})
		if err := m.Acquire(ctx, "fed", 2, 10, []core.AttributeHandle{1}, nil); err != nil {
			t.Fatal(err)
		}
		if err := m.NegotiatedDivestTwoPhase(ctx, "fed", 1, 10, []core.AttributeHandle{1}, nil); err != nil {
			t.Fatal(err)
		}
		pinned, released, observed := false, 0, 0
		m.opts.PrepareDivestiture = func(context.Context, core.FederationName, core.FederateHandle, core.ObjectHandle, []core.AttributeHandle) (func(), error) {
			pinned = true
			return func() { pinned = false; released++ }, nil
		}
		m.opts.EventLog = &divestObserverLog{observe: func() {
			observed++
			if pinned {
				t.Error("cancellation journal invoked with object pinned")
			}
			state := m.Snapshot("fed")
			if (acquire && state.PendingAcquiresCount != 0) || (!acquire && state.PendingDivestsCount != 0) {
				t.Error("cancellation journal observed uncommitted state")
			}
		}}
		caller, call := core.FederateHandle(1), m.CancelDivest
		if acquire {
			caller, call = 2, m.CancelAcquire
		}
		callbacks := len(out.Sent())
		if err := call(ctx, "fed", caller, 10, []core.AttributeHandle{1}); err != nil {
			t.Fatal(err)
		}
		if pinned || released != 1 || observed != 1 || len(out.Sent()) != callbacks {
			t.Fatalf("pin/release/journal/callbacks=%v/%d/%d/%d", pinned, released, observed, len(out.Sent())-callbacks)
		}
	}
}

func TestIEEE2010DivestIfWantedNeverRequestsConfirmationAfterTransfer(t *testing.T) {
	ctx := context.Background()
	m, out := newMgr(t, Options{})
	m.RegisterInitialOwnership("fed", 1, 10, []core.AttributeHandle{1, 2})
	if err := m.Acquire(ctx, "fed", 2, 10, []core.AttributeHandle{1}, []byte("acquire")); err != nil {
		t.Fatal(err)
	}
	before := len(out.Sent())
	got, err := m.DivestIfWantedWithResult(ctx, "fed", 1, 10, []core.AttributeHandle{1, 2})
	if err != nil || !reflect.DeepEqual(got, []core.AttributeHandle{1}) {
		t.Fatalf("result=%v, error=%v", got, err)
	}
	sent := out.Sent()[before:]
	if len(sent) != 1 || sent[0].Federate != 2 || sent[0].Event.(*CallbackEvent).Kind != CallbackAcquisitionNotification {
		t.Fatalf("completed transfer must only notify the acquirer: %+v", sent)
	}
	if !m.IsOwnedBy("fed", 2, 10, 1) || !m.IsOwnedBy("fed", 1, 10, 2) {
		t.Fatal("incorrect committed subset")
	}
}

func TestIEEE2010RTIOwnedAttributesAreNeverQueuedForTransfer(t *testing.T) {
	for _, available := range []bool{false, true} {
		m, out := newMgr(t, Options{})
		rtiOwner := ^core.FederateHandle(0)
		m.RegisterInitialOwnership("fed", rtiOwner, 10, []core.AttributeHandle{1})
		call := m.Acquire
		if available {
			call = m.AcquireIfAvailable
		}
		// An extension attribute on the same MOM object remains acquirable.
		if err := call(context.Background(), "fed", 2, 10, []core.AttributeHandle{1, 2}, nil); err != nil {
			t.Fatal(err)
		}
		if owner, owned := m.QueryOwnership("fed", 10, 1); !owned || owner != rtiOwner {
			t.Fatalf("RTI ownership changed: %d/%v", owner, owned)
		}
		if !m.IsOwnedBy("fed", 2, 10, 2) || m.Snapshot("fed").PendingAcquiresCount != 0 {
			t.Fatal("extension was not acquired or RTI acquisition was left pending")
		}
		var unavailable, acquired int
		for _, sent := range out.Sent() {
			event := sent.Event.(*CallbackEvent)
			if sent.Federate != 2 {
				t.Fatalf("request sent to internal RTI owner: %+v", sent)
			}
			switch event.Kind {
			case CallbackUnavailable:
				unavailable++
				if !reflect.DeepEqual(event.Attributes, []core.AttributeHandle{1}) {
					t.Fatal(event)
				}
			case CallbackAcquisitionNotification:
				acquired++
				if !reflect.DeepEqual(event.Attributes, []core.AttributeHandle{2}) {
					t.Fatal(event)
				}
			default:
				t.Fatalf("unexpected callback: %+v", event)
			}
		}
		if unavailable != 1 || acquired != 1 {
			t.Fatalf("unavailable/acquired=%d/%d", unavailable, acquired)
		}
	}
}
