"""M25 Phase B — SupportService end-to-end.

Spins up rtid as a subprocess, creates a federation with a small FOM
containing one object class (Vehicle + Position attribute), one
interaction class (Honk + Volume parameter), and one dimension (X with
upper_bound 1000), then exercises the Pitch-style handle/name lookups
via the Rti1516eAmbassador. Verifies the wire RPCs return the same
handles a federate would see from a publish/subscribe call.
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
from typing import cast

import pytest

REPO_ROOT = Path(__file__).resolve().parents[4]
_PYSDK = REPO_ROOT / "pysdk"
if str(_PYSDK) not in sys.path:
    sys.path.insert(0, str(_PYSDK))

# ruff: noqa: E402
from rti1516e.connection import FederationSpec, RtiConnection
from rti1516e.support import (
    ORDER_TYPE_RECEIVE,
    ORDER_TYPE_TIMESTAMP,
    TRANSPORT_BEST_EFFORT,
    TRANSPORT_RELIABLE,
)

BIN_DIR = REPO_ROOT / "bin"
RTID_BINARY = BIN_DIR / "rtid"

_FOM_XML = """<?xml version="1.0" encoding="utf-8"?>
<objectModel xmlns="http://standards.ieee.org/IEEE1516-2010">
  <modelIdentification>
    <name>M25 Support FOM</name>
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
      <interactionClass>
        <name>Honk</name>
        <sharing>PublishSubscribe</sharing>
        <transportation>HLAreliable</transportation>
        <order>TimeStamp</order>
        <parameter>
          <name>Volume</name>
          <dataType>HLAinteger32BE</dataType>
        </parameter>
      </interactionClass>
    </interactionClass>
  </interactions>
  <dimensions>
    <dimension>
      <name>X</name>
      <upperBound>1000</upperBound>
    </dimension>
  </dimensions>
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


async def _drive_lookups(url: str, fom_path: Path) -> dict[str, object]:
    spec = FederationSpec(
        name="m25-support",
        fom_modules=[str(fom_path)],
    )
    async with RtiConnection.connect(url) as rti:
        async with rti.join_federation(spec, federate_name="m25-lookup") as fed:
            vehicle_h = await fed.support.get_object_class_handle("Vehicle")
            vehicle_name = await fed.support.get_object_class_name(vehicle_h)
            pos_h = await fed.support.get_attribute_handle(vehicle_h, "Position")
            pos_name = await fed.support.get_attribute_name(vehicle_h, pos_h)
            honk_h = await fed.support.get_interaction_class_handle("Honk")
            honk_name = await fed.support.get_interaction_class_name(honk_h)
            vol_h = await fed.support.get_parameter_handle(honk_h, "Volume")
            vol_name = await fed.support.get_parameter_name(honk_h, vol_h)
            x_h = await fed.support.get_dimension_handle("X")
            x_name = await fed.support.get_dimension_name(x_h)
            x_ub = await fed.support.get_dimension_upper_bound(x_h)
            ts_type = await fed.support.get_order_type("TimeStamp")
            ts_name = await fed.support.get_order_name(ts_type)
            rel_type = await fed.support.get_transportation_type("HLAreliable")
            rel_name = await fed.support.get_transportation_name(rel_type)
    return {
        "vehicle_h": vehicle_h, "vehicle_name": vehicle_name,
        "pos_h": pos_h, "pos_name": pos_name,
        "honk_h": honk_h, "honk_name": honk_name,
        "vol_h": vol_h, "vol_name": vol_name,
        "x_h": x_h, "x_name": x_name, "x_ub": x_ub,
        "ts_type": ts_type, "ts_name": ts_name,
        "rel_type": rel_type, "rel_name": rel_name,
    }


@pytest.mark.spec
@pytest.mark.integration
def test_spec_m25_support_service_end_to_end() -> None:
    """Pitch-style handle/name lookups round-trip through the wire."""
    if shutil.which("go") is None:
        pytest.skip("go toolchain not on PATH; cannot build rtid")

    binary = _build_rtid()
    listen_port = _free_port()
    metrics_port = _free_port()
    if metrics_port == listen_port:
        metrics_port = _free_port()

    tmp = tempfile.NamedTemporaryFile(  # noqa: SIM115
        mode="w", suffix=".xml", prefix="m25-fom-", delete=False, encoding="utf-8",
    )
    tmp.write(_FOM_XML)
    tmp.flush()
    tmp.close()
    fom_path = Path(tmp.name)

    proc = _spawn_rtid(binary, listen_port, metrics_port)
    try:
        asyncio.run(_wait_for_grpc(listen_port))
        result = asyncio.run(
            _drive_lookups(f"grpc://127.0.0.1:{listen_port}", fom_path)
        )
    finally:
        proc.terminate()
        try:
            proc.wait(timeout=5)
        except subprocess.TimeoutExpired:
            proc.kill()
            proc.wait(timeout=5)

    assert cast(int, result["vehicle_h"]) > 0
    assert result["vehicle_name"] == "Vehicle"
    assert cast(int, result["pos_h"]) > 0
    assert result["pos_name"] == "Position"
    assert cast(int, result["honk_h"]) > 0
    assert result["honk_name"] == "Honk"
    assert cast(int, result["vol_h"]) > 0
    assert result["vol_name"] == "Volume"
    assert cast(int, result["x_h"]) > 0
    assert result["x_name"] == "X"
    assert result["x_ub"] == 1000
    assert result["ts_type"] == ORDER_TYPE_TIMESTAMP
    assert result["ts_name"] == "TimeStamp"
    assert result["rel_type"] == TRANSPORT_RELIABLE
    assert result["rel_name"] == "HLAreliable"


@pytest.mark.spec
def test_spec_m25_support_enum_constants() -> None:
    """The SDK constants match the Go-side encoding (1=Receive, 2=TimeStamp;
    1=Reliable, 2=BestEffort)."""
    assert ORDER_TYPE_RECEIVE == 1
    assert ORDER_TYPE_TIMESTAMP == 2
    assert TRANSPORT_RELIABLE == 1
    assert TRANSPORT_BEST_EFFORT == 2
