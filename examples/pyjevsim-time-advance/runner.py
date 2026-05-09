"""End-to-end runner for the cross-process pyjevsim-time-advance example.

Spawns rtid + 3 Python regulator subprocesses (fast/normal/slow) and
verifies the M21 acceptance invariants. Mirrors
examples/pyjevsim-relay-cross-process/runner.py structurally.

Run from the repo root::

    python3 examples/pyjevsim-time-advance/runner.py

Exit code 0 on success, 1 on failure.
"""

from __future__ import annotations

import argparse
import asyncio
import contextlib
import json
import os
import shutil
import socket
import subprocess
import sys
import tempfile
from pathlib import Path
from typing import Any

_HERE = Path(__file__).resolve().parent
_REPO_ROOT = _HERE.parents[1]
_DEFAULT_RTID = _REPO_ROOT / "bin" / "rtid"
_REGULATOR_SCRIPT = _HERE / "regulator_main.py"

SPECS = [
    {"name": "fast", "lookahead": 0.5},
    {"name": "normal", "lookahead": 1.0},
    {"name": "slow", "lookahead": 2.0},
]


def _free_port() -> int:
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
        s.bind(("127.0.0.1", 0))
        return int(s.getsockname()[1])


async def _wait_for_grpc(port: int, timeout: float = 10.0) -> None:  # noqa: ASYNC109
    loop = asyncio.get_event_loop()
    deadline = loop.time() + timeout
    while loop.time() < deadline:
        try:
            reader, writer = await asyncio.wait_for(
                asyncio.open_connection("127.0.0.1", port), timeout=0.5,
            )
            writer.close()
            with contextlib.suppress(BaseException):
                await writer.wait_closed()
            del reader
            return
        except (OSError, TimeoutError):
            await asyncio.sleep(0.1)
    raise TimeoutError(f"rtid never accepted on port {port}")


def _build_rtid_if_missing(target: Path) -> Path:
    if target.exists():
        return target
    target.parent.mkdir(parents=True, exist_ok=True)
    subprocess.run(  # noqa: S603, S607
        ["go", "build", "-o", str(target), "./rti/cmd/rtid"],
        cwd=_REPO_ROOT,
        check=True,
    )
    return target


def _spawn_rtid(binary: Path, port: int, log_path: Path, save_dir: Path) -> subprocess.Popen[bytes]:
    save_dir.mkdir(parents=True, exist_ok=True)
    log_fh = log_path.open("wb")  # noqa: SIM115
    return subprocess.Popen(  # noqa: S603
        [
            str(binary),
            "--listen", f":{port}",
            "--metrics-listen", f":{_free_port()}",
            "--admin-listen", f"127.0.0.1:{_free_port()}",
            "--log-level", "warn",
            "--save-dir", str(save_dir),
        ],
        stdout=log_fh,
        stderr=subprocess.STDOUT,
        start_new_session=True,
    )


def _spawn_regulator(
    spec: dict[str, Any], url: str, result_path: Path, log_path: Path, cycles: int, tick_step: float,
) -> subprocess.Popen[bytes]:
    log_fh = log_path.open("wb")  # noqa: SIM115
    env = {**os.environ, "PYTHONUNBUFFERED": "1"}
    return subprocess.Popen(  # noqa: S603
        [
            sys.executable,
            str(_REGULATOR_SCRIPT),
            "--url", url,
            "--result", str(result_path),
            "--name", spec["name"],
            "--lookahead", str(spec["lookahead"]),
            "--cycles", str(cycles),
            "--tick-step", str(tick_step),
        ],
        env=env,
        stdout=log_fh,
        stderr=subprocess.STDOUT,
        start_new_session=True,
    )


def _terminate(proc: subprocess.Popen[bytes], timeout: float = 5.0) -> None:
    if proc.poll() is not None:
        return
    proc.terminate()
    try:
        proc.wait(timeout=timeout)
    except subprocess.TimeoutExpired:
        proc.kill()
        with contextlib.suppress(BaseException):
            proc.wait(timeout=timeout)


async def run_once(
    *,
    cycles: int = 10,
    tick_step: float = 3.0,
    rtid_binary: Path | None = None,
    keep_tempdir: bool = False,
) -> dict[str, Any]:
    binary = _build_rtid_if_missing(rtid_binary or _DEFAULT_RTID)
    port = _free_port()
    tmpdir = Path(tempfile.mkdtemp(prefix="pyjevsim-time-advance-"))
    fed_log_dir = tmpdir / "federate-logs"
    fed_log_dir.mkdir()

    rtid_proc = _spawn_rtid(binary, port, tmpdir / "rtid.log", tmpdir / "saves")
    fed_procs: list[subprocess.Popen[bytes]] = []
    try:
        await _wait_for_grpc(port)
        url = f"grpc://127.0.0.1:{port}"
        for spec in SPECS:
            fed_procs.append(_spawn_regulator(
                spec, url,
                tmpdir / f"{spec['name']}-result.json",
                fed_log_dir / f"{spec['name']}.log",
                cycles, tick_step,
            ))
        deadline = asyncio.get_event_loop().time() + 60.0
        for proc, spec in zip(fed_procs, SPECS, strict=True):
            remaining = max(0.1, deadline - asyncio.get_event_loop().time())
            try:
                await asyncio.to_thread(proc.wait, remaining)
            except subprocess.TimeoutExpired:
                print(f"runner: federate {spec['name']} timed out", file=sys.stderr)
    finally:
        for proc in fed_procs:
            _terminate(proc)
        _terminate(rtid_proc)

    results = {}
    for spec in SPECS:
        path = tmpdir / f"{spec['name']}-result.json"
        if path.exists():
            results[spec["name"]] = json.loads(path.read_text())
    out: dict[str, Any] = {"per_federate": results, "cycles": cycles, "rtid_port": port}
    if keep_tempdir:
        out["tempdir"] = str(tmpdir)
    else:
        with contextlib.suppress(BaseException):
            shutil.rmtree(tmpdir, ignore_errors=True)
    return out


def verify(result: dict[str, Any]) -> tuple[bool, str]:
    expected = result["cycles"]
    per = result["per_federate"]
    if len(per) != 3:
        return False, f"got {len(per)} federate results, want 3"
    for name, r in per.items():
        if len(r["grants"]) != expected:
            return False, f"{name}: {len(r['grants'])} grants, want {expected}"
        for i in range(1, len(r["grants"])):
            if r["grants"][i] < r["grants"][i-1]:
                return False, f"{name}: grant {i} regressed below grant {i-1}"
    return True, "ok"


def main(argv: list[str] | None = None) -> int:
    p = argparse.ArgumentParser()
    p.add_argument("--cycles", type=int, default=10)
    p.add_argument("--tick-step", type=float, default=3.0)
    p.add_argument("--keep-tempdir", action="store_true")
    args = p.parse_args(argv)
    try:
        result = asyncio.run(run_once(
            cycles=args.cycles,
            tick_step=args.tick_step,
            keep_tempdir=args.keep_tempdir,
        ))
    except Exception as exc:  # noqa: BLE001
        print(f"runner: {exc}", file=sys.stderr)
        return 1
    ok, msg = verify(result)
    summary = " ".join(
        f"{n}={len(r['grants'])}" for n, r in result["per_federate"].items()
    )
    print(f"runner: cycles={result['cycles']} per-federate-grants={{{summary}}} verify={msg}")
    if result.get("tempdir"):
        print(f"runner: tempdir kept at {result['tempdir']}")
    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main())
