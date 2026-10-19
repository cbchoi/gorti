package object

import "github.com/cbchoi/gorti/rti/internal/hla/core"

// RecordRTIObjectDiscovery joins MOM's directly delivered discovery with the
// registry's object-knowledge history. User-owned objects use registry delivery.
func (r *RegistryOf[T]) RecordRTIObjectDiscovery(fed core.FederationName, sub core.FederateHandle, obj core.ObjectHandle) error {
	if sub == core.InvalidFederateHandle || sub == internalProducer {
		return core.ErrFederateNotJoined
	}
	r.mu.RLock()
	st := r.federations[fed]
	r.mu.RUnlock()
	if st == nil {
		return core.ErrObjectNotFound
	}
	st.mu.Lock()
	inst := st.instances[obj]
	st.mu.Unlock()
	if inst == nil || inst.owner != internalProducer {
		return core.ErrObjectNotFound
	}
	inst.deliveryMu.Lock()
	defer inst.deliveryMu.Unlock()
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.instances[obj] != inst {
		return core.ErrObjectNotFound
	}
	key := discoverKey{sub: sub, obj: obj}
	if _, known := st.discovered[key]; !known {
		st.discovered[key] = inst.cls
	}
	return nil
}
