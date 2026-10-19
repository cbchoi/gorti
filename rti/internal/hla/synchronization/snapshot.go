package sync

import (
	"fmt"
	"reflect"
	"slices"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// Version 3 also preserves admitted callback payloads until actual completion.
type ManagerState struct {
	Version            uint32
	RequireInvocations bool
	NextSequence       uint64
	NextEpoch          uint64
	Points             []PointState
	Pending            []CallbackState
	Obligations        []CallbackState
}

type PointState struct {
	Label       string
	Tag         []byte
	State       SyncPointState
	Required    []core.FederateHandle
	Achieved    []core.FederateHandle
	Failed      []core.FederateHandle
	Dynamic     bool
	AllMembers  bool
	Epoch       uint64
	Invocations []InvocationState
}

func (m *Manager) ExportState(fed core.FederationName) *ManagerState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	st := m.fed[fed]
	if st == nil {
		return nil
	}
	out := &ManagerState{Version: 3, RequireInvocations: m.opts.RequireInvocations, NextSequence: st.nextSequence, NextEpoch: st.nextEpoch}
	for _, label := range sortedLabels(st) {
		point := st.points[label]
		entry := PointState{Label: label, Tag: slices.Clone(point.tag), State: point.state,
			Required: sortedHandles(point.required), Achieved: sortedHandles(point.achieved),
			Failed: sortedHandles(point.failed), Dynamic: point.dynamic, AllMembers: point.allMembers, Epoch: point.epoch}
		for _, h := range sortedHandles(point.required) {
			inv := point.callbacks[h]
			inv.Recipient = h
			entry.Invocations = append(entry.Invocations, inv)
		}
		out.Points = append(out.Points, entry)
	}
	for _, callback := range st.pending {
		out.Pending = append(out.Pending, cloneCallbackState(callback))
	}
	if m.opts.RequireInvocations {
		for _, sequence := range sortedCallbackSequences(st.obligations) {
			out.Obligations = append(out.Obligations, cloneCallbackState(st.obligations[sequence]))
		}
	}
	return out
}

func (m *Manager) ValidateState(state *ManagerState) error {
	if state == nil {
		return nil
	}
	if state.Version != 0 && state.Version != 2 && state.Version != 3 {
		return fmt.Errorf("sync snapshot: unsupported version %d", state.Version)
	}
	if m.opts.RequireInvocations && (len(state.Points) != 0 || len(state.Pending) != 0 || len(state.Obligations) != 0 || state.NextSequence != 0) && (state.Version != 3 || !state.RequireInvocations) {
		return fmt.Errorf("sync snapshot: exact admitted callback history unavailable")
	}
	labels := map[string]PointState{}
	sequences := map[uint64]bool{}
	if err := validatePoints(state, labels, sequences); err != nil {
		return err
	}
	obligations, err := validateObligations(state, labels, sequences)
	if err != nil {
		return err
	}
	if err := validateDurableInvocations(state, obligations); err != nil {
		return err
	}
	return validatePendingCallbacks(state, labels, obligations)
}

func validatePoints(state *ManagerState, labels map[string]PointState, sequences map[uint64]bool) error {
	for _, point := range state.Points {
		if _, exists := labels[point.Label]; exists {
			return fmt.Errorf("sync snapshot: duplicate label %q", point.Label)
		}
		labels[point.Label] = point
		if err := validatePointParticipants(point); err != nil {
			return err
		}
		if state.Version != 0 {
			if err := validatePointInvocations(state, point, sequences); err != nil {
				return err
			}
		}
	}
	return nil
}

func validatePointParticipants(point PointState) error {
	if point.State != StateAnnounced && point.State != StateAchieved {
		return fmt.Errorf("sync snapshot: invalid state")
	}
	required, achieved := handleSet(point.Required), handleSet(point.Achieved)
	if len(required) != len(point.Required) || len(achieved) != len(point.Achieved) {
		return fmt.Errorf("sync snapshot: duplicate participant")
	}
	for _, h := range point.Required {
		if h == 0 {
			return fmt.Errorf("sync snapshot: zero participant")
		}
	}
	for _, h := range point.Achieved {
		if _, ok := required[h]; !ok {
			return fmt.Errorf("sync snapshot: outsider achievement")
		}
	}
	failed := handleSet(point.Failed)
	if len(failed) != len(point.Failed) {
		return fmt.Errorf("sync snapshot: duplicate failed participant")
	}
	for _, h := range point.Failed {
		if _, ok := achieved[h]; !ok {
			return fmt.Errorf("sync snapshot: failure without achievement")
		}
	}
	if point.State == StateAchieved && len(achieved) != len(required) {
		return fmt.Errorf("sync snapshot: incomplete synchronized point")
	}
	return nil
}

func validatePointInvocations(state *ManagerState, point PointState, sequences map[uint64]bool) error {
	required, achieved := handleSet(point.Required), handleSet(point.Achieved)
	if state.RequireInvocations && point.Dynamic {
		return fmt.Errorf("sync snapshot: legacy dynamic profile in strict image")
	}
	if (state.RequireInvocations && point.Epoch == 0) || point.Epoch > state.NextEpoch {
		return fmt.Errorf("sync snapshot: invalid epoch")
	}
	invoked := map[core.FederateHandle]bool{}
	for _, inv := range point.Invocations {
		if _, exists := required[inv.Recipient]; !exists || invoked[inv.Recipient] {
			return fmt.Errorf("sync snapshot: invalid invocation recipient")
		}
		invoked[inv.Recipient] = true
		if err := validateInvocation(state, point, inv, sequences, achieved); err != nil {
			return err
		}
	}
	if len(invoked) != len(required) {
		return fmt.Errorf("sync snapshot: incomplete invocation ledger")
	}
	return nil
}

func validateInvocation(state *ManagerState, point PointState, inv InvocationState, sequences map[uint64]bool, achieved map[core.FederateHandle]struct{}) error {
	if err := validateInvocationIdentity(state, point, inv, sequences); err != nil {
		return err
	}
	if (inv.AnnounceEntered || inv.AnnounceInvoked) && inv.Announce == 0 || inv.SynchronizedInvoked && inv.Synchronized == 0 {
		return fmt.Errorf("sync snapshot: invocation without identity")
	}
	if inv.AnnounceInvoked && !inv.AnnounceEntered {
		return fmt.Errorf("sync snapshot: completion without entry")
	}
	if state.RequireInvocations {
		if _, done := achieved[inv.Recipient]; done && !inv.AnnounceEntered {
			return fmt.Errorf("sync snapshot: achievement without announcement entry")
		}
	}
	return nil
}

func validateInvocationIdentity(state *ManagerState, point PointState, inv InvocationState, sequences map[uint64]bool) error {
	if state.RequireInvocations && inv.Announce == 0 && !point.Dynamic {
		return fmt.Errorf("sync snapshot: missing announcement identity")
	}
	if state.RequireInvocations && inv.Synchronized == 0 && point.State == StateAchieved {
		return fmt.Errorf("sync snapshot: missing synchronized identity")
	}
	if inv.Synchronized != 0 && point.State != StateAchieved {
		return fmt.Errorf("sync snapshot: premature synchronized identity")
	}
	for _, seq := range []uint64{inv.Announce, inv.Synchronized} {
		if seq == 0 {
			continue
		}
		if seq > state.NextSequence || sequences[seq] {
			return fmt.Errorf("sync snapshot: invalid callback identity")
		}
		sequences[seq] = true
	}
	return nil
}

func validateObligations(state *ManagerState, labels map[string]PointState, sequences map[uint64]bool) (map[uint64]CallbackState, error) {
	obligations := map[uint64]CallbackState{}
	for _, callback := range state.Obligations {
		if err := validateObligation(state, callback, obligations); err != nil {
			return nil, err
		}
		if err := validateObligationInvocation(callback, labels, sequences); err != nil {
			return nil, err
		}
		obligations[callback.Sequence] = callback
	}
	return obligations, nil
}

func validateObligation(state *ManagerState, callback CallbackState, obligations map[uint64]CallbackState) error {
	if state.Version != 3 || !state.RequireInvocations || callback.Sequence == 0 || callback.Sequence > state.NextSequence || callback.Recipient == 0 || callback.delivery().Event == nil {
		return fmt.Errorf("sync snapshot: invalid callback obligation")
	}
	if _, duplicate := obligations[callback.Sequence]; duplicate {
		return fmt.Errorf("sync snapshot: duplicate callback obligation")
	}
	if callback.Epoch > state.NextEpoch || (callback.Kind != callbackRegistrationFailed && callback.Epoch == 0) {
		return fmt.Errorf("sync snapshot: invalid callback epoch")
	}
	if callback.Kind == callbackRegistrationFailed && callback.Reason != RegistrationFailureLabelNotUnique && callback.Reason != RegistrationFailureSetMemberNotJoined {
		return fmt.Errorf("sync snapshot: invalid registration failure reason")
	}
	return nil
}

func validateObligationInvocation(callback CallbackState, labels map[string]PointState, sequences map[uint64]bool) error {
	if callback.Kind == callbackAnnounce || callback.Kind == callbackSynchronized {
		point, exists := labels[callback.Label]
		if !exists || point.Epoch != callback.Epoch {
			// A reentrant announcement can finish after its point retires.
			if callback.Kind != callbackAnnounce || !callback.Entered {
				return fmt.Errorf("sync snapshot: orphan callback obligation")
			}
		} else {
			if !matchesCallbackInvocation(point, callback, true) {
				return fmt.Errorf("sync snapshot: obligation invocation mismatch")
			}
		}
	} else if sequences[callback.Sequence] {
		return fmt.Errorf("sync snapshot: confirmation reuses point callback identity")
	}
	return nil
}

func validateDurableInvocations(state *ManagerState, obligations map[uint64]CallbackState) error {
	if state.Version == 3 && state.RequireInvocations {
		for _, point := range state.Points {
			for _, inv := range point.Invocations {
				for _, needed := range []struct {
					sequence uint64
					kind     callbackKind
					done     bool
				}{{inv.Announce, callbackAnnounce, inv.AnnounceInvoked}, {inv.Synchronized, callbackSynchronized, inv.SynchronizedInvoked}} {
					if needed.sequence == 0 || needed.done {
						continue
					}
					callback, exists := obligations[needed.sequence]
					if !exists || callback.Kind != needed.kind || callback.Epoch != point.Epoch || callback.Label != point.Label || callback.Recipient != inv.Recipient {
						return fmt.Errorf("sync snapshot: missing durable invocation obligation")
					}
				}
			}
		}
	}
	return nil
}

func validatePendingCallbacks(state *ManagerState, labels map[string]PointState, obligations map[uint64]CallbackState) error {
	pending := map[uint64]bool{}
	for _, callback := range state.Pending {
		if callback.Sequence == 0 || callback.Sequence > state.NextSequence || pending[callback.Sequence] || callback.Recipient == 0 || callback.delivery().Event == nil {
			return fmt.Errorf("sync snapshot: invalid pending callback")
		}
		pending[callback.Sequence] = true
		if state.Version == 3 && state.RequireInvocations {
			obligation, exists := obligations[callback.Sequence]
			if !exists || !reflect.DeepEqual(obligation, callback) {
				return fmt.Errorf("sync snapshot: pending callback differs from durable obligation")
			}
			continue
		}
		if callback.Kind == callbackAnnounce || callback.Kind == callbackSynchronized {
			point, exists := labels[callback.Label]
			if !exists {
				return fmt.Errorf("sync snapshot: orphan pending callback")
			}
			if !matchesCallbackInvocation(point, callback, false) {
				return fmt.Errorf("sync snapshot: pending invocation mismatch")
			}
		}
	}
	return nil
}

func matchesCallbackInvocation(point PointState, callback CallbackState, durable bool) bool {
	matched := false
	for _, inv := range point.Invocations {
		if inv.Recipient != callback.Recipient {
			continue
		}
		announceEntryMatches := !inv.AnnounceEntered
		if durable {
			announceEntryMatches = callback.Entered == inv.AnnounceEntered
		}
		matched = callback.Kind == callbackAnnounce && inv.Announce == callback.Sequence && !inv.AnnounceInvoked && announceEntryMatches ||
			callback.Kind == callbackSynchronized && inv.Synchronized == callback.Sequence && !inv.SynchronizedInvoked
	}
	return matched
}

// ValidateSnapshot checks a detached domain image without constructing services
// or making delivery calls. Persistence graph adapters use the same invariants.
func ValidateSnapshot(state *ManagerState) error {
	requireInvocations := state != nil && state.RequireInvocations
	return (&Manager{opts: Options{RequireInvocations: requireInvocations}}).ValidateState(state)
}

// PrepareImport validates and detaches the image before the restore commit.
func (m *Manager) PrepareImport(fed core.FederationName, state *ManagerState) (func(), error) {
	if err := m.ValidateState(state); err != nil {
		return nil, err
	}
	st := emptyFederationState()
	if state != nil {
		st.nextSequence, st.nextEpoch = state.NextSequence, state.NextEpoch
		for _, point := range state.Points {
			callbacks := map[core.FederateHandle]InvocationState{}
			for _, inv := range point.Invocations {
				callbacks[inv.Recipient] = inv
			}
			st.points[point.Label] = &syncPoint{tag: slices.Clone(point.Tag), state: point.State,
				required: handleSet(point.Required), achieved: handleSet(point.Achieved), failed: handleSet(point.Failed),
				dynamic: point.Dynamic, allMembers: point.AllMembers, epoch: point.Epoch, callbacks: callbacks}
		}
		for _, callback := range state.Pending {
			st.pending = append(st.pending, cloneCallbackState(callback))
		}
		if state.Version == 3 && state.RequireInvocations {
			for _, callback := range state.Obligations {
				st.obligations[callback.Sequence] = cloneCallbackState(callback)
			}
			// Queue admission is a live transport fact, not restored invocation.
			// Rebuild every uncompleted obligation in original callback order.
			st.pending = nil
			for _, sequence := range sortedCallbackSequences(st.obligations) {
				st.pending = append(st.pending, cloneCallbackState(st.obligations[sequence]))
			}
		}
	}
	return func() {
		m.opMu.Lock()
		defer m.opMu.Unlock()
		m.publish(fed, cloneFederationState(st))
	}, nil
}

func sortedCallbackSequences(callbacks map[uint64]CallbackState) []uint64 {
	sequences := make([]uint64, 0, len(callbacks))
	for sequence := range callbacks {
		sequences = append(sequences, sequence)
	}
	slices.Sort(sequences)
	return sequences
}

// ImportState preserves the legacy interface. Assessed restore paths must use
// PrepareImport so an incompatible image is returned as an error, not ignored.
func (m *Manager) ImportState(fed core.FederationName, state *ManagerState) {
	if state == nil {
		return
	}
	commit, err := m.PrepareImport(fed, state)
	if err == nil {
		commit()
	}
}
