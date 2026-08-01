"""Session fixtures for the IVCT-inspired conformance subset (FA-1).

Design notes:

- ``rtid_url`` is session-scoped: ONE rtid subprocess serves every test
  in the pytest session. It builds nothing — ``$REPO/bin/rtid`` must
  exist (CI builds it in the ``go`` stage; locally:
  ``go build -o bin/rtid ./rti/cmd/rtid``).
- rtid state persists for the whole session, so every test derives a
  UNIQUE federation name from its own test name via the
  ``federation_name`` fixture (name-reuse collisions across tests were a
  recurring failure mode in earlier suites).
- The gRPC port is chosen by the kernel (bind :0). rtid also binds a
  Prometheus metrics port (default :9090) and an AdminService port
  (default localhost:8443); both would collide with any concurrently
  running rtid, so metrics is relocated to a second free port and admin
  is disabled outright (``--admin-listen ""``).
"""

from __future__ import annotations

import hashlib
import socket
import subprocess
import sys
import time
import uuid
from collections.abc import Iterator
from pathlib import Path

import pytest

HERE = Path(__file__).resolve().parent
REPO_ROOT = HERE.parents[3]
PYSDK = REPO_ROOT / "pysdk"
GENERATED = PYSDK / "rti1516e" / "_generated"
for _p in (str(HERE), str(PYSDK), str(GENERATED)):
    if _p not in sys.path:
        sys.path.insert(0, _p)

RTID_BINARY = REPO_ROOT / "bin" / "rtid"


def pytest_configure(config: pytest.Config) -> None:
    # Belt and braces with pytest.ini: register the markers here too so
    # running a single file with an explicit -c elsewhere stays warning-free.
    config.addinivalue_line(
        "markers", "conformance: RTI conformance test (runs against a live rtid)"
    )
    config.addinivalue_line(
        "markers",
        "ivct_subset: IVCT-inspired Python-native conformance subset (NOT IVCT)",
    )


def _free_port() -> int:
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
        s.bind(("127.0.0.1", 0))
        return int(s.getsockname()[1])


def _wait_for_tcp(port: int, *, timeout: float = 15.0) -> None:
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        try:
            with socket.create_connection(("127.0.0.1", port), timeout=0.5):
                return
        except OSError:
            time.sleep(0.1)
    raise TimeoutError(f"rtid did not accept connections on :{port} within {timeout}s")


@pytest.fixture(scope="session")
def rtid_url() -> Iterator[str]:
    """Spawn one rtid for the session; yield its grpc:// URL."""
    if not RTID_BINARY.exists():
        pytest.fail(
            f"{RTID_BINARY} missing — build it first: "
            "go build -o bin/rtid ./rti/cmd/rtid "
            "(CI's `go` stage does this before the `ivct` stage runs)"
        )
    listen_port = _free_port()
    metrics_port = _free_port()
    while metrics_port == listen_port:  # pragma: no cover — kernel rarely repeats
        metrics_port = _free_port()
    proc = subprocess.Popen(  # noqa: S603
        [
            str(RTID_BINARY),
            "--listen", f"127.0.0.1:{listen_port}",
            "--metrics-listen", f"127.0.0.1:{metrics_port}",
            "--admin-listen", "",  # empty disables AdminService (see rtid main.go)
            "--log-level", "warn",
        ],
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
        start_new_session=True,
        cwd=str(REPO_ROOT),
    )
    try:
        _wait_for_tcp(listen_port)
        yield f"grpc://127.0.0.1:{listen_port}"
    finally:
        proc.terminate()
        try:
            proc.wait(timeout=5)
        except subprocess.TimeoutExpired:
            proc.kill()
            proc.wait(timeout=5)


@pytest.fixture
def federation_name(request: pytest.FixtureRequest) -> str:
    """Per-TEST unique federation name.

    rtid keeps federations for the life of the session process, so
    every test gets its own federation derived from the test name plus
    a random suffix (guards against reruns within one session, e.g.
    ``--lf`` or parametrized reuse of the same node name).

    gorti's eventlog rejects federation names longer than 32 bytes, so
    the test name is folded into a short stable digest instead of being
    embedded verbatim: ivct-<8-hex test-name digest>-<8-hex random>.
    """
    digest = hashlib.sha256(request.node.name.encode()).hexdigest()[:8]
    return f"ivct-{digest}-{uuid.uuid4().hex[:8]}"
