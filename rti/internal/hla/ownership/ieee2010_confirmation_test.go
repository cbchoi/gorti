package ownership

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// IEEE 1516.1-2010 7.6.3(g-h), 7.6.5(a-b), supplied PDF pp. 165-166.
// Each rejection isolates one condition; no exception precedence is assumed.
func TestIEEE2010ConfirmationNotRequestedRollback(t *testing.T) {
	for _, attrs := range [][]core.AttributeHandle{{2}, {1, 2}, {2, 1}} {
		for _, cancelled := range []bool{false, true} {
			t.Run(fmtConfirmationCase(attrs, cancelled), func(t *testing.T) {
				ctx := context.Background()
				journal := &permissiveLog{}
				m, out := newMgr(t, Options{EventLog: journal})
				m.RegisterInitialOwnership("fed", 1, 100, []core.AttributeHandle{1, 2})
				mustConfirmOK(t, m.Acquire(ctx, "fed", 2, 100, []core.AttributeHandle{1, 2}, []byte("acquire")))
				mustConfirmOK(t, m.NegotiatedDivestTwoPhase(ctx, "fed", 1, 100, []core.AttributeHandle{1}, nil))
				if cancelled {
					mustConfirmOK(t, m.NegotiatedDivestTwoPhase(ctx, "fed", 1, 100, []core.AttributeHandle{2}, nil))
					mustConfirmOK(t, m.CancelDivest(ctx, "fed", 1, 100, []core.AttributeHandle{2}))
				}
				before, callbacks, logs := m.ExportState("fed"), len(out.Sent()), len(journal.records)
				changed := 0
				m.SetOnOwnershipChanged(func(context.Context, core.FederationName, core.ObjectHandle) { changed++ })
				err := m.ConfirmDivestitureWithTag(ctx, "fed", 1, 100, attrs, []byte("rejected"))
				if !errors.Is(err, core.ErrOwnershipConfirmationNotRequested) {
					t.Errorf("confirm = %v, want confirmation not requested", err)
				}
				if !reflect.DeepEqual(before, m.ExportState("fed")) || len(out.Sent()) != callbacks || len(journal.records) != logs || changed != 0 {
					t.Fatal("rejected confirmation changed ownership, requests or observers")
				}
				mustConfirmOK(t, m.NegotiatedDivestTwoPhase(ctx, "fed", 1, 100, []core.AttributeHandle{2}, nil))
				mustConfirmOK(t, m.ConfirmDivestiture(ctx, "fed", 1, 100, []core.AttributeHandle{1, 2}))
			})
		}
	}
}

func TestIEEE2010ConfirmationAcquirerWithdrawnRollbackAndRecovery(t *testing.T) {
	for _, attrs := range [][]core.AttributeHandle{{2}, {1, 2}, {2, 1}} {
		t.Run(fmtConfirmationCase(attrs, false), func(t *testing.T) {
			ctx := context.Background()
			journal := &permissiveLog{}
			m, out := newMgr(t, Options{EventLog: journal})
			m.RegisterInitialOwnership("fed", 1, 100, []core.AttributeHandle{1, 2})
			mustConfirmOK(t, m.NegotiatedDivestTwoPhase(ctx, "fed", 1, 100, []core.AttributeHandle{1, 2}, []byte("offer")))
			mustConfirmOK(t, m.Acquire(ctx, "fed", 2, 100, []core.AttributeHandle{1, 2}, []byte("original")))
			mustConfirmOK(t, m.CancelAcquire(ctx, "fed", 2, 100, []core.AttributeHandle{2}))
			before, callbacks, logs := m.ExportState("fed"), len(out.Sent()), len(journal.records)
			changed := 0
			m.SetOnOwnershipChanged(func(context.Context, core.FederationName, core.ObjectHandle) { changed++ })
			err := m.ConfirmDivestiture(ctx, "fed", 1, 100, attrs)
			if !errors.Is(err, core.ErrOwnershipNoAcquisitionPending) {
				t.Errorf("confirm = %v, want no acquisition pending", err)
			}
			after := m.ExportState("fed")
			if !reflect.DeepEqual(before.Owners, after.Owners) || !reflect.DeepEqual(before.PendingAcquires, after.PendingAcquires) || len(after.PendingDivests) != 2 || len(out.Sent()) != callbacks || len(journal.records) != logs || changed != 0 {
				t.Fatal("failed confirmation transferred ownership, lost requests or notified observers")
			}
			if !before.PendingDivests[0].ConfirmationRequested || !before.PendingDivests[1].ConfirmationRequested {
				t.Fatal("prior confirmation requests were not recorded")
			}
			before.PendingDivests[1].ConfirmationRequested = false
			if !reflect.DeepEqual(before, after) {
				t.Fatal("only the withdrawn attribute should return to waiting")
			}
			// The pending offer survives the specified transition back to waiting.
			mustConfirmOK(t, m.Acquire(ctx, "fed", 3, 100, []core.AttributeHandle{2}, []byte("replacement")))
			e := out.Sent()[len(out.Sent())-1].Event.(*CallbackEvent)
			if e.Kind != CallbackDivestitureConfirmation || !reflect.DeepEqual(e.Attributes, []core.AttributeHandle{2}) {
				t.Fatalf("replacement acquisition did not request confirmation: %+v", e)
			}
			tag := []byte{0, 0x80, 0xff}
			mustConfirmOK(t, m.ConfirmDivestitureWithTag(ctx, "fed", 1, 100, []core.AttributeHandle{1, 2}, tag))
			for attr, want := range map[core.AttributeHandle]core.FederateHandle{1: 2, 2: 3} {
				if got, _ := m.QueryOwnership("fed", 100, attr); got != want {
					t.Fatalf("attribute %d owner=%d, want %d", attr, got, want)
				}
			}
			for _, sent := range out.Sent()[len(out.Sent())-2:] {
				e := sent.Event.(*CallbackEvent)
				if e.Kind != CallbackAcquisitionNotification || !bytes.Equal(e.Tag, tag) {
					t.Fatalf("confirmation callback=%+v", e)
				}
			}
		})
	}
}

func TestIEEE2010ConfirmationStateRoundTrip(t *testing.T) {
	ctx := context.Background()
	for _, acquireFirst := range []bool{false, true} {
		m, _ := newMgr(t, Options{})
		attrs := []core.AttributeHandle{1}
		m.RegisterInitialOwnership("fed", 1, 100, attrs)
		if acquireFirst {
			mustConfirmOK(t, m.Acquire(ctx, "fed", 2, 100, attrs, nil))
		}
		mustConfirmOK(t, m.NegotiatedDivestTwoPhase(ctx, "fed", 1, 100, attrs, nil))
		if !acquireFirst {
			mustConfirmOK(t, m.Acquire(ctx, "fed", 2, 100, attrs, nil))
		}
		restored, _ := newMgr(t, Options{})
		restored.ImportState("fed", m.ExportState("fed"))
		mustConfirmOK(t, restored.ConfirmDivestiture(ctx, "fed", 1, 100, attrs))
		mustConfirmOK(t, m.CancelAcquire(ctx, "fed", 2, 100, attrs))
		restored.ImportState("fed", m.ExportState("fed"))
		if err := restored.ConfirmDivestiture(ctx, "fed", 1, 100, attrs); !errors.Is(err, core.ErrOwnershipNoAcquisitionPending) {
			t.Fatalf("restored prior request lost: %v", err)
		}
	}
}

type confirmationDeferredStrategy struct{ defaultNegotiation }

func (confirmationDeferredStrategy) SelectAcquirer(SelectAcquirerContext) core.FederateHandle {
	return core.InvalidFederateHandle
}

func TestIEEE2010ConfirmationPendingOfferIsNotPriorRequest(t *testing.T) {
	ctx := context.Background()
	for _, twoPhase := range []bool{false, true} {
		m, out := newMgr(t, Options{Strategy: confirmationDeferredStrategy{}})
		attrs := []core.AttributeHandle{1}
		m.RegisterInitialOwnership("fed", 1, 100, attrs)
		mustConfirmOK(t, m.Acquire(ctx, "fed", 2, 100, attrs, nil))
		mustConfirmOK(t, m.negotiatedDivest(ctx, "fed", 1, 100, attrs, nil, twoPhase, canonicalPreparation(m.prepareDivestiture)))
		// A willing acquirer exists, but the policy has not requested confirmation.
		m.opts.Strategy = defaultNegotiation{}
		before, callbacks := m.ExportState("fed"), len(out.Sent())
		if err := m.ConfirmDivestiture(ctx, "fed", 1, 100, attrs); !errors.Is(err, core.ErrOwnershipConfirmationNotRequested) {
			t.Errorf("pending offer without confirmation request: %v", err)
		}
		if !reflect.DeepEqual(before, m.ExportState("fed")) || len(out.Sent()) != callbacks {
			t.Fatal("unrequested confirmation changed pending offer")
		}
	}
}

func TestIEEE2010ConfirmationAllWithdrawnReturnToWaiting(t *testing.T) {
	ctx := context.Background()
	m, _ := newMgr(t, Options{})
	attrs := []core.AttributeHandle{1, 2}
	m.RegisterInitialOwnership("fed", 1, 100, attrs)
	mustConfirmOK(t, m.Acquire(ctx, "fed", 2, 100, attrs, nil))
	mustConfirmOK(t, m.NegotiatedDivestTwoPhase(ctx, "fed", 1, 100, attrs, nil))
	mustConfirmOK(t, m.CancelAcquire(ctx, "fed", 2, 100, attrs))
	if err := m.ConfirmDivestiture(ctx, "fed", 1, 100, attrs); !errors.Is(err, core.ErrOwnershipNoAcquisitionPending) {
		t.Fatal(err)
	}
	for _, pd := range m.ExportState("fed").PendingDivests {
		if pd.ConfirmationRequested {
			t.Fatalf("attribute %d did not return to waiting", pd.Attribute)
		}
	}
}

func mustConfirmOK(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func fmtConfirmationCase(attrs []core.AttributeHandle, cancelled bool) string {
	name := "single"
	if len(attrs) > 1 {
		name = "invalid-last"
		if attrs[0] == 2 {
			name = "invalid-first"
		}
	}
	if cancelled {
		name += "/cancelled-offer"
	}
	return name
}
