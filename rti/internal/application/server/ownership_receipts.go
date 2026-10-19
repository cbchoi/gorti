package server

import (
	"context"
	"fmt"
	"github.com/cbchoi/gorti/rti/internal/adapters/delivery"
	grpcadapter "github.com/cbchoi/gorti/rti/internal/adapters/grpc"
	"github.com/cbchoi/gorti/rti/internal/adapters/savebundle"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/federation"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
	savepoint "github.com/cbchoi/gorti/rti/internal/hla/save_restore"
	"sync"
)

type callbackEpochSource interface {
	CallbackEpoch(core.FederationName, core.FederateHandle) (uint64, error)
}

type saveObservationAdmission interface {
	AcquireObservation(core.FederationName) func()
}

func newOwnershipScopeAdmission(members *federation.Manager, epochs callbackEpochSource, save saveObservationAdmission, authenticate func(context.Context, core.FederationName, core.FederateHandle) error) func(context.Context, core.OwnershipCallbackScope) (func(), error) {
	return func(ctx context.Context, scope core.OwnershipCallbackScope) (func(), error) {
		checkpointRelease := func() {}
		if save != nil {
			checkpointRelease = save.AcquireObservation(scope.Federation)
		}
		release, err := members.AcquireMember(scope.Federation, scope.Recipient)
		if err != nil {
			checkpointRelease()
			return nil, err
		}
		done := func() { release(); checkpointRelease() }
		if err = ctx.Err(); err != nil {
			done()
			return nil, err
		}
		if err = authenticate(ctx, scope.Federation, scope.Recipient); err != nil {
			done()
			return nil, err
		}
		generation, found := members.GenerationFor(scope.Federation)
		if !found || generation != scope.FederationGeneration {
			done()
			return nil, core.ErrOwnershipCallbackScope
		}
		epoch, err := epochs.CallbackEpoch(scope.Federation, scope.Recipient)
		if err != nil {
			done()
			return nil, err
		}
		if epoch != scope.CallbackEpoch {
			done()
			return nil, core.ErrOwnershipCallbackScope
		}
		return done, nil
	}
}

func newMandatoryOwnershipReceipts(owner *ownership.Manager, members *federation.Manager, epochs callbackEpochSource, save saveObservationAdmission, authenticate func(context.Context, core.FederationName, core.FederateHandle) error) (*grpcadapter.OwnershipCallbackReceipts, error) {
	return grpcadapter.NewOwnershipCallbackReceipts(grpcadapter.OwnershipCallbackReceiptOptions{
		Coordinator: owner, Authenticate: authenticate,
		AcquireScope: newOwnershipScopeAdmission(members, epochs, save, authenticate),
		RecoveryScope: func(ctx context.Context, fed core.FederationName, h core.FederateHandle) (core.OwnershipCallbackScope, error) {
			if err := ctx.Err(); err != nil {
				return core.OwnershipCallbackScope{}, err
			}
			generation, found := members.GenerationFor(fed)
			if !found {
				return core.OwnershipCallbackScope{}, core.ErrOwnershipCallbackScope
			}
			epoch, err := epochs.CallbackEpoch(fed, h)
			if err != nil {
				return core.OwnershipCallbackScope{}, err
			}
			return core.OwnershipCallbackScope{Federation: fed, Recipient: h, FederationGeneration: generation, CallbackEpoch: epoch}, nil
		},
	})
}

type ownershipScopeRestoreReservation struct {
	core.OutboxReservation
	owner      *ownership.Manager
	federation core.FederationName
	scopes     map[core.FederateHandle]core.OwnershipCallbackScope
	mu         sync.Mutex
	done       bool
	err        error
}

func (r *ownershipScopeRestoreReservation) Commit() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.done {
		return r.err
	}
	r.done = true
	prepared, err := r.owner.PrepareOwnershipCallbackScopeRestore(r.federation, r.scopes)
	if err != nil {
		r.OutboxReservation.Release()
		r.err = err
		return err
	}
	defer prepared.Release()
	if err := prepared.Commit(); err != nil {
		r.OutboxReservation.Release()
		r.err = err
		return err
	}
	r.err = r.OutboxReservation.Commit()
	return r.err
}

func (r *ownershipScopeRestoreReservation) Release() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.done {
		r.done = true
		r.OutboxReservation.Release()
	}
}

func newOwnershipRestoreTransition(owner *ownership.Manager, members *federation.Manager, outbox *delivery.MultiOutbox) func(context.Context, core.FederationName, savepoint.Manifest, []savepoint.RestoreParticipant, []core.OutboxDelivery) (core.OutboxReservation, error) {
	return newOwnershipRestoreTransitionOf[core.LogicalTime](owner, members, outbox)
}

func newOwnershipRestoreTransitionOf[T core.LogicalTimeScalar](owner *ownership.Manager, members *federation.Manager, outbox *delivery.MultiOutbox) func(context.Context, core.FederationName, savepoint.ManifestOf[T], []savepoint.RestoreParticipant, []core.OutboxDelivery) (core.OutboxReservation, error) {
	return newOwnershipRestoreTransitionWithPendingOf[T](owner, members, outbox, nil)
}

func newOwnershipRestoreTransitionWithPendingOf[T core.LogicalTimeScalar](owner *ownership.Manager, members *federation.Manager, outbox *delivery.MultiOutbox, pending map[core.FederateHandle][]core.OutboundEvent) func(context.Context, core.FederationName, savepoint.ManifestOf[T], []savepoint.RestoreParticipant, []core.OutboxDelivery) (core.OutboxReservation, error) {
	return func(ctx context.Context, fed core.FederationName, manifest savepoint.ManifestOf[T], participants []savepoint.RestoreParticipant, terminal []core.OutboxDelivery) (core.OutboxReservation, error) {
		saved, err := savebundle.OwnershipCallbackScopes(manifest.ManagerSnapshots[savepoint.ManagerSnapshotKeyOwnership])
		if err != nil {
			return nil, err
		}
		generation, found := members.GenerationFor(fed)
		if !found {
			return nil, core.ErrFederationNotFound
		}
		floors := make(map[core.FederateHandle]uint64, len(saved))
		known := make(map[core.FederateHandle]bool, len(participants))
		for _, p := range participants {
			known[p.SavedHandle] = true
			if scope, enabled := saved[p.SavedHandle]; enabled {
				floors[p.CurrentHandle] = scope.CallbackEpoch
			}
		}
		for h := range saved {
			if !known[h] {
				return nil, fmt.Errorf("restore callback session is not a participant: %d", h)
			}
		}
		transition, err := newRestoreTransitionWithPending(members, outbox, floors, pending)(ctx, fed, participants, terminal)
		if err != nil {
			return nil, err
		}
		planned := transition.(*restoreTransitionReservation)
		scopes := make(map[core.FederateHandle]core.OwnershipCallbackScope, len(saved))
		for _, p := range participants {
			if _, enabled := saved[p.SavedHandle]; enabled {
				scopes[p.SavedHandle] = core.OwnershipCallbackScope{Federation: fed, Recipient: p.SavedHandle, FederationGeneration: generation, CallbackEpoch: planned.epochs[p.CurrentHandle]}
			}
		}
		return &ownershipScopeRestoreReservation{OutboxReservation: transition, owner: owner, federation: fed, scopes: scopes}, nil
	}
}
