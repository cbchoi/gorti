package savepoint

import (
	"context"
	"errors"
	"io"
	"reflect"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type applicableRestoreFixture struct {
	m      *Manager
	out    *transitionOutbox
	state  *leasedTransitionSnapshot
	store  *memStore
	roster []core.FederationMember
}

func newApplicableRestoreFixture(t *testing.T) *applicableRestoreFixture {
	t.Helper()
	f := &applicableRestoreFixture{
		out: &transitionOutbox{}, store: newMemStore(),
		state:  &leasedTransitionSnapshot{transitionSnapshot: transitionSnapshot{live: testSaved}},
		roster: []core.FederationMember{{Handle: 1, Name: "one", FederateType: "producer"}, {Handle: 2, Name: "two", FederateType: "consumer"}},
	}
	var err error
	f.m, err = New(Options{Outbox: f.out, BundleStore: f.store,
		Members: func(core.FederationName) []core.FederateHandle {
			handles := make([]core.FederateHandle, len(f.roster))
			for i, member := range f.roster {
				handles[i] = member.Handle
			}
			return handles
		},
		Roster: func(core.FederationName) []core.FederationMember {
			return append([]core.FederationMember(nil), f.roster...)
		},
		RequiredSnapshots: []string{"state"}, ManagerSnapshots: map[string]ManagerSnapshotter{"state": f.state},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := f.m.RequestFederationSave(ctx, testFed, testSaved, nil); err != nil {
		t.Fatal(err)
	}
	for _, h := range []core.FederateHandle{1, 2} {
		if err := f.m.FederateSaveBegun(ctx, testFed, h); err != nil {
			t.Fatal(err)
		}
		if err := f.m.FederateSaveComplete(ctx, testFed, h); err != nil {
			t.Fatal(err)
		}
	}
	f.state.live = testCurrent
	f.out.sent = nil
	return f
}

func TestApplicableFMRestoreRequestTargetsRequesterAndOrdersEvents(t *testing.T) {
	f := newApplicableRestoreFixture(t)
	ctx := context.Background()
	if got := f.m.QueryRestoreState(testFed, testSaved); got != RestoreIdle {
		t.Fatalf("before request: %v", got)
	}
	if err := f.m.RequestFederationRestoreBy(ctx, testFed, 2, testSaved); err != nil {
		t.Fatal(err)
	}
	events := f.out.Sent()
	if len(events) != 5 {
		t.Fatalf("request notifications: %+v", events)
	}
	if ack, ok := events[0].Event.(*RestoreRequestSucceededEvent); !ok || events[0].Federate != 2 || ack.Label() != testSaved {
		t.Fatalf("first event is not requester-only confirmation: %+v", events[0])
	}
	for i, member := range f.roster {
		begun, initiate := events[i+1], events[i+3]
		if _, ok := begun.Event.(*RestoreBegunEvent); !ok || begun.Federate != member.Handle || begun.Federation != testFed {
			t.Fatalf("begun recipient %d: %+v", i, begun)
		}
		start, ok := initiate.Event.(*InitiateRestoreEvent)
		if !ok || initiate.Federation != testFed || initiate.Federate != member.Handle || start.Label() != testSaved || start.FederateHandle != member.Handle || start.FederateName != member.Name {
			t.Fatalf("initiation saved identity %d: %+v", i, initiate)
		}
	}
	if f.state.live != testCurrent || f.state.commits != 0 {
		t.Fatal("request applied state before participant responses")
	}
	label, statuses, err := f.m.QueryFederationRestoreStatus(testFed)
	if err != nil || label != testSaved || len(statuses) != 2 {
		t.Fatalf("restore status: %s %+v %v", label, statuses, err)
	}
	for i, status := range statuses {
		if status.PreRestoreHandle != f.roster[i].Handle || status.PostRestoreHandle != f.roster[i].Handle || status.Status != FederateRestoring {
			t.Fatalf("status: %+v", status)
		}
	}
	if release, err := f.m.AcquireService(testFed); !errors.Is(err, core.ErrRestoreAlreadyInProgress) {
		if release != nil {
			release()
		}
		t.Fatalf("restore must exclude new service information: %v", err)
	}
	if err := f.m.FederateRestoreComplete(ctx, testFed, 1); err != nil {
		t.Fatal(err)
	}
	if f.state.commits != 0 || len(f.out.Sent()) != 5 {
		t.Fatal("first response prematurely completed federation restore")
	}
	if err := f.m.FederateRestoreComplete(ctx, testFed, 2); err != nil {
		t.Fatal(err)
	}
	if f.state.live != testSaved || f.state.commits != 1 {
		t.Fatal("successful restore did not import the saved state once")
	}
	events = f.out.Sent()
	if len(events) != 7 {
		t.Fatalf("terminal count: %d", len(events))
	}
	for i, event := range events[5:] {
		terminal, ok := event.Event.(*RestoredEvent)
		if !ok || event.Federate != f.roster[i].Handle || event.Federation != testFed || terminal.Label() != testSaved {
			t.Fatalf("terminal: %+v", event)
		}
	}
	_, statuses, err = f.m.QueryFederationRestoreStatus(testFed)
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range statuses {
		if status.Status != NoRestoreInProgress {
			t.Fatalf("terminal status: %+v", status)
		}
	}
}

func TestApplicableFMRestoreNegativeConfirmationPreservesState(t *testing.T) {
	for _, mode := range []string{"missing-label", "missing-member", "wrong-type", testPrepareFailure} {
		t.Run(mode, func(t *testing.T) {
			f := newApplicableRestoreFixture(t)
			label := testSaved
			switch mode {
			case "missing-label":
				label = "absent"
			case "missing-member":
				f.roster = f.roster[:1]
			case "wrong-type":
				f.roster[1].FederateType = "wrong"
			case testPrepareFailure:
				f.state.prepareErr = io.ErrClosedPipe
			}
			before := append([]core.FederationMember(nil), f.roster...)
			err := f.m.RequestFederationRestoreBy(context.Background(), testFed, 1, label)
			if err == nil {
				t.Fatal("invalid restoration request succeeded")
			}
			events := f.out.Sent()
			if len(events) != 1 {
				t.Fatalf("negative confirmation changed callback obligations: %+v", events)
			}
			negative, ok := events[0].Event.(*RestoreRequestFailedEvent)
			if !ok || events[0].Federate != 1 || events[0].Federation != testFed || negative.Label() != label || negative.Reason == "" {
				t.Fatalf("negative confirmation: %+v", events[0])
			}
			if f.state.live != testCurrent || f.state.commits != 0 || !reflect.DeepEqual(before, f.roster) {
				t.Fatal("rejected restore changed live state or identities")
			}
			_, statuses, err := f.m.QueryFederationRestoreStatus(testFed)
			if err != nil {
				t.Fatal(err)
			}
			for _, status := range statuses {
				if status.Status != NoRestoreInProgress {
					t.Fatalf("negative confirmation left active protocol: %+v", status)
				}
			}
			release, err := f.m.AcquireService(testFed)
			if err != nil {
				t.Fatal("negative confirmation stranded ordinary service admission", err)
			}
			release()
		})
	}
}

func TestApplicableFMRestoreGuardsPreserveProtocol(t *testing.T) {
	f := newApplicableRestoreFixture(t)
	ctx := context.Background()
	if err := f.m.RequestFederationSave(ctx, testFed, "active-save", nil); err != nil {
		t.Fatal(err)
	}
	before := f.out.Sent()
	for name, call := range map[string]func() error{
		"request":      func() error { return f.m.RequestFederationRestoreBy(ctx, testFed, 1, testSaved) },
		"complete":     func() error { return f.m.FederateRestoreComplete(ctx, testFed, 1) },
		"not-complete": func() error { return f.m.FederateRestoreNotComplete(ctx, testFed, 1) },
		"status":       func() error { _, _, err := f.m.QueryFederationRestoreStatus(testFed); return err },
	} {
		t.Run(name, func(t *testing.T) {
			if err := call(); !errors.Is(err, core.ErrSaveAlreadyInProgress) {
				t.Fatalf("save guard: %v", err)
			}
			if !reflect.DeepEqual(before, f.out.Sent()) || f.state.live != testCurrent || f.m.QuerySaveState(testFed, "active-save") != StateInitiated {
				t.Fatal("rejected operation changed save or callbacks")
			}
		})
	}
	if err := f.m.AbortSave(ctx, testFed); err != nil {
		t.Fatal(err)
	}
	if err := f.m.RequestFederationRestoreBy(ctx, testFed, 1, testSaved); err != nil {
		t.Fatal(err)
	}
	before = f.out.Sent()
	if _, _, err := f.m.QueryFederationSaveStatus(testFed); !errors.Is(err, core.ErrRestoreAlreadyInProgress) {
		t.Fatalf("save status during restore: %v", err)
	}
	if err := f.m.RequestFederationRestoreBy(ctx, testFed, 2, testSaved); !errors.Is(err, core.ErrRestoreAlreadyInProgress) {
		t.Fatalf("overlapping restore: %v", err)
	}
	if !reflect.DeepEqual(before, f.out.Sent()) || f.state.commits != 0 {
		t.Fatal("rejected restore-side requests changed protocol")
	}
}

func TestApplicableFMRestoreAbortRequiresActiveProtocolAndNotifiesAll(t *testing.T) {
	f := newApplicableRestoreFixture(t)
	ctx := context.Background()
	for _, fed := range []core.FederationName{testFed, "other"} {
		if err := f.m.AbortRestore(ctx, fed); !errors.Is(err, core.ErrRestoreNotInProgress) {
			t.Fatalf("idle abort: %v", err)
		}
	}
	if len(f.out.Sent()) != 0 {
		t.Fatal("idle abort emitted callback")
	}
	if err := f.m.RequestFederationRestoreBy(ctx, testFed, 1, testSaved); err != nil {
		t.Fatal(err)
	}
	start := len(f.out.Sent())
	if err := f.m.AbortRestore(ctx, testFed); err != nil {
		t.Fatal(err)
	}
	terminal := f.out.Sent()[start:]
	if len(terminal) != 2 {
		t.Fatalf("abort recipients: %+v", terminal)
	}
	for i, event := range terminal {
		failure, ok := event.Event.(*NotRestoredEvent)
		if !ok || event.Federate != f.roster[i].Handle || failure.Reason != RestoreAborted || failure.Label() != testSaved {
			t.Fatalf("abort failure: %+v", event)
		}
	}
	if f.state.live != testCurrent || f.state.commits != 0 || f.state.releases != 1 {
		t.Fatal("abort changed state or retained preparation")
	}
	if err := f.m.AbortRestore(ctx, testFed); !errors.Is(err, core.ErrRestoreNotInProgress) {
		t.Fatalf("duplicate abort: %v", err)
	}
	if len(f.out.Sent()) != start+2 {
		t.Fatal("duplicate abort emitted terminal again")
	}
	if err := f.m.RequestFederationRestoreBy(ctx, testFed, 2, testSaved); err != nil {
		t.Fatal("abort prevented later request", err)
	}
}

func TestApplicableFMRestoreCommitFailureNotifiesEveryParticipant(t *testing.T) {
	f := newApplicableRestoreFixture(t)
	ctx := context.Background()
	f.state.commitErr = io.ErrClosedPipe
	if err := f.m.RequestFederationRestoreBy(ctx, testFed, 1, testSaved); err != nil {
		t.Fatal(err)
	}
	if err := f.m.FederateRestoreComplete(ctx, testFed, 1); err != nil {
		t.Fatal(err)
	}
	if err := f.m.FederateRestoreComplete(ctx, testFed, 2); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("commit failure disappeared: %v", err)
	}
	terminal := f.out.Sent()[5:]
	if len(terminal) != 2 {
		t.Fatalf("failure recipients: %+v", terminal)
	}
	for i, event := range terminal {
		failure, ok := event.Event.(*NotRestoredEvent)
		if !ok || event.Federate != f.roster[i].Handle || failure.Reason != RTIUnableToRestore || failure.Label() != testSaved {
			t.Fatalf("RTI failure: %+v", event)
		}
	}
	if f.state.live != testCurrent || f.state.releases != 1 || f.m.QueryRestoreState(testFed, testSaved) != RestoreFailed {
		t.Fatal("failed restoration changed state or retained preparation")
	}
}

type applicableBlockingRestoreStore struct {
	*memStore
	entered, release chan struct{}
}

func (s *applicableBlockingRestoreStore) Reader(fed core.FederationName, label string) (io.ReadCloser, error) {
	close(s.entered)
	<-s.release
	return s.memStore.Reader(fed, label)
}

func TestApplicableFMRestorePendingRequestExcludesAnotherRequest(t *testing.T) {
	f := newApplicableRestoreFixture(t)
	store := &applicableBlockingRestoreStore{memStore: f.store, entered: make(chan struct{}), release: make(chan struct{})}
	f.m.opts.BundleStore = store
	done := make(chan error, 1)
	go func() { done <- f.m.RequestFederationRestoreBy(context.Background(), testFed, 1, testSaved) }()
	defer func() {
		close(store.release)
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(3 * time.Second):
			t.Error("restore did not finish")
		}
	}()
	select {
	case <-store.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("restore did not reach reader")
	}
	if len(f.out.Sent()) != 0 {
		t.Fatal("confirmation issued before bundle validation")
	}
	label, statuses, err := f.m.QueryFederationRestoreStatus(testFed)
	if err != nil || label != testSaved || len(statuses) != 2 {
		t.Fatalf("pending statuses: %s %+v %v", label, statuses, err)
	}
	for _, status := range statuses {
		if status.Status != FederateRestoreRequestPending {
			t.Fatalf("not pending: %+v", status)
		}
	}
	if err := f.m.RequestFederationRestoreBy(context.Background(), testFed, 1, testSaved); !errors.Is(err, core.ErrRestoreAlreadyInProgress) {
		t.Fatalf("second pre-confirmation request admitted: %v", err)
	}
	if f.state.commits != 0 || len(f.out.Sent()) != 0 {
		t.Fatal("rejected pending request had side effects")
	}
}

type applicableRestoreFailingOutbox struct {
	base    *transitionOutbox
	failure error
}

func (o *applicableRestoreFailingOutbox) Send(ctx context.Context, fed core.FederationName, h core.FederateHandle, event core.OutboundEvent) error {
	if _, failed := event.(*RestoreRequestFailedEvent); failed {
		return o.failure
	}
	return o.base.Send(ctx, fed, h, event)
}

func TestApplicableFMRestoreNegativeConfirmationDeliveryErrorIsNotDiscarded(t *testing.T) {
	f := newApplicableRestoreFixture(t)
	f.m.opts.Outbox = &applicableRestoreFailingOutbox{base: f.out, failure: io.ErrClosedPipe}
	err := f.m.RequestFederationRestoreBy(context.Background(), testFed, 1, "absent")
	if !errors.Is(err, ErrSaveBundleNotFound) || !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("restore rejection must retain both lookup and failed confirmation causes: %v", err)
	}
	if f.state.live != testCurrent || f.state.commits != 0 || len(f.out.Sent()) != 0 {
		t.Fatal("failed confirmation mutated state or fabricated delivery")
	}
	if f.m.QueryRestoreState(testFed, "absent") != RestoreFailed {
		t.Fatal("failed request retained an active protocol")
	}
}

const testCurrent = "current"

const testFed = "fed"

const testPrepareFailure = "prepare-failure"

const testSaved = "saved"
