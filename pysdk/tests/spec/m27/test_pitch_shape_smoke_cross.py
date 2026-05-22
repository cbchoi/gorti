"""M27 Phase D — cross-federate Pitch-shape smoke.

Broadens the M26 Phase G single-publisher smoke with a SUBSCRIBER
federate that uses ONLY Pitch-style ambassador methods (override
callback slots + evokeMultipleCallbacks loop). Verifies that
discoverObjectInstance / reflectAttributeValues / receiveInteraction
fire end-to-end through the Pitch surface — no reach-around into
events.async iteration.

This is the final verification that gorti's Layer 2 ambassador is
usable as a Pitch-style federate harness, not just a publisher
proof-of-concept.
"""

from __future__ import annotations

import asyncio
import contextlib
import shutil
import socket
import subprocess
import sys
import threading
from pathlib import Path
from typing import Any

import pytest

REPO_ROOT = Path(__file__).resolve().parents[4]
_PYSDK = REPO_ROOT / "pysdk"
if str(_PYSDK) not in sys.path:
    sys.path.insert(0, str(_PYSDK))

# ruff: noqa: E402

BIN_DIR = REPO_ROOT / "bin"
RTID_BINARY = BIN_DIR / "rtid"


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


def _run_in_thread(
    target: Any, kwargs: dict[str, Any]
) -> tuple[threading.Thread, dict[str, Any]]:
    """Launch ``target(**kwargs)`` on a daemon thread; result captured into
    a one-element dict the caller reads after .join()."""
    out: dict[str, Any] = {}

    def _worker() -> None:
        try:
            out["result"] = target(**kwargs)
        except BaseException as exc:  # noqa: BLE001
            out["error"] = exc

    t = threading.Thread(target=_worker, daemon=True)
    t.start()
    return t, out


@pytest.mark.spec
@pytest.mark.integration
def test_spec_m27d_pitch_shape_cross_federate() -> None:
    """Publisher + subscriber both using ONLY Pitch-style methods.

    The subscriber observes Discover + Reflect + Receive callbacks
    fired through the override slots — proving the Layer 2
    ambassador is sufficient for a port from Pitch / Portico / MAK
    without reaching into the async event stream.
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
        asyncio.run(_wait_for_grpc(listen_port))
        url = f"grpc://127.0.0.1:{listen_port}"

        # Import the smoke federate via the example dir's sys.path.
        sys.path.insert(0, str(REPO_ROOT / "examples" / "pitch-shape-smoke"))
        try:
            from smoke_federate import (  # type: ignore[import-not-found]
                run_publisher,
                run_subscriber,
            )
        finally:
            sys.path.pop(0)

        # Coordination events: publisher signals "joined" (so subscriber
        # can attempt to join the now-existing federation); subscriber
        # signals "subscribed" (so publisher knows it's safe to register
        # / update / interact without losing the events to the
        # not-yet-subscribed window).
        pub_joined = threading.Event()
        sub_subscribed = threading.Event()

        pub_thread, pub_out = _run_in_thread(
            run_publisher,
            {
                "url": url,
                "joined_event": pub_joined,
                "proceed_event": sub_subscribed,
                "resign_when_done": True,
            },
        )

        # Wait for the publisher to create the federation before the
        # subscriber tries to join. If the publisher fails, surface
        # promptly rather than the subscriber hanging on join.
        if not pub_joined.wait(timeout=15.0):
            raise AssertionError(
                "publisher did not signal 'joined' within 15s; pub_out=" + repr(pub_out)
            )

        sub_thread, sub_out = _run_in_thread(
            run_subscriber,
            {
                "url": url,
                "evoke_seconds": 5.0,
                "subscribed_event": sub_subscribed,
            },
        )

        pub_thread.join(timeout=30.0)
        sub_thread.join(timeout=30.0)
        assert not pub_thread.is_alive(), "publisher thread hung"
        assert not sub_thread.is_alive(), "subscriber thread hung"
        if "error" in pub_out:
            raise pub_out["error"]
        if "error" in sub_out:
            raise sub_out["error"]

        sub = sub_out["result"]
        pub = pub_out["result"]

        # Handle resolution works on both sides (FOM-deterministic).
        assert sub["vehicle_class"] == pub["vehicle_class"]
        assert sub["honk_class"] == pub["honk_class"]

        # Subscriber observed the publisher's registration.
        assert len(sub["discovered"]) >= 1, (
            f"subscriber did not fire discoverObjectInstance; sub={sub!r}"
        )
        # The Discover callback names the registered instance "car-7"
        # (the publisher's reserved name).
        discovered_names = [name for _, _, name in sub["discovered"]]
        assert "car-7" in discovered_names, (
            f"expected 'car-7' in discovered; got {discovered_names!r}"
        )

        # Subscriber observed the attribute update.
        assert len(sub["reflections"]) >= 1, (
            f"subscriber did not fire reflectAttributeValues; sub={sub!r}"
        )

        # Subscriber observed the Honk interaction.
        assert len(sub["interactions"]) >= 1, (
            f"subscriber did not fire receiveInteraction; sub={sub!r}"
        )
        interaction_classes = {cn for cn, _, _ in sub["interactions"]}
        assert "Honk" in interaction_classes, (
            f"expected 'Honk' interaction; got {interaction_classes!r}"
        )

        # Sync point announced reaches both federates (publisher
        # registered it; both are required when no explicit set is
        # passed, so both should fire the callback).
        # Publisher saw it.
        pub_sync_labels = [label for label, _ in pub["sync_announcements"]]
        assert "phase1" in pub_sync_labels, (
            f"publisher missed its own sync announcement; pub={pub!r}"
        )
    finally:
        proc.terminate()
        try:
            proc.wait(timeout=5)
        except subprocess.TimeoutExpired:
            proc.kill()
            proc.wait(timeout=5)
