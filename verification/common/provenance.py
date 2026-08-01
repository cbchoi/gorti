"""Capture reproducible provenance for production-shaped performance runs."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import platform
import subprocess
from collections.abc import Mapping, Sequence
from datetime import UTC, datetime
from pathlib import Path
from typing import Any

SCHEMA = "gorti.performance.provenance/v1"


class ProvenanceError(RuntimeError):
    """Raised when required provenance cannot be captured truthfully."""


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def _run_text(
    arguments: Sequence[str], *, cwd: Path | None = None, required: bool = False
) -> str:
    try:
        # Arguments are assembled by the local verification runner, never a shell.
        completed = subprocess.run(  # noqa: S603
            list(arguments),
            cwd=cwd,
            check=True,
            capture_output=True,
            text=True,
            timeout=10,
        )
    except (OSError, subprocess.SubprocessError) as error:
        if required:
            raise ProvenanceError(
                f"required provenance command failed: {arguments[0]}"
            ) from error
        return f"<unavailable:{type(error).__name__}>"
    output = completed.stdout.strip() or completed.stderr.strip()
    return output or "<empty>"


def capture_provenance(
    *,
    repo_root: Path,
    rtid_path: Path,
    python_path: Path,
    server_arguments: Sequence[str],
    verifier_arguments: Sequence[str] = (),
    benchmark: Mapping[str, Any],
) -> dict[str, Any]:
    repo_root = repo_root.resolve()
    rtid_path = rtid_path.resolve()
    python_path = python_path.resolve()
    status = _run_text(
        ("git", "status", "--porcelain"), cwd=repo_root, required=True
    )
    dirty = status not in ("<empty>", "")
    started_at = datetime.now(UTC).isoformat()

    return {
        "schema": SCHEMA,
        "implementation": "gorti",
        "captured_at_utc": started_at,
        "timestamps": {"started_at_utc": started_at, "finished_at_utc": None},
        "outcome": {"status": "running", "exit_code": None},
        "source": {
            "repo_root": str(repo_root),
            "commit": _run_text(
                ("git", "rev-parse", "HEAD"), cwd=repo_root, required=True
            ),
            "branch": _run_text(
                ("git", "branch", "--show-current"), cwd=repo_root, required=True
            ),
            "dirty": dirty,
            "status_porcelain": [] if status == "<empty>" else status.splitlines(),
        },
        "server": {
            "executable": str(rtid_path),
            "sha256": sha256_file(rtid_path),
            "version": _run_text((str(rtid_path), "--version")),
            "arguments": list(server_arguments),
        },
        "client": {
            "python_executable": str(python_path),
            "python_sha256": sha256_file(python_path),
            "python_version": _run_text((str(python_path), "--version")),
        },
        "host": {
            "platform": platform.platform(),
            "machine": platform.machine(),
            "processor": platform.processor(),
            "logical_cpu_count": os.cpu_count(),
        },
        "processes": [
            {"role": "rtid", "argv": [str(rtid_path), *server_arguments]},
            {"role": "verifier", "argv": [str(python_path), *verifier_arguments]},
        ],
        "benchmark": dict(benchmark),
    }


def write_provenance(path: Path, record: Mapping[str, Any]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(
        json.dumps(record, ensure_ascii=True, allow_nan=False, indent=2, sort_keys=True) + "\n",
        encoding="utf-8",
        newline="\n",
    )


def finalize_provenance(path: Path, *, outcome: str, exit_code: int) -> dict[str, Any]:
    if outcome not in {"passed", "failed"}:
        raise ProvenanceError("outcome must be passed or failed")
    record = json.loads(path.read_text(encoding="utf-8"))
    record["timestamps"]["finished_at_utc"] = datetime.now(UTC).isoformat()
    record["outcome"] = {"status": outcome, "exit_code": exit_code}
    write_provenance(path, record)
    return record


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    capture = commands.add_parser("capture")
    capture.add_argument("--repo-root", type=Path, required=True)
    capture.add_argument("--rtid", type=Path, required=True)
    capture.add_argument("--python", type=Path, required=True)
    capture.add_argument("--output", type=Path, required=True)
    capture.add_argument("--server-arg", action="append", default=[])
    capture.add_argument("--verifier-arg", action="append", default=[])
    capture.add_argument("--url", required=True)
    capture.add_argument("--seed", type=int, required=True)
    capture.add_argument("--count", type=int, required=True)
    capture.add_argument("--timeout", type=float, required=True)
    capture.add_argument("--outbox-batch-size", type=int, required=True)
    capture.add_argument("--outbox-flush-interval", required=True)
    capture.add_argument(
        "--tar-transport", choices=("threaded", "async"), required=True
    )
    capture.add_argument(
        "--callback-transport", choices=("queue", "direct"), required=True
    )
    capture.add_argument("--object-class", default="VerifierEntity")
    capture.add_argument("--interaction-class", default="VerifierMessage")
    capture.add_argument("--object-name", default="CommercialRtiVerifierEntity")
    capture.add_argument(
        "--logging-mode", choices=("file", "discard"), required=True
    )
    finalize = commands.add_parser("finalize")
    finalize.add_argument("--output", type=Path, required=True)
    finalize.add_argument("--outcome", choices=("passed", "failed"), required=True)
    finalize.add_argument("--exit-code", type=int, required=True)
    return parser


def main(argv: Sequence[str] | None = None) -> int:
    args = build_parser().parse_args(argv)
    if args.command == "finalize":
        finalize_provenance(
            args.output.resolve(), outcome=args.outcome, exit_code=args.exit_code
        )
        return 0
    benchmark = {
        "url": args.url,
        "seed": args.seed,
        "count": args.count,
        "timeout_seconds": args.timeout,
        "object_class": args.object_class,
        "interaction_class": args.interaction_class,
        "object_name": args.object_name,
        "logging_mode": args.logging_mode,
        "outbox_batch_size": args.outbox_batch_size,
        "outbox_flush_interval": args.outbox_flush_interval,
        "tar_transport": args.tar_transport,
        "callback_transport": args.callback_transport,
    }
    record = capture_provenance(
        repo_root=args.repo_root,
        rtid_path=args.rtid,
        python_path=args.python,
        server_arguments=args.server_arg,
        verifier_arguments=args.verifier_arg,
        benchmark=benchmark,
    )
    write_provenance(args.output, record)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
