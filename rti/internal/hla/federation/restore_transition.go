package federation

import (
	"fmt"
	"sync"
	"time"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// RestoreIdentity names one session in a same-generation restore bijection.
type RestoreIdentity struct {
	Name                 string
	CurrentHandle        core.FederateHandle
	SavedHandle          core.FederateHandle
	SavedModules         []core.FOMModule
	ModulesKnown         bool
	SavedAutomaticResign *core.ResignAction
}

type restoreIdentityReservation struct {
	mu           sync.Mutex
	fs           *federationState
	names        map[string]core.FederateHandle
	handles      map[core.FederateHandle]string
	joined       map[core.FederateHandle]time.Time
	types        map[core.FederateHandle]string
	modules      map[core.FederateHandle][]core.FOMModule
	incarnations map[core.FederateHandle]uint64
	directives   map[string]core.ResignAction
	done         bool
}

// PrepareRestoreIdentity reserves the complete live roster, without upgrading
// the calling RPC's shared operation lease. Production callers must hold
// exclusive checkpoint admission through Commit/Release. Lock order is manager
// table (released here), roster, then the adapter's outbox/table/recipient locks.
// No lifecycle hook is invoked: dependent manager snapshots already contain
// saved identities. The monotonically allocated join counter is not rewound.
func (m *Manager) PrepareRestoreIdentity(fed core.FederationName, generation uint64, identities []RestoreIdentity) (core.OutboxReservation, error) {
	m.mu.RLock()
	fs := m.federations[fed]
	if fs == nil || fs.destroying {
		m.mu.RUnlock()
		return nil, core.ErrFederationNotFound
	}
	if fs.generation != generation {
		m.mu.RUnlock()
		return nil, core.ErrFederationGenerationMismatch
	}
	fs.mu.Lock()
	m.mu.RUnlock()
	r := &restoreIdentityReservation{fs: fs, names: map[string]core.FederateHandle{}, handles: map[core.FederateHandle]string{}, joined: map[core.FederateHandle]time.Time{}, types: map[core.FederateHandle]string{}}
	r.modules = make(map[core.FederateHandle][]core.FOMModule)
	r.incarnations = make(map[core.FederateHandle]uint64)
	r.directives = make(map[string]core.ResignAction)
	bad := func() (core.OutboxReservation, error) {
		fs.mu.Unlock()
		return nil, fmt.Errorf("federation restore: participant names/handles are not a same-generation bijection")
	}
	if len(identities) == 0 || len(identities) != len(fs.handleToName) {
		return bad()
	}
	for _, identity := range identities {
		if !r.validIdentity(identity, generation) {
			return bad()
		}
		r.names[identity.Name] = identity.SavedHandle
		r.handles[identity.SavedHandle] = identity.Name
		r.joined[identity.SavedHandle] = fs.joinedAt[identity.CurrentHandle]
		r.types[identity.SavedHandle] = fs.federateType[identity.CurrentHandle]
		r.incarnations[identity.SavedHandle] = fs.membershipIncarnation[identity.CurrentHandle]
		action := fs.automaticResign[identity.Name]
		if identity.SavedAutomaticResign != nil {
			action = *identity.SavedAutomaticResign
		}
		if !validResignAction(action) {
			return bad()
		}
		r.directives[identity.Name] = action
		// A rejoined session's modules are not the saved participant's baseline.
		// Absent history makes JoinedModules report unavailability explicitly.
		if identity.ModulesKnown {
			r.modules[identity.SavedHandle] = cloneFOMModules(identity.SavedModules)
		} else if identity.CurrentHandle == identity.SavedHandle {
			if modules, known := fs.joinedModules[identity.CurrentHandle]; known {
				r.modules[identity.SavedHandle] = cloneFOMModules(modules)
			}
		}
	}
	return r, nil
}

func (r *restoreIdentityReservation) validIdentity(identity RestoreIdentity, generation uint64) bool {
	saved := uint64(identity.SavedHandle)
	local := saved & uint64(^uint32(0))
	return identity.Name != "" && identity.CurrentHandle != 0 && identity.SavedHandle != 0 &&
		r.fs.nameToHandle[identity.Name] == identity.CurrentHandle && r.fs.handleToName[identity.CurrentHandle] == identity.Name &&
		r.names[identity.Name] == 0 && r.handles[identity.SavedHandle] == "" &&
		saved>>32 == generation && local != 0 && local <= r.fs.nextFederateHandle
}

func (r *restoreIdentityReservation) Commit() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.done {
		return nil
	}
	r.fs.nameToHandle, r.fs.handleToName = r.names, r.handles
	r.fs.joinedAt, r.fs.federateType = r.joined, r.types
	r.fs.joinedModules = r.modules
	r.fs.membershipIncarnation = r.incarnations
	r.fs.automaticResign = r.directives
	r.done = true
	r.fs.mu.Unlock()
	return nil
}

func (r *restoreIdentityReservation) Release() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.done {
		r.done = true
		r.fs.mu.Unlock()
	}
}
