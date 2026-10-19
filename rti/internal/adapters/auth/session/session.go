// Package session authenticates session-scoped controls independently of wire
// handles, which can change during federation restore.
package session

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"sync"
	"time"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const MetadataKey = "gorti-session-token-bin"
const TokenSize = 32

type Membership interface {
	MemberIdentity(core.FederationName, core.FederateHandle) (core.MemberIdentity, error)
	AcquireMember(core.FederationName, core.FederateHandle) (func(), error)
	AutomaticResignFederation(context.Context, core.MemberIdentity, func() bool) error
}

type Options struct {
	Membership     Membership
	Grace          time.Duration
	OnError        func(error)
	AcquireService func(core.FederationName) (func(), error)
	// AcquireLoss admits detected loss even during a save, but may defer restore.
	AcquireLoss func(core.FederationName) (func(), error)
}

type key struct {
	federation core.FederationName
	generation uint64
	name       string
}
type entry struct {
	identity core.MemberIdentity
	token    [TokenSize]byte
	host     string
	active   int
	revision uint64
	timer    *time.Timer
}

type Manager struct {
	mu      sync.Mutex
	entries map[key]*entry
	opts    Options
	closed  bool
}

func New(opts Options) (*Manager, error) {
	if opts.Membership == nil {
		return nil, errors.New("session: membership is required")
	}
	if opts.Grace == 0 {
		opts.Grace = 5 * time.Second
	}
	if opts.Grace < 0 {
		return nil, errors.New("session: negative reconnect grace")
	}
	return &Manager{opts: opts, entries: make(map[key]*entry)}, nil
}

func NewToken() ([]byte, error) {
	token := make([]byte, TokenSize)
	if _, err := rand.Read(token); err != nil {
		return nil, err
	}
	return token, nil
}

func identityKey(id core.MemberIdentity) key { return key{id.Federation, id.Generation, id.Name} }

// Register must run inside the join operation gate. Tokens never enter domain
// state, snapshots, journals or callback payloads.
func (m *Manager) Register(id core.MemberIdentity, token []byte) {
	m.RegisterWithHost(id, token, "")
}

// RegisterWithHost accepts a host explicitly reported and validated at join.
// Host metadata belongs to this live session, not to restored saved state.
func (m *Manager) RegisterWithHost(id core.MemberIdentity, token []byte, host string) {
	if len(token) != TokenSize {
		panic("session: invalid token length")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return
	}
	k := identityKey(id)
	if old := m.entries[k]; old != nil && old.timer != nil {
		old.timer.Stop()
	}
	next := &entry{identity: id, host: host}
	copy(next.token[:], token)
	m.entries[k] = next
}

func tokenFromContext(ctx context.Context) ([]byte, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	values := md.Get(MetadataKey)
	if len(values) != 1 || len(values[0]) != TokenSize {
		return nil, status.Error(codes.Unauthenticated, "valid session token is required")
	}
	return []byte(values[0]), nil
}

func (m *Manager) authenticatedEntry(ctx context.Context, id core.MemberIdentity) (*entry, error) {
	token, err := tokenFromContext(ctx)
	if err != nil {
		return nil, err
	}
	e := m.entries[identityKey(id)]
	if m.closed || e == nil || e.identity.Incarnation != id.Incarnation || subtle.ConstantTimeCompare(e.token[:], token) != 1 {
		return nil, status.Error(codes.PermissionDenied, "session token does not match this membership")
	}
	return e, nil
}

// Authenticate requires the caller to hold the operation lease appropriate to
// its mutation. This method deliberately does not recursively acquire one.
func (m *Manager) Authenticate(ctx context.Context, fed core.FederationName, h core.FederateHandle) error {
	id, err := m.opts.Membership.MemberIdentity(fed, h)
	if err != nil {
		return status.Error(codes.FailedPrecondition, "membership is no longer active")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	_, err = m.authenticatedEntry(ctx, id)
	return err
}

// AuthenticateIdentity captures a live membership for a subsequent exclusive
// operation. The returned identity must be revalidated by that operation; the
// shared membership lease is released before the caller acquires its write gate.
func (m *Manager) AuthenticateIdentity(ctx context.Context, fed core.FederationName, h core.FederateHandle) (core.MemberIdentity, error) {
	release, err := m.opts.Membership.AcquireMember(fed, h)
	if err != nil {
		return core.MemberIdentity{}, err
	}
	if release == nil {
		return core.MemberIdentity{}, status.Error(codes.FailedPrecondition, "session identity requires a retained membership lease")
	}
	defer release()
	id, err := m.opts.Membership.MemberIdentity(fed, h)
	if err != nil {
		return core.MemberIdentity{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := m.authenticatedEntry(ctx, id); err != nil {
		return core.MemberIdentity{}, err
	}
	return id, nil
}

// Open observes only authenticated callback streams. Legacy streams remain
// supported but cannot cancel another session's disconnection timer.
func (m *Manager) Open(ctx context.Context, fed core.FederationName, h core.FederateHandle) (func(), error) {
	md, _ := metadata.FromIncomingContext(ctx)
	if len(md.Get(MetadataKey)) == 0 {
		return func() {}, nil
	}
	release, err := m.opts.Membership.AcquireMember(fed, h)
	if err != nil {
		return nil, err
	}
	defer release()
	id, err := m.opts.Membership.MemberIdentity(fed, h)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	e, err := m.authenticatedEntry(ctx, id)
	if err != nil {
		m.mu.Unlock()
		return nil, err
	}
	e.active++
	e.revision++
	if e.timer != nil {
		e.timer.Stop()
		e.timer = nil
	}
	m.mu.Unlock()
	var once sync.Once
	return func() { once.Do(func() { m.closeStream(e) }) }, nil
}

func (m *Manager) closeStream(e *entry) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.entries[identityKey(e.identity)] != e {
		return
	}
	e.active--
	e.revision++
	if e.active != 0 {
		return
	}
	revision := e.revision
	m.scheduleLocked(e, revision)
}

func (m *Manager) scheduleLocked(e *entry, revision uint64) {
	e.timer = time.AfterFunc(m.opts.Grace, func() { m.expireDisconnected(e, revision) })
}

func (m *Manager) disconnectedLocked(e *entry, revision uint64) bool {
	return !m.closed && m.entries[identityKey(e.identity)] == e && e.active == 0 && e.revision == revision
}

func (m *Manager) rescheduleDisconnected(e *entry, revision uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.disconnectedLocked(e, revision) {
		m.scheduleLocked(e, revision)
	}
}

func (m *Manager) acquireLoss(fed core.FederationName) (func(), error) {
	acquire := m.opts.AcquireLoss
	if acquire == nil {
		acquire = m.opts.AcquireService
	}
	if acquire == nil {
		return nil, nil
	}
	return acquire(fed)
}

func (m *Manager) expireDisconnected(e *entry, revision uint64) {
	condition := func() bool {
		m.mu.Lock()
		defer m.mu.Unlock()
		return m.disconnectedLocked(e, revision)
	}
	if !condition() {
		return
	}
	// Never hold the session lock while acquiring the federation operation gate.
	release, err := m.acquireLoss(e.identity.Federation)
	if err != nil {
		if errors.Is(err, core.ErrSaveAlreadyInProgress) || errors.Is(err, core.ErrRestoreAlreadyInProgress) {
			m.rescheduleDisconnected(e, revision)
		} else if m.opts.OnError != nil {
			m.opts.OnError(err)
		}
		return
	}
	if release != nil {
		defer release()
	}
	err = m.opts.Membership.AutomaticResignFederation(context.Background(), e.identity, condition)
	if err != nil && !errors.Is(err, core.ErrFederateNotJoined) && !errors.Is(err, core.ErrFederationNotFound) && m.opts.OnError != nil {
		m.opts.OnError(err)
	}
	if errors.Is(err, core.ErrFederateOverflow) || errors.Is(err, core.ErrOutboxUnavailable) {
		m.rescheduleDisconnected(e, revision)
	}
}

func (m *Manager) ClearFederate(fed core.FederationName, h core.FederateHandle) {
	// Call before the roster entry is removed; restore can change its handle.
	if id, err := m.opts.Membership.MemberIdentity(fed, h); err == nil {
		m.ClearIdentity(id)
	}
}

func (m *Manager) ClearIdentity(id core.MemberIdentity) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := identityKey(id)
	e := m.entries[k]
	if e != nil && e.identity.Incarnation == id.Incarnation {
		if e.timer != nil {
			e.timer.Stop()
		}
		delete(m.entries, k)
	}
}

func (m *Manager) ClearFederation(fed core.FederationName) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, e := range m.entries {
		if k.federation == fed {
			if e.timer != nil {
				e.timer.Stop()
			}
			delete(m.entries, k)
		}
	}
}

func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	for _, e := range m.entries {
		if e.timer != nil {
			e.timer.Stop()
		}
	}
	clear(m.entries)
}
