"""End-to-end runner for the cross-process Generator -> Buffer ->
Processor relay pipeline.

Production-deployment shape: ``rtid`` runs as a real subprocess with a
real gRPC listener; each federate runs as its own Python subprocess
talking to rtid over ``grpc://``. No in-process fan-out tricks --
rtid does the routing.

Use ``run.sh`` or ``run.ps1`` from any current directory. Direct Python
execution is also supported::

    python3 examples/pyjevsim-relay-cross-process/runner.py

Optional flags (mostly the same as the in-process variant -- the
model code is identical, the cross-process wiring just reuses it)::

    --gen-messages N       generator emits N messages then idles (default 50)
    --capacity K           buffer holds K in flight (default 5)
    --service-period P     buffer emits once every P ticks (default 2)
    --drain-ticks D        D extra ticks after the generator stops (default 30)
    --rtid-binary PATH     override the rtid binary location
    --workdir PATH         use a specific artifact directory
    --no-keep-workdir      remove completed run artifacts

Exit code 0 on success (all federates produced result files AND the
accounting invariants hold), 1 on any failure path.

The accounting invariants the runner verifies are the same ones the
in-process variant verifies (so the tests for the two examples can
share assertion shape). What's *different* is the failure mode: in
the in-process variant a leak means a bug in the bridge or runner
fan-out; in the cross-process variant a leak ALSO covers wire-level
failure (rtid crash, dropped gRPC stream, federate process killed).

The runner uses native subprocess behavior on Windows and POSIX process
sessions on Linux. Cleanup always terminates remaining children and
escalates to a kill after a bounded wait.
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

GENERATOR_SCRIPT = _HERE / "generator_main.py"
BUFFER_SCRIPT = _HERE / "buffer_main.py"
PROCESSOR_SCRIPT = _HERE / "processor_main.py"

# Shared tee helper (echo subprocess output to console + log file).
sys.path.insert(0, str(_REPO_ROOT / "examples"))
from _log_tee import LogTee  # noqa: E402


def _is_windows() -> bool:
    return sys.platform.startswith("win")


def _free_port() -> int:
    """Bind to port 0 to discover a free TCP port; close + return.

    Race window: another process could bind before rtid does. The
    in-process tests have used the same trick across the M5 + M12
    harnesses and the collision rate has been zero -- acceptable for
    a developer-loop example runner.
    """
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
        s.bind(("127.0.0.1", 0))
        return int(s.getsockname()[1])


def _build_rtid(target: Path) -> Path:
    """Build the rtid binary into ``target``. Returns the path.

    Idempotent: ``go build`` short-circuits when nothing changed.
    Runs from the repo root so module paths resolve correctly.
    """
    go = shutil.which("go")
    if go is None:
        raise FileNotFoundError("go was not found on PATH and rtid is not built")
    target.parent.mkdir(parents=True, exist_ok=True)
    subprocess.run(  # noqa: S603
        [go, "build", "-o", str(target), "./rti/cmd/rtid"],
        cwd=_REPO_ROOT,
        check=True,
    )
    return target


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


def _ensure_rtid(binary: Path) -> Path:
    """If ``binary`` exists return it as-is; otherwise build it.

    The runner should not silently skip a build when the binary is
    missing -- a stale or missing rtid is the most common cause of
    a confusing failure for new contributors. Building costs a few
    seconds the first time and is essentially free thereafter.
    """
    binary = _with_native_suffix(binary)
    if binary.is_file():
        return binary
    if shutil.which("go") is None:
        raise RuntimeError(
            f"rtid not found at {binary}; install Go or pass --rtid-binary"
        )
    return _build_rtid(binary)


async def _wait_for_grpc(port: int, *, timeout: float = 10.0) -> None:  # noqa: ASYNC109
    """Poll a TCP connect until rtid accepts on ``port``."""
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


async def _wait_for_ready(
    path: Path,
    proc: subprocess.Popen[bytes],
    name: str,
    *,
    timeout: float = 20.0,
) -> None:
    deadline = asyncio.get_event_loop().time() + timeout
    while asyncio.get_event_loop().time() < deadline:
        if path.is_file():  # noqa: ASYNC240
            return
        if proc.poll() is not None:
            raise RuntimeError(
                f"{name} exited with status {proc.returncode} before declaring ready"
            )
        await asyncio.sleep(0.05)
    raise TimeoutError(f"{name} did not declare ready within {timeout}s")


def _resolve_workdir(workdir: Path | None) -> Path:
    """Return an absolute explicit path or a unique local run directory."""
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


def _spawn_rtid_with_tee(
    binary: Path,
    listen_port: int,
    metrics_port: int,
    admin_port: int,
    *,
    save_dir: Path,
    log_dir: Path,
    log_path: Path,
    log_level: str = "info",
) -> tuple[subprocess.Popen[bytes], LogTee]:
    """Launch rtid as a subprocess and tee its output to log + console."""
    kwargs: dict[str, Any] = {
        "stdout": subprocess.PIPE,
        "stderr": subprocess.STDOUT,
    }
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
            "--audit-replay-plugin", "event-journal",
            "--log-dir", str(log_dir),
            "--save-dir", str(save_dir),
        ],
        **kwargs,
    )
    tee = LogTee(proc, log_path=log_path, prefix="rtid")
    tee.start()
    return proc, tee


def _spawn_federate_with_tee(
    script: Path,
    *,
    url: str,
    result_path: Path,
    gen_messages: int,
    capacity: int,
    service_period: int,
    drain_ticks: int,
    tail_ticks: int,
    tick_period: float,
    startup_delay: float,
    ready_file: Path,
    start_file: Path,
    log_path: Path,
    name: str,
) -> tuple[subprocess.Popen[bytes], LogTee]:
    """Spawn one federate and tee stdout+stderr to log + console."""
    log_path.parent.mkdir(parents=True, exist_ok=True)
    kwargs: dict[str, Any] = {
        "stdout": subprocess.PIPE,
        "stderr": subprocess.STDOUT,
    }
    if not _is_windows():
        kwargs["start_new_session"] = True
    env = {**os.environ, "PYTHONUNBUFFERED": "1"}
    proc = subprocess.Popen(  # noqa: S603
        [
            sys.executable,
            str(script),
            "--url", url,
            "--result", str(result_path),
            "--gen-messages", str(gen_messages),
            "--capacity", str(capacity),
            "--service-period", str(service_period),
            "--drain-ticks", str(drain_ticks),
            "--tail-ticks", str(tail_ticks),
            "--tick-period", str(tick_period),
            "--startup-delay", str(startup_delay),
            "--ready-file", str(ready_file),
            "--start-file", str(start_file),
            "--startup-timeout", "20.0",
        ],
        env=env,
        **kwargs,
    )
    tee = LogTee(proc, log_path=log_path, prefix=name)
    tee.start()
    return proc, tee


def _terminate(proc: subprocess.Popen[bytes], *, name: str, timeout: float = 5.0) -> None:
    """Best-effort terminate; kill on timeout. Swallows teardown errors
    so one stuck process can't mask another's exit reason.
    """
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
    del name  # only kept for future structured logging


async def run_once(
    *,
    gen_messages: int = 50,
    capacity: int = 5,
    service_period: int = 2,
    drain_ticks: int = 30,
    tick_period: float = 0.05,
    rtid_binary: Path | None = None,
    workdir: Path | None = None,
    keep_workdir: bool = True,
    federate_timeout: float = 60.0,
    log_level: str = "info",
) -> dict[str, Any]:
    """Drive one full cross-process run.

    Returns a result dict shaped like the in-process variant's so the
    accounting verifier in :func:`verify` is reusable across both.
    """
    if gen_messages < 1:
        raise ValueError("gen_messages must be at least 1")
    if capacity < 1:
        raise ValueError("capacity must be at least 1")
    if service_period < 1:
        raise ValueError("service_period must be at least 1")
    if drain_ticks < 0:
        raise ValueError("drain_ticks must not be negative")
    if tick_period <= 0:
        raise ValueError("tick_period must be positive")
    if federate_timeout <= 0:
        raise ValueError("federate_timeout must be positive")
    _check_runtime_dependencies()
    binary = _ensure_rtid(rtid_binary or _default_rtid())
    listen_port = _free_port()
    metrics_port = _free_port()
    admin_port = _free_port()
    # Defensive de-dup: bind/release race makes collisions vanishingly
    # rare, but two consecutive calls ARE allowed to return the same
    # port. Resample on collision.
    while metrics_port == listen_port:
        metrics_port = _free_port()
    while admin_port in (listen_port, metrics_port):
        admin_port = _free_port()

    workdir = _resolve_workdir(workdir)
    save_dir = workdir / "saves"
    log_dir = workdir / "eventlog"
    save_dir.mkdir(parents=True, exist_ok=True)
    log_dir.mkdir(parents=True, exist_ok=True)
    workdir.mkdir(parents=True, exist_ok=True)

    gen_result = workdir / "generator-result.json"
    buf_result = workdir / "buffer-result.json"
    proc_result = workdir / "processor-result.json"
    ready_paths = {
        name: workdir / f"{name}.ready" for name in ("generator", "buffer", "processor")
    }
    start_file = workdir / "start.signal"
    for path in (gen_result, buf_result, proc_result, *ready_paths.values(), start_file):
        path.unlink(missing_ok=True)
        path.with_suffix(path.suffix + ".tmp").unlink(missing_ok=True)

    print(f"[runner] working directory: {workdir}", flush=True)
    print(f"[runner] rtid listen port: {listen_port}", flush=True)

    rtid_proc: subprocess.Popen[bytes] | None = None
    rtid_tee: LogTee | None = None
    fed_procs: list[tuple[subprocess.Popen[bytes], LogTee, str]] = []
    exit_codes: dict[str, int | None] = {}

    try:
        rtid_proc, rtid_tee = _spawn_rtid_with_tee(
            binary, listen_port, metrics_port, admin_port,
            save_dir=save_dir, log_dir=log_dir,
            log_path=workdir / "rtid.log",
            log_level=log_level,
        )
        await _wait_for_grpc(listen_port)

        url = f"grpc://127.0.0.1:{listen_port}"

        # Declaration order matters in the current RTI: a subscription
        # binds against publishers already on file. Start each process
        # in dependency order and hold every model loop behind a common
        # start signal until all declarations have completed.
        #
        # Tail-tick discipline: the buffer + processor run for
        # ``gen_messages + drain_ticks + tail_ticks`` cycles so a
        # buffer emit on the LAST drain tick still has time to be
        # delivered to the processor. Generator stops at
        # ``gen_messages + drain_ticks`` -- it has no incoming work
        # past that point, so a tail would just be dead air.
        #
        # The tail size scales with the processor's tick_period: the
        # default 0.05s tick * (buffer_tail) = wall-clock seconds of
        # grace, enough for several buffer-emit -> rtid -> processor
        # -> events_for round trips even when the kernel scheduler is
        # busy. A too-small tail surfaces as ``received != forwarded``
        # with a one- or two-seq drift; a too-large tail just makes
        # the test slower.
        #
        # The processor's tail is strictly LARGER than the buffer's
        # tail so the processor is still draining after the buffer's
        # last possible emit. Without this asymmetry, the buffer's
        # final emit can land in flight on the processor's last tick
        # and miss the drain by a few microseconds.
        buffer_tail = 20
        processor_tail = buffer_tail + 20
        gen_proc, gen_tee = _spawn_federate_with_tee(
            GENERATOR_SCRIPT, url=url, result_path=gen_result,
            gen_messages=gen_messages, capacity=capacity,
            service_period=service_period, drain_ticks=drain_ticks,
            tail_ticks=0,
            tick_period=tick_period,
            startup_delay=0.0,
            ready_file=ready_paths["generator"], start_file=start_file,
            log_path=workdir / "generator.log",
            name="generator",
        )
        fed_procs.append((gen_proc, gen_tee, "generator"))
        await _wait_for_ready(ready_paths["generator"], gen_proc, "generator")

        buf_proc, buf_tee = _spawn_federate_with_tee(
            BUFFER_SCRIPT, url=url, result_path=buf_result,
            gen_messages=gen_messages, capacity=capacity,
            service_period=service_period, drain_ticks=drain_ticks,
            tail_ticks=buffer_tail,
            tick_period=tick_period,
            startup_delay=0.0,
            ready_file=ready_paths["buffer"], start_file=start_file,
            log_path=workdir / "buffer.log",
            name="buffer",
        )
        fed_procs.append((buf_proc, buf_tee, "buffer"))
        await _wait_for_ready(ready_paths["buffer"], buf_proc, "buffer")

        proc_proc, proc_tee = _spawn_federate_with_tee(
            PROCESSOR_SCRIPT, url=url, result_path=proc_result,
            gen_messages=gen_messages, capacity=capacity,
            service_period=service_period, drain_ticks=drain_ticks,
            tail_ticks=processor_tail,
            tick_period=tick_period,
            startup_delay=0.0,
            ready_file=ready_paths["processor"], start_file=start_file,
            log_path=workdir / "processor.log",
            name="processor",
        )
        fed_procs.append((proc_proc, proc_tee, "processor"))
        await _wait_for_ready(ready_paths["processor"], proc_proc, "processor")

        start_tmp = start_file.with_suffix(start_file.suffix + ".tmp")
        start_tmp.write_text("start\n", encoding="utf-8")
        start_tmp.replace(start_file)

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
            _terminate(proc, name=name)
            tee.join(timeout=1.0)
            exit_codes[name] = proc.returncode
        if rtid_proc is not None:
            _terminate(rtid_proc, name="rtid")
        if rtid_tee is not None:
            rtid_tee.join(timeout=1.0)

        result = {
            "published": _read_list(gen_result, "published"),
            "forwarded": _read_list(buf_result, "forwarded"),
            "dropped": _read_list(buf_result, "dropped"),
            "queue_residual": _read_list(buf_result, "queue_residual"),
            "received": _read_list(proc_result, "received"),
            "workdir": str(workdir),
            "rtid_port": listen_port,
            "rtid_pid": rtid_proc.pid if rtid_proc is not None else None,
            "exit_codes": exit_codes,
            "gen_messages": gen_messages,
            "capacity": capacity,
            "service_period": service_period,
            "drain_ticks": drain_ticks,
        }

        if not keep_workdir:
            with contextlib.suppress(BaseException):
                shutil.rmtree(workdir, ignore_errors=True)
            result["workdir"] = ""

    return result


def _read_list(path: Path, key: str) -> list[int]:
    """Read a JSON file and return ``payload[key]`` as a list of ints.

    Returns ``[]`` if the file is missing or malformed -- the federate
    crashed or never wrote, and verify() should report the symptom
    rather than this helper raising.
    """
    if not path.exists():
        return []
    try:
        payload = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return []
    value = payload.get(key, [])
    if not isinstance(value, list):
        return []
    return [int(x) for x in value if isinstance(x, int)]


def verify(result: dict[str, Any]) -> tuple[bool, str]:
    """Same accounting invariants as the in-process variant.

    1. ``forwarded | dropped | residual == published`` -- no seq is
       silently lost between publication and the buffer's accounting.
    2. ``forwarded & dropped == empty`` -- a seq cannot be both
       forwarded and dropped.
    3. ``received == forwarded`` -- the processor sees exactly what
       the buffer released.

    Note: in cross-process runs, race conditions in the bridge's
    step loop CAN leave a seq in the residual that the in-process
    variant would have forwarded, which is fine -- the invariant
    still holds. The verifier doesn't distinguish "expected residual"
    from "leaked into residual"; it just checks the conservation law.
    """
    failed = {
        name: code for name, code in result.get("exit_codes", {}).items() if code != 0
    }
    if failed:
        return False, f"federate exit failure(s): {failed}"

    sequences = {
        "published": result["published"],
        "forwarded": result["forwarded"],
        "dropped": result["dropped"],
        "received": result["received"],
        "residual": result["queue_residual"],
    }
    for label, values in sequences.items():
        if len(values) != len(set(values)):
            return False, f"{label} contains duplicate sequence numbers: {values}"

    expected_published = list(range(1, result.get("gen_messages", 0) + 1))
    if sequences["published"] != expected_published:
        return False, (
            f"published sequence {sequences['published']}; "
            f"expected {expected_published}"
        )

    published = set(sequences["published"])
    forwarded = set(sequences["forwarded"])
    dropped = set(sequences["dropped"])
    received = set(sequences["received"])
    residual = set(sequences["residual"])

    if not published:
        return False, "generator produced nothing -- result file missing or empty"

    seen = forwarded | dropped | residual
    if seen != published:
        only_pub = published - seen
        only_seen = seen - published
        return False, (
            f"accounting leak: published={len(published)} but "
            f"forwarded({len(forwarded)}) | dropped({len(dropped)}) | "
            f"residual({len(residual)}) = {len(seen)} unique seqs; "
            f"only-published={sorted(only_pub)[:5]} "
            f"only-seen={sorted(only_seen)[:5]}"
        )

    overlap = forwarded & dropped
    if overlap:
        return False, f"forwarded & dropped = {sorted(overlap)} (must be empty)"

    if sequences["received"] != sequences["forwarded"]:
        only_proc = received - forwarded
        only_buf = forwarded - received
        return False, (
            "received sequence does not exactly match forwarded sequence: "
            f"only-processor={sorted(only_proc)}, only-buffer={sorted(only_buf)}"
        )

    return True, "ok"


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--gen-messages", type=int, default=50)
    parser.add_argument("--capacity", type=int, default=5)
    parser.add_argument("--service-period", type=int, default=2)
    parser.add_argument("--drain-ticks", type=int, default=30)
    parser.add_argument("--tick-period", type=float, default=0.05)
    parser.add_argument("--rtid-binary", type=Path, default=None)
    parser.add_argument(
        "--workdir", type=Path, default=None,
        help="working directory for logs + results (default: unique .run/run-*)",
    )
    parser.add_argument(
        "--no-keep-workdir", action="store_true",
        help="delete the workdir after the run (default: keep)",
    )
    parser.add_argument(
        "--log-level", default="info",
        choices=["debug", "info", "warn", "error"],
        help="rtid log level",
    )
    parser.add_argument("--federate-timeout", type=float, default=60.0)
    args = parser.parse_args(argv)

    try:
        result = asyncio.run(
            run_once(
                gen_messages=args.gen_messages,
                capacity=args.capacity,
                service_period=args.service_period,
                drain_ticks=args.drain_ticks,
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
        f"forwarded={len(result['forwarded'])}  "
        f"dropped={len(result['dropped'])}  "
        f"received={len(result['received'])}  "
        f"residual={len(result['queue_residual'])}  "
        f"rtid_port={result['rtid_port']}  "
        f"verify={msg}",
        flush=True,
    )
    if result.get("workdir"):
        print(f"[runner] workdir: {result['workdir']}", flush=True)
    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main())
