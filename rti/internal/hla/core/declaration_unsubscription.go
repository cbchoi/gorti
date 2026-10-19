package core

import "context"

// IEEE 1516.1-2010 section 5.7 distinguishes omission from an explicit empty
// set. The legacy combined method retains empty-means-whole compatibility.
type DeclarationObjectUnsubscription interface {
	UnsubscribeObjectClass(context.Context, FederationName, FederateHandle, ObjectClassHandle) error
	UnsubscribeObjectClassAttributeSet(context.Context, FederationName, FederateHandle, ObjectClassHandle, []AttributeHandle) error
}
