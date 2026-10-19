#!/usr/bin/env python3
"""Audit source locators, links and trusted execution records, not conformance.

This tool never runs tests or changes the existing quality gate. The obligation
inventory is deliberately incomplete. Imported JUnit records are trusted local
records, not authenticated proof of execution or organizational independence.
"""

from __future__ import annotations

import argparse
import ast
import hashlib
import importlib.util
import json
import re
import sys
import xml.etree.ElementTree as ET
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
DEFAULT = "engineering/verification/ieee1516-2010-normative-obligations.json"
STANDARDS = {"IEEE 1516-2010", "IEEE 1516.1-2010", "IEEE 1516.2-2010"}


class TraceError(ValueError):
    pass


def require(condition, message):
    if not condition:
        raise TraceError(message)


def shape(value, keys, label):
    require(isinstance(value, dict) and set(value) == set(keys.split()),
            f"{label}: expected exactly {keys}")


def string(value, label):
    require(isinstance(value, str) and bool(value.strip()), f"{label}: expected nonempty string")


def strings(value, label, unique=True):
    require(isinstance(value, list), f"{label}: expected array")
    for item in value:
        string(item, label)
    require(not unique or len(set(value)) == len(value), f"{label}: duplicate entry")


def indexed(rows, label):
    require(isinstance(rows, list), f"{label}: expected array")
    result = {}
    for row in rows:
        require(isinstance(row, dict), f"{label}: expected object")
        string(row.get("id"), label + ".id")
        require(row["id"] not in result, f"{label}: duplicate id {row['id']}")
        result[row["id"]] = row
    return result


def local(root, relative):
    string(relative, "local path")
    require("\\" not in relative and ":" not in relative, "use relative POSIX repository paths")
    path = Path(relative)
    require(not path.is_absolute() and ".." not in path.parts, f"path escapes repository: {relative}")
    resolved = (root / path).resolve()
    require(resolved.is_relative_to(root.resolve()), f"path escapes repository: {relative}")
    return resolved


def digest(path):
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def unique_object(pairs):
    result = {}
    for key, value in pairs:
        require(key not in result, f"duplicate JSON key: {key}")
        result[key] = value
    return result


def reject_constant(value):
    raise TraceError(f"nonfinite JSON constant: {value}")


def decode_json(payload):
    return json.loads(payload, object_pairs_hook=unique_object, parse_constant=reject_constant)


def validate_inventory(data, root):
    shape(data, "schema_version coverage_complete sources requirements", "inventory")
    require(type(data["schema_version"]) is int and data["schema_version"] == 1, "unsupported schema")
    require(data["coverage_complete"] is False, "seed inventory cannot declare complete coverage")
    sources = indexed(data["sources"], "sources")
    require(bool(sources), "sources cannot be empty")
    for source in sources.values():
        shape(source, "id standard filename sha256", "source")
        require(source["standard"] in STANDARDS, "unknown standard")
        string(source["filename"], "source.filename")
        require(Path(source["filename"]).name == source["filename"] and ":" not in source["filename"]
                and "\\" not in source["filename"], "source filename must be a basename")
        require(isinstance(source["sha256"], str) and re.fullmatch(r"[0-9a-f]{64}", source["sha256"]),
                "source.sha256: invalid SHA-256")
    rows = indexed(data["requirements"], "requirements")
    require(bool(rows), "requirements cannot be empty")
    for row in rows.values():
        shape(row, "id source_id clause pdf_pages subject applicability statement conditions review_status implementation tests open_items", "requirement")
        require(row["source_id"] in sources, "unknown normative source")
        for key in ("clause", "statement", "conditions"):
            string(row[key], "requirement." + key)
        pages = row["pdf_pages"]
        require(isinstance(pages, list) and bool(pages) and all(type(p) is int and p > 0 for p in pages),
                "pdf_pages: expected positive one-based integer locators")
        require(len(pages) == len(set(pages)), "duplicate page locator")
        require(row["subject"] in {"rti", "federate", "federation", "omt-tool"}, "invalid responsibility")
        require(row["applicability"] in {"applicable", "not-applicable", "unresolved"}, "invalid applicability")
        require(row["review_status"] in {"source-reviewed", "candidate"}, "invalid review status")
        strings(row["implementation"], "implementation")
        strings(row["open_items"], "open_items")
        for path in row["implementation"]:
            local(root, path)
        require(isinstance(row["tests"], list), "tests: expected array")
        pairs = set()
        for test in row["tests"]:
            shape(test, "path name", "test")
            local(root, test["path"])
            string(test["name"], "test.name")
            require(re.fullmatch(r"[A-Za-z_][A-Za-z0-9_]*", test["name"]), "literal test name required")
            pair = (test["path"], test["name"])
            require(pair not in pairs, "duplicate test mapping")
            pairs.add(pair)
    return data


def declaration_text(payload):
    # Bounded lexical links, not type checking. Preserve newlines while removing
    # C/Go/Java comments and quoted literals so they cannot invent declarations.
    tokens = r'//[^\n]*|/\*[\s\S]*?\*/|"(?:\\.|[^"\\])*"|\'(?:\\.|[^\'\\])*\'|`[^`]*`'
    return re.sub(tokens, lambda match: "\n" * match.group(0).count("\n"), payload)


def python_test_names(payload):
    module = ast.parse(payload)
    functions = [node for node in module.body if isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef))]
    for node in module.body:
        if isinstance(node, ast.ClassDef) and node.name.startswith("Test"):
            functions.extend(child for child in node.body
                             if isinstance(child, (ast.FunctionDef, ast.AsyncFunctionDef)))
    return {node.name for node in functions if node.name.startswith("test_")}


def test_declared(path, name):
    payload = path.read_text(encoding="utf-8-sig")
    if path.suffix == ".py":
        return name in python_test_names(payload)
    if path.suffix == ".go":
        return bool(re.search(r"(?m)^func\s+" + re.escape(name) + r"\s*\(", declaration_text(payload)))
    if path.suffix == ".java":
        return bool(re.search(r"@Test\s+(?:public\s+)?void\s+" + re.escape(name) + r"\s*\(", declaration_text(payload)))
    raise TraceError(f"unsupported test declaration language: {path.suffix}")


def verify_campaign(data, root, inventory):
    shape(data, "schema_version independence author binary test_source junit command returncode requirement_tests", "campaign")
    require(type(data["schema_version"]) is int and data["schema_version"] == 1, "campaign schema")
    require(data["independence"] == "internal-black-box", "only internal black-box independence is assessed")
    string(data["author"], "campaign.author")
    require(type(data["returncode"]) is int and data["returncode"] == 0, "campaign did not exit successfully")
    strings(data["command"], "campaign.command", unique=False)
    require(bool(data["command"]), "empty campaign command")
    for key in ("binary", "test_source", "junit"):
        shape(data[key], "path sha256", "campaign." + key)
        record = data[key]
        string(record["path"], key + ".path")
        require(isinstance(record["sha256"], str) and re.fullmatch(r"[0-9a-f]{64}", record["sha256"]),
                f"{key}: invalid SHA-256")
        path = local(root, record["path"]) if key == "test_source" else Path(record["path"])
        require(path.is_file(), f"{key}: missing artifact {path}")
        require(digest(path) == record["sha256"], f"{key}: SHA-256 mismatch")
    command = data["command"]
    executable = Path(command[0]).name.lower()
    if re.fullmatch(r"python(?:\d+(?:\.\d+)?)?(?:\.exe)?", executable):
        require("-m" in command, "expected python -m pytest")
        module_index = command.index("-m")
        require(all(arg in {"-B", "-E", "-I", "-O", "-OO", "-s", "-S", "-u"}
                    for arg in command[1:module_index]), "unsupported Python execution mode")
        require(module_index + 1 < len(command) and command[module_index + 1] == "pytest",
                "recorded Python module is not pytest")
        arguments = command[module_index + 2:]
    else:
        require(executable in {"pytest", "pytest.exe"}, "recorded executable is not Python/pytest")
        arguments = command[1:]
    source_selectors = []
    for arg in arguments:
        selection = arg.replace("\\", "/").split("::", 1)
        selected_path = Path(selection[0])
        if selection[0] == data["test_source"]["path"] or (
                selected_path.is_absolute() and selected_path.resolve() == local(root, data["test_source"]["path"])):
            source_selectors.append(selection)
    require(bool(source_selectors),
            "command does not select recorded test source")
    path = Path(data["junit"]["path"])
    reports = [arg.split("=", 1)[1] for arg in arguments if arg.startswith("--junitxml=")]
    for index, arg in enumerate(arguments):
        if arg == "--junitxml" and index + 1 < len(arguments):
            reports.append(arguments[index + 1])
    require(len(reports) == 1 and Path(reports[0]).resolve() == path.resolve(),
            "recorded command does not produce this JUnit artifact")
    require(path.stat().st_size <= 16 * 1024 * 1024, "JUnit artifact too large")
    payload = path.read_bytes().decode("utf-8-sig")
    require("<!DOCTYPE" not in payload.upper() and "<!ENTITY" not in payload.upper(), "JUnit DTD prohibited")
    tree = ET.fromstring(payload)
    require(tree.tag in {"testsuites", "testsuite"}, "unsupported JUnit root")
    suites = [tree] if tree.tag == "testsuite" else list(tree)
    require(bool(suites) and all(s.tag == "testsuite" for s in suites), "empty or nested JUnit campaign")
    passed = set()
    source = Path(data["test_source"]["path"])
    module_names = {source.with_suffix("").as_posix().replace("/", "."),
                    source.with_suffix("").as_posix().split("/", 1)[-1].replace("/", ".")}
    for suite in suites:
        require(all(child.tag in {"testcase", "properties", "system-out", "system-err"} for child in suite),
                "nested or unknown JUnit suite content")
        cases = suite.findall("testcase")
        require(int(suite.get("tests", "-1")) == len(cases), "JUnit test count mismatch")
        require(all(int(suite.get(key, "-1")) == 0 for key in ("failures", "errors", "skipped")),
                "JUnit failure, error or skip")
        for case in cases:
            name = case.get("name")
            string(name, "JUnit test name")
            require(case.get("classname") in module_names, "JUnit case belongs to another test module")
            if case.get("file") is not None:
                file = case.get("file").replace("\\", "/")
                require(file in {source.as_posix(), source.as_posix().split("/", 1)[-1]},
                        "JUnit case belongs to another test file")
            require(name not in passed, "duplicate JUnit test name")
            require(not any(case.find(key) is not None for key in ("failure", "error", "skipped")),
                    "JUnit testcase failure, error or skip")
            passed.add(name)
    require(bool(passed), "no executed test cases")
    require(not any(arg == "-k" or arg.startswith("-k=") or arg == "-m" or arg.startswith("-m=")
                    for arg in arguments), "filtered pytest selection requires a separately reviewed profile")
    if all(len(selection) == 2 for selection in source_selectors):
        nodes = {selection[1] for selection in source_selectors}
        require(all("::" not in node for node in nodes), "class-qualified node selection unsupported")
        require(all(name in nodes or name.split("[", 1)[0] in nodes for name in passed),
                "JUnit case was not selected by the recorded command")
        require(all(any(name == node or name.split("[", 1)[0] == node for name in passed)
                    for node in nodes), "selected pytest node is missing from JUnit")
    else:
        declared_names = python_test_names(local(root, data["test_source"]["path"]).read_text(encoding="utf-8-sig"))
        require({name.split("[", 1)[0] for name in passed} == declared_names,
                "whole-file JUnit is missing a declared test or contains an unknown test")
    if tree.tag == "testsuites":
        for key, expected in (("tests", len(passed)), ("failures", 0), ("errors", 0), ("skipped", 0)):
            if tree.get(key) is not None:
                require(int(tree.get(key)) == expected, "JUnit root aggregate mismatch")
    requirements = {row["id"]: row for row in inventory["requirements"]}
    require(isinstance(data["requirement_tests"], list) and bool(data["requirement_tests"]), "empty campaign mapping")
    observed = {}
    used = set()
    for mapping in data["requirement_tests"]:
        shape(mapping, "requirement_id tests", "campaign mapping")
        key = mapping["requirement_id"]
        require(key in requirements and key not in observed, "unknown or duplicate campaign requirement")
        strings(mapping["tests"], "campaign mapping tests")
        require(bool(mapping["tests"]) and set(mapping["tests"]) <= passed, "required test absent from executed JUnit")
        declared = {test["name"] for test in requirements[key]["tests"]
                    if test["path"] == data["test_source"]["path"]}
        require(all(name.split("[", 1)[0] in declared for name in mapping["tests"]), "campaign has no matching traceability test link")
        require(all(test_declared(local(root, data["test_source"]["path"]), name.split("[", 1)[0])
                    for name in mapping["tests"]), "campaign source does not declare selected collectible test")
        observed[key] = mapping["tests"]
        used.update(mapping["tests"])
    require(used == passed, "executed test has no requirement mapping")
    return {"independence": data["independence"], "author": data["author"],
            "binary": data["binary"], "junit": data["junit"], "test_source": data["test_source"],
            "executed_tests": sorted(passed), "requirement_tests": observed,
            "production_source_attested": False, "organizationally_independent": False}


def audit_inventory(data, root, source_dir=None, campaign=None):
    validate_inventory(data, root)
    errors, sources, rows = [], [], []
    for source in data["sources"]:
        state = "not-checked"
        if source_dir is not None:
            try:
                require(digest(Path(source_dir) / source["filename"]) == source["sha256"], "normative source SHA-256 mismatch")
                state = "hash-verified"
            except (OSError, TraceError) as error:
                errors.append(f"{source['id']}: {error}")
                state = "failed"
        sources.append({**source, "verification": state})
    for row in data["requirements"]:
        problems = []
        for relative in row["implementation"]:
            if not local(root, relative).is_file():
                problems.append("missing implementation: " + relative)
        for test in row["tests"]:
            try:
                require(test_declared(local(root, test["path"]), test["name"]), "missing test declaration")
            except (OSError, ValueError, SyntaxError) as error:
                problems.append(f"{test['path']}::{test['name']}: {error}")
        errors.extend(f"{row['id']}: {problem}" for problem in problems)
        state = "open"
        if problems:
            state = "broken-links"
        elif row["review_status"] == "source-reviewed" and not row["open_items"]:
            if row["applicability"] == "not-applicable":
                state = "not-applicable-reviewed"
            elif row["applicability"] == "applicable" and row["implementation"] and row["tests"]:
                state = "source-reviewed-mapped"
        link_status = ("broken" if problems else "complete" if row["implementation"] and row["tests"]
                       else "missing-tests" if not row["tests"] else "missing-implementation")
        rows.append({**row, "trace_status": state, "link_status": link_status, "independent_executed_tests": []})
    verified = None
    if campaign is not None:
        try:
            verified = verify_campaign(campaign, root, data)
            for row in rows:
                row["independent_executed_tests"] = verified["requirement_tests"].get(row["id"], [])
        except (OSError, ValueError, TypeError, ET.ParseError) as error:
            errors.append("campaign: " + str(error))
    return {"schema_version": 1, "scope": "incomplete obligation seed and bounded internal black-box records",
            "verdict": "FAIL" if errors else "RECORDS_VALID",
            "coverage_complete": False, "conformance_established": False, "certification": False,
            "sources": sources, "requirements": rows, "campaign": verified, "errors": errors,
            "counts": {"requirements": len(rows), "mapped": sum(r["trace_status"] == "source-reviewed-mapped" for r in rows),
                       "linked_requirements": sum(r["link_status"] == "complete" for r in rows),
                       "not_applicable": sum(r["trace_status"] == "not-applicable-reviewed" for r in rows),
                       "open": sum(r["trace_status"] in {"open", "broken-links"} for r in rows),
                       "independent_executed": sum(bool(r["independent_executed_tests"]) for r in rows)}}


def audit_regression_links(data, root):
    """Check declarations in the historical ledger, without trusting its status."""
    groups = indexed(data["tests"], "regression test groups")
    requirements = indexed(data["requirements"], "regression requirements")
    errors, checked, rows, cache = [], {}, [], {}
    for key, group in groups.items():
        problems, links = [], []
        try:
            package = group["package"]
            require(isinstance(package, str) and package.startswith("./"), "exact local package required")
            directory = local(root, package[2:])
            require(directory.is_dir(), "missing test package")
            if directory not in cache:
                declarations = {}
                for path in sorted(directory.glob("*_test.go")):
                    for name in re.findall(r"(?m)^func\s+(Test[A-Za-z0-9_]+)\s*\(", declaration_text(path.read_text(encoding="utf-8-sig"))):
                        declarations.setdefault(name, []).append(path.relative_to(root).as_posix())
                cache[directory] = declarations
            strings(group["required_tests"], "required test names")
            for name in group["required_tests"]:
                paths = cache[directory].get(name, [])
                if not paths:
                    problems.append("missing declared test: " + name)
                links.append({"name": name, "paths": paths})
        except (OSError, ValueError, KeyError) as error:
            problems.append(str(error))
        checked[key] = {"id": key, "package": group.get("package"), "tests": links, "errors": problems}
        errors.extend(f"{key}: {error}" for error in problems)
    for key, row in requirements.items():
        references = row["tests"]
        missing = set(references) - groups.keys()
        if missing:
            errors.append(f"{key}: missing test groups {sorted(missing)}")
        rows.append({"id": key, "kind": "engineering" if key.startswith("ENG-") else "bounded-regression",
                     "clause": row.get("clause"), "tests": references,
                     "recorded_status": row.get("status"), "execution_verified": False})
    return {"scope": "lexical declaration links only, not a normative obligation census",
            "tests_executed": False, "requirements": rows, "test_groups": list(checked.values()),
            "errors": errors, "counts": {"requirements": len(rows), "groups": len(groups),
                                         "test_references": sum(len(g["required_tests"]) for g in groups.values())}}


def quality_gate():
    spec = importlib.util.spec_from_file_location("ieee_gate", ROOT / "scripts/ieee2010_quality_gate.py")
    gate = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(gate)
    return gate


def verify_go_campaign(data, record, root):
    require(isinstance(record, dict), "Go campaign must be an object")
    require(record.get("mode") == "run" and record.get("claim") == "supported-profile",
            "Go campaign must be an executed bounded gate, not a metadata check")
    require(record.get("verdict") == "PASS" and record.get("errors") == [], "Go gate did not pass")
    expected = hashlib.sha256(json.dumps(data, sort_keys=True).encode()).hexdigest()
    require(record.get("manifest_sha256") == expected, "Go campaign manifest SHA-256 mismatch")
    require(record.get("profile") == data["profile"], "Go campaign profile mismatch")
    require(isinstance(record.get("root"), str) and Path(record["root"]).resolve() == root.resolve(),
            "Go campaign repository mismatch")
    groups = indexed(data["tests"], "Go test groups")
    results = indexed(record.get("tests"), "executed Go groups")
    require(groups.keys() == results.keys(), "Go campaign group coverage mismatch")
    require(type(record.get("commands_started")) is int and record["commands_started"] == len(groups),
            "Go campaign command count mismatch")
    module = re.search(r"(?m)^module\s+([^\s\"`]+)\s*$", (root / "go.mod").read_text(encoding="utf-8-sig"))
    require(module is not None, "missing module declaration")
    gate, passed = quality_gate(), {}
    for key, group in groups.items():
        result = results[key]
        command = ["go", "test", "-json", "-count=1", "-timeout=120s", "-run",
                   "^(" + "|".join(group["required_tests"]) + ")$", group["package"]]
        require(result.get("command") == command, f"{key}: executed command mismatch")
        require(type(result.get("returncode")) is int and result["returncode"] == 0,
                f"{key}: nonzero/missing exit code")
        require(result.get("status") == "pass" and isinstance(result.get("stdout"), str),
                f"{key}: no executed raw transcript")
        parsed = gate.parse_go_test(result["stdout"], result["returncode"],
                                    module.group(1) + group["package"][1:], group["required_tests"])
        require(parsed["status"] == "pass", f"{key}: raw events rejected: {parsed['errors']}")
        passed[key] = parsed["passed_tests"]
    return {"groups_executed": len(passed), "passed_tests_by_group": passed,
            "manifest_sha256": expected, "source_id": data["profile"]["source_id"],
            "production_source_attested": False, "independence": "existing internal regression"}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--inventory", default=str(ROOT / DEFAULT))
    parser.add_argument("--source-dir", help="private directory containing the supplied PDFs; checks hashes only")
    parser.add_argument("--campaign", help="explicit trusted local campaign JSON; imports records, never runs tests")
    parser.add_argument("--regression-manifest", default=str(ROOT / "engineering/verification/ieee1516-2010-evidence.json"))
    parser.add_argument("--go-campaign", help="bounded gate JSON with raw Go events; imports records only")
    args = parser.parse_args()
    try:
        data = decode_json(Path(args.inventory).read_text(encoding="utf-8-sig"))
        campaign = decode_json(Path(args.campaign).read_text(encoding="utf-8-sig")) if args.campaign else None
        result = audit_inventory(data, ROOT, args.source_dir, campaign)
        regression = quality_gate().load_manifest(args.regression_manifest)
        result["regression_inventory"] = audit_regression_links(regression, ROOT)
        if args.go_campaign:
            record = decode_json(Path(args.go_campaign).read_text(encoding="utf-8-sig"))
            verified = verify_go_campaign(regression, record, ROOT)
            result["regression_inventory"]["campaign"] = verified
            for row in result["regression_inventory"]["requirements"]:
                row["execution_verified"] = bool(row["tests"]) and all(
                    group in verified["passed_tests_by_group"] for group in row["tests"])
        result["errors"].extend(result["regression_inventory"]["errors"])
        if result["errors"]:
            result["verdict"] = "FAIL"
    except (OSError, ValueError, TypeError) as error:
        result = {"verdict": "FAIL", "conformance_established": False, "errors": [str(error)]}
    print(json.dumps(result, ensure_ascii=True, indent=2))
    return 1 if result["verdict"] == "FAIL" else 0


if __name__ == "__main__":
    sys.exit(main())
