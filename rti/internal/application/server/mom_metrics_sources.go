package server

import (
	"context"
	"errors"
	"fmt"
	"slices"

	grpcadapter "github.com/cbchoi/gorti/rti/internal/adapters/grpc"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
	savepoint "github.com/cbchoi/gorti/rti/internal/hla/save_restore"
	timedomain "github.com/cbchoi/gorti/rti/internal/hla/time"
)

type momMetricDependencies struct {
	CurrentFDD                         func(context.Context, core.FederationName) ([]byte, error)
	MIMDesignator                      func(context.Context, core.FederationName) (string, error)
	Catalog                            momCatalogDependencies
	Time                               *timedomain.Manager
	Save                               *savepoint.Manager
	IntegerTime                        *timedomain.ManagerOf[int64]
	IntegerSave                        *savepoint.ManagerOf[int64]
	RTIVersion, TimeImplementationName string
	TimeImplementation                 func(core.FederationName) (string, bool)
	// ROQueue is a custom waiting-RO source for configurations without Time.
	ROQueue    func(context.Context, core.FederationName, core.FederateHandle) (uint64, error)
	DataQueues func(context.Context, core.FederationName, core.FederateHandle) (core.DataQueueCounts, error)
	Host       func(core.FederationName, core.FederateHandle) (string, bool, error)
}

func configureMOMMetrics(manager *mom.Manager, deps momMetricDependencies) error {
	if manager == nil {
		return fmt.Errorf("MOM: metric manager unavailable")
	}
	catalog, err := newMOMCatalogSources(deps.Catalog)
	if err != nil {
		return err
	}
	sources := momMetricSources{momMetricDependencies: deps, catalog: catalog}
	manager.SetMetricSources(mom.MetricSources{
		ElapsedTime: sources.elapsedTime,
		Federate:    sources.federate,
		Federation:  sources.federation,
	})
	return nil
}

func toMOMLogicalTime(value *core.LogicalTime) mom.LogicalTimeMetric {
	if value == nil {
		return mom.LogicalTimeMetric{}
	}
	return mom.LogicalTimeMetric{Defined: true, Value: *value}
}

// The subscription caller retains checkpoint and membership admission. Taking
// another membership lease here could deadlock behind a waiting lifecycle writer.
func refreshMOMClassMetrics(ctx context.Context, fed core.FederationName, class core.ObjectClassHandle, manager *mom.Manager, foms core.FOMRepository, members func(core.FederationName) []core.FederateHandle) error {
	fom, err := foms.Get(ctx, fed)
	if err != nil {
		return err
	}
	var targets []core.FederateHandle
	if handle, ok := fom.LookupObjectClass(mom.ClassHLAfederation); ok && handle == class {
		targets = []core.FederateHandle{0}
	} else if handle, ok := fom.LookupObjectClass(mom.ClassHLAfederate); ok && handle == class {
		targets = members(fed)
	}
	var result error
	for _, target := range targets {
		result = errors.Join(result, manager.RefreshFOMModules(ctx, fed, target))
		result = errors.Join(result, manager.RefreshMetrics(ctx, fed, target))
	}
	return result
}

type momObservationDependencies struct {
	Manager                     *mom.Manager
	Dispatcher                  *mom.Dispatcher
	FOMs                        core.FOMRepository
	Authenticate                func(context.Context, core.FederationName, core.FederateHandle) error
	AcquireService              func(context.Context, core.FederationName) (func(), error)
	ResolveCaller               func(context.Context) (core.FederationName, core.FederateHandle, func(), error)
	OnError                     func(error)
	CallbackEntry               func(context.Context, core.FederationName, core.FederateHandle, *rtiv1.FederateEvent) error
	DataCallbacksCountedAtEntry bool
	LocalDataCallbackEntry      bool
	DataCallbackReports         *grpcadapter.DataCallbackReportBatch
	CallbackInvoked             func(context.Context, core.FederationName, core.FederateHandle, *rtiv1.FederateEvent, bool, string) error
	ValidateRequiredRecipient   func(context.Context, core.FederationName, core.FederateHandle) error
}

func newMOMRuntimeObservers(deps momObservationDependencies) (*grpcadapter.MOMObserver, *grpcadapter.CallbackReceipts, error) {
	if deps.DataCallbacksCountedAtEntry && deps.CallbackEntry == nil {
		return nil, nil, fmt.Errorf("MOM: entry accounting requires a callback entry observer")
	}
	if deps.Manager != nil && (deps.Dispatcher == nil || deps.FOMs == nil) {
		return nil, nil, fmt.Errorf("MOM: observation dependencies incomplete")
	}
	if deps.Manager != nil {
		if err := deps.Manager.SetDataEntryMetricsEnabled(deps.DataCallbacksCountedAtEntry); err != nil {
			return nil, nil, err
		}
		if err := deps.Manager.SetRuntimeMetricsEnabled(true); err != nil {
			return nil, nil, err
		}
	}
	resolveCaller := deps.observationCaller()
	acquireService := deps.observationAdmission()
	var capture func(core.FederationName, core.FederateHandle) bool
	var pending func(core.FederationName, core.FederateHandle, int)
	var unavailable func(core.FederationName, core.FederateHandle, error)
	if deps.Manager != nil {
		capture = deps.Manager.ReportingEnabled
		pending = deps.Manager.CallbackPending
	}
	unavailable = func(fed core.FederationName, h core.FederateHandle, cause error) {
		if deps.Manager != nil {
			deps.Manager.CallbackUnavailable(fed, h, cause)
		}
		if deps.OnError != nil {
			deps.OnError(cause)
		}
	}
	observer := grpcadapter.NewMOMObserver(grpcadapter.MOMObserverOptions{CaptureArguments: capture, ResolveCaller: resolveCaller, OnError: deps.OnError, Observe: deps.observeService})
	receipts, err := grpcadapter.NewCallbackReceipts(grpcadapter.CallbackReceiptOptions{ControlReserve: 64, Authenticate: deps.Authenticate, AcquireService: acquireService, ObserveEntry: deps.CallbackEntry, ObserveRequired: deps.CallbackInvoked, ValidateRequiredRecipient: deps.ValidateRequiredRecipient, Pending: pending, OnUnavailable: unavailable, LocalDataEntry: deps.LocalDataCallbackEntry, Observe: deps.observeCallback, DataReports: deps.DataCallbackReports})
	return observer, receipts, err
}

func momReportClass(fom core.FOMHandle, class core.InteractionClassHandle) bool {
	classes := []core.InteractionClassHandle{class}
	if ancestry, ok := fom.(core.FOMClassAncestry); ok {
		classes = ancestry.InteractionClassAncestors(class)
	}
	for _, name := range []string{"HLAmanager.HLAfederate.HLAreport", "HLAmanager.HLAfederation.HLAreport"} {
		if report, ok := fom.LookupInteractionClass(name); ok && slices.Contains(classes, report) {
			return true
		}
	}
	return false
}

type momMetricSources struct {
	momMetricDependencies
	catalog mom.CatalogSources
}

func (deps momMetricSources) isInteger(fed core.FederationName) bool {
	if deps.TimeImplementation == nil {
		return false
	}
	name, exists := deps.TimeImplementation(fed)
	return exists && name == core.ScalarRepresentation[int64]().String()
}

func (deps momMetricSources) validate(fed core.FederationName, h core.FederateHandle) error {
	if _, ok := deps.Catalog.Federations.GenerationFor(fed); !ok {
		return core.ErrFederationNotFound
	}
	if h != 0 && !slices.Contains(deps.Catalog.Federations.MembersOf(fed), h) {
		return core.ErrFederateNotJoined
	}
	return nil
}

func (deps momMetricSources) elapsedTime(_ context.Context, fed core.FederationName, h core.FederateHandle) (mom.ElapsedTimeMetrics, error) {
	if err := deps.validate(fed, h); err != nil {
		return mom.ElapsedTimeMetrics{}, err
	}
	if deps.Time == nil {
		return mom.ElapsedTimeMetrics{}, fmt.Errorf("MOM: elapsed-time source unavailable")
	}
	timeSource := elapsedMetricSource(deps.Time)
	if deps.isInteger(fed) {
		timeSource = deps.IntegerTime
	}
	measurement, err := timeSource.MeasureElapsed(fed, h)
	if err != nil {
		return mom.ElapsedTimeMetrics{}, err
	}
	return mom.ElapsedTimeMetrics{
		Granted: measurement.Granted, Advancing: measurement.Advancing,
		Acknowledge: func(attributes mom.ElapsedTimeAttributes) error {
			return timeSource.AcknowledgeElapsed(fed, h, measurement, timedomain.ElapsedAttributes{Granted: attributes.Granted, Advancing: attributes.Advancing})
		},
	}, nil
}

func (deps momMetricSources) federate(ctx context.Context, fed core.FederationName, h core.FederateHandle) (mom.FederateMetrics, error) {
	if err := deps.validate(fed, h); err != nil {
		return mom.FederateMetrics{}, err
	}
	var result mom.FederateMetrics
	if deps.Host != nil {
		host, known, err := deps.Host(fed, h)
		if err != nil {
			return result, err
		}
		if known {
			result.Host = &host
		}
	}
	if deps.RTIVersion != "" {
		value := deps.RTIVersion
		result.RTIVersion = &value
	}
	if deps.Time != nil {
		if deps.isInteger(fed) {
			result.Time, result.ROQueued = typedMOMTimeMetrics(deps.IntegerTime, fed, h)
		} else {
			result.Time, result.ROQueued = typedMOMTimeMetrics(deps.Time, fed, h)
		}
	}
	if deps.Save != nil {
		var state uint32
		if deps.isInteger(fed) {
			state = deps.IntegerSave.QueryMOMMetrics(fed, h).FederateState
		} else {
			state = deps.Save.QueryMOMMetrics(fed, h).FederateState
		}
		result.FederateState = &state
	}
	if err := deps.populateDataQueueMetrics(ctx, fed, h, &result); err != nil {
		return result, err
	}
	counts, err := deps.catalog.ObjectInstanceCounts(ctx, fed, h, mom.ObjectsDeletable)
	if err != nil {
		return result, err
	}
	var total uint64
	for _, count := range counts {
		total += uint64(count)
	}
	result.DeletableObjects = &total
	return result, nil
}

func (deps momMetricSources) federation(ctx context.Context, fed core.FederationName) (mom.FederationMetrics, error) {
	if err := deps.validate(fed, 0); err != nil {
		return mom.FederationMetrics{}, err
	}
	var result mom.FederationMetrics
	if deps.RTIVersion != "" {
		value := deps.RTIVersion
		result.RTIVersion = &value
	}
	timeName := deps.TimeImplementationName
	if deps.TimeImplementation != nil {
		var found bool
		timeName, found = deps.TimeImplementation(fed)
		if !found || timeName == "" {
			return result, core.ErrFederationNotFound
		}
	}
	if timeName != "" {
		value := timeName
		result.TimeImplementationName = &value
	}
	if deps.MIMDesignator != nil {
		if designator, err := deps.MIMDesignator(ctx, fed); err == nil && designator != "" {
			result.MIMDesignator = &designator
		}
	}
	if deps.CurrentFDD != nil {
		if encoded, err := deps.CurrentFDD(ctx, fed); err == nil {
			value := string(encoded)
			result.CurrentFDD = &value
		}
	}
	if deps.Save != nil {
		deps.populateFederationSaveMetrics(fed, &result)
	}
	return result, nil
}

func (deps momObservationDependencies) report(ctx context.Context, observation grpcadapter.ServiceObservation) error {
	fom, err := deps.FOMs.Get(ctx, observation.Federation)
	if err != nil {
		return err
	}
	names, ok := fom.(core.FOMHandleNameLookup)
	if !ok {
		return fmt.Errorf("MOM: FOM names unavailable")
	}
	inv := mom.ServiceInvocation{Service: observation.Service, Success: observation.Success, SuppliedArguments: observation.SuppliedArguments, ReturnedArguments: observation.ReturnedArguments, Exception: observation.Exception}
	return deps.Dispatcher.ReportServiceInvocation(ctx, observation.Federation, observation.Federate, inv, fom, names)
}

func (deps momObservationDependencies) observeService(ctx context.Context, observation grpcadapter.ServiceObservation) error {
	if deps.Manager == nil {
		return nil
	}
	inv := mom.ServiceInvocation{Service: observation.Service, Success: observation.Success}
	countErr := deps.Manager.RecordServiceInvocation(observation.Federation, observation.Federate, inv)
	return errors.Join(countErr, deps.report(ctx, observation))
}

func (deps momObservationDependencies) observeCallback(ctx context.Context, fed core.FederationName, h core.FederateHandle, event *rtiv1.FederateEvent, success bool, exception string) (result error) {
	if deps.Manager == nil {
		return nil
	}
	observation, known := grpcadapter.CallbackObservation(fed, h, event, success, exception)
	if !known {
		return fmt.Errorf("MOM: callback observation unavailable")
	}
	inv := mom.CallbackInvocation{Service: observation.Service}
	if reflected := event.GetReflect(); reflected != nil {
		inv.Object = core.ObjectHandle(reflected.GetObjectHandle())
		inv.ObjectClass = core.ObjectClassHandle(reflected.GetObjectClassHandle())
	}
	countErr := deps.Manager.RecordCallbackCompletion(fed, h, mom.CallbackCompletion{
		Invocation: inv, DataCountedAtEntry: deps.DataCallbacksCountedAtEntry && momEntryCountedData(event),
	})
	// Do not generate telemetry about receiving telemetry. This is an
	// explicit implementation loop-prevention rule, not a normative claim.
	// The actual callback remains included in invocation metrics above.
	if received := event.GetReceive(); received != nil {
		fom, err := deps.FOMs.Get(ctx, fed)
		if err != nil {
			return errors.Join(countErr, err)
		}
		if momReportClass(fom, core.InteractionClassHandle(received.GetInteractionClassHandle())) {
			return countErr
		}
	}
	return errors.Join(countErr, deps.report(ctx, observation))
}

func (deps momMetricSources) populateDataQueueMetrics(ctx context.Context, fed core.FederationName, h core.FederateHandle, result *mom.FederateMetrics) error {
	if deps.Time == nil && deps.ROQueue != nil {
		value, err := deps.ROQueue(ctx, fed, h)
		if err != nil {
			return err
		}
		result.ROQueued = &value
	}
	if deps.DataQueues != nil {
		counts, err := deps.DataQueues(ctx, fed, h)
		if err != nil {
			return err
		}
		result.ROQueued = &counts.RO
		if result.Time == nil {
			return fmt.Errorf("MOM: data queue time source unavailable")
		}
		result.Time.TSOQueued = &counts.TSO
	}
	return nil
}

func (deps momMetricSources) populateFederationSaveMetrics(fed core.FederationName, result *mom.FederationMetrics) {
	if deps.isInteger(fed) {
		actual := deps.IntegerSave.QueryMOMMetrics(fed, 0)
		if actual.LastSaveHistoryKnown {
			result.LastSaveName = &actual.LastSaveName
			value := typedMOMLogicalTime(actual.LastSaveTime)
			result.LastSaveTime = &value
		}
		result.NextSaveName = &actual.NextSaveName
		value := typedMOMLogicalTime(actual.NextSaveTime)
		result.NextSaveTime = &value
		return
	}
	actual := deps.Save.QueryMOMMetrics(fed, 0)
	if actual.LastSaveHistoryKnown {
		result.LastSaveName = &actual.LastSaveName
		value := toMOMLogicalTime(actual.LastSaveTime)
		result.LastSaveTime = &value
	}
	result.NextSaveName = &actual.NextSaveName
	value := toMOMLogicalTime(actual.NextSaveTime)
	result.NextSaveTime = &value
}

func (deps momObservationDependencies) observationCaller() func(context.Context) (core.FederationName, core.FederateHandle, func(), error) {
	resolveCaller := deps.ResolveCaller
	if resolveCaller != nil {
		original := resolveCaller
		resolveCaller = func(ctx context.Context) (core.FederationName, core.FederateHandle, func(), error) {
			if id, ok := ctx.Value(momServiceAdmissionKey{}).(momServiceAdmissionIdentity); ok {
				return id.federation, id.sender, func() {}, ctx.Err()
			}
			return original(ctx)
		}
	}
	return resolveCaller
}

func (deps momObservationDependencies) observationAdmission() func(context.Context, core.FederationName) (func(), error) {
	acquireService := deps.AcquireService
	if acquireService != nil {
		original := acquireService
		acquireService = func(ctx context.Context, fed core.FederationName) (func(), error) {
			admitted, held := grpcadapter.CheckpointAdmissionFederation(ctx)
			if heldMOMCheckpoint(ctx, fed) || held && admitted == fed {
				return func() {}, ctx.Err()
			}
			return original(ctx, fed)
		}
	}
	return acquireService
}
