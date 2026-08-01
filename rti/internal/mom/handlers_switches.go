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
// Encoding (IEEE 1516.2-2010 §B.2.2 HLAswitch): a single octet,
// 0 = HLAfalse, 1 = HLAtrue. Other values are rejected.

package mom

import (
	"context"
	"fmt"

	"github.com/cbchoi/gorti/rti/internal/core"
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

// decodeHLAswitch reads a 1-byte HLAswitch encoding: 0 → false,
// 1 → true. Returns (value, present, err). Absent or empty bytes
// yield present=false.
func decodeHLAswitch(b []byte) (bool, bool, error) {
	if len(b) == 0 {
		return false, false, nil
	}
	if len(b) != 1 {
		return false, false, fmt.Errorf("HLAswitch: want 1 byte, got %d", len(b))
	}
	switch b[0] {
	case 0:
		return false, true, nil
	case 1:
		return true, true, nil
	default:
		return false, false, fmt.Errorf("HLAswitch: invalid value %d", b[0])
	}
}

// findParam returns the encoded bytes of a named parameter, or nil
// if the sender didn't include it. The dispatcher uses
// FOMHandleNameLookup.LookupParameter to resolve the name to a
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
	return params[h]
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
	sender core.FederateHandle,
	params map[core.ParameterHandle][]byte,
) ([]ResponseInteraction, error) {
	if b := findParam(dctx, ClassFederateSetSwitches, "HLAconveyRegionDesignatorSets", params); b != nil {
		v, present, err := decodeHLAswitch(b)
		if err != nil {
			return nil, fmt.Errorf("HLAconveyRegionDesignatorSets: %w", err)
		}
		if present {
			dctx.MOM.SetConveyRegionDesignatorSetsSwitch(dctx.Federation, sender, v)
		}
	}
	if b := findParam(dctx, ClassFederateSetSwitches, "HLAconveyProducingFederate", params); b != nil {
		v, present, err := decodeHLAswitch(b)
		if err != nil {
			return nil, fmt.Errorf("HLAconveyProducingFederate: %w", err)
		}
		if present {
			dctx.MOM.SetConveyProducingFederateSwitch(dctx.Federation, sender, v)
		}
	}
	return nil, nil
}

// HLAsetServiceReporting + HLAsetExceptionReporting (M20.4) — each
// carries a single HLAreportingState parameter (HLAswitch). They're
// distinct interactions from HLAsetSwitches because IEEE 1516.1
// §10.4 expects the per-toggle interactions to also fire a
// confirmation HLAreport* (M20.6 wires the confirmations).

func handleSetServiceReporting(
	_ context.Context,
	dctx DispatchContext,
	sender core.FederateHandle,
	params map[core.ParameterHandle][]byte,
) ([]ResponseInteraction, error) {
	b := findParam(dctx, ClassSetServiceReporting, "HLAreportingState", params)
	if b == nil {
		return nil, nil
	}
	v, present, err := decodeHLAswitch(b)
	if err != nil {
		return nil, fmt.Errorf("HLAreportingState: %w", err)
	}
	if present {
		dctx.MOM.SetServiceReporting(dctx.Federation, sender, v)
	}
	return nil, nil
}

func handleSetExceptionReporting(
	_ context.Context,
	dctx DispatchContext,
	sender core.FederateHandle,
	params map[core.ParameterHandle][]byte,
) ([]ResponseInteraction, error) {
	b := findParam(dctx, ClassSetExceptionReporting, "HLAreportingState", params)
	if b == nil {
		return nil, nil
	}
	v, present, err := decodeHLAswitch(b)
	if err != nil {
		return nil, fmt.Errorf("HLAreportingState: %w", err)
	}
	if present {
		dctx.MOM.SetExceptionReporting(dctx.Federation, sender, v)
	}
	return nil, nil
}
