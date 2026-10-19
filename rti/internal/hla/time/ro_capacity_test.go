package time

import (
	"context"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type roBoundedOutbox struct {
	capacity int
	queue    []core.OutboundEvent
	observed []core.OutboundEvent
}

func (o *roBoundedOutbox) Send(ctx context.Context, fed core.FederationName, h core.FederateHandle, e core.OutboundEvent) error {
	r, err := o.Reserve(ctx, fed, []core.OutboxDelivery{{Recipient: h, Event: e}})
	if err != nil {
		return err
	}
	defer r.Release()
	return r.Commit()
}

func (o *roBoundedOutbox) Reserve(_ context.Context, _ core.FederationName, ds []core.OutboxDelivery) (core.OutboxReservation, error) {
	if r, ok := enableFixtureAdmission(ds); ok {
		return r, nil
	}
	if len(o.queue)+len(ds) > o.capacity {
		return nil, core.ErrFederateOverflow
	}
	return &ieee2010ReviewReservation{commit: func() error {
		for _, d := range ds {
			o.queue = append(o.queue, d.Event)
			o.observed = append(o.observed, d.Event)
		}
		return nil
	}}, nil
}

const (
	roOperationAsync      = "async"
	roOperationConstraint = "constraint"
	roOperationGrant      = "grant"
)

func TestROBacklogLargerThanTransportCapacityRecovers(t *testing.T) {
	for _, operation := range []string{roOperationAsync, roOperationConstraint, roOperationGrant} {
		t.Run(operation, func(t *testing.T) {
			ctx := context.Background()
			out := &roBoundedOutbox{capacity: 2}
			m := newROTestManager(t, out)
			if err := m.EnableConstrained(ctx, "fed", 1); err != nil {
				t.Fatal(err)
			}
			for seq := uint64(1); seq <= 5; seq++ {
				enqueueROTest(t, m, seq)
			}
			var err error
			switch operation {
			case roOperationAsync:
				err = m.EnableAsynchronousDelivery(ctx, "fed", 1)
			case roOperationConstraint:
				err = m.DisableConstrained(ctx, "fed", 1)
			case roOperationGrant:
				err = m.TimeAdvanceRequest(ctx, "fed", 1, 10)
			}
			if operation == roOperationGrant {
				if !errors.Is(err, core.ErrFederateOverflow) {
					t.Fatalf("grant pressure=%v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if len(out.observed) != 2 || m.Metrics("fed", 1).ROQueued != 3 || len(m.PendingDeliveryRetries()) != 1 {
				t.Fatalf("no bounded progress: observed=%d state=%+v retries=%v", len(out.observed), m.Metrics("fed", 1), m.PendingDeliveryRetries())
			}
			for i := 0; i < 4 && len(m.PendingDeliveryRetries()) > 0; i++ {
				out.queue = nil
				err = m.RetryPendingDeliveries(ctx, "fed")
				if err != nil && !errors.Is(err, core.ErrFederateOverflow) {
					t.Fatal(err)
				}
			}
			want := 5
			if operation == roOperationGrant {
				want++
			}
			if len(out.observed) != want || len(m.PendingDeliveryRetries()) != 0 || m.Metrics("fed", 1).ROQueued != 0 {
				t.Fatalf("retry incomplete: observed=%d state=%+v retries=%v", len(out.observed), m.Metrics("fed", 1), m.PendingDeliveryRetries())
			}
			for i := 0; i < 5; i++ {
				if out.observed[i].Seq() != uint64(i+1) {
					t.Fatalf("duplicate/reordered event %d: %v", i, out.observed[i])
				}
			}
			if operation == roOperationGrant {
				if _, ok := out.observed[5].(*TimeAdvanceGrant); !ok {
					t.Fatalf("last=%T", out.observed[5])
				}
			}
		})
	}
}

func TestRODeliveryRetryHintsDoNotSurviveGenerationOrImport(t *testing.T) {
	m := newROTestManager(t, &roBoundedOutbox{capacity: 1})
	m.markDeliveryRetry("fed")
	m.OnFederationDestroyed("fed")
	if len(m.PendingDeliveryRetries()) != 0 {
		t.Fatal("destroy retained retry hint")
	}
	m.markDeliveryRetry("fed")
	if err := m.ImportState("fed", nil, nil); err != nil {
		t.Fatal(err)
	}
	if len(m.PendingDeliveryRetries()) != 0 {
		t.Fatal("import retained obsolete retry hint")
	}
}

type roGrantJournal struct {
	recordingGrantLog
	fail bool
}

func (l *roGrantJournal) Append(ctx context.Context, fed core.FederationName, e core.EventRecord) error {
	if l.fail {
		return errIEEE2010ReviewDelivery
	}
	return l.recordingGrantLog.Append(ctx, fed, e)
}

func TestROGrantJournalFailureRetainsQueueForOwnedRetry(t *testing.T) {
	ctx := context.Background()
	out := &roBoundedOutbox{capacity: 4}
	log := &roGrantJournal{fail: true}
	m, err := New(Options{Outbox: out, EventLog: log, Clock: core.NewManualClock(zeroTime())})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.EnableConstrained(ctx, "fed", 1); err != nil {
		t.Fatal(err)
	}
	enqueueROTest(t, m, 1)
	if err := m.TimeAdvanceRequest(ctx, "fed", 1, 10); !errors.Is(err, errIEEE2010ReviewDelivery) {
		t.Fatalf("grant error=%v", err)
	}
	if len(out.observed) != 0 || m.Metrics("fed", 1).ROQueued != 1 || !m.Metrics("fed", 1).Advancing || len(m.PendingDeliveryRetries()) != 1 {
		t.Fatal("journal failure lost retry state")
	}
	log.fail = false
	if err := m.RetryPendingDeliveries(ctx, "fed"); err != nil {
		t.Fatal(err)
	}
	if len(out.observed) != 2 || out.observed[0].Seq() != 1 {
		t.Fatalf("retry callbacks=%v", out.observed)
	}
	if _, ok := out.observed[1].(*TimeAdvanceGrant); !ok {
		t.Fatalf("last event=%T", out.observed[1])
	}
	if len(log.snapshot()) != 1 || len(m.PendingDeliveryRetries()) != 0 {
		t.Fatal("retry duplicated journal or retained hint")
	}
}
