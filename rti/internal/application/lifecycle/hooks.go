// Package lifecycle coordinates application-level service hooks without
// introducing dependencies between HLA service implementations.
package lifecycle

import (
	"context"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type FederateJoinedHook func(context.Context, core.FederationName, core.FederateHandle, string, string)
type FederateResignedHook func(context.Context, core.FederationName, core.FederateHandle)

func ChainFederateJoined(hooks ...FederateJoinedHook) FederateJoinedHook {
	return func(ctx context.Context, federation core.FederationName, handle core.FederateHandle, name, federateType string) {
		for _, hook := range hooks {
			if hook != nil {
				hook(ctx, federation, handle, name, federateType)
			}
		}
	}
}

func ChainFederateResigned(hooks ...FederateResignedHook) FederateResignedHook {
	return func(ctx context.Context, federation core.FederationName, handle core.FederateHandle) {
		for _, hook := range hooks {
			if hook != nil {
				hook(ctx, federation, handle)
			}
		}
	}
}
