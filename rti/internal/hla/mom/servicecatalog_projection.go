package mom

import (
	"fmt"
	"slices"
	"strings"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func predefinedManagerParameters(leaf string) ([]string, bool) {
	for _, def := range serviceDefinitions {
		if leaf == ServiceClassPrefix+def.leaf {
			return append(append([]string{"HLAfederate"}, def.required...), def.optional...), true
		}
	}
	switch leaf {
	case ClassFederationSetSwitches:
		return []string{"HLAautoProvide"}, true
	case ClassFederateSetSwitches:
		return []string{"HLAfederate", "HLAconveyRegionDesignatorSets", "HLAconveyProducingFederate"}, true
	case ClassSetServiceReporting, ClassSetExceptionReporting:
		return []string{"HLAfederate", "HLAreportingState"}, true
	case ClassSetTiming:
		return []string{"HLAfederate", "HLAreportPeriod"}, true
	case ClassModifyAttributeState:
		return []string{"HLAfederate", "HLAobjectInstance", "HLAattribute", "HLAattributeState"}, true
	case ClassRequestObjectInstanceInformation:
		return []string{"HLAfederate", "HLAobjectInstance"}, true
	case ClassRequestFederateFOMModuleData:
		return []string{"HLAfederate", "HLAFOMmoduleIndicator"}, true
	case ClassRequestFederationFOMModuleData:
		return []string{"HLAFOMmoduleIndicator"}, true
	case ClassRequestSynchronizationPointStatus, ClassRequestSynchronizationPoints, ClassRequestMIMData:
		return nil, true
	case ClassRequestSubscriptions, ClassRequestPublications, ClassRequestObjectInstancesThatCanBeDeleted, ClassRequestObjectInstancesUpdated, ClassRequestObjectInstancesReflected, ClassRequestUpdatesSent, ClassRequestInteractionsSent, ClassRequestReflectionsReceived, ClassRequestInteractionsReceived:
		return []string{"HLAfederate"}, true
	}
	return nil, false
}

func projectManagerParameters(dctx DispatchContext, sent, leaf string, params map[core.ParameterHandle][]byte) (map[core.ParameterHandle][]byte, error) {
	names, predefined := predefinedManagerParameters(leaf)
	if !predefined {
		return params, nil
	}
	if dctx.FOM == nil {
		return params, nil
	}
	sent = strings.TrimPrefix(sent, "HLAinteractionRoot.")
	sentHandle, ok := dctx.FOM.LookupInteractionClass(sent)
	if !ok {
		return nil, core.ErrInteractionClassNotFound
	}
	leafHandle, ok := dctx.FOM.LookupInteractionClass(leaf)
	if !ok {
		return nil, core.ErrInteractionClassNotFound
	}
	if sent != leaf {
		hierarchy, ok := dctx.FOM.(core.FOMClassAncestry)
		if !ok || !slices.Contains(hierarchy.InteractionClassAncestors(sentHandle), leafHandle) {
			return nil, fmt.Errorf("MOM extension is not a FOM descendant of the subscribed leaf")
		}
	}
	known, err := managerParameterMapping(dctx.FOM, sentHandle, leafHandle, names)
	if err != nil {
		return nil, err
	}
	result := make(map[core.ParameterHandle][]byte)
	for h, value := range params {
		if mapped, ok := known[h]; ok {
			result[mapped] = value
			continue
		}
		if dctx.FOMNames == nil {
			return nil, malformed("MOM extension parameter validation unavailable")
		}
		if _, valid := dctx.FOMNames.ParameterName(sentHandle, h); !valid {
			return nil, malformed("MOM interaction contains an undefined parameter")
		}
	}
	return result, nil
}
func managerParameterMapping(fom core.FOMHandle, sentHandle, leafHandle core.InteractionClassHandle, names []string) (map[core.ParameterHandle]core.ParameterHandle, error) {
	known := map[core.ParameterHandle]core.ParameterHandle{}
	for _, name := range names {
		from, ok := fom.LookupParameter(sentHandle, name)
		if !ok {
			if name == "HLAfederate" {
				return nil, malformed("HLAfederate: unresolved target parameter")
			}
			continue
		}
		to, ok := fom.LookupParameter(leafHandle, name)
		if !ok {
			if name == "HLAfederate" {
				return nil, malformed("HLAfederate: unresolved target parameter")
			}
			continue
		}
		known[from] = to
	}
	return known, nil
}
