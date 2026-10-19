"""Source-derived FOM admission, error mapping and atomic rejection checks.

S1516.2 3.3.1 (PDF pages 31-32), S1516.1 4.1.4 / 4.1.4.1 (pages 47-48).
These cases do not cover every standard FOM admission condition.
"""

import asyncio
import os
from contextlib import AsyncExitStack
from pathlib import Path

import grpc
import pytest

from rti1516e import FederationSpec, Rti1516eAmbassador, RtiConnection
from rti1516e._grpc_errors import translate_rpc_error
from rti1516e.errors import ErrorReadingFDD, FomValidationFailed
from tests.test_ieee2010_independent_contract_live import (
    _write_module,
    independent_artifacts,  # noqa: F401 - pytest fixture dependency
)
from tests.test_ieee2010_independent_contract_live import (
    independent_server as _independent_server,  # noqa: F401 - named pytest fixture
)

pytestmark = pytest.mark.skipif(
    os.environ.get("GORTI_RUN_LIVE_E2E") != "1",
    reason="Set GORTI_RUN_LIVE_E2E=1 and select the pinned GORTI_RTID binary.",
)


def test_valid_name_control_can_create_and_destroy(independent_server):
    endpoint, base, _, _ = independent_server
    rti = Rti1516eAmbassador()
    rti.connect(rti, endpoint)
    try:
        rti.createFederationExecution("valid-control", [str(base)])
        rti.destroyFederationExecution("valid-control")
    finally:
        rti.disconnect()


@pytest.mark.parametrize(
    "local_name",
    ["1Sensor", "Sensor:Bad", "Sensor Bad", "HLAcustom", "NA"],
    ids=["leading-digit", "colon", "space", "reserved-hla", "reserved-na"],
)
def test_noncompliant_local_class_name_rejected_at_create(independent_server, local_name):
    endpoint, base, _, run = independent_server
    path = Path(run) / "invalid-name.xml"
    _write_module(path, local_name)
    rti = Rti1516eAmbassador()
    rti.connect(rti, endpoint)
    accepted = False
    try:
        try:
            rti.createFederationExecution("name-probe", [str(path)])
        except FomValidationFailed as error:
            assert type(error) is ErrorReadingFDD
            # Reuse the same name: rejection must not leave an execution behind.
            rti.createFederationExecution("name-probe", [str(base)])
            rti.destroyFederationExecution("name-probe")
            return
        accepted = True
    finally:
        if accepted:
            rti.destroyFederationExecution("name-probe")
        rti.disconnect()
    pytest.fail(f"Server admitted noncompliant user-defined local class name: {local_name!r}")


@pytest.mark.parametrize("invalid_first", [True, False], ids=["invalid-first", "invalid-last"])
def test_invalid_join_module_leaves_roster_and_complete_fdd_unchanged(
    independent_server, invalid_first
):
    endpoint, base, _, run = independent_server
    valid, invalid = Path(run) / "valid-extension.xml", Path(run) / "invalid-extension.xml"
    _write_module(valid, "IndependentCandidate")
    _write_module(invalid, "HLAcustom")

    async def exercise():
        from rti.v1 import common_pb2, federation_pb2

        async with asyncio.timeout(30), AsyncExitStack() as stack:
            owner = await stack.enter_async_context(RtiConnection.connect(endpoint))
            member = await stack.enter_async_context(
                owner.join_federation(
                    FederationSpec("name-join", [str(base)]), federate_name="owner"
                )
            )
            request = federation_pb2.GetFOMViewRequest(
                wire_version=1, federation_name="name-join", federate_handle=member.handle
            )
            metadata = owner.transport.session_metadata(
                owner.transport.session_token(member.handle)
            )
            before = await owner.transport.federation.GetFOMView(request, metadata=metadata)
            roster = (await member.mom.query_federation_attributes()).federate_handles
            modules = [invalid, valid] if invalid_first else [valid, invalid]
            join = federation_pb2.JoinFederationRequest(
                wire_version=1,
                federation_name="name-join",
                federate_name="extender",
                expected_federation_generation=owner.transport._generation_by_federation[
                    "name-join"
                ],
                additional_fom_modules=[
                    common_pb2.FOMModule(path=p.name, xml=p.read_bytes()) for p in modules
                ],
            )
            with pytest.raises(FomValidationFailed) as caught:
                try:
                    await owner.transport.federation.JoinFederation(join)
                except grpc.RpcError as error:
                    translate_rpc_error(error)
            assert type(caught.value) is ErrorReadingFDD
            after = await owner.transport.federation.GetFOMView(request, metadata=metadata)
            assert after == before
            assert (await member.mom.query_federation_attributes()).federate_handles == roster
            # Rejected modules/name cannot prevent a subsequent valid extension.
            entrant = await stack.enter_async_context(RtiConnection.connect(endpoint))
            joined = await stack.enter_async_context(
                entrant.join_federation(
                    FederationSpec("name-join", additional_fom_modules=[str(valid)]),
                    federate_name="extender",
                )
            )
            assert await joined.support.get_object_class_handle(
                "HLAobjectRoot.IndependentCandidate"
            )
            assert len((await member.mom.query_federation_attributes()).federate_handles) == 2

    asyncio.run(exercise())
