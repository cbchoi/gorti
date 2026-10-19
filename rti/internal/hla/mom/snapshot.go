package mom

import (
	"fmt"
	"math"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/pkg/logicaltime"
)

type ManagerState struct {
	Federation FederationState
	Federates  []FederateState
	Objects    ObjectInstanceState
	Traffic    *TrafficState
}

type FederationState struct {
	Name              core.FederationName
	FederateHandles   []core.FederateHandle
	FOMModuleNames    []string
	AutoProvideSwitch bool
}

// ObjectInstanceState preserves resolved MOM handles, not the registry or FOM
// dependencies. The object registry must be restored from the same checkpoint.
type ObjectInstanceState struct {
	Enabled                 bool
	FederationClass         core.ObjectClassHandle
	FederateClass           core.ObjectClassHandle
	AttributeFederationName core.AttributeHandle
	AttributeFederates      core.AttributeHandle
	AttributeFederateHandle core.AttributeHandle
	AttributeFederateName   core.AttributeHandle
	AttributeFederateType   core.AttributeHandle
	FederationObject        core.ObjectHandle
	FederationObjectName    string
}

type FederateState struct {
	Handle                           core.FederateHandle
	Name                             string
	FederateType                     string
	TimeRegulating                   bool
	TimeConstrained                  bool
	Lookahead                        core.LogicalTime
	LogicalTime                      core.LogicalTime
	LookaheadValue                   *logicaltime.Interval
	LogicalTimeValue                 *logicaltime.Time
	InteractionsSent                 uint32
	InteractionsReceived             uint32
	UpdatesSent                      uint32
	ReflectionsReceived              uint32
	ObjectHandle                     core.ObjectHandle
	ObjectName                       string
	ConveyRegionDesignatorSetsSwitch bool
	ConveyProducingFederateSwitch    bool
	ServiceReportingEnabled          bool
	ExceptionReportingEnabled        bool
	ServiceReports                   *ServiceReportState
	ReportPeriod                     int32
	RuntimeMetrics                   *RuntimeMetricState
}

func (m *Manager) ExportState(fed core.FederationName) *ManagerState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	st, ok := m.fed[fed]
	if !ok {
		return nil
	}
	out := &ManagerState{Federation: FederationState{
		Name:              st.federation.name,
		FederateHandles:   append([]core.FederateHandle(nil), st.federation.federateHandles...),
		FOMModuleNames:    append([]string(nil), st.federation.fomModuleNames...),
		AutoProvideSwitch: st.federation.autoProvideSwitch,
	}}
	out.Objects = ObjectInstanceState{
		Enabled:                 st.objects.ok,
		FederationClass:         st.objects.federationCls,
		FederateClass:           st.objects.federateCls,
		AttributeFederationName: st.objects.attrFederationName,
		AttributeFederates:      st.objects.attrFederatesInFederation,
		AttributeFederateHandle: st.objects.attrFederateHandle,
		AttributeFederateName:   st.objects.attrFederateName,
		AttributeFederateType:   st.objects.attrFederateType,
		FederationObject:        st.objects.federationObj,
		FederationObjectName:    st.objects.federationObjName,
	}
	if m.traffic != nil {
		out.Traffic = m.traffic.ExportState(fed)
	}
	slices.Sort(out.Federation.FederateHandles)
	handles := make([]core.FederateHandle, 0, len(st.federates))
	for handle := range st.federates {
		handles = append(handles, handle)
	}
	slices.Sort(handles)
	for _, handle := range handles {
		entry := st.federates[handle]
		out.Federates = append(out.Federates, FederateState{
			Handle: entry.handle, Name: entry.name, FederateType: entry.federateType,
			TimeRegulating: entry.timeRegulating, TimeConstrained: entry.timeConstrained,
			Lookahead: entry.lookahead, LogicalTime: entry.logicalTime,
			LookaheadValue: cloneIntervalValue(entry.lookaheadValue), LogicalTimeValue: cloneTimeValue(entry.logicalTimeValue),
			InteractionsSent: atomic.LoadUint32(&entry.interactionsSent), InteractionsReceived: atomic.LoadUint32(&entry.interactionsReceived),
			UpdatesSent: atomic.LoadUint32(&entry.updatesSent), ReflectionsReceived: atomic.LoadUint32(&entry.reflectionsReceived),
			ObjectHandle: entry.objectHandle, ObjectName: entry.objectName,
			ConveyRegionDesignatorSetsSwitch: entry.conveyRegionDesignatorSetsSwitch,
			ConveyProducingFederateSwitch:    entry.conveyProducingFederateSwitch,
			ServiceReportingEnabled:          entry.serviceReportingEnabled,
			ExceptionReportingEnabled:        entry.exceptionReportingEnabled,
			ReportPeriod:                     entry.reportPeriod,
			RuntimeMetrics:                   cloneRuntimeMetrics(entry.runtimeMetrics),
		})
		if entry.serviceSerialKnown {
			out.Federates[len(out.Federates)-1].ServiceReports = &ServiceReportState{NextSerial: entry.serviceSerial}
		}
	}
	return out
}

// ImportState retains the historical void API for already-validated callers.
// New persistence paths use PrepareImportState to surface validation failures.
func (m *Manager) ImportState(fed core.FederationName, state *ManagerState) {
	commit, err := m.PrepareImportState(fed, state)
	if err == nil {
		commit()
	}
}

// PrepareImportState validates and detaches both MOM and traffic state before
// mutation. The commit holds the MOM lock across both replacements. Ordinary
// cross-service operations must still use the production checkpoint admission.
func (m *Manager) PrepareImportState(fed core.FederationName, state *ManagerState) (func(), error) {
	if err := state.Validate(fed); err != nil {
		return nil, err
	}
	m.mu.RLock()
	traffic := m.traffic
	m.mu.RUnlock()
	if state != nil && !m.fanoutEnabled() {
		for _, f := range state.Federates {
			if f.ReportPeriod > 0 {
				return nil, fmt.Errorf("mom: periodic restore requires object publication dependencies")
			}
		}
	}
	trafficCommit, err := prepareMOMTrafficRestore(fed, state, traffic)
	if err != nil {
		return nil, err
	}
	var once sync.Once
	if state == nil {
		return func() {
			once.Do(func() {
				m.periodicMu.Lock()
				defer m.periodicMu.Unlock()
				m.mu.Lock()
				defer m.mu.Unlock()
				stopStatePeriods(m.fed[fed])
				delete(m.fed, fed)
				if trafficCommit != nil {
					trafficCommit()
				}
			})
		}, nil
	}
	st := restoreMOMState(state)
	return func() {
		once.Do(func() {
			m.periodicMu.Lock()
			defer m.periodicMu.Unlock()
			m.mu.Lock()
			defer m.mu.Unlock()
			stopStatePeriods(m.fed[fed])
			m.fed[fed] = st
			if trafficCommit != nil {
				trafficCommit()
			}
			for _, fs := range st.federates {
				m.scheduleFederateLocked(fed, fs)
			}
		})
	}, nil
}

// Validate checks internal consistency without resolving external services or
// mutating live state. Positive-infinite logical time is a valid final time.
func (state *ManagerState) Validate(fed core.FederationName) error {
	if state == nil {
		return nil
	}
	if fed == "" || state.Federation.Name != fed {
		return fmt.Errorf("MOM federation name %q does not match %q", state.Federation.Name, fed)
	}
	roster, err := validateMOMRoster(state.Federation.FederateHandles)
	if err != nil {
		return err
	}
	h := state.Objects
	if err := validateMOMObjectCache(h); err != nil {
		return err
	}
	objects := make(map[core.ObjectHandle]struct{}, len(state.Federates)+1)
	names := make(map[string]struct{}, len(state.Federates)+1)
	if h.FederationObject != 0 {
		objects[h.FederationObject] = struct{}{}
		names[h.FederationObjectName] = struct{}{}
	}
	for _, entry := range state.Federates {
		if err := entry.RuntimeMetrics.validate(); err != nil {
			return err
		}
		seen, exists := roster[entry.Handle]
		if !exists || seen {
			return fmt.Errorf("MOM federate %d is duplicated or absent from roster", entry.Handle)
		}
		roster[entry.Handle] = true
		if err := validateMOMFederateValues(entry, h); err != nil {
			return err
		}
		if err := validateMOMFederateObject(entry, h, objects, names); err != nil {
			return err
		}
	}
	if len(state.Federates) != len(roster) {
		return fmt.Errorf("MOM roster and federate records differ")
	}
	return state.validateTrafficRoster(fed, roster)
}
func prepareMOMTrafficRestore(fed core.FederationName, state *ManagerState, traffic *TrafficAccumulator) (func(), error) {
	var trafficCommit func()
	if traffic != nil {
		var history *TrafficState
		if state != nil {
			history = state.Traffic
			if history == nil {
				// Legacy snapshots lack a detailed baseline. Preserve their
				// aggregate values without inventing class/transport history.
				history = &TrafficState{Version: 1, Federation: fed}
				for _, entry := range state.Federates {
					f := TrafficFederateState{Handle: entry.Handle}
					for _, kind := range []CounterKind{InteractionsSent, InteractionsReceived, UpdatesSent, ReflectionsReceived} {
						f.Failures = append(f.Failures, TrafficFailureState{Kind: kind, Reason: "unavailable"})
					}
					history.Federates = append(history.Federates, f)
				}
			}
		}
		var err error
		trafficCommit, err = traffic.PrepareRestore(fed, history)
		if err != nil {
			return nil, err
		}
	} else if state != nil && state.Traffic != nil {
		return nil, fmt.Errorf("mom: traffic snapshot requires an accumulator")
	}
	return trafficCommit, nil
}

func restoreMOMState(state *ManagerState) *momState {
	st := newMOMState()
	st.federation.name = state.Federation.Name
	st.federation.federateHandles = append([]core.FederateHandle(nil), state.Federation.FederateHandles...)
	slices.Sort(st.federation.federateHandles)
	st.federation.fomModuleNames = append([]string(nil), state.Federation.FOMModuleNames...)
	st.federation.autoProvideSwitch = state.Federation.AutoProvideSwitch
	st.objects = momClassHandles{
		ok:                        state.Objects.Enabled,
		federationCls:             state.Objects.FederationClass,
		federateCls:               state.Objects.FederateClass,
		attrFederationName:        state.Objects.AttributeFederationName,
		attrFederatesInFederation: state.Objects.AttributeFederates,
		attrFederateHandle:        state.Objects.AttributeFederateHandle,
		attrFederateName:          state.Objects.AttributeFederateName,
		attrFederateType:          state.Objects.AttributeFederateType,
		federationObj:             state.Objects.FederationObject,
		federationObjName:         state.Objects.FederationObjectName,
	}
	for _, entry := range state.Federates {
		fs := &federateSnapshot{handle: entry.Handle, name: entry.Name, federateType: entry.FederateType, timeRegulating: entry.TimeRegulating, timeConstrained: entry.TimeConstrained, lookahead: entry.Lookahead, logicalTime: entry.LogicalTime}
		fs.lookaheadValue = cloneIntervalValue(entry.LookaheadValue)
		fs.logicalTimeValue = cloneTimeValue(entry.LogicalTimeValue)
		fs.objectHandle = entry.ObjectHandle
		fs.objectName = entry.ObjectName
		fs.conveyRegionDesignatorSetsSwitch = entry.ConveyRegionDesignatorSetsSwitch
		fs.conveyProducingFederateSwitch = entry.ConveyProducingFederateSwitch
		fs.serviceReportingEnabled = entry.ServiceReportingEnabled
		fs.exceptionReportingEnabled = entry.ExceptionReportingEnabled
		fs.reportPeriod = entry.ReportPeriod
		fs.runtimeMetrics = cloneRuntimeMetrics(entry.RuntimeMetrics)
		if fs.runtimeMetrics != nil && fs.runtimeMetrics.PendingCallbacks > 0 {
			for _, name := range callbackCounterNames {
				fs.runtimeMetrics.Failures[name] = "restored outstanding callback receipts unavailable"
			}
			fs.runtimeMetrics.PendingCallbacks = 0
		}
		if entry.ServiceReports != nil {
			fs.serviceSerialKnown = true
			fs.serviceSerial = entry.ServiceReports.NextSerial
		}
		atomic.StoreUint32(&fs.interactionsSent, entry.InteractionsSent)
		atomic.StoreUint32(&fs.interactionsReceived, entry.InteractionsReceived)
		atomic.StoreUint32(&fs.updatesSent, entry.UpdatesSent)
		atomic.StoreUint32(&fs.reflectionsReceived, entry.ReflectionsReceived)
		st.federates[fs.handle] = fs
	}
	return st
}

func validateMOMObjectCache(h ObjectInstanceState) error {
	if !h.Enabled && h != (ObjectInstanceState{}) {
		return fmt.Errorf("disabled MOM object cache contains handles")
	}
	if h.Enabled && (h.FederationClass == 0 || h.FederateClass == 0 || h.FederationClass == h.FederateClass || h.FederationObject == 0 || h.FederationObjectName == "") {
		return fmt.Errorf("incomplete MOM object cache")
	}
	return nil
}

func validateMOMFederateValues(entry FederateState, h ObjectInstanceState) error {
	if entry.ReportPeriod < 0 || (entry.ReportPeriod > 0 && (!h.Enabled || entry.ObjectHandle == 0)) {
		return fmt.Errorf("MOM invalid periodic report state")
	}
	if entry.ServiceReports != nil && entry.ServiceReports.NextSerial > math.MaxInt32+1 {
		return fmt.Errorf("MOM invalid service report serial")
	}
	if !entry.LogicalTime.IsValid() || !entry.Lookahead.IsValid() {
		return fmt.Errorf("MOM federate %d has an invalid time value", entry.Handle)
	}
	if err := validateSnapshotTimeValues(entry.LogicalTimeValue, entry.LookaheadValue, entry.LogicalTime, entry.Lookahead); err != nil {
		return fmt.Errorf("MOM federate %d: %w", entry.Handle, err)
	}
	return nil
}

func validateMOMFederateObject(entry FederateState, h ObjectInstanceState, objects map[core.ObjectHandle]struct{}, names map[string]struct{}) error {
	if (entry.ObjectHandle == 0) != (entry.ObjectName == "") || (!h.Enabled && entry.ObjectHandle != 0) {
		return fmt.Errorf("MOM federate %d has inconsistent object identity", entry.Handle)
	}
	if entry.ObjectHandle != 0 {
		_, duplicateHandle := objects[entry.ObjectHandle]
		_, duplicateName := names[entry.ObjectName]
		if duplicateHandle || duplicateName {
			return fmt.Errorf("MOM federate %d has a duplicate object identity", entry.Handle)
		}
		objects[entry.ObjectHandle] = struct{}{}
		names[entry.ObjectName] = struct{}{}
	}
	return nil
}

func (state *ManagerState) validateTrafficRoster(fed core.FederationName, roster map[core.FederateHandle]bool) error {
	if state.Traffic != nil {
		if err := state.Traffic.Validate(fed); err != nil {
			return err
		}
		if len(state.Traffic.Federates) != len(roster) {
			return fmt.Errorf("MOM traffic roster differs")
		}
		for _, entry := range state.Traffic.Federates {
			if _, ok := roster[entry.Handle]; !ok {
				return fmt.Errorf("MOM traffic federate absent from roster")
			}
		}
	}
	return nil
}
func validateMOMRoster(handles []core.FederateHandle) (map[core.FederateHandle]bool, error) {
	roster := make(map[core.FederateHandle]bool, len(handles))
	for _, h := range handles {
		if _, exists := roster[h]; h == core.InvalidFederateHandle || exists {
			return nil, fmt.Errorf("invalid or duplicate MOM roster handle %d", h)
		}
		roster[h] = false
	}
	return roster, nil
}
