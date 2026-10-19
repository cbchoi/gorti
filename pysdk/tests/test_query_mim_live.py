"""R7 production-path evidence; bounded SDK behavior, not full conformance."""

import asyncio
import struct
import xml.etree.ElementTree as ET
from contextlib import AsyncExitStack
from pathlib import Path

import grpc
import pytest

from rti1516e import FederationSpec, MIMModule, RtiConnection
from rti1516e.errors import RtiError
from rti1516e.events import (
    FederationRestored,
    FederationSaved,
    InitiateFederateRestore,
    InitiateFederateSave,
    ReceiveInteraction,
)
from tests.test_live_service_e2e import LiveCallbacks, live_artifacts  # noqa: F401 - pytest fixture
from tests.test_live_service_e2e import live_server as _live_server  # noqa: F401 - named fixture

ROOT = Path(__file__).resolve().parents[2]
MIM = ROOT / "rti/pkg/fom/mim/standard-mim.xml"
REPORT = "HLAinteractionRoot.HLAmanager.HLAfederate.HLAreport.HLAreportServiceInvocation"
REPORT_SWITCH = "HLAinteractionRoot.HLAmanager.HLAfederate.HLAadjust.HLAsetServiceReporting"


def _unicode(data):
    count = struct.unpack(">I", data[:4])[0]
    assert len(data) == 4 + 2 * count
    return data[4:].decode("utf-16-be")


def test_live_session_queries_report_only_authenticated_caller(live_server):
    url, base, _ = live_server

    async def exercise():
        async with asyncio.timeout(50), AsyncExitStack() as stack:
            left = await stack.enter_async_context(RtiConnection.connect(url))
            right = await stack.enter_async_context(RtiConnection.connect(url))
            spec = FederationSpec("query-caller", [str(base)])
            caller = await stack.enter_async_context(
                left.join_federation(spec, federate_name="caller")
            )
            observer = await stack.enter_async_context(
                right.join_federation(spec, federate_name="observer")
            )
            await observer.subscribe_interaction_class(REPORT)
            log = LiveCallbacks(observer)
            stack.push_async_callback(log.close)
            await caller.publish_interaction_class(REPORT_SWITCH)
            await caller.send_interaction(
                REPORT_SWITCH,
                {
                    "HLAfederate": struct.pack(">IQ", 8, caller.handle),
                    "HLAreportingState": struct.pack(">I", 1),
                },
            )
            report_handle = await observer.support.get_interaction_class_handle(REPORT)
            for name in ("HLAfederate", "HLAservice", "HLAsuccessIndicator"):
                assert await observer.support.get_parameter_handle(report_handle, name) > 0

            def reports(after, name):
                return [
                    event
                    for event in log.events[after:]
                    if isinstance(event, ReceiveInteraction)
                    and _unicode(event.parameters["HLAservice"]) == name
                ]

            async def expect(name, call, *, failure=False):
                mark = len(log.events)
                if failure:
                    with pytest.raises(Exception, match="[Oo]rder"):
                        await call()
                else:
                    await call()
                event = await log.wait(
                    ReceiveInteraction,
                    lambda e: _unicode(e.parameters["HLAservice"]) == name,
                    after=mark,
                )
                assert event.parameters["HLAfederate"] == struct.pack(">IQ", 8, caller.handle)
                assert event.parameters["HLAsuccessIndicator"] == struct.pack(">I", not failure)

            await expect("getOrderType", lambda: caller.support.get_order_type("Receive"))
            await expect(
                "getObjectClassHandle", lambda: caller.support.get_object_class_handle("Vehicle")
            )
            await expect("queryFederationSaveStatus", caller.savepoint.query_federation_save_status)
            await expect(
                "queryFederationRestoreStatus", caller.savepoint.query_federation_restore_status
            )
            await expect(
                "getOrderType", lambda: caller.support.get_order_type("invalid-order"), failure=True
            )

            # The signed barrier report is ordered after the unsigned/stale calls
            # on the observer stream, so absence is not inferred from a sleep.
            mark = len(log.events)
            support = caller._support_client
            original = support._session_token
            try:
                for invalid in (b"", b"not-a-valid-live-session"):
                    support._session_token = invalid
                    assert await caller.support.get_order_type("Receive") == 1
            finally:
                support._session_token = original
            await expect("getOrderName", lambda: caller.support.get_order_name(1))
            assert reports(mark, "getOrderType") == []

    asyncio.run(exercise())


@pytest.mark.parametrize("source", ["path", "inline"], ids=["path", "inline"])
def test_live_selected_mim_join_mom_and_restore(live_server, source):
    url, base, _ = live_server
    root = ET.parse(MIM).getroot()  # noqa: S314 - checked-in, trusted MIM fixture
    semantics = root.find(".//{http://standards.ieee.org/IEEE1516-2010}semantics")
    assert semantics is not None
    semantics.text = "R7 selected MIM descriptive variant."
    selected = MIM if source == "path" else MIMModule("selected-mim.xml", ET.tostring(root))
    designator = str(MIM) if source == "path" else selected.designator
    selected_bytes = MIM.read_bytes() if source == "path" else selected.xml

    async def exercise():
        async with asyncio.timeout(50), RtiConnection.connect(url) as connection:
            spec = FederationSpec("selected-mim", [str(base)], mim_module=selected)
            async with connection.join_federation(spec, federate_name="caller") as fed:
                transport = connection.transport
                assert transport._mim_module == MIMModule(designator, selected_bytes)
                assert transport._fom_cache.mim_module == transport._mim_module
                assert [path for path, _ in transport._fom_modules] == [str(base)]
                attrs = await fed.mom.query_federation_attributes()
                assert _unicode(attrs.attribute_values["HLAMIMdesignator"]) == designator
                assert designator not in attrs.fom_module_names
                log = LiveCallbacks(fed)
                try:
                    await fed.savepoint.request_federation_save("selected")
                    await log.wait(InitiateFederateSave)
                    await fed.savepoint.federate_save_begun()
                    await fed.savepoint.federate_save_complete()
                    await log.wait(FederationSaved)
                    mark = len(log.events)
                    await fed.savepoint.request_federation_restore("selected")
                    await log.wait(InitiateFederateRestore, after=mark)
                    await fed.savepoint.federate_restore_complete()
                    await log.wait(FederationRestored, after=mark)
                    assert transport._mim_module == MIMModule(designator, selected_bytes)
                    assert transport._fom_cache.mim_module == transport._mim_module
                    attrs = await fed.mom.query_federation_attributes()
                    assert _unicode(attrs.attribute_values["HLAMIMdesignator"]) == designator
                finally:
                    await log.close()

    asyncio.run(exercise())


def test_live_invalid_mim_rejected_without_creating_federation(live_server):
    url, base, _ = live_server

    async def exercise():
        async with asyncio.timeout(30), RtiConnection.connect(url) as connection:
            bad = MIMModule("invalid-mim.xml", b"<objectModel/>")
            # Some server errors retain their gRPC type instead of an SDK mapping.
            with pytest.raises((RtiError, grpc.RpcError), match="MIM validation"):
                await connection.create_federation(
                    FederationSpec("rejected-mim", [str(base)], mim_module=bad)
                )
            await connection.create_federation(FederationSpec("rejected-mim", [str(base)]))
            await connection.destroy_federation("rejected-mim")

    asyncio.run(exercise())


def test_live_mim_join_precondition_rejects_before_fom_or_roster_change(live_server):
    from rti.v1 import federation_pb2

    url, base, extra = live_server
    selected = MIMModule("selected.xml", MIM.read_bytes())

    async def exercise():
        async with asyncio.timeout(30), AsyncExitStack() as stack:
            owner = await stack.enter_async_context(RtiConnection.connect(url))
            other = await stack.enter_async_context(RtiConnection.connect(url))
            spec = FederationSpec("mim-precondition", [str(base)], mim_module=selected)
            fed = await stack.enter_async_context(
                owner.join_federation(spec, federate_name="owner")
            )
            request = federation_pb2.GetFOMViewRequest(
                wire_version=1, federation_name=spec.name, federate_handle=fed.handle
            )
            before = await owner.transport.federation.GetFOMView(request)
            rejected = FederationSpec(
                spec.name,
                additional_fom_modules=[str(extra)],
                mim_module=MIMModule("different.xml", selected.xml),
            )
            with pytest.raises(Exception, match="[Mm][Ii][Mm]"):
                async with other.join_federation(rejected, federate_name="rejected"):
                    pytest.fail("mismatched MIM joined")
            after = await owner.transport.federation.GetFOMView(request)
            assert after == before
            attrs = await fed.mom.query_federation_attributes()
            assert attrs.federate_handles == (fed.handle,)
            assert _unicode(attrs.attribute_values["HLAMIMdesignator"]) == selected.designator

    asyncio.run(exercise())
