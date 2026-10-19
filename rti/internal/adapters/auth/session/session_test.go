package session_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/adapters/auth/session"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type members struct {
	gate sync.RWMutex
	mu   sync.Mutex
	id   core.MemberIdentity
	gone chan struct{}
}

func (m *members) MemberIdentity(_ core.FederationName, h core.FederateHandle) (core.MemberIdentity, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.id.Handle != h {
		return core.MemberIdentity{}, core.ErrFederateNotJoined
	}
	return m.id, nil
}
func (m *members) AcquireMember(_ core.FederationName, h core.FederateHandle) (func(), error) {
	m.gate.RLock()
	if _, err := m.MemberIdentity("f", h); err != nil {
		m.gate.RUnlock()
		return nil, err
	}
	return m.gate.RUnlock, nil
}
func (m *members) AutomaticResignFederation(_ context.Context, id core.MemberIdentity, condition func() bool) error {
	m.gate.Lock()
	defer m.gate.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.id.Incarnation != id.Incarnation {
		return core.ErrFederateNotJoined
	}
	if condition() {
		m.id.Handle = 0
		close(m.gone)
	}
	return nil
}
func setup(t *testing.T) (*session.Manager, *members, context.Context, []byte) {
	t.Helper()
	mm := &members{id: core.MemberIdentity{Federation: "f", Generation: 2, Name: "a", Handle: 1, Incarnation: 1}, gone: make(chan struct{})}
	mgr, err := session.New(session.Options{Membership: mm, Grace: 40 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mgr.Close)
	token, err := session.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	mgr.Register(mm.id, token)
	return mgr, mm, metadata.NewIncomingContext(context.Background(), metadata.Pairs(session.MetadataKey, string(token))), token
}
func TestTokenRejectsMissingMalformedAndWrongMember(t *testing.T) {
	mgr, _, ctx, token := setup(t)
	if err := mgr.Authenticate(ctx, "f", 1); err != nil {
		t.Fatal(err)
	}
	if status.Code(mgr.Authenticate(context.Background(), "f", 1)) != codes.Unauthenticated {
		t.Fatal("missing token accepted")
	}
	token[0] ^= 1
	wrong := metadata.NewIncomingContext(context.Background(), metadata.Pairs(session.MetadataKey, string(token)))
	if status.Code(mgr.Authenticate(wrong, "f", 1)) != codes.PermissionDenied {
		t.Fatal("wrong token accepted")
	}
}
func TestDisconnectResignsOnlyAfterLastAuthenticatedStream(t *testing.T) {
	mgr, mm, ctx, _ := setup(t)
	a, err := mgr.Open(ctx, "f", 1)
	if err != nil {
		t.Fatal(err)
	}
	b, err := mgr.Open(ctx, "f", 1)
	if err != nil {
		t.Fatal(err)
	}
	a()
	a()
	select {
	case <-mm.gone:
		t.Fatal("another stream active")
	case <-time.After(70 * time.Millisecond):
	}
	b()
	select {
	case <-mm.gone:
	case <-time.After(time.Second):
		t.Fatal("no automatic resign")
	}
}
func TestReconnectCancelsStaleTimerAndRetainsTokenAcrossRebind(t *testing.T) {
	mgr, mm, ctx, _ := setup(t)
	closeOld, err := mgr.Open(ctx, "f", 1)
	if err != nil {
		t.Fatal(err)
	}
	closeOld()
	mm.mu.Lock()
	mm.id.Handle = 8
	mm.mu.Unlock()
	closeNew, err := mgr.Open(ctx, "f", 8)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-mm.gone:
		t.Fatal("reconnected session resigned")
	case <-time.After(90 * time.Millisecond):
	}
	if err := mgr.Authenticate(ctx, "f", 8); err != nil {
		t.Fatal(err)
	}
	closeNew()
	select {
	case <-mm.gone:
	case <-time.After(time.Second):
		t.Fatal("rebound session did not resign")
	}
}
func TestRejoinRotatesTokenAndInvalidatesOldStreamClose(t *testing.T) {
	mgr, mm, old, _ := setup(t)
	closeOld, err := mgr.Open(old, "f", 1)
	if err != nil {
		t.Fatal(err)
	}
	mm.mu.Lock()
	mm.id.Handle = 2
	mm.id.Incarnation = 2
	id := mm.id
	mm.mu.Unlock()
	token, _ := session.NewToken()
	mgr.Register(id, token)
	closeOld()
	if status.Code(mgr.Authenticate(old, "f", 2)) != codes.PermissionDenied {
		t.Fatal("old token accepted")
	}
	select {
	case <-mm.gone:
		t.Fatal("old close resigned rejoin")
	case <-time.After(90 * time.Millisecond):
	}
}
func TestLegacyStreamDoesNotArmAutomaticCleanup(t *testing.T) {
	mgr, mm, _, _ := setup(t)
	done, err := mgr.Open(context.Background(), "f", 1)
	if err != nil {
		t.Fatal(err)
	}
	done()
	select {
	case <-mm.gone:
		t.Fatal("legacy stream armed timer")
	case <-time.After(70 * time.Millisecond):
	}
}

func TestAutomaticResignWaitsForCheckpointCompletion(t *testing.T) {
	mm := &members{id: core.MemberIdentity{Federation: "f", Generation: 2, Name: "a", Handle: 1, Incarnation: 1}, gone: make(chan struct{})}
	var saving atomic.Bool
	saving.Store(true)
	mgr, err := session.New(session.Options{Membership: mm, Grace: 15 * time.Millisecond, AcquireService: func(core.FederationName) (func(), error) {
		if saving.Load() {
			return nil, core.ErrSaveAlreadyInProgress
		}
		return func() {}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Close()
	token, _ := session.NewToken()
	mgr.Register(mm.id, token)
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(session.MetadataKey, string(token)))
	closeStream, err := mgr.Open(ctx, "f", 1)
	if err != nil {
		t.Fatal(err)
	}
	closeStream()
	select {
	case <-mm.gone:
		t.Fatal("cleanup crossed active checkpoint")
	case <-time.After(60 * time.Millisecond):
	}
	saving.Store(false)
	select {
	case <-mm.gone:
	case <-time.After(time.Second):
		t.Fatal("cleanup not resumed after checkpoint")
	}
}
