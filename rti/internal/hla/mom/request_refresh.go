package mom

import (
	"context"
	"fmt"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// The internal MOM owner has no federate callback queue. Attribute requests
// therefore enter the same source-backed publication path as periodic updates.
func (d *Dispatcher) RequestMOMAttributeUpdate(ctx context.Context, fed core.FederationName, obj core.ObjectHandle, attrs []core.AttributeHandle) error {
	if d.mom == nil {
		return fmt.Errorf("MOM: attribute refresh unavailable")
	}
	return d.mom.requestAttributeUpdate(ctx, fed, obj, attrs)
}

func (m *Manager) requestAttributeUpdate(ctx context.Context, fed core.FederationName, obj core.ObjectHandle, attrs []core.AttributeHandle) error {
	m.periodicMu.Lock()
	defer m.periodicMu.Unlock()
	m.mu.RLock()
	st := m.fed[fed]
	if st == nil {
		m.mu.RUnlock()
		return core.ErrFederationNotFound
	}
	class := st.objects.federationCls
	var target core.FederateHandle
	found := obj == st.objects.federationObj
	if !found {
		for h, fs := range st.federates {
			if fs.objectHandle == obj {
				target, class, found = h, st.objects.federateCls, true
				break
			}
		}
	}
	m.mu.RUnlock()
	if !found {
		return core.ErrObjectHandleInvalid
	}
	var snapshot MetricSnapshot
	var err error
	if target == 0 {
		snapshot, err = m.FederationMetricValues(ctx, fed)
	} else {
		snapshot, err = m.FederateMetricValues(ctx, fed, target)
	}
	if err != nil {
		return err
	}
	fom, err := m.fanout.foms.Get(ctx, fed)
	if err != nil {
		return err
	}
	names, ok := fom.(core.FOMHandleNameLookup)
	if !ok {
		return fmt.Errorf("MOM: attribute names unavailable")
	}
	values := make(map[string][]byte, len(attrs))
	for _, attr := range attrs {
		name, ok := names.AttributeName(class, attr)
		if !ok {
			return core.ErrAttributeNotFound
		}
		value, ok := snapshot.Values[name]
		if !ok {
			return fmt.Errorf("MOM: requested attribute %s unavailable: %s", name, snapshot.Unavailable[name])
		}
		values[name] = value
	}
	snapshot.Values = values
	return m.publishMetricSnapshot(ctx, fed, obj, class, snapshot)
}
