package sync

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type completionOutbox struct {
	mu         sync.Mutex
	fail       bool
	deliveries []core.OutboxDelivery
}

func (o *completionOutbox) Send(_ context.Context, _ core.FederationName, h core.FederateHandle, event core.OutboundEvent) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.fail {
		return core.ErrFederateOverflow
	}
	o.deliveries = append(o.deliveries, core.OutboxDelivery{Recipient: h, Event: event})
	return nil
}
func (o *completionOutbox) Reserve(_ context.Context, _ core.FederationName, deliveries []core.OutboxDelivery) (core.OutboxReservation, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.fail {
		return nil, core.ErrFederateOverflow
	}
	return &completionReservation{out: o, deliveries: append([]core.OutboxDelivery(nil), deliveries...)}, nil
}

type completionReservation struct {
	once       sync.Once
	out        *completionOutbox
	deliveries []core.OutboxDelivery
}

func (r *completionReservation) Commit() error {
	r.once.Do(func() {
		r.out.mu.Lock()
		defer r.out.mu.Unlock()
		r.out.deliveries = append(r.out.deliveries, r.deliveries...)
	})
	return nil
}
func (r *completionReservation) Release()     { r.once.Do(func() {}) }
func (o *completionOutbox) setFail(fail bool) { o.mu.Lock(); o.fail = fail; o.mu.Unlock() }
func (o *completionOutbox) sent() []core.OutboxDelivery {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]core.OutboxDelivery(nil), o.deliveries...)
}
func strictCompletionManager(t *testing.T, out *completionOutbox, members ...core.FederateHandle) *Manager {
	t.Helper()
	m, err := New(Options{Outbox: out, RequireInvocations: true, Members: func(core.FederationName) []core.FederateHandle { return append([]core.FederateHandle(nil), members...) }})
	if err != nil {
		t.Fatal(err)
	}
	return m
}
func terminalIdentities(out *completionOutbox, label string) map[core.FederateHandle]uint64 {
	result := map[core.FederateHandle]uint64{}
	for _, delivery := range out.sent() {
		if callback, ok := delivery.Event.(*SynchronizedEvent); ok && callback.Label() == label {
			result[delivery.Recipient] = callback.Seq()
		}
	}
	return result
}
func mustCompletion(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func reportCompletion(t *testing.T, m *Manager, h core.FederateHandle, kind core.SyncCallbackKind, label string, sequence uint64) {
	t.Helper()
	mustCompletion(t, m.ObserveSyncCallbackEntry(context.Background(), "f", h, kind, label, sequence))
	mustCompletion(t, m.ObserveSyncCallback(context.Background(), "f", h, kind, label, sequence))
}

func enterCompletionAnnouncements(t *testing.T, m *Manager, out *completionOutbox) {
	t.Helper()
	for _, delivery := range out.sent() {
		if announced, ok := delivery.Event.(*AnnounceEvent); ok {
			mustCompletion(t, m.ObserveSyncCallbackEntry(context.Background(), "f", delivery.Recipient, core.SyncCallbackAnnounced, announced.Label(), announced.Seq()))
		}
	}
}

func TestCompletionStrictConfiguration(t *testing.T) {
	if _, err := New(Options{Outbox: &fakeOutbox{}, RequireInvocations: true, Members: func(core.FederationName) []core.FederateHandle { return nil }}); err == nil {
		t.Fatal("non-reservable strict profile accepted")
	}
	if _, err := New(Options{Outbox: &completionOutbox{}, RequireInvocations: true}); err == nil {
		t.Fatal("no membership resolver accepted")
	}
}

func TestCompletionStrictQueuedAnnouncementCannotAuthorizeAchievement(t *testing.T) {
	ctx := context.Background()
	out := &completionOutbox{}
	m := strictCompletionManager(t, out, 1, 2)
	mustCompletion(t, m.RegisterBy(ctx, "f", 1, "p", nil, nil))
	before := m.ExportState("f")
	if err := m.Achieve(ctx, "f", 1, "p"); !errors.Is(err, core.ErrSyncPointNotRegistered) {
		t.Fatalf("never-invoked queued announcement authorized achievement: %v", err)
	}
	if !reflect.DeepEqual(before, m.ExportState("f")) {
		t.Fatal("failed achievement changed state")
	}
}

func TestCompletionLifecycleInvocationRetirementAndEpochReuse(t *testing.T) {
	ctx := context.Background()
	out := &completionOutbox{}
	m := strictCompletionManager(t, out, 1, 2)
	mustCompletion(t, m.RegisterBy(ctx, "f", 1, "p", []byte("old"), nil))
	enterCompletionAnnouncements(t, m, out)
	mustCompletion(t, m.Achieve(ctx, "f", 1, "p"))
	mustCompletion(t, m.FederateJoined(ctx, "f", 3))
	mustCompletion(t, m.AchieveWith(ctx, "f", 2, "p", false))
	if m.QueryState("f", "p") != StateAnnounced {
		t.Fatal("late join did not delay completion")
	}
	mustCompletion(t, m.FederateResigned(ctx, "f", 3))
	if m.QueryState("f", "p") != StateAchieved {
		t.Fatal("resign did not complete remaining set")
	}
	old := terminalIdentities(out, "p")
	if len(old) != 2 {
		t.Fatalf("terminal fanout %v", old)
	}
	before := len(out.sent())
	mustCompletion(t, m.FederateJoined(ctx, "f", 4))
	if len(out.sent()) != before {
		t.Fatal("late join after all achievements was announced")
	}
	reportCompletion(t, m, 1, core.SyncCallbackSynchronized, "p", old[1])
	if m.QueryState("f", "p") != StateAchieved {
		t.Fatal("one invocation retired whole label")
	}
	image := m.ExportState("f")
	restored := strictCompletionManager(t, &completionOutbox{}, 1, 2)
	commit, err := restored.PrepareImport("f", image)
	mustCompletion(t, err)
	commit()
	restoredImage := restored.ExportState("f")
	restoredImage.Pending = image.Pending // Restore rebuilds live admission, not invocation history.
	if !reflect.DeepEqual(image, restoredImage) {
		t.Fatal("partial invocation ledger did not round trip exactly")
	}
	reportCompletion(t, m, 2, core.SyncCallbackSynchronized, "p", old[2])
	if m.QueryState("f", "p") != StateUnknown {
		t.Fatal("label not retired after all invocations")
	}
	mustCompletion(t, m.RegisterBy(ctx, "f", 1, "p", []byte("new"), nil))
	enterCompletionAnnouncements(t, m, out)
	mustCompletion(t, m.Achieve(ctx, "f", 1, "p"))
	mustCompletion(t, m.Achieve(ctx, "f", 2, "p"))
	fresh := terminalIdentities(out, "p")
	for h, seq := range old {
		mustCompletion(t, m.ObserveSyncCallback(ctx, "f", h, core.SyncCallbackSynchronized, "p", seq))
	}
	if m.QueryState("f", "p") != StateAchieved {
		t.Fatal("old receipts retired reused label")
	}
	for h, seq := range fresh {
		reportCompletion(t, m, h, core.SyncCallbackSynchronized, "p", seq)
	}
	if m.QueryState("f", "p") != StateUnknown {
		t.Fatal("fresh invocation ledger did not retire label")
	}
	for _, delivery := range out.sent() {
		if callback, ok := delivery.Event.(*SynchronizedEvent); ok && callback.Seq() == old[1] && !reflect.DeepEqual(callback.FailedToSync(), []core.FederateHandle{2}) {
			t.Fatal("unsuccessful set incorrect")
		}
	}
}

func TestCompletionExplicitSubsetDoesNotExpand(t *testing.T) {
	out := &completionOutbox{}
	m := strictCompletionManager(t, out, 1, 2, 3)
	ctx := context.Background()
	mustCompletion(t, m.RegisterBy(ctx, "f", 1, "p", nil, []core.FederateHandle{1, 2}))
	mustCompletion(t, m.FederateJoined(ctx, "f", 3))
	if got := m.Snapshot("f")[0].RequiredHandles; !reflect.DeepEqual(got, []core.FederateHandle{1, 2}) {
		t.Fatalf("subset expanded %v", got)
	}
	if err := m.Achieve(ctx, "f", 3, "p"); !errors.Is(err, core.ErrSyncPointNotRegistered) {
		t.Fatalf("outsider accepted %v", err)
	}
}

func TestCompletionAdmissionFailureIsAtomic(t *testing.T) {
	ctx := context.Background()
	out := &completionOutbox{fail: true}
	m := strictCompletionManager(t, out, 1, 2)
	if err := m.RegisterBy(ctx, "f", 1, "p", nil, nil); !errors.Is(err, core.ErrFederateOverflow) {
		t.Fatal(err)
	}
	if m.ExportState("f") != nil || len(out.sent()) != 0 {
		t.Fatal("failed registration changed state or delivered partial fanout")
	}
	out.setFail(false)
	mustCompletion(t, m.RegisterBy(ctx, "f", 1, "p", nil, nil))
	enterCompletionAnnouncements(t, m, out)
	mustCompletion(t, m.Achieve(ctx, "f", 1, "p"))
	before := m.ExportState("f")
	out.setFail(true)
	if err := m.Achieve(ctx, "f", 2, "p"); !errors.Is(err, core.ErrFederateOverflow) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, m.ExportState("f")) {
		t.Fatal("failed last achievement changed state")
	}
	out.setFail(false)
	mustCompletion(t, m.Achieve(ctx, "f", 2, "p"))
}

func TestCompletionDurableUnsentJoinAndRestore(t *testing.T) {
	ctx := context.Background()
	out := &completionOutbox{}
	m := strictCompletionManager(t, out, 1, 2)
	mustCompletion(t, m.RegisterBy(ctx, "f", 1, "p", nil, nil))
	out.setFail(true)
	if err := m.FederateJoined(ctx, "f", 3); !errors.Is(err, core.ErrFederateOverflow) {
		t.Fatal(err)
	}
	image := m.ExportState("f")
	if len(image.Pending) != 1 || len(image.Points[0].Required) != 3 {
		t.Fatalf("membership obligation lost %+v", image)
	}
	if err := m.Achieve(ctx, "f", 3, "p"); !errors.Is(err, core.ErrSyncPointNotRegistered) {
		t.Fatalf("unsent announcement authorized achievement: %v", err)
	}
	nextOut := &completionOutbox{}
	next := strictCompletionManager(t, nextOut, 1, 2, 3)
	commit, err := next.PrepareImport("f", image)
	mustCompletion(t, err)
	image.Pending[0].Tag = []byte("caller mutated")
	commit()
	mustCompletion(t, next.RetryPending(ctx, "f"))
	if len(nextOut.sent()) != len(image.Obligations) || len(next.ExportState("f").Pending) != 0 {
		t.Fatal("pending callback not retried exactly")
	}
	enterCompletionAnnouncements(t, next, nextOut)
	mustCompletion(t, next.Achieve(ctx, "f", 3, "p"))
}

func TestCompletionDurableResignCompletion(t *testing.T) {
	ctx := context.Background()
	out := &completionOutbox{}
	m := strictCompletionManager(t, out, 1, 2, 3)
	mustCompletion(t, m.RegisterBy(ctx, "f", 1, "p", nil, nil))
	enterCompletionAnnouncements(t, m, out)
	mustCompletion(t, m.Achieve(ctx, "f", 1, "p"))
	mustCompletion(t, m.Achieve(ctx, "f", 2, "p"))
	out.setFail(true)
	if err := m.FederateResigned(ctx, "f", 3); !errors.Is(err, core.ErrFederateOverflow) {
		t.Fatal(err)
	}
	image := m.ExportState("f")
	if len(image.Pending) != 2 || image.Points[0].State != StateAchieved {
		t.Fatal("resign did not retain terminal obligations")
	}
	out.setFail(false)
	mustCompletion(t, m.RetryPending(ctx, "f"))
	if len(terminalIdentities(out, "p")) != 2 {
		t.Fatal("terminal callbacks lost")
	}
	if m.QueryState("f", "p") != StateAchieved {
		t.Fatal("retry/send retired label without invocation")
	}
}

func TestCompletionRejectsInexactOrCorruptStrictImage(t *testing.T) {
	m := strictCompletionManager(t, &completionOutbox{}, 1)
	for _, image := range []*ManagerState{
		{Points: []PointState{{Label: "p", State: StateAnnounced, Required: []core.FederateHandle{1}}}},
		{Version: 2, RequireInvocations: true, NextEpoch: 1, NextSequence: 2, Points: []PointState{{Label: "p", State: StateAnnounced, Epoch: 1, Required: []core.FederateHandle{1}, Invocations: []InvocationState{{Recipient: 1, Announce: 3}}}}},
	} {
		if _, err := m.PrepareImport("f", image); err == nil {
			t.Fatal("incomplete image accepted")
		}
		if m.ExportState("f") != nil {
			t.Fatal("invalid restore changed state")
		}
	}
}

func TestCompletionConcurrentInvocationAndQueries(t *testing.T) {
	ctx := context.Background()
	out := &completionOutbox{}
	members := make([]core.FederateHandle, 64)
	for i := range members {
		members[i] = core.FederateHandle(i + 1)
	}
	m := strictCompletionManager(t, out, members...)
	mustCompletion(t, m.RegisterBy(ctx, "f", 1, "p", nil, nil))
	enterCompletionAnnouncements(t, m, out)
	var wg sync.WaitGroup
	for _, h := range members {
		wg.Add(1)
		go func(h core.FederateHandle) {
			defer wg.Done()
			if err := m.Achieve(ctx, "f", h, "p"); err != nil {
				t.Error(err)
			}
			_ = m.ExportState("f")
			_ = m.Snapshot("f")
		}(h)
	}
	wg.Wait()
	for h, seq := range terminalIdentities(out, "p") {
		wg.Add(1)
		go func(h core.FederateHandle, seq uint64) {
			defer wg.Done()
			if err := m.ObserveSyncCallbackEntry(ctx, "f", h, core.SyncCallbackSynchronized, "p", seq); err != nil {
				t.Error(err)
			}
			if err := m.ObserveSyncCallback(ctx, "f", h, core.SyncCallbackSynchronized, "p", seq); err != nil {
				t.Error(err)
			}
			_ = m.ExportState("f")
		}(h, seq)
	}
	wg.Wait()
	if m.QueryState("f", "p") != StateUnknown {
		t.Fatal("concurrent reports did not retire point")
	}
}

func TestCompletionStrictAnnouncementEntryAllowsReentrantAchievement(t *testing.T) {
	ctx := context.Background()
	out := &completionOutbox{}
	m := strictCompletionManager(t, out, 1, 2)
	mustCompletion(t, m.RegisterBy(ctx, "f", 1, "p", nil, nil))
	for _, delivery := range out.sent() {
		announced, ok := delivery.Event.(*AnnounceEvent)
		if !ok {
			continue
		}
		mustCompletion(t, m.ObserveSyncCallbackEntry(ctx, "f", delivery.Recipient, core.SyncCallbackAnnounced, announced.Label(), announced.Seq()))
		// Simulate the body of announceSynchronizationPoint, before completion.
		mustCompletion(t, m.Achieve(ctx, "f", delivery.Recipient, announced.Label()))
	}
	for _, inv := range m.ExportState("f").Points[0].Invocations {
		if !inv.AnnounceEntered || inv.AnnounceInvoked {
			t.Fatal("entry confused with callback completion")
		}
	}
	for h, seq := range terminalIdentities(out, "p") {
		mustCompletion(t, m.ObserveSyncCallbackEntry(ctx, "f", h, core.SyncCallbackSynchronized, "p", seq))
	}
	if m.QueryState("f", "p") != StateAchieved {
		t.Fatal("terminal entry retired label before callback completion")
	}
}
