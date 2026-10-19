package object

import (
	"context"
	"fmt"
	"slices"
	"unicode/utf8"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// ReplayObjectRegistration restores a journaled admission without fabricating a
// reservation or allocating another handle. It is deliberately separate from
// Register: journal records contain canonical names, including generated HLA
// names, but contain neither reservation requests nor releases.
func (r *RegistryOf[T]) ReplayObjectRegistration(ctx context.Context, fed core.FederationName, record core.ObjectRegistrationRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := r.validateRecordedRegistration(ctx, fed, record); err != nil {
		return err
	}
	published := r.registrationAttributes(ctx, fed, record.Registrant, record.Class)
	if len(published) == 0 {
		return core.ErrObjectClassNotPublished
	}
	owned := slices.Clone(published)
	st := r.stateFor(fed)
	st.mu.Lock()
	if uint64(record.Object) <= st.nextObjectHandle || st.instances[record.Object] != nil {
		st.mu.Unlock()
		return fmt.Errorf("%w: recorded handle %d is not newer than the prior allocation", core.ErrObjectRegistrationReplayState, record.Object)
	}
	if _, occupied := st.nameToHandle[record.Name]; occupied {
		st.mu.Unlock()
		return fmt.Errorf("%w: recorded name %q is occupied", core.ErrObjectRegistrationReplayState, record.Name)
	}
	err := r.reservations.replayRegistration(fed, record, func() error {
		if r.opts.EventLog == nil {
			return nil
		}
		return r.opts.EventLog.Append(ctx, fed, &core.ObjectJournalRecordOf[T]{
			Kind: core.ObjectJournalRegistered, Object: record.Object,
			ObjectClass: record.Class, Federate: record.Registrant,
			ObjectName: record.Name, WallNS: record.WallNS,
		})
	})
	if err != nil {
		st.mu.Unlock()
		return err
	}
	inst := &objectInstance{handle: record.Object, name: record.Name, cls: record.Class, owner: record.Registrant}
	st.nextObjectHandle = uint64(record.Object)
	st.instances[record.Object] = inst
	st.nameToHandle[record.Name] = record.Object
	if record.Registrant != internalProducer {
		st.discovered[discoverKey{sub: record.Registrant, obj: record.Object}] = record.Class
	}
	if r.opts.OnRegister != nil {
		r.opts.OnRegister(fed, record.Registrant, record.Object, record.Class, owned)
	}
	st.mu.Unlock()
	r.fanoutDiscover(ctx, fed, st, record.Registrant, inst, published)
	if r.opts.OnObjectRegistered != nil {
		r.opts.OnObjectRegistered(ctx, fed, record.Registrant, record.Object, record.Class, slices.Clone(owned))
	}
	return nil
}

// Caller holds the object-state lock, matching normal registration's lock order.
// A matching reservation from a prior snapshot is retained; a conflicting one
// indicates that replay is not starting from the record's preceding state.
func (s *reservationStore) replayRegistration(fed core.FederationName, record core.ObjectRegistrationRecord, persist func() error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, occupied := s.registered[fed][record.Name]; occupied {
		return fmt.Errorf("%w: recorded name is registered", core.ErrObjectRegistrationReplayState)
	}
	if holder, reserved := s.reservations[fed][record.Name]; reserved && holder != record.Registrant {
		return fmt.Errorf("%w: recorded name is reserved by another federate", core.ErrObjectRegistrationReplayState)
	}
	if err := persist(); err != nil {
		return err
	}
	registered := s.registered[fed]
	if registered == nil {
		registered = make(map[string]struct{})
		s.registered[fed] = registered
	}
	registered[record.Name] = struct{}{}
	return nil
}

var _ core.ObjectRegistrationReplay = (*Registry)(nil)

func (r *RegistryOf[T]) validateRecordedRegistration(ctx context.Context, fed core.FederationName, record core.ObjectRegistrationRecord) error {
	if fed == "" || record.Object == 0 || record.Class == 0 || record.Registrant == 0 || record.Name == "" || !utf8.ValidString(record.Name) {
		return fmt.Errorf("%w: invalid recorded identity", core.ErrObjectRegistrationReplayState)
	}
	fom, err := r.opts.FOMs.Get(ctx, fed)
	if err != nil {
		return fmt.Errorf("object: replay FOM: %w", err)
	}
	lookup, ok := fom.(interface {
		ObjectClassName(core.ObjectClassHandle) (string, bool)
	})
	if !ok || isNilInterface(fom) || !fom.IsValid() {
		return fmt.Errorf("%w: FOM cannot validate recorded class", core.ErrObjectRegistrationReplayState)
	}
	if _, exists := lookup.ObjectClassName(record.Class); !exists {
		return fmt.Errorf("%w: unknown recorded class %d", core.ErrObjectRegistrationReplayState, record.Class)
	}
	return nil
}
