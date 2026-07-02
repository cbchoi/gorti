"""M17.7 — cross-language Pitch smoke: C++ publisher ↔ Python subscriber.

Spawns:
  1. rtid as a subprocess on a free port
  2. examples/cpp-pitch-smoke/cpp_pitch_publisher (built via
     cppsdk/build) pointed at the same rtid
  3. a Python Rti1516eAmbassador subscriber that joins the same
     federation, subscribes to Vehicle.Position/Velocity + Honk,
     drives evokeMultipleCallbacks until it observes Discover +
     Reflect + Receive

Pins the cross-language compatibility story for the C++ SDK: a
federate using only Pitch-style C++ ambassador methods can fan out
its events to a Python subscriber using only Pitch-style Python
ambassador methods, both wired against the same gorti rtid.
"""

from __future__ import annotations

import shutil
import socket
import subprocess
import sys
import time
from pathlib import Path
from typing import Any

import pytest

REPO_ROOT = Path(__file__).resolve().parents[4]
_PYSDK = REPO_ROOT / "pysdk"
if str(_PYSDK) not in sys.path:
    sys.path.insert(0, str(_PYSDK))

# ruff: noqa: E402
from rti1516e.standard import Rti1516eAmbassador

BIN_DIR = REPO_ROOT / "bin"
RTID_BINARY = BIN_DIR / "rtid"
CPP_PUBLISHER = (
    REPO_ROOT / "cppsdk" / "build" / "examples" / "cpp-pitch-smoke"
    / "cpp_pitch_publisher"
)
CPP_FOM = REPO_ROOT / "examples" / "cpp-pitch-smoke" / "federation.fom.xml"
FEDERATION = "cpp-pitch-smoke"


def _free_port() -> int:
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
        s.bind(("127.0.0.1", 0))
        return int(s.getsockname()[1])


def _build_rtid() -> Path:
    BIN_DIR.mkdir(parents=True, exist_ok=True)
    subprocess.run(  # noqa: S603
        ["go", "build", "-o", str(RTID_BINARY), "./rti/cmd/rtid"],  # noqa: S607
        cwd=REPO_ROOT, check=True,
    )
    return RTID_BINARY


def _spawn_rtid(binary: Path, port: int, mport: int) -> subprocess.Popen[bytes]:
    return subprocess.Popen(  # noqa: S603
        [str(binary),
         "--listen", f":{port}",
         "--metrics-listen", f":{mport}",
         "--log-level", "warn"],
        stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
        start_new_session=True,
    )


def _wait_for_grpc(port: int, *, timeout: float = 10.0) -> None:
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        try:
            with socket.create_connection(("127.0.0.1", port), timeout=0.5):
                return
        except OSError:
            time.sleep(0.1)
    raise TimeoutError(f"rtid did not accept on :{port} within {timeout}s")


class _RecordingAmbassador(Rti1516eAmbassador):
    def __init__(self) -> None:
        super().__init__()
        self.discovered: list[tuple[int, str, str]] = []
        self.reflections: list[tuple[int, dict[str, Any]]] = []
        self.interactions: list[tuple[str, dict[str, Any]]] = []

    def discoverObjectInstance(  # noqa: N802
        self,
        object_handle: int,
        class_name: str,
        instance_name: str,
        object_class: int | None = None,  # M39 typed-handle parity (§6.9)
    ) -> None:
        self.discovered.append((object_handle, class_name, instance_name))

    def reflectAttributeValues(  # noqa: N802
        self,
        object_handle: int,
        values: dict[str, Any],
        timestamp: float | None,
        attribute_values: dict[Any, bytes] | None = None,  # M39 (§6.11)
    ) -> None:
        self.reflections.append((object_handle, dict(values)))

    def receiveInteraction(  # noqa: N802
        self,
        class_name: str,
        parameters: dict[str, Any],
        timestamp: float | None,
    ) -> None:
        self.interactions.append((class_name, dict(parameters)))


@pytest.mark.spec
@pytest.mark.integration
def test_spec_m17_7_cpp_publisher_python_subscriber() -> None:
    """A Python subscriber receives Discover + Reflect + Receive
    emitted by a C++ publisher, both joined to the same gorti
    federation."""
    if shutil.which("go") is None:
        pytest.skip("go toolchain not on PATH; cannot build rtid")
    if not CPP_PUBLISHER.exists():
        pytest.skip(
            f"cpp_pitch_publisher not built at {CPP_PUBLISHER}; "
            "run `cmake --build cppsdk/build` first"
        )

    binary = _build_rtid()
    port = _free_port()
    mport = _free_port()
    if mport == port:
        mport = _free_port()
    rtid = _spawn_rtid(binary, port, mport)
    url = f"grpc://127.0.0.1:{port}"

    try:
        _wait_for_grpc(port)

        # Subscriber joins FIRST and pre-subscribes, so the C++
        # publisher's Discover lands on its event stream.
        sub = _RecordingAmbassador()
        sub.connect(sub, url)
        sub.createFederationExecution(FEDERATION, [str(CPP_FOM)])
        sub.joinFederationExecution(
            "py-subscriber", FEDERATION,
            additional_fom_modules=[str(CPP_FOM)],
        )
        vehicle = sub.getObjectClassHandle("Vehicle")
        pos = sub.getAttributeHandle(vehicle, "Position")
        vel = sub.getAttributeHandle(vehicle, "Velocity")
        honk = sub.getInteractionClassHandle("Honk")
        sub.subscribeObjectClassAttributes(vehicle, [pos, vel])
        sub.subscribeInteractionClass(honk)

        # Spawn the C++ publisher.
        cpp = subprocess.Popen(  # noqa: S603
            [str(CPP_PUBLISHER),
             "--url", url,
             "--federation", FEDERATION,
             "--fom", str(CPP_FOM),
             "--hold", "3"],
            stdout=subprocess.PIPE, stderr=subprocess.PIPE,
        )

        # Drain subscriber callbacks for up to 10 s.
        deadline = time.monotonic() + 10.0
        while time.monotonic() < deadline:
            sub.evokeMultipleCallbacks(approx_min_time=0.1, approx_max_time=0.5)
            if sub.discovered and sub.reflections and sub.interactions:
                break

        # Cleanly join the C++ publisher subprocess so we can read
        # its stdout for diagnostics if the asserts below fail.
        cpp_stdout, cpp_stderr = cpp.communicate(timeout=10.0)

        assert sub.discovered, (
            f"no discoverObjectInstance fired; cpp stdout={cpp_stdout!r}, "
            f"stderr={cpp_stderr!r}"
        )
        assert sub.reflections, (
            f"no reflectAttributeValues fired; cpp stdout={cpp_stdout!r}"
        )
        assert sub.interactions, (
            f"no receiveInteraction fired; cpp stdout={cpp_stdout!r}"
        )

        # The C++ publisher registered "cpp-car-1" specifically.
        names = [n for _, _, n in sub.discovered]
        assert "cpp-car-1" in names, (
            f"expected 'cpp-car-1' in discovered names; got {names!r}"
        )

        sub.resignFederationExecution()
        sub.disconnect()
        # destroyFederationExecution isn't on the pysdk Layer-2
        # ambassador surface yet (a Cut-2 gap noted for the Python
        # SDK); the federation tears down with rtid's exit anyway.
    finally:
        rtid.terminate()
        try:
            rtid.wait(timeout=5)
        except subprocess.TimeoutExpired:
            rtid.kill()
            rtid.wait(timeout=5)
