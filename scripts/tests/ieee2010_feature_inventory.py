"""Local feature inventory checks, separate from the existing evidence gate."""

import argparse
import hashlib
import json
import re
from collections import Counter
from pathlib import Path


STATES = {"fixed-tested", "reproducedgap", "unverified", "normativeunknown", "justifiedNA"}
FACETS = {"inputs", "exceptions", "lifecycle", "post_state", "callbacks", "failure_concurrency", "persistence", "binding_encoding"}
AUDITS = {f"{prefix}{number:02d}" for prefix, count in [("C", 10), ("N", 12), ("F", 13), ("Q", 10)] for number in range(1, count + 1)}
PROFILES = {"go-windows", "go-linux", "python-windows", "python-linux", "cpp-linux", "cpp-windows", "python-tls", "cpp-tsan", "cpp-official-abi", "java-wsdl"}


def digest(value):
    return hashlib.sha256(json.dumps(value, sort_keys=True, separators=(",", ":")).encode()).hexdigest()


def load_json(path):
    def unique(pairs):
        result = {}
        for key, value in pairs:
            if key in result:
                raise ValueError(f"duplicate JSON key: {key}")
            result[key] = value
        return result
    return json.loads(Path(path).read_text(encoding="utf-8-sig"), object_pairs_hook=unique)


def file_hash(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def safe_source_path(root, path):
    candidate = (Path(root) / path).resolve()
    if Path(path).is_absolute() or not candidate.is_relative_to(Path(root).resolve()):
        raise ValueError("source path escapes selected root")
    return candidate


def _evaluate(data, root=None, official_root=None, require_completion=False):
    """Return a non-executing inventory assessment; never conformance evidence."""
    errors = []
    if not isinstance(data, dict):
        return {"verdict": "FAIL", "errors": ["inventory must be an object"], "engineering_complete": False,
                "conformance_established": False, "certification": False, "commands_started": 0}
    if type(data.get("schema_version")) is not int or data["schema_version"] != 1:
        errors.append("schema_version must be 1")
    if data.get("claim") != "inventory-only":
        errors.append("only inventory-only is supported; use the release gate for broader claims")
    if data.get("claims") != {"engineering_complete": False, "conformance_established": False, "certification": False}:
        errors.append("inventory must not assert engineering completion, conformance or certification")
    if not data.get("source_snapshot"):
        errors.append("source snapshot identity required")

    def index(entries, name):
        result = {}
        if not isinstance(entries, list):
            errors.append(f"{name} must be a list")
            return result
        for entry in entries:
            if not isinstance(entry, dict) or not isinstance(entry.get("id"), str) or not entry["id"]:
                errors.append(f"{name}: nonempty string id required")
                continue
            if entry["id"] in result:
                errors.append(f"{name}: duplicate id {entry['id']}")
            result[entry["id"]] = entry
        return result

    rows = index(data.get("rows"), "rows")
    sources = index(data.get("sources"), "sources")
    evidence = index(data.get("evidence"), "evidence")
    if not rows:
        errors.append("inventory has no rows")
    ids = sorted(rows)
    expected = data.get("expected_ids")
    if not isinstance(expected, list) or expected != ids:
        errors.append("rows do not match the sorted complete expected_ids catalog")
    if data.get("surface_digest") != digest(ids):
        errors.append("surface catalog digest mismatch")
    for name in sorted((AUDITS | PROFILES) - rows.keys()):
        errors.append(f"required reconciliation/profile row missing: {name}")

    checked_sources = 0
    for source in sources.values():
        if source.get("root") not in {"repository", "official"} or not source.get("path") or not re.fullmatch(r"[a-f0-9]{64}", source.get("sha256", "")):
            errors.append(f"{source['id']}: invalid source identity")
            continue
        selected = root if source["root"] == "repository" else official_root
        if selected is not None:
            try:
                path = safe_source_path(selected, source["path"])
                if file_hash(path) != source["sha256"]:
                    errors.append(f"{source['id']}: source digest drift: {source['path']}")
                checked_sources += 1
            except (OSError, ValueError) as exc:
                errors.append(f"{source['id']}: source unavailable: {exc}")

    traces = data.get("traces", {})
    if not isinstance(traces, dict):
        traces = {}
        errors.append("traces must be an object")
    for name, trace in traces.items():
        if not isinstance(trace, dict) or not trace.get("implementation_paths") or not trace.get("oracle") or trace.get("mapping_status") not in STATES:
            errors.append(f"{name}: implementation paths, oracle and mapping disposition required")

    def executed_record(item, row):
        prefix = f"{row['id']}: evidence {item['id']}"
        if item.get("kind") != "executed-behavior" or item.get("result") != "pass" or item.get("snapshot") != data["source_snapshot"]:
            errors.append(f"{prefix}: not passing executed evidence for this snapshot")
            return
        if not item.get("executed_cases") or not set(row["required_tests"]).issubset(item.get("executed_cases", [])) or not set(row["profiles"]).issubset(item.get("profiles", [])):
            errors.append(f"{prefix}: required cases/profiles not executed")
        try:
            path = Path(item["path"])
            if not path.is_absolute():
                if root is None:
                    raise ValueError("root required to verify execution artifact")
                path = safe_source_path(root, item["path"])
            if file_hash(path) != item["sha256"]:
                raise ValueError("execution artifact digest mismatch")
            artifact = load_json(path)
            if artifact.get("snapshot") != data["source_snapshot"]:
                raise ValueError("execution artifact snapshot mismatch")
            observed = {(c["id"], c["profile"]) for c in artifact.get("cases", []) if c.get("executed") is True and c.get("result") == "pass"}
            required = {(case, profile) for case in row["required_tests"] for profile in row["profiles"]}
            if not required.issubset(observed):
                raise ValueError("execution artifact lacks required passing case/profile observations")
            if any(c.get("result") != "pass" or c.get("executed") is not True for c in artifact.get("cases", [])):
                raise ValueError("execution artifact contains skipped, failed or unexecuted cases")
        except (OSError, ValueError, KeyError, TypeError) as exc:
            errors.append(f"{prefix}: {exc}")

    def inspect_record(row, kind, result):
        matches = [evidence[e] for e in row["evidence"] if e in evidence and evidence[e].get("kind") == kind]
        if not matches:
            errors.append(f"{row['id']}: {kind} evidence required")
        for item in matches:
            try:
                path = Path(item["path"])
                if not path.is_absolute():
                    if root is None:
                        raise ValueError("root required to verify inspection artifact")
                    path = safe_source_path(root, item["path"])
                if item.get("snapshot") != data["source_snapshot"] or item.get("result") != result or file_hash(path) != item["sha256"]:
                    raise ValueError("inspection identity/verdict/digest mismatch")
                artifact = load_json(path)
                if artifact.get("snapshot") != data["source_snapshot"] or artifact.get("kind") != kind or artifact.get("result") != result or row["id"] not in artifact.get("row_ids", []):
                    raise ValueError("inspection artifact does not support row disposition")
                if kind == "applicability-review" and (not artifact.get("independent") or artifact.get("reviewer") != row.get("reviewer") or not artifact.get("justification")):
                    raise ValueError("independent applicability review required")
                if kind == "behavioral-reproduction" and (not artifact.get("executed_case") or not artifact.get("expected") or not artifact.get("actual") or artifact["expected"] == artifact["actual"]):
                    raise ValueError("reproduction requires an executed case and differing expected/actual behavior")
            except (OSError, ValueError, KeyError, TypeError) as exc:
                errors.append(f"{row['id']}: {exc}")

    counts = Counter()
    for row in rows.values():
        name, state = row["id"], row.get("status")
        counts[str(state)] += 1
        if state not in STATES:
            errors.append(f"{name}: untriaged status")
        if row.get("applicability") not in {"product-required", "unresolved", "not-applicable"} or not row.get("rationale"):
            errors.append(f"{name}: explicit applicability and rationale required")
        if row.get("trace") not in traces:
            errors.append(f"{name}: unresolved trace reference")
        anchors = row.get("anchors", [])
        if not anchors or any(not isinstance(a, dict) or a.get("source") not in sources or not a.get("locator") for a in anchors):
            errors.append(f"{name}: valid source anchors required")
        profiles = row.get("profiles", [])
        if not profiles or not set(profiles).issubset(PROFILES):
            errors.append(f"{name}: explicit recognized profiles required")
        facets = row.get("facets", {})
        if not isinstance(facets, dict) or set(facets) != FACETS or any(s not in STATES for s in facets.values()):
            errors.append(f"{name}: all state semantics facets must be triaged")
        if not isinstance(row.get("required_tests"), list) or not row["required_tests"]:
            errors.append(f"{name}: required test/oracle identifiers required")
        if not isinstance(row.get("blocking_dependencies"), list) or not isinstance(row.get("evidence"), list):
            errors.append(f"{name}: explicit blocking_dependencies and evidence lists required")
            continue
        if any(e not in evidence for e in row["evidence"]):
            errors.append(f"{name}: unknown evidence reference")
        if state in {"unverified", "normativeunknown", "reproducedgap"} and not row["blocking_dependencies"]:
            errors.append(f"{name}: open item has no blocking dependency")
        if state == "reproducedgap" and (not row.get("reproducer") or not row["evidence"]):
            errors.append(f"{name}: behavioral reproducer and evidence required, source suspicion is not reproduction")
        if state == "reproducedgap":
            inspect_record(row, "behavioral-reproduction", "fail")
        if state == "justifiedNA" and (row.get("applicability") != "not-applicable" or not row.get("reviewer") or not row.get("na_justification") or not row["evidence"]):
            errors.append(f"{name}: NA requires applicability argument and review evidence")
        if state == "justifiedNA":
            inspect_record(row, "applicability-review", "justifiedNA")
        if name.startswith("Q") and name in AUDITS and state != "normativeunknown":
            errors.append(f"{name}: normative closure unsupported without an authorized reviewed inventory")
        if state == "fixed-tested":
            if not row.get("reviewer") or row["blocking_dependencies"] or not row["evidence"] or any(s not in {"fixed-tested", "justifiedNA"} for s in facets.values()):
                errors.append(f"{name}: completion requires reviewed evidence, closed facets and no blockers")
            matching = [evidence[e] for e in row["evidence"] if e in evidence and evidence[e].get("kind") == "executed-behavior"]
            if not matching:
                errors.append(f"{name}: historical reports or source inspection cannot close a current row")
            for item in matching:
                executed_record(item, row)
    if require_completion and any(r.get("status") not in {"fixed-tested", "justifiedNA"} for r in rows.values()):
        errors.append("engineering completion blocked by open inventory rows")
    return {"verdict": "FAIL" if errors else "PASS", "errors": errors,
            "mode": "completion-prerequisites" if require_completion else "inventory-triage",
            "row_count": len(rows), "status_counts": dict(sorted(counts.items())), "source_files_verified": checked_sources,
            "source_verification": "selected roots only" if root or official_root else "not requested; structure only",
            "engineering_complete": False, "conformance_established": False,
            "certification": False, "commands_started": 0}


def evaluate(data, root=None, official_root=None, require_completion=False):
    """Malformed ledger data fails closed for CLI and imported callers alike."""
    try:
        return _evaluate(data, root, official_root, require_completion)
    except (AttributeError, KeyError, TypeError, ValueError) as exc:
        return {"verdict": "FAIL", "errors": [f"invalid inventory structure: {exc}"],
                "engineering_complete": False, "conformance_established": False,
                "certification": False, "commands_started": 0}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--inventory", type=Path, default=Path(__file__).resolve().parents[2] / "engineering/verification/ieee1516-2010-feature-inventory.json")
    parser.add_argument("--root", type=Path)
    parser.add_argument("--official-root", type=Path)
    parser.add_argument("--require-completion", action="store_true")
    args = parser.parse_args()
    try:
        result = evaluate(load_json(args.inventory), args.root, args.official_root, args.require_completion)
    except (OSError, ValueError, TypeError, KeyError) as exc:
        result = {"verdict": "FAIL", "errors": [str(exc)], "engineering_complete": False,
                  "conformance_established": False, "certification": False, "commands_started": 0}
    print(json.dumps(result, indent=2))
    return int(result["verdict"] != "PASS")


if __name__ == "__main__":
    raise SystemExit(main())
