"""M4 replay gate — Python orchestrates the rtid replay round-trip and
asserts byte-identical reproduction (M6 W2 Part C).

The original M4 scaffold skipped this test pending real-rtid integration.
M6 W2 closes that gap. The simplest robust Python harness — adopted here
— mirrors the Go-side ``examples/go-pingpong/replay_test.go``:

  1. Build the rtid binary (``go build ./rti/cmd/rtid``).
  2. Launch ``rtid -mode=pingpong-demo --log-dir=A --pingpong-deterministic``
     to produce a deterministic source event log at ``A/<federation>.log``.
  3. Launch ``rtid -mode=replay-from-log --replay-input=A/<fed>.log
     --log-dir=B`` to replay the source through a fresh rtid; rtid writes
     a captured copy at ``B/<federation>.log``.
  4. Assert sha256(A/<fed>.log) == sha256(B/<fed>.log) — byte-identical
     reproduction.

Why pingpong-demo (Go-side) instead of the pyjevsim Python harness:
the Python harness uses an in-process driver (``InProcessTransport``)
that does NOT persist event logs to disk — there's no rtid round-trip
to replay through. Wiring the Python harness against a real rtid via
gRPC (essentially the M5 cross-language smoke path) would re-test the
gRPC plumbing rather than the replay contract. The pingpong-demo path
exercises the rtid log writer + replayer end-to-end, which is what the
M4 NFR-DET-2 ("byte-identical replay") really demands. The Python
side's role is to orchestrate + assert.

Implements: FR-EVT-3, NFR-DET-2.
"""

from __future__ import annotations

import hashlib
import shutil
import subprocess
from pathlib import Path

import pytest

REPO_ROOT = Path(__file__).resolve().parents[4]
RTID_PKG = "./rti/cmd/rtid"
FEDERATION = "pyspec-m4-replay"


def _build_rtid_binary(target: Path) -> Path:
    """Compile rtid into ``target``. Raises ``CalledProcessError`` on
    build failure (so the test surfaces a clear go-build error instead of
    a confusing missing-binary one)."""
    target.parent.mkdir(parents=True, exist_ok=True)
    subprocess.run(  # noqa: S603 — controlled args
        ["go", "build", "-o", str(target), RTID_PKG],  # noqa: S607 — ``go`` is the standard toolchain
        cwd=REPO_ROOT,
        check=True,
    )
    return target


def _sha256(path: Path) -> str:
    """sha256 hex digest of the file at ``path``."""
    h = hashlib.sha256()
    with path.open("rb") as f:
        for chunk in iter(lambda: f.read(65536), b""):
            h.update(chunk)
    return h.hexdigest()


@pytest.mark.spec
def test_spec_m4_python_example_replays_byte_identical(tmp_path: Path) -> None:
    """Drive the rtid replay round-trip from Python and assert byte parity.

    Skipped when the ``go`` toolchain is unavailable — the test cannot
    build rtid. Otherwise the test runs end-to-end:

      - Build rtid.
      - Run pingpong-demo deterministic into ``dirA/``.
      - Replay from ``dirA/<fed>.log`` into ``dirB/`` via rtid.
      - sha256(dirA/<fed>.log) MUST equal sha256(dirB/<fed>.log).

    The rtid replay path (``runReplayFromFile`` in
    ``rti/cmd/rtid/replay.go``) explicitly preserves the source
    header's ``CreatedAtNs`` so the captured log is fully byte-equal,
    not just body-equal.
    """
    if shutil.which("go") is None:
        pytest.skip("go toolchain not on PATH; cannot build rtid for replay test")

    # Use a stable bin directory under the repo's bin/ so concurrent
    # tests reuse the same compiled binary; a tempdir would force a
    # rebuild per test run.
    bin_dir = REPO_ROOT / "bin"
    bin_dir.mkdir(parents=True, exist_ok=True)
    rtid_bin = _build_rtid_binary(bin_dir / "rtid")

    dir_a = tmp_path / "logsA"
    dir_b = tmp_path / "logsB"
    dir_a.mkdir()
    dir_b.mkdir()

    # Step 1: produce the source log via the pingpong demo. Deterministic
    # mode pins the FakeClock so the body bytes are reproducible across
    # runs — necessary for the assertion below to be meaningful as a
    # determinism + replay-fidelity check.
    rounds = 50
    proc1 = subprocess.run(  # noqa: S603 — controlled args, rtid_bin from build
        [
            str(rtid_bin),
            "-mode=pingpong-demo",
            f"-pingpong-rounds={rounds}",
            f"-pingpong-federation={FEDERATION}",
            f"-log-dir={dir_a}",
            "-pingpong-deterministic",
            "-log-format=text",
        ],
        cwd=REPO_ROOT,
        capture_output=True,
        timeout=60,
        check=False,
    )
    assert proc1.returncode == 0, (
        f"rtid pingpong-demo failed: rc={proc1.returncode}\n"
        f"stdout={proc1.stdout!r}\nstderr={proc1.stderr!r}"
    )
    src_log = dir_a / f"{FEDERATION}.log"
    assert src_log.is_file(), f"expected source log at {src_log}"
    src_size = src_log.stat().st_size
    assert src_size > 0, "source log is empty"

    # Step 2: replay through fresh rtid. The replayer reads the source
    # header, opens a fresh writer with the same federation/mode/seed
    # and a fixed clock pinned to the source's CreatedAtNs, then feeds
    # every record through. Output goes to dir_b/<federation>.log.
    proc2 = subprocess.run(  # noqa: S603 — controlled args, rtid_bin from build
        [
            str(rtid_bin),
            "-mode=replay-from-log",
            f"-replay-input={src_log}",
            f"-log-dir={dir_b}",
            "-log-format=text",
        ],
        cwd=REPO_ROOT,
        capture_output=True,
        timeout=60,
        check=False,
    )
    assert proc2.returncode == 0, (
        f"rtid replay-from-log failed: rc={proc2.returncode}\n"
        f"stdout={proc2.stdout!r}\nstderr={proc2.stderr!r}"
    )
    dst_log = dir_b / f"{FEDERATION}.log"
    assert dst_log.is_file(), f"expected captured log at {dst_log}"

    # Step 3: byte-identical assertion via sha256 (cheaper to display
    # in failure messages than diffing 50KB of binary content).
    src_sum = _sha256(src_log)
    dst_sum = _sha256(dst_log)
    assert src_sum == dst_sum, (
        f"replay sha256 mismatch:\n"
        f"  source  ({src_log}, {src_log.stat().st_size}B): {src_sum}\n"
        f"  replay  ({dst_log}, {dst_log.stat().st_size}B): {dst_sum}"
    )


