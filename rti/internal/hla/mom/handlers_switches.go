// HLAadjust.HLAsetSwitches handlers (M20.4).
//
// HLAsetSwitches is the spec-defined way for federates to flip
// MOM-tracked behavioral switches without a dedicated control RPC.
// Two classes:
//   HLAmanager.HLAfederation.HLAadjust.HLAsetSwitches
//     — federation-wide switches (HLAautoProvide)
//   HLAmanager.HLAfederate.HLAadjust.HLAsetSwitches
//     — per-federate switches (HLAconveyRegionDesignatorSets,
//        HLAconveyProducingFederate)
//
// The standard MIM defines HLAswitch as HLAinteger32BE:
// 0 = HLAfalse, 1 = HLAtrue. Other values are rejected.

package mom

import (
	"context"
	"encoding/binary"
	"fmt"
	"slices"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

const (
	ClassFederationSetSwitches = "HLAmanager.HLAfederation.HLAadjust.HLAsetSwitches"
	ClassFederateSetSwitches   = "HLAmanager.HLAfederate.HLAadjust.HLAsetSwitches"
	ClassSetServiceReporting   = "HLAmanager.HLAfederate.HLAadjust.HLAsetServiceReporting"
	ClassSetExceptionReporting = "HLAmanager.HLAfederate.HLAadjust.HLAsetExceptionReporting"
)

func registerSwitchHandlers(d *Dispatcher) {
	d.Register(ClassFederationSetSwitches, handleFederationSetSwitches)
	d.Register(ClassFederateSetSwitches, handleFederateSetSwitches)
	d.Register(ClassSetServiceReporting, handleSetServiceReporting)
	d.Register(ClassSetExceptionReporting, handleSetExceptionReporting)
}

// decodeHLAswitch reads the standard four-byte big-endian enumeration.
// A nil slice denotes an omitted parameter, not an encoded false value.
func decodeHLAswitch(b []byte) (bool, bool, error) {
	if b == nil {
		return false, false, nil
	}
	if len(b) != 4 {
		return false, false, fmt.Errorf("HLAswitch: want 4 bytes, got %d", len(b))
	}
	value := binary.BigEndian.Uint32(b)
	switch value {
	case 0:
		return false, true, nil
	case 1:
		return true, true, nil
	default:
		return false, false, fmt.Errorf("HLAswitch: invalid value %d", value)
	}
}

// findParam returns the encoded bytes of a named parameter, or nil
// if the sender didn't include it. The dispatcher uses
// FOMHandle.LookupParameter to resolve the name to a
// handle and then indexes into the per-call params map.
func findParam(dctx DispatchContext, cls string, paramName string, params map[core.ParameterHandle][]byte) []byte {
	classHandle, ok := dctx.FOM.LookupInteractionClass(cls)
	if !ok {
		return nil
	}
	h, ok := dctx.FOM.LookupParameter(classHandle, paramName)
	if !ok {
		return nil
	}
	b, present := params[h]
	if present && b == nil {
		return []byte{} // A present, empty encoding is malformed, not omitted.
	}
	return b
}

// decodeSwitchTarget requires the inherited HLAfederate parameter encoded
// as the MIM's HLAvariableArray<HLAbyte>. GORTI's SDK encodes its opaque
// handle in eight BE bytes; the array contributes a four-byte count.
// Raw four-byte handles are not accepted: they are indistinguishable from
// truncated array headers and could silently select a different federate.
func decodeSwitchTarget(dctx DispatchContext, cls string, params map[core.ParameterHandle][]byte) (core.FederateHandle, error) {
	if dctx.FOM == nil {
		return core.InvalidFederateHandle, fmt.Errorf("HLAfederate: no FOM lookup")
	}
	b := findParam(dctx, cls, "HLAfederate", params)
	if b == nil {
		return core.InvalidFederateHandle, fmt.Errorf("HLAfederate: missing or unresolved target parameter")
	}
	if len(b) < 4 {
		return core.InvalidFederateHandle, fmt.Errorf("HLAfederate: truncated HLAhandle array count")
	}
	if count := binary.BigEndian.Uint32(b); count != 8 {
		return core.InvalidFederateHandle, fmt.Errorf("HLAfederate: want 8 SDK handle bytes, array count is %d", count)
	}
	if len(b) != 12 {
		return core.InvalidFederateHandle, fmt.Errorf("HLAfederate: want 12 framed handle bytes, got %d", len(b))
	}
	target := core.FederateHandle(binary.BigEndian.Uint64(b[4:]))
	if target == core.InvalidFederateHandle {
		return target, fmt.Errorf("HLAfederate: invalid zero handle")
	}
	return target, nil
}

// Membership validation and the complete adjustment share a lock with
// join/resign, so a rejected target cannot leave a partial switch update.
func (m *Manager) adjustFederateSwitches(fed core.FederationName, target core.FederateHandle, apply func(*federateSnapshot)) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, ok := m.fed[fed]
	if !ok || st.federates[target] == nil {
		return fmt.Errorf("HLAfederate: target %d is not joined to federation %q", target, fed)
	}
	apply(st.federates[target])
	return nil
}

func handleFederationSetSwitches(
	_ context.Context,
	dctx DispatchContext,
	_ core.FederateHandle,
	params map[core.ParameterHandle][]byte,
) ([]ResponseInteraction, error) {
	// HLAautoProvide — federation-wide.
	if b := findParam(dctx, ClassFederationSetSwitches, "HLAautoProvide", params); b != nil {
		v, present, err := decodeHLAswitch(b)
		if err != nil {
			return nil, fmt.Errorf("HLAautoProvide: %w", err)
		}
		if present {
			dctx.MOM.SetAutoProvideSwitch(dctx.Federation, v)
		}
	}
	return nil, nil
}

func handleFederateSetSwitches(
	_ context.Context,
	dctx DispatchContext,
	_ core.FederateHandle,
	params map[core.ParameterHandle][]byte,
) ([]ResponseInteraction, error) {
	target, err := decodeSwitchTarget(dctx, ClassFederateSetSwitches, params)
	if err != nil {
		return nil, err
	}
	regions, hasRegions, err := decodeHLAswitch(findParam(dctx, ClassFederateSetSwitches, "HLAconveyRegionDesignatorSets", params))
	if err != nil {
		return nil, fmt.Errorf("HLAconveyRegionDesignatorSets: %w", err)
	}
	producer, hasProducer, err := decodeHLAswitch(findParam(dctx, ClassFederateSetSwitches, "HLAconveyProducingFederate", params))
	if err != nil {
		return nil, fmt.Errorf("HLAconveyProducingFederate: %w", err)
	}
	// Validate the entire interaction before changing either switch.
	return nil, dctx.MOM.adjustFederateSwitches(dctx.Federation, target, func(fs *federateSnapshot) {
		if hasRegions {
			fs.conveyRegionDesignatorSetsSwitch = regions
		}
		if hasProducer {
			fs.conveyProducingFederateSwitch = producer
		}
	})
}

// The MIM declares HLAreportingState as HLAboolean, whose four-byte
// 0/1 representation matches HLAswitch. Reporting/failure interactions
// and the service-report subscription exclusion are separate obligations.

func handleSetServiceReporting(
	ctx context.Context,
	dctx DispatchContext,
	_ core.FederateHandle,
	params map[core.ParameterHandle][]byte,
) ([]ResponseInteraction, error) {
	// Preserve the historical omitted-value contract for legacy callers without
	// reporting routes. Fully wired dispatch requires the MIM parameter.
	extra := []string{"HLAreportingState"}
	if dctx.Catalog.ReportRecipients == nil && findParam(dctx, ClassSetServiceReporting, "HLAreportingState", params) == nil {
		extra = nil
	}
	target, err := catalogTarget(dctx, ClassSetServiceReporting, params, extra...)
	if err != nil {
		return nil, err
	}
	v, present, err := decodeHLAswitch(findParam(dctx, ClassSetServiceReporting, "HLAreportingState", params))
	if err != nil {
		return nil, malformed("HLAreportingState: %v", err)
	}
	if v && dctx.Catalog.Subscriptions == nil && dctx.Catalog.ReportRecipients != nil {
		return nil, fmt.Errorf("mom: subscription state required for service reporting")
	}
	if v && dctx.Catalog.Subscriptions != nil {
		if err := validateServiceReportingSubscriptions(ctx, dctx, target); err != nil {
			return nil, err
		}
	}
	return nil, dctx.MOM.adjustFederateSwitches(dctx.Federation, target, func(fs *federateSnapshot) {
		if present {
			fs.serviceReportingEnabled = v
		}
	})
}

func handleSetExceptionReporting(
	_ context.Context,
	dctx DispatchContext,
	_ core.FederateHandle,
	params map[core.ParameterHandle][]byte,
) ([]ResponseInteraction, error) {
	if dctx.Catalog.ReportRecipients != nil {
		if err := validateParameters(dctx, ClassSetExceptionReporting, params, "HLAfederate", "HLAreportingState"); err != nil {
			return nil, err
		}
	}
	target, err := decodeSwitchTarget(dctx, ClassSetExceptionReporting, params)
	if err != nil {
		return nil, err
	}
	v, present, err := decodeHLAswitch(findParam(dctx, ClassSetExceptionReporting, "HLAreportingState", params))
	if err != nil {
		return nil, malformed("HLAreportingState: %v", err)
	}
	return nil, dctx.MOM.adjustFederateSwitches(dctx.Federation, target, func(fs *federateSnapshot) {
		if present {
			fs.exceptionReportingEnabled = v
		}
	})
}
func validateServiceReportingSubscriptions(ctx context.Context, dctx DispatchContext, target core.FederateHandle) error {
	state, err := dctx.Catalog.Subscriptions(ctx, dctx.Federation, target)
	if err != nil {
		return err
	}
	class, ok := dctx.FOM.LookupInteractionClass(ClassReportServiceInvocation)
	if !ok {
		return fmt.Errorf("mom: service report class unavailable")
	}
	classes := []core.InteractionClassHandle{class}
	if ancestry, ok := dctx.FOM.(core.FOMClassAncestry); ok {
		classes = ancestry.InteractionClassAncestors(class)
	}
	for _, sub := range state.Interactions {
		if slices.Contains(classes, sub.Class) {
			return fmt.Errorf("mom: service reporting cannot be enabled for a service-report subscriber")
		}
	}
	return nil
}
