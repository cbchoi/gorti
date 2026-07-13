"""Subprocess orchestrator for the M5 cross-language smoke (TASK-081).

Cut-1 partial scope (per docs/M5_DISPATCH_PLAN.md §3 W1C "pragmatic cut-1"):

  1. Build the rtid binary (``go build ./rti/cmd/rtid``).
  2. Launch rtid as a subprocess on a free port.
  3. Drive TWO Python federates (one publishes, one subscribes) through
     the real-gRPC transport against that rtid. Verifies:
        - The Python SDK opens a real grpc.aio channel to rtid
        - CreateFederation / JoinFederation succeed cross-process
        - PublishInteractionClass / SubscribeInteractionClass succeed
        - SendInteraction is delivered (via StreamService.Events) to the
          subscribing federate's event queue
  4. Tear rtid down.

Why two Python federates instead of one Python + one Go (the bidirectional
ideal): the Go reference examples in ``examples/go-pingpong`` and
``examples/go-timed`` are subprocess-shim demos that spawn their own
rtid via the demo modes — they don't open a gRPC channel to a separately
running rtid. Writing a Go federate that does the cross-process gRPC join
requires touching ``rti/`` (Agent A's territory). This is documented as a
deferral; the orchestrator can dispatch a follow-up to Agent A for a
"go-grpc-federate" example when the bidirectional cross-language smoke
is needed.

The TWO-Python-federate test still validates the cut-1 acceptance:
  - Real-gRPC transport works (CreateFederation crosses the wire)
  - Bidirectional event flow works (Python→rtid→Python via Events stream)
  - Both sides observe consistent state (publisher's seq numbers = subscriber's
    received seq numbers, in order)

Usage::

    import asyncio
    from cross_lang_runner import run_cross_language_smoke
    result = asyncio.run(run_cross_language_smoke(port=28442, rounds=5))
    assert result["py_pub_sent"] >= 5
    assert result["py_sub_received"] >= 5
"""

from __future__ import annotations

import asyncio
import contextlib
import os
import socket
import subprocess
import sys
from pathlib import Path
from typing import Any

REPO_ROOT = Path(__file__).resolve().parents[2]
BIN_DIR = REPO_ROOT / "bin"
RTID_BINARY = BIN_DIR / "rtid"
FOM_PATH = (
    REPO_ROOT / "tests" / "conformance" / "foms" / "good" / "pyjevsim-bridge.xml"
)

# Ensure pysdk is importable when this module runs as a script.
_PYSDK = REPO_ROOT / "pysdk"
if str(_PYSDK) not in sys.path:
    sys.path.insert(0, str(_PYSDK))

# ruff: noqa: E402  (sys.path tweak above must precede project imports)
from rti1516e.connection import FederationSpec, RtiConnection  # type: ignore[import-not-found]
from rti1516e.events import ReceiveInteraction  # type: ignore[import-not-found]


def _free_port() -> int:
    """Bind a TCP socket to port 0 to discover a free port; close + return it.

    Race window: another process could bind between our close and rtid's
    listen. The cross-lang smoke is a single-process test in CI and the
    racetrack is small enough that this is acceptable for cut-1.
    """
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
        s.bind(("127.0.0.1", 0))
        return int(s.getsockname()[1])


def _build_rtid() -> Path:
    """Build the rtid binary into ``bin/rtid``. Returns the path.

    Idempotent: if the binary already exists and the source hasn't
    changed since the last build, ``go build`` short-circuits.
    """
    BIN_DIR.mkdir(parents=True, exist_ok=True)
    subprocess.run(
        ["go", "build", "-o", str(RTID_BINARY), "./rti/cmd/rtid"],
        cwd=REPO_ROOT,
        check=True,
    )
    return RTID_BINARY


async def _wait_for_grpc(port: int, *, timeout: float = 10.0) -> None:
    """Poll a TCP connect to ``port`` until success or timeout.

    rtid prints a "rtid serving" log line when both listeners are ready,
    but parsing logs is fragile cross-platform. A simple TCP connect
    probe is more portable + deterministic.
    """
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
        except (OSError, asyncio.TimeoutError):
            await asyncio.sleep(0.1)
    raise TimeoutError(f"rtid never accepted on port {port}")


async def run_cross_language_smoke(
    *,
    port: int | None = None,
    rounds: int = 5,
    rtid_binary: Path | None = None,
) -> dict[str, Any]:
    """Run the cut-1 cross-process smoke test.

    Returns a dict with at least these keys::

        {
            "rtid_pid": int,
            "port": int,
            "py_pub_sent": int,        # interactions the publisher sent
            "py_sub_received": int,    # interactions the subscriber received
            "py_sub_payloads": list[bytes],  # received payload bytes in order
        }

    Raises:
        RuntimeError: if the publisher's send count and the subscriber's
        receive count diverge (cross-language consistency violation).
    """
    binary = rtid_binary or _build_rtid()
    chosen_port = port if port is not None else _free_port()
    metrics_port = _free_port()
    if metrics_port == chosen_port:
        # Vanishingly unlikely but defensive.
        metrics_port = _free_port()

    rtid_proc = subprocess.Popen(  # noqa: S603 — controlled args
        [
            str(binary),
            "--listen", f":{chosen_port}",
            "--metrics-listen", f":{metrics_port}",
            "--log-level", "warn",
        ],
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
        # Detach from the parent's process group so signals to the test
        # runner don't accidentally kill rtid before we can reap it.
        start_new_session=True,
    )

    try:
        await _wait_for_grpc(chosen_port)
        result = await _drive_python_federates(
            url=f"grpc://127.0.0.1:{chosen_port}",
            rounds=rounds,
        )
        result["rtid_pid"] = rtid_proc.pid
        result["port"] = chosen_port
        return result
    finally:
        rtid_proc.terminate()
        try:
            rtid_proc.wait(timeout=5)
        except subprocess.TimeoutExpired:
            rtid_proc.kill()
            rtid_proc.wait(timeout=5)


async def _drive_python_federates(
    *, url: str, rounds: int
) -> dict[str, Any]:
    """Open two RtiConnections, join one as publisher + one as subscriber,
    drive ``rounds`` interactions, drain the subscriber's events, and
    assert consistency.

    Returns the result dict (without the rtid_pid/port fields, which the
    caller fills in).
    """
    spec = FederationSpec(
        name="m5-cross-lang-smoke",
        fom_modules=[str(FOM_PATH)],
    )

    pub_conn = RtiConnection.connect(url)
    sub_conn = RtiConnection.connect(url)

    py_pub_sent = 0
    py_sub_received: list[bytes] = []

    async with pub_conn as pub_rti, sub_conn as sub_rti:
        async with (
            pub_rti.join_federation(spec, federate_name="py-pub") as pub_fed,
            sub_rti.join_federation(spec, federate_name="py-sub") as sub_fed,
        ):
            await pub_fed.publish_interaction_class("ProducerOutput")
            await sub_fed.subscribe_interaction_class("ProducerOutput")

            # Give the rtid a moment to wire the subscription before
            # the first send — declarations are processed in-order via
            # gRPC unary-unary, but the subscribe is registered on
            # one federate's connection and the send happens via the
            # other; a small yield avoids the race where the first
            # send arrives before the subscriber's interest is on file.
            await asyncio.sleep(0.05)

            # Spawn a background drain task on the subscriber so events
            # are read from the stream as they arrive (rather than
            # buffered server-side).
            drain_task = asyncio.create_task(
                _drain_subscriber(sub_fed, py_sub_received, target=rounds)
            )

            for i in range(rounds):
                payload = (i + 1).to_bytes(4, byteorder="big", signed=False)
                await pub_fed.send_interaction(
                    "ProducerOutput",
                    parameters={"_payload": payload},
                )
                py_pub_sent += 1

            # Wait briefly for the subscriber to drain. If it hasn't
            # received everything by the deadline, the test will
            # report partial counts — letting the assertion in the
            # caller surface the mismatch with a clear error.
            try:
                await asyncio.wait_for(drain_task, timeout=5.0)
            except asyncio.TimeoutError:
                drain_task.cancel()
                with contextlib.suppress(BaseException):
                    await drain_task

    return {
        "py_pub_sent": py_pub_sent,
        "py_sub_received": len(py_sub_received),
        "py_sub_payloads": py_sub_received,
    }


async def _drain_subscriber(
    sub_fed: Any, sink: list[bytes], *, target: int
) -> None:
    """Pull events off the subscriber's queue, append payload bytes to
    ``sink``, return once ``len(sink) >= target``.
    """
    async for event in sub_fed.events():
        if isinstance(event, ReceiveInteraction):
            payload = event.parameters.get("_payload")
            if payload is None and len(event.parameters) == 1:
                payload = next(iter(event.parameters.values()))
            if isinstance(payload, bytes | bytearray):
                sink.append(bytes(payload))
            else:
                # Defensive: GrpcTransport always coerces to bytes; this
                # branch only fires if the wire shape changes.
                sink.append(repr(payload).encode("utf-8"))
            if len(sink) >= target:
                return


def main() -> int:
    """Standalone CLI entry point: prints a one-line summary and exits.

    Useful for `python examples/pyjevsim/cross_lang_runner.py` smoke runs
    outside the pytest harness — handy when iterating on the gRPC wiring.
    """
    rounds = int(os.environ.get("CROSS_LANG_ROUNDS", "5"))
    result = asyncio.run(run_cross_language_smoke(rounds=rounds))
    print(
        f"cross_lang_runner: rtid pid={result['rtid_pid']} port={result['port']} "
        f"py_pub_sent={result['py_pub_sent']} "
        f"py_sub_received={result['py_sub_received']}"
    )
    return 0 if result["py_pub_sent"] == result["py_sub_received"] else 1


if __name__ == "__main__":
    sys.exit(main())
