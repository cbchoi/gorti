package ownership

import (
	"context"
	"fmt"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// ModifyAttributeState is the notification-free MOM primitive. The caller must
// pin and validate object knowledge, FOM definition and publication first.
func (m *Manager) ModifyAttributeState(ctx context.Context, fed core.FederationName, target core.FederateHandle, obj core.ObjectHandle, attr core.AttributeHandle, owned bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if fed == "" || target == 0 || target == ^core.FederateHandle(0) || obj == 0 || attr == 0 {
		return fmt.Errorf("MOM ownership: invalid identity")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.fed[fed]
	key := ownershipKey{obj: obj, attr: attr}
	if st == nil {
		st = newFederationState()
	}
	if err := st.validateMOMOwnership(key, target, owned); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if owned {
		st.owners[key] = ownershipRecord{owner: target}
		delete(st.unownedOffers, key)
		m.fed[fed] = st
	} else {
		delete(st.owners, key)
	}
	return nil
}
func (st *federationState) validateMOMOwnership(key ownershipKey, target core.FederateHandle, owned bool) error {
	current := st.owners[key].owner
	if current == ^core.FederateHandle(0) {
		return fmt.Errorf("MOM ownership: RTI-owned attribute")
	}
	if _, pending := st.pendingDivests[key]; pending {
		return fmt.Errorf("MOM ownership: pending divestiture adjustment unavailable")
	}
	for k := range st.pendingAcquires {
		if k.obj == key.obj && k.attr == key.attr {
			return fmt.Errorf("MOM ownership: pending acquisition adjustment unavailable")
		}
	}
	if !owned && current != 0 && current != target {
		return fmt.Errorf("MOM ownership: attribute is owned by a different federate")
	}
	return nil
}
