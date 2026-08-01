"""M28 W4 — IEEE 1516 typed-handle smoke federate end-to-end.

Runs ``examples/ieee1516e-typed-smoke/smoke_federate.py`` against a live
rtid subprocess. Verifies that a federate written using ONLY the
IEEE 1516 service-style typed handle + typed collection API (no bare ints, no
bare ``list`` / ``dict`` declarations at the ambassador call sites)
can drive a full lifecycle:

  - lookup handles (typed return values, §10.2)
  - publish using ``AttributeHandleSet`` (typed declaration)
  - reserve object instance name (§6.1-6.5)
  - register by handle, returns typed ``ObjectInstanceHandle``
  - update using ``AttributeHandleValueMap`` (typed map)
  - send interaction using ``ParameterHandleValueMap`` from factory
  - register sync point
  - evoke callbacks, resign

This is the M28 sibling of
``pysdk/tests/spec/m26/test_ieee1516e_ambassador_smoke.py`` — the M26 smoke
proves the bare-int / bare-list API still works (back-compat); this
smoke proves the typed-form API works too (IEEE 1516 source compatibility).
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
@pytest.mark.skipif(
    shutil.which("rtid") is None and not RTID_BINARY.exists() and shutil.which("go") is None,
    reason="no rtid binary and no go toolchain to build one",
)
def test_spec_m28_ieee1516e_typed_smoke() -> None:
    """examples/ieee1516e-typed-smoke/smoke_federate.py runs end-to-end."""
    if shutil.which("go") is None and not RTID_BINARY.exists():
        pytest.skip("go toolchain not on PATH and no prebuilt rtid; cannot run")
    binary = RTID_BINARY if RTID_BINARY.exists() else _build_rtid()
    listen_port = _free_port()
    metrics_port = _free_port()
    if metrics_port == listen_port:
        metrics_port = _free_port()
    proc = _spawn_rtid(binary, listen_port, metrics_port)
    try:
        asyncio.run(_wait_for_grpc(listen_port))
        url = f"grpc://127.0.0.1:{listen_port}"

        # Import the typed smoke under a unique module name so the M26
        # ieee1516e-ambassador-smoke's ``smoke_federate`` module (if already
        # imported in the same pytest process) isn't returned from
        # ``sys.modules`` cache.
        import importlib.util
        spec_path = REPO_ROOT / "examples" / "ieee1516e-typed-smoke" / "smoke_federate.py"
        spec = importlib.util.spec_from_file_location(
            "ieee1516e_typed_smoke_federate", spec_path
        )
        assert spec is not None and spec.loader is not None
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        run_publisher = module.run_publisher

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
        assert not t.is_alive(), "typed smoke federate did not finish within 30s"
        if "e" in err_box:
            raise err_box["e"]

        result = result_box["r"]
        assert isinstance(result, dict)

        # Handle services — every lookup returns >0.
        assert result["vehicle_class"] > 0
        assert result["position_attr"] > 0
        assert result["velocity_attr"] > 0
        assert result["honk_class"] > 0
        assert result["volume_param"] > 0
        # Distinct handles for distinct attributes.
        assert result["position_attr"] != result["velocity_attr"]

        # Typed-form witnesses — every variable in user code was the
        # correct typed handle / collection class.
        assert result["vehicle_class_is_typed"] is True
        assert result["position_attr_is_typed"] is True
        assert result["honk_class_is_typed"] is True
        assert result["volume_param_is_typed"] is True
        assert result["object_handle_is_typed"] is True
        assert result["attr_set_is_typed"] is True
        assert result["attr_values_is_typed"] is True
        assert result["param_values_is_typed"] is True

        # Reservation flow.
        assert "car-7" in result["reserved_callbacks"], (
            f"expected 'car-7' in reservation_ok callbacks, "
            f"got {result['reserved_callbacks']!r}"
        )
        assert result["reservation_evoke_fired"] is True

        # Register-by-handle succeeded.
        assert result["object_handle"] > 0

        # Sync point announcement.
        labels = [a[0] for a in result["sync_announcements"]]
        assert "phase1" in labels, f"expected 'phase1' announcement, got {labels!r}"
    finally:
        proc.terminate()
        try:
            proc.wait(timeout=5)
        except subprocess.TimeoutExpired:
            proc.kill()
            proc.wait(timeout=5)
