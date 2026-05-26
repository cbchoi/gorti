package mom

import (
	"slices"
	"sync/atomic"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// momState holds the live attribute snapshot for one federation's MOM
// instances: one HLAfederation record (the federation itself) and a
// per-federate map of HLAfederate records.
//
// Concurrency: the struct's pointer is owned by Manager.fed[name]; it
// is only ever read after Manager.mu has been acquired (RLock for
// readers, Lock for writers that mutate the maps). The per-federate
// counter increments use atomic ops on the embedded uint32s so the
// hot-path metric updates do not need to take the manager mutex.
type momState struct {
	federation federationSnapshot
	federates  map[core.FederateHandle]*federateSnapshot
}

// federationSnapshot is the live HLAfederation attribute set.
//
// Mirrors mom.FederationAttributes; kept private so the public type is
// safe to copy out without aliasing the live state.
type federationSnapshot struct {
	name            core.FederationName
	federateHandles []core.FederateHandle // sorted ascending
	fomModuleNames  []string
	// M20.4 — federation-wide switches (HLAmanager.HLAfederation.
	// HLAadjust.HLAsetSwitches).
	autoProvideSwitch bool
}

// federateSnapshot is the live HLAfederate attribute set. Counter
// fields are uint32 so they can be incremented atomically without the
// manager mutex; the readonly snapshot accessor copies them out.
type federateSnapshot struct {
	handle          core.FederateHandle
	name            string
	federateType    string
	timeRegulating  bool
	timeConstrained bool
	lookahead       core.LogicalTime
	logicalTime     core.LogicalTime

	// counters — atomic increment, atomic load on snapshot
	interactionsSent     uint32
	interactionsReceived uint32
	updatesSent          uint32
	reflectionsReceived  uint32

	// M20.4 — per-federate switches (HLAmanager.HLAfederate.
	// HLAadjust.HLAsetSwitches).
	conveyRegionDesignatorSetsSwitch bool
	conveyProducingFederateSwitch    bool
}

func newMOMState() *momState {
	return &momState{
		federates: map[core.FederateHandle]*federateSnapshot{},
	}
}

// addFederateHandle inserts h into the federation snapshot's handle
// list, preserving sorted order without duplicates.
func (s *momState) addFederateHandle(h core.FederateHandle) {
	if _, exists := s.findHandle(h); exists {
		return
	}
	s.federation.federateHandles = append(s.federation.federateHandles, h)
	slices.Sort(s.federation.federateHandles)
}

// removeFederateHandle removes h from the federation snapshot's handle
// list. No-op if absent.
func (s *momState) removeFederateHandle(h core.FederateHandle) {
	idx, ok := s.findHandle(h)
	if !ok {
		return
	}
	s.federation.federateHandles = append(
		s.federation.federateHandles[:idx],
		s.federation.federateHandles[idx+1:]...,
	)
}

// findHandle returns the index of h in the federation handle list and
// whether it was found. The list is kept sorted, so a binary search
// would suffice, but federations rarely exceed a few dozen federates so
// the linear scan keeps the code obvious.
func (s *momState) findHandle(h core.FederateHandle) (int, bool) {
	for i, existing := range s.federation.federateHandles {
		if existing == h {
			return i, true
		}
	}
	return -1, false
}

// snapshotFederation returns a copy of the public attribute view for
// the HLAfederation MOM instance. Lists are deep-copied so callers may
// retain the result safely.
func (s *momState) snapshotFederation() FederationAttributes {
	attrs := FederationAttributes{
		Name: s.federation.name,
	}
	if len(s.federation.federateHandles) > 0 {
		attrs.FederateHandles = make([]core.FederateHandle, len(s.federation.federateHandles))
		copy(attrs.FederateHandles, s.federation.federateHandles)
	}
	if len(s.federation.fomModuleNames) > 0 {
		attrs.FOMModuleNames = make([]string, len(s.federation.fomModuleNames))
		copy(attrs.FOMModuleNames, s.federation.fomModuleNames)
	}
	return attrs
}

// snapshotFederate returns a copy of the public attribute view for the
// given HLAfederate MOM instance. Counter values are read atomically so
// concurrent IncrementX calls don't tear.
func (fs *federateSnapshot) snapshot() FederateAttributes {
	return FederateAttributes{
		Handle:               fs.handle,
		Name:                 fs.name,
		Type:                 fs.federateType,
		TimeRegulating:       fs.timeRegulating,
		TimeConstrained:      fs.timeConstrained,
		Lookahead:            fs.lookahead,
		LogicalTime:          fs.logicalTime,
		InteractionsSent:     atomic.LoadUint32(&fs.interactionsSent),
		InteractionsReceived: atomic.LoadUint32(&fs.interactionsReceived),
		UpdatesSent:          atomic.LoadUint32(&fs.updatesSent),
		ReflectionsReceived:  atomic.LoadUint32(&fs.reflectionsReceived),
	}
}

// fomModuleNamesFor extracts the display name of each FOM module. The
// Path field is preferred; when empty (e.g. an inline XML module), the
// fallback "module-N" placeholder keeps the list stable so MOM
// subscribers see a non-empty entry per module.
func fomModuleNamesFor(modules []core.FOMModule) []string {
	if len(modules) == 0 {
		return nil
	}
	out := make([]string, 0, len(modules))
	for i, m := range modules {
		if m.Path != "" {
			out = append(out, m.Path)
			continue
		}
		out = append(out, placeholderFOMName(i))
	}
	return out
}

// placeholderFOMName builds a deterministic stand-in name for inline
// (path-less) FOM modules so callers always see a non-empty entry per
// module. Index is 0-based; the human-facing label is 1-based.
func placeholderFOMName(idx int) string {
	// Manual int→string formatting to avoid the fmt dependency at this
	// hot path and keep the linter quiet about unconditional fmt.Sprintf
	// calls in helpers.
	return "module-" + itoa(idx+1)
}

// itoa is a minimal positive-int decimal formatter. Avoids strconv to
// keep the helpers module tiny; modules-per-federation is small so the
// allocation cost is negligible.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	if n < 0 {
		return "-" + itoa(-n)
	}
	var digits [20]byte
	i := len(digits)
	for n > 0 {
		i--
		digits[i] = byte('0' + n%10)
		n /= 10
	}
	return string(digits[i:])
}
