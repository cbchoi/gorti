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

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// InteractionEvent is a transport-neutral MOM response interaction.
type InteractionEvent struct {
	Sequence   uint64
	Class      core.InteractionClassHandle
	Parameters map[core.ParameterHandle][]byte
}

func (e *InteractionEvent) Seq() uint64 {
	if e == nil {
		return 0
	}
	return e.Sequence
}

// DiscoverEvent and ReflectEvent expose MOM object instances through the
// same domain callback boundary used by regular object management.
type DiscoverEvent struct {
	Sequence uint64
	Object   core.ObjectHandle
	Class    core.ObjectClassHandle
	Name     string
}

func (e *DiscoverEvent) Seq() uint64 {
	if e == nil {
		return 0
	}
	return e.Sequence
}

type ReflectEvent struct {
	Sequence   uint64
	Object     core.ObjectHandle
	Class      core.ObjectClassHandle
	Attributes map[core.AttributeHandle][]byte
}

func (e *ReflectEvent) Seq() uint64 {
	if e == nil {
		return 0
	}
	return e.Sequence
}

// momSeq is a global counter for the seq field on emitted MOM
// response events. Distinct from the per-federation outbound seq
// the object registry tracks; MOM responses ride a separate
// numbering since they aren't part of the registry's fanout.
var momSeq uint64

// NewProductionEmitter returns a ResponseEmitter that resolves
// names to handles via the FOM lookup and forwards the resulting
// proto event through “outbox“. M20.6 wires this in cmd/rtid.
func NewProductionEmitter(outbox core.Outbox) ResponseEmitter {
	return func(
		ctx context.Context,
		fed core.FederationName,
		recipient core.FederateHandle,
		resp ResponseInteraction,
		fom core.FOMHandle,
		_ core.FOMHandleNameLookup,
	) error {
		if fom == nil || outbox == nil {
			return fmt.Errorf("mom emit %s: FOM and outbox are required", resp.ClassName)
		}
		clsH, ok := fom.LookupInteractionClass(resp.ClassName)
		if !ok {
			return fmt.Errorf("mom emit %s: report class not in FOM", resp.ClassName)
		}
		params := make(map[core.ParameterHandle][]byte, len(resp.Params))
		for name, bytes := range resp.Params {
			h, ok := fom.LookupParameter(clsH, name)
			if !ok {
				return fmt.Errorf("mom emit %s: parameter %q not in FOM",
					resp.ClassName, name)
			}
			params[h] = append([]byte(nil), bytes...)
		}
		seq := atomic.AddUint64(&momSeq, 1)
		evt := &InteractionEvent{Sequence: seq, Class: clsH, Parameters: params}
		return outbox.Send(ctx, fed, recipient, evt)
	}
}
