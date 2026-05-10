"""End-to-end runner for the cross-process Generator -> Buffer ->
Processor relay pipeline.

Production-deployment shape: ``rtid`` runs as a real subprocess with a
real gRPC listener; each federate runs as its own Python subprocess
talking to rtid over ``grpc://``. No in-process fan-out tricks --
rtid does the routing.

Run from the repo root::

    python3 examples/pyjevsim-relay-cross-process/runner.py

Optional flags (mostly the same as the in-process variant -- the
model code is identical, the cross-process wiring just reuses it)::

    --gen-messages N       generator emits N messages then idles (default 50)
    --capacity K           buffer holds K in flight (default 5)
    --service-period P     buffer emits once every P ticks (default 2)
    --drain-ticks D        D extra ticks after the generator stops (default 30)
    --rtid-binary PATH     override the rtid binary location (default
                           ``<repo>/bin/rtid``; built via ``go build``
                           if missing)
    --keep-tempdir         leave the temp dir intact for inspection

Exit code 0 on success (all federates produced result files AND the
accounting invariants hold), 1 on any failure path.

The accounting invariants the runner verifies are the same ones the
in-process variant verifies (so the tests for the two examples can
share assertion shape). What's *different* is the failure mode: in
the in-process variant a leak means a bug in the bridge or runner
fan-out; in the cross-process variant a leak ALSO covers wire-level
failure (rtid crash, dropped gRPC stream, federate process killed).

Cross-platform note (Windows): subprocess kill semantics on Windows
differ from POSIX -- ``terminate()`` on Windows sends
``TerminateProcess`` immediately rather than ``SIGTERM``, and
``start_new_session`` is a POSIX-only flag. The runner avoids
``start_new_session`` on Windows; the rtid + federate procs share
the parent's process group, which is fine because Windows test
runners cleanly tear down child processes via job-object inheritance.
End-to-end runs on Windows haven't been tested in CI yet -- if you
hit a teardown issue on Windows, file a bug.
"""

from __future__ import annotations

import argparse
import datetime as _dt
import asyncio
import contextlib
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
    target.parent.mkdir(parents=True, exist_ok=True)
    subprocess.run(  # noqa: S603, S607
        ["go", "build", "-o", str(target), "./rti/cmd/rtid"],
        cwd=_REPO_ROOT,
        check=True,
    )
    return target


def _ensure_rtid(binary: Path) -> Path:
    """If ``binary`` exists return it as-is; otherwise build it.

    The runner should not silently skip a build when the binary is
    missing -- a stale or missing rtid is the most common cause of
    a confusing failure for new contributors. Building costs a few
    seconds the first time and is essentially free thereafter.
    """
    if binary.exists():
        return binary
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


def _resolve_workdir(workdir: Path | None) -> Path:
    """Return the working directory. Default: a timestamped dir under
    ``examples/pyjevsim-relay-cross-process/.run/``.
    """
    if workdir is not None:
        return workdir
    stamp = _dt.datetime.now().strftime("%Y%m%d-%H%M%S")
    return _HERE / ".run" / stamp


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
    proc.terminate()
    try:
        proc.wait(timeout=timeout)
    except subprocess.TimeoutExpired:
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
    binary = _ensure_rtid(rtid_binary or _DEFAULT_RTID)
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

        # Spawn all three federates back-to-back. The buffer + processor
        # use a tiny startup delay so the generator's
        # publish_interaction_class lands first; rtid's declaration
        # service is order-tolerant, but staggering helps surface any
        # race the example accidentally introduces (e.g. a federate
        # subscribing to a class with no publisher yet -- subscriptions
        # without publishers are valid HLA, but it's clearer in the
        # event log when the publisher is on file first).
        # Buffer + processor join FIRST so their subscriptions are on
        # file before the generator starts publishing -- without that,
        # a generator emit that lands at rtid before either consumer
        # has registered interest is dropped server-side and never
        # delivered (no buffering of pre-subscription sends, by
        # design). The 0.5s sleep before generator spawn gives the
        # consumer connections time to complete their join + subscribe
        # round-trips.
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
        proc_proc, proc_tee = _spawn_federate_with_tee(
            PROCESSOR_SCRIPT, url=url, result_path=proc_result,
            gen_messages=gen_messages, capacity=capacity,
            service_period=service_period, drain_ticks=drain_ticks,
            tail_ticks=processor_tail,
            tick_period=tick_period,
            startup_delay=0.0,
            log_path=workdir / "processor.log",
            name="processor",
        )
        fed_procs.append((proc_proc, proc_tee, "processor"))
        buf_proc, buf_tee = _spawn_federate_with_tee(
            BUFFER_SCRIPT, url=url, result_path=buf_result,
            gen_messages=gen_messages, capacity=capacity,
            service_period=service_period, drain_ticks=drain_ticks,
            tail_ticks=buffer_tail,
            tick_period=tick_period,
            startup_delay=0.0,
            log_path=workdir / "buffer.log",
            name="buffer",
        )
        fed_procs.append((buf_proc, buf_tee, "buffer"))
        # Brief pause for the consumers' subscribe RPCs to round-trip
        # before the generator starts publishing.
        await asyncio.sleep(0.5)
        gen_proc, gen_tee = _spawn_federate_with_tee(
            GENERATOR_SCRIPT, url=url, result_path=gen_result,
            gen_messages=gen_messages, capacity=capacity,
            service_period=service_period, drain_ticks=drain_ticks,
            tail_ticks=0,
            tick_period=tick_period,
            startup_delay=0.0,
            log_path=workdir / "generator.log",
            name="generator",
        )
        fed_procs.append((gen_proc, gen_tee, "generator"))

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
        for proc, tee, name in fed_procs:
            _terminate(proc, name=name)
            tee.join(timeout=1.0)
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
    published = set(result["published"])
    forwarded = set(result["forwarded"])
    dropped = set(result["dropped"])
    received = set(result["received"])
    residual = set(result["queue_residual"])

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

    if received != forwarded:
        only_proc = received - forwarded
        only_buf = forwarded - received
        return False, (
            f"received != forwarded: only-processor={sorted(only_proc)}, "
            f"only-buffer={sorted(only_buf)}"
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
        help="working directory for logs + result files (default: examples/pyjevsim-relay-cross-process/.run/<timestamp>/)",
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
