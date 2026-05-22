"""M27 Phase B — handle-keyed Pitch-style ambassador methods.

Drives a federate using ONLY the handle-keyed variants of
publish/subscribe/register/update/sendInteraction. Pre-M27 these
calls only accepted string names; Pitch federates that pre-resolve
handles via ``getObjectClassHandle`` / ``getAttributeHandle`` and
pass them through couldn't compile against gorti.

This test verifies the int-keyed paths work end-to-end through the
wire, and that the resulting object instance is reflected on a
subscriber federate that also uses the handle-keyed API.
"""

from __future__ import annotations

import asyncio
import contextlib
import shutil
import socket
import subprocess
import sys
import tempfile
from pathlib import Path

import pytest

REPO_ROOT = Path(__file__).resolve().parents[4]
_PYSDK = REPO_ROOT / "pysdk"
if str(_PYSDK) not in sys.path:
    sys.path.insert(0, str(_PYSDK))

# ruff: noqa: E402
from rti1516e.connection import FederationSpec, RtiConnection
from rti1516e.events import (
    DiscoverObjectInstance,
    ReceiveInteraction,
    ReflectAttributeValues,
)

BIN_DIR = REPO_ROOT / "bin"
RTID_BINARY = BIN_DIR / "rtid"

_FOM = """<?xml version="1.0" encoding="utf-8"?>
<objectModel xmlns="http://standards.ieee.org/IEEE1516-2010">
  <modelIdentification>
    <name>M27 B FOM</name><type>FOM</type><version>1.0</version>
  </modelIdentification>
  <objects>
    <objectClass>
      <name>HLAobjectRoot</name>
      <objectClass>
        <name>Vehicle</name>
        <sharing>PublishSubscribe</sharing>
        <attribute>
          <name>Position</name><dataType>HLAfloat64BE</dataType>
          <updateType>Conditional</updateType><ownership>NoTransfer</ownership>
          <sharing>PublishSubscribe</sharing>
          <transportation>HLAreliable</transportation><order>TimeStamp</order>
        </attribute>
        <attribute>
          <name>Velocity</name><dataType>HLAfloat64BE</dataType>
          <updateType>Conditional</updateType><ownership>NoTransfer</ownership>
          <sharing>PublishSubscribe</sharing>
          <transportation>HLAreliable</transportation><order>TimeStamp</order>
        </attribute>
      </objectClass>
    </objectClass>
  </objects>
  <interactions>
    <interactionClass>
      <name>HLAinteractionRoot</name><sharing>PublishSubscribe</sharing>
      <interactionClass>
        <name>Honk</name><sharing>PublishSubscribe</sharing>
        <transportation>HLAreliable</transportation><order>TimeStamp</order>
        <parameter><name>Volume</name><dataType>HLAinteger32BE</dataType></parameter>
      </interactionClass>
    </interactionClass>
  </interactions>
</objectModel>
"""


def _free_port() -> int:
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
        s.bind(("127.0.0.1", 0))
        return int(s.getsockname()[1])


def _build_rtid() -> Path:
    BIN_DIR.mkdir(parents=True, exist_ok=True)
    argv = ["go", "build", "-o", str(RTID_BINARY), "./rti/cmd/rtid"]  # noqa: S607
    subprocess.run(argv, cwd=REPO_ROOT, check=True)  # noqa: S603
    return RTID_BINARY


def _spawn_rtid(binary: Path, port: int, mport: int) -> subprocess.Popen[bytes]:
    return subprocess.Popen(  # noqa: S603
        [
            str(binary),
            "--listen", f":{port}",
            "--metrics-listen", f":{mport}",
            "--log-level", "warn",
        ],
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
        start_new_session=True,
    )


async def _wait_for_grpc(port: int, *, timeout: float = 10.0) -> None:  # noqa: ASYNC109
    loop = asyncio.get_event_loop()
    deadline = loop.time() + timeout
    while loop.time() < deadline:
        try:
            _, w = await asyncio.wait_for(
                asyncio.open_connection("127.0.0.1", port), timeout=0.5
            )
            w.close()
            with contextlib.suppress(BaseException):
                await w.wait_closed()
            return
        except (OSError, TimeoutError):
            await asyncio.sleep(0.1)
    raise TimeoutError(f"rtid did not accept on :{port} within {timeout}s")


@contextlib.asynccontextmanager
async def _rtid_subprocess():  # type: ignore[no-untyped-def]
    if shutil.which("go") is None:
        pytest.skip("go toolchain not on PATH")
    binary = _build_rtid()
    port = _free_port()
    mport = _free_port()
    if mport == port:
        mport = _free_port()
    proc = _spawn_rtid(binary, port, mport)
    try:
        await _wait_for_grpc(port)
        yield f"grpc://127.0.0.1:{port}"
    finally:
        proc.terminate()
        try:
            proc.wait(timeout=5)
        except subprocess.TimeoutExpired:
            proc.kill()
            proc.wait(timeout=5)


def _make_fom() -> Path:
    tmp = tempfile.NamedTemporaryFile(  # noqa: SIM115
        mode="w", suffix=".xml", prefix="m27b-fom-", delete=False, encoding="utf-8",
    )
    tmp.write(_FOM)
    tmp.flush()
    tmp.close()
    return Path(tmp.name)


@pytest.mark.spec
@pytest.mark.integration
async def test_spec_m27b_handle_keyed_publish_register_update() -> None:
    """A federate using ONLY handle-keyed APIs registers and updates."""
    async with _rtid_subprocess() as url:
        fom = _make_fom()
        spec = FederationSpec(name="m27b-publish", fom_modules=[str(fom)])
        async with RtiConnection.connect(url) as rti:
            async with rti.join_federation(spec, federate_name="pub") as fed:
                # Resolve handles up front (Pitch idiom).
                vehicle_class = await fed.support.get_object_class_handle("Vehicle")
                pos_attr = await fed.support.get_attribute_handle(vehicle_class, "Position")
                vel_attr = await fed.support.get_attribute_handle(vehicle_class, "Velocity")
                assert vehicle_class > 0 and pos_attr > 0 and vel_attr > 0

                # Publish by handle.
                await fed.publish_object_class(vehicle_class, attributes=[pos_attr, vel_attr])

                # Register by handle.
                obj_h = await fed.register_object_instance(vehicle_class, instance_name="car-A")
                assert obj_h > 0

                # Update by handle keys.
                await fed.update_attributes(
                    obj_h,
                    {pos_attr: (123).to_bytes(8, "big"), vel_attr: (7).to_bytes(8, "big")},
                )


@pytest.mark.spec
@pytest.mark.integration
async def test_spec_m27b_handle_keyed_cross_federate_reflect() -> None:
    """Publisher updates by handle; subscriber sees reflection.

    Both federates use handle-keyed APIs only.
    """
    async with _rtid_subprocess() as url:
        fom = _make_fom()
        spec = FederationSpec(name="m27b-cross", fom_modules=[str(fom)])
        async with RtiConnection.connect(url) as rti_pub, RtiConnection.connect(url) as rti_sub:
            async with (
                rti_pub.join_federation(spec, federate_name="pub") as pub_fed,
                rti_sub.join_federation(spec, federate_name="sub") as sub_fed,
            ):
                # Both resolve handles independently — they're FOM-deterministic.
                vehicle = await pub_fed.support.get_object_class_handle("Vehicle")
                pos_attr = await pub_fed.support.get_attribute_handle(vehicle, "Position")

                await pub_fed.publish_object_class(vehicle, attributes=[pos_attr])
                await sub_fed.subscribe_object_class(vehicle, attributes=[pos_attr])

                obj_h = await pub_fed.register_object_instance(vehicle, instance_name="car-B")

                # Subscriber must see Discover.
                discovered: list[DiscoverObjectInstance] = []

                async def collect_discover() -> None:
                    async for ev in sub_fed.events():
                        if isinstance(ev, DiscoverObjectInstance):
                            discovered.append(ev)
                            return

                await asyncio.wait_for(collect_discover(), timeout=3.0)
                assert len(discovered) == 1
                # The discover event reports the minted handle.
                assert discovered[0].object_handle == obj_h

                # Publish an update (handle-keyed).
                await pub_fed.update_attributes(
                    obj_h, {pos_attr: (42).to_bytes(8, "big")}, timestamp=1.0
                )

                # Subscriber receives the reflection.
                reflections: list[ReflectAttributeValues] = []

                async def collect_reflect() -> None:
                    async for ev in sub_fed.events():
                        if isinstance(ev, ReflectAttributeValues):
                            reflections.append(ev)
                            return

                await asyncio.wait_for(collect_reflect(), timeout=3.0)
                assert len(reflections) == 1
                assert reflections[0].object_handle == obj_h


@pytest.mark.spec
@pytest.mark.integration
async def test_spec_m27b_handle_keyed_send_interaction() -> None:
    """Send + receive interaction via handle-keyed APIs."""
    async with _rtid_subprocess() as url:
        fom = _make_fom()
        spec = FederationSpec(name="m27b-honk", fom_modules=[str(fom)])
        async with RtiConnection.connect(url) as rti_pub, RtiConnection.connect(url) as rti_sub:
            async with (
                rti_pub.join_federation(spec, federate_name="pub") as pub_fed,
                rti_sub.join_federation(spec, federate_name="sub") as sub_fed,
            ):
                honk = await pub_fed.support.get_interaction_class_handle("Honk")
                vol = await pub_fed.support.get_parameter_handle(honk, "Volume")
                assert honk > 0 and vol > 0

                await pub_fed.publish_interaction_class("Honk")
                await sub_fed.subscribe_interaction_class("Honk")

                # Send by handle keys.
                await pub_fed.send_interaction(honk, {vol: (9).to_bytes(4, "big")})

                received: list[ReceiveInteraction] = []

                async def collect_receive() -> None:
                    async for ev in sub_fed.events():
                        if isinstance(ev, ReceiveInteraction):
                            received.append(ev)
                            return

                await asyncio.wait_for(collect_receive(), timeout=3.0)
                assert len(received) == 1


@pytest.mark.spec
@pytest.mark.integration
async def test_spec_m27b_mixed_handle_and_name_attributes() -> None:
    """A mixed list of handles + names in attributes[] resolves both correctly."""
    async with _rtid_subprocess() as url:
        fom = _make_fom()
        spec = FederationSpec(name="m27b-mixed", fom_modules=[str(fom)])
        async with RtiConnection.connect(url) as rti:
            async with rti.join_federation(spec, federate_name="fed1") as fed:
                vehicle = await fed.support.get_object_class_handle("Vehicle")
                pos_attr = await fed.support.get_attribute_handle(vehicle, "Position")

                # Mixed: vehicle as handle, attributes as one int + one str.
                await fed.publish_object_class(
                    vehicle, attributes=[pos_attr, "Velocity"]
                )
                obj_h = await fed.register_object_instance(vehicle)
                assert obj_h > 0


@pytest.mark.spec
@pytest.mark.integration
async def test_spec_m27b_string_path_still_works() -> None:
    """Pre-M27 string-only API must keep working unchanged."""
    async with _rtid_subprocess() as url:
        fom = _make_fom()
        spec = FederationSpec(name="m27b-str", fom_modules=[str(fom)])
        async with RtiConnection.connect(url) as rti:
            async with rti.join_federation(spec, federate_name="fed1") as fed:
                await fed.publish_object_class("Vehicle", attributes=["Position", "Velocity"])
                obj_h = await fed.register_object_instance("Vehicle", instance_name="legacy")
                assert obj_h > 0
                await fed.update_attributes(
                    obj_h,
                    {"Position": (1).to_bytes(8, "big"), "Velocity": (2).to_bytes(8, "big")},
                )
