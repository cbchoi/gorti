package composition

import (
	"sync/atomic"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type fomReferenceValue struct{ handle core.FOMHandle }
type fomReference struct {
	value atomic.Pointer[fomReferenceValue]
}

func (r *fomReference) CurrentFOMHandle() core.FOMHandle {
	if current := r.value.Load(); current != nil {
		return current.handle
	}
	return nil
}

func (r *fomReference) publish(handle core.FOMHandle) {
	if handle == nil {
		r.value.Store(nil)
		return
	}
	r.value.Store(&fomReferenceValue{handle: handle})
}

// A reference follows the same publication boundary as repository Get. Its
// current-view read is atomic; it neither rebuilds a FOM nor takes the repo lock.
func (r *Repository) FOMHandleReference(fed core.FederationName) core.FOMHandleReference {
	return &r.slotFor(fed).reference
}
