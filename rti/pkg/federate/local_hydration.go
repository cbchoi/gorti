package federate

import (
	"context"
	"fmt"
	"sync"
	"time"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
)

type localHydration struct {
	id, handle, epoch, fomEpoch uint64
	identity                    *federateIdentity
	ctx                         context.Context
	reply                       chan struct{}
	state                       *rtiv1.LocalValidationState
	done                        chan error
	once                        sync.Once
}

func (p *localHydration) finish(err error) { p.once.Do(func() { p.done <- err }) }

// ReinitializeLocalLRC rebuilds validation state after restore. The caller must
// suspend its service calls, but continue consuming Events until this returns.
// Other members may keep operating. Earlier queued callbacks are drained before
// the snapshot is installed; later callbacks are applied afterward. Failed or
// timed-out hydration keeps receive-order calls on the confirmed transport.
// Old queued work is never replayed and its AwaitLocalLRC tickets remain valid.
func (f *Federate) ReinitializeLocalLRC(ctx context.Context) error {
	if f == nil || f.conn == nil || f.conn.localLRC == nil || !f.conn.localLRCEnabled || f.handles == nil {
		return ErrLocalLRCUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	f.localHydrationMu.Lock()
	if f.localHydration != nil {
		f.localHydrationMu.Unlock()
		return fmt.Errorf("federate: local validation hydration already in progress")
	}
	if !f.restoreLocalDisabled.Load() {
		f.localHydrationMu.Unlock()
		return nil
	}
	if f.localHydrationID == ^uint64(0) {
		f.localHydrationMu.Unlock()
		return fmt.Errorf("federate: local validation snapshot IDs exhausted")
	}
	f.localHydrationID++
	p := &localHydration{id: f.localHydrationID, handle: f.CurrentHandle(), epoch: f.CallbackEpoch(), identity: f.restoredIdentity.Load(), fomEpoch: f.handles.refreshEpoch.Load(), ctx: ctx, reply: make(chan struct{}), done: make(chan error, 1)}
	f.localHydration = p
	f.localHydrationMu.Unlock()
	defer func() {
		f.localHydrationMu.Lock()
		if f.localHydration == p {
			f.localHydration = nil
		}
		f.localHydrationMu.Unlock()
	}()
	f.localLRCMu.Lock()
	f.retireLocalLRCLocked()
	f.localLRCMu.Unlock()
	state, err := f.conn.localLRC.GetLocalValidationState(f.sessionContext(ctx), &rtiv1.GetLocalValidationStateRequest{WireVersion: rtiv1.WireVersion_WIRE_VERSION_V1, FederationName: f.federationName, FederateHandle: p.handle, ExpectedFederationGeneration: f.federationGeneration, ExpectedCallbackEpoch: p.epoch, SnapshotId: p.id})
	if err != nil {
		return wrapStatusErr(err)
	}
	p.state = state
	close(p.reply)
	select {
	case err := <-p.done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Called only by the single ordered callback drainer, before translation. A
// late or duplicate marker never installs state and never reaches user Events.
func (f *Federate) consumeLocalValidationBarrier(evt *rtiv1.FederateEvent) bool {
	marker := evt.GetLocalValidationBarrier()
	if marker == nil {
		return false
	}
	f.localHydrationMu.Lock()
	p := f.localHydration
	f.localHydrationMu.Unlock()
	if p == nil || marker.SnapshotId != p.id || marker.FederateHandle != p.handle || marker.CallbackEpoch != p.epoch {
		return true
	}
	select {
	case <-p.reply:
	case <-p.ctx.Done():
		p.finish(p.ctx.Err())
		return true
	}
	p.finish(f.installLocalValidationState(p))
	return true
}

func (f *Federate) installLocalValidationState(p *localHydration) error {
	state := p.state
	if !validHydrationSnapshot(p, f.federationGeneration) {
		return fmt.Errorf("federate: local validation snapshot identity mismatch")
	}
	tables, err := tablesFromFOMView(state.FomView)
	if err != nil {
		return err
	}
	knowledge, err := readLocalValidationKnowledge(state, tables)
	if err != nil {
		return err
	}
	// Opening uses the restore transport lifetime so a new restore cancels a
	// blocked handshake before waiting for any SDK state lock.
	lrc, err := openLocalLRC(p.ctx, f)
	if err != nil {
		return wrapStatusErr(err)
	}
	accepted := false
	defer func() {
		if !accepted {
			lrc.fail(context.Canceled)
		}
	}()
	f.localHydrationMu.Lock()
	defer f.localHydrationMu.Unlock()
	if f.localHydration != p || f.restoredIdentity.Load() != p.identity || f.CurrentHandle() != p.handle || f.CallbackEpoch() != p.epoch {
		return fmt.Errorf("federate: hydration superseded by restore")
	}
	if err := p.ctx.Err(); err != nil {
		return err
	}
	f.localLRCMu.Lock()
	defer f.localLRCMu.Unlock()
	if f.localLRCClosing.Load() {
		return ErrLocalLRCClosed
	}
	f.handles.publishMu.Lock()
	if f.handles.refreshEpoch.Load() != p.fomEpoch {
		f.handles.publishMu.Unlock()
		return fmt.Errorf("federate: hydration FOM superseded by restore")
	}
	err = f.handles.install(tables)
	f.handles.publishMu.Unlock()
	if err != nil {
		return err
	}
	f.mu.Lock()
	f.publishedObjectAttributes, f.publishedInteractions = knowledge.publications, knowledge.interactions
	f.objectClasses, f.ownedObjectAttributes = knowledge.objects, knowledge.owners
	f.mu.Unlock()
	lrc.ticketBase = f.localTicketBase
	f.localLRC = lrc
	f.localLRCUnsupported.Store(false)
	f.restoreLocalDisabled.Store(false)
	f.localHydration = nil
	accepted = true
	return nil
}

func (f *Federate) cancelLocalHydration() {
	f.localHydrationMu.Lock()
	defer f.localHydrationMu.Unlock()
	f.restoreLocalDisabled.Store(true)
	if p := f.localHydration; p != nil {
		f.localHydration = nil
		p.finish(fmt.Errorf("federate: hydration superseded by restore"))
	}
}

type retiredLocalLRC struct {
	base, last, acked, failureSequence uint64
	failure                            error
}

func (r retiredLocalLRC) result(ticket uint64) error {
	sequence := ticket - r.base
	if sequence <= r.acked {
		return nil
	}
	if sequence == r.failureSequence {
		return wrapStatusErr(r.failure)
	}
	return fmt.Errorf("%w: %v", ErrLocalLRCIndeterminate, r.failure)
}

func (f *Federate) retireLocalLRCLocked() {
	l := f.localLRC
	if l == nil {
		return
	}
	l.fail(context.Canceled)
	l.admitMu.Lock()
	l.closing = true
	l.closingFlag.Store(true)
	l.stateMu.Lock()
	if l.next > 0 {
		f.retiredLocalLRC = append(f.retiredLocalLRC, retiredLocalLRC{base: l.ticketBase, last: l.ticketBase + l.next, acked: l.acked, failureSequence: l.failureSequence, failure: l.failure})
	}
	f.localTicketBase = l.ticketBase + l.next
	l.stateMu.Unlock()
	l.admitMu.Unlock()
	f.localLRC = nil
}

func (l *localLRC) enqueueTicket(ctx context.Context, build func(uint64) *rtiv1.LocalLRCOperation) (uint64, error) {
	sequence, err := l.enqueue(ctx, build)
	if sequence == 0 {
		return 0, err
	}
	return l.ticketBase + sequence, err
}

type localValidationKnowledge struct {
	publications map[uint64]map[uint64]struct{}
	interactions map[uint64]struct{}
	objects      map[uint64]uint64
	owners       map[uint64]map[uint64]struct{}
}

func validHydrationSnapshot(p *localHydration, generation uint64) bool {
	state := p.state
	return state != nil && state.SnapshotId == p.id && state.FederateHandle == p.handle && state.CallbackEpoch == p.epoch && state.FederationGeneration == generation
}

func invalidLocalValidationState() error {
	return fmt.Errorf("federate: malformed authoritative local validation state")
}

func readLocalValidationAttrs(tables *fomTables, class uint64, attrs []uint64) (map[uint64]struct{}, error) {
	if _, ok := tables.objectCanonical[class]; !ok {
		return nil, invalidLocalValidationState()
	}
	set := make(map[uint64]struct{}, len(attrs))
	for _, a := range attrs {
		if _, ok := tables.attrByHandle[class][a]; !ok {
			return nil, invalidLocalValidationState()
		}
		if _, ok := set[a]; ok {
			return nil, invalidLocalValidationState()
		}
		set[a] = struct{}{}
	}
	return set, nil
}

func readLocalValidationKnowledge(state *rtiv1.LocalValidationState, tables *fomTables) (localValidationKnowledge, error) {
	publications := make(map[uint64]map[uint64]struct{})
	interactions := make(map[uint64]struct{})
	objects := make(map[uint64]uint64)
	owners := make(map[uint64]map[uint64]struct{})
	for _, pub := range state.Publications {
		if pub == nil {
			return localValidationKnowledge{}, invalidLocalValidationState()
		}
		if _, ok := publications[pub.ObjectClassHandle]; ok {
			return localValidationKnowledge{}, invalidLocalValidationState()
		}
		attrs, err := readLocalValidationAttrs(tables, pub.ObjectClassHandle, pub.AttributeHandles)
		if err != nil {
			return localValidationKnowledge{}, err
		}
		publications[pub.ObjectClassHandle] = attrs
	}
	for _, c := range state.PublishedInteractionClasses {
		if _, ok := tables.interactionCanonical[c]; !ok {
			return localValidationKnowledge{}, invalidLocalValidationState()
		}
		if _, ok := interactions[c]; ok {
			return localValidationKnowledge{}, invalidLocalValidationState()
		}
		interactions[c] = struct{}{}
	}
	for _, obj := range state.Objects {
		if obj == nil || obj.ObjectHandle == 0 {
			return localValidationKnowledge{}, invalidLocalValidationState()
		}
		if _, ok := objects[obj.ObjectHandle]; ok {
			return localValidationKnowledge{}, invalidLocalValidationState()
		}
		attrs, err := readLocalValidationAttrs(tables, obj.ObjectClassHandle, obj.OwnedAttributeHandles)
		if err != nil {
			return localValidationKnowledge{}, err
		}
		objects[obj.ObjectHandle] = obj.ObjectClassHandle
		owners[obj.ObjectHandle] = attrs
	}

	return localValidationKnowledge{publications, interactions, objects, owners}, nil
}
