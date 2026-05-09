// TASK-205½ (M21) — Publish/Subscribe declaration RPCs.

package federate

import (
	"context"
	"fmt"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
)

// PublishInteractionClass declares this federate publishes the named
// interaction class. The class name is resolved to a wire handle via
// the FOM tables built at JoinFederation. Returns ErrInteractionClassNotFound
// if the class is not in the FOM.
func (f *Federate) PublishInteractionClass(ctx context.Context, className string) error {
	h, ok := f.handles.interactionHandle(className)
	if !ok {
		return fmt.Errorf("federate: interaction class %q not in FOM", className)
	}
	_, err := f.conn.decl.PublishInteractionClass(ctx, &rtiv1.PubInterRequest{
		WireVersion:            rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName:         f.federationName,
		FederateHandle:         f.federateHandle,
		InteractionClassHandle: h,
	})
	return wrapStatusErr(err)
}

// SubscribeInteractionClass declares this federate subscribes to the
// named interaction class.
func (f *Federate) SubscribeInteractionClass(ctx context.Context, className string) error {
	h, ok := f.handles.interactionHandle(className)
	if !ok {
		return fmt.Errorf("federate: interaction class %q not in FOM", className)
	}
	_, err := f.conn.decl.SubscribeInteractionClass(ctx, &rtiv1.SubInterRequest{
		WireVersion:            rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName:         f.federationName,
		FederateHandle:         f.federateHandle,
		InteractionClassHandle: h,
	})
	return wrapStatusErr(err)
}
