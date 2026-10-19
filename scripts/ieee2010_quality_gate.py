#!/usr/bin/env python3
"""Fail-closed LOCAL/MANUAL IEEE 1516-2010 evidence gate (stdlib only).

Run from any directory with --run; the default/--check/--dry-run NEVER executes
tests and exits 2, not gate success. Exit 0 is an executed gate PASS; 1 is FAIL.
JSON is written to stdout, including raw Go transcripts for executed commands.

Schema v1 is enforced by validate_manifest; see the repository example manifest.
All listed test commands are required. A supported-profile PASS covers only its
named requirements and exact tests. A full-2010 PASS checks evidence prerequisites,
not certification or the truth/completeness of human normative interpretations.
Full mode requires a reviewed inventory across all three standards, resolved
applicability, clause/test traceability and hashed independent campaign evidence.
This automates record checks for parts of G0/G1 in ieee1516-2010-quality-gates.md;
it does not award that document's L1-L4 claims or replace G2-G5 human review.
Schema v1 conservatively requires executable tests for each applicable row;
review-only verification arguments need a future, separately reviewed extension.

Only fixed go test commands on exact local packages/top-level tests are supported.
No shell or manifest-supplied executable/flags are accepted. Running trusted Go
repository tests still executes code: this is NOT a sandbox. PATH, the toolchain,
source_id, scope declarations and independent-reviewer identity are trusted local
inputs, not authenticated attestations. Use immutable reviewed sources for claims.
No CI policy, reports or source files are modified by this tool.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import re
import subprocess
import sys
from datetime import datetime, timezone
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
DEFAULT_MANIFEST = "engineering/verification/ieee1516-2010-evidence.json"
STANDARDS = {"IEEE 1516-2010", "IEEE 1516.1-2010", "IEEE 1516.2-2010"}
CLAIMS = ("supported-profile", "full-2010")


class GateError(ValueError):
    """Invalid or unverifiable gate input."""


def require(condition, message):
    if not condition:
        raise GateError(message)


def shape(value, keys, label):
    require(isinstance(value, dict), f"{label}: expected object")
    require(set(value) == set(keys.split()), f"{label}: expected exactly {keys}")


def string(value, label, empty=False):
    require(isinstance(value, str) and (empty or bool(value.strip())),
            f"{label}: expected {'possibly empty ' if empty else 'nonempty '}string")


def strings(value, label, nonempty=False):
    require(isinstance(value, list), f"{label}: expected array")
    for item in value:
        string(item, label)
    require(len(value) == len(set(value)), f"{label}: duplicate entries")
    require(not nonempty or bool(value), f"{label}: cannot be empty")


def indexed(rows, label):
    require(isinstance(rows, list), f"{label}: expected array")
    result = {}
    for row in rows:
        require(isinstance(row, dict), f"{label}: expected object entries")
        string(row.get("id"), f"{label}.id")
        require(row["id"] not in result, f"{label}: duplicate id {row['id']}")
        result[row["id"]] = row
    return result


def validate_manifest(data):
    shape(data, "schema_version claim profile normative requirements tests independent_evidence", "manifest")
    require(type(data["schema_version"]) is int and data["schema_version"] == 1,
            "schema_version: expected integer 1")
    require(data["claim"] in CLAIMS, "claim: expected supported-profile or full-2010")
    profile = data["profile"]
    shape(profile, "id scope bindings platforms source_id required_requirements limitations", "profile")
    for key in ("id", "scope", "source_id"):
        string(profile[key], "profile." + key)
    for key in ("bindings", "platforms", "required_requirements", "limitations"):
        strings(profile[key], "profile." + key, nonempty=True)
    normative = data["normative"]
    shape(normative, "coverage_complete standards requirement_ids review_evidence", "normative")
    require(type(normative["coverage_complete"]) is bool, "normative.coverage_complete: expected boolean")
    strings(normative["standards"], "normative.standards")
    require(set(normative["standards"]) <= STANDARDS, "normative.standards: unsupported edition")
    strings(normative["requirement_ids"], "normative.requirement_ids")
    if normative["review_evidence"] is not None:
        string(normative["review_evidence"], "normative.review_evidence")
    tests = indexed(data["tests"], "tests")
    require(bool(tests), "tests: at least one required command is necessary")
    for test in tests.values():
        shape(test, "id package required_tests", "test")
        string(test["package"], "test.package")
        require(re.fullmatch(r"\./(?:[A-Za-z0-9_-]+/)*[A-Za-z0-9_-]+", test["package"]),
                "test.package: expected exact local package, not glob/flag/shell command")
        strings(test["required_tests"], "test.required_tests", nonempty=True)
        for name in test["required_tests"]:
            require(re.fullmatch(r"Test[A-Z0-9_][A-Za-z0-9_]*", name),
                    f"test.required_tests: expected literal top-level Go test name: {name}")
    requirements = indexed(data["requirements"], "requirements")
    for row in requirements.values():
        shape(row, "id standard clause applicability rationale status tests", "requirement")
        require(isinstance(row["standard"], str) and row["standard"] in STANDARDS,
                "requirement.standard: unsupported standard")
        if row["clause"] is not None:
            string(row["clause"], "requirement.clause")
        require(row["applicability"] in ("applicable", "not-applicable", "unresolved"),
                "requirement.applicability: invalid value")
        require(row["status"] in ("resolved", "unresolved"), "requirement.status: invalid value")
        string(row["rationale"], "requirement.rationale", empty=True)
        strings(row["tests"], "requirement.tests")
        require(set(row["tests"]) <= tests.keys(), f"{row['id']}: unknown test reference")
    require(set(profile["required_requirements"]) <= requirements.keys(),
            "profile.required_requirements: unknown requirement reference")
    for evidence in indexed(data["independent_evidence"], "independent_evidence").values():
        shape(evidence, "id kind reviewer independent source_id verdict path sha256 requirement_ids", "evidence")
        require(evidence["kind"] in ("normative-review", "independent-test"), "evidence.kind: invalid value")
        for key in ("reviewer", "source_id", "path", "sha256"):
            string(evidence[key], "evidence." + key)
        require(type(evidence["independent"]) is bool, "evidence.independent: expected boolean")
        require(evidence["verdict"] in ("pass", "fail", "skip", "not-run"), "evidence.verdict: invalid value")
        require(re.fullmatch(r"[0-9a-f]{64}", evidence["sha256"]), "evidence.sha256: expected lowercase SHA-256")
        strings(evidence["requirement_ids"], "evidence.requirement_ids", nonempty=True)
    return data


def unique_object(pairs):
    result = {}
    for key, value in pairs:
        require(key not in result, f"duplicate JSON key: {key}")
        result[key] = value
    return result


def reject_constant(value):
    raise GateError(f"nonfinite JSON constant: {value}")


def decode_json(text):
    try:
        return json.loads(text, object_pairs_hook=unique_object, parse_constant=reject_constant)
    except (ValueError, RecursionError) as error:
        raise GateError(f"invalid JSON: {error}") from error


def load_manifest(path):
    try:
        return validate_manifest(decode_json(Path(path).read_text(encoding="utf-8-sig")))
    except (OSError, UnicodeError) as error:
        raise GateError(f"cannot read manifest: {error}") from error


def local_path(root, relative):
    path = Path(relative)
    require(not path.is_absolute() and not path.drive and ".." not in path.parts,
            f"path must stay inside repository: {relative}")
    resolved = (root / path).resolve()
    require(resolved.is_relative_to(root), f"path escapes repository: {relative}")
    return resolved


def policy_errors(data, root, claim):
    errors = []
    requirements = {row["id"]: row for row in data["requirements"]}
    selected = set(data["profile"]["required_requirements"])
    if claim == "full-2010":
        normative = data["normative"]
        selected = set(requirements)
        if not normative["coverage_complete"]:
            errors.append("normative requirement coverage is incomplete")
        if set(normative["standards"]) != STANDARDS:
            errors.append("normative inventory must cover all three 2010 standards")
        inventory = set(normative["requirement_ids"])
        if not inventory or inventory != selected:
            errors.append("normative inventory/requirement coverage mismatch: " +
                          ", ".join(sorted(inventory ^ selected)))
        if {row["standard"] for row in requirements.values()} != STANDARDS:
            errors.append("normative requirement rows must cover all three 2010 standards")
        for row in requirements.values():
            if not row["clause"]:
                errors.append(f"{row['id']}: missing normative clause")
        valid_evidence = {}
        for evidence in data["independent_evidence"]:
            try:
                require(evidence["independent"] and evidence["verdict"] == "pass",
                        "independent passing evidence required")
                require(evidence["source_id"] == data["profile"]["source_id"],
                        "evidence source_id does not match profile")
                require(set(evidence["requirement_ids"]) <= selected,
                        "evidence references unknown requirements")
                payload = local_path(root, evidence["path"]).read_bytes()
                require(bool(payload), "empty evidence artifact")
                require(hashlib.sha256(payload).hexdigest() == evidence["sha256"],
                        "evidence SHA-256 mismatch")
                valid_evidence[evidence["id"]] = evidence
            except (GateError, OSError) as error:
                errors.append(f"{evidence['id']}: {error}")
        review = valid_evidence.get(normative["review_evidence"])
        if not review or review["kind"] != "normative-review" or not selected <= set(review["requirement_ids"]):
            errors.append("normative inventory lacks independent review evidence covering every row")
        independent = set()
        for evidence in valid_evidence.values():
            if evidence["kind"] == "independent-test":
                independent.update(evidence["requirement_ids"])
        applicable = {key for key, row in requirements.items() if row["applicability"] == "applicable"}
        if not applicable or not applicable <= independent:
            errors.append("independent executed campaign evidence missing for applicable requirements: " +
                          ", ".join(sorted(applicable - independent)))
    for key in sorted(selected):
        row = requirements[key]
        if row["status"] != "resolved" or row["applicability"] == "unresolved":
            errors.append(f"{key}: unresolved requirement/applicability")
        if not row["rationale"].strip():
            errors.append(f"{key}: missing applicability rationale")
        if row["applicability"] == "not-applicable":
            if claim == "supported-profile":
                errors.append(f"{key}: supported-profile requirement must be applicable")
        elif not row["tests"]:
            errors.append(f"{key}: required tests are missing")
    return errors


def parse_go_test(output, returncode, package, required_tests):
    """Require actual run/pass lifecycles; package success alone proves nothing."""
    errors = []
    states = {}
    package_state = None
    if returncode != 0:
        errors.append(f"go test exit code {returncode}")
    for number, line in enumerate(output.splitlines(), 1):
        if not line.strip():
            continue
        try:
            value = decode_json(line)
            require(isinstance(value, dict), "event must be an object")
            action = value.get("Action")
            require(action in ("start", "run", "pause", "cont", "output", "pass", "fail", "skip"),
                    f"unknown/failing action: {action}")
            require(value.get("Package") == package, "unexpected/missing package")
            test = value.get("Test")
            require(test is None or (isinstance(test, str) and bool(test)), "invalid Test field")
            if action in ("fail", "skip"):
                errors.append(f"{test or package}: {action}")
            if action == "output":
                require(isinstance(value.get("Output"), str), "output event lacks Output text")
                continue
            if test is None:
                if action == "start":
                    require(package_state is None, "duplicate/out-of-order package start")
                    package_state = "started"
                else:
                    require(action in ("pass", "fail", "skip") and package_state == "started",
                            "invalid package terminal event")
                    require(not any(state in ("run", "pause") for state in states.values()),
                            "package ended with unfinished tests")
                    package_state = action
            else:
                require(package_state == "started", "test outside active package")
                previous = states.get(test)
                allowed = {"run": (None,), "pause": ("run",), "cont": ("pause",),
                           "pass": ("run",), "fail": ("run", "pause"), "skip": ("run",)}
                require(action in allowed and previous in allowed[action],
                        f"{test}: invalid {action} after {previous}")
                states[test] = "run" if action == "cont" else action
        except GateError as error:
            errors.append(f"Go JSON line {number}: {error}")
    passed = sorted(test for test, state in states.items() if state == "pass")
    if package_state != "pass":
        errors.append("package did not complete with pass")
    missing = set(required_tests) - set(passed)
    if missing:
        errors.append("required tests unexecuted or not passing: " + ", ".join(sorted(missing)))
    return {"status": "fail" if errors else "pass", "errors": errors,
            "passed_tests": passed, "observed_tests": sorted(states)}


def evaluate(data, root, execute=False, claim=None):
    validate_manifest(data)
    root = Path(root).resolve()
    claim = claim or data["claim"]
    require(claim in CLAIMS, "invalid claim override")
    report = {
        "authority": "LOCAL/MANUAL", "claim": claim, "certification": False,
        "conformance_established": False,
        "scope": ("supported-profile bounded regression only" if claim == "supported-profile"
                  else "full-2010 evidence prerequisites only; not certification"),
        "mode": "run" if execute else "check", "verdict": "FAIL",
        "profile": data["profile"], "host": {"os": sys.platform, "python": sys.version},
        "root": str(root),
        "created_at_utc": datetime.now(timezone.utc).isoformat(),
        "manifest_sha256": hashlib.sha256(json.dumps(data, sort_keys=True).encode()).hexdigest(),
        "limitations": ["Normative inventory completeness, oracle correctness, reviewer independence and source identity require human verification.",
                        "Only the current host and listed Go tests execute; no other binding/platform is inferred."],
        "commands_started": 0, "errors": policy_errors(data, root, claim), "tests": [],
    }
    try:
        module_text = (root / "go.mod").read_text(encoding="utf-8-sig")
        module = re.search(r"(?m)^module\s+([^\s\"`]+)\s*$", module_text)
        require(module is not None, "go.mod: unsupported/missing module declaration")
        module = module.group(1)
    except (OSError, UnicodeError, GateError) as error:
        report["errors"].append(str(error))
        module = ""
    for test in data["tests"]:
        command = ["go", "test", "-json", "-count=1", "-timeout=120s", "-run",
                   "^(" + "|".join(test["required_tests"]) + ")$", test["package"]]
        report["tests"].append({"id": test["id"], "command": command,
                                "status": "not_run", "passed_tests": [], "observed_tests": []})
        try:
            require(local_path(root, test["package"]).is_dir(),
                    f"{test['id']}: local package directory is missing")
        except (GateError, OSError) as error:
            report["errors"].append(str(error))
    if report["errors"]:
        return report
    if not execute:
        report["verdict"] = "CHECK_ONLY"
        return report
    env = dict(os.environ, GOFLAGS="", GOWORK="off", GOTOOLCHAIN="local")
    for test, result in zip(data["tests"], report["tests"]):
        try:
            completed = subprocess.run(result["command"], cwd=root, shell=False, env=env,
                                       capture_output=True, text=True, encoding="utf-8",
                                       errors="strict", timeout=180, check=False)
            report["commands_started"] += 1
            result.update(stdout=completed.stdout, stderr=completed.stderr, returncode=completed.returncode)
            result.update(parse_go_test(completed.stdout, completed.returncode,
                                        module + test["package"][1:], test["required_tests"]))
        except subprocess.TimeoutExpired as error:
            report["commands_started"] += 1
            result.update(status="fail", errors=["go test exceeded 180s process timeout"],
                          stdout=partial_text(error.stdout), stderr=partial_text(error.stderr))
        except UnicodeError as error:
            report["commands_started"] += 1
            result.update(status="fail", errors=[f"go test output could not be decoded: {error}"])
        except OSError as error:
            result.update(status="fail", errors=[f"go test could not complete: {error}"])
        report["errors"].extend(f"{test['id']}: {error}" for error in result["errors"])
    report["verdict"] = "FAIL" if report["errors"] else "PASS"
    return report


def partial_text(value):
    return value.decode("utf-8", errors="replace") if isinstance(value, bytes) else value or ""


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--manifest", type=Path, help="schema-v1 JSON; default is root/" + DEFAULT_MANIFEST)
    parser.add_argument("--root", type=Path, default=ROOT, help="trusted repository root")
    parser.add_argument("--claim", choices=CLAIMS, help="override manifest claim, e.g. test full-2010 readiness")
    mode = parser.add_mutually_exclusive_group()
    mode.add_argument("--run", action="store_true", help="execute all required allowlisted Go test commands")
    mode.add_argument("--check", "--dry-run", action="store_true", help="validate only (default); never gate PASS; exit 2")
    args = parser.parse_args(argv)
    try:
        data = load_manifest(args.manifest or args.root / DEFAULT_MANIFEST)
        result = evaluate(data, args.root, execute=args.run, claim=args.claim)
    except (GateError, OSError) as error:
        result = {"authority": "LOCAL/MANUAL", "verdict": "FAIL", "certification": False,
                  "conformance_established": False,
                  "commands_started": 0, "tests": [], "errors": [str(error)]}
    print(json.dumps(result, indent=2, ensure_ascii=True))
    return {"PASS": 0, "FAIL": 1, "CHECK_ONLY": 2}[result["verdict"]]


if __name__ == "__main__":
    sys.exit(main())
