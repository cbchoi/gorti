"""M27 Phase D — §11 MOM delegates on Rti1516eAmbassador.

End-to-end test that the IEEE 1516 service-style MOM query methods on the
ambassador return the expected snapshot of the federation and its
federates. Drives the federate through the SDK to populate MOM
state, then queries via the ambassador.
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
from rti1516e.mom import (
    CLASS_HLA_FEDERATE,
    CLASS_HLA_FEDERATION,
    FederateAttributes,
    FederationAttributes,
    MomInstance,
)

BIN_DIR = REPO_ROOT / "bin"
RTID_BINARY = BIN_DIR / "rtid"

_FOM = """<?xml version="1.0" encoding="utf-8"?>
<objectModel xmlns="http://standards.ieee.org/IEEE1516-2010">
  <modelIdentification>
    <name>M27 D MOM FOM</name><type>FOM</type><version>1.0</version>
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
        mode="w", suffix=".xml", prefix="m27d-fom-", delete=False, encoding="utf-8",
    )
    tmp.write(_FOM)
    tmp.flush()
    tmp.close()
    return Path(tmp.name)


@pytest.mark.spec
@pytest.mark.integration
async def test_spec_m27d_query_federation_attributes() -> None:
    """Federation MOM snapshot reflects the active federation."""
    async with _rtid_subprocess() as url:
        fom = _make_fom()
        spec = FederationSpec(name="m27d-fed-attrs", fom_modules=[str(fom)])
        async with RtiConnection.connect(url) as rti:
            async with rti.join_federation(spec, federate_name="alice") as fed:
                attrs = await fed.mom.query_federation_attributes()
                assert isinstance(attrs, FederationAttributes)
                assert attrs.federation_name == "m27d-fed-attrs"
                # alice should be in the federate handles tuple.
                assert fed.handle in attrs.federate_handles


@pytest.mark.spec
@pytest.mark.integration
async def test_spec_m27d_query_federate_attributes() -> None:
    """Federate MOM snapshot reflects the joined federate's identity."""
    async with _rtid_subprocess() as url:
        fom = _make_fom()
        spec = FederationSpec(name="m27d-fedate-attrs", fom_modules=[str(fom)])
        async with RtiConnection.connect(url) as rti:
            async with rti.join_federation(spec, federate_name="alice") as fed:
                attrs = await fed.mom.query_federate_attributes(fed.handle)
                assert isinstance(attrs, FederateAttributes)
                assert attrs.found
                assert attrs.federate_handle == fed.handle
                assert attrs.federate_name == "alice"


@pytest.mark.spec
@pytest.mark.integration
async def test_spec_m27d_enumerate_mom_instances() -> None:
    """MOM enumeration lists HLAfederation singleton + one HLAfederate per joined federate."""
    async with _rtid_subprocess() as url:
        fom = _make_fom()
        spec = FederationSpec(name="m27d-enum", fom_modules=[str(fom)])
        async with RtiConnection.connect(url) as rti_a, RtiConnection.connect(url) as rti_b:
            async with (
                rti_a.join_federation(spec, federate_name="alice") as fed_a,
                rti_b.join_federation(spec, federate_name="bob") as fed_b,
            ):
                instances = await fed_a.mom.enumerate_mom_instances()
                assert all(isinstance(i, MomInstance) for i in instances)
                classes = {i.class_name for i in instances}
                assert CLASS_HLA_FEDERATION in classes
                assert CLASS_HLA_FEDERATE in classes
                fed_handles = {
                    i.federate_handle
                    for i in instances
                    if i.class_name == CLASS_HLA_FEDERATE
                }
                assert fed_a.handle in fed_handles
                assert fed_b.handle in fed_handles


@pytest.mark.spec
@pytest.mark.integration
async def test_spec_m27d_query_federate_attributes_unknown_returns_not_found() -> None:
    """Querying a handle that was never joined returns FederateAttributes(found=False)."""
    async with _rtid_subprocess() as url:
        fom = _make_fom()
        spec = FederationSpec(name="m27d-unknown", fom_modules=[str(fom)])
        async with RtiConnection.connect(url) as rti:
            async with rti.join_federation(spec, federate_name="alice") as fed:
                attrs = await fed.mom.query_federate_attributes(99_999)
                assert isinstance(attrs, FederateAttributes)
                assert attrs.found is False
