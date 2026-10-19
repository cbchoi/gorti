"""Run the explicit Python HLA inventory; never treat skips or missing IDs as success."""

from __future__ import annotations

import argparse
import hashlib
import importlib.metadata
import json
import os
import platform
import shutil
import subprocess
import sys
from collections import Counter
from datetime import datetime, timezone
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
CONFIG = ROOT / "pysdk/tests/hla_campaign.json"
EXPECTED = ROOT / "pysdk/tests/hla_expected_nodeids.txt"


def _unique_configuration_object(pairs: list[tuple[str, object]]) -> dict:
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError(f"duplicate configuration key: {key}")
        result[key] = value
    return result


def read_configuration() -> dict:
    return json.loads(
        CONFIG.read_text(encoding="utf-8"), object_pairs_hook=_unique_configuration_object
    )


def inventory(root: Path, config: dict) -> list[str]:
    entries = config["files"]
    actual = {p.relative_to(root).as_posix() for p in (root / "pysdk/tests").rglob("test_*.py")}
    if actual != set(entries):
        raise ValueError(
            f"inventory drift: unclassified={sorted(actual - set(entries))}; "
            f"missing={sorted(set(entries) - actual)}"
        )
    selected = []
    for path, category in sorted(entries.items()):
        rule = config["categories"][category]
        if not rule["reason"]:
            raise ValueError(f"missing classification reason: {path}")
        if rule["selected"]:
            selected.append(path)
    if not selected:
        raise ValueError("empty HLA selection")
    return selected


def evaluate(expected: list[str], collected: list[str], reports: list[dict]) -> dict:
    by_id = {}
    for report in reports:
        by_id.setdefault(report["nodeid"], []).append(report)
    outcomes = {}
    for nodeid in collected:
        phases = by_id.get(nodeid, [])
        if any(p.get("wasxfail") is not None for p in phases):
            outcome = "xfail_or_xpass"
        elif any(p["outcome"] == "failed" for p in phases):
            outcome = "failed"
        elif any(p["outcome"] == "skipped" for p in phases):
            outcome = "skipped"
        elif Counter((p["when"], p["outcome"]) for p in phases) == Counter(
            {
                ("setup", "passed"): 1,
                ("call", "passed"): 1,
                ("teardown", "passed"): 1,
            }
        ):
            outcome = "passed"
        else:
            outcome = "incomplete"
        outcomes[nodeid] = outcome
    missing = sorted(set(expected) - set(collected))
    unexpected = sorted(set(collected) - set(expected))
    duplicate = sorted(node for node, count in Counter(collected).items() if count != 1)
    unexpected_reports = sorted(set(by_id) - set(collected))
    counts = dict(Counter(outcomes.values()))
    passed = bool(expected) and not (
        missing
        or unexpected
        or duplicate
        or unexpected_reports
        or len(expected) != len(set(expected))
        or any(value != "passed" for value in outcomes.values())
    )
    return {
        "passed": passed,
        "counts": counts,
        "outcomes": outcomes,
        "missing": missing,
        "unexpected": unexpected,
        "duplicate": duplicate,
        "unexpected_reports": unexpected_reports,
    }


def source_hashes() -> dict[str, str]:
    paths = []
    for name in ("pysdk", "rti", "proto", "examples", "cppsdk", "tests/conformance"):
        for path in (ROOT / name).rglob("*"):
            relative = path.relative_to(ROOT)
            if any(
                part.startswith(".") or part in {"__pycache__", "build", "node_modules"}
                for part in relative.parts
            ):
                continue
            if path.is_file() and path.suffix in {
                ".py",
                ".go",
                ".proto",
                ".xml",
                ".json",
                ".toml",
                ".txt",
                ".h",
                ".hpp",
                ".cc",
                ".cpp",
                ".cmake",
            }:
                paths.append(path)
    paths.extend([Path(__file__), CONFIG, EXPECTED, ROOT / "go.mod", ROOT / "go.sum"])
    return {
        p.relative_to(ROOT).as_posix(): hashlib.sha256(p.read_bytes()).hexdigest()
        for p in sorted(set(paths))
        if p.is_file()
    }


def cpp_publisher_identity() -> dict:
    default = ROOT / "cppsdk/build/examples/cpp-ieee1516e-smoke" / (
        "cpp_ieee1516e_publisher.exe" if os.name == "nt" else "cpp_ieee1516e_publisher"
    )
    path = Path(os.environ.get("GORTI_CPP_PUBLISHER", str(default))).resolve()
    return {
        "path": str(path),
        "sha256": hashlib.sha256(path.read_bytes()).hexdigest() if path.is_file() else None,
    }


def write_json(path: Path, data: object) -> None:
    path.write_text(json.dumps(data, indent=2, sort_keys=True) + "\n", encoding="utf-8")


class EvidencePlugin:
    def __init__(self, output: Path):
        self.output = output
        self.collected = []
        self.reports = []
        self.collection_issues = []

    def pytest_collection_finish(self, session):
        self.collected = [item.nodeid for item in session.items]
        self.flush(None)

    def pytest_collectreport(self, report):
        if report.failed or report.skipped:
            self.collection_issues.append(
                {"nodeid": report.nodeid, "outcome": report.outcome, "detail": str(report.longrepr)}
            )

    def pytest_runtest_logreport(self, report):
        self.reports.append(
            {
                "nodeid": report.nodeid,
                "when": report.when,
                "outcome": report.outcome,
                "wasxfail": getattr(report, "wasxfail", None),
                "duration": report.duration,
                "detail": str(report.longrepr) if report.longrepr else "",
            }
        )
        self.flush(None)

    def pytest_runtest_logstart(self, nodeid, location):
        (self.output.parent / "current-test.txt").write_text(nodeid, encoding="utf-8")

    def pytest_sessionfinish(self, session, exitstatus):
        self.flush(int(exitstatus))

    def flush(self, exitstatus):
        write_json(
            self.output,
            {
                "collected": self.collected,
                "reports": self.reports,
                "collection_issues": self.collection_issues,
                "pytest_exit_code": exitstatus,
            },
        )


def worker(output: Path, collect_only: bool) -> int:
    import pytest

    selected = inventory(ROOT, read_configuration())
    args = [
        *selected,
        "-c",
        str(ROOT / "pysdk/pyproject.toml"),
        "--rootdir",
        str(ROOT),
        "-q",
        "-ra",
        # WSL DrvFS cannot reliably truncate pytest's anonymous FD capture file.
        "--capture=sys",
        "-p",
        "no:cacheprovider",
        "--basetemp",
        str(output.parent / "tmp"),
    ]
    if collect_only:
        args.append("--collect-only")
    else:
        args.append(f"--junitxml={output.parent / 'junit.xml'}")
    return int(pytest.main(args, plugins=[EvidencePlugin(output)]))


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--artifacts",
        type=Path,
        required=True,
        help="New evidence directory outside the repository",
    )
    parser.add_argument(
        "--collect-only",
        action="store_true",
        help="Write candidate IDs for review; does not certify or update expectations",
    )
    parser.add_argument("--worker", action="store_true", help=argparse.SUPPRESS)
    args = parser.parse_args()
    directory = args.artifacts.resolve()
    if directory == ROOT or ROOT in directory.parents:
        parser.error("artifacts must be outside the repository")
    if args.worker:
        return worker(directory / "pytest.json", args.collect_only)
    directory.mkdir(parents=True, exist_ok=False)
    config = read_configuration()
    selected = inventory(ROOT, config)
    versions = {dist.metadata["Name"]: dist.version for dist in importlib.metadata.distributions()}
    preflight = {
        "python": sys.version,
        "executable": sys.executable,
        "platform": platform.platform(),
        "packages": versions,
        "selected_files": selected,
        "excluded_files": {p: c for p, c in config["files"].items() if p not in selected},
        "categories": config["categories"],
        "started_utc": datetime.now(timezone.utc).isoformat(),
    }
    go = shutil.which("go")
    preflight["go_executable"] = go
    if go is not None:
        version = subprocess.run(  # noqa: S603 - resolved local tool, no shell
            [go, "version"], capture_output=True, text=True, check=False, timeout=30
        )
        preflight["go_version"] = version.stdout.strip()
        preflight["go_version_exit_code"] = version.returncode
    preflight["go_cache"] = os.environ.get("GOCACHE", str(ROOT.parent / ".gocache"))
    preflight["go_module_cache"] = os.environ.get(
        "GOMODCACHE", str(ROOT.parent / ".tools/gomodcache")
    )
    publisher_before = cpp_publisher_identity()
    preflight["cpp_publisher"] = publisher_before["path"]
    preflight["cpp_publisher_sha256"] = publisher_before["sha256"]
    write_json(directory / "cpp-publisher-before.json", publisher_before)
    pin_errors = []
    for name, version in config["dependencies"].items():
        if versions.get(name) != version:
            pin_errors.append(f"{name}: expected {version}, got {versions.get(name, 'missing')}")
    preflight["dependency_errors"] = pin_errors
    write_json(directory / "environment.json", preflight)
    if pin_errors:
        print("Dependency prerequisite failure: " + "; ".join(pin_errors))
        return 2
    before = source_hashes()
    write_json(directory / "source-before.json", before)
    env = os.environ.copy()
    env.update(
        {
            "PYTHONPATH": str(ROOT / "pysdk"),
            "PYTHONDONTWRITEBYTECODE": "1",
            "PYTEST_DISABLE_PLUGIN_AUTOLOAD": "1",
            "PYTEST_PLUGINS": "pytest_asyncio.plugin",
            "PYTEST_ADDOPTS": "",
            "GORTI_RUN_LIVE_E2E": "1",
            "GORTI_LIVE_ARTIFACTS": str(directory),
            "GORTI_RESEARCH_CONFIG": "",
            "TEMP": str(directory),
            "TMP": str(directory),
            "TMPDIR": str(directory),
        }
    )
    for name in ("GORTI_LIVE_RTID", "RTID_BINARY", "GORTI_FEDERATE_HOST"):
        env.pop(name, None)
    env.setdefault("GOCACHE", str(ROOT.parent / ".gocache"))
    env.setdefault("GOMODCACHE", str(ROOT.parent / ".tools/gomodcache"))
    write_json(
        directory / "test-environment.json",
        {
            key: env[key]
            for key in (
                "GOCACHE",
                "GOMODCACHE",
                "GORTI_RUN_LIVE_E2E",
                "GORTI_LIVE_ARTIFACTS",
                "GORTI_RESEARCH_CONFIG",
                "PYTEST_DISABLE_PLUGIN_AUTOLOAD",
                "PYTEST_PLUGINS",
                "PYTEST_ADDOPTS",
                "TEMP",
                "TMP",
                "TMPDIR",
            )
        },
    )
    command = [
        sys.executable,
        "-B",
        str(Path(__file__).resolve()),
        "--worker",
        "--artifacts",
        str(directory),
    ]
    if args.collect_only:
        command.append("--collect-only")
    with (directory / "pytest.log").open("wb") as log:
        result = subprocess.run(  # noqa: S603 - owned script and interpreter, no shell
            command,
            cwd=ROOT,
            env=env,
            stdout=log,
            stderr=subprocess.STDOUT,
            check=False,
            timeout=1800,
        )
    data = json.loads((directory / "pytest.json").read_text(encoding="utf-8"))
    (directory / "candidate-nodeids.txt").write_text(
        "\n".join(data["collected"]) + "\n", encoding="utf-8"
    )
    after = source_hashes()
    write_json(directory / "source-after.json", after)
    publisher_after = cpp_publisher_identity()
    write_json(directory / "cpp-publisher-after.json", publisher_after)
    changed = sorted(p for p in before.keys() | after.keys() if before.get(p) != after.get(p))
    if args.collect_only:
        print(
            f"Collected {len(data['collected'])} IDs from {len(selected)} files; review {directory}"
        )
        return 0 if result.returncode == 0 and not data["collection_issues"] else 1
    expected = EXPECTED.read_text(encoding="utf-8").splitlines()
    summary = evaluate(expected, data["collected"], data["reports"])
    summary.update(
        {
            "source_changes": changed,
            "cpp_publisher_changed": publisher_before != publisher_after,
            "source_identity_before_sha256": hashlib.sha256(
                json.dumps(before, sort_keys=True).encode("utf-8")
            ).hexdigest(),
            "source_identity_after_sha256": hashlib.sha256(
                json.dumps(after, sort_keys=True).encode("utf-8")
            ).hexdigest(),
            "collection_issues": data["collection_issues"],
            "pytest_exit_code": result.returncode,
            "expected_count": len(expected),
            "collected_count": len(data["collected"]),
            "manifest_sha256": hashlib.sha256(CONFIG.read_bytes()).hexdigest(),
            "expected_ids_sha256": hashlib.sha256(EXPECTED.read_bytes()).hexdigest(),
            "completed_utc": datetime.now(timezone.utc).isoformat(),
        }
    )
    summary["passed"] = (
        summary["passed"]
        and not changed
        and publisher_before == publisher_after
        and not data["collection_issues"]
        and result.returncode == 0
    )
    write_json(directory / "summary.json", summary)
    print(json.dumps({key: value for key, value in summary.items() if key != "outcomes"}, indent=2))
    print(f"Evidence: {directory}")
    return 0 if summary["passed"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
