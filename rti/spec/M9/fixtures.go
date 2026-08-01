package m9spec

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"

	"github.com/cbchoi/gorti/rti/internal/core"
	savepkg "github.com/cbchoi/gorti/rti/internal/savepoint"
)

// fakeOutbox + permissiveEventLog mirror prior milestones' fixtures.

type sentRecord struct {
	Federation core.FederationName
	Federate   core.FederateHandle
	Event      core.OutboundEvent
}

type fakeOutbox struct {
	mu   sync.Mutex
	sent []sentRecord
}

func newFakeOutbox() *fakeOutbox { return &fakeOutbox{} }

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

type permissiveEventLog struct {
	mu      sync.Mutex
	nextSeq uint64
}

func newPermissiveEventLog() *permissiveEventLog { return &permissiveEventLog{} }

func (l *permissiveEventLog) Append(_ context.Context, _ core.FederationName, _ core.EventRecord) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.nextSeq++
	return nil
}

func (*permissiveEventLog) Sync(_ context.Context, _ core.FederationName) error { return nil }

func (*permissiveEventLog) OpenReader(_ context.Context, _ string) (core.EventLogReader, error) {
	return nil, errors.New("permissiveEventLog: OpenReader not supported in fixtures")
}

// inMemStorage is an in-memory savepoint.Storage for spec tests. Maps
// (fed, label) → bytes; goroutine-safe.

type inMemStorageKey struct {
	fed   core.FederationName
	label string
}

type inMemStorage struct {
	mu      sync.Mutex
	bundles map[inMemStorageKey][]byte
}

func newInMemStorage() *inMemStorage {
	return &inMemStorage{bundles: map[inMemStorageKey][]byte{}}
}

func (s *inMemStorage) Writer(fed core.FederationName, label string) (io.WriteCloser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.bundles[inMemStorageKey{fed, label}]; exists {
		return nil, savepkg.ErrSaveBundleExists
	}
	return &inMemBundleWriter{store: s, key: inMemStorageKey{fed, label}}, nil
}

func (s *inMemStorage) Reader(fed core.FederationName, label string) (io.ReadCloser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, ok := s.bundles[inMemStorageKey{fed, label}]
	if !ok {
		return nil, savepkg.ErrSaveBundleNotFound
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (s *inMemStorage) Exists(fed core.FederationName, label string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.bundles[inMemStorageKey{fed, label}]
	return ok
}

type inMemBundleWriter struct {
	store *inMemStorage
	key   inMemStorageKey
	buf   bytes.Buffer
}

func (w *inMemBundleWriter) Write(p []byte) (int, error) { return w.buf.Write(p) }

func (w *inMemBundleWriter) Close() error {
	w.store.mu.Lock()
	defer w.store.mu.Unlock()
	w.store.bundles[w.key] = append([]byte(nil), w.buf.Bytes()...)
	return nil
}
