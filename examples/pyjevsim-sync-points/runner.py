"""End-to-end cross-process runner for the sync-points example.

Spawns rtid + 3 Python participant subprocesses that rendezvous at
named sync labels (start_simulation, end_simulation), exchange Tick
heartbeats during the running phase, and resign.

Run from the repo root::

    python3 examples/pyjevsim-sync-points/runner.py

Optional flags::

    --running-ticks N    ticks between start and end (default 10)
    --tick-period P      wall-clock seconds per tick (default 0.05)
    --rtid-binary PATH   override rtid binary location
    --keep-tempdir       leave logs + result JSON for inspection

Exit code 0 on success (every federate achieved + synchronized at
both labels and the running-phase Tick counts match), 1 on failure.

Mirrors examples/pyjevsim-relay-cross-process/runner.py and
examples/pyjevsim/runner.py structurally so the three runners share
a mental model.
"""

from __future__ import annotations

import argparse
import asyncio
import contextlib
import datetime as _dt
import json
import os
import shutil
import socket
import subprocess
import sys
from pathlib import Path
from typing import Any

_HERE = Path(__file__).resolve().parent
_REPO_ROOT = _HERE.parents[1]
_BIN_DIR = _REPO_ROOT / "bin"
_DEFAULT_RTID = _BIN_DIR / "rtid"

PARTICIPANT_SCRIPT = _HERE / "participant_main.py"
PARTICIPANT_NAMES = ("alpha", "beta", "gamma")

# Shared tee helper.
sys.path.insert(0, str(_REPO_ROOT / "examples"))
from _log_tee import LogTee  # noqa: E402


def _resolve_workdir(workdir: Path | None) -> Path:
    if workdir is not None:
        return workdir
    stamp = _dt.datetime.now().strftime("%Y%m%d-%H%M%S")
    return _HERE / ".run" / stamp


def _is_windows() -> bool:
    return sys.platform.startswith("win")


def _free_port() -> int:
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
        s.bind(("127.0.0.1", 0))
        return int(s.getsockname()[1])


def _build_rtid(target: Path) -> Path:
    target.parent.mkdir(parents=True, exist_ok=True)
    subprocess.run(  # noqa: S603, S607
        ["go", "build", "-o", str(target), "./rti/cmd/rtid"],
        cwd=_REPO_ROOT,
        check=True,
    )
    return target


def _ensure_rtid(binary: Path) -> Path:
    if binary.exists():
        return binary
    return _build_rtid(binary)


async def _wait_for_grpc(port: int, *, timeout: float = 10.0) -> None:  # noqa: ASYNC109
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


def _spawn_rtid_with_tee(
    binary: Path, listen_port: int, metrics_port: int, admin_port: int,
    *, save_dir: Path, log_dir: Path, log_path: Path, log_level: str,
) -> tuple[subprocess.Popen[bytes], LogTee]:
    kwargs: dict[str, Any] = {"stdout": subprocess.PIPE, "stderr": subprocess.STDOUT}
    if not _is_windows():
        kwargs["start_new_session"] = True
    proc = subprocess.Popen(  # noqa: S603
        [
            str(binary),
            "--listen", f":{listen_port}",
            "--metrics-listen", f":{metrics_port}",
            "--admin-listen", f"127.0.0.1:{admin_port}",
            "--log-level", log_level,
            "--log-format", "text",
            "--log-dir", str(log_dir),
            "--save-dir", str(save_dir),
        ],
        **kwargs,
    )
    tee = LogTee(proc, log_path=log_path, prefix="rtid")
    tee.start()
    return proc, tee


def _spawn_participant_with_tee(
    *, name: str, url: str, result_path: Path, running_ticks: int,
    tick_period: float, join_settle: float, rendezvous_timeout: float,
    log_path: Path,
) -> tuple[subprocess.Popen[bytes], LogTee]:
    log_path.parent.mkdir(parents=True, exist_ok=True)
    kwargs: dict[str, Any] = {"stdout": subprocess.PIPE, "stderr": subprocess.STDOUT}
    if not _is_windows():
        kwargs["start_new_session"] = True
    env = {**os.environ, "PYTHONUNBUFFERED": "1"}
    proc = subprocess.Popen(  # noqa: S603
        [
            sys.executable,
            str(PARTICIPANT_SCRIPT),
            "--url", url,
            "--result", str(result_path),
            "--name", name,
            "--running-ticks", str(running_ticks),
            "--tick-period", str(tick_period),
            "--join-settle", str(join_settle),
            "--rendezvous-timeout", str(rendezvous_timeout),
        ],
        env=env,
        **kwargs,
    )
    tee = LogTee(proc, log_path=log_path, prefix=name)
    tee.start()
    return proc, tee


def _terminate(proc: subprocess.Popen[bytes], *, timeout: float = 5.0) -> None:
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
    running_ticks: int = 10,
    tick_period: float = 0.05,
    rtid_binary: Path | None = None,
    workdir: Path | None = None,
    keep_workdir: bool = True,
    federate_timeout: float = 60.0,
    log_level: str = "info",
) -> dict[str, Any]:
    binary = _ensure_rtid(rtid_binary or _DEFAULT_RTID)
    listen_port = _free_port()
    metrics_port = _free_port()
    admin_port = _free_port()
    while metrics_port == listen_port:
        metrics_port = _free_port()
    while admin_port in (listen_port, metrics_port):
        admin_port = _free_port()

    workdir = _resolve_workdir(workdir)
    save_dir = workdir / "saves"
    log_dir = workdir / "eventlog"
    for d in (save_dir, log_dir, workdir):
        d.mkdir(parents=True, exist_ok=True)

    result_paths = {n: workdir / f"{n}-result.json" for n in PARTICIPANT_NAMES}

    print(f"[runner] working directory: {workdir}", flush=True)
    print(f"[runner] rtid listen port: {listen_port}", flush=True)

    rtid_proc: subprocess.Popen[bytes] | None = None
    rtid_tee: LogTee | None = None
    fed_procs: list[tuple[subprocess.Popen[bytes], LogTee, str]] = []

    try:
        rtid_proc, rtid_tee = _spawn_rtid_with_tee(
            binary, listen_port, metrics_port, admin_port,
            save_dir=save_dir, log_dir=log_dir,
            log_path=workdir / "rtid.log",
            log_level=log_level,
        )
        await _wait_for_grpc(listen_port)
        url = f"grpc://127.0.0.1:{listen_port}"

        for name in PARTICIPANT_NAMES:
            p, t = _spawn_participant_with_tee(
                name=name,
                url=url,
                result_path=result_paths[name],
                running_ticks=running_ticks,
                tick_period=tick_period,
                join_settle=1.5,
                rendezvous_timeout=20.0,
                log_path=workdir / f"{name}.log",
            )
            fed_procs.append((p, t, name))

        deadline = asyncio.get_event_loop().time() + federate_timeout
        for proc, _tee, name in [(p, t, n) for p, t, n in fed_procs]:
            remaining = max(0.1, deadline - asyncio.get_event_loop().time())
            try:
                await asyncio.to_thread(proc.wait, remaining)
            except subprocess.TimeoutExpired:
                print(
                    f"[runner] participant {name} did not exit within "
                    f"{remaining:.1f}s; will be force-terminated",
                    file=sys.stderr,
                )

    finally:
        for proc, tee, _name in fed_procs:
            _terminate(proc)
            tee.join(timeout=1.0)
        if rtid_proc is not None:
            _terminate(rtid_proc)
        if rtid_tee is not None:
            rtid_tee.join(timeout=1.0)

        per_name = {n: _read_result(result_paths[n]) for n in PARTICIPANT_NAMES}
        result = {
            "running_ticks": running_ticks,
            "labels": ["start_simulation", "end_simulation"],
            "per_federate": per_name,
            "workdir": str(workdir),
            "rtid_port": listen_port,
            "rtid_pid": rtid_proc.pid if rtid_proc is not None else None,
        }

        if not keep_workdir:
            with contextlib.suppress(BaseException):
                shutil.rmtree(workdir, ignore_errors=True)
            result["workdir"] = ""

    return result


def _read_result(path: Path) -> dict[str, Any]:
    if not path.exists():
        return {}
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return {}


def verify(result: dict[str, Any]) -> tuple[bool, str]:
    """Cross-process invariants:
      1. Every federate achieved + synchronized at both labels (in
         that order).
      2. Every federate sent exactly running_ticks Ticks.
      3. (Loose) Each federate received between 0 and 2*running_ticks
         peer Ticks. We don't pin an exact count -- send/receive race
         can drop the very first or last in-flight Tick depending on
         when the running phase ends.
    """
    labels = list(result["labels"])
    rt = result["running_ticks"]
    per = result["per_federate"]

    for name in PARTICIPANT_NAMES:
        d = per.get(name) or {}
        if not d:
            return False, f"{name}: result file missing or empty"
        if d.get("achieved") != labels:
            return False, f"{name}.achieved={d.get('achieved')!r} != {labels!r}"
        if d.get("synchronized") != labels:
            return False, f"{name}.synchronized={d.get('synchronized')!r} != {labels!r}"
        sent = d.get("sent_ticks") or []
        if len(sent) != rt:
            return False, f"{name} sent {len(sent)} ticks; expected {rt}"
        if sent != list(range(1, rt + 1)):
            return False, f"{name}.sent_ticks not monotonic 1..{rt}: {sent}"

    return True, "ok"


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--running-ticks", type=int, default=10)
    parser.add_argument("--tick-period", type=float, default=0.05)
    parser.add_argument("--rtid-binary", type=Path, default=None)
    parser.add_argument(
        "--workdir", type=Path, default=None,
        help="working directory (default: examples/pyjevsim-sync-points/.run/<timestamp>/)",
    )
    parser.add_argument("--no-keep-workdir", action="store_true")
    parser.add_argument(
        "--log-level", default="info",
        choices=["debug", "info", "warn", "error"],
    )
    parser.add_argument("--federate-timeout", type=float, default=60.0)
    args = parser.parse_args(argv)

    try:
        result = asyncio.run(
            run_once(
                running_ticks=args.running_ticks,
                tick_period=args.tick_period,
                rtid_binary=args.rtid_binary,
                workdir=args.workdir,
                keep_workdir=not args.no_keep_workdir,
                federate_timeout=args.federate_timeout,
                log_level=args.log_level,
            )
        )
    except Exception as exc:  # noqa: BLE001
        print(f"[runner] {exc}", file=sys.stderr)
        return 1

    ok, msg = verify(result)
    sent_summary = " ".join(
        f"{n}={len((result['per_federate'].get(n) or {}).get('sent_ticks') or [])}"
        for n in PARTICIPANT_NAMES
    )
    print(
        f"[runner] federates={len(PARTICIPANT_NAMES)}  labels={result['labels']}  "
        f"sent={{{sent_summary}}}  rtid_port={result['rtid_port']}  verify={msg}",
        flush=True,
    )
    if result.get("workdir"):
        print(f"[runner] workdir: {result['workdir']}", flush=True)
    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main())
