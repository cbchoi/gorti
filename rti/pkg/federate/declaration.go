// TASK-205½ (M21) — Publish/Subscribe declaration RPCs.

package federate

import (
	"context"
	"fmt"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
)

// PublishObjectClassAttributes declares publication of the named attributes
// on an object class. Class and attribute names are resolved from the FOM
// supplied at join time.
func (f *Federate) PublishObjectClassAttributes(
	ctx context.Context, className string, attributeNames []string,
) error {
	classHandle, attributeHandles, err := f.resolveObjectAttributes(className, attributeNames)
	if err != nil {
		return err
	}
	_, err = f.conn.decl.PublishObjectClassAttributes(ctx, &rtiv1.PubObjAttrsRequest{
		WireVersion:       rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName:    f.federationName,
		FederateHandle:    f.federateHandle,
		ObjectClassHandle: classHandle,
		AttributeHandles:  append([]uint64(nil), attributeHandles...),
	})
	return wrapStatusErr(err)
}

// SubscribeObjectClassAttributes declares subscription to the named
// attributes on an object class.
func (f *Federate) SubscribeObjectClassAttributes(
	ctx context.Context, className string, attributeNames []string,
) error {
	classHandle, attributeHandles, err := f.resolveObjectAttributes(className, attributeNames)
	if err != nil {
		return err
	}
	_, err = f.conn.decl.SubscribeObjectClassAttributes(ctx, &rtiv1.SubObjAttrsRequest{
		WireVersion:       rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName:    f.federationName,
		FederateHandle:    f.federateHandle,
		ObjectClassHandle: classHandle,
		AttributeHandles:  append([]uint64(nil), attributeHandles...),
	})
	return wrapStatusErr(err)
}

func (f *Federate) resolveObjectAttributes(className string, attributeNames []string) (uint64, []uint64, error) {
	classHandle, ok := f.handles.objectClassHandle(className)
	if !ok {
		return 0, nil, fmt.Errorf("federate: object class %q not in FOM", className)
	}
	attributeHandles := make([]uint64, len(attributeNames))
	for i, name := range attributeNames {
		handle, found := f.handles.attributeHandle(className, name)
		if !found {
			return 0, nil, fmt.Errorf("federate: attribute %q not in object class %q", name, className)
		}
		attributeHandles[i] = handle
	}
	return classHandle, attributeHandles, nil
}

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
