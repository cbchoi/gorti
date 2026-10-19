package mom

import (
	"fmt"
	"math"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

const (
	serviceReceiveInteraction     = "receiveInteraction"
	serviceReflectAttributeValues = "reflectAttributeValues"
	metricInteractionsReceived    = "HLAinteractionsReceived"
	metricReflectionsReceived     = "HLAreflectionsReceived"
)

// DataCallbackEntry records one callback invocation, irrespective of payload size.
type DataCallbackEntry struct {
	Service          string
	Object           core.ObjectHandle
	ObjectClass      core.ObjectClassHandle
	InteractionClass core.InteractionClassHandle
	Transportation   core.TransportType
}

// CallbackCompletion preserves completion-only observation for legacy callers.
// DataCountedAtEntry also covers unavailable accounting handled at ENTRY.
type CallbackCompletion struct {
	Invocation         CallbackInvocation
	DataCountedAtEntry bool
}

func (m *Manager) RecordCallbackCompletion(fed core.FederationName, h core.FederateHandle, completion CallbackCompletion) error {
	if completion.DataCountedAtEntry && (completion.Invocation.Service == serviceReflectAttributeValues || completion.Invocation.Service == serviceReceiveInteraction) {
		return nil
	}
	return m.RecordCallbackInvocation(fed, h, completion.Invocation)
}

// DataCallbackEntryUnavailable affects telemetry, never callback admission.
func (m *Manager) DataCallbackEntryUnavailable(fed core.FederationName, h core.FederateHandle, entry DataCallbackEntry, cause error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.traffic != nil {
		m.traffic.mu.Lock()
		defer m.traffic.mu.Unlock()
	}
	m.dataCallbackEntryUnavailableLocked(fed, h, entry, cause)
}

func (m *Manager) dataCallbackEntryUnavailableLocked(fed core.FederationName, h core.FederateHandle, entry DataCallbackEntry, cause error) {
	if cause == nil {
		return
	}
	var metric string
	var kind CounterKind
	switch entry.Service {
	case serviceReceiveInteraction:
		metric, kind = metricInteractionsReceived, InteractionsReceived
	case serviceReflectAttributeValues:
		metric, kind = metricReflectionsReceived, ReflectionsReceived
	default:
		return
	}
	if r, err := m.runtimeMetricsLocked(fed, h); err == nil {
		r.Failures[metric] = cause.Error()
		if kind == ReflectionsReceived {
			r.Failures["HLAobjectInstancesReflected"] = cause.Error()
		}
	}
	if m.traffic == nil {
		return
	}
	if fs := m.traffic.federations[fed][h]; fs != nil {
		fs.failures[kind] = "entryAccountingUnavailable"
		if kind == ReflectionsReceived {
			fs.history.unavailable[ObjectsReflected] = true
		}
	}
}

// RecordDataCallbackEntry prevalidates optional telemetry before changing either
// runtime or class-partition counters. Its caller marks unavailable coverage on
// error instead of rejecting callback admission. Receipt retry serialization is
// outside this domain helper; no receipt identity or persistence format lives here.
func (m *Manager) RecordDataCallbackEntry(fed core.FederationName, h core.FederateHandle, entry DataCallbackEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.traffic != nil {
		m.traffic.mu.Lock()
		defer m.traffic.mu.Unlock()
	}
	return m.recordDataCallbackEntryLocked(fed, h, entry)
}

func (m *Manager) recordDataCallbackEntryLocked(fed core.FederationName, h core.FederateHandle, entry DataCallbackEntry) error {
	kind, metric, class, err := dataCallbackIdentity(entry)
	if err != nil {
		return err
	}
	r, err := m.runtimeMetricsLocked(fed, h)
	if err != nil {
		return err
	}
	if m.traffic == nil {
		return ErrCounterDataUnavailable
	}
	fs := m.traffic.federations[fed][h]
	if fs == nil {
		return core.ErrFederateNotJoined
	}
	if why := r.Failures[metric]; why != "" {
		return fmt.Errorf("MOM: %s: %s", metric, why)
	}
	if why := fs.failures[kind]; why != "" {
		return counterFailure(why)
	}
	counts := fs.transportCounts(entry.Transportation)
	if r.Counters[metric] >= math.MaxInt32 || counts[kind][class] >= math.MaxInt32 {
		return ErrCounterOverflow
	}
	var reflectedCount uint32
	if kind == ReflectionsReceived {
		reflectedCount, err = validateReflectedEntry(r, fs, entry)
		if err != nil {
			return err
		}
	}
	if counts[kind] == nil {
		counts[kind] = make(map[uint64]uint32)
	}
	counts[kind][class]++
	r.Counters[metric]++
	if kind == ReflectionsReceived {
		r.ReflectedObjects[entry.Object] = entry.ObjectClass
		r.Counters["HLAobjectInstancesReflected"] = reflectedCount
		fs.history.objects[ObjectsReflected][entry.Object] = entry.ObjectClass
	}
	return nil
}
func validateReflectedEntry(r *RuntimeMetricState, fs *trafficFederate, entry DataCallbackEntry) (uint32, error) {
	if why := r.Failures["HLAobjectInstancesReflected"]; why != "" {
		return 0, fmt.Errorf("MOM: HLAobjectInstancesReflected: %s", why)
	}
	if fs.history.unavailable[ObjectsReflected] {
		return 0, ErrCounterDataUnavailable
	}
	// The existing snapshot format requires consistent historical classes.
	if prior := fs.history.objects[ObjectsUpdated][entry.Object]; prior != 0 && prior != entry.ObjectClass {
		return 0, ErrCounterDataUnavailable
	}
	count := uint64(len(r.ReflectedObjects))
	if r.ReflectedObjects[entry.Object] == 0 {
		count++
	}
	if count > math.MaxInt32 {
		return 0, ErrCounterOverflow
	}
	if fs.history.objects[ObjectsReflected][entry.Object] == 0 && uint64(len(fs.history.objects[ObjectsReflected])) >= math.MaxInt32 {
		return 0, ErrCounterOverflow
	}
	return uint32(count), nil
}
func dataCallbackIdentity(entry DataCallbackEntry) (CounterKind, string, uint64, error) {
	var kind CounterKind
	var metric string
	var class uint64
	switch entry.Service {
	case serviceReflectAttributeValues:
		kind, metric, class = ReflectionsReceived, metricReflectionsReceived, uint64(entry.ObjectClass)
		if entry.Object == 0 {
			return kind, metric, class, fmt.Errorf("MOM: reflected object identity unavailable")
		}
	case serviceReceiveInteraction:
		kind, metric, class = InteractionsReceived, metricInteractionsReceived, uint64(entry.InteractionClass)
	default:
		return kind, metric, class, fmt.Errorf("MOM: unsupported data callback %q", entry.Service)
	}
	if class == 0 {
		return kind, metric, class, fmt.Errorf("MOM: callback class identity unavailable")
	}
	if entry.Transportation != core.TransportTypeReliable && entry.Transportation != core.TransportTypeBestEffort {
		return kind, metric, class, ErrCounterTransportUnsupported
	}
	return kind, metric, class, nil
}
