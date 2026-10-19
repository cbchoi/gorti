package federate

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type federateIdentity struct {
	handle               uint64
	epoch                uint64
	nextRetractionHandle uint64
}

type federateTransport struct {
	ctx    context.Context
	cancel context.CancelFunc
}

// CurrentHandle returns the current server identity, including a restored
// identity. The original join handle is immutable. Applications must suspend
// service calls during restore until FederationRestored is consumed; this
// accessor does not make pre-restore object or application state valid again.
func (f *Federate) CurrentHandle() uint64 {
	if f == nil {
		return 0
	}
	if identity := f.restoredIdentity.Load(); identity != nil {
		return identity.handle
	}
	return f.federateHandle
}

// CallbackEpoch reports the most recently installed restore callback epoch.
// Zero means that the server has not supplied one.
func (f *Federate) CallbackEpoch() uint64 {
	if f == nil {
		return 0
	}
	if identity := f.restoredIdentity.Load(); identity != nil {
		return identity.epoch
	}
	return 0
}

// NextRetractionHandle reports the server's allocation recommendation from the
// latest restore transition. It does not allocate, reserve, or advance an
// ID. Unknown (including a legacy zero value) returns false. Callers still own
// allocation; the server rejects exact reuse, not unique nonmonotonic IDs.
func (f *Federate) NextRetractionHandle() (uint64, bool) {
	if f != nil {
		if identity := f.restoredIdentity.Load(); identity != nil {
			return identity.nextRetractionHandle, identity.nextRetractionHandle != 0
		}
	}
	return 0, false
}

// ErrLocalLRCRestoreStateUnavailable means the SDK no longer has an
// authoritative local declaration/ownership mirror. After restore, standard
// calls use confirmed transport; explicit queue admission stays disabled for
// this joined Federate. Re-publication alone cannot reconstruct ownership.
var ErrLocalLRCRestoreStateUnavailable = errors.New("federate: LocalLRC disabled after restore; use confirmed operations because restored declaration and ownership state is not available locally")

// RestoreStateError is a local SDK synchronization failure, not an RTI restore
// failure callback. The callback channel closes after this event. Stop using
// the Federate and resign/rejoin; no successful FederationRestored is emitted.
type RestoreStateError struct {
	Label string
	Err   error
}

func (RestoreStateError) isFederateEvent() {}
func (e RestoreStateError) Error() string {
	return fmt.Sprintf("federate: restore %q local synchronization failed: %v", e.Label, e.Err)
}
func (e RestoreStateError) Unwrap() error { return e.Err }

func (f *Federate) transportLifetime() *federateTransport {
	if current := f.restoreTransport.Load(); current != nil {
		return current
	}
	parent := f.interactionContext
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	candidate := &federateTransport{ctx: ctx, cancel: cancel}
	if f.restoreTransport.CompareAndSwap(nil, candidate) {
		return candidate
	}
	cancel()
	return f.restoreTransport.Load()
}

// Applications must suspend service calls during restore, until they consume
// FederationRestored. This barrier refreshes SDK metadata, not application
// state or a replacement declaration/ownership mirror.
func (f *Federate) prepareRestoredCallback(ctx context.Context, event Event) (Event, bool) {
	if info, _ := runtimeInfo(event); info.recovered {
		return event, true
	}
	if initiated, ok := event.(InitiateFederateRestore); ok {
		if err := f.stageRuntimeRestore(initiated); err != nil {
			return RestoreStateError{Label: initiated.Label, Err: err}, false
		}
		return event, true
	}
	switch event.(type) {
	case FederationRestoreBegun, RequestFederationRestoreSucceeded:
		f.resetRuntimeRestoreReadiness(true)
	case FederationNotRestored, RequestFederationRestoreFailed:
		f.resetRuntimeRestoreReadiness(false)
	}
	restored, ok := event.(FederationRestored)
	if !ok {
		return event, true
	}
	f.checkpointMu.Lock()
	f.runtimeRestoreReady = false
	stagingErr := f.runtimeRestoreError
	f.checkpointMu.Unlock()
	defer f.resetRuntimeRestoreReadiness(false)
	if stagingErr != nil {
		return RestoreStateError{Label: restored.Label, Err: stagingErr}, false
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := f.applyRestoredState(ctx, restored); err != nil {
		return RestoreStateError{Label: restored.Label, Err: err}, false
	}
	f.checkpointMu.Lock()
	restored.CallbackInfo.runtimeOwner, restored.CallbackInfo.runtimeEpoch = f, f.runtimeEpoch
	f.checkpointMu.Unlock()
	return restored, true
}

func (f *Federate) applyRestoredState(ctx context.Context, restored FederationRestored) error {
	identity, err := f.restoredCallbackIdentity(restored)
	if err != nil {
		return err
	}
	f.checkpointMu.Lock()
	if f.runtimeRestoring {
		f.checkpointMu.Unlock()
		return errors.New("SDK restore already in progress")
	}
	prepared := f.runtimeStaged
	if prepared != nil && prepared.state.Self != identity.handle {
		f.checkpointMu.Unlock()
		return errors.New("restored SDK runtime identity does not match terminal callback")
	}
	if f.runtimeActive != 0 {
		f.checkpointMu.Unlock()
		return errors.New("restore overlaps an invoking callback")
	}
	if prepared != nil {
		prepared.state.Epoch = identity.epoch
	}
	f.runtimeRestoring = true
	f.runtimeEpoch++
	f.runtimePending = nil
	f.runtimeRecovered = nil
	f.runtimeStaged = nil
	f.checkpointMu.Unlock()
	f.cancelLocalHydration()
	// Cancel before taking the ordered transport lock: a pending result may
	// hold that lock while waiting for the server, which is now in a new epoch.
	f.transportLifetime().cancel()
	f.localLRCMu.Lock()
	if f.localLRC != nil {
		f.localLRC.fail(context.Canceled)
	}
	f.localLRCMu.Unlock()
	f.interactionStreamMu.Lock()
	defer f.interactionStreamMu.Unlock()
	f.resetConfirmedObjectStreamLocked()
	f.resetInteractionStreamLocked()
	f.restoredIdentity.Store(identity)
	f.genericCallbackMu.Lock()
	f.genericCallbacks = nil
	f.genericCallbackCompleted = nil
	if prepared != nil {
		f.genericCallbacks = make(map[string]*genericCallbackInvocation, len(prepared.state.PendingOutcomes))
		for _, outcome := range prepared.state.PendingOutcomes {
			f.genericCallbacks[outcome.Identity] = &genericCallbackInvocation{entered: true, complete: true, success: outcome.Success, exception: outcome.Exception}
		}
	}
	f.genericCallbackMu.Unlock()
	f.ownershipCallbackMu.Lock()
	f.ownershipPending = nil
	f.ownershipInvoked = nil
	f.callbackKnowledge = nil
	f.retiredKnowledge = nil
	f.ownershipCallbackMu.Unlock()
	f.mu.Lock()
	f.resetDeletionKnowledgeLocked()
	f.objectClasses = nil
	f.publishedObjectAttributes = nil
	f.publishedInteractions = nil
	f.ownedObjectAttributes = nil
	f.mu.Unlock()
	if err := f.restoreFOMRefresh(ctx); err != nil {
		return err
	}
	if prepared != nil {
		if err := f.restorePendingCallbacks(prepared); err != nil {
			return err
		}
		// Translation reconstructs public events, but its bookkeeping must not
		// consume grants/removals a second time: this image is post-receive state.
		f.installRuntimeKnowledge(prepared)
	}
	parent := f.interactionContext
	if parent == nil {
		parent = context.Background()
	}
	transportCtx, cancel := context.WithCancel(parent)
	f.restoreTransport.Store(&federateTransport{ctx: transportCtx, cancel: cancel})
	f.checkpointMu.Lock()
	f.runtimeRestoring = false
	f.checkpointMu.Unlock()
	return nil
}

func (f *Federate) restoredCallbackIdentity(restored FederationRestored) (*federateIdentity, error) {
	identity := &federateIdentity{handle: f.CurrentHandle(), epoch: f.CallbackEpoch()}
	if restored.NextRetractionHandle != nil {
		identity.nextRetractionHandle = *restored.NextRetractionHandle
	}
	if restored.FederateHandle != nil {
		if *restored.FederateHandle == 0 {
			return nil, errors.New("restored federate handle is zero")
		}
		identity.handle = *restored.FederateHandle
	}
	if restored.CallbackEpoch != nil {
		if *restored.CallbackEpoch == 0 || *restored.CallbackEpoch <= identity.epoch {
			return nil, errors.New("restored callback epoch did not advance")
		}
		identity.epoch = *restored.CallbackEpoch
	}
	return identity, nil
}

func (f *Federate) restorePendingCallbacks(prepared *sdkPreparedRuntime) error {
	for _, wire := range prepared.wires {
		event := f.translate(wire)
		if event == nil {
			return errors.New("restored pending callback is not supported")
		}
		if failure, ok := event.(CallbackProtocolError); ok {
			return failure
		}
		f.checkpointMu.Lock()
		f.runtimeRecovered = append(f.runtimeRecovered, event)
		f.checkpointMu.Unlock()
	}
	return nil
}
