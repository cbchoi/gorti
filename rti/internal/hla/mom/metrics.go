package mom

import (
	"context"
	"encoding/binary"
	"fmt"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/pkg/logicaltime"
	"maps"
	"math"
	"strings"
	"time"
)

type LogicalTimeMetric struct {
	Defined    bool
	Value      core.LogicalTime
	ExactValue []byte
}
type TimeMetrics struct {
	Regulating, Constrained, AsynchronousDelivery, Advancing bool
	LogicalTime                                              core.LogicalTime
	LogicalTimeValue                                         *logicaltime.Time
	Lookahead, GALT, LITS                                    LogicalTimeMetric
	TSOQueued                                                *uint64
	GrantedMilliseconds, AdvancingMilliseconds               *uint64
}
type FederateMetrics struct {
	Host, RTIVersion           *string
	Time                       *TimeMetrics
	FederateState              *uint32
	ROQueued, DeletableObjects *uint64
}
type FederationMetrics struct {
	RTIVersion, MIMDesignator, CurrentFDD, TimeImplementationName *string
	LastSaveName, NextSaveName                                    *string
	LastSaveTime, NextSaveTime                                    *LogicalTimeMetric
}
type MetricSources struct {
	Federate    func(context.Context, core.FederationName, core.FederateHandle) (FederateMetrics, error)
	Federation  func(context.Context, core.FederationName) (FederationMetrics, error)
	ElapsedTime func(context.Context, core.FederationName, core.FederateHandle) (ElapsedTimeMetrics, error)
}

// ElapsedTimeAttributes identifies the independently updated MIM attributes.
type ElapsedTimeAttributes struct{ Granted, Advancing bool }

// ElapsedTimeMetrics carries unconsumed durations and a measurement-bound
// acknowledgement. Acknowledge must not sample the clock again or consume any
// duration beyond this measurement; it retains sub-millisecond remainders.
// Both values are unavailable if this required acknowledgement port is nil.
type ElapsedTimeMetrics struct {
	Granted, Advancing time.Duration
	Acknowledge        func(ElapsedTimeAttributes) error
}
type MetricSnapshot struct {
	Values                 map[string][]byte
	Unavailable            map[string]string
	elapsedAcknowledgement func(ElapsedTimeAttributes) error
}
type RuntimeMetricState struct {
	Counters         map[string]uint32
	Failures         map[string]string
	PendingCallbacks uint64
	// Distinct reflected objects and their latest observed callback-known class.
	// Unlike the registered class, this can change after local deletion and rediscovery.
	ReflectedObjects map[core.ObjectHandle]core.ObjectClassHandle
}
type CallbackInvocation struct {
	Service     string
	Object      core.ObjectHandle
	ObjectClass core.ObjectClassHandle
}

func (m *Manager) SetMetricSources(s MetricSources) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.metricSources = s
}
func (m *Manager) SetRuntimeMetricsEnabled(enabled bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, st := range m.fed {
		if len(st.federates) > 0 {
			return fmt.Errorf("MOM: runtime metrics must be configured before joins")
		}
	}
	m.runtimeMetricsEnabled = enabled
	return nil
}

// SetDataEntryMetricsEnabled selects the authoritative data ENTRY boundary.
// Enable only with mandatory ENTRY reporting and completion double-count guards.
func (m *Manager) SetDataEntryMetricsEnabled(enabled bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, st := range m.fed {
		if len(st.federates) > 0 {
			return fmt.Errorf("MOM: data ENTRY metrics must be configured before joins")
		}
	}
	m.dataEntryMetricsEnabled = enabled
	return nil
}

var runtimeCounterNames = []string{"HLAupdatesSent", "HLAinteractionsSent", "HLAobjectInstancesRegistered", "HLAobjectInstancesDeleted", metricReflectionsReceived, metricInteractionsReceived, "HLAobjectInstancesDiscovered", "HLAobjectInstancesRemoved", "HLAobjectInstancesReflected"}
var callbackCounterNames = runtimeCounterNames[4:]

const federateMetricNames = `HLAfederateHandle HLAfederateName HLAfederateType HLAfederateHost HLARTIversion HLAFOMmoduleDesignatorList HLAtimeConstrained HLAtimeRegulating HLAasynchronousDelivery HLAfederateState HLAtimeManagerState HLAlogicalTime HLAlookahead HLAGALT HLALITS HLAROlength HLATSOlength HLAreflectionsReceived HLAupdatesSent HLAinteractionsReceived HLAinteractionsSent HLAobjectInstancesThatCanBeDeleted HLAobjectInstancesUpdated HLAobjectInstancesReflected HLAobjectInstancesDeleted HLAobjectInstancesRemoved HLAobjectInstancesRegistered HLAobjectInstancesDiscovered HLAtimeGrantedTime HLAtimeAdvancingTime HLAconveyRegionDesignatorSets HLAconveyProducingFederate`

const federationMetricNames = `HLAfederationName HLAfederatesInFederation HLARTIversion HLAMIMdesignator HLAFOMmoduleDesignatorList HLAcurrentFDD HLAtimeImplementationName HLAlastSaveName HLAlastSaveTime HLAAnnexSaveName HLAnextSaveTime HLAautoProvide`

func newRuntimeMetrics() *RuntimeMetricState {
	return &RuntimeMetricState{Counters: map[string]uint32{}, Failures: map[string]string{}, ReflectedObjects: map[core.ObjectHandle]core.ObjectClassHandle{}}
}
func cloneRuntimeMetrics(s *RuntimeMetricState) *RuntimeMetricState {
	if s == nil {
		return nil
	}
	out := &RuntimeMetricState{Counters: maps.Clone(s.Counters), Failures: maps.Clone(s.Failures), PendingCallbacks: s.PendingCallbacks, ReflectedObjects: maps.Clone(s.ReflectedObjects)}
	if out.Counters == nil {
		out.Counters = map[string]uint32{}
	}
	if out.Failures == nil {
		out.Failures = map[string]string{}
	}
	if out.ReflectedObjects == nil {
		out.ReflectedObjects = map[core.ObjectHandle]core.ObjectClassHandle{}
	}
	return out
}
func (m *Manager) runtimeMetricsLocked(fed core.FederationName, h core.FederateHandle) (*RuntimeMetricState, error) {
	st := m.fed[fed]
	if st == nil || st.federates[h] == nil {
		return nil, core.ErrFederateNotJoined
	}
	r := st.federates[h].runtimeMetrics
	if r == nil {
		return nil, fmt.Errorf("MOM: runtime metric baseline unavailable")
	}
	return r, nil
}
func incrementMetric(r *RuntimeMetricState, name string) error {
	if why := r.Failures[name]; why != "" {
		return fmt.Errorf("MOM: %s: %s", name, why)
	}
	if r.Counters[name] >= math.MaxInt32 {
		r.Failures[name] = failureOverflow
		return ErrCounterOverflow
	}
	r.Counters[name]++
	return nil
}
func (m *Manager) RecordServiceInvocation(fed core.FederationName, h core.FederateHandle, inv ServiceInvocation) error {
	var metric string
	switch inv.Service {
	case "registerObjectInstance", "registerObjectInstanceWithRegions":
		metric = "HLAobjectInstancesRegistered"
	case "deleteObjectInstance":
		metric = "HLAobjectInstancesDeleted"
	case "updateAttributeValues":
		if inv.Success {
			metric = "HLAupdatesSent"
		}
	case "sendInteraction", "sendInteractionWithRegions":
		if inv.Success {
			metric = "HLAinteractionsSent"
		}
	}
	if metric == "" {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	r, err := m.runtimeMetricsLocked(fed, h)
	if err != nil {
		return err
	}
	return incrementMetric(r, metric)
}
func (m *Manager) RecordCallbackInvocation(fed core.FederationName, h core.FederateHandle, inv CallbackInvocation) error {
	var metric string
	switch inv.Service {
	case serviceReflectAttributeValues:
		metric = metricReflectionsReceived
	case serviceReceiveInteraction:
		metric = metricInteractionsReceived
	case "discoverObjectInstance":
		metric = "HLAobjectInstancesDiscovered"
	case "removeObjectInstance":
		metric = "HLAobjectInstancesRemoved"
	}
	if metric == "" {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	r, err := m.runtimeMetricsLocked(fed, h)
	if err != nil {
		return err
	}
	if err = incrementMetric(r, metric); err != nil {
		return err
	}
	if inv.Service == serviceReflectAttributeValues {
		if inv.Object == 0 || inv.ObjectClass == 0 {
			r.Failures["HLAobjectInstancesReflected"] = "callback object identity unavailable"
			return fmt.Errorf("MOM: reflected identity unavailable")
		}
		r.ReflectedObjects[inv.Object] = inv.ObjectClass
		count := uint64(len(r.ReflectedObjects))
		if count > math.MaxInt32 {
			r.Failures["HLAobjectInstancesReflected"] = failureOverflow
			return ErrCounterOverflow
		}
		r.Counters["HLAobjectInstancesReflected"] = uint32(count)
	}
	return nil
}
func (m *Manager) CallbackPending(fed core.FederationName, h core.FederateHandle, delta int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callbackPendingLocked(fed, h, delta)
}

func (m *Manager) callbackPendingLocked(fed core.FederationName, h core.FederateHandle, delta int) {
	r, err := m.runtimeMetricsLocked(fed, h)
	if err != nil {
		return
	}
	if delta == 1 && r.PendingCallbacks < math.MaxUint64 {
		r.PendingCallbacks++
		return
	}
	if delta == -1 && r.PendingCallbacks > 0 {
		r.PendingCallbacks--
		return
	}
	for _, name := range callbackCounterNames {
		r.Failures[name] = "callback pending baseline invalid"
	}
}
func (m *Manager) CallbackUnavailable(fed core.FederationName, h core.FederateHandle, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, e := m.runtimeMetricsLocked(fed, h)
	if e != nil {
		return
	}
	why := "callback coverage unavailable"
	if err != nil {
		why = err.Error()
	}
	for _, name := range callbackCounterNames {
		r.Failures[name] = why
	}
}
func metricSnapshot(names string) MetricSnapshot {
	s := MetricSnapshot{Values: map[string][]byte{}, Unavailable: map[string]string{}}
	for _, name := range strings.Fields(names) {
		s.Unavailable[name] = "authoritative source unavailable"
	}
	return s
}
func (s *MetricSnapshot) put(name string, value []byte) {
	s.Values[name] = value
	delete(s.Unavailable, name)
}
func (s *MetricSnapshot) count(name string, value uint64) {
	if value > math.MaxInt32 {
		s.Unavailable[name] = failureOverflow
		delete(s.Values, name)
		return
	}
	s.put(name, encodeHLAcount(uint32(value)))
}
func logicalMetric(value LogicalTimeMetric) []byte {
	if value.ExactValue != nil && len(value.ExactValue) != 8 {
		return nil
	}
	if !value.Defined {
		return make([]byte, 4)
	}
	b := make([]byte, 12)
	binary.BigEndian.PutUint32(b, 8)
	if len(value.ExactValue) == 8 {
		copy(b[4:], value.ExactValue)
		return b
	}
	binary.BigEndian.PutUint64(b[4:], math.Float64bits(float64(value.Value)))
	return b
}

func (s *MetricSnapshot) putLogical(name string, value LogicalTimeMetric) error {
	if value.ExactValue != nil && len(value.ExactValue) != 8 {
		return fmt.Errorf("MOM: %s: %w", name, logicaltime.ErrInvalidEncoding)
	}
	s.put(name, logicalMetric(value))
	return nil
}
func boolMetric(value bool) []byte {
	if value {
		return encodeHLAcount(1)
	}
	return encodeHLAcount(0)
}
func (m *Manager) FederateMetricValues(ctx context.Context, fed core.FederationName, h core.FederateHandle) (MetricSnapshot, error) {
	m.mu.RLock()
	st := m.fed[fed]
	if st == nil || st.federates[h] == nil {
		m.mu.RUnlock()
		return MetricSnapshot{}, core.ErrFederateNotJoined
	}
	fs := st.federates[h]
	name, kind := fs.name, fs.federateType
	conveyRegions, conveyProducer := fs.conveyRegionDesignatorSetsSwitch, fs.conveyProducingFederateSwitch
	runtime := cloneRuntimeMetrics(fs.runtimeMetrics)
	dataEntryMetricsEnabled := m.dataEntryMetricsEnabled
	sources, modules, traffic := m.metricSources, m.moduleSource, m.traffic
	m.mu.RUnlock()
	result := metricSnapshot(federateMetricNames)
	result.put("HLAfederateHandle", encodeHLAhandle(h))
	result.put("HLAfederateName", encodeHLAunicodeString(name))
	result.put("HLAfederateType", encodeHLAunicodeString(kind))
	result.put("HLAconveyRegionDesignatorSets", boolMetric(conveyRegions))
	result.put("HLAconveyProducingFederate", boolMetric(conveyProducer))
	result.addRuntimeMetrics(runtime, dataEntryMetricsEnabled)
	if traffic != nil {
		counts, err := traffic.ObjectInstanceCounts(ctx, fed, h, ObjectsUpdated)
		if err == nil {
			var count uint64
			for _, n := range counts {
				count += uint64(n)
			}
			result.count("HLAobjectInstancesUpdated", count)
		} else {
			result.Unavailable["HLAobjectInstancesUpdated"] = err.Error()
		}
	}
	if modules != nil {
		if list, err := modules(ctx, fed, h); err == nil {
			var names []string
			for _, module := range list {
				names = append(names, module.Designator)
			}
			result.putModuleDesignators(names)
		} else {
			result.Unavailable["HLAFOMmoduleDesignatorList"] = err.Error()
		}
	}
	if sources.Federate != nil {
		actual, err := sources.Federate(ctx, fed, h)
		if err != nil {
			return result, err
		}
		result, err = applyFederateMetrics(result, actual)
		if err != nil {
			return result, err
		}
	}
	if sources.ElapsedTime != nil {
		elapsed, err := sources.ElapsedTime(ctx, fed, h)
		result.addElapsedMetrics(elapsed, err)
	}
	return result, nil
}

func (m *Manager) FederationMetricValues(ctx context.Context, fed core.FederationName) (MetricSnapshot, error) {
	m.mu.RLock()
	st := m.fed[fed]
	if st == nil {
		m.mu.RUnlock()
		return MetricSnapshot{}, core.ErrFederationNotFound
	}
	name := st.federation.name
	members := append([]core.FederateHandle(nil), st.federation.federateHandles...)
	auto := st.federation.autoProvideSwitch
	sources, modules := m.metricSources, m.moduleSource
	m.mu.RUnlock()
	result := metricSnapshot(federationMetricNames)
	result.put("HLAfederationName", encodeHLAunicodeString(string(name)))
	result.put("HLAfederatesInFederation", encodeFederateHandleList(members))
	result.put("HLAautoProvide", boolMetric(auto))
	if modules != nil {
		if list, err := modules(ctx, fed, 0); err == nil {
			var names []string
			for _, module := range list {
				names = append(names, module.Designator)
			}
			result.putModuleDesignators(names)
		} else {
			result.Unavailable["HLAFOMmoduleDesignatorList"] = err.Error()
		}
	}
	if sources.Federation != nil {
		actual, err := sources.Federation(ctx, fed)
		if err != nil {
			return result, err
		}
		for name, value := range map[string]*string{"HLARTIversion": actual.RTIVersion, "HLAMIMdesignator": actual.MIMDesignator, "HLAcurrentFDD": actual.CurrentFDD, "HLAtimeImplementationName": actual.TimeImplementationName, "HLAlastSaveName": actual.LastSaveName, "HLAAnnexSaveName": actual.NextSaveName} {
			if value != nil {
				result.put(name, encodeHLAunicodeString(*value))
			}
		}
		if actual.LastSaveTime != nil {
			if err := result.putLogical("HLAlastSaveTime", *actual.LastSaveTime); err != nil {
				return MetricSnapshot{}, err
			}
		}
		if actual.NextSaveTime != nil {
			if err := result.putLogical("HLAnextSaveTime", *actual.NextSaveTime); err != nil {
				return MetricSnapshot{}, err
			}
		}
	}
	return result, nil
}

func (r *RuntimeMetricState) validate() error {
	if r == nil {
		return nil
	}
	allowed := map[string]bool{}
	for _, name := range runtimeCounterNames {
		allowed[name] = true
	}
	for name, count := range r.Counters {
		if !allowed[name] || count > math.MaxInt32 {
			return fmt.Errorf("MOM: invalid runtime counter %q", name)
		}
	}
	for name, reason := range r.Failures {
		if !allowed[name] || reason == "" {
			return fmt.Errorf("MOM: invalid runtime failure %q", name)
		}
	}
	for obj, cls := range r.ReflectedObjects {
		if obj == 0 || cls == 0 {
			return fmt.Errorf("MOM: invalid reflected object history")
		}
	}
	if uint64(len(r.ReflectedObjects)) != uint64(r.Counters["HLAobjectInstancesReflected"]) {
		return fmt.Errorf("MOM: reflected object history count differs")
	}
	return nil
}

// RefreshMetrics publishes only values backed by a source. Callers can inspect
// the corresponding MetricSnapshot.Unavailable map for every omitted MIM field.
func (m *Manager) RefreshMetrics(ctx context.Context, fed core.FederationName, h core.FederateHandle) error {
	// Serialize the complete measurement/publication/ack transaction with
	// periodic updates and lifecycle replacement. Queries remain read-only.
	m.periodicMu.Lock()
	defer m.periodicMu.Unlock()
	m.mu.RLock()
	st := m.fed[fed]
	if st == nil {
		m.mu.RUnlock()
		return core.ErrFederationNotFound
	}
	obj, class := st.objects.federationObj, st.objects.federationCls
	if h != 0 {
		fs := st.federates[h]
		if fs == nil {
			m.mu.RUnlock()
			return core.ErrFederateNotJoined
		}
		obj, class = fs.objectHandle, st.objects.federateCls
	}
	m.mu.RUnlock()
	var snapshot MetricSnapshot
	var err error
	if h == 0 {
		snapshot, err = m.FederationMetricValues(ctx, fed)
	} else {
		snapshot, err = m.FederateMetricValues(ctx, fed, h)
	}
	if err != nil {
		return err
	}
	return m.publishMetricSnapshot(ctx, fed, obj, class, snapshot)
}

func (m *Manager) publishMetricSnapshot(ctx context.Context, fed core.FederationName, obj core.ObjectHandle, class core.ObjectClassHandle, snapshot MetricSnapshot) error {
	if err := m.publishInstanceValues(ctx, fed, obj, class, snapshot.Values); err != nil {
		return err
	}
	if snapshot.elapsedAcknowledgement == nil {
		return nil
	}
	_, granted := snapshot.Values["HLAtimeGrantedTime"]
	_, advancing := snapshot.Values["HLAtimeAdvancingTime"]
	if !granted && !advancing {
		return nil
	}
	return snapshot.elapsedAcknowledgement(ElapsedTimeAttributes{Granted: granted, Advancing: advancing})
}
func (s *MetricSnapshot) addRuntimeMetrics(runtime *RuntimeMetricState, dataEntryMetricsEnabled bool) {
	if runtime != nil {
		for _, name := range runtimeCounterNames {
			if reason := runtime.Failures[name]; reason != "" {
				s.Unavailable[name] = reason
				continue
			}
			isCallback := false
			for _, c := range callbackCounterNames {
				if name == c {
					isCallback = true
				}
			}
			countedAtEntry := dataEntryMetricsEnabled && (name == metricInteractionsReceived || name == metricReflectionsReceived || name == "HLAobjectInstancesReflected")
			if isCallback && !countedAtEntry && runtime.PendingCallbacks > 0 {
				s.Unavailable[name] = "callback receipts outstanding"
				continue
			}
			s.count(name, uint64(runtime.Counters[name]))
		}
	}
}

func applyFederateMetrics(result MetricSnapshot, actual FederateMetrics) (MetricSnapshot, error) {
	for name, value := range map[string]*string{"HLAfederateHost": actual.Host, "HLARTIversion": actual.RTIVersion} {
		if value != nil {
			result.put(name, encodeHLAunicodeString(*value))
		}
	}
	if actual.FederateState != nil {
		if *actual.FederateState == 1 || *actual.FederateState == 3 || *actual.FederateState == 5 {
			result.put("HLAfederateState", encodeHLAcount(*actual.FederateState))
		} else {
			return result, fmt.Errorf("MOM: invalid federate state")
		}
	}
	for name, value := range map[string]*uint64{"HLAROlength": actual.ROQueued, "HLAobjectInstancesThatCanBeDeleted": actual.DeletableObjects} {
		if value != nil {
			result.count(name, *value)
		}
	}
	if actual.Time != nil {
		if err := result.addTimeMetrics(actual.Time); err != nil {
			return MetricSnapshot{}, err
		}
	}
	return result, nil
}

func (s *MetricSnapshot) addTimeMetrics(t *TimeMetrics) error {
	s.put("HLAtimeRegulating", boolMetric(t.Regulating))
	s.put("HLAtimeConstrained", boolMetric(t.Constrained))
	s.put("HLAasynchronousDelivery", boolMetric(t.AsynchronousDelivery))
	s.put("HLAtimeManagerState", boolMetric(t.Advancing))
	current := LogicalTimeMetric{Defined: true, Value: t.LogicalTime}
	if t.LogicalTimeValue != nil {
		var err error
		current.ExactValue, err = t.LogicalTimeValue.MarshalBinary()
		if err != nil {
			return err
		}
	}
	for _, field := range []struct {
		name  string
		value LogicalTimeMetric
	}{{"HLAlogicalTime", current}, {"HLAlookahead", t.Lookahead}, {"HLAGALT", t.GALT}, {"HLALITS", t.LITS}} {
		if err := s.putLogical(field.name, field.value); err != nil {
			return err
		}
	}
	if t.TSOQueued != nil {
		s.count("HLATSOlength", *t.TSOQueued)
	}
	for name, value := range map[string]*uint64{"HLAtimeGrantedTime": t.GrantedMilliseconds, "HLAtimeAdvancingTime": t.AdvancingMilliseconds} {
		if value != nil {
			s.count(name, *value)
		}
	}
	return nil
}

func (s *MetricSnapshot) addElapsedMetrics(elapsed ElapsedTimeMetrics, err error) {
	if err == nil && elapsed.Acknowledge == nil {
		err = fmt.Errorf("MOM: elapsed publication acknowledgement unavailable")
	}
	for name, value := range map[string]time.Duration{
		"HLAtimeGrantedTime": elapsed.Granted, "HLAtimeAdvancingTime": elapsed.Advancing,
	} {
		delete(s.Values, name)
		milliseconds, nonnegative := elapsedMilliseconds(value)
		switch {
		case err != nil:
			s.Unavailable[name] = err.Error()
		case !nonnegative:
			s.Unavailable[name] = "negative elapsed duration"
		default:
			s.count(name, milliseconds)
		}
	}
	if err == nil {
		s.elapsedAcknowledgement = elapsed.Acknowledge
	}
}

func elapsedMilliseconds(value time.Duration) (uint64, bool) {
	nanoseconds := int64(value)
	if nanoseconds < 0 {
		return 0, false
	}
	return uint64(nanoseconds) / uint64(time.Millisecond), true
}
func (s *MetricSnapshot) putModuleDesignators(names []string) {
	encoded, err := encodeStringList(names)
	if err != nil {
		s.Unavailable["HLAFOMmoduleDesignatorList"] = err.Error()
		return
	}
	s.put("HLAFOMmoduleDesignatorList", encoded)
}
