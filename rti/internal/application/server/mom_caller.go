package server

import (
	"context"
	"fmt"

	"github.com/cbchoi/gorti/rti/internal/adapters/auth/session"
	grpcadapter "github.com/cbchoi/gorti/rti/internal/adapters/grpc"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/federation"
)

func newMOMCallerResolver(sessions *session.Manager, federations *federation.Manager, acquireObservation func(core.FederationName) func()) func(context.Context) (core.FederationName, core.FederateHandle, func(), error) {
	return func(ctx context.Context) (core.FederationName, core.FederateHandle, func(), error) {
		id, err := sessions.CallerIdentity(ctx)
		if err != nil {
			return "", 0, nil, err
		}
		checkpointRelease := func() {}
		if admitted, held := grpcadapter.CheckpointAdmissionFederation(ctx); held {
			if admitted != id.Federation {
				return "", 0, nil, fmt.Errorf("MOM caller: request and session federations differ")
			}
		} else if acquireObservation != nil {
			checkpointRelease = acquireObservation(id.Federation)
		}
		generationRelease, err := federations.AcquireGeneration(id.Federation, id.Generation)
		if err != nil {
			checkpointRelease()
			return "", 0, nil, err
		}
		release := func() { generationRelease(); checkpointRelease() }
		fail := func(err error) (core.FederationName, core.FederateHandle, func(), error) {
			release()
			return "", 0, nil, err
		}
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		// Resolve by stable membership identity under the lifecycle lease, never
		// by a pre-restore handle or by another member's numeric handle alone.
		for _, member := range federations.ListMembers(id.Federation) {
			if member.Name != id.Name {
				continue
			}
			current, err := federations.MemberIdentity(id.Federation, member.Handle)
			if err != nil {
				return fail(err)
			}
			if current.Generation != id.Generation || current.Incarnation != id.Incarnation {
				return fail(core.ErrFederateNotJoined)
			}
			if err := sessions.Authenticate(ctx, current.Federation, current.Handle); err != nil {
				return fail(err)
			}
			return current.Federation, current.Handle, release, nil
		}
		return fail(fmt.Errorf("MOM caller: %w", core.ErrFederateNotJoined))
	}
}
