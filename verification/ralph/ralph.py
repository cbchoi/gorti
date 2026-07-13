#!/usr/bin/env python3
"""Bounded PLAN-DO-REVIEW-REFLECT orchestration for Pitch/gorti parity runs."""

from __future__ import annotations

import argparse
import contextlib
import difflib
import hashlib
import json
import os
import shutil
import signal
import subprocess  # noqa: S404
import sys
import time
from collections.abc import Sequence
from dataclasses import asdict, dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, cast

EXIT_MATCH = 0
EXIT_NO_MATCH = 1
EXIT_CONFIGURATION = 2
DIFF_LINE_LIMIT = 200


def utc_now() -> str:
    return datetime.now(timezone.utc).isoformat()


def write_json(path: Path, value: object) -> None:
    path.write_text(json.dumps(value, indent=2, sort_keys=True) + "\n", encoding="utf-8")


def parse_json_args(value: str, option: str) -> list[str]:
    try:
        parsed = json.loads(value)
    except json.JSONDecodeError as exc:
        raise argparse.ArgumentTypeError(f"{option} must be a JSON array: {exc}") from exc
    if not isinstance(parsed, list) or not all(isinstance(item, str) for item in parsed):
        raise argparse.ArgumentTypeError(f"{option} must be a JSON array of strings")
    return parsed


def render(value: str, context: dict[str, object]) -> str:
    for name, replacement in context.items():
        value = value.replace("{" + name + "}", str(replacement))
    return value


def resolve_executable(command: str, cwd: Path) -> str | None:
    candidate = Path(command).expanduser()
    if candidate.is_absolute() or candidate.parent != Path("."):
        if not candidate.is_absolute():
            candidate = cwd / candidate
        return str(candidate.resolve()) if candidate.is_file() else None
    return shutil.which(command)


def file_digest(path: Path) -> tuple[int, str]:
    digest = hashlib.sha256()
    size = 0
    with path.open("rb") as stream:
        while chunk := stream.read(1024 * 1024):
            size += len(chunk)
            digest.update(chunk)
    return size, digest.hexdigest()


@dataclass(frozen=True)
class ProcessResult:
    role: str
    argv: list[str]
    status: str
    exit_code: int | None
    started_at: str
    finished_at: str
    duration_seconds: float
    log: str
    error: str | None = None

    @property
    def succeeded(self) -> bool:
        return self.status == "completed" and self.exit_code == 0


def terminate_process_tree(process: subprocess.Popen[bytes]) -> None:
    if process.poll() is not None:
        return
    if os.name == "nt":
        try:
            taskkill = shutil.which("taskkill") or "taskkill.exe"
            subprocess.run(  # noqa: S603
                [taskkill, "/PID", str(process.pid), "/T", "/F"],
                check=False,
                stdout=subprocess.DEVNULL,
                stderr=subprocess.DEVNULL,
                timeout=10,
            )
        except (OSError, subprocess.SubprocessError):
            process.kill()
        if process.poll() is None:
            process.kill()
    else:
        try:
            os.killpg(process.pid, signal.SIGTERM)
            process.wait(timeout=5)
        except (ProcessLookupError, subprocess.TimeoutExpired):
            with contextlib.suppress(ProcessLookupError):
                os.killpg(process.pid, signal.SIGKILL)


def run_process(
    role: str,
    argv: list[str],
    cwd: Path,
    iteration_dir: Path,
    seed: int,
    iteration: int,
    timeout: float,
) -> ProcessResult:
    log_path = iteration_dir / f"{role}.log"
    started_at = utc_now()
    started = time.monotonic()
    environment = os.environ.copy()
    environment.update(
        {
            "RALPH_SEED": str(seed),
            "RALPH_ITERATION": str(iteration),
            "RALPH_ROLE": role,
            "RALPH_OUTPUT_DIR": str(iteration_dir),
        }
    )
    creationflags = subprocess.CREATE_NEW_PROCESS_GROUP if os.name == "nt" else 0
    popen_options: dict[str, Any] = {
        "cwd": cwd,
        "env": environment,
        "stdin": subprocess.DEVNULL,
        "stdout": None,
        "stderr": subprocess.STDOUT,
        "shell": False,
        "creationflags": creationflags,
    }
    if os.name != "nt":
        popen_options["start_new_session"] = True

    process: subprocess.Popen[bytes] | None = None
    status = "spawn_error"
    exit_code: int | None = None
    error: str | None = None
    with log_path.open("wb") as log_stream:
        popen_options["stdout"] = log_stream
        try:
            process = subprocess.Popen(argv, **popen_options)  # noqa: S603
            try:
                exit_code = process.wait(timeout=timeout)
                status = "completed"
            except subprocess.TimeoutExpired:
                status = "timed_out"
                error = f"process exceeded {timeout:g} seconds"
                terminate_process_tree(process)
                try:
                    exit_code = process.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    process.kill()
                    exit_code = process.wait()
        except OSError as exc:
            error = f"{type(exc).__name__}: {exc}"
            log_stream.write((error + "\n").encode("utf-8", errors="replace"))

    result = ProcessResult(
        role=role,
        argv=argv,
        status=status,
        exit_code=exit_code,
        started_at=started_at,
        finished_at=utc_now(),
        duration_seconds=round(time.monotonic() - started, 6),
        log=log_path.name,
        error=error,
    )
    write_json(iteration_dir / f"{role}.process.json", asdict(result))
    return result


def write_diff(pitch_log: Path, gorti_log: Path, output: Path, byte_limit: int) -> bool:
    pitch_size = pitch_log.stat().st_size
    gorti_size = gorti_log.stat().st_size
    if pitch_size > byte_limit or gorti_size > byte_limit:
        output.write_text(
            "Diff omitted because at least one log exceeds "
            f"the {byte_limit}-byte diff limit. See review.json for hashes.\n",
            encoding="utf-8",
        )
        return True

    pitch_lines = pitch_log.read_text(encoding="utf-8", errors="replace").splitlines(True)
    gorti_lines = gorti_log.read_text(encoding="utf-8", errors="replace").splitlines(True)
    diff = difflib.unified_diff(
        pitch_lines,
        gorti_lines,
        fromfile="pitch.log",
        tofile="gorti.log",
    )
    lines: list[str] = []
    truncated = False
    for line_number, line in enumerate(diff):
        if line_number >= DIFF_LINE_LIMIT:
            truncated = True
            break
        lines.append(line)
    if truncated:
        lines.append(f"\n... diff truncated after {DIFF_LINE_LIMIT} lines ...\n")
    output.write_text("".join(lines), encoding="utf-8")
    return truncated


def review(
    iteration_dir: Path,
    pitch: ProcessResult,
    gorti: ProcessResult,
    diff_byte_limit: int,
    review_mode: str,
) -> dict[str, object]:
    canonical_pitch = iteration_dir / "pitch.ndjson"
    canonical_gorti = iteration_dir / "gorti.ndjson"
    use_semantic = review_mode == "semantic" or (
        review_mode == "auto" and (canonical_pitch.exists() or canonical_gorti.exists())
    )
    pitch_log = canonical_pitch if use_semantic else iteration_dir / pitch.log
    gorti_log = canonical_gorti if use_semantic else iteration_dir / gorti.log
    pitch_exists = pitch_log.is_file()
    gorti_exists = gorti_log.is_file()
    pitch_size, pitch_hash = file_digest(pitch_log) if pitch_exists else (0, None)
    gorti_size, gorti_hash = file_digest(gorti_log) if gorti_exists else (0, None)
    logs_equal = (
        pitch_exists
        and gorti_exists
        and pitch_size == gorti_size
        and pitch_hash == gorti_hash
    )
    comparison: dict[str, object] | None = None
    comparison_error: str | None = None
    matched = logs_equal
    if use_semantic:
        if not pitch_exists or not gorti_exists:
            comparison_error = "one or both canonical NDJSON logs are missing"
            matched = False
        else:
            common_dir = Path(__file__).resolve().parents[1] / "common"
            sys.path.insert(0, str(common_dir))
            try:
                from compare_logs import compare_files

                comparison = compare_files(pitch_log, gorti_log)
                matched = bool(comparison.get("semantic_match"))
            except (ImportError, OSError, ValueError, json.JSONDecodeError) as exc:
                comparison_error = f"semantic comparison failed: {type(exc).__name__}: {exc}"
                matched = False
            finally:
                if sys.path and sys.path[0] == str(common_dir):
                    sys.path.pop(0)
    diff_path: str | None = None
    diff_truncated = False
    if not matched and pitch_exists and gorti_exists:
        diff_path = "review.diff"
        diff_truncated = write_diff(
            pitch_log, gorti_log, iteration_dir / diff_path, diff_byte_limit
        )
    result: dict[str, object] = {
        "phase": "REVIEW",
        "comparison_mode": "semantic" if use_semantic else "captured_bytes",
        "pitch": {
            "bytes": pitch_size,
            "sha256": pitch_hash,
            "log": pitch_log.name,
            "log_exists": pitch_exists,
            "succeeded": pitch.succeeded,
        },
        "gorti": {
            "bytes": gorti_size,
            "sha256": gorti_hash,
            "log": gorti_log.name,
            "log_exists": gorti_exists,
            "succeeded": gorti.succeeded,
        },
        "logs_equal": logs_equal,
        "matched": matched,
        "passed": pitch.succeeded and gorti.succeeded and matched,
        "comparison": comparison,
        "comparison_error": comparison_error,
        "diff": diff_path,
        "diff_truncated": diff_truncated,
    }
    write_json(iteration_dir / "review.json", result)
    return result


def create_plan(
    iteration_dir: Path,
    iteration: int,
    seed: int,
    cwd: Path,
    pitch_argv: list[str],
    gorti_argv: list[str],
    timeout: float,
) -> tuple[dict[str, object], list[str], list[str]]:
    context: dict[str, object] = {
        "seed": seed,
        "iteration": iteration,
        "output_dir": str(iteration_dir.parent),
        "run_dir": str(iteration_dir),
        "role": "pitch",
        "log": str(iteration_dir / "pitch.ndjson"),
    }
    errors: list[str] = []
    rendered: dict[str, list[str]] = {}
    for role, raw_argv in (("pitch", pitch_argv), ("gorti", gorti_argv)):
        context["role"] = role
        context["log"] = str(iteration_dir / f"{role}.ndjson")
        argv = [render(item, context) for item in raw_argv]
        resolved = resolve_executable(argv[0], cwd)
        if resolved is None:
            errors.append(f"{role}: executable not found: {argv[0]}")
        else:
            argv[0] = resolved
        rendered[role] = argv

    dependencies = [
        {"name": "working_directory", "ready": cwd.is_dir(), "value": str(cwd)},
        {
            "name": "pitch_executable",
            "ready": resolve_executable(rendered["pitch"][0], cwd) is not None,
            "value": rendered["pitch"][0],
        },
        {
            "name": "gorti_executable",
            "ready": resolve_executable(rendered["gorti"][0], cwd) is not None,
            "value": rendered["gorti"][0],
        },
    ]
    if not cwd.is_dir():
        errors.append(f"working directory does not exist: {cwd}")
    plan: dict[str, object] = {
        "phase": "PLAN",
        "iteration": iteration,
        "seed": seed,
        "timeout_seconds": timeout,
        "status": "ready" if not errors else "blocked",
        "dependencies": dependencies,
        "pitch_argv": rendered["pitch"],
        "gorti_argv": rendered["gorti"],
        "errors": errors,
    }
    write_json(iteration_dir / "plan.json", plan)
    return plan, rendered["pitch"], rendered["gorti"]


def reflect(
    iteration_dir: Path,
    iteration: int,
    max_iterations: int,
    review_result: dict[str, object] | None,
    plan_errors: list[str],
) -> dict[str, object]:
    if plan_errors:
        decision = "stop"
        reason = "PLAN dependencies are not ready; retrying unchanged configuration cannot help."
        retry = False
    elif review_result and review_result["passed"]:
        decision = "complete"
        if review_result["comparison_mode"] == "semantic":
            reason = "Both commands succeeded and their canonical logs match semantically."
        else:
            reason = "Both commands succeeded and their captured logs match byte-for-byte."
        retry = False
    elif iteration < max_iterations:
        decision = "retry"
        reason = "The runs failed or differed, and the iteration budget permits another attempt."
        retry = True
    else:
        decision = "stop"
        reason = "The runs failed or differed and the maximum iteration count was reached."
        retry = False
    result: dict[str, object] = {
        "phase": "REFLECT",
        "iteration": iteration,
        "decision": decision,
        "retry": retry,
        "reason": reason,
        "remaining_iterations": max(0, max_iterations - iteration),
    }
    write_json(iteration_dir / "reflect.json", result)
    report = [
        f"# Ralph iteration {iteration}\n",
        "\n",
        f"- Decision: **{decision}**\n",
        f"- Retry: **{'yes' if retry else 'no'}**\n",
        f"- Reason: {reason}\n",
    ]
    if review_result:
        pitch_review = cast(dict[str, object], review_result["pitch"])
        gorti_review = cast(dict[str, object], review_result["gorti"])
        report.extend(
            [
                f"- Pitch succeeded: {pitch_review['succeeded']}\n",
                f"- gorti succeeded: {gorti_review['succeeded']}\n",
                f"- Comparison mode: {review_result['comparison_mode']}\n",
                f"- Logs equal: {review_result['logs_equal']}\n",
            ]
        )
    if plan_errors:
        report.append(f"- PLAN errors: {'; '.join(plan_errors)}\n")
    (iteration_dir / "report.md").write_text("".join(report), encoding="utf-8")
    return result


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--max-iterations", type=int, default=3)
    parser.add_argument("--seed", type=int, default=0)
    parser.add_argument("--output-dir", type=Path, required=True)
    parser.add_argument("--working-dir", type=Path, default=Path.cwd())
    parser.add_argument("--timeout", type=float, default=300.0, help="seconds per command")
    parser.add_argument("--diff-byte-limit", type=int, default=1024 * 1024)
    parser.add_argument(
        "--review-mode", choices=("auto", "captured", "semantic"), default="auto"
    )
    parser.add_argument("--pitch-command", required=True)
    parser.add_argument("--pitch-arg", action="append", default=[])
    parser.add_argument("--pitch-args-json", default="[]")
    parser.add_argument("--gorti-command", required=True)
    parser.add_argument("--gorti-arg", action="append", default=[])
    parser.add_argument("--gorti-args-json", default="[]")
    return parser


def validate_args(parser: argparse.ArgumentParser, args: argparse.Namespace) -> None:
    if args.max_iterations < 1:
        parser.error("--max-iterations must be at least 1")
    if args.timeout <= 0:
        parser.error("--timeout must be greater than 0")
    if args.diff_byte_limit < 0:
        parser.error("--diff-byte-limit must be at least 0")
    args.pitch_args_json = parse_json_args(args.pitch_args_json, "--pitch-args-json")
    args.gorti_args_json = parse_json_args(args.gorti_args_json, "--gorti-args-json")


def prepare_output(path: Path) -> Path:
    output = path.expanduser().resolve()
    output.mkdir(parents=True, exist_ok=True)
    if any(output.iterdir()):
        raise ValueError(f"output directory is not empty: {output}")
    return output


def orchestrate(args: argparse.Namespace) -> int:
    try:
        output_dir = prepare_output(args.output_dir)
    except (OSError, ValueError) as exc:
        print(f"ralph: {exc}", file=sys.stderr)
        return EXIT_CONFIGURATION
    cwd = args.working_dir.expanduser().resolve()
    pitch_argv = [args.pitch_command, *args.pitch_arg, *args.pitch_args_json]
    gorti_argv = [args.gorti_command, *args.gorti_arg, *args.gorti_args_json]
    run_record = {
        "started_at": utc_now(),
        "max_iterations": args.max_iterations,
        "seed": args.seed,
        "output_dir": str(output_dir),
        "working_dir": str(cwd),
    }
    write_json(output_dir / "run.json", run_record)

    final_reflection: dict[str, object] | None = None
    exit_code = EXIT_NO_MATCH
    completed_iterations = 0
    for iteration in range(1, args.max_iterations + 1):
        completed_iterations = iteration
        iteration_dir = output_dir / f"iteration-{iteration:03d}"
        iteration_dir.mkdir()
        plan, pitch_command, gorti_command = create_plan(
            iteration_dir,
            iteration,
            args.seed,
            cwd,
            pitch_argv,
            gorti_argv,
            args.timeout,
        )
        plan_errors = list(plan["errors"])
        if plan_errors:
            final_reflection = reflect(
                iteration_dir, iteration, args.max_iterations, None, plan_errors
            )
            exit_code = EXIT_CONFIGURATION
            break

        pitch_result = run_process(
            "pitch", pitch_command, cwd, iteration_dir, args.seed, iteration, args.timeout
        )
        gorti_result = run_process(
            "gorti", gorti_command, cwd, iteration_dir, args.seed, iteration, args.timeout
        )
        review_result = review(
            iteration_dir,
            pitch_result,
            gorti_result,
            args.diff_byte_limit,
            args.review_mode,
        )
        final_reflection = reflect(
            iteration_dir, iteration, args.max_iterations, review_result, []
        )
        if final_reflection["decision"] == "complete":
            exit_code = EXIT_MATCH
            break
        if not final_reflection["retry"]:
            break

    summary = {
        **run_record,
        "finished_at": utc_now(),
        "iterations": completed_iterations,
        "exit_code": exit_code,
        "decision": final_reflection["decision"] if final_reflection else "stop",
        "reason": final_reflection["reason"] if final_reflection else "No iteration ran.",
    }
    write_json(output_dir / "summary.json", summary)
    (output_dir / "summary.md").write_text(
        "# Ralph summary\n\n"
        f"- Decision: **{summary['decision']}**\n"
        f"- Iterations: {completed_iterations}/{args.max_iterations}\n"
        f"- Seed: {args.seed}\n"
        f"- Exit code: {exit_code}\n"
        f"- Reason: {summary['reason']}\n",
        encoding="utf-8",
    )
    print(f"Ralph {summary['decision']} after {completed_iterations} iteration(s): {output_dir}")
    return exit_code


def main(argv: Sequence[str] | None = None) -> int:
    parser = build_parser()
    args = parser.parse_args(argv)
    try:
        validate_args(parser, args)
    except argparse.ArgumentTypeError as exc:
        parser.error(str(exc))
    return orchestrate(args)


if __name__ == "__main__":
    raise SystemExit(main())
