// HLAreport* response emitter (M20.6).
//
// The production ResponseEmitter resolves the response's class +
// parameter names to FOM handles, builds a *rtiv1.ReceiveInteraction
// proto, and sends it through the supplied Outbox to the requesting
// federate. The Outbox is typically the same one the MOM Manager
// holds, so responses ride the existing wire path.

package mom

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/cbchoi/gorti/rti/internal/core"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
)

// momOutboundEvent satisfies core.OutboundEvent + federateEventCarrier
// (the unexported interface in rti/internal/transport/grpc). The
// transport reads Inner() to extract the wire proto. Same pattern
// the savepoint package uses.
type momOutboundEvent struct {
	pb *rtiv1.FederateEvent
}

func (e *momOutboundEvent) Seq() uint64                  { return e.pb.GetSeq() }
func (e *momOutboundEvent) Inner() *rtiv1.FederateEvent  { return e.pb }

// momSeq is a global counter for the seq field on emitted MOM
// response events. Distinct from the per-federation outbound seq
// the object registry tracks; MOM responses ride a separate
// numbering since they aren't part of the registry's fanout.
var momSeq uint64

// NewProductionEmitter returns a ResponseEmitter that resolves
// names to handles via the FOM lookup and forwards the resulting
// proto event through ``outbox``. M20.6 wires this in cmd/rtid.
func NewProductionEmitter(outbox core.Outbox) ResponseEmitter {
	return func(
		ctx context.Context,
		fed core.FederationName,
		recipient core.FederateHandle,
		resp ResponseInteraction,
		fom core.FOMHandle,
		_ core.FOMHandleNameLookup,
	) error {
		clsH, ok := fom.LookupInteractionClass(resp.ClassName)
		if !ok {
			// FOM doesn't declare the response class — typical when a
			// federation doesn't subscribe to the HLAmanager MOM
			// classes. Silently drop the response; the federate
			// didn't subscribe so it couldn't receive anyway.
			return nil
		}
		params := make(map[uint64][]byte, len(resp.Params))
		for name, bytes := range resp.Params {
			h, ok := fom.LookupParameter(clsH, name)
			if !ok {
				return fmt.Errorf("mom emit %s: parameter %q not in FOM",
					resp.ClassName, name)
			}
			params[uint64(h)] = bytes
		}
		seq := atomic.AddUint64(&momSeq, 1)
		evt := &momOutboundEvent{pb: &rtiv1.FederateEvent{
			Seq: seq,
			Event: &rtiv1.FederateEvent_Receive{
				Receive: &rtiv1.ReceiveInteraction{
					InteractionClassHandle: uint64(clsH),
					Parameters:             params,
				},
			},
		}}
		return outbox.Send(ctx, fed, recipient, evt)
	}
}
