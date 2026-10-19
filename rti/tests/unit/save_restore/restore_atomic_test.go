package save_restore_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/adapters/savebundle/storage"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	savepoint "github.com/cbchoi/gorti/rti/internal/hla/save_restore"
)

type recordedEvent struct {
	handle core.FederateHandle
	event  core.OutboundEvent
}
type outbox struct {
	mu     sync.Mutex
	events []recordedEvent
}

func (o *outbox) Reserve(_ context.Context, _ core.FederationName, deliveries []core.OutboxDelivery) (core.OutboxReservation, error) {
	entries := make([]recordedEvent, len(deliveries))
	for i, d := range deliveries {
		entries[i] = recordedEvent{d.Recipient, d.Event}
	}
	return &recordedReservation{outbox: o, entries: entries}, nil
}

type recordedReservation struct {
	mu      sync.Mutex
	outbox  *outbox
	entries []recordedEvent
	done    bool
}

func (r *recordedReservation) Commit() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.done {
		return nil
	}
	r.outbox.mu.Lock()
	r.outbox.events = append(r.outbox.events, r.entries...)
	r.outbox.mu.Unlock()
	r.done = true
	return nil
}
func (r *recordedReservation) Release() { r.mu.Lock(); r.done = true; r.mu.Unlock() }

func (o *outbox) Send(_ context.Context, _ core.FederationName, h core.FederateHandle, event core.OutboundEvent) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.events = append(o.events, recordedEvent{h, event})
	return nil
}
func (o *outbox) snapshot() []recordedEvent {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]recordedEvent(nil), o.events...)
}
func seed(t *testing.T, store savepoint.Storage, label string, snapshots map[string][]byte) {
	t.Helper()
	w, err := store.Writer("fed", label)
	if err != nil {
		t.Fatal(err)
	}
	if err := savepoint.WriteBundle(w, savepoint.Manifest{Federation: "fed", Label: label, Federates: []core.FederateHandle{1, 2}, ManagerSnapshots: snapshots}, nil); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}
func newStore(t *testing.T) *storage.FSStorage {
	t.Helper()
	s, err := storage.NewFSStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func manager(t *testing.T, s savepoint.Storage, snaps map[string]savepoint.ManagerSnapshotter) (*savepoint.Manager, *outbox) {
	t.Helper()
	o := &outbox{}
	m, err := savepoint.New(savepoint.Options{Outbox: o, BundleStore: s, Members: func(core.FederationName) []core.FederateHandle { return []core.FederateHandle{2, 1} }, ManagerSnapshots: snaps})
	if err != nil {
		t.Fatal(err)
	}
	return m, o
}

type mutableSnapshot struct {
	state      string
	marshalErr error
	failValue  string
	calls      int
}

func (s *mutableSnapshot) Marshal(core.FederationName) ([]byte, error) {
	return []byte(s.state), s.marshalErr
}
func (s *mutableSnapshot) Unmarshal(_ core.FederationName, data []byte) error {
	s.calls++
	s.state = string(data)
	if s.state == s.failValue {
		return errors.New("injected late failure")
	}
	return nil
}

type readerBarrier struct {
	savepoint.Storage
	entered, release chan struct{}
	once             sync.Once
}

func (s *readerBarrier) Reader(fed core.FederationName, label string) (io.ReadCloser, error) {
	s.once.Do(func() { close(s.entered); <-s.release })
	return s.Storage.Reader(fed, label)
}
func await(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("operation did not finish")
		return nil
	}
}
func TestRestoreReservesBeforeReadingOrApplying(t *testing.T) {
	s := newStore(t)
	seed(t, s, "old", map[string][]byte{"state": []byte(savedStateValue)})
	barrier := &readerBarrier{Storage: s, entered: make(chan struct{}), release: make(chan struct{})}
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(barrier.release) }) })
	snap := &mutableSnapshot{state: liveStateValue}
	m, _ := manager(t, barrier, map[string]savepoint.ManagerSnapshotter{"state": snap})
	done := make(chan error, 1)
	go func() { done <- m.RequestFederationRestore(context.Background(), "fed", "old") }()
	select {
	case <-barrier.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("reader not reached")
	}
	if got := m.QueryRestoreState("fed", "old"); got != savepoint.RestoreLoading {
		t.Fatalf("state = %v", got)
	}
	second := make(chan error, 1)
	go func() { second <- m.RequestFederationRestore(context.Background(), "fed", "old") }()
	if err := await(t, second); !errors.Is(err, core.ErrRestoreAlreadyInProgress) {
		t.Fatalf("competing restore: %v", err)
	}
	if snap.calls != 0 || snap.state != liveStateValue {
		t.Fatal("competing request changed live state")
	}
	release.Do(func() { close(barrier.release) })
	if err := await(t, done); err != nil {
		t.Fatal(err)
	}
	if snap.calls != 1 || snap.state != savedStateValue {
		t.Fatalf("snapshot applied %d times: %q", snap.calls, snap.state)
	}
}
func TestRestoreLateFailureRollsBackEveryAttemptedSnapshot(t *testing.T) {
	s := newStore(t)
	seed(t, s, "bad", map[string][]byte{"a": []byte("saved-a"), "z": []byte("bad-z")})
	a, z := &mutableSnapshot{state: "live-a"}, &mutableSnapshot{state: "live-z", failValue: "bad-z"}
	m, o := manager(t, s, map[string]savepoint.ManagerSnapshotter{"a": a, "z": z})
	err := m.RequestFederationRestoreBy(context.Background(), "fed", 1, "bad")
	if err == nil || !strings.Contains(err.Error(), "injected late failure") {
		t.Fatalf("restore error: %v", err)
	}
	if a.state != "live-a" || z.state != "live-z" || a.calls != 2 || z.calls != 2 {
		t.Fatalf("rollback failed: a=%+v z=%+v", a, z)
	}
	if got := m.QueryRestoreState("fed", "bad"); got != savepoint.RestoreFailed {
		t.Fatalf("state = %v", got)
	}
	for _, e := range o.snapshot() {
		if _, ok := e.event.(*savepoint.RestoreRequestFailedEvent); !ok {
			t.Fatalf("unexpected success callback: %T", e.event)
		}
	}
	if err := m.RequestFederationSave(context.Background(), "fed", "next", nil); err != nil {
		t.Fatalf("failed restore retained reservation: %v", err)
	}
}
func TestRestoreRollbackCaptureFailureNeverMutates(t *testing.T) {
	for _, empty := range []bool{false, true} {
		t.Run(map[bool]string{false: "marshal-error", true: "empty-image"}[empty], func(t *testing.T) {
			s := newStore(t)
			seed(t, s, "bad", map[string][]byte{"a": []byte("saved-a"), "z": []byte("saved-z")})
			a, z := &mutableSnapshot{state: "live-a"}, &mutableSnapshot{state: "live-z", marshalErr: errors.New("capture failed")}
			if empty {
				z.state, z.marshalErr = "", nil
			}
			m, _ := manager(t, s, map[string]savepoint.ManagerSnapshotter{"a": a, "z": z})
			if err := m.RequestFederationRestore(context.Background(), "fed", "bad"); err == nil {
				t.Fatal("unsafe restore accepted")
			}
			if a.calls != 0 || z.calls != 0 {
				t.Fatal("mutation before all rollback images captured")
			}
		})
	}
}
