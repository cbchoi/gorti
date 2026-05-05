"""Test-side runner helpers for M12 W2 cut-3 SDK exposure.

Builds + spawns the ``rtid`` binary on a free port, dials a real gRPC
channel, and exposes a per-test ``RtidProcess`` context manager with
robust subprocess teardown (terminate → wait → kill on timeout). The
fixture mirrors the M5 cross-language harness (see
``pysdk/tests/spec/m5/_helpers.py``); the M12 variant is parameterized
on FOM module path + federation name only — the four spec tests reuse
the same harness with different FOM fixtures.

Why a dedicated helpers module: the M12 tests each spawn their own
rtid + drive 1-2 federates over real gRPC against the four cut-3
service groups. Sharing the spawn/teardown logic keeps every test
short and ensures uniform robustness against zombie processes.
"""

from __future__ import annotations

import asyncio
import contextlib
import socket
import subprocess
import sys
import tempfile
from collections.abc import AsyncIterator
from pathlib import Path
from types import TracebackType
from typing import Any

REPO_ROOT = Path(__file__).resolve().parents[4]
BIN_DIR = REPO_ROOT / "bin"
RTID_BINARY = BIN_DIR / "rtid"

_PYSDK = REPO_ROOT / "pysdk"
if str(_PYSDK) not in sys.path:
    sys.path.insert(0, str(_PYSDK))


def free_port() -> int:
    """Return an available TCP port on localhost.

    The bind/release race window is acceptable in CI tests — every
    test allocates two ports (rtid gRPC + Prometheus metrics) and
    teardown is bounded; even with parallel pytest workers the
    collision rate has been zero across the M5 harness's lifetime.
    """
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
        s.bind(("127.0.0.1", 0))
        return int(s.getsockname()[1])


def build_rtid() -> Path:
    """Build the rtid binary (idempotent). Returns the binary path.

    Builds via ``go build`` resolved through PATH so the helper runs
    on contributor laptops + CI without an env-var dance.
    """
    BIN_DIR.mkdir(parents=True, exist_ok=True)
    argv = ["go", "build", "-o", str(RTID_BINARY), "./rti/cmd/rtid"]  # noqa: S607
    subprocess.run(argv, cwd=REPO_ROOT, check=True)  # noqa: S603
    return RTID_BINARY


def _spawn_rtid(
    binary: Path,
    listen_port: int,
    metrics_port: int,
    *,
    save_dir: Path,
    log_dir: Path,
) -> subprocess.Popen[bytes]:
    """Launch rtid as a subprocess. Sync wrapper for ASYNC220 conformance.

    --save-dir is set per-test so save bundles are isolated and
    cleaned up via the tempdir lifecycle. --log-dir is similarly
    isolated; --log-level=warn suppresses noise.
    """
    return subprocess.Popen(  # noqa: S603 — controlled args
        [
            str(binary),
            "--listen", f":{listen_port}",
            "--metrics-listen", f":{metrics_port}",
            "--log-level", "warn",
            "--log-dir", str(log_dir),
            "--save-dir", str(save_dir),
        ],
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
        start_new_session=True,
    )


# noqa: ASYNC109 — ``timeout`` is the deadline for the entire poll
# loop, not a per-call I/O timeout. Same exemption as M5's _helpers.
async def _wait_for_grpc(port: int, *, timeout: float = 10.0) -> None:  # noqa: ASYNC109
    """Poll a TCP connect to ``port`` until success or timeout."""
    loop = asyncio.get_event_loop()
    deadline = loop.time() + timeout
    while loop.time() < deadline:
        try:
            reader, writer = await asyncio.wait_for(
                asyncio.open_connection("127.0.0.1", port), timeout=0.5
            )
            writer.close()
            with contextlib.suppress(BaseException):
                await writer.wait_closed()
            del reader
            return
        except (OSError, TimeoutError):
            await asyncio.sleep(0.1)
    raise TimeoutError(f"rtid never accepted on port {port}")


class RtidProcess:
    """Async context manager for a per-test rtid subprocess.

    Usage::

        async with RtidProcess() as rtid:
            url = rtid.url  # "grpc://127.0.0.1:<port>"
            ...

    Teardown:
      - On exit (normal or via exception): terminate the process,
        wait up to 5s, kill on timeout. The tempdir for save bundles
        + event logs is cleaned up automatically.
      - The ``start_new_session=True`` flag in ``_spawn_rtid`` keeps
        rtid out of the test runner's process group so a SIGINT to
        pytest doesn't reap rtid before this teardown can fire.
    """

    def __init__(self) -> None:
        self._proc: subprocess.Popen[bytes] | None = None
        self._tmpdir: tempfile.TemporaryDirectory[str] | None = None
        self._port: int | None = None

    @property
    def url(self) -> str:
        if self._port is None:
            raise RuntimeError("RtidProcess.url accessed before __aenter__")
        return f"grpc://127.0.0.1:{self._port}"

    @property
    def port(self) -> int:
        if self._port is None:
            raise RuntimeError("RtidProcess.port accessed before __aenter__")
        return self._port

    @property
    def save_dir(self) -> Path:
        if self._tmpdir is None:
            raise RuntimeError("RtidProcess.save_dir accessed before __aenter__")
        return Path(self._tmpdir.name) / "saves"

    async def __aenter__(self) -> RtidProcess:
        binary = build_rtid()
        port = free_port()
        metrics_port = free_port()
        if metrics_port == port:
            metrics_port = free_port()
        # Hold the tempdir for the lifetime of the process; cleanup
        # happens in __aexit__ via TemporaryDirectory.__exit__.
        self._tmpdir = tempfile.TemporaryDirectory(prefix="m12-rtid-")
        save_dir = Path(self._tmpdir.name) / "saves"
        log_dir = Path(self._tmpdir.name) / "logs"
        save_dir.mkdir(parents=True, exist_ok=True)
        log_dir.mkdir(parents=True, exist_ok=True)
        self._proc = _spawn_rtid(
            binary, port, metrics_port, save_dir=save_dir, log_dir=log_dir
        )
        self._port = port
        try:
            await _wait_for_grpc(port)
        except BaseException:
            await self._teardown()
            raise
        return self

    async def __aexit__(
        self,
        exc_type: type[BaseException] | None,
        exc: BaseException | None,
        tb: TracebackType | None,
    ) -> None:
        await self._teardown()

    async def _teardown(self) -> None:
        proc = self._proc
        self._proc = None
        if proc is not None:
            proc.terminate()
            try:
                proc.wait(timeout=5)
            except subprocess.TimeoutExpired:
                proc.kill()
                with contextlib.suppress(BaseException):
                    proc.wait(timeout=5)
        tmpdir = self._tmpdir
        self._tmpdir = None
        if tmpdir is not None:
            with contextlib.suppress(BaseException):
                tmpdir.cleanup()


def write_minimal_fom() -> Path:
    """Write a tiny FOM with one Vehicle object class to a temp file.

    Returns the file path. The file is intentionally NOT cleaned up
    by the helper (pytest tmpdir machinery would tie it to the test
    fixture and complicate the helper's reuse from a CLI). The OS
    temp dir is cleared on reboot, which is acceptable for a
    write-once test fixture.
    """
    xml = """<?xml version="1.0" encoding="UTF-8"?>
<objectModel xmlns="http://standards.ieee.org/IEEE1516-2010"
             xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
             xsi:schemaLocation="http://standards.ieee.org/IEEE1516-2010 IEEE1516-DIF-2010.xsd">
  <modelIdentification>
    <name>m12-minimal</name>
    <type>FOM</type>
    <version>1.0</version>
    <modificationDate>2026-05-03</modificationDate>
    <securityClassification>Unclassified</securityClassification>
    <description>Inline FOM for M12 W2 SDK exposure tests.</description>
    <useHistory>None</useHistory>
  </modelIdentification>
  <objects>
    <objectClass>
      <name>HLAobjectRoot</name>
      <objectClass>
        <name>Vehicle</name>
        <sharing>PublishSubscribe</sharing>
        <semantics>A simulated vehicle.</semantics>
        <attribute>
          <name>position</name>
          <dataType>HLAfloat64BE</dataType>
          <updateType>Periodic</updateType>
          <updateCondition>NA</updateCondition>
          <ownership>NoTransfer</ownership>
          <sharing>PublishSubscribe</sharing>
          <transportation>HLAreliable</transportation>
          <order>TimeStamp</order>
          <semantics>Position scalar.</semantics>
        </attribute>
      </objectClass>
    </objectClass>
  </objects>
  <interactions>
    <interactionClass>
      <name>HLAinteractionRoot</name>
    </interactionClass>
  </interactions>
</objectModel>
"""
    tmp = tempfile.NamedTemporaryFile(  # noqa: SIM115 — caller owns the path
        mode="w",
        suffix=".xml",
        prefix="m12-fom-",
        delete=False,
        encoding="utf-8",
    )
    tmp.write(xml)
    tmp.flush()
    tmp.close()
    return Path(tmp.name)


def write_ddm_fom() -> Path:
    """Write a FOM with a Vehicle class + two dimensions (X, Y).

    The dimensions belong to the implicit ``default`` routing space
    (cut-2 ddm.Manager treats every dimension as global; see
    ``rti/internal/ddm/manager.go``). Tests use the dimension handles
    to bind regions for the overlap-driven delivery scenario.
    """
    xml = """<?xml version="1.0" encoding="UTF-8"?>
<objectModel xmlns="http://standards.ieee.org/IEEE1516-2010"
             xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
             xsi:schemaLocation="http://standards.ieee.org/IEEE1516-2010 IEEE1516-DIF-2010.xsd">
  <modelIdentification>
    <name>m12-ddm</name>
    <type>FOM</type>
    <version>1.0</version>
    <modificationDate>2026-05-03</modificationDate>
    <securityClassification>Unclassified</securityClassification>
    <description>Inline FOM for M12 W2 DDM tests.</description>
    <useHistory>None</useHistory>
  </modelIdentification>
  <objects>
    <objectClass>
      <name>HLAobjectRoot</name>
      <objectClass>
        <name>Vehicle</name>
        <sharing>PublishSubscribe</sharing>
        <semantics>A simulated vehicle.</semantics>
        <attribute>
          <name>position</name>
          <dataType>HLAfloat64BE</dataType>
          <updateType>Periodic</updateType>
          <updateCondition>NA</updateCondition>
          <ownership>NoTransfer</ownership>
          <sharing>PublishSubscribe</sharing>
          <transportation>HLAreliable</transportation>
          <order>TimeStamp</order>
          <semantics>Position scalar.</semantics>
        </attribute>
      </objectClass>
    </objectClass>
  </objects>
  <interactions>
    <interactionClass>
      <name>HLAinteractionRoot</name>
    </interactionClass>
  </interactions>
  <dimensions>
    <dimension>
      <name>X</name>
      <upperBound>1000</upperBound>
      <normalization>linear 0..1000</normalization>
    </dimension>
    <dimension>
      <name>Y</name>
      <upperBound>1000</upperBound>
    </dimension>
  </dimensions>
</objectModel>
"""
    tmp = tempfile.NamedTemporaryFile(  # noqa: SIM115
        mode="w",
        suffix=".xml",
        prefix="m12-ddm-fom-",
        delete=False,
        encoding="utf-8",
    )
    tmp.write(xml)
    tmp.flush()
    tmp.close()
    return Path(tmp.name)


@contextlib.asynccontextmanager
async def two_federates(
    url: str, *, federation_name: str, fom_path: Path
) -> AsyncIterator[tuple[Any, Any]]:
    """Open two RtiConnections + two Federates against the same federation.

    Yields ``(fed_a, fed_b)``. Each federate is joined under its
    own connection so they have independent gRPC channels +
    independent stream-draining tasks.
    """
    from rti1516e.connection import FederationSpec, RtiConnection

    spec = FederationSpec(name=federation_name, fom_modules=[str(fom_path)])
    async with (
        RtiConnection.connect(url) as rti_a,
        RtiConnection.connect(url) as rti_b,
    ):
        async with (
            rti_a.join_federation(spec, federate_name="fed-a") as fed_a,
            rti_b.join_federation(spec, federate_name="fed-b") as fed_b,
        ):
            yield fed_a, fed_b
