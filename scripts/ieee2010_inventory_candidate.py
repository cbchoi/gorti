"""Versioned interim discovery and fail-closed reconciliation, never release proof.

Reuses the baseline's structured extractors without rewriting its ledger. The
check command re-discovers source; an internally consistent edited catalog is
not its own completeness oracle. No historical disposition is promoted.
"""

import argparse
import collections
import copy
import hashlib
import importlib.util
import json
from pathlib import Path, PurePosixPath, PureWindowsPath
import re
import sys
import zipfile


HERE = Path(__file__).resolve().parent
BASELINE_PATH = "engineering/verification/ieee1516-2010-feature-inventory.json"
BASELINE_SHA256 = "fb780ef397f5874ce22d3897333aa6cf0d9331b7c937768144c8edb570345635"
PLAN = "engineering/design/ieee1516-2010-feature-completion-plan.md"
CHECKPOINT = "engineering/verification/ieee1516-2010-feature-completion-20260920.md"
OPEN_STATES = {"unverified", "normativeunknown"}


def load_module(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


gate = load_module("candidate_inventory_gate", HERE / "tests/ieee2010_feature_inventory.py")

# These are project work orders and proposed engineering oracles, not clauses.
WORK = {
    "R0": ("P0", "verification/integration", ["normative", "quality"],
           "Authorized applicability and baseline reconciliation",
           "Review authorized texts/corrections by locator and responsible party; review every disposition against the candidate source and oracle.",
           ["authorized-texts-and-corrections", "independent-applicability-review"]),
    "R1": ("P1", "model", ["fom"], "FOM metadata and FDD retention",
           "Independent schema plus semantic vectors through parse/merge/join/snapshot/restore/export, including rejected mutations.",
           ["per-field-merge-default-review", "independent-xsd-semantic-vectors"]),
    "R2": ("P1", "model/integration", ["fom", "federation", "binding", "wire"],
           "Explicit MIM role and provenance",
           "Omitted/default/alternate/invalid MIM scenarios across SDK, server, MOM and restore; reject incompatible peers atomically.",
           ["R0-alternate-mim-applicability", "R1", "shared-wire-contract-review"]),
    "R3": ("P1", "delivery", ["mom", "time", "object"], "RO and TSO accounting",
           "Independent admitted=completed+canceled/invalidated+outstanding conservation across handoff, reconnect, retraction and restore without duplicate counting.",
           ["reviewed-measurement-boundary", "cross-stage-conservation-tests"]),
    "R4": ("P1", "mom/integration", ["mom", "federation", "save"],
           "MOM catalog, attribution and lifecycle",
           "Pinned MIM catalog with stateful requested/periodic/query/reflection, legitimate issuer, post-resign and restore publication oracles; unavailable is not satisfied.",
           ["R0-mom-applicability", "R3", "production-composition-lifecycle-tests"]),
    "R5": ("P1", "bindings", ["binding"], "Strict C++ 2010 profile",
           "Unmodified official-header clients compile/link/execute exact ownership types, overloads, callbacks and exceptions on every claimed compiler/ABI.",
           ["strict-modern-profile-separation", "per-platform-official-header-runtime"]),
    "R6": ("P1", "integration/bindings", ["time", "save", "binding", "wire"],
           "Typed exact logical time",
           "Independent signed integer64 vectors above 2^53 through services, queues, arithmetic, callbacks and restore; reject mixed kinds before mutation; retain float64 tests.",
           ["R0-time-factory-applicability", "shared-time-wire-contract", "overflow-boundary-oracles"]),
    "R7": ("P1", "integration/python", ["binding", "federation", "support", "wire"],
           "Go and Python advertised parity",
           "Explicit operation/input/error/callback matrix, including standard facade forwarding of type, MIM and time designator plus old call forms.",
           ["R2", "R6", "independent-forwarding-and-callback-tests"]),
    "R8": ("P0-reproduced-defects", "semantics/integration", ["federation", "save", "object", "ownership", "ddm", "declaration", "time", "support", "mom"],
           "State machines, atomicity and partial orders",
           "Stateful positive/invalid/boundary/concurrent/failure rows detect dropped inputs, wrong recipients/errors, early grants and successful no-ops on production paths.",
           ["per-service-state-exception-matrix", "independent-oracle-review"]),
    "R9": ("P0-release", "verification/integration", ["quality", "binding"],
           "Frozen integrated evidence and release review",
           "One frozen candidate executes every explicit required test/profile without skips, missing cases or drift; separate race/sanitizer/fault/reference/performance gates and independent review.",
           ["R0-applicability-closure", "settled-source-freeze", "multi-host-campaigns", "independent-release-review"]),
}

WORK_PATHS = {
    "R0": [PLAN, "engineering/verification/ieee1516-2010-requirements-matrix.md", "scripts/ieee2010_inventory_candidate.py"],
    "R1": ["rti/pkg/fom/parser/strict.go", "rti/pkg/fom/exporter/exporter.go", "rti/internal/application/composition/fom_current_fdd.go"],
    "R2": ["cppsdk/src/dlc/RTIambassadorImpl.cpp", "proto/rti/v1/federation.proto", "rti/pkg/fom/mim/merge.go"],
    "R3": ["rti/internal/adapters/delivery/outbox.go", "rti/internal/application/server/mom_metrics_sources.go", "rti/internal/hla/time/metrics.go"],
    "R4": ["rti/internal/hla/mom/catalog.go", "rti/internal/adapters/grpc/mom_observer.go", "rti/internal/application/server/run.go"],
    "R5": ["cppsdk/CMakeLists.txt", "cppsdk/include/RTI/RTIambassador.h", "scripts/verify_official_cpp2010.py"],
    "R6": ["rti/internal/hla/core/time.go", "rti/internal/hla/time/manager.go", "cppsdk/src/dlc/LogicalTime.cpp"],
    "R7": ["pysdk/rti1516e/standard.py", "pysdk/rti1516e/connection.py", "rti/pkg/federate/federation_services.go"],
    "R8": ["rti/internal/adapters/grpc/server.go", "rti/internal/hla/save_restore/manager.go", "rti/internal/hla/ownership/manager.go"],
    "R9": ["scripts/ieee2010_quality_gate.py", "scripts/verify_python_hla.py", "scripts/verify_official_cpp2010.py"],
}


def relative_path(value):
    if not isinstance(value, str) or not value or "\\" in value:
        raise ValueError("expected canonical repository-relative POSIX path")
    path = PurePosixPath(value)
    if path.is_absolute() or PureWindowsPath(value).drive or ".." in path.parts or path.as_posix() != value:
        raise ValueError(f"unsafe or noncanonical source path: {value}")
    return value


def index(entries, label):
    result = {}
    for row in entries:
        if not isinstance(row, dict) or not isinstance(row.get("id"), str) or not row["id"]:
            raise ValueError(f"{label}: invalid row")
        if row["id"] in result:
            raise ValueError(f"{label}: duplicate id {row['id']}")
        result[row["id"]] = row
    return result


def contract(row):
    value = copy.deepcopy(row.get("contract"))
    if isinstance(value, dict):
        value.pop("line", None)
    return {"kind": row["kind"], "contract": value, "profiles": row.get("profiles", [])}


def reconcile(baseline, discovered):
    """Retain removed rows as unresolved obligations, not silent scope reduction."""
    previous = index(baseline["rows"], "baseline")
    current = index(discovered["rows"], "discovery")
    old_sources = index(baseline["sources"], "baseline sources")
    new_sources = index(discovered["sources"], "discovery sources")
    changes = []
    for name in sorted(previous.keys() | current.keys()):
        before, after = previous.get(name), current.get(name)
        change = "added" if before is None else "removed-unresolved" if after is None else "unchanged-contract"
        if before is not None and after is not None:
            if before.get("contract") != after.get("contract") and contract(before) != contract(after):
                change = "contract-changed"
            elif before.get("profiles") != after.get("profiles"):
                change = "profile-changed"
            elif before["kind"] != after["kind"]:
                change = "classification-changed"
        source_changes = []
        if before is not None:
            for anchor in before.get("anchors", []):
                source = anchor["source"]
                if old_sources.get(source) != new_sources.get(source):
                    source_changes.append(source)
        changes.append({"id": name, "change": change,
                        "baseline_row_sha256": gate.digest(before) if before else None,
                        "candidate_row_sha256": gate.digest(after) if after else None,
                        "baseline_status": before.get("status") if before else None,
                        "source_changes": sorted(set(source_changes)),
                        "disposition_transfer": "none; review and source-bound evidence required"})
    return changes


def verify_capture(manifest_path):
    manifest_digest = gate.file_hash(manifest_path)
    manifest = gate.load_json(manifest_path)
    files = manifest.get("files")
    if not isinstance(files, dict) or not files:
        raise ValueError("before capture requires a nonempty file manifest")
    for path, sha in files.items():
        relative_path(path)
        if not isinstance(sha, str) or not re.fullmatch(r"[a-f0-9]{64}", sha):
            raise ValueError(f"invalid captured digest: {path}")
    archive = manifest_path.with_name("source.zip")
    archive_digest = gate.file_hash(archive)
    with zipfile.ZipFile(archive) as zipped:
        names = [entry.filename for entry in zipped.infolist() if not entry.is_dir()]
        if len(set(names)) != len(names) or set(names) != set(files):
            raise ValueError("before archive members differ from manifest or contain duplicates")
        for name in names:
            if hashlib.sha256(zipped.read(name)).hexdigest() != files[name]:
                raise ValueError(f"before archive content mismatch: {name}")
    if gate.file_hash(manifest_path) != manifest_digest or gate.file_hash(archive) != archive_digest:
        raise ValueError("before capture changed during verification")
    return manifest, {"manifest_path": manifest_path.resolve().as_posix(),
                      "manifest_sha256": manifest_digest,
                      "source_zip_sha256": archive_digest, "files_verified": len(files),
                      "exclusions": manifest.get("exclusions", []),
                      "unavailable_paths": manifest.get("unavailable_paths", []),
                      "unexpected_missing": manifest.get("unexpected_missing", []),
                      "boundary": "Parent before capture verified; exclusions are not positive evidence."}


def capture_sources(root, official, before):
    # Include the parent's declared files and new files in bounded product roots.
    names = set(before["files"])
    for directory, patterns in {
        "rti": ["*.go", "go.mod", "go.sum"],
        "proto": ["*.proto"],
        "cppsdk/include": ["*.h", "*.hpp"],
        "cppsdk/src": ["*.cpp", "*.h", "*.hpp"],
        "cppsdk/tests": ["*.cpp", "*.h", "*.hpp", "CMakeLists.txt"],
        "pysdk/rti1516e": ["*.py"],
        "pysdk/tests": ["*.py"],
        "scripts": ["*.py", "*.go"],
    }.items():
        for pattern in patterns:
            names.update(p.relative_to(root).as_posix() for p in (root / directory).rglob(pattern))
    names.update([PLAN, CHECKPOINT, BASELINE_PATH])
    files, missing = {}, []
    for name in sorted(names):
        path = gate.safe_source_path(root, relative_path(name))
        if path.is_file():
            files[name] = gate.file_hash(path)
        else:
            missing.append(name)
    official_files = {p.relative_to(official).as_posix(): gate.file_hash(p) for p in sorted(official.rglob("*.h"))}
    value = {"repository": files, "official": official_files, "missing_before_paths": missing}
    return {"sha256": gate.digest(value), **value,
            "policy": "Parent manifest paths plus bounded Go/proto/C++/Python/test/tool discovery roots; not a full release freeze."}


def sanitize_discovery(data, baseline, scanner):
    previous = index(baseline["rows"], "baseline")
    for row in data["rows"]:
        normative = row["id"] in {f"Q{n:02d}" for n in range(1, 11)} or row["id"] == "java-wsdl"
        status = "normativeunknown" if normative else "unverified"
        row.update(status=status, applicability="unresolved" if normative else "product-required",
                   facets={key: status for key in sorted(gate.FACETS)}, evidence=[], reviewer=None,
                   blocking_dependencies=["authorized-text-review"] if normative else ["per-row-trace-and-oracle-review", "frozen-profile-execution"])
        row.pop("reproducer", None)
        row.pop("na_justification", None)
        row.pop("historical_disposition", None)
        if row["kind"] in {"implementation-gap", "review-gap", "audit", "profile"}:
            if row["kind"] in {"implementation-gap", "review-gap"}:
                row["kind"] = "historical-followup"
            row["rationale"] = ("Authorized normative applicability remains unresolved; no clause or satisfaction inferred." if normative else
                                "Historical follow-up retained for candidate review. The first-batch checkpoint and R0-R9 supersede old absence/assignment descriptions; no current defect or closure inferred.")
            row["anchors"].append({"source": scanner.source(CHECKPOINT), "locator": "Implemented Boundaries; Verification; remaining limits"})
            row["finding_basis"] = "historical-followup-not-a-current-reproduction"
        row["work_orders"] = sorted(name for name, work in WORK.items() if row["trace"] in work[2])
        row["owner"] = {"fom": "model", "binding": "bindings/integration", "mom": "mom/delivery/integration",
                        "normative": "verification/integration", "quality": "verification/integration"}.get(row["trace"], "integration/semantics")
        if row["id"] in previous:
            row["prior_disposition"] = {"row_sha256": gate.digest(previous[row["id"]]),
                                        "status": previous[row["id"]]["status"], "transferred": False}


def build(root, official, baseline_path, manifest_path):
    if gate.file_hash(baseline_path) != BASELINE_SHA256:
        raise ValueError("immutable 2285-row baseline digest mismatch")
    baseline = gate.load_json(baseline_path)
    if len(baseline["rows"]) != 2285 or gate.evaluate(baseline)["verdict"] != "PASS":
        raise ValueError("immutable baseline shape mismatch")
    before, before_ref = verify_capture(manifest_path)
    start = capture_sources(root, official, before)
    pinned_official = {s["path"]: s["sha256"] for s in baseline["sources"] if s["root"] == "official"}
    if start["official"] != pinned_official:
        raise ValueError("official supplementary headers differ from the immutable baseline pin")
    sources = load_module("candidate_source_extractors", HERE / "tests/ieee2010_inventory_sources.py")
    scanner = sources.Inventory(root, official)
    data = scanner.build()
    sanitize_discovery(data, baseline, scanner)
    for name, (priority, owner, groups, title, oracle, blockers) in WORK.items():
        status = "normativeunknown" if name == "R0" else "unverified"
        row = scanner.add("work:" + name, "project-work-order", PLAN,
                          f"Remaining Work Orders: {name}; project ID, not IEEE clause", groups[0], status,
                          title + "; candidate review backlog, not a reproduced current defect.",
                          owner=owner, priority=priority, work_orders=[name],
                          blocking_dependencies=blockers, required_tests=[f"planned:{name}:independent-oracle"])
        row["oracle"] = oracle
        row["implementation_paths"] = WORK_PATHS[name]
        for path in WORK_PATHS[name]:
            row["anchors"].append({"source": scanner.source(path), "locator": "whole file; engineering review candidate"})
    data["rows"] = sorted(scanner.rows, key=lambda row: row["id"])
    # Tool inputs and the checkpoint are source-bound too; raw logs are not.
    for path in [PLAN, CHECKPOINT, "scripts/ieee2010_inventory_candidate.py",
                 "scripts/tests/test_ieee2010_inventory_candidate.py",
                 "scripts/tests/ieee2010_inventory_sources.py", "scripts/tests/ieee2010_inventory_go.go",
                 "scripts/tests/ieee2010_feature_inventory.py"]:
        scanner.source(path)
    data["sources"] = sorted(scanner.sources.values(), key=lambda item: item["id"])
    end = capture_sources(root, official, before)
    if start != end:
        raise ValueError("source drift during discovery; retry after writers settle")
    for source in data["sources"]:
        category = "repository" if source["root"] == "repository" else "official"
        if end[category].get(source["path"]) != source["sha256"]:
            raise ValueError(f"source changed while scanning: {source['path']}")
    data["source_snapshot"] = "interim-discovery-sha256:" + end["sha256"]
    data["expected_ids"] = [row["id"] for row in data["rows"]]
    data["surface_digest"] = gate.digest(data["expected_ids"])
    for key in ["baseline", "current_batch_reported", "historical_test_groups", "as_of"]:
        data.pop(key, None)
    data["evidence"] = []
    data["scope"] = "Versioned R0-R9 candidate discovery and historical follow-up, not runtime, conformance, release or certification evidence."
    data["limitations"] = [
        "No authorized normative fulltexts/corrections were added. All prior dispositions require reviewed source-bound evidence; none are transferred.",
        "Source hashes describe an interim scan of a concurrently developed checkout, not a frozen release or runtime campaign.",
        "Legacy gap IDs are historical follow-ups; implemented first-batch host/FDD/history/elapsed features are not declared absent.",
        "C++ extraction is bounded, support headers need member/ABI audit; dynamic/inherited Python APIs and Go build profiles need independent review.",
        "Generated descriptor parsing does not independently regenerate proto parity. Component traces and name links do not prove semantic coverage.",
        "Missing discovery rows are retained in reconciliation as removed-unresolved, not waived; unavailable platforms/inputs remain blockers.",
    ]
    source_diff = {"added": sorted(end["repository"].keys() - before["files"].keys()),
                   "removed": sorted(before["files"].keys() - end["repository"].keys()),
                   "changed": sorted(p for p in before["files"].keys() & end["repository"].keys()
                                     if before["files"][p] != end["repository"][p])}
    return {"schema_version": 1, "kind": "interim-candidate-discovery",
            "baseline": {"path": BASELINE_PATH, "sha256": BASELINE_SHA256, "row_count": 2285,
                         "source_snapshot": baseline["source_snapshot"]},
            "before_capture": before_ref, "source_capture": end, "source_delta_from_before": source_diff,
            "inventory": data, "reconciliation": reconcile(baseline, data)}


def assess(candidate, rediscovered, require_completion=False):
    """Only accepts exact fresh discovery; status promotions need another dossier."""
    errors = []
    try:
        if candidate.get("schema_version") != 1 or type(candidate.get("schema_version")) is not int:
            errors.append("candidate schema_version must be 1")
        if candidate.get("kind") != "interim-candidate-discovery":
            errors.append("only interim-candidate-discovery is supported")
        result = gate.evaluate(candidate["inventory"])
        errors.extend(result["errors"])
        for row in candidate["inventory"]["rows"]:
            if row["status"] not in OPEN_STATES or any(state not in OPEN_STATES for state in row["facets"].values()):
                errors.append(f"{row['id']}: candidate discovery cannot carry closure dispositions")
            if row.get("evidence") or row.get("reviewer"):
                errors.append(f"{row['id']}: discovery does not accept evidence/review promotions")
        if rediscovered is None:
            errors.append("fresh source rediscovery required")
        elif candidate != rediscovered:
            for key in sorted(candidate.keys() | rediscovered.keys()):
                if candidate.get(key) != rediscovered.get(key):
                    errors.append(f"fresh discovery mismatch: {key}")
        if require_completion:
            errors.append("interim discovery never establishes completion; reviewed frozen-source dossier required")
    except (AttributeError, KeyError, TypeError, ValueError) as exc:
        errors.append(f"invalid candidate: {exc}")
    return {"verdict": "FAIL" if errors else "PASS", "errors": errors,
            "mode": "candidate-rediscovery", "engineering_complete": False,
            "conformance_established": False, "certification": False,
            "runtime_tests_executed": 0}


def write_new(path, data):
    # Exclusive creation also rejects existing symlink aliases of the baseline.
    with path.open("x", encoding="utf-8", newline="\n") as stream:
        json.dump(data, stream, indent=2, sort_keys=True)
        stream.write("\n")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=["discover", "check"])
    parser.add_argument("--root", type=Path, required=True)
    parser.add_argument("--official-root", type=Path, required=True)
    parser.add_argument("--before-manifest", type=Path, required=True)
    parser.add_argument("--candidate", type=Path, required=True)
    parser.add_argument("--require-completion", action="store_true")
    args = parser.parse_args()
    try:
        if args.command == "discover" and args.candidate.exists():
            raise ValueError("candidate already exists; use a new versioned path")
        expected = build(args.root.resolve(), args.official_root.resolve(),
                         args.root / BASELINE_PATH, args.before_manifest)
        candidate = expected if args.command == "discover" else gate.load_json(args.candidate)
        result = assess(candidate, expected, args.require_completion)
        if result["verdict"] == "PASS" and args.command == "discover":
            write_new(args.candidate, candidate)
        result.update(candidate=args.candidate.as_posix(),
                      row_count=len(candidate["inventory"]["rows"]),
                      source_identity=candidate["source_capture"]["sha256"],
                      reconciliation_counts=dict(collections.Counter(r["change"] for r in candidate["reconciliation"])),
                      kind_counts=dict(collections.Counter(r["kind"] for r in candidate["inventory"]["rows"])),
                      status_counts=dict(collections.Counter(r["status"] for r in candidate["inventory"]["rows"])))
    except (OSError, ValueError, KeyError, TypeError, RuntimeError, zipfile.BadZipFile) as exc:
        result = {"verdict": "FAIL", "errors": [str(exc)], "engineering_complete": False,
                  "conformance_established": False, "certification": False}
    print(json.dumps(result, indent=2))
    return int(result["verdict"] != "PASS")


if __name__ == "__main__":
    sys.exit(main())
