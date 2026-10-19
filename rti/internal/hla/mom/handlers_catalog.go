package mom

import (
	"bytes"
	"cmp"
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

const (
	federateRequest                             = "HLAmanager.HLAfederate.HLArequest."
	federationRequest                           = "HLAmanager.HLAfederation.HLArequest."
	ClassRequestSubscriptions                   = federateRequest + "HLArequestSubscriptions"
	ClassReportObjectClassSubscription          = "HLAmanager.HLAfederate.HLAreport.HLAreportObjectClassSubscription"
	ClassReportInteractionSubscription          = "HLAmanager.HLAfederate.HLAreport.HLAreportInteractionSubscription"
	ClassRequestObjectInstancesThatCanBeDeleted = federateRequest + "HLArequestObjectInstancesThatCanBeDeleted"
	ClassRequestObjectInstancesUpdated          = federateRequest + "HLArequestObjectInstancesUpdated"
	ClassRequestObjectInstancesReflected        = federateRequest + "HLArequestObjectInstancesReflected"
	ClassRequestObjectInstanceInformation       = federateRequest + "HLArequestObjectInstanceInformation"
	ClassRequestFederateFOMModuleData           = federateRequest + "HLArequestFOMmoduleData"
	ClassRequestFederationFOMModuleData         = federationRequest + "HLArequestFOMmoduleData"
	ClassRequestSynchronizationPoints           = federationRequest + "HLArequestSynchronizationPoints"
	ClassRequestSynchronizationPointStatus      = federationRequest + "HLArequestSynchronizationPointStatus"
	ClassModifyAttributeState                   = "HLAmanager.HLAfederate.HLAadjust.HLAmodifyAttributeState"
)

type parameterError struct{ message string }

func (e *parameterError) Error() string { return e.message }
func malformed(format string, args ...any) error {
	return &parameterError{fmt.Sprintf(format, args...)}
}

func registerCatalogHandlers(d *Dispatcher) {
	d.Register(ClassRequestSubscriptions, handleRequestSubscriptions)
	for request, kind := range map[string]ObjectInstanceCountKind{
		ClassRequestObjectInstancesThatCanBeDeleted: ObjectsDeletable,
		ClassRequestObjectInstancesUpdated:          ObjectsUpdated,
		ClassRequestObjectInstancesReflected:        ObjectsReflected,
	} {
		d.Register(request, instanceCountHandler(request, kind))
	}
	d.Register(ClassRequestObjectInstanceInformation, handleRequestObjectInformation)
	d.Register(ClassRequestFederateFOMModuleData, moduleDataHandler(ClassRequestFederateFOMModuleData, true))
	d.Register(ClassRequestFederationFOMModuleData, moduleDataHandler(ClassRequestFederationFOMModuleData, false))
	d.Register(ClassRequestSynchronizationPoints, synchronizationHandler(false))
	d.Register(ClassRequestSynchronizationPointStatus, synchronizationHandler(true))
	d.Register(ClassModifyAttributeState, handleModifyAttributeState)
}

func validateParameters(dctx DispatchContext, class string, params map[core.ParameterHandle][]byte, names ...string) error {
	if len(params) != len(names) {
		return malformed("%s: expected %d parameters, got %d", class, len(names), len(params))
	}
	for _, name := range names {
		if dctx.FOM == nil || findParam(dctx, class, name, params) == nil {
			return malformed("%s: missing %s", class, name)
		}
	}
	return nil
}

func catalogTarget(dctx DispatchContext, class string, params map[core.ParameterHandle][]byte, extra ...string) (core.FederateHandle, error) {
	if err := validateParameters(dctx, class, params, append([]string{"HLAfederate"}, extra...)...); err != nil {
		return 0, err
	}
	target, err := decodeSwitchTarget(dctx, class, params)
	if err != nil {
		return 0, malformed("%v", err)
	}
	if dctx.MOM == nil {
		return 0, fmt.Errorf("mom: manager unavailable")
	}
	if _, ok := dctx.MOM.QueryFederateAttributes(dctx.Federation, target); !ok {
		return 0, core.ErrFederateNotJoined
	}
	return target, nil
}

func catalogFederation(dctx DispatchContext) error {
	if dctx.MOM == nil {
		return fmt.Errorf("mom: manager unavailable")
	}
	if _, ok := dctx.MOM.QueryFederationAttributes(dctx.Federation); !ok {
		return core.ErrFederationNotFound
	}
	return nil
}

func parameterHandle(dctx DispatchContext, class, name string, params map[core.ParameterHandle][]byte) (uint64, error) {
	b := findParam(dctx, class, name, params)
	if len(b) != 12 || binary.BigEndian.Uint32(b) != 8 || binary.BigEndian.Uint64(b[4:]) == 0 {
		return 0, malformed("%s: invalid framed HLAhandle", name)
	}
	return binary.BigEndian.Uint64(b[4:]), nil
}

func parameterInteger(dctx DispatchContext, class, name string, params map[core.ParameterHandle][]byte) (int32, error) {
	b := findParam(dctx, class, name, params)
	if len(b) != 4 {
		return 0, malformed("%s: expected HLAinteger32BE", name)
	}
	var value int32
	if err := binary.Read(bytes.NewReader(b), binary.BigEndian, &value); err != nil {
		return 0, err
	}
	return value, nil
}

func reportFor(request string, params map[string][]byte) ResponseInteraction {
	return ResponseInteraction{ClassName: strings.ReplaceAll(request, "HLArequest", "HLAreport"), Params: params}
}

func encodedAttributes(attrs []core.AttributeHandle) ([]byte, error) {
	attrs = slices.Clone(attrs)
	slices.Sort(attrs)
	handles := make([]core.FederateHandle, len(attrs))
	for i, a := range attrs {
		if a == 0 || (i > 0 && a == attrs[i-1]) {
			return nil, fmt.Errorf("mom: invalid or duplicate source attribute")
		}
		handles[i] = core.FederateHandle(a)
	}
	return encodeFederateHandleList(handles), nil
}

func boolEncoding(v bool) []byte {
	if v {
		return encodeHLAcount(1)
	}
	return encodeHLAcount(0)
}

func handleRequestSubscriptions(ctx context.Context, dctx DispatchContext, _ core.FederateHandle, params map[core.ParameterHandle][]byte) ([]ResponseInteraction, error) {
	target, err := catalogTarget(dctx, ClassRequestSubscriptions, params)
	if err != nil {
		return nil, err
	}
	if dctx.Catalog.Subscriptions == nil {
		return nil, fmt.Errorf("mom: subscription state unavailable")
	}
	state, err := dctx.Catalog.Subscriptions(ctx, dctx.Federation, target)
	if err != nil {
		return nil, err
	}
	responses, err := objectSubscriptionReports(target, state.Objects)
	if err != nil {
		return nil, err
	}
	encoded, err := encodeInteractionSubscriptions(state.Interactions)
	if err != nil {
		return nil, err
	}
	responses = append(responses, ResponseInteraction{ClassName: ClassReportInteractionSubscription, Params: map[string][]byte{"HLAfederate": encodeHLAhandle(target), "HLAinteractionClassList": encoded}})
	return responses, nil
}

func compareSubscription(classA, classB uint64, activeA, activeB bool) int {
	if order := cmp.Compare(classA, classB); order != 0 {
		return order
	}
	if activeA == activeB {
		return 0
	}
	if activeA {
		return 1
	}
	return -1
}

func objectSubscriptionReports(target core.FederateHandle, objects []ObjectSubscription) ([]ResponseInteraction, error) {
	objects = slices.Clone(objects)
	slices.SortFunc(objects, func(a, b ObjectSubscription) int {
		return compareSubscription(uint64(a.Class), uint64(b.Class), a.Active, b.Active)
	})
	count, err := encodeArrayLength(len(objects))
	if err != nil {
		return nil, err
	}
	var responses []ResponseInteraction
	for i, o := range objects {
		if o.Class == 0 || (i > 0 && o.Class == objects[i-1].Class && o.Active == objects[i-1].Active) {
			return nil, fmt.Errorf("mom: invalid or duplicate object subscription combination")
		}
		attrs, err := encodedAttributes(o.Attributes)
		if err != nil {
			return nil, err
		}
		responses = append(responses, ResponseInteraction{ClassName: ClassReportObjectClassSubscription, Params: map[string][]byte{
			"HLAfederate": encodeHLAhandle(target), "HLAnumberOfClasses": count,
			"HLAobjectClass": encodeHLAhandle(core.FederateHandle(o.Class)), "HLAactive": boolEncoding(o.Active),
			"HLAmaxUpdateRate": encodeHLAunicodeString(o.MaxUpdateRate), "HLAattributeList": attrs,
		}})
	}
	if len(objects) == 0 {
		responses = append(responses, ResponseInteraction{ClassName: ClassReportObjectClassSubscription, Params: map[string][]byte{"HLAfederate": encodeHLAhandle(target), "HLAnumberOfClasses": encodeHLAcount(0)}})
	}
	return responses, nil
}

func encodeInteractionSubscriptions(interactions []InteractionSubscription) ([]byte, error) {
	interactions = slices.Clone(interactions)
	slices.SortFunc(interactions, func(a, b InteractionSubscription) int {
		return compareSubscription(uint64(a.Class), uint64(b.Class), a.Active, b.Active)
	})
	encoded, err := encodeArrayLength(len(interactions))
	if err != nil {
		return nil, err
	}
	for i, v := range interactions {
		if v.Class == 0 || (i > 0 && v.Class == interactions[i-1].Class && v.Active == interactions[i-1].Active) {
			return nil, fmt.Errorf("mom: invalid or duplicate interaction subscription")
		}
		encoded = append(encoded, encodeHLAhandle(core.FederateHandle(v.Class))...)
		encoded = append(encoded, boolEncoding(v.Active)...)
	}
	return encoded, nil
}

func instanceCountHandler(request string, kind ObjectInstanceCountKind) Handler {
	return func(ctx context.Context, dctx DispatchContext, _ core.FederateHandle, params map[core.ParameterHandle][]byte) ([]ResponseInteraction, error) {
		target, err := catalogTarget(dctx, request, params)
		if err != nil {
			return nil, err
		}
		if dctx.Catalog.ObjectInstanceCounts == nil {
			return nil, ErrCounterDataUnavailable
		}
		counts, err := dctx.Catalog.ObjectInstanceCounts(ctx, dctx.Federation, target, kind)
		if err != nil {
			return nil, err
		}
		encoded, err := encodeClassCounts(counts)
		if err != nil {
			return nil, err
		}
		return []ResponseInteraction{reportFor(request, map[string][]byte{"HLAfederate": encodeHLAhandle(target), "HLAobjectInstanceCounts": encoded})}, nil
	}
}

func handleRequestObjectInformation(ctx context.Context, dctx DispatchContext, _ core.FederateHandle, params map[core.ParameterHandle][]byte) ([]ResponseInteraction, error) {
	target, err := catalogTarget(dctx, ClassRequestObjectInstanceInformation, params, "HLAobjectInstance")
	if err != nil {
		return nil, err
	}
	obj, err := parameterHandle(dctx, ClassRequestObjectInstanceInformation, "HLAobjectInstance", params)
	if err != nil {
		return nil, err
	}
	if dctx.Catalog.ObjectInstanceInformation == nil {
		return nil, fmt.Errorf("mom: object information unavailable")
	}
	info, err := dctx.Catalog.ObjectInstanceInformation(ctx, dctx.Federation, target, core.ObjectHandle(obj))
	if err != nil {
		return nil, err
	}
	if info.Known && (info.RegisteredClass == 0 || info.KnownClass == 0) || !info.Known && (info.RegisteredClass != 0 || info.KnownClass != 0 || len(info.OwnedAttributes) != 0) {
		return nil, fmt.Errorf("mom: inconsistent object information")
	}
	attrs, err := encodedAttributes(info.OwnedAttributes)
	if err != nil {
		return nil, err
	}
	p := map[string][]byte{"HLAfederate": encodeHLAhandle(target), "HLAobjectInstance": encodeHLAhandle(core.FederateHandle(obj)), "HLAownedInstanceAttributeList": attrs}
	if info.Known {
		p["HLAregisteredClass"] = encodeHLAhandle(core.FederateHandle(info.RegisteredClass))
		p["HLAknownClass"] = encodeHLAhandle(core.FederateHandle(info.KnownClass))
	}
	return []ResponseInteraction{reportFor(ClassRequestObjectInstanceInformation, p)}, nil
}

func moduleDataHandler(request string, federate bool) Handler {
	return func(ctx context.Context, dctx DispatchContext, _ core.FederateHandle, params map[core.ParameterHandle][]byte) ([]ResponseInteraction, error) {
		var target core.FederateHandle
		var err error
		if federate {
			target, err = catalogTarget(dctx, request, params, "HLAFOMmoduleIndicator")
		} else {
			err = validateParameters(dctx, request, params, "HLAFOMmoduleIndicator")
			if err == nil {
				err = catalogFederation(dctx)
			}
		}
		if err != nil {
			return nil, err
		}
		indicator, err := parameterInteger(dctx, request, "HLAFOMmoduleIndicator", params)
		if err != nil {
			return nil, err
		}
		if indicator < 0 {
			return nil, malformed("HLAFOMmoduleIndicator: negative index")
		}
		if dctx.Catalog.FOMModules == nil {
			return nil, fmt.Errorf("mom: FOM module source unavailable")
		}
		modules, err := dctx.Catalog.FOMModules(ctx, dctx.Federation, target)
		if err != nil {
			return nil, err
		}
		if err := validateModules(modules); err != nil {
			return nil, err
		}
		for _, module := range modules {
			if module.Indicator == indicator {
				p := map[string][]byte{"HLAFOMmoduleIndicator": encodeHLAcount(uint32(indicator)), "HLAFOMmoduleData": encodeHLAunicodeString(module.Data)}
				if federate {
					p["HLAfederate"] = encodeHLAhandle(target)
				}
				return []ResponseInteraction{reportFor(request, p)}, nil
			}
		}
		return nil, fmt.Errorf("mom: FOM module indicator %d unavailable", indicator)
	}
}

func validateModules(modules []FOMModule) error {
	seen := map[int32]bool{}
	for _, m := range modules {
		if m.Indicator < 0 || seen[m.Indicator] || m.Designator == "" || m.Data == "" {
			return fmt.Errorf("mom: incomplete or duplicate FOM module source")
		}
		seen[m.Indicator] = true
	}
	return nil
}

// HLAvariableArray elements begin on their four-byte octet boundary. A final
// odd-length UTF-16 string needs no trailing padding; only the next element does.
func encodeStringList(values []string) ([]byte, error) {
	b, err := encodeArrayLength(len(values))
	if err != nil {
		return nil, err
	}
	for _, v := range values {
		for len(b)%4 != 0 {
			b = append(b, 0)
		}
		b = append(b, encodeHLAunicodeString(v)...)
	}
	return b, nil
}

func encodeArrayLength(length int) ([]byte, error) {
	if length < 0 {
		return nil, ErrCounterOverflow
	}
	count := uint64(length)
	if count > math.MaxUint32 {
		return nil, ErrCounterOverflow
	}
	return encodeHLAcount(uint32(count)), nil
}

func synchronizationHandler(status bool) Handler {
	request := ClassRequestSynchronizationPoints
	if status {
		request = ClassRequestSynchronizationPointStatus
	}
	return func(ctx context.Context, dctx DispatchContext, _ core.FederateHandle, params map[core.ParameterHandle][]byte) ([]ResponseInteraction, error) {
		if err := validateParameters(dctx, request, params); err != nil {
			return nil, err
		}
		if err := catalogFederation(dctx); err != nil {
			return nil, err
		}
		if dctx.Catalog.SynchronizationPoints == nil {
			return nil, fmt.Errorf("mom: synchronization state unavailable")
		}
		points, err := dctx.Catalog.SynchronizationPoints(ctx, dctx.Federation)
		if err != nil {
			return nil, err
		}
		points = slices.Clone(points)
		slices.SortFunc(points, func(a, b SynchronizationPoint) int { return strings.Compare(a.Name, b.Name) })
		var names []string
		for i, p := range points {
			if p.Name == "" || (i > 0 && p.Name == points[i-1].Name) {
				return nil, fmt.Errorf("mom: invalid or duplicate synchronization point")
			}
			names = append(names, p.Name)
		}
		if !status {
			encoded, err := encodeStringList(names)
			if err != nil {
				return nil, err
			}
			return []ResponseInteraction{reportFor(request, map[string][]byte{"HLAsyncPoints": encoded})}, nil
		}
		// The embedded MIM declares no selector. Enumerate actual points,
		// retaining the point name on every report rather than inventing one.
		var reports []ResponseInteraction
		for _, point := range points {
			encoded, err := encodeSynchronizationFederates(point.Federates)
			if err != nil {
				return nil, err
			}
			reports = append(reports, reportFor(request, map[string][]byte{"HLAsyncPointName": encodeHLAunicodeString(point.Name), "HLAsyncPointFederates": encoded}))
		}
		return reports, nil
	}
}

func handleModifyAttributeState(ctx context.Context, dctx DispatchContext, _ core.FederateHandle, params map[core.ParameterHandle][]byte) ([]ResponseInteraction, error) {
	target, err := catalogTarget(dctx, ClassModifyAttributeState, params, "HLAobjectInstance", "HLAattribute", "HLAattributeState")
	if err != nil {
		return nil, err
	}
	obj, err := parameterHandle(dctx, ClassModifyAttributeState, "HLAobjectInstance", params)
	if err != nil {
		return nil, err
	}
	attr, err := parameterHandle(dctx, ClassModifyAttributeState, "HLAattribute", params)
	if err != nil {
		return nil, err
	}
	state, err := parameterInteger(dctx, ClassModifyAttributeState, "HLAattributeState", params)
	if err != nil {
		return nil, err
	}
	if state < 0 || state > 1 {
		return nil, malformed("HLAattributeState: expected Unowned(0) or Owned(1)")
	}
	if dctx.Catalog.ModifyAttributeState == nil {
		return nil, fmt.Errorf("mom: ownership adjustment provider unavailable")
	}
	return nil, dctx.Catalog.ModifyAttributeState(ctx, dctx.Federation, target, core.ObjectHandle(obj), core.AttributeHandle(attr), state == 1)
}
func encodeSynchronizationFederates(members []SynchronizationFederate) ([]byte, error) {
	members = slices.Clone(members)
	slices.SortFunc(members, func(a, b SynchronizationFederate) int { return cmp.Compare(a.Handle, b.Handle) })
	encoded, err := encodeArrayLength(len(members))
	if err != nil {
		return nil, err
	}
	for i, m := range members {
		if m.Handle == 0 || m.Status > 3 || (i > 0 && members[i-1].Handle == m.Handle) {
			return nil, fmt.Errorf("mom: invalid synchronization federate state")
		}
		encoded = append(encoded, encodeHLAhandle(m.Handle)...)
		encoded = append(encoded, encodeHLAcount(m.Status)...)
	}
	return encoded, nil
}
