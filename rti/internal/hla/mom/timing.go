package mom

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

const ClassSetTiming = "HLAmanager.HLAfederate.HLAadjust.HLAsetTiming"

// SetBackgroundAdmission fences periodic publications with the application's
// save/restore service admission. Configure before enabling periodic updates.
func (m *Manager) SetBackgroundAdmission(acquire func(context.Context, core.FederationName) (func(), error)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.backgroundAdmission = acquire
}

func handleSetTiming(_ context.Context, dctx DispatchContext, _ core.FederateHandle, params map[core.ParameterHandle][]byte) ([]ResponseInteraction, error) {
	target, err := catalogTarget(dctx, ClassSetTiming, params, "HLAreportPeriod")
	if err != nil {
		return nil, err
	}
	seconds, err := parameterInteger(dctx, ClassSetTiming, "HLAreportPeriod", params)
	if err != nil {
		return nil, err
	}
	if seconds < 0 {
		return nil, malformed("HLAreportPeriod: negative seconds")
	}
	m := dctx.MOM
	m.periodicMu.Lock()
	defer m.periodicMu.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.fed[dctx.Federation]
	if st == nil || st.federates[target] == nil {
		return nil, core.ErrFederateNotJoined
	}
	fs := st.federates[target]
	if seconds > 0 && (!m.fanoutEnabled() || !st.objects.ok || fs.objectHandle == 0) {
		return nil, fmt.Errorf("mom: periodic object publication unavailable")
	}
	stopFederatePeriod(fs)
	fs.reportPeriod = seconds
	fs.periodicError = nil
	m.scheduleFederateLocked(dctx.Federation, fs)
	return nil, nil
}

func stopFederatePeriod(fs *federateSnapshot) {
	fs.periodGeneration++
	if fs.stopPeriodic != nil {
		fs.stopPeriodic()
		fs.stopPeriodic = nil
	}
}

func stopStatePeriods(st *momState) {
	if st != nil {
		for _, fs := range st.federates {
			stopFederatePeriod(fs)
		}
	}
}

func (m *Manager) scheduleFederateLocked(fed core.FederationName, fs *federateSnapshot) {
	if fs.reportPeriod == 0 {
		return
	}
	generation := fs.periodGeneration
	fs.stopPeriodic = m.opts.SchedulePeriodic(time.Duration(fs.reportPeriod)*time.Second, func() { m.periodicTick(fed, fs, generation) })
}

func (m *Manager) periodicTick(fed core.FederationName, fs *federateSnapshot, generation uint64) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	m.mu.RLock()
	admission := m.backgroundAdmission
	m.mu.RUnlock()
	if admission != nil {
		release, err := admission(ctx, fed)
		if err == nil && release == nil {
			err = fmt.Errorf("mom: background admission returned no release")
		}
		if err != nil {
			m.retryPeriodicAdmission(fed, fs, generation, err)
			return
		}
		defer release()
	}
	m.periodicMu.Lock()
	defer m.periodicMu.Unlock()
	m.mu.RLock()
	st := m.fed[fed]
	if st == nil || st.federates[fs.handle] != fs || fs.periodGeneration != generation || fs.reportPeriod == 0 {
		m.mu.RUnlock()
		return
	}
	attrs := fs.snapshot()
	traffic := m.traffic
	runtimeMetricsEnabled := m.runtimeMetricsEnabled
	obj, class := fs.objectHandle, st.objects.federateCls
	m.mu.RUnlock()
	values := map[string][]byte{}
	var metrics MetricSnapshot
	var err error
	if runtimeMetricsEnabled {
		metrics, err = m.FederateMetricValues(ctx, fed, fs.handle)
		if err == nil {
			values = metrics.Values
		}
	} else {
		values, err = periodicCounterValues(ctx, fed, fs.handle, attrs, traffic)
	}
	if err == nil {
		if runtimeMetricsEnabled {
			err = m.publishMetricSnapshot(ctx, fed, obj, class, metrics)
		} else {
			err = m.publishInstanceValues(ctx, fed, obj, class, values)
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	fs.periodicError = err
	m.scheduleFederateLocked(fed, fs)
}

// PeriodicStatus exposes asynchronous publication errors rather than hiding a
// failed timer behind successful HLAsetTiming acknowledgement.
func (m *Manager) PeriodicStatus(fed core.FederationName, h core.FederateHandle) (int32, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	st := m.fed[fed]
	if st == nil || st.federates[h] == nil {
		return 0, core.ErrFederateNotJoined
	}
	return st.federates[h].reportPeriod, st.federates[h].periodicError
}

func (m *Manager) publishInstanceValues(ctx context.Context, fed core.FederationName, obj core.ObjectHandle, class core.ObjectClassHandle, values map[string][]byte) error {
	if !m.fanoutEnabled() || obj == 0 || class == 0 {
		return fmt.Errorf("mom: object publication unavailable")
	}
	fom, err := m.fanout.foms.Get(ctx, fed)
	if err != nil {
		return err
	}
	attrs := map[core.AttributeHandle][]byte{}
	var handles []core.AttributeHandle
	for name, value := range values {
		attr, ok := fom.LookupAttribute(class, name)
		if !ok {
			return fmt.Errorf("mom: attribute %s unavailable", name)
		}
		attrs[attr] = value
		handles = append(handles, attr)
	}
	if err := m.prepareInternalProducer(ctx, fed); err != nil {
		return err
	}
	if err := m.fanout.decls.PublishObjectClassAttributes(ctx, fed, momProducer, class, handles); err != nil {
		return err
	}
	return m.publishInstanceAttributes(ctx, fed, obj, class, attrs)
}

// The RTI's internal producer has no federate callback stream. Disable its
// publisher advisories before declaration changes; real subscribers retain
// their own switches and still receive normal MOM updates.
func (m *Manager) prepareInternalProducer(ctx context.Context, fed core.FederationName) error {
	if switches, ok := m.fanout.decls.(core.AdvisorySwitchManagement); ok {
		for _, kind := range []core.AdvisorySwitchKind{core.AdvisoryObjectClassRelevance, core.AdvisoryAttributeRelevance, core.AdvisoryAttributeScope, core.AdvisoryInteractionRelevance} {
			if switches.AdvisorySwitchEnabled(fed, momProducer, kind) {
				if err := switches.SetAdvisorySwitch(ctx, fed, momProducer, kind, false); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// RefreshFOMModules publishes the authoritative designator list. Target zero
// selects HLAfederation; a nonzero target selects its HLAfederate instance.
func (m *Manager) RefreshFOMModules(ctx context.Context, fed core.FederationName, target core.FederateHandle) error {
	m.mu.RLock()
	source := m.moduleSource
	st := m.fed[fed]
	if st == nil {
		m.mu.RUnlock()
		return core.ErrFederationNotFound
	}
	obj, class := st.objects.federationObj, st.objects.federationCls
	if target != 0 {
		fs := st.federates[target]
		if fs == nil {
			m.mu.RUnlock()
			return core.ErrFederateNotJoined
		}
		obj, class = fs.objectHandle, st.objects.federateCls
	}
	m.mu.RUnlock()
	if source == nil {
		return fmt.Errorf("mom: FOM module source unavailable")
	}
	modules, err := source(ctx, fed, target)
	if err != nil {
		return err
	}
	if err := validateModules(modules); err != nil {
		return err
	}
	names := make([]string, len(modules))
	for i, module := range modules {
		names[i] = module.Designator
	}
	encoded, err := encodeStringList(names)
	if err != nil {
		return err
	}
	if err := m.publishInstanceValues(ctx, fed, obj, class, map[string][]byte{"HLAFOMmoduleDesignatorList": encoded}); err != nil {
		return err
	}
	if target == 0 {
		m.mu.Lock()
		if current := m.fed[fed]; current == st {
			st.federation.fomModuleNames = names
		}
		m.mu.Unlock()
	}
	return nil
}
func periodicCounterValues(ctx context.Context, fed core.FederationName, h core.FederateHandle, attrs FederateAttributes, traffic *TrafficAccumulator) (map[string][]byte, error) {
	values := map[string][]byte{}
	var err error
	// Only actual maintained periodic attributes are sent. Time, queue and
	// other metrics require authoritative providers; absence is not zero.
	for _, counter := range []struct {
		name  string
		kind  CounterKind
		count uint32
	}{
		{"HLAupdatesSent", UpdatesSent, attrs.UpdatesSent}, {metricReflectionsReceived, ReflectionsReceived, attrs.ReflectionsReceived},
		{"HLAinteractionsSent", InteractionsSent, attrs.InteractionsSent}, {metricInteractionsReceived, InteractionsReceived, attrs.InteractionsReceived},
	} {
		count := counter.count
		if traffic != nil {
			var totals []TransportationCounts
			totals, err = traffic.Snapshot(ctx, fed, h, counter.kind)
			if err != nil {
				break
			}
			count, err = totalTransportationCounts(totals)
			if err != nil {
				break
			}
		}
		if count > math.MaxInt32 {
			err = ErrCounterOverflow
			break
		}
		values[counter.name] = encodeHLAcount(count)
	}

	return values, err
}

func totalTransportationCounts(totals []TransportationCounts) (uint32, error) {
	var total uint64
	for _, transport := range totals {
		for _, n := range transport.Counts {
			total += uint64(n)
			if total > math.MaxInt32 {
				return 0, ErrCounterOverflow
			}
		}
	}
	if total > math.MaxInt32 {
		return 0, ErrCounterOverflow
	}
	return uint32(total), nil
}

func (m *Manager) retryPeriodicAdmission(fed core.FederationName, fs *federateSnapshot, generation uint64, err error) {
	// No publication or periodic mutex is entered while checkpoint
	// admission is busy. Re-arm only if this is still the live timer.
	m.mu.Lock()
	defer m.mu.Unlock()
	if st := m.fed[fed]; st != nil && st.federates[fs.handle] == fs && fs.periodGeneration == generation && fs.reportPeriod > 0 {
		fs.periodicError = err
		m.scheduleFederateLocked(fed, fs)
	}
}
