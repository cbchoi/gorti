package mom

import (
	"context"
	"fmt"
	"math"
	"slices"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type ObjectHistoryEntry struct {
	Object core.ObjectHandle
	Class  core.ObjectClassHandle
}

// A nil history in an older TrafficFederateState is unavailable, not empty.
type ObjectHistoryState struct {
	Updated     []ObjectHistoryEntry
	Reflected   []ObjectHistoryEntry
	Unavailable []ObjectInstanceCountKind
}

type objectHistory struct {
	objects     map[ObjectInstanceCountKind]map[core.ObjectHandle]core.ObjectClassHandle
	unavailable map[ObjectInstanceCountKind]bool
}

func newObjectHistory() objectHistory {
	return objectHistory{objects: map[ObjectInstanceCountKind]map[core.ObjectHandle]core.ObjectClassHandle{ObjectsUpdated: {}, ObjectsReflected: {}}, unavailable: map[ObjectInstanceCountKind]bool{}}
}

func validObjectHistoryKind(k ObjectInstanceCountKind) bool {
	return k == ObjectsUpdated || k == ObjectsReflected
}

// RecordObjectTraffic records distinct instance identities at the same RTI
// accepted update/fanout boundary as Record, not physical callback receipt.
func (a *TrafficAccumulator) RecordObjectTraffic(fed core.FederationName, h core.FederateHandle, obj core.ObjectHandle, class core.ObjectClassHandle, incoming bool) error {
	if obj == 0 || class == 0 {
		return fmt.Errorf("mom: object history requires nonzero object and class")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	fs := a.federations[fed][h]
	if fs == nil {
		return core.ErrFederateNotJoined
	}
	kind := ObjectsUpdated
	if incoming {
		kind = ObjectsReflected
	}
	if fs.history.unavailable[kind] {
		return ErrCounterDataUnavailable
	}
	for _, objects := range fs.history.objects {
		if prior := objects[obj]; prior != 0 && prior != class {
			fs.history.unavailable[kind] = true
			return fmt.Errorf("mom: conflicting registered class for object %d", obj)
		}
	}
	fs.history.objects[kind][obj] = class
	return nil
}

func (a *TrafficAccumulator) ObjectInstanceCounts(ctx context.Context, fed core.FederationName, h core.FederateHandle, kind ObjectInstanceCountKind) (map[uint64]uint32, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !validObjectHistoryKind(kind) {
		return nil, fmt.Errorf("mom: unsupported historical object kind %q", kind)
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	fs := a.federations[fed][h]
	if fs == nil {
		return nil, core.ErrFederateNotJoined
	}
	if fs.history.unavailable[kind] {
		return nil, ErrCounterDataUnavailable
	}
	counts := map[uint64]uint32{}
	for _, class := range fs.history.objects[kind] {
		if counts[uint64(class)] == math.MaxInt32 {
			return nil, ErrCounterOverflow
		}
		counts[uint64(class)]++
	}
	return counts, nil
}

func (h objectHistory) export() *ObjectHistoryState {
	state := &ObjectHistoryState{}
	for _, kind := range []ObjectInstanceCountKind{ObjectsUpdated, ObjectsReflected} {
		var entries []ObjectHistoryEntry
		for obj, class := range h.objects[kind] {
			entries = append(entries, ObjectHistoryEntry{Object: obj, Class: class})
		}
		slices.SortFunc(entries, func(a, b ObjectHistoryEntry) int {
			if a.Object < b.Object {
				return -1
			}
			if a.Object > b.Object {
				return 1
			}
			return 0
		})
		if kind == ObjectsUpdated {
			state.Updated = entries
		} else {
			state.Reflected = entries
		}
		if h.unavailable[kind] {
			state.Unavailable = append(state.Unavailable, kind)
		}
	}
	return state
}

func prepareObjectHistory(state *ObjectHistoryState) (objectHistory, error) {
	h := newObjectHistory()
	if state == nil {
		h.unavailable[ObjectsUpdated] = true
		h.unavailable[ObjectsReflected] = true
		return h, nil
	}
	seen := map[core.ObjectHandle]core.ObjectClassHandle{}
	for kind, entries := range map[ObjectInstanceCountKind][]ObjectHistoryEntry{ObjectsUpdated: state.Updated, ObjectsReflected: state.Reflected} {
		for _, e := range entries {
			if e.Object == 0 || e.Class == 0 || h.objects[kind][e.Object] != 0 || (seen[e.Object] != 0 && seen[e.Object] != e.Class) {
				return objectHistory{}, fmt.Errorf("mom: invalid or conflicting object history record")
			}
			h.objects[kind][e.Object] = e.Class
			seen[e.Object] = e.Class
		}
	}
	for _, kind := range state.Unavailable {
		if !validObjectHistoryKind(kind) || h.unavailable[kind] {
			return objectHistory{}, fmt.Errorf("mom: invalid or duplicate object history failure")
		}
		h.unavailable[kind] = true
	}
	return h, nil
}
