package server

import (
	"context"
	"fmt"
	"math"
	"slices"

	"github.com/cbchoi/gorti/rti/internal/application/composition"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ddm"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
	"github.com/cbchoi/gorti/rti/internal/hla/federation"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
	syncpkg "github.com/cbchoi/gorti/rti/internal/hla/synchronization"
	"github.com/cbchoi/gorti/rti/pkg/fom/mim"
	"github.com/cbchoi/gorti/rti/pkg/fom/parser"
)

type momCatalogDependencies struct {
	Manager        *mom.Manager
	Traffic        *mom.TrafficAccumulator
	Declarations   *declaration.Manager
	Objects        *object.Registry
	ObjectServices interface {
		LookupKnownObjectInstance(core.FederationName, core.FederateHandle, core.ObjectHandle) (string, core.ObjectClassHandle, bool)
		ClassOf(core.FederationName, core.ObjectHandle) core.ObjectClassHandle
		ModifyAttributeState(context.Context, core.FederationName, core.FederateHandle, core.ObjectHandle, core.AttributeHandle, bool) error
	}
	Ownership       *ownership.Manager
	Synchronization *syncpkg.Manager
	Regions         *ddm.Manager
	FOMs            *composition.Repository
	Federations     *federation.Manager
	JoinedModules   func(context.Context, core.FederationName, core.FederateHandle) ([]mom.FOMModule, error)
}

func configureMOMCatalog(dispatcher *mom.Dispatcher, deps momCatalogDependencies) error {
	if dispatcher == nil {
		return fmt.Errorf("MOM catalog dispatcher unavailable")
	}
	sources, err := newMOMCatalogSources(deps)
	if err != nil {
		return err
	}
	dispatcher.SetCatalogSources(sources)
	return nil
}

func newMOMCatalogSources(deps momCatalogDependencies) (mom.CatalogSources, error) {
	if deps.Manager == nil || deps.Traffic == nil || deps.Declarations == nil || (deps.Objects == nil && deps.ObjectServices == nil) || deps.Ownership == nil || deps.Synchronization == nil || deps.Regions == nil || deps.FOMs == nil || deps.Federations == nil {
		return mom.CatalogSources{}, fmt.Errorf("MOM catalog dependencies incomplete")
	}
	if deps.ObjectServices == nil {
		deps.ObjectServices = deps.Objects
	}
	return mom.CatalogSources{
		Subscriptions:             deps.catalogSubscriptions,
		ObjectInstanceCounts:      deps.catalogObjectInstanceCounts,
		ObjectInstanceInformation: deps.catalogObjectInstanceInformation,
		FOMModules:                deps.catalogFOMModules,
		SynchronizationPoints:     deps.catalogSynchronizationPoints,
		ModifyAttributeState:      deps.catalogModifyAttributeState,
		ReportRecipients:          deps.catalogReportRecipients,
	}, nil
}

func momFederationModulesFromSource(view core.FOMView) ([]mom.FOMModule, error) {
	baseline, err := mim.StandardMIMHandle()
	if view.MIMModule != nil {
		baseline, err = mim.ParseSelected(view.MIMModule.XML)
	}
	if err != nil {
		return nil, err
	}
	modules := make([]parser.Module, len(view.Modules))
	for i, module := range view.Modules {
		modules[i] = parser.Module{Path: module.Path, XML: module.XML}
	}
	indices, err := parser.ContributingModuleIndices(modules, baseline)
	if err != nil {
		return nil, err
	}
	contributing := make([]core.FOMModule, 0, len(indices))
	for _, i := range indices {
		contributing = append(contributing, view.Modules[i])
	}
	return momModulesFromSource(contributing)
}

// Indicators are zero-based positions in the emitted local designator array.
// Exact byte duplicates retain the first supplied designator and source bytes.
// Empty designators/source bytes are unavailable, not replaced with placeholders.
func momModulesFromSource(modules []core.FOMModule) ([]mom.FOMModule, error) {
	var result []mom.FOMModule
	seen := map[string]bool{}
	for _, module := range modules {
		if len(module.XML) == 0 {
			return nil, fmt.Errorf("MOM: exact FOM module source unavailable")
		}
		if seen[string(module.XML)] {
			continue
		}
		seen[string(module.XML)] = true
		if module.Path == "" {
			return nil, fmt.Errorf("MOM: supplied FOM module designator unavailable")
		}
		indicator := len(result)
		if indicator < 0 || indicator > math.MaxInt32 {
			return nil, mom.ErrCounterOverflow
		}
		result = append(result, mom.FOMModule{Indicator: int32(indicator), Designator: module.Path, Data: string(module.XML)})
	}
	return result, nil
}

func momSubscriptions(global *declaration.ManagerState, regional *ddm.ManagerState, fom core.FOMHandle, target core.FederateHandle) (mom.SubscriptionSnapshot, error) {
	a := momSubscriptionAccumulator{fom: fom, objects: map[momSubscriptionKey]*momSubscriptionGroup{}, interactions: map[mom.InteractionSubscription]bool{}}
	if err := a.addGlobal(global, target); err != nil {
		return mom.SubscriptionSnapshot{}, err
	}
	if err := a.addRegional(regional, target); err != nil {
		return mom.SubscriptionSnapshot{}, err
	}
	return a.snapshot(), nil
}

type momSubscriptionKey struct {
	class  core.ObjectClassHandle
	active bool
}
type momSubscriptionGroup struct {
	attrs     map[core.AttributeHandle]bool
	rate      string
	value     float64
	unbounded bool
}

type momSubscriptionAccumulator struct {
	fom          core.FOMHandle
	objects      map[momSubscriptionKey]*momSubscriptionGroup
	interactions map[mom.InteractionSubscription]bool
}

func (a *momSubscriptionAccumulator) add(class core.ObjectClassHandle, attr core.AttributeHandle, active bool, rate string) error {
	k := momSubscriptionKey{class, active}
	g := a.objects[k]
	if g == nil {
		g = &momSubscriptionGroup{attrs: map[core.AttributeHandle]bool{}}
		a.objects[k] = g
	}
	g.attrs[attr] = true
	if rate == "" {
		g.unbounded = true
		g.rate = ""
		return nil
	}
	rates, ok := a.fom.(core.FOMUpdateRates)
	if !ok {
		return fmt.Errorf("MOM: update-rate values unavailable")
	}
	value, ok := rates.UpdateRateValue(rate)
	if !ok {
		return fmt.Errorf("MOM: unknown subscribed update rate %q", rate)
	}
	if !g.unbounded && (g.rate == "" || value > g.value || value == g.value && rate < g.rate) {
		g.rate, g.value = rate, value
	}
	return nil
}

func (a *momSubscriptionAccumulator) addGlobal(global *declaration.ManagerState, target core.FederateHandle) error {
	for _, entry := range global.ObjectOptions {
		if entry.Federate == target {
			if err := a.add(entry.Class, entry.Attribute, entry.Options.Active, entry.Options.UpdateRateDesignator); err != nil {
				return err
			}
		}
	}
	for _, entry := range global.InteractionOptions {
		if entry.Federate == target {
			a.interactions[mom.InteractionSubscription{Class: entry.Class, Active: entry.Active}] = true
		}
	}
	return nil
}

func (a *momSubscriptionAccumulator) addRegional(regional *ddm.ManagerState, target core.FederateHandle) error {
	if regional != nil {
		for _, entry := range regional.ObjectSubscriptions {
			if entry.Subscriber == target {
				if err := a.add(entry.ObjectClass, entry.Attribute, !entry.Passive, entry.UpdateRateDesignator); err != nil {
					return err
				}
			}
		}
		for _, entry := range regional.InteractionSubscriptions {
			if entry.Subscriber == target {
				a.interactions[mom.InteractionSubscription{Class: entry.InteractionClass, Active: !entry.Passive}] = true
			}
		}
	}
	return nil
}

func (a *momSubscriptionAccumulator) snapshot() mom.SubscriptionSnapshot {
	var result mom.SubscriptionSnapshot
	for k, g := range a.objects {
		entry := mom.ObjectSubscription{Class: k.class, Active: k.active, MaxUpdateRate: g.rate}
		for attr := range g.attrs {
			entry.Attributes = append(entry.Attributes, attr)
		}
		slices.Sort(entry.Attributes)
		result.Objects = append(result.Objects, entry)
	}
	for entry := range a.interactions {
		result.Interactions = append(result.Interactions, entry)
	}
	slices.SortFunc(result.Objects, compareMOMObjectSubscriptions)
	slices.SortFunc(result.Interactions, compareMOMInteractionSubscriptions)
	return result
}

// Callers hold operation admission; validate the roster without reacquiring its lease.
func (deps momCatalogDependencies) validateCatalogMember(ctx context.Context, fed core.FederationName, h core.FederateHandle) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, ok := deps.Federations.GenerationFor(fed); !ok {
		return core.ErrFederationNotFound
	}
	if h != 0 && !slices.Contains(deps.Federations.MembersOf(fed), h) {
		return core.ErrFederateNotJoined
	}
	return nil
}

func (deps momCatalogDependencies) catalogSubscriptions(ctx context.Context, fed core.FederationName, h core.FederateHandle) (mom.SubscriptionSnapshot, error) {
	if err := deps.validateCatalogMember(ctx, fed, h); err != nil {
		return mom.SubscriptionSnapshot{}, err
	}
	fom, err := deps.FOMs.Get(ctx, fed)
	if err != nil {
		return mom.SubscriptionSnapshot{}, err
	}
	return momSubscriptions(deps.Declarations.ExportState(fed), deps.Regions.ExportState(fed), fom, h)
}

func (deps momCatalogDependencies) catalogObjectInstanceCounts(ctx context.Context, fed core.FederationName, h core.FederateHandle, kind mom.ObjectInstanceCountKind) (map[uint64]uint32, error) {
	if err := deps.validateCatalogMember(ctx, fed, h); err != nil {
		return nil, err
	}
	if kind != mom.ObjectsDeletable {
		return deps.Traffic.ObjectInstanceCounts(ctx, fed, h, kind)
	}
	fom, err := deps.FOMs.Get(ctx, fed)
	if err != nil {
		return nil, err
	}
	counts := map[uint64]uint32{}
	for _, inst := range objectInstances(deps.ObjectServices, fed) {
		if _, _, known := deps.ObjectServices.LookupKnownObjectInstance(fed, h, inst.Handle); !known {
			continue
		}
		privilege, ok := fom.LookupAttribute(inst.Class, "HLAprivilegeToDeleteObject")
		if !ok {
			return nil, fmt.Errorf("MOM: delete privilege definition unavailable")
		}
		if owner, owned := deps.Ownership.QueryOwnership(fed, inst.Handle, privilege); owned && owner == h {
			if counts[uint64(inst.Class)] == math.MaxInt32 {
				return nil, mom.ErrCounterOverflow
			}
			counts[uint64(inst.Class)]++
		}
	}
	return counts, nil
}

func (deps momCatalogDependencies) catalogObjectInstanceInformation(ctx context.Context, fed core.FederationName, h core.FederateHandle, obj core.ObjectHandle) (mom.ObjectInstanceInformation, error) {
	if err := deps.validateCatalogMember(ctx, fed, h); err != nil {
		return mom.ObjectInstanceInformation{}, err
	}
	_, knownClass, known := deps.ObjectServices.LookupKnownObjectInstance(fed, h, obj)
	if !known {
		return mom.ObjectInstanceInformation{}, nil
	}
	registered := deps.ObjectServices.ClassOf(fed, obj)
	if registered == 0 {
		return mom.ObjectInstanceInformation{}, fmt.Errorf("MOM: object changed during information query")
	}
	info := mom.ObjectInstanceInformation{Known: true, RegisteredClass: registered, KnownClass: knownClass}
	if state := deps.Ownership.ExportState(fed); state != nil {
		for _, entry := range state.Owners {
			if entry.Object == obj && entry.Owner == h {
				info.OwnedAttributes = append(info.OwnedAttributes, entry.Attribute)
			}
		}
	}
	return info, nil
}

func (deps momCatalogDependencies) catalogFOMModules(ctx context.Context, fed core.FederationName, h core.FederateHandle) ([]mom.FOMModule, error) {
	if err := deps.validateCatalogMember(ctx, fed, h); err != nil {
		return nil, err
	}
	if h != 0 {
		if deps.JoinedModules == nil {
			return nil, fmt.Errorf("MOM: join-supplied FOM module baseline unavailable")
		}
		return deps.JoinedModules(ctx, fed, h)
	}
	view, err := deps.FOMs.FOMView(ctx, fed)
	if err != nil {
		return nil, err
	}
	if view.Digest == "" {
		return nil, fmt.Errorf("MOM: authoritative FOM source baseline unavailable")
	}
	return momFederationModulesFromSource(view)
}

func (deps momCatalogDependencies) catalogSynchronizationPoints(ctx context.Context, fed core.FederationName) ([]mom.SynchronizationPoint, error) {
	if err := deps.validateCatalogMember(ctx, fed, 0); err != nil {
		return nil, err
	}
	var points []mom.SynchronizationPoint
	if state := deps.Synchronization.ExportState(fed); state != nil {
		for _, point := range state.Points {
			if point.State == syncpkg.StateAchieved {
				continue
			}
			if point.State != syncpkg.StateAnnounced || point.Dynamic {
				return nil, fmt.Errorf("MOM: synchronization participant baseline unavailable")
			}
			p := mom.SynchronizationPoint{Name: point.Label}
			for _, h := range point.Required {
				status := uint32(2)
				if slices.Contains(point.Achieved, h) {
					status = 3
				}
				p.Federates = append(p.Federates, mom.SynchronizationFederate{Handle: h, Status: status})
			}
			points = append(points, p)
		}
	}
	return points, nil
}

func (deps momCatalogDependencies) catalogModifyAttributeState(ctx context.Context, fed core.FederationName, h core.FederateHandle, obj core.ObjectHandle, attr core.AttributeHandle, owned bool) error {
	if err := deps.validateCatalogMember(ctx, fed, h); err != nil {
		return err
	}
	return deps.ObjectServices.ModifyAttributeState(ctx, fed, h, obj, attr, owned)
}

func (deps momCatalogDependencies) catalogReportRecipients(ctx context.Context, fed core.FederationName, subject core.FederateHandle, class, service string) ([]core.FederateHandle, error) {
	validationSubject := deps.reportValidationSubject(fed, subject, class, service)
	if err := deps.validateCatalogMember(ctx, fed, validationSubject); err != nil {
		return nil, err
	}
	fom, err := deps.FOMs.Get(ctx, fed)
	if err != nil {
		return nil, err
	}
	cls, ok := fom.LookupInteractionClass(class)
	if !ok {
		return nil, fmt.Errorf("MOM: report class unavailable")
	}
	classes := []core.InteractionClassHandle{cls}
	if ancestry, ok := fom.(core.FOMClassAncestry); ok {
		classes = ancestry.InteractionClassAncestors(cls)
	}
	var recipients []core.FederateHandle
	for _, c := range classes {
		recipients = append(recipients, deps.Declarations.InteractionSubscribersFor(ctx, fed, c)...)
	}
	regional, err := momRegionalReportRecipients(deps.Regions.ExportState(fed), fom, classes, subject, service)
	if err != nil {
		return nil, err
	}
	recipients = append(recipients, regional...)
	slices.Sort(recipients)
	recipients = slices.Compact(recipients)
	if validationSubject == 0 && subject != 0 && !slices.Contains(deps.Federations.MembersOf(fed), subject) {
		recipients = slices.DeleteFunc(recipients, func(h core.FederateHandle) bool { return h == subject })
	}
	for _, h := range recipients {
		if err := deps.validateCatalogMember(ctx, fed, h); err != nil {
			return nil, err
		}
	}
	return recipients, nil
}

func (deps momCatalogDependencies) reportValidationSubject(fed core.FederationName, subject core.FederateHandle, class, service string) core.FederateHandle {
	validationSubject := subject
	// The completed resign is reported under its exclusive lifecycle
	// lease after roster removal, while its MOM state is still retained.
	// No other service may route a report for an absent subject.
	if service == momServiceResignFederationExecution && (class == mom.ClassReportServiceInvocation || class == mom.ClassReportException) {
		if _, retained := deps.Manager.QueryFederateAttributes(fed, subject); retained {
			validationSubject = 0
		}
	}
	return validationSubject
}

func compareMOMObjectSubscriptions(a, b mom.ObjectSubscription) int {
	if a.Class < b.Class {
		return -1
	}
	if a.Class > b.Class {
		return 1
	}
	if !a.Active && b.Active {
		return -1
	}
	if a.Active && !b.Active {
		return 1
	}
	return 0
}

func compareMOMInteractionSubscriptions(a, b mom.InteractionSubscription) int {
	if a.Class < b.Class {
		return -1
	}
	if a.Class > b.Class {
		return 1
	}
	if !a.Active && b.Active {
		return -1
	}
	if a.Active && !b.Active {
		return 1
	}
	return 0
}
