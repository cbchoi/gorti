package savepoint

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestSaveHistorySuccessfulImmediateAndTimed(t *testing.T) {
	f := newScheduleFixture(t)
	ctx := context.Background()
	for _, timed := range []bool{true, false} {
		label := "immediate"
		var at *core.LogicalTime
		if timed {
			label = "timed"
			value := core.LogicalTime(17)
			at = &value
		}
		if err := f.m.RequestFederationSave(ctx, "fed", label, at); err != nil {
			t.Fatal(err)
		}
		if timed {
			f.ready = true
			if err := progressSchedule(t, f.m); err != nil {
				t.Fatal(err)
			}
		}
		for _, h := range f.members {
			if err := f.m.FederateSaveBegun(ctx, "fed", h); err != nil {
				t.Fatal(err)
			}
			if err := f.m.FederateSaveComplete(ctx, "fed", h); err != nil {
				t.Fatal(err)
			}
		}
		got := f.m.QueryMOMMetrics("fed", 1)
		if !got.LastSaveHistoryKnown || got.LastSaveName != label {
			t.Fatalf("completed %s save missing authoritative history: %+v", label, got)
		}
		if timed {
			if got.LastSaveTime == nil || *got.LastSaveTime != 17 {
				t.Fatalf("timed history: %+v", got)
			}
			*got.LastSaveTime = 99
			if *f.m.QueryMOMMetrics("fed", 1).LastSaveTime != 17 {
				t.Fatal("history time aliases manager state")
			}
		} else if got.LastSaveTime != nil {
			t.Fatalf("immediate save retained earlier timed value: %v", *got.LastSaveTime)
		}
	}
}

type historyMemoryStore struct {
	mu            sync.Mutex
	records       map[SaveHistoryKey]SaveHistory
	fail          bool
	before, after func(SaveHistory)
}

func (s *historyMemoryStore) LoadSaveHistory(key SaveHistoryKey) (SaveHistory, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneSaveHistory(s.records[key]), nil
}

func (s *historyMemoryStore) StoreSaveHistory(key SaveHistoryKey, history SaveHistory) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.before != nil {
		s.before(history)
	}
	if s.fail {
		return errors.New("history persistence unavailable")
	}
	if s.records == nil {
		s.records = make(map[SaveHistoryKey]SaveHistory)
	}
	s.records[key] = cloneSaveHistory(history)
	if s.after != nil {
		s.after(history)
	}
	return nil
}

type historyOutbox struct {
	fakeOutbox
	reserveErr, commitErr           error
	onReserve, onCommit             func()
	reservations, commits, releases int
}

func (o *historyOutbox) Reserve(ctx context.Context, fed core.FederationName, ds []core.OutboxDelivery) (core.OutboxReservation, error) {
	o.reservations++
	if o.onReserve != nil {
		o.onReserve()
	}
	if o.reserveErr != nil {
		return nil, o.reserveErr
	}
	return &historyReservation{out: o, ctx: ctx, fed: fed, ds: ds}, nil
}

type historyReservation struct {
	out  *historyOutbox
	ctx  context.Context
	fed  core.FederationName
	ds   []core.OutboxDelivery
	done bool
}

func (r *historyReservation) Commit() error {
	if r.done {
		return nil
	}
	r.done = true
	r.out.commits++
	if r.out.onCommit != nil {
		r.out.onCommit()
	}
	if r.out.commitErr != nil {
		return r.out.commitErr
	}
	for _, d := range r.ds {
		_ = r.out.Send(r.ctx, r.fed, d.Recipient, d.Event)
	}
	return nil
}

func (r *historyReservation) Release() {
	if !r.done {
		r.done = true
		r.out.releases++
	}
}

func newHistoryManager(t *testing.T, history *historyMemoryStore, out core.Outbox, bundle *memStore) *Manager {
	t.Helper()
	m, err := New(Options{Outbox: out, BundleStore: bundle, HistoryStore: history,
		Members: func(core.FederationName) []core.FederateHandle { return []core.FederateHandle{1} }})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func assertLastHistory(t *testing.T, m *Manager, known bool, name string) {
	t.Helper()
	got := m.QueryMOMMetrics("fed", 1)
	if got.LastSaveHistoryKnown != known || got.LastSaveName != name {
		t.Fatalf("history=%+v, want known=%v name=%q", got, known, name)
	}
}

func TestSaveHistoryKnownFreshVersusLegacyAbsent(t *testing.T) {
	store := &historyMemoryStore{}
	m := newHistoryManager(t, store, &historyOutbox{}, newMemStore())
	if err := m.RecoverSaveHistory("fed"); err != nil {
		t.Fatal(err)
	}
	assertLastHistory(t, m, false, "")
	if err := m.InitializeSaveHistory("fed"); err != nil {
		t.Fatal(err)
	}
	assertLastHistory(t, m, true, "")
	restarted := newHistoryManager(t, store, &historyOutbox{}, newMemStore())
	if err := restarted.RecoverSaveHistory("fed"); err != nil {
		t.Fatal(err)
	}
	assertLastHistory(t, restarted, true, "")
	completeSave(t, m, "saved")
	if err := restarted.InitializeSaveHistory("fed"); err != nil {
		t.Fatal(err)
	}
	assertLastHistory(t, restarted, true, "saved")
}

func TestSaveHistoryPreservesPreviousOnFailureAbortAndRetry(t *testing.T) {
	for _, mode := range []string{"participant", "abort", "bundle", "history", "reservation", "snapshot"} {
		t.Run(mode, func(t *testing.T) {
			store, out, bundle := &historyMemoryStore{}, &historyOutbox{}, newMemStore()
			m := newHistoryManager(t, store, out, bundle)
			completeSave(t, m, "previous")
			ctx := context.Background()
			if err := m.RequestFederationSave(ctx, "fed", "candidate", nil); err != nil {
				t.Fatal(err)
			}
			assertLastHistory(t, m, true, "previous")
			if err := m.FederateSaveBegun(ctx, "fed", 1); err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "participant":
				if err := m.FederateSaveNotComplete(ctx, "fed", 1); err != nil {
					t.Fatal(err)
				}
			case "abort":
				if err := m.AbortSave(ctx, "fed"); err != nil {
					t.Fatal(err)
				}
			default:
				bundle.failOnWrite = mode == "bundle"
				store.fail = mode == "history"
				if mode == "reservation" {
					out.reserveErr = errors.New("full")
				}
				if mode == "snapshot" {
					m.opts.ManagerSnapshots = map[string]ManagerSnapshotter{"broken": &fakeSnapshotter{marshalErr: errors.New("snapshot")}}
				}
				err := m.FederateSaveComplete(ctx, "fed", 1)
				if (mode == "history" || mode == "reservation") && err == nil {
					t.Fatal("missing infrastructure error")
				}
			}
			assertLastHistory(t, m, true, "previous")
			if m.QuerySaveState("fed", "candidate") != StateNotSaved {
				t.Fatal("failed save reported saved")
			}
			for _, sent := range out.Sent() {
				if event, ok := sent.Event.(*SavedEvent); ok && event.Label() == "candidate" {
					t.Fatal("failed save announced success")
				}
			}
			restart := newHistoryManager(t, store, &historyOutbox{}, bundle)
			if err := restart.RecoverSaveHistory("fed"); err != nil {
				t.Fatal(err)
			}
			assertLastHistory(t, restart, true, "previous")
			store.fail, bundle.failOnWrite, out.reserveErr = false, false, nil
			m.opts.ManagerSnapshots = nil
			completeSave(t, m, "retry")
			assertLastHistory(t, m, true, "retry")
		})
	}
}

func TestSaveHistoryCommitBoundaryAndSimulatedCrashWindows(t *testing.T) {
	for _, afterDecision := range []bool{false, true} {
		t.Run(fmt.Sprintf("after-decision-%v", afterDecision), func(t *testing.T) {
			store, out, bundle := &historyMemoryStore{}, &historyOutbox{}, newMemStore()
			m := newHistoryManager(t, store, out, bundle)
			completeSave(t, m, "previous")
			check := func(history SaveHistory) {
				if history.Name != testCrash {
					return
				}
				if out.reservations != 2 || out.commits != 1 || !bundle.Exists("fed", testCrash) {
					t.Fatal("decision before reserved fanout and bundle")
				}
				panic("simulated process discontinuity")
			}
			if afterDecision {
				store.after = check
			} else {
				store.before = check
			}
			func() {
				defer func() {
					if recover() == nil {
						t.Fatal("did not hit crash window")
					}
				}()
				completeSave(t, m, testCrash)
			}()
			store.before, store.after = nil, nil
			restart := newHistoryManager(t, store, &historyOutbox{}, bundle)
			if err := restart.RecoverSaveHistory("fed"); err != nil {
				t.Fatal(err)
			}
			want := "previous"
			if afterDecision {
				want = testCrash
			}
			assertLastHistory(t, restart, true, want)
			for _, sent := range out.Sent() {
				if event, ok := sent.Event.(*SavedEvent); ok && event.Label() == testCrash {
					t.Fatal("crash window executed callback")
				}
			}
		})
	}
}

func TestSaveHistoryBrokenGuaranteedCommitIsExplicit(t *testing.T) {
	store, out, bundle := &historyMemoryStore{}, &historyOutbox{}, newMemStore()
	m := newHistoryManager(t, store, out, bundle)
	ctx := context.Background()
	if err := m.RequestFederationSave(ctx, "fed", "decision", nil); err != nil {
		t.Fatal(err)
	}
	if err := m.FederateSaveBegun(ctx, "fed", 1); err != nil {
		t.Fatal(err)
	}
	out.commitErr = errors.New("broken reservation")
	if err := m.FederateSaveComplete(ctx, "fed", 1); !errors.Is(err, out.commitErr) {
		t.Fatalf("commit violation hidden: %v", err)
	}
	assertLastHistory(t, m, true, "decision")
	restart := newHistoryManager(t, store, &historyOutbox{}, bundle)
	if err := restart.RecoverSaveHistory("fed"); err != nil {
		t.Fatal(err)
	}
	assertLastHistory(t, restart, true, "decision")
}

func TestSaveHistoryRestoreDoesNotRewindOrInventHistory(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(fmt.Sprintf("legacy-%v", legacy), func(t *testing.T) {
			store, out, bundle := &historyMemoryStore{}, &historyOutbox{}, newMemStore()
			m := newHistoryManager(t, store, out, bundle)
			completeSave(t, m, "old")
			completeSave(t, m, "latest")
			if legacy {
				store = &historyMemoryStore{}
				m = newHistoryManager(t, store, &historyOutbox{}, bundle)
			}
			for _, succeed := range []bool{false, true} {
				ctx := context.Background()
				if err := m.RequestFederationRestore(ctx, "fed", "old"); err != nil {
					t.Fatal(err)
				}
				var err error
				if succeed {
					err = m.FederateRestoreComplete(ctx, "fed", 1)
				} else {
					err = m.FederateRestoreNotComplete(ctx, "fed", 1)
				}
				if err != nil {
					t.Fatal(err)
				}
				if legacy {
					assertLastHistory(t, m, false, "")
				} else {
					assertLastHistory(t, m, true, "latest")
				}
			}
		})
	}
}

func TestSaveHistoryDurableStoreRejectsLegacyOutbox(t *testing.T) {
	if _, err := New(Options{Outbox: &fakeOutbox{}, BundleStore: newMemStore(), HistoryStore: &historyMemoryStore{}}); err == nil {
		t.Fatal("durable decision accepted fallible callback fanout")
	}
}

func TestSaveHistoryConcurrentQueriesAndCommits(t *testing.T) {
	m := newHistoryManager(t, &historyMemoryStore{}, &historyOutbox{}, newMemStore())
	done := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
					_ = m.QueryMOMMetrics("fed", 1)
				}
			}
		}()
	}
	for i := 0; i < 40; i++ {
		completeSave(t, m, fmt.Sprintf("save-%d", i))
	}
	close(done)
	wg.Wait()
	assertLastHistory(t, m, true, "save-39")
}

type historyLegacyOutbox struct {
	fakeOutbox
	fail bool
}

func (o *historyLegacyOutbox) Send(ctx context.Context, fed core.FederationName, h core.FederateHandle, event core.OutboundEvent) error {
	if _, saved := event.(*SavedEvent); saved && o.fail {
		return errors.New("legacy callback failed")
	}
	return o.fakeOutbox.Send(ctx, fed, h, event)
}

func TestSaveHistoryOptionalStoreAndFallibleLegacyFanout(t *testing.T) {
	out := &historyLegacyOutbox{}
	opts := Options{Outbox: out, BundleStore: newMemStore(), Members: func(core.FederationName) []core.FederateHandle { return []core.FederateHandle{1} }}
	m, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	assertLastHistory(t, m, false, "")
	if err := m.InitializeSaveHistory("fed"); err != nil {
		t.Fatal(err)
	}
	assertLastHistory(t, m, true, "")
	completeSave(t, m, "previous")
	out.fail = true
	ctx := context.Background()
	if err := m.RequestFederationSave(ctx, "fed", "failed", nil); err != nil {
		t.Fatal(err)
	}
	if err := m.FederateSaveBegun(ctx, "fed", 1); err != nil {
		t.Fatal(err)
	}
	if err := m.FederateSaveComplete(ctx, "fed", 1); err == nil {
		t.Fatal("legacy delivery failure hidden")
	}
	assertLastHistory(t, m, true, "previous")
	if m.QuerySaveState("fed", "failed") != StateNotSaved {
		t.Fatal("failed legacy fanout reported successful")
	}
	restart, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := restart.RecoverSaveHistory("fed"); err != nil {
		t.Fatal(err)
	}
	assertLastHistory(t, restart, false, "")
}

func TestSaveHistoryInitializationFailureDoesNotPublishKnownNone(t *testing.T) {
	store := &historyMemoryStore{fail: true}
	m := newHistoryManager(t, store, &historyOutbox{}, newMemStore())
	if err := m.InitializeSaveHistory("fed"); err == nil {
		t.Fatal("creation-time persistence failure hidden")
	}
	assertLastHistory(t, m, false, "")
	store.fail = false
	if err := m.InitializeSaveHistory("fed"); err != nil {
		t.Fatal(err)
	}
	assertLastHistory(t, m, true, "")
}

func TestSaveHistoryGenerationIsolationDestroyAndRecovery(t *testing.T) {
	store, out := &historyMemoryStore{}, &historyOutbox{}
	generation := uint64(1)
	opts := strictOptions(newMemStore(), &generation, nil)
	opts.Outbox, opts.HistoryStore = out, store
	m, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	completeSave(t, m, "first-execution")
	m.OnFederationDestroyed("fed")
	assertLastHistory(t, m, false, "")
	generation = 2
	if err := m.RecoverSaveHistory("fed"); err != nil {
		t.Fatal(err)
	}
	assertLastHistory(t, m, false, "")
	if err := m.InitializeSaveHistory("fed"); err != nil {
		t.Fatal(err)
	}
	assertLastHistory(t, m, true, "")
	generation = 1
	if err := m.RecoverSaveHistory("fed"); err != nil {
		t.Fatal(err)
	}
	assertLastHistory(t, m, true, "first-execution")
}

func TestSaveHistoryCrossGenerationRestoreKeepsTargetDecision(t *testing.T) {
	ctx := context.Background()
	generation := uint64(1)
	handle := core.FederateHandle(1<<32 | 1)
	opts := strictOptions(newMemStore(), &generation, nil)
	opts.Outbox, opts.HistoryStore = &historyOutbox{}, &historyMemoryStore{}
	opts.Members = func(core.FederationName) []core.FederateHandle { return []core.FederateHandle{handle} }
	opts.Roster = func(core.FederationName) []core.FederationMember {
		return []core.FederationMember{{Handle: handle, Name: "one"}}
	}
	opts.RequiredSnapshots = []string{"fom"}
	opts.ManagerSnapshots = map[string]ManagerSnapshotter{"fom": &transitionSnapshot{live: "saved", digest: testFOMDigest}}
	opts.ValidateRestoreSnapshots = func(core.FederationName, Manifest, core.FOMHandle) error { return nil }
	opts.RebindRestore = func(_ context.Context, _ core.FederationName, target uint64, manifest Manifest) (Manifest, error) {
		manifest.FederationGeneration = &target
		manifest.Federates = []core.FederateHandle{handle}
		return manifest, nil
	}
	opts.PrepareRestoreTransition = func(context.Context, core.FederationName, []RestoreParticipant, []core.OutboxDelivery) (core.OutboxReservation, error) {
		return &transitionReservation{commit: func() {}}, nil
	}
	m, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	for _, label := range []string{"source", "target"} {
		if err := m.RequestFederationSave(ctx, "fed", label, nil); err != nil {
			t.Fatal(err)
		}
		if err := m.FederateSaveBegun(ctx, "fed", handle); err != nil {
			t.Fatal(err)
		}
		if err := m.FederateSaveComplete(ctx, "fed", handle); err != nil {
			t.Fatal(err)
		}
		if label == "source" {
			generation = 2
			handle = 2<<32 | 1
			assertLastHistory(t, m, false, "")
		}
	}
	if err := m.RequestFederationRestoreFrom(ctx, "fed", handle, "source", 1); err != nil {
		t.Fatal(err)
	}
	if err := m.FederateRestoreComplete(ctx, "fed", handle); err != nil {
		t.Fatal(err)
	}
	assertLastHistory(t, m, true, "target")
	if err := m.RecoverSaveHistory("fed"); err != nil {
		t.Fatal(err)
	}
	assertLastHistory(t, m, true, "target")
}

const testCrash = "crash"
