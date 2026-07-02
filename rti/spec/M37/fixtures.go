// Package m37spec — M37 Agent EA spec tests for the additive
// FederateEvent proto-slot batch (docs/PITCH_PARITY.md "M37 backlog").
//
// Fixture style follows rti/spec/M36: in-process manager composition
// with a recording fakeOutbox; events are asserted by unwrapping the
// carrier's Inner() *rtiv1.FederateEvent.
package m37spec

import (
	"bytes"
	"context"
	"io"
	"sync"

	"github.com/cbchoi/gorti/rti/internal/core"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/savepoint"
)

// fakeOutbox records every Send for assertion. Goroutine-safe.
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

// SentTo filters the recorded sends down to one recipient federate and
// unwraps the inner FederateEvent protos.
func (o *fakeOutbox) SentTo(h core.FederateHandle) []*rtiv1.FederateEvent {
	var out []*rtiv1.FederateEvent
	for _, rec := range o.Sent() {
		if rec.Federate != h {
			continue
		}
		if carrier, ok := rec.Event.(interface{ Inner() *rtiv1.FederateEvent }); ok {
			out = append(out, carrier.Inner())
		}
	}
	return out
}

// Reset drops the recorded sends (so tests can slice per-phase).
func (o *fakeOutbox) Reset() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.sent = nil
}

// memStore is an in-memory savepoint.Storage (mirrors the internal
// savepoint test fixture).
type memStoreKey struct {
	fed   core.FederationName
	label string
}

type memStore struct {
	mu      sync.Mutex
	bundles map[memStoreKey][]byte
}

func newMemStore() *memStore { return &memStore{bundles: map[memStoreKey][]byte{}} }

func (s *memStore) Writer(fed core.FederationName, label string) (io.WriteCloser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.bundles[memStoreKey{fed, label}]; exists {
		return nil, savepoint.ErrSaveBundleExists
	}
	return &memBundleWriter{store: s, key: memStoreKey{fed, label}}, nil
}

func (s *memStore) Reader(fed core.FederationName, label string) (io.ReadCloser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, ok := s.bundles[memStoreKey{fed, label}]
	if !ok {
		return nil, savepoint.ErrSaveBundleNotFound
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
