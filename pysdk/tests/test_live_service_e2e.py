"""Opt-in production SDK checks: GORTI_RUN_LIVE_E2E=1.

Each test owns one bounded localhost rtid process. Builds, FOM fixtures,
save bundles, and raw logs live outside the repository.
"""

from __future__ import annotations

import asyncio
import os
import shutil
import socket
import struct
import subprocess
import tempfile
import time
from contextlib import AsyncExitStack
from pathlib import Path

import pytest

from rti1516e.connection import FederationSpec, RtiConnection
from rti1516e.events import (
    DiscoverObjectInstance,
    FederationRestored,
    FederationSaved,
    InitiateFederateRestore,
    InitiateFederateSave,
    OrderType,
    ReceiveInteraction,
    ReflectAttributeValues,
    TimeAdvanceGrant,
    TurnUpdatesOffForObjectInstance,
    TurnUpdatesOnForObjectInstance,
)

pytestmark = pytest.mark.skipif(
    os.environ.get("GORTI_RUN_LIVE_E2E") != "1", reason="production localhost tests are opt-in"
)
ROOT = Path(__file__).resolve().parents[2]
VEHICLE = "HLAobjectRoot.Vehicle"
BASE_SIGNAL = "HLAinteractionRoot.BaseSignal"
ADDED_SIGNAL = "HLAinteractionRoot.AddedSignal"


def _module(name: str, signal: str, *, objects: bool = False) -> str:
    object_xml = (
        """
    <objects><objectClass><name>HLAobjectRoot</name><objectClass>
      <name>Vehicle</name><sharing>PublishSubscribe</sharing><attribute>
        <name>Value</name><dataType>HLAfloat64BE</dataType>
        <updateType>Conditional</updateType><updateCondition>NA</updateCondition>
        <ownership>DivestAcquire</ownership><sharing>PublishSubscribe</sharing>
        <transportation>HLAreliable</transportation><order>Receive</order>
        <semantics>Test value.</semantics>
      </attribute></objectClass></objectClass></objects>"""
        if objects
        else ""
    )
    return f"""<?xml version="1.0" encoding="UTF-8"?>
    <objectModel xmlns="http://standards.ieee.org/IEEE1516-2010">
      <modelIdentification><name>{name}</name><type>FOM</type><version>1.0</version>
        <description>Production Python SDK regression fixture.</description></modelIdentification>
      {object_xml}
      <updateRates><updateRate><name>Slow</name><rate>2.5</rate></updateRate></updateRates>
      <interactions><interactionClass><name>HLAinteractionRoot</name>
        <interactionClass><name>{signal}</name><sharing>PublishSubscribe</sharing>
          <transportation>HLAreliable</transportation><order>Receive</order>
          <parameter><name>Payload</name><dataType>HLAfloat64BE</dataType>
            <semantics>Binary test payload.</semantics></parameter>
        </interactionClass></interactionClass></interactions>
    </objectModel>"""


@pytest.fixture(scope="module")
def live_artifacts():
    parent = Path(os.environ.get("GORTI_LIVE_ARTIFACTS", tempfile.gettempdir())).resolve()
    assert parent != ROOT and ROOT not in parent.parents, "raw artifacts must stay outside repo"
    parent.mkdir(parents=True, exist_ok=True)
    directory = Path(tempfile.mkdtemp(prefix="gorti-live-sdk-", dir=parent))
    binary = (
        Path(os.environ["GORTI_LIVE_RTID"])
        if "GORTI_LIVE_RTID" in os.environ
        else directory / ("rtid.exe" if os.name == "nt" else "rtid")
    )
    if "GORTI_LIVE_RTID" not in os.environ:
        go = shutil.which("go")
        assert go is not None, "Go is required unless GORTI_LIVE_RTID supplies a built server"
        env = os.environ.copy()
        env.setdefault("GOCACHE", str(ROOT.parent / ".gocache"))
        env.setdefault("GOMODCACHE", str(ROOT.parent / ".tools" / "gomodcache"))
        with (directory / "build.log").open("wb") as output:
            result = subprocess.run(  # noqa: S603 - explicit local tool, no shell
                [go, "build", "-o", str(binary), "./rti/cmd/rtid"],
                cwd=ROOT,
                env=env,
                stdout=output,
                stderr=subprocess.STDOUT,
                timeout=180,
                check=False,
            )
        assert result.returncode == 0, f"rtid build failed; see {directory / 'build.log'}"
    base, extra = directory / "base.xml", directory / "extra.xml"
    base.write_text(_module("PythonBase", "BaseSignal", objects=True), encoding="utf-8")
    extra.write_text(_module("PythonExtension", "AddedSignal"), encoding="utf-8")
    return directory, binary, base, extra


@pytest.fixture(name="live_server")
def live_server(live_artifacts, request):
    directory, binary, base, extra = live_artifacts
    run = directory / request.node.name
    run.mkdir()
    reservations = [socket.socket() for _ in range(2)]
    try:
        for listener in reservations:
            listener.bind(("127.0.0.1", 0))
        ports = [listener.getsockname()[1] for listener in reservations]
    finally:
        for listener in reservations:
            listener.close()
    env = os.environ.copy()
    env["GORTI_RESEARCH_CONFIG"] = ""
    with (run / "server.log").open("wb") as output:
        child = subprocess.Popen(  # noqa: S603 - owned server binary, no shell
            [
                str(binary),
                f"--listen=127.0.0.1:{ports[0]}",
                f"--admin-listen=127.0.0.1:{ports[1]}",
                "--metrics-listen=127.0.0.1:0",
                f"--save-dir={run / 'saves'}",
                "--audit-replay-plugin=none",
                "--log-dir=",
                "--log-level=warn",
            ],
            cwd=ROOT,
            env=env,
            stdout=output,
            stderr=subprocess.STDOUT,
            creationflags=subprocess.CREATE_NO_WINDOW if os.name == "nt" else 0,
        )
        try:
            deadline = time.monotonic() + 15
            while True:
                assert child.poll() is None, f"rtid exited; see {run / 'server.log'}"
                try:
                    with socket.create_connection(("127.0.0.1", ports[0]), timeout=0.2):
                        break
                except OSError:
                    assert time.monotonic() < deadline, (
                        f"rtid readiness timeout; see {run / 'server.log'}"
                    )
                    time.sleep(0.05)
            yield f"grpc://127.0.0.1:{ports[0]}", base, extra
        finally:
            if child.poll() is None:
                child.terminate()
            try:
                child.wait(timeout=5)
            except subprocess.TimeoutExpired:
                child.kill()
                child.wait(timeout=5)


class LiveCallbacks:
    def __init__(self, federate):
        self.events = []
        self.failure = None
        self.changed = asyncio.Event()
        self.task = asyncio.create_task(self._collect(federate))

    async def _collect(self, federate):
        try:
            async for event in federate.events():
                self.events.append(event)
                self.changed.set()
        except asyncio.CancelledError:
            raise
        except Exception as exc:
            self.failure = exc
            self.changed.set()

    async def wait(self, kind, predicate=lambda _: True, *, after=0):
        try:
            async with asyncio.timeout(12):
                while True:
                    self.changed.clear()
                    for event in self.events[after:]:
                        if isinstance(event, kind) and predicate(event):
                            return event
                    if self.failure is not None:
                        raise self.failure
                    await self.changed.wait()
        except TimeoutError:
            pytest.fail(f"missing {kind.__name__}; callbacks: {self.events[after:]!r}")

    async def close(self):
        self.task.cancel()
        await asyncio.gather(self.task, return_exceptions=True)


def test_live_two_connections_authoritative_fom_extension(live_server):
    url, base, extra = live_server

    async def exercise():
        async with asyncio.timeout(50), AsyncExitStack() as stack:
            left = await stack.enter_async_context(RtiConnection.connect(url))
            right = await stack.enter_async_context(RtiConnection.connect(url))
            a = await stack.enter_async_context(
                left.join_federation(
                    FederationSpec("live-fom", [str(base)]), federate_name="reader"
                )
            )
            log = LiveCallbacks(a)
            stack.push_async_callback(log.close)
            original = await a.support.get_interaction_class_handle(BASE_SIGNAL)
            b = await stack.enter_async_context(
                right.join_federation(
                    FederationSpec("live-fom", additional_fom_modules=[str(extra)]),
                    federate_name="extender",
                )
            )
            assert left.transport is not right.transport
            added = await b.support.get_interaction_class_handle(ADDED_SIGNAL)
            assert await a.support.get_interaction_class_handle(ADDED_SIGNAL) == added
            assert await a.support.get_interaction_class_handle(BASE_SIGNAL) == original
            await a.refresh_fom()
            await a.subscribe_interaction_class(ADDED_SIGNAL)
            await b.publish_interaction_class(ADDED_SIGNAL)
            payload, tag = struct.pack(">d", 42.5), b"\x00\xfflive"
            await b.send_interaction(ADDED_SIGNAL, {"Payload": payload}, tag=tag)
            received = await log.wait(ReceiveInteraction)
            assert received.class_name == "AddedSignal"
            assert await a.support.get_interaction_class_handle(received.class_name) == added
            assert received.parameters == {"Payload": payload}
            assert received.tag == tag
            assert received.metadata.producing_federate == b.handle
            assert await b.support.get_interaction_class_handle(BASE_SIGNAL) == original

    asyncio.run(exercise())


def test_live_timed_save_rejoin_restore_identity(live_server):
    url, base, _ = live_server

    async def exercise():
        async with asyncio.timeout(70), AsyncExitStack() as stack:
            left = await stack.enter_async_context(RtiConnection.connect(url))
            right = await stack.enter_async_context(RtiConnection.connect(url))
            spec = FederationSpec("live-restore", [str(base)])
            a = await stack.enter_async_context(
                left.join_federation(spec, federate_name="owner", federate_type="live")
            )
            old_context = right.join_federation(spec, federate_name="reader", federate_type="live")
            b = await stack.enter_async_context(old_context)
            a_log, b_log = LiveCallbacks(a), LiveCallbacks(b)
            stack.push_async_callback(a_log.close)
            stack.push_async_callback(b_log.close)
            await a.publish_object_class(VEHICLE, attributes=["Value"])
            await b.subscribe_object_class(VEHICLE, attributes=["Value"])
            obj = await a.register_object_instance(VEHICLE)
            await b_log.wait(DiscoverObjectInstance, lambda e: e.object_handle == obj)
            saved_handles = a.handle, b.handle
            for fed in (a, b):
                await fed.enable_time_regulation(0.5)
                await fed.enable_time_constrained()
            await a.savepoint.request_federation_save("timed", save_time=5)
            assert not any(isinstance(e, InitiateFederateSave) for e in a_log.events)
            await a.time_advance_request(5)
            await b.time_advance_request(5)
            for log in (a_log, b_log):
                begun = await log.wait(InitiateFederateSave, lambda e: e.label == "timed")
                assert begun.save_time == 5
            for fed in (a, b):
                await fed.savepoint.federate_save_begun()
                await fed.savepoint.federate_save_complete()
            for log in (a_log, b_log):
                await log.wait(FederationSaved, lambda e: e.label == "timed")
            await a.time_advance_request(8)
            await b.time_advance_request(8)
            for log in (a_log, b_log):
                await log.wait(TimeAdvanceGrant, lambda e: e.time == 8)
            await b_log.close()
            await old_context.__aexit__(None, None, None)
            new_context = right.join_federation(spec, federate_name="reader", federate_type="live")
            restored_reader = await stack.enter_async_context(new_context)
            assert restored_reader.handle != saved_handles[1]
            current_handle = restored_reader.handle
            cached_support = restored_reader.support
            cached_savepoint = restored_reader.savepoint
            restored_log = LiveCallbacks(restored_reader)
            stack.push_async_callback(restored_log.close)
            mark = len(a_log.events)
            await a.savepoint.request_federation_restore("timed")
            initiated = await restored_log.wait(
                InitiateFederateRestore, lambda e: e.label == "timed"
            )
            await a_log.wait(InitiateFederateRestore, lambda e: e.label == "timed", after=mark)
            assert initiated.federate_handle == saved_handles[1]
            assert initiated.federate_name == "reader"
            assert restored_reader.handle == current_handle
            await cached_savepoint.federate_restore_complete()
            await a.savepoint.federate_restore_complete()
            terminal = await restored_log.wait(FederationRestored, lambda e: e.label == "timed")
            await a_log.wait(FederationRestored, lambda e: e.label == "timed", after=mark)
            assert terminal.federate_handle == saved_handles[1]
            assert terminal.callback_epoch is not None and terminal.callback_epoch > 0
            assert terminal.next_retraction_handle == 1
            assert restored_reader.next_retraction_handle == 1
            assert restored_reader.handle == saved_handles[1]
            assert restored_reader.support is cached_support
            assert restored_reader.savepoint is cached_savepoint
            cls = await cached_support.get_object_class_handle(VEHICLE)
            assert await cached_support.get_known_object_class_handle(obj) == cls
            assert await restored_reader.query_logical_time() == 5
            restored_state = await restored_reader.mom.query_federate_attributes(
                restored_reader.handle
            )
            assert restored_state.found
            assert restored_state.time_regulating and restored_state.time_constrained
            assert restored_state.lookahead == 0.5
            assert restored_state.attribute_values["HLAasynchronousDelivery"] == struct.pack(
                ">i", 0
            )
            assert restored_state.attribute_values["HLAtimeManagerState"] == struct.pack(">i", 0)
            # Restore retains constrained/async-OFF state. Explicitly allow idle RO.
            await restored_reader.enable_asynchronous_delivery()
            value = struct.pack(">d", 9)
            await a.update_attributes(obj, {"Value": value}, tag=b"after-restore")
            reflected = await restored_log.wait(
                ReflectAttributeValues, lambda e: e.tag == b"after-restore"
            )
            assert reflected.object_handle == obj
            attr = await cached_support.get_attribute_handle(cls, "Value")
            assert reflected.attribute_values == {attr: value}
            assert reflected.timestamp is None
            assert reflected.metadata.sent_order == OrderType.RECEIVE
            assert reflected.metadata.received_order == OrderType.RECEIVE
            assert reflected.metadata.producing_federate == saved_handles[0]
            assert await restored_reader.query_logical_time() == 5

    asyncio.run(exercise())


def test_live_object_update_rate_callback(live_server):
    url, base, _ = live_server

    async def exercise():
        async with asyncio.timeout(40), AsyncExitStack() as stack:
            left = await stack.enter_async_context(RtiConnection.connect(url))
            right = await stack.enter_async_context(RtiConnection.connect(url))
            spec = FederationSpec("live-rate", [str(base)])
            a = await stack.enter_async_context(
                left.join_federation(spec, federate_name="publisher")
            )
            b = await stack.enter_async_context(
                right.join_federation(spec, federate_name="subscriber")
            )
            log = LiveCallbacks(a)
            stack.push_async_callback(log.close)
            await a.publish_object_class(VEHICLE, attributes=["Value"])
            obj = await a.register_object_instance(VEHICLE)
            await b.subscribe_object_class(VEHICLE, attributes=["Value"])
            event = await log.wait(TurnUpdatesOnForObjectInstance, lambda e: e.object_handle == obj)
            cls = await a.support.get_object_class_handle(VEHICLE)
            attr = await a.support.get_attribute_handle(cls, "Value")
            assert event.attribute_handles == (attr,)
            assert event.update_rate_designator is None
            mark = len(log.events)
            await b.subscribe_object_class(
                VEHICLE, attributes=["Value"], update_rate_designator="Slow"
            )
            named = await log.wait(TurnUpdatesOnForObjectInstance, after=mark)
            assert named.object_handle == obj
            assert named.attribute_handles == (attr,)
            assert named.update_rate_designator == "Slow"
            mark = len(log.events)
            await b.subscribe_object_class(VEHICLE, attributes=["Value"], active=False)
            disabled = await log.wait(TurnUpdatesOffForObjectInstance, after=mark)
            assert disabled.object_handle == obj
            assert disabled.attribute_handles == (attr,)

    asyncio.run(exercise())


def test_live_restore_retains_retraction_history_across_rejoin(live_server):
    url, base, _ = live_server

    async def exercise():
        async with asyncio.timeout(70), AsyncExitStack() as stack:
            left = await stack.enter_async_context(RtiConnection.connect(url))
            right = await stack.enter_async_context(RtiConnection.connect(url))
            spec = FederationSpec("live-retractions", [str(base)])
            old_context = left.join_federation(spec, federate_name="producer")
            sender = await stack.enter_async_context(old_context)
            receiver = await stack.enter_async_context(
                right.join_federation(spec, federate_name="consumer")
            )
            sent_log, received_log = LiveCallbacks(sender), LiveCallbacks(receiver)
            stack.push_async_callback(sent_log.close)
            stack.push_async_callback(received_log.close)
            await sender.publish_interaction_class(BASE_SIGNAL)
            await receiver.subscribe_interaction_class(BASE_SIGNAL)
            signal = await sender.support.get_interaction_class_handle(BASE_SIGNAL)
            from rti.v1 import object_pb2

            await sender.change_interaction_order_type(signal, object_pb2.ORDER_TYPE_TIMESTAMP)
            for fed in (sender, receiver):
                await fed.enable_time_regulation(0.5)
                await fed.enable_time_constrained()
            payload = {"Payload": struct.pack(">d", 7)}
            await sender.send_interaction(
                BASE_SIGNAL, payload, timestamp=10, retraction_handle=7, tag=b"saved"
            )
            await sender.savepoint.request_federation_save("pending")
            for log in (sent_log, received_log):
                await log.wait(InitiateFederateSave, lambda e: e.label == "pending")
            for fed in (sender, receiver):
                await fed.savepoint.federate_save_begun()
                await fed.savepoint.federate_save_complete()
            for log in (sent_log, received_log):
                await log.wait(FederationSaved, lambda e: e.label == "pending")
            await sender.send_interaction(
                BASE_SIGNAL, payload, timestamp=11, retraction_handle=23, tag=b"after-save"
            )
            saved_handle = sender.handle
            await sent_log.close()
            await old_context.__aexit__(None, None, None)
            restored = await stack.enter_async_context(
                left.join_federation(spec, federate_name="producer")
            )
            assert restored.handle != saved_handle
            restored_log = LiveCallbacks(restored)
            stack.push_async_callback(restored_log.close)
            mark = len(received_log.events)
            await receiver.savepoint.request_federation_restore("pending")
            await restored_log.wait(InitiateFederateRestore, lambda e: e.label == "pending")
            await received_log.wait(
                InitiateFederateRestore, lambda e: e.label == "pending", after=mark
            )
            await restored.savepoint.federate_restore_complete()
            await receiver.savepoint.federate_restore_complete()
            terminal = await restored_log.wait(FederationRestored, lambda e: e.label == "pending")
            await received_log.wait(FederationRestored, lambda e: e.label == "pending", after=mark)
            assert restored.handle == saved_handle
            assert terminal.next_retraction_handle == 24
            assert restored.next_retraction_handle == 24
            with pytest.raises(Exception, match="retraction ID.*already"):
                await restored.send_interaction(
                    BASE_SIGNAL, payload, timestamp=12, retraction_handle=7, tag=b"duplicate"
                )
            await restored.send_interaction(
                BASE_SIGNAL, payload, timestamp=12, retraction_handle=3, tag=b"retracted"
            )
            await restored.retract(3)
            await restored.time_advance_request(15)
            await receiver.time_advance_request(15)
            await restored_log.wait(TimeAdvanceGrant, lambda e: e.time == 15)
            await received_log.wait(TimeAdvanceGrant, lambda e: e.time == 15, after=mark)
            delivered = [e for e in received_log.events[mark:] if isinstance(e, ReceiveInteraction)]
            assert [e.tag for e in delivered] == [b"saved"]
            assert delivered[0].parameters == payload

    asyncio.run(exercise())
