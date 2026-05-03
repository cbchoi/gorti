"""SDK Layer 1 — async events() stream + typed exceptions per ErrorCode.

Implements: IR-PYAPI-1.
"""

from __future__ import annotations

import pytest

from rti1516e import (
    FederationSpec,
    RtiConnection,
    TimeAdvanceGrant,
)
from rti1516e.errors import ERROR_CODE_TO_EXCEPTION, RtiError


@pytest.mark.spec
def test_spec_m4_every_proto_error_code_has_exception() -> None:
    """Every numeric ErrorCode in proto/rti/v1/errors.proto must map to a
    concrete RtiError subclass. The mapping table is in rti1516e.errors;
    the proto file is the source of truth."""
    # Hardcoded set of expected codes mirrors proto/rti/v1/errors.proto.
    # If proto adds a new code, this test fails until rti1516e.errors is
    # extended to match.
    expected_codes = {
        1, 2, 3, 4, 5, 6, 7,            # FED_*
        100, 101,                        # FOM_*
        200, 201, 202, 203, 204, 205, 206,  # OBJ_*
        300, 301, 302, 303, 304, 305,    # TIME_*
        400, 401, 402,                   # ENC_*
        500, 501,                        # WIRE_*
    }
    for code in expected_codes:
        assert code in ERROR_CODE_TO_EXCEPTION, f"missing exception for code {code}"
        cls = ERROR_CODE_TO_EXCEPTION[code]
        assert issubclass(cls, RtiError), f"{cls!r} is not RtiError"
        assert cls.error_code == code, f"{cls.__name__}.error_code = {cls.error_code} != {code}"


@pytest.mark.spec
@pytest.mark.asyncio
async def test_spec_m4_time_advance_grant_yielded(fake_rti) -> None:  # type: ignore[no-untyped-def]
    async with RtiConnection.connect("memory://fake-rti") as rti:
        async with rti.join_federation(FederationSpec(name="demo"), federate_name="alice") as fed:
            await fed.enable_time_regulation(lookahead=1.0)
            await fed.next_message_request(time=5.0)
            fake_rti.push_event(fed.handle, TimeAdvanceGrant(time=5.0))
            async for evt in fed.events():
                assert isinstance(evt, TimeAdvanceGrant)
                assert evt.time == 5.0
                break
