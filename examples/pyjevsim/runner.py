"""End-to-end runner for the cross-process Producer -> Consumer
pipeline.

Production-deployment shape: ``rtid`` runs as a real subprocess with a
real gRPC listener; each of the two federates runs as its own Python
subprocess talking to rtid over ``grpc://``. No in-process fan-out
tricks -- rtid does the routing.

Run from the repo root::

    python3 examples/pyjevsim/runner.py

Optional flags::

    --ticks N            producer emits N messages then idles (default 50)
    --drain-ticks D      D extra idle ticks after the emit phase (default 30)
    --tick-period P      wall-clock seconds per tick (default 0.05)
    --rtid-binary PATH   override the rtid binary (default <repo>/bin/rtid;
                         built via 'go build' if missing)
    --keep-tempdir       leave the temp dir intact for inspection

Exit code 0 on success (both federates produced result files AND the
accounting invariant holds: consumer.received == producer.published),
1 on any failure path.

Mirrors the structure of
``examples/pyjevsim-relay-cross-process/runner.py`` so contributors
who learn one runner understand the other immediately.
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
_BIN_DIR = _REPO_ROOT / "bin"
_DEFAULT_RTID = _BIN_DIR / "rtid"

PRODUCER_SCRIPT = _HERE / "producer_main.py"
CONSUMER_SCRIPT = _HERE / "consumer_main.py"


def _is_windows() -> bool:
    return sys.platform.startswith("win")


def _free_port() -> int:
    """Bind to port 0 to discover a free TCP port; close + return."""
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
        s.bind(("127.0.0.1", 0))
        return int(s.getsockname()[1])


def _build_rtid(target: Path) -> Path:
    """Build the rtid binary into ``target``. Idempotent."""
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


def _spawn_rtid(
    binary: Path,
    listen_port: int,
    metrics_port: int,
    admin_port: int,
    *,
    save_dir: Path,
    log_dir: Path,
    log_path: Path,
) -> subprocess.Popen[bytes]:
    """Launch rtid as a subprocess. Mirrors the relay-cross-process
    runner so teardown behaviour is consistent across both examples.
    """
    kwargs: dict[str, Any] = {
        "stdout": log_path.open("wb"),  # noqa: SIM115
        "stderr": subprocess.STDOUT,
    }
    if not _is_windows():
        kwargs["start_new_session"] = True
    return subprocess.Popen(  # noqa: S603
        [
            str(binary),
            "--listen", f":{listen_port}",
            "--metrics-listen", f":{metrics_port}",
            "--admin-listen", f"127.0.0.1:{admin_port}",
            "--log-level", "warn",
            "--log-dir", str(log_dir),
            "--save-dir", str(save_dir),
        ],
        **kwargs,
    )


def _spawn_federate(
    script: Path,
    *,
    url: str,
    result_path: Path,
    ticks: int,
    drain_ticks: int,
    tail_ticks: int,
    tick_period: float,
    startup_delay: float,
    log_path: Path,
) -> subprocess.Popen[bytes]:
    """Spawn one federate as ``python3 <script> ...``. Stdout + stderr
    are tee'd into ``log_path`` for post-mortem of a crashed federate.
    """
    log_path.parent.mkdir(parents=True, exist_ok=True)
    log_fh = log_path.open("wb")  # noqa: SIM115
    kwargs: dict[str, Any] = {"stdout": log_fh, "stderr": subprocess.STDOUT}
    if not _is_windows():
        kwargs["start_new_session"] = True
    env = {**os.environ, "PYTHONUNBUFFERED": "1"}
    return subprocess.Popen(  # noqa: S603
        [
            sys.executable,
            str(script),
            "--url", url,
            "--result", str(result_path),
            "--ticks", str(ticks),
            "--drain-ticks", str(drain_ticks),
            "--tail-ticks", str(tail_ticks),
            "--tick-period", str(tick_period),
            "--startup-delay", str(startup_delay),
        ],
        env=env,
        **kwargs,
    )


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
    ticks: int = 50,
    drain_ticks: int = 30,
    tick_period: float = 0.05,
    rtid_binary: Path | None = None,
    keep_tempdir: bool = False,
    federate_timeout: float = 60.0,
) -> dict[str, Any]:
    """Drive one full cross-process run.

    Consumer's tail is set so a producer emit on its last drain tick
    is still delivered before the consumer resigns.
    """
    binary = _ensure_rtid(rtid_binary or _DEFAULT_RTID)
    listen_port = _free_port()
    metrics_port = _free_port()
    admin_port = _free_port()
    while metrics_port == listen_port:
        metrics_port = _free_port()
    while admin_port in (listen_port, metrics_port):
        admin_port = _free_port()

    tmpdir = Path(tempfile.mkdtemp(prefix="pyjevsim-cross-"))
    save_dir = tmpdir / "saves"
    log_dir = tmpdir / "logs"
    fed_log_dir = tmpdir / "federate-logs"
    for d in (save_dir, log_dir, fed_log_dir):
        d.mkdir(parents=True, exist_ok=True)

    prod_result = tmpdir / "producer-result.json"
    cons_result = tmpdir / "consumer-result.json"

    rtid_proc: subprocess.Popen[bytes] | None = None
    fed_procs: list[subprocess.Popen[bytes]] = []

    try:
        rtid_proc = _spawn_rtid(
            binary, listen_port, metrics_port, admin_port,
            save_dir=save_dir, log_dir=log_dir,
            log_path=tmpdir / "rtid.log",
        )
        await _wait_for_grpc(listen_port)
        url = f"grpc://127.0.0.1:{listen_port}"

        # Consumer first so its subscribe lands before any publish.
        # Pre-subscription publishes are dropped server-side -- the
        # 0.5s sleep before the producer spawn gives the consumer's
        # subscribe RPC time to round-trip.
        consumer_tail = 40
        cons_proc = _spawn_federate(
            CONSUMER_SCRIPT, url=url, result_path=cons_result,
            ticks=ticks, drain_ticks=drain_ticks,
            tail_ticks=consumer_tail,
            tick_period=tick_period,
            startup_delay=0.0,
            log_path=fed_log_dir / "consumer.log",
        )
        fed_procs.append(cons_proc)
        await asyncio.sleep(0.5)

        prod_proc = _spawn_federate(
            PRODUCER_SCRIPT, url=url, result_path=prod_result,
            ticks=ticks, drain_ticks=drain_ticks,
            tail_ticks=0,
            tick_period=tick_period,
            startup_delay=0.0,
            log_path=fed_log_dir / "producer.log",
        )
        fed_procs.append(prod_proc)

        deadline = asyncio.get_event_loop().time() + federate_timeout
        for proc, name in (
            (prod_proc, "producer"),
            (cons_proc, "consumer"),
        ):
            remaining = max(0.1, deadline - asyncio.get_event_loop().time())
            try:
                await asyncio.to_thread(proc.wait, remaining)
            except subprocess.TimeoutExpired:
                print(
                    f"runner: federate {name} did not exit within "
                    f"{remaining:.1f}s; will be force-terminated",
                    file=sys.stderr,
                )

    finally:
        for proc in fed_procs:
            _terminate(proc)
        if rtid_proc is not None:
            _terminate(rtid_proc)

        result = {
            "published": _read_list(prod_result, "published"),
            "received": _read_list(cons_result, "received"),
            "tempdir": str(tmpdir) if keep_tempdir else None,
            "rtid_port": listen_port,
            "rtid_pid": rtid_proc.pid if rtid_proc is not None else None,
        }

        if not keep_tempdir:
            with contextlib.suppress(BaseException):
                shutil.rmtree(tmpdir, ignore_errors=True)

    return result


def _read_list(path: Path, key: str) -> list[int]:
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
    """Cross-process accounting invariant: every seq the producer
    published should arrive at the consumer, in order, exactly once.

    With only one publisher and one subscriber and gRPC in-order
    delivery, this should hold exactly -- there is no buffer to drop
    on overflow, no race between concurrent publishers. If it FAILS,
    something is structurally wrong (subscription didn't land in
    time, or a federate crashed mid-loop).
    """
    pub = result["published"]
    recv = result["received"]
    if not pub:
        return False, "producer published nothing -- result file missing or empty"
    if recv != pub:
        only_pub = [s for s in pub if s not in set(recv)]
        only_recv = [s for s in recv if s not in set(pub)]
        return False, (
            f"received != published: published={len(pub)} received={len(recv)}; "
            f"only-published(first 5)={only_pub[:5]} "
            f"only-received(first 5)={only_recv[:5]}"
        )
    return True, "ok"


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--ticks", type=int, default=50)
    parser.add_argument("--drain-ticks", type=int, default=30)
    parser.add_argument("--tick-period", type=float, default=0.05)
    parser.add_argument("--rtid-binary", type=Path, default=None)
    parser.add_argument("--keep-tempdir", action="store_true")
    parser.add_argument("--federate-timeout", type=float, default=60.0)
    args = parser.parse_args(argv)

    try:
        result = asyncio.run(
            run_once(
                ticks=args.ticks,
                drain_ticks=args.drain_ticks,
                tick_period=args.tick_period,
                rtid_binary=args.rtid_binary,
                keep_tempdir=args.keep_tempdir,
                federate_timeout=args.federate_timeout,
            )
        )
    except Exception as exc:  # noqa: BLE001
        print(f"runner: {exc}", file=sys.stderr)
        return 1

    ok, msg = verify(result)
    print(
        f"runner: published={len(result['published'])}  "
        f"received={len(result['received'])}  "
        f"rtid_port={result['rtid_port']}  "
        f"verify={msg}",
        flush=True,
    )
    if result.get("tempdir"):
        print(f"runner: tempdir kept at {result['tempdir']}", flush=True)
    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main())
