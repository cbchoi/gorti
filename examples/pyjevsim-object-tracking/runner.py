"""End-to-end runner for the time-managed object-tracking example.

Spawns rtid + 1 producer + 2 trackers. Every federate enables time
regulation + constrained, so rtid's TimeService coordinates the
cycle. Producer registers a Vehicle, updates Position+Speed at each
NER grant; trackers receive reflects via the events stream and feed
them into pyjevsim-style external_transition.

Use ``run.sh`` or ``run.ps1`` from any current directory. Direct Python
execution is also supported::

    python3 examples/pyjevsim-object-tracking/runner.py

Logs are written to a unique ``.run/run-*`` directory
AND streamed to the parent's stderr with [rtid] / [<federate>] prefixes.

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
_PRODUCER_SCRIPT = _HERE / "producer_main.py"
_TRACKER_SCRIPT = _HERE / "tracker_main.py"

# Shared tee helper (echo subprocess output to console + log file).
sys.path.insert(0, str(_REPO_ROOT / "examples"))
from _log_tee import LogTee  # noqa: E402


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


def _with_native_suffix(path: Path) -> Path:
    if sys.platform.startswith("win") and path.suffix.lower() != ".exe":
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
    if not sys.platform.startswith("win"):
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


def _spawn_federate_with_tee(
    script: Path, *, url: str, result_path: Path, log_path: Path,
    name: str, cycles: int, tick_step: float, lookahead: float,
) -> tuple[subprocess.Popen[bytes], LogTee]:
    log_path.parent.mkdir(parents=True, exist_ok=True)
    env = {**os.environ, "PYTHONUNBUFFERED": "1"}
    proc = subprocess.Popen(  # noqa: S603
        [
            sys.executable,
            str(script),
            "--url", url,
            "--result", str(result_path),
            "--name", name,
            "--cycles", str(cycles),
            "--tick-step", str(tick_step),
            "--lookahead", str(lookahead),
        ],
        env=env,
        **_popen_kwargs(),
    )
    tee = LogTee(proc, log_path=log_path, prefix=name)
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


# Federate roster: one regulating producer and two constrained-only
# trackers, all coordinated by gorti's TimeService.
SPECS = [
    {"role": "producer", "name": "producer", "lookahead": 1.0},
    {"role": "tracker",  "name": "tracker-A", "lookahead": 1.0},
    {"role": "tracker",  "name": "tracker-B", "lookahead": 1.0},
]


def _validate_tso_step(tick_step: float, lookahead: float) -> None:
    """Ensure each update timestamp satisfies the regulating sender rule."""
    if tick_step < lookahead:
        raise ValueError(
            "tick_step must be at least the producer lookahead so every TSO "
            "timestamp is >= current logical time + lookahead"
        )


async def run_once(
    *, cycles: int = 5, tick_step: float = 1.0,
    rtid_binary: Path | None = None, workdir: Path | None = None,
    keep_workdir: bool = True, federate_timeout: float = 60.0,
    log_level: str = "info",
) -> dict[str, Any]:
    if cycles < 1:
        raise ValueError("cycles must be at least 1")
    if tick_step <= 0:
        raise ValueError("tick_step must be positive")
    _validate_tso_step(tick_step, float(SPECS[0]["lookahead"]))
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
            binary, port, workdir / "rtid.log",
            workdir / "saves", workdir / "eventlog", log_level,
        )
        await _wait_for_grpc(port)
        url = f"grpc://127.0.0.1:{port}"

        # Producer FIRST. It joins, enables time regulation, publishes
        # and registers the Vehicle BEFORE entering its NMRA loop.
        # Trackers spawned afterwards see (a) the producer as a
        # regulator (so LBTS isn't +Inf) and (b) a
        # DiscoverObjectInstance event arriving as a barrier before
        # their own NMRA loop starts.
        for spec in SPECS:
            if spec["role"] != "producer":
                continue
            p, t = _spawn_federate_with_tee(
                _PRODUCER_SCRIPT, url=url,
                result_path=result_paths[spec["name"]],
                log_path=workdir / f"{spec['name']}.log",
                name=spec["name"], cycles=cycles, tick_step=tick_step,
                lookahead=spec["lookahead"],
            )
            fed_procs.append((p, t, spec["name"]))

        # Give the producer time to join + enable_time_regulation +
        # register the Vehicle before the trackers join. Without this
        # the trackers would see no regulators and grant NMRA(t)
        # instantly with LBTS=+Inf, racing past the producer's updates.
        await asyncio.sleep(0.6)

        for spec in SPECS:
            if spec["role"] != "tracker":
                continue
            p, t = _spawn_federate_with_tee(
                _TRACKER_SCRIPT, url=url,
                result_path=result_paths[spec["name"]],
                log_path=workdir / f"{spec['name']}.log",
                name=spec["name"], cycles=cycles, tick_step=tick_step,
                lookahead=spec["lookahead"],
            )
            fed_procs.append((p, t, spec["name"]))

        deadline = asyncio.get_event_loop().time() + federate_timeout
        for proc, _tee, name in [(p, t, n) for p, t, n in fed_procs]:
            remaining = max(0.1, deadline - asyncio.get_event_loop().time())
            try:
                await asyncio.to_thread(proc.wait, remaining)
            except subprocess.TimeoutExpired:
                print(
                    f"[runner] federate {name} did not exit within "
                    f"{remaining:.1f}s; will be force-terminated",
                    file=sys.stderr,
                )
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
    """Cross-process invariants.

    1. Producer emitted exactly cycles updates.
    2. Each tracker discovered the Vehicle (1 instance).
    3. Each tracker received exactly cycles reflects, position values
       matching producer.published[i].position. (TimeService TSO order
       guarantees in-order delivery up to grant time.)
    """
    per = result["per_federate"]
    cycles = result["cycles"]
    failed = {
        name: code for name, code in result.get("exit_codes", {}).items() if code != 0
    }
    if failed:
        return False, f"federate exit failure(s): {failed}"
    producer = per.get("producer") or {}
    pub = producer.get("published") or []
    if len(pub) != cycles:
        return False, f"producer published {len(pub)} updates; expected {cycles}"
    if producer.get("model_cycles") != cycles:
        return False, "producer did not complete one model transition per grant"

    tick_step = result["tick_step"]
    expected_times = [i * tick_step for i in range(1, cycles + 1)]
    actual_times = [u.get("t") for u in pub]
    if actual_times != expected_times:
        return False, f"producer grant sequence {actual_times}; expected {expected_times}"
    expected_positions = [t * 2.0 for t in expected_times]
    if [u.get("position") for u in pub] != expected_positions:
        return False, "producer position sequence does not match the model"
    if [u.get("speed") for u in pub] != [5.0] * cycles:
        return False, "producer speed sequence does not match the model"
    for name, role in (("tracker-A", "tracker-A"), ("tracker-B", "tracker-B")):
        tr = per.get(name) or {}
        if not tr:
            return False, f"{name}: result file missing or empty"
        if len(tr.get("discovered") or []) < 1:
            return False, f"{name}: did not discover the Vehicle instance"
        if tr.get("model_cycles") != cycles:
            return False, f"{name}: did not complete one model transition per grant"
        recv = tr.get("received") or []
        if len(recv) != cycles:
            return False, (
                f"{name}: received {len(recv)} reflects; expected {cycles}"
            )
        recv_positions = [r["position"] for r in recv]
        if recv_positions != expected_positions:
            return False, (
                f"{name}: position sequence mismatch — "
                f"got {recv_positions}, expected {expected_positions}"
            )
        if [r.get("speed") for r in recv] != [5.0] * cycles:
            return False, f"{name}: speed sequence mismatch"
        if [r.get("t") for r in recv] != expected_times:
            return False, f"{name}: delivery grant sequence mismatch"
        del role
    return True, "ok"


def main(argv: list[str] | None = None) -> int:
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--cycles", type=int, default=5)
    p.add_argument("--tick-step", type=float, default=1.0)
    p.add_argument("--rtid-binary", type=Path, default=None)
    p.add_argument(
        "--workdir", type=Path, default=None,
        help="working directory (default: a unique .run/run-* directory)",
    )
    p.add_argument("--no-keep-workdir", action="store_true")
    p.add_argument(
        "--log-level", default="info",
        choices=["debug", "info", "warn", "error"],
    )
    p.add_argument("--federate-timeout", type=float, default=60.0)
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
        f"{n}={(r.get('role') or '?')}({len(r.get('received') or r.get('published') or [])})"
        for n, r in result["per_federate"].items()
    )
    print(
        f"[runner] cycles={result['cycles']} federates={{{summary}}} "
        f"rtid_port={result['rtid_port']} verify={msg}",
        flush=True,
    )
    if result.get("workdir"):
        print(f"[runner] workdir: {result['workdir']}", flush=True)
    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main())
