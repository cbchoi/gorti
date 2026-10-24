#!/usr/bin/env python3
"""Reproduce classified evidence tables from portable, versioned inputs."""

import argparse
import csv
import hashlib
import json
from collections import Counter, defaultdict
from pathlib import Path

PASSED_LINK_STATUS = "passing-records-only-completeness-unassessed"
CATEGORIES = ("service-behavior", "api-structure", "documentation-reporting")


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def oracle_digest(oracles):
    wire = json.dumps(sorted(set(oracles)), ensure_ascii=True, separators=(",", ":"))
    return hashlib.sha256(wire.encode()).hexdigest()


def key(row):
    return row["path"], row["name"]


def classification_index(data, classification):
    targets, hashes = defaultdict(list), {}
    for requirement in data["requirements"]:
        for test in requirement["tests"]:
            targets[key(test)].append(test["oracle"])
        for test in requirement["test_evidence"]:
            previous = hashes.setdefault(key(test), test["source_sha256_at_review"])
            if previous != test["source_sha256_at_review"]:
                raise ValueError("Conflicting test-source hashes")
    index = {}
    for row in classification["tests"]:
        target = key(row)
        if target in index:
            raise ValueError(f"Duplicate classification: {target}")
        if row["category"] not in CATEGORIES or not row.get("rationale"):
            raise ValueError(f"Invalid classification: {target}")
        index[target] = row
    if index.keys() != targets.keys():
        raise ValueError("Classification must cover exactly the named test targets")
    for target, oracles in targets.items():
        row = index[target]
        if row["source_sha256"] != hashes[target]:
            raise ValueError(f"Stale classification source: {target}")
        if row["oracles_sha256"] != oracle_digest(oracles):
            raise ValueError(f"Stale classification oracle: {target}")
    return index


def requirement_status(requirement):
    category = requirement["applicability"]
    tests = requirement["test_evidence"]
    if category != "applicable":
        return category
    if not tests or any(t["execution_status"] == "not-found" for t in tests):
        return "missing-results"
    statuses = {i["status"] for t in tests for i in t["invocations"]}
    if "fail" in statuses:
        return "with-failure"
    if "skip" in statuses:
        return "with-skip"
    if all(t["execution_status"] == PASSED_LINK_STATUS for t in tests):
        return "all-linked-invocations-passed"
    raise ValueError(f"Unclassified execution evidence: {requirement['id']}")


def summarize(data, classification):
    index = classification_index(data, classification)
    groups = defaultdict(Counter)
    links = defaultdict(Counter)
    profiles = defaultdict(Counter)
    applicable_targets = defaultdict(set)
    exceptions = defaultdict(list)
    rows, requirement_rows = [], []
    for requirement in data["requirements"]:
        status = requirement_status(requirement)
        groups[requirement["group"]][status] += 1
        classes = sorted({index[key(t)]["category"] for t in requirement["tests"]})
        profile = "+".join(classes) or "no-linked-test"
        if requirement["applicability"] == "applicable":
            profiles[profile][status] += 1
            if status != "all-linked-invocations-passed":
                exceptions[status].append(requirement["id"])
        requirement_rows.append({
            "requirement_id": requirement["id"], "group": requirement["group"],
            "applicability": requirement["applicability"], "clause": requirement["clause"],
            "execution_status": status, "evidence_profile": profile,
            "open_items": " | ".join(requirement.get("open_items", [])),
        })
        oracles = {key(t): t["oracle"] for t in requirement["tests"]}
        for test in requirement["test_evidence"]:
            category = index[key(test)]["category"]
            links[category]["all_links"] += 1
            links[category][test["execution_status"]] += 1
            if requirement["applicability"] == "applicable":
                links[category]["applicable_links"] += 1
                applicable_targets[category].add(key(test))
            for invocation in test["invocations"] or [{}]:
                rows.append({
                    "requirement_id": requirement["id"], "group": requirement["group"],
                    "applicability": requirement["applicability"], "clause": requirement["clause"],
                    "requirement_evidence_status": status,
                    "evidence_category": category, "requirement_evidence_profile": profile,
                    "classification_rationale": index[key(test)]["rationale"],
                    "test_path": test["path"], "test_name": test["name"],
                    "oracle": oracles[key(test)],
                    "test_source_sha256": test["source_sha256_at_review"],
                    "invocation": invocation.get("identity", ""),
                    "status": invocation.get("status", "not-found"),
                    "artifact": invocation.get("artifact", ""),
                    "artifact_sha256": invocation.get("artifact_sha256", ""),
                })
    unique = Counter(row["category"] for row in index.values())
    summary = {
        "source_commit": data["source_commit"],
        "groups": {k: dict(v) for k, v in sorted(groups.items())},
        "test_links": dict(Counter(t["execution_status"] for r in data["requirements"]
                                   for t in r["test_evidence"])),
        "evidence_categories": {c: dict(links[c], distinct_targets=unique[c],
                                      applicable_distinct_targets=len(applicable_targets[c]))
                                for c in CATEGORIES},
        "applicable_requirement_profiles": {k: dict(v) for k, v in sorted(profiles.items())},
        "missing_ids": sorted(exceptions["missing-results"]),
        "skipped_ids": sorted(exceptions["with-skip"]),
        "failed_ids": sorted(exceptions["with-failure"]),
        "historical_source_comparison": data.get("historical_source_comparison"),
        "interpretation": (
            "Execution linkage and primary evidence type, not requirement satisfaction "
            "or an IEEE certificate. Service behavior includes controlled-dependency "
            "unit and SDK tests; it is not synonymous with live end-to-end coverage."),
        "execution_artifacts": data["execution_artifacts"],
    }
    return summary, rows, requirement_rows


def verify_files(data, source_root=None, artifact_root=None):
    checks = {"source_files_checked": 0, "execution_artifacts_checked": 0}
    if source_root:
        sources = {t["path"]: t["source_sha256_at_review"] for r in data["requirements"]
                   for t in r["test_evidence"]}
        for name, expected in sources.items():
            path = source_root / name
            if not path.is_file() or digest(path) != expected:
                raise ValueError(f"Test-source mismatch: {name}")
        checks["source_files_checked"] = len(sources)
    if artifact_root:
        available = defaultdict(list)
        for path in artifact_root.rglob("*"):
            if path.is_file():
                available[path.name].append(path)
        for artifact in data["execution_artifacts"]:
            matches = [p for p in available[artifact["filename"]]
                       if digest(p) == artifact["sha256"]]
            if len(matches) != 1:
                raise ValueError(f"Missing or ambiguous execution artifact: {artifact['filename']}")
        checks["execution_artifacts_checked"] = len(data["execution_artifacts"])
    return checks


def write_csv(path, rows):
    with path.open("w", encoding="utf-8", newline="") as stream:
        writer = csv.DictWriter(stream, fieldnames=list(rows[0]))
        writer.writeheader()
        writer.writerows(rows)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--reconciled", required=True, type=Path)
    parser.add_argument("--classes", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--source-root", type=Path)
    parser.add_argument("--artifact-root", type=Path)
    args = parser.parse_args()
    data = json.loads(args.reconciled.read_text(encoding="utf-8"))
    classification = json.loads(args.classes.read_text(encoding="utf-8"))
    summary, rows, requirements = summarize(data, classification)
    summary.update(reconciliation_sha256=digest(args.reconciled),
                   classification_sha256=digest(args.classes))
    checks = verify_files(data, args.source_root, args.artifact_root)
    args.output.mkdir(parents=True, exist_ok=True)
    write_csv(args.output / "requirement-invocations.csv", rows)
    write_csv(args.output / "requirement-status.csv", requirements)
    (args.output / "requirements-summary.json").write_text(
        json.dumps(summary, indent=2) + "\n", encoding="utf-8", newline="\n")
    # Optional local checks are not embedded in the deterministic scientific tables.
    (args.output / "reproduction-checks.json").write_text(
        json.dumps(checks, indent=2) + "\n", encoding="utf-8", newline="\n")
    print(json.dumps({k: v for k, v in summary.items() if k != "execution_artifacts"}, indent=2))


if __name__ == "__main__":
    main()
