"""M27 Phase A — outbox race regression.

Reproduces the pre-M27 race: fire a service-group RPC (here a
reservation request) IMMEDIATELY after JoinFederation, with no
artificial delay. Pre-M27 the success/failure callback was dropped
on the floor because rtid's outbox didn't have a channel for the
federate yet. Post-M27 the federation manager's OnFederateJoined
hook pre-binds the outbox state, so the event buffers until the
StreamService.Events stream attaches.

Runs 10× to provoke any remaining timing window.
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
    ObjectInstanceNameReservationFailed,
    ObjectInstanceNameReservationSucceeded,
)

BIN_DIR = REPO_ROOT / "bin"
RTID_BINARY = BIN_DIR / "rtid"

_FOM = """<?xml version="1.0" encoding="utf-8"?>
<objectModel xmlns="http://standards.ieee.org/IEEE1516-2010">
  <modelIdentification>
    <name>M27 Race FOM</name><type>FOM</type><version>1.0</version>
  </modelIdentification>
  <objects>
    <objectClass>
      <name>HLAobjectRoot</name>
      <objectClass>
        <name>Vehicle</name>
        <sharing>PublishSubscribe</sharing>
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


async def _drain_one(fed) -> object:  # type: ignore[no-untyped-def]
    async def _inner() -> object:
        async for ev in fed.events():
            if isinstance(
                ev,
                (
                    ObjectInstanceNameReservationSucceeded,
                    ObjectInstanceNameReservationFailed,
                ),
            ):
                return ev
        return None

    return await asyncio.wait_for(_inner(), timeout=2.0)


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
        mode="w", suffix=".xml", prefix="m27-fom-", delete=False, encoding="utf-8",
    )
    tmp.write(_FOM)
    tmp.flush()
    tmp.close()
    return Path(tmp.name)


@pytest.mark.spec
@pytest.mark.integration
async def test_spec_m27_service_rpc_immediately_after_join() -> None:
    """A service-group RPC fired with no delay after join still delivers
    its callback. Pre-M27 this raced and lost the event."""
    async with _rtid_subprocess() as url:
        fom = _make_fom()
        spec = FederationSpec(name="m27-race", fom_modules=[str(fom)])
        async with RtiConnection.connect(url) as rti:
            async with rti.join_federation(spec, federate_name="fed1") as fed:
                # ZERO sleep here — this is the regression. Pre-M27,
                # the outbox didn't have a channel for fed yet and
                # the success event was dropped.
                await fed.reservation.reserve("instant-1")
                ev = await _drain_one(fed)
                assert isinstance(ev, ObjectInstanceNameReservationSucceeded)
                assert ev.object_name == "instant-1"


@pytest.mark.spec
@pytest.mark.integration
async def test_spec_m27_burst_reservations_after_join() -> None:
    """Burst 5 reservations immediately after join; all 5 callbacks arrive.

    Exercises the buffering capacity of the pre-bound channel — Bind
    must create a channel large enough to hold events that pile up
    during the post-join, pre-stream-attach window.
    """
    async with _rtid_subprocess() as url:
        fom = _make_fom()
        spec = FederationSpec(name="m27-burst", fom_modules=[str(fom)])
        async with RtiConnection.connect(url) as rti:
            async with rti.join_federation(spec, federate_name="fed1") as fed:
                names = [f"burst-{i}" for i in range(5)]
                # Fire all 5 with no awaits between (other than the
                # network round-trip each one requires).
                for n in names:
                    await fed.reservation.reserve(n)
                # Collect 5 success callbacks.
                received: list[str] = []

                async def collect() -> None:
                    async for ev in fed.events():
                        if isinstance(ev, ObjectInstanceNameReservationSucceeded):
                            received.append(ev.object_name)
                            if len(received) == 5:
                                return

                await asyncio.wait_for(collect(), timeout=5.0)
                assert sorted(received) == sorted(names)


@pytest.mark.spec
@pytest.mark.integration
async def test_spec_m27_race_10x_repeatable() -> None:
    """Run the race scenario 10 times in a single rtid to confirm it's
    not just lucky timing. Each iteration is a fresh federation +
    federate so per-federate state doesn't accumulate."""
    async with _rtid_subprocess() as url:
        fom = _make_fom()
        for i in range(10):
            spec = FederationSpec(name=f"m27-iter-{i}", fom_modules=[str(fom)])
            async with RtiConnection.connect(url) as rti:
                async with rti.join_federation(spec, federate_name="fed1") as fed:
                    await fed.reservation.reserve(f"item-{i}")
                    ev = await _drain_one(fed)
                    assert isinstance(
                        ev, ObjectInstanceNameReservationSucceeded
                    ), f"iter {i}: got {ev!r}"
                    assert ev.object_name == f"item-{i}"
