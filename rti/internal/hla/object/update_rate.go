package object

import (
	"context"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// GetUpdateRateValueForAttribute validates object knowledge independently of
// ownership: the official API does not require the caller to own the attribute.
func (r *RegistryOf[T]) GetUpdateRateValueForAttribute(ctx context.Context, fed core.FederationName, caller core.FederateHandle, obj core.ObjectHandle, attr core.AttributeHandle) (float64, error) {
	r.mu.RLock()
	st := r.federations[fed]
	r.mu.RUnlock()
	if st == nil {
		return 0, core.ErrObjectHandleInvalid
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	inst, err := r.transportationObject(st, fed, caller, obj, []core.AttributeHandle{attr})
	if err != nil {
		return 0, err
	}
	attrs, err := r.knownAttributes(ctx, fed, st, caller, inst, []core.AttributeHandle{attr})
	if err != nil {
		return 0, err
	}
	rates, ok := r.opts.Declarations.(core.AttributeUpdateRateReader)
	if !ok {
		return 0, core.ErrUpdateRateValueUnavailable
	}
	demand, ok := rates.EffectiveAttributeUpdateRate(fed, obj, attrs[0])
	if !ok || !demand.Relevant || demand.Default {
		return 0, core.ErrUpdateRateValueUnavailable
	}
	return demand.Rate, nil
}
