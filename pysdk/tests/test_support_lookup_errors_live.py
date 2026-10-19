"""Real-server exception contracts for IEEE 2010 Support name lookups."""

from __future__ import annotations

from contextlib import ExitStack

import pytest

from rti1516e import Rti1516eAmbassador, errors
from tests.spec.m25.test_support_service import _FOM_XML
from tests.test_ieee2010_independent_contract_live import (
    independent_artifacts,  # noqa: F401 - imported pytest fixture
)
from tests.test_ieee2010_independent_contract_live import (
    independent_server as _independent_server,  # noqa: F401 - named pytest fixture
)


@pytest.fixture
def lookup_member(independent_server):
    url, _, _, run = independent_server
    model = run / "lookup.xml"
    model.write_text(_FOM_XML, encoding="utf-8")
    with ExitStack() as cleanup:
        rti = Rti1516eAmbassador()
        rti.connect(rti, url)
        cleanup.callback(rti.disconnect)
        rti.createFederationExecution("support-lookup-errors", [str(model)])
        cleanup.callback(rti.destroyFederationExecution, "support-lookup-errors")
        rti.joinFederationExecution("lookup-reader", "support-lookup-errors")
        cleanup.callback(rti.resignFederationExecution, "NO_ACTION")
        yield rti


def _lookup(rti, service, missing):
    absent = "DefinitelyAbsentLookupProbe"
    if service == "object":
        return rti.getObjectClassHandle(absent if missing else "HLAobjectRoot.Vehicle")
    if service == "attribute":
        cls = rti.getObjectClassHandle("HLAobjectRoot.Vehicle")
        return rti.getAttributeHandle(cls, absent if missing else "Position")
    if service == "interaction":
        return rti.getInteractionClassHandle(absent if missing else "HLAinteractionRoot.Honk")
    if service == "parameter":
        cls = rti.getInteractionClassHandle("HLAinteractionRoot.Honk")
        return rti.getParameterHandle(cls, absent if missing else "Volume")
    if service == "dimension":
        return rti.getDimensionHandle(absent if missing else "X")
    raise AssertionError(service)


def _assert_exact_wire_error(caught, expected):
    assert type(caught.value) is expected
    cause = caught.value.__cause__
    assert cause is not None
    assert dict(cause.trailing_metadata() or {}).get("rti-spec-exception") == expected.__name__


@pytest.mark.parametrize(
    "service", ["object", "attribute", "interaction", "parameter", "dimension"]
)
def test_missing_lookup_names_preserve_standard_exception_and_state(lookup_member, service):
    rti = lookup_member
    before = _lookup(rti, service, False)
    with pytest.raises(errors.RtiError) as caught:
        _lookup(rti, service, True)
    _assert_exact_wire_error(caught, errors.NameNotFound)
    assert _lookup(rti, service, False) == before


@pytest.mark.parametrize("handle", [0, 2**63 - 1])
@pytest.mark.parametrize("service", ["attribute", "parameter"])
def test_invalid_class_is_not_reported_as_missing_name(lookup_member, service, handle):
    rti = lookup_member
    before = _lookup(rti, service, False)
    with pytest.raises(errors.RtiError) as caught:
        if service == "attribute":
            rti.getAttributeHandle(handle, "Position")
        else:
            rti.getParameterHandle(handle, "Volume")
    expected = (
        errors.InvalidObjectClassHandle
        if service == "attribute"
        else errors.InvalidInteractionClassHandle
    )
    _assert_exact_wire_error(caught, expected)
    assert _lookup(rti, service, False) == before
