"""TASK-270 (M23 W6) — Python-side acceptance gate for AC §3.

Surface introspection: pysdk Federate exposes the 6 new §6 methods,
DDMClient exposes the 6 new §9 methods, typed exceptions are present.
"""

from __future__ import annotations

import pytest

from rti1516e import _grpc_errors as ge
from rti1516e import ddm
from rti1516e.connection import Federate
from rti1516e.standard import Rti1516eAmbassador


@pytest.mark.spec
@pytest.mark.parametrize(
    "name",
    [
        # W1 + W2 + W3 — §6 additions
        "delete_object_instance",
        "local_delete_object_instance",
        "request_attribute_value_update",
        "request_class_attribute_value_update",
        "change_attribute_transportation_type",
        "change_interaction_transportation_type",
    ],
)
def test_ac_3_object_federate_method_present(name: str) -> None:
    """AC §3.x — pysdk Federate exposes the 6 new §6 methods."""
    assert hasattr(Federate, name), f"Federate is missing {name!r}"


@pytest.mark.spec
@pytest.mark.parametrize(
    "name",
    [
        "deleteObjectInstance",
        "localDeleteObjectInstance",
        "requestAttributeValueUpdate",
        "requestClassAttributeValueUpdate",
        "changeAttributeTransportationType",
        "changeInteractionTransportationType",
    ],
)
def test_ac_3_object_ambassador_method_present(name: str) -> None:
    """AC §3.x — Rti1516eAmbassador exposes the 6 new §6 camelCase methods."""
    assert hasattr(Rti1516eAmbassador, name), f"Rti1516eAmbassador is missing {name!r}"


@pytest.mark.spec
@pytest.mark.parametrize(
    "name",
    [
        # W5 — §9 additions
        "associate_regions_for_updates",
        "unassociate_regions_for_updates",
        "unsubscribe_object_class_attributes_with_regions",
        "unsubscribe_interaction_class_with_regions",
        "send_interaction_with_regions",
        "request_attribute_value_update_with_regions",
    ],
)
def test_ac_3_ddm_client_method_present(name: str) -> None:
    """AC §3.x — pysdk DDMClient exposes the 6 new §9 methods."""
    assert hasattr(ddm.DDMClient, name), f"DDMClient is missing {name!r}"


@pytest.mark.spec
def test_ac_3_object_typed_exceptions_present() -> None:
    """AC §3.x — typed exceptions for the M23 §6 errors."""
    expected = {
        "ObjectNotOwned",
        "AttributeNotPublishedByFederation",
        "ObjectAlreadyDeleted",
        "TransportTypeUnspecified",
    }
    actual = {n for n in dir(ge) if not n.startswith("_")}
    missing = expected - actual
    assert not missing, f"typed exceptions missing: {missing}"
    # Codes 710-713 continue M22's 700-709 range.
    assert ge.ObjectNotOwned.error_code == 710
    assert ge.AttributeNotPublishedByFederation.error_code == 711
    assert ge.ObjectAlreadyDeleted.error_code == 712
    assert ge.TransportTypeUnspecified.error_code == 713
