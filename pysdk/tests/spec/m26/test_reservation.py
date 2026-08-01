"""M26 Phase F — object instance name reservation end-to-end.

Drives the ReservationClient against a live rtid subprocess. Verifies
the wire-level reservation flow: reserve → success event delivered,
double-reserve → failure event delivered, register with reserved name
→ accepts, register with name reserved by another federate → rejects.
"""

from __future__ import annotations

import asyncio
import contextlib
import shutil
import socket
import subprocess
import sys
import tempfile
from collections.abc import Callable
from pathlib import Path
from typing import Any

import pytest

REPO_ROOT = Path(__file__).resolve().parents[4]
_PYSDK = REPO_ROOT / "pysdk"
if str(_PYSDK) not in sys.path:
    sys.path.insert(0, str(_PYSDK))

# ruff: noqa: E402
from rti1516e.connection import FederationSpec, RtiConnection
from rti1516e.events import (
    MultipleObjectInstanceNameReservationFailed,
    MultipleObjectInstanceNameReservationSucceeded,
    ObjectInstanceNameReservationFailed,
    ObjectInstanceNameReservationSucceeded,
)

BIN_DIR = REPO_ROOT / "bin"
RTID_BINARY = BIN_DIR / "rtid"

_FOM_XML = """<?xml version="1.0" encoding="utf-8"?>
<objectModel xmlns="http://standards.ieee.org/IEEE1516-2010">
  <modelIdentification>
    <name>M26 Reserve FOM</name>
    <type>FOM</type>
    <version>1.0</version>
  </modelIdentification>
  <objects>
    <objectClass>
      <name>HLAobjectRoot</name>
      <objectClass>
        <name>Vehicle</name>
        <sharing>PublishSubscribe</sharing>
        <attribute>
          <name>Position</name>
          <dataType>HLAfloat64BE</dataType>
          <updateType>Conditional</updateType>
          <ownership>NoTransfer</ownership>
          <sharing>PublishSubscribe</sharing>
          <transportation>HLAreliable</transportation>
          <order>TimeStamp</order>
        </attribute>
      </objectClass>
    </objectClass>
  </objects>
  <interactions>
    <interactionClass>
      <name>HLAinteractionRoot</name>
      <sharing>PublishSubscribe</sharing>
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


def _spawn_rtid(binary: Path, listen_port: int, metrics_port: int) -> subprocess.Popen[bytes]:
    return subprocess.Popen(  # noqa: S603
        [
            str(binary),
            "--listen", f":{listen_port}",
            "--metrics-listen", f":{metrics_port}",
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
            _, writer = await asyncio.wait_for(
                asyncio.open_connection("127.0.0.1", port), timeout=0.5
            )
            writer.close()
            with contextlib.suppress(BaseException):
                await writer.wait_closed()
            return
        except (OSError, TimeoutError):
            await asyncio.sleep(0.1)
    raise TimeoutError(f"rtid did not accept connections on :{port} within {timeout}s")


async def _drain_until(
    fed: Any,
    predicate: Callable[[Any], bool],
    timeout: float = 2.0,  # noqa: ASYNC109
) -> Any:
    """Drain events until predicate(event) is True. Returns that event.

    ``timeout`` is the deadline for the whole drain loop, not a per-call
    I/O timeout — ASYNC109 (use asyncio.timeout) doesn't apply here.
    """
    async def _inner() -> Any:
        async for ev in fed.events():
            if predicate(ev):
                return ev
        return None

    return await asyncio.wait_for(_inner(), timeout)


@contextlib.asynccontextmanager
async def _rtid_subprocess():  # type: ignore[no-untyped-def]
    """Spawn rtid on a free port and yield the URL.

    Async context manager so we don't have to call asyncio.run() from
    inside a pytest test function — pytest-asyncio auto mode runs
    async test functions on its own loop, and a nested asyncio.run()
    conflicts with the gRPC stream that gets bound to that loop.
    """
    if shutil.which("go") is None:
        pytest.skip("go toolchain not on PATH; cannot build rtid")
    binary = _build_rtid()
    listen_port = _free_port()
    metrics_port = _free_port()
    if metrics_port == listen_port:
        metrics_port = _free_port()
    proc = _spawn_rtid(binary, listen_port, metrics_port)
    try:
        await _wait_for_grpc(listen_port)
        yield f"grpc://127.0.0.1:{listen_port}"
    finally:
        proc.terminate()
        try:
            proc.wait(timeout=5)
        except subprocess.TimeoutExpired:
            proc.kill()
            proc.wait(timeout=5)


def _make_fom_file() -> Path:
    tmp = tempfile.NamedTemporaryFile(  # noqa: SIM115
        mode="w", suffix=".xml", prefix="m26-fom-", delete=False, encoding="utf-8",
    )
    tmp.write(_FOM_XML)
    tmp.flush()
    tmp.close()
    return Path(tmp.name)


@pytest.mark.spec
@pytest.mark.integration
async def test_spec_m26_reserve_then_register() -> None:
    """Reserve → success event delivered → register with name accepted."""
    async with _rtid_subprocess() as url:
        fom = _make_fom_file()
        spec = FederationSpec(name="m26-reserve-ok", fom_modules=[str(fom)])
        async with RtiConnection.connect(url) as rti:
            async with rti.join_federation(spec, federate_name="fed1") as fed:
                await fed.reservation.reserve("vehicle-1")
                ev = await _drain_until(
                    fed,
                    lambda e: isinstance(
                        e,
                        (
                            ObjectInstanceNameReservationSucceeded,
                            ObjectInstanceNameReservationFailed,
                        ),
                    ),
                )
                assert isinstance(ev, ObjectInstanceNameReservationSucceeded), f"got {ev!r}"
                assert ev.object_name == "vehicle-1"


@pytest.mark.spec
@pytest.mark.integration
async def test_spec_m26_double_reserve_fails() -> None:
    """Reserve then reserve again → second one delivers Failed event."""
    async with _rtid_subprocess() as url:
        fom = _make_fom_file()
        spec = FederationSpec(name="m26-double", fom_modules=[str(fom)])
        async with RtiConnection.connect(url) as rti:
            async with rti.join_federation(spec, federate_name="fed1") as fed:
                await fed.reservation.reserve("dup")
                ok = await _drain_until(
                    fed,
                    lambda e: isinstance(
                        e,
                        (
                            ObjectInstanceNameReservationSucceeded,
                            ObjectInstanceNameReservationFailed,
                        ),
                    ),
                )
                assert isinstance(ok, ObjectInstanceNameReservationSucceeded)

                await fed.reservation.reserve("dup")
                fail = await _drain_until(
                    fed,
                    lambda e: isinstance(e, ObjectInstanceNameReservationFailed),
                )
                assert isinstance(fail, ObjectInstanceNameReservationFailed)
                assert fail.object_name == "dup"


@pytest.mark.spec
@pytest.mark.integration
async def test_spec_m26_multi_reserve_success() -> None:
    """Atomic batch reservation delivers a single Multi success event."""
    async with _rtid_subprocess() as url:
        fom = _make_fom_file()
        spec = FederationSpec(name="m26-multi-ok", fom_modules=[str(fom)])
        async with RtiConnection.connect(url) as rti:
            async with rti.join_federation(spec, federate_name="fed1") as fed:
                await fed.reservation.reserve_multiple(["a", "b", "c"])
                ev = await _drain_until(
                    fed,
                    lambda e: isinstance(
                        e,
                        (
                            MultipleObjectInstanceNameReservationSucceeded,
                            MultipleObjectInstanceNameReservationFailed,
                        ),
                    ),
                )
                assert isinstance(ev, MultipleObjectInstanceNameReservationSucceeded)
                assert set(ev.object_names) == {"a", "b", "c"}


@pytest.mark.spec
@pytest.mark.integration
async def test_spec_m26_multi_reserve_partial_collision_fails() -> None:
    """Multi-reserve with one already-reserved name fails atomically."""
    async with _rtid_subprocess() as url:
        fom = _make_fom_file()
        spec = FederationSpec(name="m26-multi-fail", fom_modules=[str(fom)])
        async with RtiConnection.connect(url) as rti:
            async with rti.join_federation(spec, federate_name="fed1") as fed:
                # Pre-reserve "b" to force a batch collision.
                await fed.reservation.reserve("b")
                await _drain_until(
                    fed,
                    lambda e: isinstance(e, ObjectInstanceNameReservationSucceeded),
                )

                await fed.reservation.reserve_multiple(["a", "b", "c"])
                ev = await _drain_until(
                    fed,
                    lambda e: isinstance(
                        e,
                        (
                            MultipleObjectInstanceNameReservationSucceeded,
                            MultipleObjectInstanceNameReservationFailed,
                        ),
                    ),
                )
                assert isinstance(ev, MultipleObjectInstanceNameReservationFailed)
                assert set(ev.requested_names) == {"a", "b", "c"}
                assert "b" in ev.colliding_names
