package save_restore_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/adapters/delivery"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	savepoint "github.com/cbchoi/gorti/rti/internal/hla/save_restore"
)

type terminalDeliveryOutbox struct {
	outbox
	blocked         core.FederateHandle
	blockErr        error
	commitErr       error
	failureErr      error
	failureAttempts []core.FederateHandle
	batches         [][]core.OutboxDelivery
	reservations    []*terminalDeliveryReservation
	onReserve       func()
	onCommit        func()
	directSuccess   int
}

func (o *terminalDeliveryOutbox) Send(ctx context.Context, fed core.FederationName, h core.FederateHandle, event core.OutboundEvent) error {
	if _, ok := event.(*savepoint.RestoredEvent); ok {
		o.directSuccess++
		if h == o.blocked {
			return o.blockErr
		}
	}
	if _, ok := event.(*savepoint.NotRestoredEvent); ok {
		o.failureAttempts = append(o.failureAttempts, h)
		if h == 1 && o.failureErr != nil {
			return o.failureErr
		}
	}
	return o.outbox.Send(ctx, fed, h, event)
}

func (o *terminalDeliveryOutbox) Reserve(_ context.Context, _ core.FederationName, deliveries []core.OutboxDelivery) (core.OutboxReservation, error) {
	batch := append([]core.OutboxDelivery(nil), deliveries...)
	o.batches = append(o.batches, batch)
	for _, delivery := range batch {
		if delivery.Recipient == o.blocked {
			return nil, o.blockErr
		}
	}
	reservation := &terminalDeliveryReservation{owner: o, deliveries: batch}
	o.reservations = append(o.reservations, reservation)
	if o.onReserve != nil {
		o.onReserve()
	}
	return reservation, nil
}

type terminalDeliveryReservation struct {
	owner      *terminalDeliveryOutbox
	deliveries []core.OutboxDelivery
	committed  bool
	released   bool
	commits    int
	releases   int
}

func (r *terminalDeliveryReservation) Commit() error {
	if r.committed || r.released {
		return nil
	}
	r.commits++
	if r.owner.onCommit != nil {
		r.owner.onCommit()
	}
	if r.owner.commitErr != nil {
		return r.owner.commitErr
	}
	r.owner.mu.Lock()
	defer r.owner.mu.Unlock()
	for _, delivery := range r.deliveries {
		r.owner.events = append(r.owner.events, recordedEvent{delivery.Recipient, delivery.Event})
	}
	r.committed = true
	return nil
}

func (r *terminalDeliveryReservation) Release() {
	if r.committed || r.released {
		return
	}
	r.released = true
	r.releases++
}

func terminalDeliveryManager(t *testing.T, output *terminalDeliveryOutbox) (*savepoint.Manager, *preparedState, *[]core.FederateHandle) {
	t.Helper()
	store := newStore(t)
	seed(t, store, savedStateValue, map[string][]byte{"state": []byte(savedStateValue)})
	snapshot := &preparedState{value: liveStateValue}
	members := []core.FederateHandle{2, 1}
	m, err := savepoint.New(savepoint.Options{
		Outbox: output, BundleStore: store,
		Members:          func(core.FederationName) []core.FederateHandle { return append([]core.FederateHandle(nil), members...) },
		ManagerSnapshots: map[string]savepoint.ManagerSnapshotter{"state": snapshot}, RequiredSnapshots: []string{"state"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return m, snapshot, &members
}

func startTerminalRestore(t *testing.T, m *savepoint.Manager) {
	t.Helper()
	if err := m.RequestFederationRestore(context.Background(), "fed", savedStateValue); err != nil {
		t.Fatal(err)
	}
	if err := m.FederateRestoreComplete(context.Background(), "fed", 1); err != nil {
		t.Fatal(err)
	}
}

func terminalSuccessHandles(o *terminalDeliveryOutbox) []core.FederateHandle {
	var handles []core.FederateHandle
	for _, recorded := range o.snapshot() {
		if _, ok := recorded.event.(*savepoint.RestoredEvent); ok {
			handles = append(handles, recorded.handle)
		}
	}
	return handles
}

func assertTerminalFailure(t *testing.T, m *savepoint.Manager, snapshot *preparedState, output *terminalDeliveryOutbox) {
	t.Helper()
	if snapshot.value != liveStateValue || snapshot.commits != 0 {
		t.Fatalf("failed admission committed state: %+v", snapshot)
	}
	if got := m.QueryRestoreState("fed", savedStateValue); got != savepoint.RestoreFailed {
		t.Fatalf("state=%v, want RestoreFailed", got)
	}
	if got := terminalSuccessHandles(output); len(got) != 0 {
		t.Fatalf("partial success callbacks: %v", got)
	}
	if !reflect.DeepEqual(output.failureAttempts, []core.FederateHandle{1, 2}) {
		t.Fatalf("failure fanout=%v", output.failureAttempts)
	}
}

func TestRestoreTerminalReservationFailurePreservesStateAndAllowsRetry(t *testing.T) {
	for name, reserveErr := range map[string]error{"capacity": core.ErrFederateOverflow, "lifecycle": core.ErrOutboxUnavailable} {
		t.Run(name, func(t *testing.T) {
			output := &terminalDeliveryOutbox{blocked: 2, blockErr: reserveErr}
			m, snapshot, _ := terminalDeliveryManager(t, output)
			startTerminalRestore(t, m)
			if err := m.FederateRestoreComplete(context.Background(), "fed", 2); !errors.Is(err, reserveErr) {
				t.Fatalf("completion error=%v", err)
			}
			assertTerminalFailure(t, m, snapshot, output)
			if len(output.batches) != 1 || len(output.batches[0]) != 2 || output.batches[0][0].Recipient != 1 || output.batches[0][1].Recipient != 2 {
				t.Fatalf("missing all-recipient reservation: %+v", output.batches)
			}
			if output.directSuccess != 0 {
				t.Fatal("used non-atomic success Send")
			}
			output.blocked = 0
			startTerminalRestore(t, m)
			if err := m.FederateRestoreComplete(context.Background(), "fed", 2); err != nil {
				t.Fatal(err)
			}
			if snapshot.value != savedStateValue || snapshot.commits != 1 {
				t.Fatalf("retry state=%+v", snapshot)
			}
			if got := terminalSuccessHandles(output); !reflect.DeepEqual(got, []core.FederateHandle{1, 2}) {
				t.Fatalf("retry success callbacks=%v", got)
			}
		})
	}
}

func TestRestoreTerminalReservationPrecedesStateAndCallbackCommit(t *testing.T) {
	output := &terminalDeliveryOutbox{}
	m, snapshot, _ := terminalDeliveryManager(t, output)
	var order []string
	output.onReserve = func() {
		order = append(order, "reserve")
		if snapshot.value != liveStateValue || snapshot.commits != 0 {
			t.Fatal("state changed before reservation")
		}
	}
	output.onCommit = func() {
		order = append(order, "deliver")
		if snapshot.value != savedStateValue || snapshot.commits != 1 {
			t.Fatal("callbacks committed before state")
		}
	}
	startTerminalRestore(t, m)
	if len(output.batches) != 0 {
		t.Fatal("reserved before all participant reports")
	}
	if err := m.FederateRestoreComplete(context.Background(), "fed", 2); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(order, []string{"reserve", "deliver"}) {
		t.Fatalf("operation order=%v", order)
	}
	if output.directSuccess != 0 || len(output.reservations) != 1 || output.reservations[0].commits != 1 {
		t.Fatal("terminal delivery was not committed exactly once")
	}
	for _, delivery := range output.batches[0] {
		if _, ok := delivery.Event.(*savepoint.RestoredEvent); !ok {
			t.Fatalf("reserved wrong callback %T", delivery.Event)
		}
	}
	if m.QueryRestoreState("fed", savedStateValue) != savepoint.RestoreCompleted {
		t.Fatal("restore not completed")
	}
}

func TestRestoreTerminalReservationReleasedOnValidationFailure(t *testing.T) {
	output := &terminalDeliveryOutbox{}
	m, snapshot, members := terminalDeliveryManager(t, output)
	startTerminalRestore(t, m)
	output.onReserve = func() { *members = []core.FederateHandle{1, 3} }
	if err := m.FederateRestoreComplete(context.Background(), "fed", 2); !errors.Is(err, savepoint.ErrSaveBundleIncompatible) {
		t.Fatalf("completion error=%v", err)
	}
	assertTerminalFailure(t, m, snapshot, output)
	if len(output.reservations) != 1 || output.reservations[0].commits != 0 || output.reservations[0].releases != 1 {
		t.Fatal("failed validation leaked reservation")
	}
}

func TestRestoreTerminalReservationReleasedOnCancellation(t *testing.T) {
	output := &terminalDeliveryOutbox{}
	m, snapshot, _ := terminalDeliveryManager(t, output)
	startTerminalRestore(t, m)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	output.onReserve = cancel
	if err := m.FederateRestoreComplete(ctx, "fed", 2); !errors.Is(err, context.Canceled) {
		t.Fatalf("completion error=%v", err)
	}
	assertTerminalFailure(t, m, snapshot, output)
	if len(output.reservations) != 1 || output.reservations[0].commits != 0 || output.reservations[0].releases != 1 {
		t.Fatal("cancellation leaked reservation")
	}
}

func TestRestoreTerminalFailureFanoutContinuesAfterDeliveryError(t *testing.T) {
	callbackErr := errors.New("failure notification unavailable")
	output := &terminalDeliveryOutbox{blocked: 2, blockErr: core.ErrFederateOverflow, failureErr: callbackErr}
	m, snapshot, _ := terminalDeliveryManager(t, output)
	startTerminalRestore(t, m)
	err := m.FederateRestoreComplete(context.Background(), "fed", 2)
	if !errors.Is(err, core.ErrFederateOverflow) || !errors.Is(err, callbackErr) {
		t.Fatalf("combined error=%v", err)
	}
	assertTerminalFailure(t, m, snapshot, output)
}

func TestRestoreTerminalParticipantFailureAndAbortDoNotReserveSuccess(t *testing.T) {
	for _, abort := range []bool{false, true} {
		t.Run(map[bool]string{false: "participant-failure", true: "abort"}[abort], func(t *testing.T) {
			output := &terminalDeliveryOutbox{}
			m, snapshot, _ := terminalDeliveryManager(t, output)
			startTerminalRestore(t, m)
			var err error
			if abort {
				err = m.AbortRestore(context.Background(), "fed")
			} else {
				err = m.FederateRestoreNotComplete(context.Background(), "fed", 2)
			}
			if err != nil {
				t.Fatal(err)
			}
			assertTerminalFailure(t, m, snapshot, output)
			if len(output.batches) != 0 {
				t.Fatal("reserved success callbacks for failed restore")
			}
		})
	}
}

func TestRestoreTerminalUnexpectedCommitErrorDoesNotClaimRollback(t *testing.T) {
	deliveryErr := errors.New("unexpected commit failure")
	output := &terminalDeliveryOutbox{commitErr: deliveryErr}
	m, snapshot, _ := terminalDeliveryManager(t, output)
	startTerminalRestore(t, m)
	err := m.FederateRestoreComplete(context.Background(), "fed", 2)
	if !errors.Is(err, deliveryErr) || !errors.Is(err, savepoint.ErrRestoreStateCommitted) || !strings.Contains(err.Error(), "state committed") {
		t.Fatalf("completion error=%v", err)
	}
	if snapshot.value != savedStateValue || snapshot.commits != 1 || m.QueryRestoreState("fed", savedStateValue) != savepoint.RestoreFailed {
		t.Fatalf("committed state misreported: %+v", snapshot)
	}
	if len(output.failureAttempts) != 0 || output.directSuccess != 0 {
		t.Fatal("unexpected commit error caused misleading failure or duplicate success fanout")
	}
	if len(output.reservations) != 1 || output.reservations[0].releases != 1 {
		t.Fatal("unexpected commit error leaked reservation")
	}
}

type terminalFillerEvent struct{}

func (*terminalFillerEvent) Seq() uint64 { return 0 }

func takeTerminalBatch(t *testing.T, queue <-chan []core.OutboundEvent) []core.OutboundEvent {
	t.Helper()
	select {
	case events := <-queue:
		if len(events) != 1 {
			t.Fatalf("batch=%v", events)
		}
		return events
	default:
		t.Fatal("expected an admitted callback batch")
		return nil
	}
}

func TestRestoreTerminalProductionOutboxFullRecipientAndRetry(t *testing.T) {
	ctx := context.Background()
	output := delivery.NewWithBatch(2, 1, 0)
	first, cancelFirst, err := output.Subscribe(ctx, "fed", 1)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cancelFirst() }()
	second, cancelSecond, err := output.Subscribe(ctx, "fed", 2)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cancelSecond() }()
	store := newStore(t)
	seed(t, store, savedStateValue, map[string][]byte{"state": []byte(savedStateValue)})
	snapshot := &preparedState{value: liveStateValue}
	m, err := savepoint.New(savepoint.Options{
		Outbox: output, BundleStore: store,
		Members:          func(core.FederationName) []core.FederateHandle { return []core.FederateHandle{1, 2} },
		ManagerSnapshots: map[string]savepoint.ManagerSnapshotter{"state": snapshot}, RequiredSnapshots: []string{"state"},
	})
	if err != nil {
		t.Fatal(err)
	}
	start := func() {
		t.Helper()
		startTerminalRestore(t, m)
		for _, queue := range []<-chan []core.OutboundEvent{first, second} {
			if _, ok := takeTerminalBatch(t, queue)[0].(*savepoint.RestoreBegunEvent); !ok {
				t.Fatal("missing restore-begun callback")
			}
			if _, ok := takeTerminalBatch(t, queue)[0].(*savepoint.InitiateRestoreEvent); !ok {
				t.Fatal("missing initiate-restore callback")
			}
		}
	}
	start()
	for i := 0; i < 2; i++ {
		if err := output.Send(ctx, "fed", 2, &terminalFillerEvent{}); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.FederateRestoreComplete(ctx, "fed", 2); !errors.Is(err, core.ErrFederateOverflow) {
		t.Fatalf("completion error=%v", err)
	}
	if snapshot.value != liveStateValue || snapshot.commits != 0 || m.QueryRestoreState("fed", savedStateValue) != savepoint.RestoreFailed {
		t.Fatalf("full recipient still committed: %+v", snapshot)
	}
	if _, ok := takeTerminalBatch(t, first)[0].(*savepoint.NotRestoredEvent); !ok {
		t.Fatal("first recipient received partial success")
	}
	for i := 0; i < 2; i++ {
		if _, ok := takeTerminalBatch(t, second)[0].(*terminalFillerEvent); !ok {
			t.Fatal("second recipient received partial success")
		}
	}
	start()
	if err := m.FederateRestoreComplete(ctx, "fed", 2); err != nil {
		t.Fatal(err)
	}
	for _, queue := range []<-chan []core.OutboundEvent{first, second} {
		if _, ok := takeTerminalBatch(t, queue)[0].(*savepoint.RestoredEvent); !ok {
			t.Fatal("retry did not deliver success to each recipient")
		}
	}
	if snapshot.value != savedStateValue || snapshot.commits != 1 || m.QueryRestoreState("fed", savedStateValue) != savepoint.RestoreCompleted {
		t.Fatalf("retry state=%+v", snapshot)
	}
}
