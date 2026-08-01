"""Cross-process runner for real pyjevsim models and gorti."""

from __future__ import annotations

import argparse
import asyncio
import datetime as dt
import importlib.util
import os
import secrets
import sys
from pathlib import Path
from types import ModuleType

HERE = Path(__file__).resolve().parent
REPO_ROOT = HERE.parents[1]
BASE_RUNNER = REPO_ROOT / "examples" / "pyjevsim" / "runner.py"


def _load_base_runner() -> ModuleType:
    spec = importlib.util.spec_from_file_location(
        "gorti_pyjevsim_base_runner", BASE_RUNNER
    )
    if spec is None or spec.loader is None:
        raise RuntimeError(f"cannot load shared runner: {BASE_RUNNER}")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def _default_workdir() -> Path:
    stamp = dt.datetime.now().strftime("%Y%m%d-%H%M%S-%f")
    suffix = f"{os.getpid()}-{secrets.token_hex(4)}"
    return HERE / ".run" / f"{stamp}-{suffix}"


async def run(args: argparse.Namespace) -> int:
    base = _load_base_runner()
    base.PRODUCER_SCRIPT = HERE / "producer_main.py"
    base.CONSUMER_SCRIPT = HERE / "consumer_main.py"
    workdir = args.workdir or _default_workdir()
    default_rtid = REPO_ROOT / "bin" / (
        "rtid.exe" if sys.platform.startswith("win") else "rtid"
    )
    result = await base.run_once(
        ticks=args.ticks,
        drain_ticks=args.drain_ticks,
        tick_period=args.tick_period,
        rtid_binary=args.rtid_binary or default_rtid,
        workdir=workdir,
        keep_workdir=not args.no_keep_workdir,
        federate_timeout=args.timeout,
        log_level=args.log_level,
    )
    ok, detail = base.verify(result)
    print(
        "pyjevsim-real-model: "
        f"published={len(result['published'])} "
        f"received={len(result['received'])} verify={detail}"
    )
    if result.get("workdir"):
        print(f"workdir: {result['workdir']}")
    return 0 if ok else 1


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--ticks", type=int, default=12)
    parser.add_argument("--drain-ticks", type=int, default=4)
    parser.add_argument("--tick-period", type=float, default=0.05)
    parser.add_argument("--timeout", type=float, default=45.0)
    parser.add_argument("--rtid-binary", type=Path)
    parser.add_argument("--workdir", type=Path)
    parser.add_argument("--no-keep-workdir", action="store_true")
    parser.add_argument(
        "--log-level", choices=("debug", "info", "warn", "error"),
        default="info",
    )
    args = parser.parse_args(argv)
    try:
        return asyncio.run(run(args))
    except Exception as exc:  # noqa: BLE001
        print(f"pyjevsim-real-model: {exc}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
