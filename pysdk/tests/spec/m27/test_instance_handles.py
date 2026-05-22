"""M27 Phase C — §6.30 / §6.31 runtime instance handle services.

Federate A registers "car-7". Federate B (a late joiner) calls
getObjectInstanceHandle("car-7") to recover the handle without
having received the Discover callback. Federate A also round-trips
its own handle through getObjectInstanceName.
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

BIN_DIR = REPO_ROOT / "bin"
RTID_BINARY = BIN_DIR / "rtid"

_FOM = """<?xml version="1.0" encoding="utf-8"?>
<objectModel xmlns="http://standards.ieee.org/IEEE1516-2010">
  <modelIdentification>
    <name>M27 C FOM</name><type>FOM</type><version>1.0</version>
  </modelIdentification>
  <objects>
    <objectClass>
      <name>HLAobjectRoot</name>
      <objectClass>
        <name>Vehicle</name><sharing>PublishSubscribe</sharing>
        <attribute>
          <name>P</name><dataType>HLAfloat64BE</dataType>
          <updateType>Conditional</updateType><ownership>NoTransfer</ownership>
          <sharing>PublishSubscribe</sharing>
          <transportation>HLAreliable</transportation><order>TimeStamp</order>
        </attribute>
      </objectClass>
    </objectClass>
  </objects>
  <interactions>
    <interactionClass><name>HLAinteractionRoot</name><sharing>PublishSubscribe</sharing></interactionClass>
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
        mode="w", suffix=".xml", prefix="m27c-fom-", delete=False, encoding="utf-8",
    )
    tmp.write(_FOM)
    tmp.flush()
    tmp.close()
    return Path(tmp.name)


@pytest.mark.spec
@pytest.mark.integration
async def test_spec_m27c_get_object_instance_handle_round_trip() -> None:
    """Register an instance, then look it up by name and by handle."""
    async with _rtid_subprocess() as url:
        fom = _make_fom()
        spec = FederationSpec(name="m27c-rt", fom_modules=[str(fom)])
        async with RtiConnection.connect(url) as rti:
            async with rti.join_federation(spec, federate_name="fed1") as fed:
                vehicle = await fed.support.get_object_class_handle("Vehicle")
                await fed.publish_object_class(vehicle, attributes=["P"])
                obj_h = await fed.register_object_instance(
                    vehicle, instance_name="car-7"
                )
                assert obj_h > 0

                # Round-trip name → handle.
                got_h = await fed.support.get_object_instance_handle("car-7")
                assert got_h == obj_h

                # Round-trip handle → name.
                got_name = await fed.support.get_object_instance_name(obj_h)
                assert got_name == "car-7"


@pytest.mark.spec
@pytest.mark.integration
async def test_spec_m27c_late_joiner_can_resolve_handle() -> None:
    """Federate B can resolve "car-7" even without having received the
    Discover callback. Closes the §6.30 use case for late joiners."""
    async with _rtid_subprocess() as url:
        fom = _make_fom()
        spec = FederationSpec(name="m27c-late", fom_modules=[str(fom)])
        async with RtiConnection.connect(url) as rti_a, RtiConnection.connect(url) as rti_b:
            async with rti_a.join_federation(spec, federate_name="A") as fed_a:
                vehicle = await fed_a.support.get_object_class_handle("Vehicle")
                await fed_a.publish_object_class(vehicle, attributes=["P"])
                obj_h = await fed_a.register_object_instance(
                    vehicle, instance_name="car-7"
                )

                # Fed B joins AFTER the instance was registered.
                async with rti_b.join_federation(spec, federate_name="B") as fed_b:
                    # B never subscribed and never saw Discover; still
                    # can resolve the handle.
                    h = await fed_b.support.get_object_instance_handle("car-7")
                    assert h == obj_h


@pytest.mark.spec
@pytest.mark.integration
async def test_spec_m27c_unknown_instance_returns_not_found() -> None:
    """Looking up a name that was never registered returns an SDK error."""
    async with _rtid_subprocess() as url:
        fom = _make_fom()
        spec = FederationSpec(name="m27c-miss", fom_modules=[str(fom)])
        async with RtiConnection.connect(url) as rti:
            async with rti.join_federation(spec, federate_name="fed1") as fed:
                with pytest.raises(Exception):  # noqa: BLE001, PT011
                    await fed.support.get_object_instance_handle("does-not-exist")
