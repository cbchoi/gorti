package savepoint

import (
	"bytes"
	"context"
	"errors"
	"io"
	gosync "sync"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// fakeOutbox / permissiveLog / memStore are local fixtures duplicated
// here (rather than imported from rti/spec/M9/) to keep the spec
// fixtures package free of test-deps and to let this internal test
// exercise edge cases the spec tests don't (writer-error injection,
// closed-out idempotency, etc.).

type fakeOutbox struct {
	mu   gosync.Mutex
	sent []sentRecord
}

type sentRecord struct {
	Federation core.FederationName
	Federate   core.FederateHandle
	Event      core.OutboundEvent
}

func (o *fakeOutbox) Send(_ context.Context, fed core.FederationName, h core.FederateHandle, evt core.OutboundEvent) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.sent = append(o.sent, sentRecord{fed, h, evt})
	return nil
}

func (o *fakeOutbox) Sent() []sentRecord {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]sentRecord, len(o.sent))
	copy(out, o.sent)
	return out
}

type permissiveLog struct{}

func (permissiveLog) Append(_ context.Context, _ core.FederationName, _ core.EventRecord) error {
	return nil
}
func (permissiveLog) Sync(_ context.Context, _ core.FederationName) error { return nil }
func (permissiveLog) OpenReader(_ context.Context, _ string) (core.EventLogReader, error) {
	return nil, errors.New("not supported")
}

type memStoreKey struct {
	fed   core.FederationName
	label string
}

type memStore struct {
	mu      gosync.Mutex
	bundles map[memStoreKey][]byte
	// failOnWrite, when true, makes the next Writer call return io.ErrClosedPipe.
	failOnWrite bool
}

func newMemStore() *memStore { return &memStore{bundles: map[memStoreKey][]byte{}} }

func (s *memStore) Writer(fed core.FederationName, label string) (io.WriteCloser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failOnWrite {
		return nil, io.ErrClosedPipe
	}
	if _, exists := s.bundles[memStoreKey{fed, label}]; exists {
		return nil, ErrSaveBundleExists
	}
	return &memBundleWriter{store: s, key: memStoreKey{fed, label}}, nil
}

func (s *memStore) Reader(fed core.FederationName, label string) (io.ReadCloser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, ok := s.bundles[memStoreKey{fed, label}]
	if !ok {
		return nil, ErrSaveBundleNotFound
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (s *memStore) Exists(fed core.FederationName, label string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.bundles[memStoreKey{fed, label}]
	return ok
}

type memBundleWriter struct {
	store *memStore
	key   memStoreKey
	buf   bytes.Buffer
}

func (w *memBundleWriter) Write(p []byte) (int, error) { return w.buf.Write(p) }
func (w *memBundleWriter) Close() error {
	w.store.mu.Lock()
	defer w.store.mu.Unlock()
	w.store.bundles[w.key] = append([]byte(nil), w.buf.Bytes()...)
	return nil
}

// --- Tests ---------------------------------------------------------------

func TestNew_RejectsMissingDeps(t *testing.T) {
	_, err := New(Options{})
	if err == nil {
		t.Errorf("New with empty Options succeeded; expected error")
	}
	_, err = New(Options{Outbox: &fakeOutbox{}})
	if err == nil {
		t.Errorf("New without BundleStore succeeded; expected error")
	}
}

func TestRequestSave_HaltedFederation(t *testing.T) {
	mgr, err := New(Options{
		Outbox:      &fakeOutbox{},
		BundleStore: newMemStore(),
		Halted: func(_ core.FederationName) bool {
			return true
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	err = mgr.RequestFederationSave(context.Background(), "fed", "lbl", nil)
	if !errors.Is(err, core.ErrFederationHalted) {
		t.Errorf("err = %v, want ErrFederationHalted", err)
	}
}

func TestSave_BundleWriteFailureFlipsToNotSaved(t *testing.T) {
	store := newMemStore()
	store.failOnWrite = true
	outbox := &fakeOutbox{}
	mgr, err := New(Options{
		Outbox:      outbox,
		BundleStore: store,
		EventLog:    permissiveLog{},
		Members:     func(core.FederationName) []core.FederateHandle { return []core.FederateHandle{1} },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	if err := mgr.RequestFederationSave(ctx, "fed", "lbl", nil); err != nil {
		t.Fatalf("RequestFederationSave: %v", err)
	}
	if err := mgr.FederateSaveComplete(ctx, "fed", 1); err != nil {
		t.Fatalf("FederateSaveComplete: %v", err)
	}
	if got := mgr.QuerySaveState("fed", "lbl"); got != StateNotSaved {
		t.Errorf("QuerySaveState = %v, want StateNotSaved (write-failure flips outcome)", got)
	}
}

func TestSave_QuerySaveStateAcrossLifecycle(t *testing.T) {
	store := newMemStore()
	mgr, err := New(Options{
		Outbox:      &fakeOutbox{},
		BundleStore: store,
		EventLog:    permissiveLog{},
		Members:     func(core.FederationName) []core.FederateHandle { return []core.FederateHandle{1, 2} },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	if got := mgr.QuerySaveState("fed", "lbl"); got != StateIdle {
		t.Errorf("pre-request QuerySaveState = %v, want StateIdle", got)
	}
	if err := mgr.RequestFederationSave(ctx, "fed", "lbl", nil); err != nil {
		t.Fatalf("RequestFederationSave: %v", err)
	}
	if got := mgr.QuerySaveState("fed", "lbl"); got != StateInitiated {
		t.Errorf("after-request QuerySaveState = %v, want StateInitiated", got)
	}
	_ = mgr.FederateSaveComplete(ctx, "fed", 1)
	if got := mgr.QuerySaveState("fed", "lbl"); got != StateInitiated {
		t.Errorf("partial QuerySaveState = %v, want StateInitiated", got)
	}
	_ = mgr.FederateSaveComplete(ctx, "fed", 2)
	if got := mgr.QuerySaveState("fed", "lbl"); got != StateSaved {
		t.Errorf("final QuerySaveState = %v, want StateSaved", got)
	}
}

func TestRecordFederateSave_NotInProgress(t *testing.T) {
	mgr, err := New(Options{Outbox: &fakeOutbox{}, BundleStore: newMemStore()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := mgr.FederateSaveComplete(context.Background(), "fed", 1); !errors.Is(err, core.ErrFederateNotInSave) {
		t.Errorf("err = %v, want ErrFederateNotInSave", err)
	}
}

func TestRecordFederateSave_NotInRequiredSet(t *testing.T) {
	mgr, err := New(Options{
		Outbox:      &fakeOutbox{},
		BundleStore: newMemStore(),
		Members:     func(core.FederationName) []core.FederateHandle { return []core.FederateHandle{1} },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	_ = mgr.RequestFederationSave(ctx, "fed", "lbl", nil)
	if err := mgr.FederateSaveComplete(ctx, "fed", 99); !errors.Is(err, core.ErrFederateNotInSave) {
		t.Errorf("err = %v, want ErrFederateNotInSave", err)
	}
}

func TestSaveTime_Captured(t *testing.T) {
	store := newMemStore()
	mgr, err := New(Options{
		Outbox:      &fakeOutbox{},
		BundleStore: store,
		Members:     func(core.FederationName) []core.FederateHandle { return []core.FederateHandle{1} },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	saveTime := core.LogicalTime(42.5)
	if err := mgr.RequestFederationSave(ctx, "fed", "lbl", &saveTime); err != nil {
		t.Fatalf("RequestFederationSave: %v", err)
	}
	if err := mgr.FederateSaveComplete(ctx, "fed", 1); err != nil {
		t.Fatalf("FederateSaveComplete: %v", err)
	}
	manifest, err := mgr.LoadManifest("fed", "lbl")
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if manifest.SaveTime == nil || *manifest.SaveTime != saveTime {
		t.Errorf("manifest.SaveTime = %v, want %v", manifest.SaveTime, saveTime)
	}
}
