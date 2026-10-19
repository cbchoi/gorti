#!/usr/bin/env python3
"""Reconcile redline-source review units, requirement links and local results.

The census is a candidate index, not an exhaustive normative interpretation.
No test pass or extraction result closes a requirement automatically. Raw PDF
text and execution records remain outside the repository.
"""

from __future__ import annotations

import argparse
from collections import Counter, defaultdict
import json
from pathlib import Path
import re
import xml.etree.ElementTree as ET

from ieee2010_traceability import (
    ROOT, TraceError, decode_json, declaration_text, digest, local, require,
    test_declared,
)


# One-based PDF page ranges, checked against the supplied redline editions.
# Normative/informative classification does not make every sentence obligatory.
BODY = {"1516": (15, 35), "1516.1": (17, 390), "1516.2": (15, 93)}
ANNEX = {
    "1516": [("A", 36, 38, False), ("B", 39, 41, False)],
    "1516.1": [("A", 391, 438, False), ("B", 439, 502, True),
               ("C", 503, 565, True), ("D", 566, 566, True),
               ("E", 567, 581, False), ("F", 582, 582, True),
               ("G", 582, 583, True), ("H", 583, 584, False),
               ("I", 585, 588, False)],
    "1516.2": [("A", 94, 94, False), ("B", 95, 96, True),
               ("C", 97, 101, True), ("D", 102, 112, True),
               ("E", 113, 113, True), ("F", 114, 126, False),
               ("G", 127, 161, False), ("H", 162, 164, False)],
}
HEADING = re.compile(r"^(?P<clause>(?:\d+\.[\d.]*\d|\d+\.|[A-I](?:\.\d+)+))\s+\.?\s*(?P<title>[A-Za-z].*)$")
CAPTION = re.compile(r"^(?P<kind>Table|Figure)\s+(?P<number>\d+)\s*[\u2014\u2013-]\s*(?P<title>.+)$")


def heading(line):
    match = HEADING.fullmatch(line.strip())
    if not match or re.search(r"\.{3,}", line):
        return None
    title = match["title"].strip()
    if re.search(r"\s(?:Yes|No)$", title) or re.search(r"\s{2,}\d{1,3}$", line):
        return None  # TOC page numbers and service-usage table entries.
    return match["clause"].rstrip("."), title


def census_source(source, pages):
    sid = source["id"]
    first, last = BODY[sid]
    records = []
    for page in pages:
        number = page["pdf_page"]
        if not first <= number <= last and not any(
                start <= number <= end and normative
                for _, start, end, normative in ANNEX[sid]):
            continue
        for line_number, line in enumerate(page["active_candidate"].splitlines(), start=1):
            found = heading(line)
            if found:
                clause, title = found
                if clause[0].isalpha() and not any(
                        label == clause[0] and start <= number <= end and normative
                        for label, start, end, normative in ANNEX[sid]):
                    continue
                records.append({"id": "S" + sid + ":" + clause,
                                "source_id": "S" + sid, "kind": "clause",
                                "clause": clause, "title": title,
                                "start_page": number, "start_line": line_number})
            caption = CAPTION.fullmatch(line.strip())
            if caption:
                records.append({"id": "S" + sid + ":" + caption["kind"] + caption["number"],
                                "source_id": "S" + sid, "kind": caption["kind"].lower(),
                                "clause": caption["kind"] + " " + caption["number"],
                                "title": caption["title"], "start_page": number, "start_line": line_number})
    grouped = defaultdict(list)
    for record in records:
        grouped[record["id"]].append(record)
    result = []
    clauses = [r for r in records if r["kind"] == "clause"]
    for occurrences in grouped.values():
        record = dict(occurrences[0])
        record["occurrence_pages"] = sorted({r["start_page"] for r in occurrences})
        record["candidate_only"] = True
        record["ambiguous"] = len(occurrences) != 1
        end = record["start_page"]
        end_line = None
        if record["kind"] == "clause" and not record["ambiguous"]:
            depth = len(record["clause"].split("."))
            following = [r for r in clauses
                         if (r["start_page"], r["start_line"]) > (record["start_page"], record["start_line"])
                         and len(r["clause"].split(".")) <= depth]
            boundary = last if record["clause"][0].isdigit() else next(
                stop for label, _, stop, normative in ANNEX[sid]
                if label == record["clause"][0] and normative)
            next_heading = min(following, key=lambda r: (r["start_page"], r["start_line"])) if following else None
            end = min(next_heading["start_page"], boundary) if next_heading else boundary
            if next_heading and next_heading["start_page"] <= boundary:
                end_line = next_heading["start_line"]
        record["end_page_inclusive_candidate"] = max(end, record["start_page"])
        record["end_before_line_on_end_page_candidate"] = end_line
        result.append(record)
    for label, start, end, normative in ANNEX[sid]:
        if normative:
            result.append({"id": "S" + sid + ":Annex" + label,
                           "source_id": "S" + sid, "kind": "annex",
                           "clause": "Annex " + label, "title": "Normative annex " + label,
                           "start_page": start, "end_page_inclusive_candidate": end,
                           "occurrence_pages": [start], "candidate_only": True,
                           "ambiguous": False})
    return result


def declared(path, name):
    if path.suffix != ".java":
        return test_declared(path, name)
    text = declaration_text(path.read_text(encoding="utf-8-sig"))
    pattern = (r"@(?:\w+\.)*(?:Test|ParameterizedTest|RepeatedTest|TestFactory|TestTemplate)\b"
               r"(?:\s*\([^)]*\))?\s*(?:@[\w.]+(?:\([^)]*\))?\s*)*"
               r"(?:(?:public|protected|final)\s+)*(?:void|Stream<[^>]+>|Collection<[^>]+>)\s+"
               + re.escape(name) + r"\s*\(")
    return bool(re.search(pattern, text, re.S))


def parse_locators(source_id, clause):
    """Keep the original bullet/range locator; index its containing clauses."""
    result = []
    unresolved_ranges = []
    for part in clause.split(";"):
        for match in re.finditer(r"(?<![\w.])(?:\d+(?:\.\d+)*|[A-I](?:\.\d+)*)(?![\w.])", part):
            value = match[0]
            before = part[:match.start()].strip()
            caption = re.search(r"\b(Table|Figure)\s*$", before)
            if caption:
                value = caption[1] + value
            elif re.fullmatch(r"[A-I]", value):
                value = "Annex" + value
            result.append(source_id + ":" + value)
    for match in re.finditer(r"(\d+(?:\.\d+)+)\s*[-\u2013\u2014]\s*(\d+(?:\.\d+)+)", clause):
        start, end = match[1].split("."), match[2].split(".")
        lower, upper = int(start[-1]), int(end[-1])
        if start[:-1] == end[:-1] and lower <= upper and upper - lower <= 1000:
            result.extend(source_id + ":" + ".".join(start[:-1] + [str(number)])
                          for number in range(lower, upper + 1))
        else:
            unresolved_ranges.append(match[0])
    return sorted(set(result)), unresolved_ranges


def locator_ids(source_id, clause):
    return parse_locators(source_id, clause)[0]


def validate_decompositions(reviewed, units, requirements):
    ids = {unit["id"] for unit in units}
    require(reviewed <= ids, "reviewed units absent from candidate census: " + str(sorted(reviewed - ids)))
    mapped = {locator for row in requirements if row["review_status"] == "source-reviewed"
              for locator in locator_ids(row["source_id"], row["clause"])}
    require(reviewed <= mapped, "reviewed units without source-reviewed decomposition: " + str(sorted(reviewed - mapped)))


def validate_slice(data, root, page_counts):
    require(type(data.get("schema_version")) is int and data["schema_version"] == 1, "unsupported slice schema")
    require(data.get("coverage_complete") is False, "review slice cannot claim complete coverage")
    require(data.get("source_id") in page_counts, "unknown slice source")
    require(isinstance(data.get("reviewed_units"), list), "reviewed_units must be an array")
    require(isinstance(data.get("requirements"), list), "requirements must be an array")
    for row in data["requirements"]:
        require(row.get("source_id") in page_counts, "unknown requirement source")
        require(row["source_id"] == data["source_id"], "requirement source differs from review slice")
        for key in ("id", "clause", "statement", "conditions"):
            require(isinstance(row.get(key), str) and row[key].strip(), "missing " + key)
        require(row.get("subject") in {"rti", "federate", "federation", "omt-tool"}, "invalid subject")
        require(row.get("applicability") in {"applicable", "not-applicable", "unresolved"}, "invalid applicability")
        require(row.get("review_status") in {"source-reviewed", "candidate"}, "invalid review status")
        pages = row.get("pdf_pages")
        require(isinstance(pages, list) and pages and all(
            type(p) is int and 1 <= p <= page_counts[row["source_id"]] for p in pages), "invalid page locator")
        require(len(pages) == len(set(pages)), "duplicate page locator")
        require(isinstance(row.get("implementation"), list), "implementation must be an array")
        require(isinstance(row.get("tests"), list), "tests must be an array")
        require(isinstance(row.get("open_items"), list) and all(
            isinstance(item, str) and item.strip() for item in row["open_items"]), "invalid open_items")
        for relative in row["implementation"]:
            require(local(root, relative).is_file(), "missing implementation " + relative)
        pairs = set()
        for test in row["tests"]:
            require(isinstance(test.get("oracle"), str) and test["oracle"].strip(), "missing test oracle")
            require(isinstance(test.get("name"), str) and re.fullmatch(r"[A-Za-z_]\w*", test["name"]), "literal test name required")
            path = local(root, test["path"])
            require(path.is_file() and declared(path, test["name"]), "test not declared: " + str(test))
            pair = (test["path"], test["name"])
            require(pair not in pairs, "duplicate test link")
            pairs.add(pair)
        if row["applicability"] == "not-applicable":
            require(row["review_status"] == "source-reviewed", "exclusion requires source review")
    return data


def junit_records(path):
    records = []
    for case in ET.parse(path).getroot().iter("testcase"):
        status = "pass"
        if case.find("skipped") is not None:
            status = "skip"
        if case.find("failure") is not None or case.find("error") is not None:
            status = "fail"
        records.append((case.get("classname", ""), case.get("name", ""), status))
    return records


def execution_index(go_path=None, python_path=None, java_dir=None, supplemental_junit=(), supplemental_go=()):
    index = ExecutionIndex()
    artifacts = []
    go_paths = [("go-json", go_path)] if go_path else []
    go_paths.extend(("supplemental-go-json", path) for path in supplemental_go)
    for kind, path in go_paths:
        source_hash = digest(path)
        artifacts.append({"kind": kind, "filename": path.name, "sha256": source_hash})
        for line in path.read_text(encoding="utf-8-sig").splitlines():
            event = decode_json(line)
            if event.get("Test") and event.get("Action") in {"pass", "fail", "skip"}:
                name = event["Test"]
                if "/" not in name:
                    key = (event["Package"], name)
                    index[key].append(event["Action"])
                    index.invocations[key].append({"artifact": path.name, "artifact_sha256": source_hash,
                                                   "identity": event["Package"] + "::" + name, "status": event["Action"]})
    xml = []
    if python_path:
        xml.append(("python-junit", python_path))
    if java_dir:
        java_files = sorted(java_dir.glob("TEST-*.xml"))
        require(bool(java_files), "no Java JUnit records")
        xml.extend(("java-junit", path) for path in java_files)
    xml.extend(("supplemental-junit", path) for path in supplemental_junit)
    for kind, path in xml:
        source_hash = digest(path)
        artifacts.append({"kind": kind, "filename": path.name, "sha256": source_hash})
        for classname, name, status in junit_records(path):
            # Parameterized cases retain distinct terminal statuses. One skipped
            # or failed variant prevents crediting the whole literal test.
            base = re.split(r"\[|\(", name, maxsplit=1)[0]
            key = (classname, base)
            index[key].append(status)
            index.invocations[key].append({"artifact": path.name, "artifact_sha256": source_hash,
                                           "identity": classname + "::" + name, "status": status})
    return index, artifacts


class ExecutionIndex(defaultdict):
    def __init__(self):
        super().__init__(list)
        self.invocations = defaultdict(list)


def execution_keys(test, index, root):
    path = local(root, test["path"])
    if path.suffix == ".go":
        module = re.search(r"(?m)^module\s+(\S+)", (root / "go.mod").read_text())[1]
        key = (module + "/" + path.parent.relative_to(root).as_posix(), test["name"])
        return [key]
    elif path.suffix == ".py":
        bases = {root}
        if path.is_relative_to(root / "pysdk"):
            bases.add(root / "pysdk")
        # A nested pytest.ini defines a separate collection root, as in IVCT.
        for parent in path.parents:
            if parent == root or not parent.is_relative_to(root):
                break
            if (parent / "pytest.ini").is_file():
                bases.add(parent)
                break
        modules = {path.relative_to(base).with_suffix("").as_posix().replace("/", ".")
                   for base in bases}
        return [(classname, name) for classname, name in index
                if name == test["name"] and any(
                    classname == module or classname.startswith(module + ".") for module in modules)]
    else:
        package = re.search(r"(?m)^package\s+([\w.]+)\s*;", path.read_text(encoding="utf-8-sig"))
        require(package is not None, "Java test without package")
        return [(package[1] + "." + path.stem, test["name"])]


def execution_status(test, index, root):
    states = [status for key in execution_keys(test, index, root) for status in index.get(key, [])]
    if not states:
        return "not-found"
    if any(status != "pass" for status in states):
        return "nonpassing-variant"
    return "passing-records-only-completeness-unassessed"


def applicable_linkage(requirements):
    applicable = [row for row in requirements if row["applicability"] == "applicable"]
    missing = sorted(row["id"] for row in applicable if not row["tests"])
    return {"scope": "reviewed-applicable-records-only", "applicable": len(applicable),
            "linked": len(applicable) - len(missing), "unlinked_ids": missing,
            "complete": bool(applicable) and not missing}


def build(args):
    source_root = args.sources.resolve()
    sources = decode_json((source_root / "sources.json").read_text(encoding="utf-8"))
    units, source_records, counts = [], [], {}
    for source in sources:
        path = args.pdf_dir / source["filename"]
        require(path.is_file() and digest(path) == source["sha256"], "PDF source hash mismatch")
        projection = source_root / (source["id"] + "-pages.jsonl")
        pages = [decode_json(line) for line in projection.read_text(encoding="utf-8").splitlines()]
        require([p["pdf_page"] for p in pages] == list(range(1, source["pages"] + 1)), "missing/duplicate projection pages")
        counts["S" + source["id"]] = source["pages"]
        units.extend(census_source(source, pages))
        source_records.append({"id": "S" + source["id"], "filename": source["filename"],
                               "sha256": source["sha256"], "pages": len(pages),
                               "projection_sha256": digest(projection), "candidate_only": True})
    requirements, reviewed, review_inputs, seen = [], set(), [], set()
    for path in sorted(args.slices.glob("*.json")):
        data = validate_slice(decode_json(path.read_text(encoding="utf-8")), ROOT, counts)
        review_inputs.append({"path": path.relative_to(ROOT).as_posix(), "sha256": digest(path)})
        for clause in data["reviewed_units"]:
            require(isinstance(clause, str), "review unit must be a clause string")
            reviewed.add(data["source_id"] + ":" + clause)
        for row in data["requirements"]:
            require(row["id"] not in seen, "duplicate requirement id: " + row["id"])
            seen.add(row["id"])
            requirements.append(row)
    linkage = applicable_linkage(requirements)
    if getattr(args, "require_applicable_oracles", False):
        require(linkage["complete"], "applicable oracle linkage incomplete: " +
                (", ".join(linkage["unlinked_ids"]) or "no applicable records"))
    ids = {unit["id"] for unit in units}
    validate_decompositions(reviewed, units, requirements)
    index, artifacts = execution_index(args.go_results, args.python_results, args.java_results,
                                       args.supplemental_junit, args.supplemental_go_results)
    for row in requirements:
        locators, ranges = parse_locators(row["source_id"], row["clause"])
        row["unexpanded_locator_ranges"] = ranges
        row["candidate_index_locator_ids"] = [locator for locator in locators if locator in ids]
        row["candidate_index_locator_found"] = bool(locators) and not ranges and all(locator in ids for locator in locators)
        row["test_evidence"] = [{"path": t["path"], "name": t["name"],
                                 "source_sha256_at_review": digest(local(ROOT, t["path"])),
                                 "execution_status": execution_status(t, index, ROOT),
                                 "invocation_completeness": "not-reconciled",
                                 "invocations": [record for key in execution_keys(t, index, ROOT)
                                                 for record in index.invocations.get(key, [])]} for t in row["tests"]]
        row["semantic_verdict"] = "not-applicable" if row["applicability"] == "not-applicable" else "open"
    for unit in units:
        unit["reviewed_decomposition_declared"] = unit["id"] in reviewed
        unit["review_scope"] = "direct-body-only; descendants and referenced contracts assessed separately"
        unit["linked_requirement_ids"] = [r["id"] for r in requirements
                                          if unit["id"] in r["candidate_index_locator_ids"]]
    result = {"schema_version": 1, "coverage_complete": False, "full_conformance": False,
              "applicable_oracle_linkage": linkage,
              "sources": source_records, "review_inputs": review_inputs,
              "execution_artifacts": artifacts, "review_units": units, "requirements": requirements,
              "limitations": ["Redline projection and heading recognition require visual review; missing headings are possible.",
                              "Clause review declarations are not independent semantic qualification.",
                              "Historical local results are not authenticated execution or proof of an unchanged source tree.",
                              "Tables, figures, API declarations and schema elements require further decomposition.",
                              "Corrigenda/errata reconciliation remains open."]}
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(result, indent=2, ensure_ascii=True) + "\n", encoding="utf-8")
    print(json.dumps({"pages": sum(counts.values()), "candidate_review_units": len(units),
                      "decomposition_declarations": len(reviewed), "requirements": len(requirements),
                      "test_links": sum(len(r["tests"]) for r in requirements),
                      "applicable_oracle_linkage": linkage,
                      "execution_statuses": dict(Counter(t["execution_status"] for r in requirements for t in r["test_evidence"])),
                      "open_items": sum(len(r["open_items"]) for r in requirements),
                      "full_conformance": False}, indent=2))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--sources", required=True, type=Path)
    parser.add_argument("--pdf-dir", required=True, type=Path)
    parser.add_argument("--slices", type=Path, default=ROOT / "engineering/verification/requirements2010")
    parser.add_argument("--go-results", type=Path)
    parser.add_argument("--python-results", type=Path)
    parser.add_argument("--java-results", type=Path)
    parser.add_argument("--supplemental-junit", action="append", default=[], type=Path,
                        help="Additional selected execution; failing/skipped variants remain uncredited")
    parser.add_argument("--supplemental-go-results", action="append", default=[], type=Path,
                        help="Additional Go JSON execution; failures/skips remain uncredited")
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--require-applicable-oracles", action="store_true",
                        help="Reject unlinked applicable records; does not establish semantic conformance")
    args = parser.parse_args()
    try:
        build(args)
    except (TraceError, OSError, ValueError, ET.ParseError) as error:
        parser.exit(1, str(error) + "\n")


if __name__ == "__main__":
    main()
