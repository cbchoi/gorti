"""M26 Phase G — Pitch-shape smoke federate end-to-end.

Runs examples/pitch-shape-smoke/smoke_federate.py against a live
rtid subprocess. Verifies that a federate written using ONLY the
Pitch-style Rti1516eAmbassador methods (no reach-around into
sdk.ownership.* / sdk.ddm.* modules) can drive a full lifecycle:

  - lookup handles (M25 Phase B)
  - publish + subscribe by class name
  - reserve object instance name (M26 Phase F)
  - register object by handle, update attributes
  - send interaction
  - register sync point (M25 Phase C)
  - evoke callbacks (M26 Phase E)
  - resign

This is the answer to option (2) of the original
"verify gorti matches Pitch semantics" task — the smoke is the
federate we verify against.
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
            _, w = await asyncio.wait_for(
                asyncio.open_connection("127.0.0.1", port), timeout=0.5
            )
            w.close()
            with contextlib.suppress(BaseException):
                await w.wait_closed()
            return
        except (OSError, TimeoutError):
            await asyncio.sleep(0.1)
    raise TimeoutError(f"rtid did not accept connections on :{port} within {timeout}s")


@pytest.mark.spec
@pytest.mark.integration
def test_spec_m26_pitch_shape_smoke() -> None:
    """examples/pitch-shape-smoke/smoke_federate.py runs end-to-end."""
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

        # Import + run in a separate thread because Rti1516eAmbassador
        # spawns its own event loop in a background thread; calling
        # connect/disconnect from pytest's main thread is fine. Use a
        # thread isolate the test from pytest's loop machinery.
        sys.path.insert(0, str(REPO_ROOT / "examples" / "pitch-shape-smoke"))
        try:
            from smoke_federate import run_publisher
        finally:
            sys.path.pop(0)

        result_box: dict[str, object] = {}
        err_box: dict[str, BaseException] = {}

        def _worker() -> None:
            try:
                result_box["r"] = run_publisher(url)
            except BaseException as exc:  # noqa: BLE001
                err_box["e"] = exc

        t = threading.Thread(target=_worker, daemon=True)
        t.start()
        t.join(timeout=30)
        assert not t.is_alive(), "smoke federate did not finish within 30s"
        if "e" in err_box:
            raise err_box["e"]

        result = result_box["r"]
        assert isinstance(result, dict)

        # Handle services (Phase B).
        assert result["vehicle_class"] > 0
        assert result["position_attr"] > 0
        assert result["velocity_attr"] > 0
        assert result["honk_class"] > 0
        # The two attributes have distinct handles.
        assert result["position_attr"] != result["velocity_attr"]

        # Reservation flow (Phase F).
        assert "car-7" in result["reserved_callbacks"], (
            f"expected 'car-7' in reservation_ok callbacks, "
            f"got {result['reserved_callbacks']!r}"
        )
        assert result["reservation_evoke_fired"] is True

        # Register-by-handle succeeded.
        assert result["object_handle"] > 0

        # Sync point announcement (Phase C/D).
        labels = [a[0] for a in result["sync_announcements"]]
        assert "phase1" in labels, f"expected 'phase1' announcement, got {labels!r}"
    finally:
        proc.terminate()
        try:
            proc.wait(timeout=5)
        except subprocess.TimeoutExpired:
            proc.kill()
            proc.wait(timeout=5)
