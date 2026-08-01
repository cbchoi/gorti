"""Test-side runner helpers for M5 mode-verification (TASK-082).

Builds and launches the ``rtid`` binary as a subprocess on a free port,
drives two Python federates (publisher + subscriber) over real gRPC,
sends a single timestamped interaction, captures the
``ReceiveInteraction`` event seen by the subscriber, and returns the
captured timestamp.

Mirrors the shape of ``examples/pyjevsim/cross_lang_runner.py`` (W1C
output) but parameterized over ``federation_mode`` ("verbose" |
"best-effort") and ``interaction_order`` ("TimeStamp" | "Receive") so the
two M5-mode tests can drive different scenarios from a single source.

Why interactions, not attribute updates: the cut-1 GrpcTransport in
``rti1516e/_transport.py`` dispatches ``send_interaction`` but
intentionally records-only ``update_attributes`` (see the cut-1 notes in
that file). The interaction-side ``deliveryTimestampForInteraction`` in
``rti/internal/object/interaction.go`` exercises the SAME RO-vs-TSO
decision path as ``deliveryTimestampForAttributes`` (mode + per-class
order lookup), so swapping the dimension keeps the M5 modes contract
exercised end-to-end without depending on object-update wire dispatch
that cut-1 deferred.

The Python side passes the federation mode through to the wire via
``FederationSpec(mode=...)``; the Go-side handler stores the mode on the
federation and the registry consults it during fan-out. The FOM order is
a per-class declaration the FOM parser already accepts — we write a tiny
FOM XML to a temp file at test time so we don't need to grow the
conformance fixtures ( territory).
"""

from __future__ import annotations

import asyncio
import contextlib
import socket
import subprocess
import sys
import tempfile
from pathlib import Path
from typing import Any

REPO_ROOT = Path(__file__).resolve().parents[4]
BIN_DIR = REPO_ROOT / "bin"
RTID_BINARY = BIN_DIR / "rtid"

# Make the pysdk package importable when this helper is consumed from
# pytest. The pytest invocation runs from inside pysdk/ so rti1516e is on
# sys.path already; the explicit insertion here is defensive for callers
# that might import this module from a different cwd.
_PYSDK = REPO_ROOT / "pysdk"
if str(_PYSDK) not in sys.path:
    sys.path.insert(0, str(_PYSDK))

# ruff: noqa: E402  (sys.path tweak above must precede project imports)
from rti1516e.connection import FederationSpec, RtiConnection
from rti1516e.events import ReceiveInteraction


def _free_port() -> int:
    """Return an available TCP port. Race window is acceptable in CI tests."""
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
        s.bind(("127.0.0.1", 0))
        return int(s.getsockname()[1])


def _build_rtid() -> Path:
    """Build the rtid binary (idempotent). Returns the binary path.

    Uses the bare command name "go" (resolved via PATH) rather than an
    absolute path so the same helper works in CI containers and on
    contributor laptops without an env-var dance. The S607 lint is
    suppressed at the argv site because it is the literal that triggers
    the rule.
    """
    BIN_DIR.mkdir(parents=True, exist_ok=True)
    argv = ["go", "build", "-o", str(RTID_BINARY), "./rti/cmd/rtid"]  # noqa: S607 — PATH-resolved by design
    subprocess.run(argv, cwd=REPO_ROOT, check=True)  # noqa: S603 — argv built from literals
    return RTID_BINARY


def _spawn_rtid(
    binary: Path, listen_port: int, metrics_port: int
) -> subprocess.Popen[bytes]:
    """Launch rtid as a subprocess. Sync wrapper for ASYNC220 conformance.

    Detaches via ``start_new_session=True`` so test-runner SIGINT does
    not reap rtid before the async caller's ``finally`` block can
    terminate it cleanly.
    """
    return subprocess.Popen(  # noqa: S603 — controlled args
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


# noqa: ASYNC109 — the ``timeout`` parameter is the deadline for the
# entire poll loop, not a per-call I/O timeout (``asyncio.timeout``
# wraps a single awaitable). Mirrors cross_lang_runner._wait_for_grpc
# in shape; that helper has the same exemption.
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


def _write_modes_fom(*, interaction_order: str) -> Path:
    """Write a one-class FOM XML to a temp file with the requested order.

    Returns the file path. The file lives in the system temp directory
    and is intentionally NOT cleaned up by the helper — pytest's tmp dir
    handling would tie its lifecycle to the test fixture; we keep the
    helper standalone so it can be invoked from a CLI for hand-driving.

    The FOM declares a single user-defined interaction class
    ``ModesProbe`` whose ``<order>`` is the requested value. Schema is
    the same shape ``tests/conformance/foms/good/pyjevsim-bridge.xml``
    uses; the Go parser accepts unknown ``<order>`` text without
    rejecting (it's stored as an opaque string for the FOM handle to
    interpret), so "Receive" passes parser validation cleanly.
    """
    if interaction_order not in ("TimeStamp", "Receive"):
        raise ValueError(
            f"interaction_order must be 'TimeStamp' or 'Receive', got {interaction_order!r}"
        )
    xml = f"""<?xml version="1.0" encoding="UTF-8"?>
<objectModel xmlns="http://standards.ieee.org/IEEE1516-2010"
             xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
             xsi:schemaLocation="http://standards.ieee.org/IEEE1516-2010 IEEE1516-DIF-2010.xsd">
  <modelIdentification>
    <name>m5-modes-probe</name>
    <type>FOM</type>
    <version>1.0</version>
    <modificationDate>2026-05-03</modificationDate>
    <securityClassification>Unclassified</securityClassification>
    <description>Inline FOM for TASK-082 mode verification.</description>
    <useHistory>None</useHistory>
  </modelIdentification>
  <objects>
    <objectClass>
      <name>HLAobjectRoot</name>
    </objectClass>
  </objects>
  <interactions>
    <interactionClass>
      <name>HLAinteractionRoot</name>
      <interactionClass>
        <name>ModesProbe</name>
        <sharing>PublishSubscribe</sharing>
        <transportation>HLAreliable</transportation>
        <order>{interaction_order}</order>
        <semantics>Probe interaction for TASK-082 RO/TSO verification.</semantics>
        <parameter>
          <name>payload</name>
          <dataType>HLAinteger32BE</dataType>
          <semantics>Probe sequence number.</semantics>
        </parameter>
      </interactionClass>
    </interactionClass>
  </interactions>
</objectModel>
"""
    tmp = tempfile.NamedTemporaryFile(  # noqa: SIM115 — handle reused by callers
        mode="w",
        suffix=".xml",
        prefix="m5-modes-fom-",
        delete=False,
        encoding="utf-8",
    )
    tmp.write(xml)
    tmp.flush()
    tmp.close()
    return Path(tmp.name)


async def run_modes_smoke(
    *,
    federation_mode: str,
    interaction_order: str,
    timestamp: float,
    port: int | None = None,
    rtid_binary: Path | None = None,
) -> dict[str, Any]:
    """Drive the mode-verification smoke against a fresh rtid subprocess.

    Parameters:
        federation_mode: "verbose" or "best-effort". Passed through to
            ``FederationSpec.mode``; the Python SDK encodes it onto the
            ``CreateFederationRequest.mode`` proto field. The Go-side
            federation manager persists it on the federation state and
            the object registry consults it when computing per-event
            delivery semantics.
        interaction_order: "TimeStamp" or "Receive". Encoded into the
            inline FOM's ``<order>`` element on the ``ModesProbe``
            interaction class.
        timestamp: logical timestamp the publisher attaches to the sent
            interaction. The subscriber's ``ReceiveInteraction.timestamp``
            field is what the test asserts against.
        port: explicit gRPC port for rtid; ``None`` picks a free one.
        rtid_binary: explicit path to a pre-built rtid; ``None`` builds
            it via ``go build`` (idempotent).

    Returns a dict with at least::

        {
            "rtid_pid": int,
            "port": int,
            "received_timestamp": float | None,  # the subscriber-observed ts
            "received_count": int,
            "fom_path": str,
        }

    The "received_timestamp" key is the SDK-translated ``timestamp``
    field on ``ReflectAttributeValues`` / ``ReceiveInteraction`` —
    ``None`` indicates the rtid stripped the timestamp (RO delivery)
    while a non-``None`` float indicates TSO delivery.
    """
    if federation_mode not in ("verbose", "best-effort"):
        raise ValueError(
            f"federation_mode must be 'verbose' or 'best-effort', got {federation_mode!r}"
        )

    binary = rtid_binary or _build_rtid()
    chosen_port = port if port is not None else _free_port()
    metrics_port = _free_port()
    if metrics_port == chosen_port:
        metrics_port = _free_port()
    fom_path = _write_modes_fom(interaction_order=interaction_order)

    # Spawn the rtid subprocess via a sync helper so the ASYNC220 lint
    # (subprocess in async function) has a single, marked-up exemption
    # site rather than spreading the noqa comments through this entry
    # point. Matches cross_lang_runner's documented pattern.
    rtid_proc = _spawn_rtid(binary, chosen_port, metrics_port)

    try:
        await _wait_for_grpc(chosen_port)
        result = await _drive_modes_federates(
            url=f"grpc://127.0.0.1:{chosen_port}",
            fom_path=fom_path,
            federation_mode=federation_mode,
            timestamp=timestamp,
        )
        result["rtid_pid"] = rtid_proc.pid
        result["port"] = chosen_port
        result["fom_path"] = str(fom_path)
        return result
    finally:
        rtid_proc.terminate()
        try:
            rtid_proc.wait(timeout=5)
        except subprocess.TimeoutExpired:
            rtid_proc.kill()
            rtid_proc.wait(timeout=5)


async def _drive_modes_federates(
    *,
    url: str,
    fom_path: Path,
    federation_mode: str,
    timestamp: float,
) -> dict[str, Any]:
    """Open two RtiConnections, declare pub/sub on ``ModesProbe``, send one
    interaction with ``timestamp``, and return the subscriber's first
    received-interaction timestamp.

    Each test gets a unique federation name (suffixed with the mode +
    order) so back-to-back invocations from the same pytest process don't
    collide on FederationAlreadyExists in a long-lived rtid (we spawn a
    fresh rtid per call so this is belt-and-suspenders).
    """
    federation_name = f"m5-modes-{federation_mode}"
    spec = FederationSpec(
        name=federation_name,
        fom_modules=[str(fom_path)],
        mode=federation_mode,
    )

    pub_conn = RtiConnection.connect(url)
    sub_conn = RtiConnection.connect(url)

    received: list[ReceiveInteraction] = []

    async with pub_conn as pub_rti, sub_conn as sub_rti:
        async with (
            pub_rti.join_federation(spec, federate_name="modes-pub") as pub_fed,
            sub_rti.join_federation(spec, federate_name="modes-sub") as sub_fed,
        ):
            await pub_fed.publish_interaction_class("ModesProbe")
            await sub_fed.subscribe_interaction_class("ModesProbe")

            # Allow the subscribe to register before the send; same race
            # window discussed in cross_lang_runner._drive_python_federates.
            await asyncio.sleep(0.05)

            drain_task = asyncio.create_task(
                _drain_one_receive(sub_fed, received)
            )

            await pub_fed.send_interaction(
                "ModesProbe",
                parameters={"_payload": (1).to_bytes(4, byteorder="big", signed=False)},
                timestamp=timestamp,
            )

            try:
                await asyncio.wait_for(drain_task, timeout=5.0)
            except TimeoutError:
                drain_task.cancel()
                with contextlib.suppress(BaseException):
                    await drain_task

    if not received:
        return {
            "received_timestamp": None,
            "received_count": 0,
        }
    first = received[0]
    return {
        "received_timestamp": first.timestamp,
        "received_count": len(received),
    }


async def _drain_one_receive(
    sub_fed: Any, sink: list[ReceiveInteraction]
) -> None:
    """Pull events off the subscriber's queue until one ReceiveInteraction
    lands, then return. The caller wraps this in ``wait_for`` so we don't
    spin if the event never arrives."""
    async for event in sub_fed.events():
        if isinstance(event, ReceiveInteraction):
            sink.append(event)
            return
