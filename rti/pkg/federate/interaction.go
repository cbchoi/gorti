// TASK-205½ (M21) — SendInteraction wire dispatcher.

package federate

import (
	"context"
	"fmt"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
)

// SendInteraction sends an interaction. parameters is keyed by
// parameter name; the SDK resolves to wire handles via the FOM tables.
// timestamp is optional (nil → untimed).
//
// Unknown parameter names are silently dropped (the wire surface
// only carries handle-keyed entries; unrepresentable names produce
// no wire bytes). Pass at-least-one valid parameter to actually
// transmit anything.
func (f *Federate) SendInteraction(
	ctx context.Context, className string, parameters map[string][]byte, timestamp *float64,
) error {
	classHandle, ok := f.handles.interactionHandle(className)
	if !ok {
		return fmt.Errorf("federate: interaction class %q not in FOM", className)
	}
	wireParams := make(map[uint64][]byte, len(parameters))
	for name, payload := range parameters {
		ph, pok := f.handles.parameterHandle(className, name)
		if !pok {
			continue // unknown param name — drop on the floor
		}
		wireParams[ph] = payload
	}
	req := &rtiv1.SendInteractionRequest{
		WireVersion:            rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName:         f.federationName,
		FederateHandle:         f.federateHandle,
		InteractionClassHandle: classHandle,
		Parameters:             wireParams,
	}
	if timestamp != nil {
		req.LogicalTime = timestamp
	}
	_, err := f.conn.obj.SendInteraction(ctx, req)
	return wrapStatusErr(err)
}
