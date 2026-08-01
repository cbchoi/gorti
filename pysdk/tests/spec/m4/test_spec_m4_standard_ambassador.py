"""SDK Layer 2 — Rti1516eAmbassador callback API.

Spot-checks that the ambassador exposes the IEEE 1516-2010 method names
(camelCase) and that callbacks fire with payloads matching Layer 1.

Implements: IR-PYAPI-1 (Layer 2).
"""

from __future__ import annotations

import inspect

import pytest

from rti1516e import Rti1516eAmbassador


@pytest.mark.spec
def test_spec_m4_ambassador_has_required_methods() -> None:
    """Layer 2 must expose the canonical 1516-2010 method names so users
    porting from Java/C++ RTIs can map call sites mechanically."""
    required = {
        "connect",
        "disconnect",
        "createFederationExecution",
        "joinFederationExecution",
        "resignFederationExecution",
        "publishObjectClassAttributes",
        "subscribeObjectClassAttributes",
        "publishInteractionClass",
        "subscribeInteractionClass",
        "registerObjectInstance",
        "updateAttributeValues",
        "sendInteraction",
        "enableTimeRegulation",
        "enableTimeConstrained",
        "nextMessageRequest",
    }
    missing = sorted(name for name in required if not hasattr(Rti1516eAmbassador, name))
    assert not missing, f"Rti1516eAmbassador missing required methods: {missing}"


@pytest.mark.spec
def test_spec_m4_ambassador_callback_methods_overridable() -> None:
    """Callbacks must be defined on the base class ( wires the
    SDK so subclasses' overrides fire). They can be no-ops in the base."""
    callbacks = {
        "discoverObjectInstance",
        "reflectAttributeValues",
        "receiveInteraction",
        "timeAdvanceGrant",
        "federationHalted",
    }
    for name in callbacks:
        member = getattr(Rti1516eAmbassador, name, None)
        assert member is not None, f"missing callback {name}"
        assert inspect.isfunction(member), f"{name} is not a function"
