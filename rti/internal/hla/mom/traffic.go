package mom

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"sync"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

const (
	failureOverflow             = "overflow"
	failureUnsupportedTransport = "unsupportedTransport"
)

var (
	ErrCounterOverflow             = errors.New("mom: counter exceeds HLAinteger32BE range")
	ErrCounterTransportUnsupported = errors.New("mom: traffic accounting requires a known actual transport")
)

type TrafficCountState struct {
	Kind  CounterKind
	Class uint64
	Count uint32
}

type TrafficFailureState struct {
	Kind   CounterKind
	Reason string
}

type TrafficFederateState struct {
	Handle           core.FederateHandle
	Counts           []TrafficCountState
	BestEffortCounts []TrafficCountState `json:",omitempty"`
	Failures         []TrafficFailureState
	ObjectHistory    *ObjectHistoryState
}

type TrafficState struct {
	Version    uint32
	Federation core.FederationName
	Federates  []TrafficFederateState
}

// TrafficAccumulator counts RTI-accepted sends and fanout events at the same
// boundary as the existing aggregate hooks. It does not measure network receipt
// or application callback invocation. Reliable and best-effort transportation
// are separate partitions; unknown observed types mark coverage unavailable.
type TrafficAccumulator struct {
	mu          sync.RWMutex
	federations map[core.FederationName]map[core.FederateHandle]*trafficFederate
}

type trafficFederate struct {
	counts           map[CounterKind]map[uint64]uint32
	bestEffortCounts map[CounterKind]map[uint64]uint32
	failures         map[CounterKind]string
	history          objectHistory
}

func newTrafficFederate() *trafficFederate {
	return &trafficFederate{counts: make(map[CounterKind]map[uint64]uint32), bestEffortCounts: make(map[CounterKind]map[uint64]uint32), failures: make(map[CounterKind]string), history: newObjectHistory()}
}

func (f *trafficFederate) transportCounts(transport core.TransportType) map[CounterKind]map[uint64]uint32 {
	if transport == core.TransportTypeBestEffort {
		return f.bestEffortCounts
	}
	return f.counts
}

func validCounterKind(kind CounterKind) bool {
	return kind == InteractionsSent || kind == InteractionsReceived || kind == UpdatesSent || kind == ReflectionsReceived
}

func counterFailure(reason string) error {
	switch reason {
	case failureOverflow:
		return ErrCounterOverflow
	case failureUnsupportedTransport:
		return ErrCounterTransportUnsupported
	default:
		return ErrCounterDataUnavailable
	}
}

func NewTrafficAccumulator() *TrafficAccumulator {
	return &TrafficAccumulator{federations: make(map[core.FederationName]map[core.FederateHandle]*trafficFederate)}
}

// FederateJoined must run at the beginning of the federate lifetime, not lazily
// after a first event. Duplicate calls do not reset already collected history.
func (a *TrafficAccumulator) FederateJoined(fed core.FederationName, h core.FederateHandle) error {
	if fed == "" || h == 0 {
		return fmt.Errorf("mom traffic: nonempty federation and valid federate required")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.federations == nil {
		a.federations = make(map[core.FederationName]map[core.FederateHandle]*trafficFederate)
	}
	if a.federations[fed] == nil {
		a.federations[fed] = make(map[core.FederateHandle]*trafficFederate)
	}
	if a.federations[fed][h] != nil {
		return fmt.Errorf("mom traffic: federate %d already tracked", h)
	}
	a.federations[fed][h] = newTrafficFederate()
	return nil
}

func (a *TrafficAccumulator) FederateResigned(fed core.FederationName, h core.FederateHandle) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.federations[fed], h)
}

func (a *TrafficAccumulator) FederationDestroyed(fed core.FederationName) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.federations, fed)
}

// Record accepts the actual transport at the accepted-send/fanout boundary.
// Never pass an unused configured override as though it were a delivered type.
func (a *TrafficAccumulator) Record(fed core.FederationName, h core.FederateHandle, kind CounterKind, class uint64, transport core.TransportType) error {
	if !validCounterKind(kind) || class == 0 {
		return fmt.Errorf("mom traffic: invalid kind or class")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	fs := a.federations[fed][h]
	if fs == nil {
		return core.ErrFederateNotJoined
	}
	if reason := fs.failures[kind]; reason != "" {
		return counterFailure(reason)
	}
	if transport != core.TransportTypeReliable && transport != core.TransportTypeBestEffort {
		fs.failures[kind] = failureUnsupportedTransport
		return ErrCounterTransportUnsupported
	}
	counts := fs.transportCounts(transport)
	if counts[kind] == nil {
		counts[kind] = make(map[uint64]uint32)
	}
	if counts[kind][class] == math.MaxInt32 {
		fs.failures[kind] = failureOverflow
		return ErrCounterOverflow
	}
	counts[kind][class]++
	return nil
}

func (a *TrafficAccumulator) Snapshot(ctx context.Context, fed core.FederationName, h core.FederateHandle, kind CounterKind) ([]TransportationCounts, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !validCounterKind(kind) {
		return nil, fmt.Errorf("mom traffic: invalid counter kind %q", kind)
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	fs := a.federations[fed][h]
	if fs == nil {
		return nil, core.ErrFederateNotJoined
	}
	if reason := fs.failures[kind]; reason != "" {
		return nil, counterFailure(reason)
	}
	counts := make(map[uint64]uint32, len(fs.counts[kind]))
	for class, count := range fs.counts[kind] {
		counts[class] = count
	}
	rows := []TransportationCounts{{Transportation: "HLAreliable", Counts: counts}}
	if len(fs.bestEffortCounts[kind]) != 0 {
		best := make(map[uint64]uint32, len(fs.bestEffortCounts[kind]))
		for class, count := range fs.bestEffortCounts[kind] {
			best[class] = count
		}
		rows = append(rows, TransportationCounts{Transportation: "HLAbestEffort", Counts: best})
	}
	return rows, nil
}

func (a *TrafficAccumulator) ExportState(fed core.FederationName) *TrafficState {
	a.mu.RLock()
	defer a.mu.RUnlock()
	state := &TrafficState{Version: 1, Federation: fed}
	var handles []core.FederateHandle
	for h := range a.federations[fed] {
		handles = append(handles, h)
	}
	sort.Slice(handles, func(i, j int) bool { return handles[i] < handles[j] })
	for _, h := range handles {
		fs := a.federations[fed][h]
		entry := TrafficFederateState{Handle: h, ObjectHistory: fs.history.export()}
		for kind, counts := range fs.counts {
			for class, count := range counts {
				entry.Counts = append(entry.Counts, TrafficCountState{Kind: kind, Class: class, Count: count})
			}
		}
		for kind, counts := range fs.bestEffortCounts {
			for class, count := range counts {
				entry.BestEffortCounts = append(entry.BestEffortCounts, TrafficCountState{Kind: kind, Class: class, Count: count})
			}
		}
		if len(entry.BestEffortCounts) != 0 {
			state.Version = 2
		}
		sort.Slice(entry.BestEffortCounts, func(i, j int) bool {
			if entry.BestEffortCounts[i].Kind != entry.BestEffortCounts[j].Kind {
				return entry.BestEffortCounts[i].Kind < entry.BestEffortCounts[j].Kind
			}
			return entry.BestEffortCounts[i].Class < entry.BestEffortCounts[j].Class
		})
		sort.Slice(entry.Counts, func(i, j int) bool {
			if entry.Counts[i].Kind != entry.Counts[j].Kind {
				return entry.Counts[i].Kind < entry.Counts[j].Kind
			}
			return entry.Counts[i].Class < entry.Counts[j].Class
		})
		for kind, reason := range fs.failures {
			entry.Failures = append(entry.Failures, TrafficFailureState{Kind: kind, Reason: reason})
		}
		sort.Slice(entry.Failures, func(i, j int) bool { return entry.Failures[i].Kind < entry.Failures[j].Kind })
		state.Federates = append(state.Federates, entry)
	}
	return state
}

func (state *TrafficState) Validate(fed core.FederationName) error {
	_, err := prepareTrafficState(fed, state)
	return err
}

func prepareTrafficState(fed core.FederationName, state *TrafficState) (map[core.FederateHandle]*trafficFederate, error) {
	if fed == "" {
		return nil, fmt.Errorf("mom traffic: empty federation")
	}
	if state == nil {
		return nil, nil
	}
	if (state.Version != 1 && state.Version != 2) || state.Federation != fed {
		return nil, fmt.Errorf("mom traffic: wrong snapshot version or federation")
	}
	prepared := make(map[core.FederateHandle]*trafficFederate, len(state.Federates))
	for _, entry := range state.Federates {
		if entry.Handle == 0 || prepared[entry.Handle] != nil {
			return nil, fmt.Errorf("mom traffic: invalid or duplicate federate")
		}
		fs := newTrafficFederate()
		var err error
		fs.history, err = prepareObjectHistory(entry.ObjectHistory)
		if err != nil {
			return nil, err
		}
		if err := restoreTrafficCounts(fs.counts, entry.Counts, "counter"); err != nil {
			return nil, err
		}
		if state.Version == 1 && len(entry.BestEffortCounts) != 0 {
			return nil, fmt.Errorf("mom traffic: best-effort counts require version 2")
		}
		if err := restoreTrafficCounts(fs.bestEffortCounts, entry.BestEffortCounts, "best-effort counter"); err != nil {
			return nil, err
		}
		if err := fs.restoreFailures(entry.Failures); err != nil {
			return nil, err
		}
		prepared[entry.Handle] = fs
	}
	return prepared, nil
}

// PrepareRestore validates and detaches the whole snapshot. External checkpoint
// admission must exclude ordinary events between preparation and commit.
func (a *TrafficAccumulator) PrepareRestore(fed core.FederationName, state *TrafficState) (func(), error) {
	prepared, err := prepareTrafficState(fed, state)
	if err != nil {
		return nil, err
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			a.mu.Lock()
			defer a.mu.Unlock()
			if prepared == nil {
				delete(a.federations, fed)
				return
			}
			if a.federations == nil {
				a.federations = make(map[core.FederationName]map[core.FederateHandle]*trafficFederate)
			}
			a.federations[fed] = prepared
		})
	}, nil
}
func (a *TrafficAccumulator) ImportState(fed core.FederationName, state *TrafficState) error {
	commit, err := a.PrepareRestore(fed, state)
	if err != nil {
		return err
	}
	commit()
	return nil
}
func restoreTrafficCounts(counts map[CounterKind]map[uint64]uint32, entries []TrafficCountState, description string) error {
	for _, count := range entries {
		if !validCounterKind(count.Kind) || count.Class == 0 || count.Count == 0 || count.Count > math.MaxInt32 {
			return fmt.Errorf("mom traffic: invalid %s record", description)
		}
		if counts[count.Kind] == nil {
			counts[count.Kind] = make(map[uint64]uint32)
		}
		if counts[count.Kind][count.Class] != 0 {
			return fmt.Errorf("mom traffic: duplicate %s record", description)
		}
		counts[count.Kind][count.Class] = count.Count
	}
	return nil
}

func (f *trafficFederate) restoreFailures(entries []TrafficFailureState) error {
	for _, failure := range entries {
		if !validCounterKind(failure.Kind) || (failure.Reason != failureOverflow && failure.Reason != failureUnsupportedTransport && failure.Reason != "unavailable" && failure.Reason != "entryAccountingUnavailable") || f.failures[failure.Kind] != "" {
			return fmt.Errorf("mom traffic: invalid or duplicate failure record")
		}
		if failure.Reason == failureOverflow {
			hasMax := false
			for _, count := range f.counts[failure.Kind] {
				if count == math.MaxInt32 {
					hasMax = true
				}
			}
			for _, count := range f.bestEffortCounts[failure.Kind] {
				if count == math.MaxInt32 {
					hasMax = true
				}
			}
			if !hasMax {
				return fmt.Errorf("mom traffic: overflow without saturated counter")
			}
		}
		f.failures[failure.Kind] = failure.Reason
	}
	return nil
}
