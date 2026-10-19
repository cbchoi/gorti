package mom

import (
	"context"
	"fmt"
	"slices"
	"sync/atomic"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

const ClassReportFederateLost = "HLAmanager.HLAfederate.HLAreport.HLAreportFederateLost"

// ReportFederateLost is called while the lost member still has its identity and
// last time state. Undefined time is encoded as an empty HLAlogicalTime value.
func (d *Dispatcher) ReportFederateLost(ctx context.Context, id core.MemberIdentity, lastGood LogicalTimeMetric, description string, model core.FOMHandle) error {
	deliveries, err := d.FederateLostReportDeliveries(ctx, id, lastGood, description, model)
	if err != nil || len(deliveries) == 0 {
		return err
	}
	out, ok := d.mom.opts.Outbox.(core.ReservableOutbox)
	if !ok {
		return fmt.Errorf("mom: lost federate reports require reservable delivery")
	}
	reservation, err := out.Reserve(ctx, id.Federation, deliveries)
	if err != nil {
		if reservation != nil {
			reservation.Release()
		}
		return err
	}
	if reservation == nil {
		return fmt.Errorf("mom: missing lost federate report reservation")
	}
	defer reservation.Release()
	return reservation.Commit()
}

// FederateLostReportDeliveries builds, but does not publish or reserve, the report
// fanout so loss handling can admit it together with checkpoint fault outcomes.
// The caller holds the same membership/checkpoint admission as ReportFederateLost.
func (d *Dispatcher) FederateLostReportDeliveries(ctx context.Context, id core.MemberIdentity, lastGood LogicalTimeMetric, description string, model core.FOMHandle) ([]core.OutboxDelivery, error) {
	if id.Handle == 0 || id.Name == "" || description == "" || model == nil {
		return nil, fmt.Errorf("mom: incomplete lost federate report")
	}
	stamp := logicalMetric(lastGood)
	if stamp == nil {
		return nil, fmt.Errorf("mom: invalid last known good time")
	}
	d.mu.RLock()
	route := d.catalog.ReportRecipients
	d.mu.RUnlock()
	if route == nil || d.mom == nil {
		return nil, fmt.Errorf("mom: lost federate routing unavailable")
	}
	recipients, err := route(ctx, id.Federation, id.Handle, ClassReportFederateLost, "")
	if err != nil {
		return nil, err
	}
	slices.Sort(recipients)
	recipients = slices.Compact(recipients)
	class, ok := model.LookupInteractionClass(ClassReportFederateLost)
	if !ok {
		return nil, fmt.Errorf("mom: lost federate report class unavailable")
	}
	values := map[string][]byte{"HLAfederate": encodeHLAhandle(id.Handle), "HLAfederateName": encodeHLAunicodeString(id.Name), "HLAtimeStamp": stamp, "HLAfaultDescription": encodeHLAunicodeString(description)}
	params := make(map[core.ParameterHandle][]byte, len(values))
	for name, value := range values {
		h, ok := model.LookupParameter(class, name)
		if !ok {
			return nil, fmt.Errorf("mom: lost federate report parameter %s unavailable", name)
		}
		params[h] = value
	}
	var deliveries []core.OutboxDelivery
	for _, h := range recipients {
		if h != id.Handle {
			deliveries = append(deliveries, core.OutboxDelivery{Recipient: h, Event: &InteractionEvent{Sequence: atomic.AddUint64(&momSeq, 1), Class: class, Parameters: params}})
		}
	}
	return deliveries, nil
}
