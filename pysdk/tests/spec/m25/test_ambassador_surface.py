"""M25 Phase C — Layer-2 Rti1516eAmbassador surface contract.

Asserts every IEEE 1516 service-style method this milestone promises is present
on the ambassador class as a callable. Does NOT exercise the wire —
the corresponding service-module tests already do that. The point
here is to lock the surface so a future refactor can't silently
remove a method a ported IEEE 1516 federate relies on.
"""

from __future__ import annotations

import inspect

import pytest

from rti1516e.standard import Rti1516eAmbassador

# IEEE 1516 service-style method names the M25 ambassador must expose. Grouped
# by spec section for navigability. If you remove a name here, also
# remove it from standard.py; if you add a name to standard.py for
# cross-implementation parity, append it here so the surface is locked.
IEEE1516_SURFACE_METHODS = [
    # §10.2 Support services (M25 Phase B)
    "getObjectClassHandle", "getObjectClassName",
    "getAttributeHandle", "getAttributeName",
    "getInteractionClassHandle", "getInteractionClassName",
    "getParameterHandle", "getParameterName",
    "getDimensionHandle", "getDimensionName", "getDimensionUpperBound",
    "getOrderType", "getOrderName",
    "getTransportationType", "getTransportationName",
    # §4.11-4.13 Sync points (M25 Phase C)
    "registerFederationSynchronizationPoint",
    "synchronizationPointAchieved",
    # §7 Ownership (M25 Phase C)
    "unconditionalAttributeOwnershipDivestiture",
    "negotiatedAttributeOwnershipDivestiture",
    "attributeOwnershipAcquisition",
    "cancelNegotiatedAttributeOwnershipDivestiture",
    "cancelAttributeOwnershipAcquisition",
    "attributeOwnershipDivestitureIfWanted",
    "queryAttributeOwnership",
    "isAttributeOwnedByFederate",
    # §4.8-4.15 Save / Restore (M25 Phase C)
    "requestFederationSave",
    "federateSaveComplete", "federateSaveNotComplete",
    "queryFederationSaveStatus",
    "requestFederationRestore",
    "federateRestoreComplete",
    "queryFederationRestoreStatus",
    # §9 DDM (M25 Phase C)
    "createRegion", "setRangeBounds",
    "commitRegionModifications", "deleteRegion",
    "subscribeObjectClassAttributesWithRegions",
    "subscribeInteractionClassWithRegions",
    "registerObjectInstanceWithRegions",
    "associateRegionsForUpdates", "unassociateRegionsForUpdates",
    "unsubscribeObjectClassAttributesWithRegions",
    "unsubscribeInteractionClassWithRegions",
    "sendInteractionWithRegions",
    "requestAttributeValueUpdateWithRegions",
    # §10.4 Callback evocation + enable/disable (M26 Phase E, M27 Phase C)
    "evokeCallback", "evokeMultipleCallbacks",
    "enableCallbacks", "disableCallbacks",
    # §6.1-6.5 reservation (M26 Phase F)
    "reserveObjectInstanceName", "releaseObjectInstanceName",
    "reserveMultipleObjectInstanceNames",
    # §6.30-6.31 runtime instance handle services (M27 Phase C)
    "getObjectInstanceHandle", "getObjectInstanceName",
    # §11 MOM (M27 Phase D)
    "queryFederationAttributes", "queryFederateAttributes",
    "enumerateMomInstances",
]


@pytest.mark.spec
@pytest.mark.parametrize("method_name", IEEE1516_SURFACE_METHODS)
def test_spec_m25_ambassador_exposes_ieee1516_method(method_name: str) -> None:
    """Each IEEE 1516 service-style method exists on Rti1516eAmbassador as a callable."""
    attr = getattr(Rti1516eAmbassador, method_name, None)
    assert attr is not None, f"Rti1516eAmbassador is missing {method_name}"
    assert callable(attr), f"Rti1516eAmbassador.{method_name} exists but is not callable"


@pytest.mark.spec
def test_spec_m25_ambassador_methods_are_instance_bound() -> None:
    """All IEEE 1516 service-style methods take self as first arg (instance methods, not
    classmethods or staticmethods)."""
    for name in IEEE1516_SURFACE_METHODS:
        attr = getattr(Rti1516eAmbassador, name)
        sig = inspect.signature(attr)
        first = next(iter(sig.parameters), None)
        assert first == "self", (
            f"{name}: first parameter is {first!r}; want 'self' "
            "(instance methods only)"
        )
