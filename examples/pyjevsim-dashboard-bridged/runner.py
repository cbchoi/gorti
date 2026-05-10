"""End-to-end cross-process runner for the Sensor -> Dashboard
object-class example.

Spawns rtid + 2 Python federate subprocesses (sensor + dashboard).
Sensor publishes a ``SensorReading`` object instance and updates its
``value`` attribute every tick; Dashboard subscribes and accumulates
the reflected sequence via ``ReflectAttributeValues`` events.

Run from the repo root::

    python3 examples/pyjevsim-dashboard-bridged/runner.py

Optional flags::

    --ticks N            sensor publishes N updates (default 10)
    --mode {sequence,sine}
    --amplitude A        for sine mode (default 100)
    --tick-period P      wall-clock seconds per tick (default 0.05)
    --rtid-binary PATH   override the rtid binary
    --keep-tempdir       leave temp dir for inspection

Mirrors examples/pyjevsim-dashboard/runner.py and
examples/pyjevsim-relay-cross-process/runner.py.
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

SENSOR_SCRIPT = _HERE / "sensor_main.py"
DASHBOARD_SCRIPT = _HERE / "dashboard_main.py"

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
    return binary if binary.exists() else _build_rtid(binary)


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


def _spawn_federate_with_tee(
    script: Path, *, url: str, result_path: Path, ticks: int, drain_ticks: int,
    mode: str, amplitude: int, tick_period: float, startup_delay: float,
    log_path: Path, name: str,
) -> tuple[subprocess.Popen[bytes], LogTee]:
    log_path.parent.mkdir(parents=True, exist_ok=True)
    kwargs: dict[str, Any] = {"stdout": subprocess.PIPE, "stderr": subprocess.STDOUT}
    if not _is_windows():
        kwargs["start_new_session"] = True
    env = {**os.environ, "PYTHONUNBUFFERED": "1"}
    proc = subprocess.Popen(  # noqa: S603
        [
            sys.executable,
            str(script),
            "--url", url,
            "--result", str(result_path),
            "--ticks", str(ticks),
            "--drain-ticks", str(drain_ticks),
            "--mode", mode,
            "--amplitude", str(amplitude),
            "--tick-period", str(tick_period),
            "--startup-delay", str(startup_delay),
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
    ticks: int = 10,
    mode: str = "sequence",
    amplitude: int = 100,
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

    sensor_result = workdir / "sensor-result.json"
    dashboard_result = workdir / "dashboard-result.json"

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

        dash_proc, dash_tee = _spawn_federate_with_tee(
            DASHBOARD_SCRIPT, url=url, result_path=dashboard_result,
            ticks=ticks, drain_ticks=20,
            mode=mode, amplitude=amplitude,
            tick_period=tick_period,
            startup_delay=0.0,
            log_path=workdir / "dashboard.log",
            name="dashboard",
        )
        fed_procs.append((dash_proc, dash_tee, "dashboard"))
        await asyncio.sleep(0.5)

        sensor_proc, sensor_tee = _spawn_federate_with_tee(
            SENSOR_SCRIPT, url=url, result_path=sensor_result,
            ticks=ticks, drain_ticks=0,
            mode=mode, amplitude=amplitude,
            tick_period=tick_period,
            startup_delay=0.0,
            log_path=workdir / "sensor.log",
            name="sensor",
        )
        fed_procs.append((sensor_proc, sensor_tee, "sensor"))

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

    finally:
        for proc, tee, _name in fed_procs:
            _terminate(proc)
            tee.join(timeout=1.0)
        if rtid_proc is not None:
            _terminate(rtid_proc)
        if rtid_tee is not None:
            rtid_tee.join(timeout=1.0)

        sensor_data = _read_result(sensor_result)
        dashboard_data = _read_result(dashboard_result)
        result = {
            "published": list(sensor_data.get("published") or []),
            "received": list(dashboard_data.get("received") or []),
            "discovered": list(dashboard_data.get("discovered") or []),
            "instance_name": sensor_data.get("instance_name"),
            "mode": sensor_data.get("mode") or mode,
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
    pub = result["published"]
    recv = result["received"]
    discovered = result["discovered"]
    if not pub:
        return False, "sensor published nothing — result file missing or empty"
    if recv != pub:
        only_pub = [v for v in pub if v not in set(recv)]
        only_recv = [v for v in recv if v not in set(pub)]
        return False, (
            f"received != published: published={len(pub)} received={len(recv)}; "
            f"only-published(first 5)={only_pub[:5]} "
            f"only-received(first 5)={only_recv[:5]}"
        )
    if not discovered:
        return False, "dashboard saw no DiscoverObjectInstance event"
    # discovered is [[handle, instance_name], ...]; sanity-check that
    # the sensor's instance_name shows up.
    instance = result.get("instance_name")
    if instance and not any(d[1] == instance for d in discovered):
        return False, f"discovered={discovered} missing instance {instance!r}"
    return True, "ok"


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--ticks", type=int, default=10)
    parser.add_argument("--mode", choices=("sequence", "sine"), default="sequence")
    parser.add_argument("--amplitude", type=int, default=100)
    parser.add_argument("--tick-period", type=float, default=0.05)
    parser.add_argument("--rtid-binary", type=Path, default=None)
    parser.add_argument(
        "--workdir", type=Path, default=None,
        help="working directory (default: examples/pyjevsim-dashboard-bridged/.run/<timestamp>/)",
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
                ticks=args.ticks,
                mode=args.mode,
                amplitude=args.amplitude,
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
    print(
        f"[runner] published={len(result['published'])}  "
        f"received={len(result['received'])}  "
        f"discovered={len(result['discovered'])}  "
        f"rtid_port={result['rtid_port']}  verify={msg}",
        flush=True,
    )
    if result.get("workdir"):
        print(f"[runner] workdir: {result['workdir']}", flush=True)
    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main())
