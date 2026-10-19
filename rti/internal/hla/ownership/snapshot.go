package ownership

import (
	"slices"
	"sync"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type ManagerState struct {
	Owners           []OwnerState
	PendingDivests   []PendingDivestState
	PendingAcquires  []PendingAcquireState
	UnownedOffers    []UnownedOfferState
	CallbackSessions []CallbackSessionState
	PendingCallbacks []TerminalCallbackState
}

type UnownedOfferState struct {
	Object    core.ObjectHandle
	Attribute core.AttributeHandle
	Divester  core.FederateHandle
}

type OwnerState struct {
	Object    core.ObjectHandle
	Attribute core.AttributeHandle
	Owner     core.FederateHandle
}

type PendingDivestState struct {
	Object                core.ObjectHandle
	Attribute             core.AttributeHandle
	Owner                 core.FederateHandle
	Tag                   []byte
	TwoPhase              bool
	ConfirmationRequested bool
}

type PendingAcquireState struct {
	Object    core.ObjectHandle
	Attribute core.AttributeHandle
	Acquirer  core.FederateHandle
	Tag       []byte
}

func (m *Manager) ExportState(fed core.FederationName) *ManagerState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	st, ok := m.fed[fed]
	if !ok {
		return nil
	}
	out := &ManagerState{}
	exportCallbackStateLocked(st, out)
	for key, value := range st.owners {
		out.Owners = append(out.Owners, OwnerState{Object: key.obj, Attribute: key.attr, Owner: value.owner})
	}
	for key, value := range st.pendingDivests {
		out.PendingDivests = append(out.PendingDivests, PendingDivestState{Object: key.obj, Attribute: key.attr, Owner: value.owner, Tag: append([]byte(nil), value.tag...), TwoPhase: value.twoPhase, ConfirmationRequested: value.confirmationRequested})
	}
	for key, value := range st.pendingAcquires {
		out.PendingAcquires = append(out.PendingAcquires, PendingAcquireState{Object: key.obj, Attribute: key.attr, Acquirer: key.acquirer, Tag: append([]byte(nil), value.tag...)})
	}
	for key, divester := range st.unownedOffers {
		out.UnownedOffers = append(out.UnownedOffers, UnownedOfferState{Object: key.obj, Attribute: key.attr, Divester: divester})
	}
	slices.SortFunc(out.UnownedOffers, func(a, b UnownedOfferState) int {
		return compareObjectAttribute(a.Object, a.Attribute, b.Object, b.Attribute)
	})
	slices.SortFunc(out.Owners, func(a, b OwnerState) int { return compareObjectAttribute(a.Object, a.Attribute, b.Object, b.Attribute) })
	slices.SortFunc(out.PendingDivests, func(a, b PendingDivestState) int {
		return compareObjectAttribute(a.Object, a.Attribute, b.Object, b.Attribute)
	})
	slices.SortFunc(out.PendingAcquires, func(a, b PendingAcquireState) int {
		if comparison := compareObjectAttribute(a.Object, a.Attribute, b.Object, b.Attribute); comparison != 0 {
			return comparison
		}
		return compareUint64(uint64(a.Acquirer), uint64(b.Acquirer))
	})
	return out
}

func (m *Manager) ImportState(fed core.FederationName, state *ManagerState) {
	commit, err := m.PrepareImportState(fed, state)
	if err != nil {
		return
	}
	commit()
}

// PrepareImportState rejects malformed callback lifetimes without changing
// live state. Parent validates the remaining service DTO before this port.
func (m *Manager) PrepareImportState(fed core.FederationName, state *ManagerState) (func(), error) {
	if state == nil {
		return func() {}, nil
	}
	if err := ValidateOwnershipCallbackState(state); err != nil {
		return nil, err
	}
	st := newFederationState()
	for _, entry := range state.Owners {
		st.owners[ownershipKey{obj: entry.Object, attr: entry.Attribute}] = ownershipRecord{owner: entry.Owner}
	}
	for _, entry := range state.PendingDivests {
		st.pendingDivests[ownershipKey{obj: entry.Object, attr: entry.Attribute}] = pendingDivest{owner: entry.Owner, tag: append([]byte(nil), entry.Tag...), twoPhase: entry.TwoPhase, confirmationRequested: entry.ConfirmationRequested}
	}
	for _, entry := range state.PendingAcquires {
		st.pendingAcquires[acquireKey{obj: entry.Object, attr: entry.Attribute, acquirer: entry.Acquirer}] = pendingAcquire{tag: append([]byte(nil), entry.Tag...)}
	}
	for _, entry := range state.UnownedOffers {
		st.unownedOffers[ownershipKey{obj: entry.Object, attr: entry.Attribute}] = entry.Divester
	}
	// The coordinated decoder validates this section before ImportState.
	// Transport attempts are deliberately reset and strict receipt scopes must
	// be rebound to a fresh live epoch before any restored ACK can apply.
	importCallbackStateLocked(st, fed, state)
	var once sync.Once
	return func() { once.Do(func() { m.mu.Lock(); m.fed[fed] = st; m.mu.Unlock() }) }, nil
}

func compareObjectAttribute(aObject core.ObjectHandle, aAttribute core.AttributeHandle, bObject core.ObjectHandle, bAttribute core.AttributeHandle) int {
	if aObject != bObject {
		return compareUint64(uint64(aObject), uint64(bObject))
	}
	return compareUint64(uint64(aAttribute), uint64(bAttribute))
}

func compareUint64(a, b uint64) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}
