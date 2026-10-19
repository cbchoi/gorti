package object

import (
	"context"
	"fmt"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func (r *RegistryOf[T]) reserveRetractionID(ctx context.Context, fed core.FederationName, producer core.FederateHandle, id uint64, timestamp *T) (core.OutboxReservation, error) {
	if id == 0 || timestamp == nil {
		return nil, nil
	}
	admission := r.opts.Retractions
	if admission == nil {
		admission, _ = r.opts.TSOGate.(core.RetractionIDAdmission)
	}
	if admission == nil {
		return nil, core.ErrRetractionHistoryUnavailable
	}
	var reservation core.OutboxReservation
	var err error
	if source, ok := admission.(interface {
		ReserveRetractionMessage(context.Context, core.FederationName, core.FederateHandle, uint64, T) (core.OutboxReservation, error)
	}); ok {
		reservation, err = source.ReserveRetractionMessage(ctx, fed, producer, id, *timestamp)
	} else {
		reservation, err = admission.ReserveRetractionID(ctx, fed, producer, id)
	}
	if err != nil {
		if reservation != nil {
			reservation.Release()
		}
		return nil, err
	}
	if reservation == nil {
		return nil, fmt.Errorf("%w: nil originating reservation", core.ErrRetractionHistoryUnavailable)
	}
	return reservation, nil
}
