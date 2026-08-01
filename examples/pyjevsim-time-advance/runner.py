"""End-to-end runner for the cross-process pyjevsim-time-advance example.

Spawns rtid + 3 Python regulator subprocesses (fast/normal/slow) and
verifies the M21 acceptance invariants. Mirrors
examples/pyjevsim-relay-cross-process/runner.py structurally.

Use ``run.sh`` or ``run.ps1`` from any current directory. Direct Python
execution is also supported::

    python3 examples/pyjevsim-time-advance/runner.py

Logs are written to a unique ``.run/run-*`` directory
AND streamed to the parent's stderr with [rtid] / [<federate>] prefixes.
Pass ``--workdir DIR`` to override; ``--no-keep-workdir`` to delete on exit.

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
_REPO_RTID = _REPO_ROOT / "bin" / "rtid"
_LOCAL_RTID = _HERE / ".run" / "bin" / "rtid"
_REGULATOR_SCRIPT = _HERE / "regulator_main.py"

# Shared tee helper.
sys.path.insert(0, str(_REPO_ROOT / "examples"))
from _log_tee import LogTee  # noqa: E402

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


def _is_windows() -> bool:
    return sys.platform.startswith("win")


def _with_native_suffix(path: Path) -> Path:
    if _is_windows() and path.suffix.lower() != ".exe":
        return path.with_suffix(".exe")
    return path


def _default_rtid() -> Path:
    for candidate in (
        _with_native_suffix(_REPO_RTID),
        _REPO_RTID,
        _with_native_suffix(_LOCAL_RTID),
        _LOCAL_RTID,
    ):
        if candidate.is_file():
            return candidate
    return _with_native_suffix(_LOCAL_RTID)


def _build_rtid_if_missing(target: Path) -> Path:
    target = _with_native_suffix(target)
    if target.is_file():
        return target
    go = shutil.which("go")
    if go is None:
        raise RuntimeError(
            f"rtid not found at {target}; install Go or pass --rtid-binary"
        )
    target.parent.mkdir(parents=True, exist_ok=True)
    subprocess.run(  # noqa: S603
        [go, "build", "-o", str(target), "./rti/cmd/rtid"],
        cwd=_REPO_ROOT,
        check=True,
    )
    return target


def _resolve_workdir(workdir: Path | None) -> Path:
    if workdir is not None:
        return workdir.resolve()
    root = _HERE / ".run"
    root.mkdir(parents=True, exist_ok=True)
    return Path(tempfile.mkdtemp(prefix="run-", dir=root))


def _check_runtime_dependencies() -> None:
    pysdk = _REPO_ROOT / "pysdk"
    if str(pysdk) not in sys.path:
        sys.path.insert(0, str(pysdk))
    try:
        import google.protobuf  # noqa: F401, PLC0415
        import grpc  # noqa: F401, PLC0415
        import rti1516e.connection  # noqa: F401, PLC0415
    except (ImportError, RuntimeError) as exc:
        raise RuntimeError(
            "Python runtime dependencies are unavailable; install them with "
            f"'{sys.executable} -m pip install -e {pysdk}' ({exc})"
        ) from exc


def _popen_kwargs() -> dict[str, Any]:
    kwargs: dict[str, Any] = {
        "stdout": subprocess.PIPE,
        "stderr": subprocess.STDOUT,
    }
    if not _is_windows():
        kwargs["start_new_session"] = True
    return kwargs


def _spawn_rtid_with_tee(
    binary: Path, port: int, log_path: Path, save_dir: Path,
    log_dir: Path, log_level: str,
) -> tuple[subprocess.Popen[bytes], LogTee]:
    save_dir.mkdir(parents=True, exist_ok=True)
    log_dir.mkdir(parents=True, exist_ok=True)
    proc = subprocess.Popen(  # noqa: S603
        [
            str(binary),
            "--listen", f":{port}",
            "--metrics-listen", f":{_free_port()}",
            "--admin-listen", f"127.0.0.1:{_free_port()}",
            "--log-level", log_level,
            "--log-format", "text",
            "--audit-replay-plugin", "event-journal",
            "--log-dir", str(log_dir),
            "--save-dir", str(save_dir),
        ],
        **_popen_kwargs(),
    )
    tee = LogTee(proc, log_path=log_path, prefix="rtid")
    tee.start()
    return proc, tee


def _spawn_regulator_with_tee(
    spec: dict[str, Any],
    url: str,
    result_path: Path,
    log_path: Path,
    cycles: int,
    tick_step: float,
) -> tuple[subprocess.Popen[bytes], LogTee]:
    env = {**os.environ, "PYTHONUNBUFFERED": "1"}
    proc = subprocess.Popen(  # noqa: S603
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
        **_popen_kwargs(),
    )
    tee = LogTee(proc, log_path=log_path, prefix=spec["name"])
    tee.start()
    return proc, tee


def _terminate(proc: subprocess.Popen[bytes], timeout: float = 5.0) -> None:
    if proc.poll() is not None:
        return
    with contextlib.suppress(ProcessLookupError, OSError):
        proc.terminate()
    try:
        proc.wait(timeout=timeout)
    except subprocess.TimeoutExpired:
        with contextlib.suppress(ProcessLookupError, OSError):
            proc.kill()
        with contextlib.suppress(BaseException):
            proc.wait(timeout=timeout)


async def run_once(
    *,
    cycles: int = 10,
    tick_step: float = 3.0,
    rtid_binary: Path | None = None,
    workdir: Path | None = None,
    keep_workdir: bool = True,
    federate_timeout: float = 60.0,
    log_level: str = "info",
) -> dict[str, Any]:
    if cycles < 1:
        raise ValueError("cycles must be at least 1")
    if tick_step <= max(float(spec["lookahead"]) for spec in SPECS):
        raise ValueError("tick_step must exceed the largest federate lookahead")
    if federate_timeout <= 0:
        raise ValueError("federate_timeout must be positive")
    _check_runtime_dependencies()
    binary = _build_rtid_if_missing(rtid_binary or _default_rtid())
    port = _free_port()
    workdir = _resolve_workdir(workdir)
    workdir.mkdir(parents=True, exist_ok=True)
    result_paths = {
        spec["name"]: workdir / f"{spec['name']}-result.json" for spec in SPECS
    }
    for path in result_paths.values():
        path.unlink(missing_ok=True)
        path.with_suffix(path.suffix + ".tmp").unlink(missing_ok=True)

    print(f"[runner] working directory: {workdir}", flush=True)
    print(f"[runner] rtid listen port: {port}", flush=True)

    rtid_proc: subprocess.Popen[bytes] | None = None
    rtid_tee: LogTee | None = None
    fed_procs: list[tuple[subprocess.Popen[bytes], LogTee, str]] = []
    exit_codes: dict[str, int | None] = {}
    try:
        rtid_proc, rtid_tee = _spawn_rtid_with_tee(
            binary, port, workdir / "rtid.log", workdir / "saves",
            workdir / "eventlog", log_level,
        )
        await _wait_for_grpc(port)
        url = f"grpc://127.0.0.1:{port}"
        for spec in SPECS:
            p, t = _spawn_regulator_with_tee(
                spec, url,
                result_paths[spec["name"]],
                workdir / f"{spec['name']}.log",
                cycles, tick_step,
            )
            fed_procs.append((p, t, spec["name"]))
        deadline = asyncio.get_event_loop().time() + federate_timeout
        for proc, _tee, name in [(p, t, n) for p, t, n in fed_procs]:
            remaining = max(0.1, deadline - asyncio.get_event_loop().time())
            try:
                await asyncio.to_thread(proc.wait, remaining)
            except subprocess.TimeoutExpired:
                print(f"[runner] federate {name} timed out", file=sys.stderr)
            exit_codes[name] = proc.returncode
    finally:
        for proc, tee, name in fed_procs:
            _terminate(proc)
            tee.join(timeout=1.0)
            exit_codes[name] = proc.returncode
        if rtid_proc is not None:
            _terminate(rtid_proc)
        if rtid_tee is not None:
            rtid_tee.join(timeout=1.0)

    results: dict[str, Any] = {}
    for spec in SPECS:
        path = result_paths[spec["name"]]
        if path.exists():
            results[spec["name"]] = json.loads(path.read_text(encoding="utf-8"))
    out: dict[str, Any] = {
        "per_federate": results,
        "cycles": cycles,
        "tick_step": tick_step,
        "rtid_port": port,
        "exit_codes": exit_codes,
        "workdir": str(workdir),
    }
    if not keep_workdir:
        with contextlib.suppress(BaseException):
            shutil.rmtree(workdir, ignore_errors=True)
        out["workdir"] = ""
    return out


def verify(result: dict[str, Any]) -> tuple[bool, str]:
    expected = result["cycles"]
    per = result["per_federate"]
    failed = {
        name: code for name, code in result.get("exit_codes", {}).items() if code != 0
    }
    if failed:
        return False, f"federate exit failure(s): {failed}"
    if len(per) != 3:
        return False, f"got {len(per)} federate results, want 3"
    expected_times = [i * result["tick_step"] for i in range(1, expected + 1)]
    for name, r in per.items():
        if len(r["grants"]) != expected:
            return False, f"{name}: {len(r['grants'])} grants, want {expected}"
        if r.get("requested") != expected_times:
            return False, f"{name}: requested={r.get('requested')} want {expected_times}"
        if r["grants"] != expected_times:
            return False, f"{name}: grants={r['grants']} want {expected_times}"
        if r.get("model_cycles") != expected:
            return False, f"{name}: model_cycles={r.get('model_cycles')} want {expected}"
        if r.get("output_calls") != expected:
            return False, f"{name}: output_calls={r.get('output_calls')} want {expected}"
    return True, "ok"


def main(argv: list[str] | None = None) -> int:
    p = argparse.ArgumentParser()
    p.add_argument("--cycles", type=int, default=10)
    p.add_argument("--tick-step", type=float, default=3.0)
    p.add_argument("--rtid-binary", type=Path, default=None)
    p.add_argument(
        "--workdir", type=Path, default=None,
        help="working directory (default: a unique .run/run-* directory)",
    )
    p.add_argument("--no-keep-workdir", action="store_true")
    p.add_argument("--federate-timeout", type=float, default=60.0)
    p.add_argument(
        "--log-level", default="info",
        choices=["debug", "info", "warn", "error"],
    )
    args = p.parse_args(argv)
    try:
        result = asyncio.run(run_once(
            cycles=args.cycles,
            tick_step=args.tick_step,
            rtid_binary=args.rtid_binary,
            workdir=args.workdir,
            keep_workdir=not args.no_keep_workdir,
            federate_timeout=args.federate_timeout,
            log_level=args.log_level,
        ))
    except Exception as exc:  # noqa: BLE001
        print(f"[runner] {exc}", file=sys.stderr)
        return 1
    ok, msg = verify(result)
    summary = " ".join(
        f"{n}={len(r['grants'])}" for n, r in result["per_federate"].items()
    )
    print(f"[runner] cycles={result['cycles']} per-federate-grants={{{summary}}} verify={msg}")
    if result.get("workdir"):
        print(f"[runner] workdir: {result['workdir']}")
    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main())
