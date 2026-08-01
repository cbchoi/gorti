from __future__ import annotations

import argparse
import json
from pathlib import Path
from typing import Any

try:
    from .schema import SERVICES
except ImportError:  # Script execution from verification/common.
    from schema import SERVICES

_RUNTIME_ONLY_KEYS = frozenset(
    {
        "duration_ns",
        "implementation",
        "monotonic_ns",
        "run_id",
        "runtime_handle",
        "wall_time_ns",
    }
)


def read_ndjson(path: str | Path) -> list[dict[str, Any]]:
    records = []
    with Path(path).open(encoding="utf-8") as stream:
        for line_number, line in enumerate(stream, start=1):
            if not line.strip():
                continue
            record = json.loads(line)
            if not isinstance(record, dict) or "kind" not in record:
                raise ValueError(f"{path}:{line_number}: invalid NDJSON record")
            records.append(record)
    return records


def _portable(value: Any) -> Any:
    if isinstance(value, dict):
        return {
            key: _portable(item)
            for key, item in sorted(value.items())
            if key not in _RUNTIME_ONLY_KEYS
        }
    if isinstance(value, list):
        return [_portable(item) for item in value]
    return value


def canonical_semantics(records: list[dict[str, Any]]) -> list[dict[str, Any]]:
    result = []
    for record in records:
        if record.get("kind") != "semantic":
            continue
        result.append(
            _portable(
                {
                    "service": record.get("service"),
                    "event": record.get("event"),
                    "actor": record.get("actor"),
                    "data": record.get("data", {}),
                }
            )
        )
    return result


def _metrics(records: list[dict[str, Any]]) -> dict[tuple[str, str, str], float]:
    result = {}
    for record in records:
        if record.get("kind") != "metric":
            continue
        key = (record["service"], record["metric"], record["unit"])
        result[key] = float(record["value"])
    return result


def compare_records(
    reference_rti: list[dict[str, Any]], gorti: list[dict[str, Any]]
) -> dict[str, Any]:
    reference_rti_semantics = canonical_semantics(reference_rti)
    gorti_semantics = canonical_semantics(gorti)
    mismatches = []
    count = max(len(reference_rti_semantics), len(gorti_semantics))
    for index in range(count):
        left = reference_rti_semantics[index] if index < len(reference_rti_semantics) else None
        right = gorti_semantics[index] if index < len(gorti_semantics) else None
        if left != right:
            mismatches.append({"index": index, "reference_rti": left, "gorti": right})

    covered = {
        record["service"]
        for record in reference_rti_semantics + gorti_semantics
        if record.get("service") in SERVICES
    }
    missing_services = sorted(SERVICES - covered)

    reference_rti_metrics = _metrics(reference_rti)
    gorti_metrics = _metrics(gorti)
    metric_rows = []
    for key in sorted(set(reference_rti_metrics) | set(gorti_metrics)):
        left = reference_rti_metrics.get(key)
        right = gorti_metrics.get(key)
        ratio = None if left in {None, 0.0} or right is None else right / left
        metric_rows.append(
            {
                "service": key[0],
                "metric": key[1],
                "unit": key[2],
                "reference_rti": left,
                "gorti": right,
                "gorti_over_reference_rti": ratio,
            }
        )

    return {
        "semantic_match": not mismatches and not missing_services,
        "semantic_event_count": {
            "reference_rti": len(reference_rti_semantics),
            "gorti": len(gorti_semantics),
        },
        "missing_service_groups": missing_services,
        "mismatches": mismatches,
        "performance": metric_rows,
    }


def compare_files(reference_rti_path: str | Path, gorti_path: str | Path) -> dict[str, Any]:
    return compare_records(read_ndjson(reference_rti_path), read_ndjson(gorti_path))


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("reference_rti_log", type=Path)
    parser.add_argument("gorti_log", type=Path)
    parser.add_argument("--report", type=Path)
    args = parser.parse_args()
    report = compare_files(args.reference_rti_log, args.gorti_log)
    rendered = json.dumps(report, indent=2, sort_keys=True)
    if args.report:
        args.report.parent.mkdir(parents=True, exist_ok=True)
        args.report.write_text(rendered + "\n", encoding="utf-8")
    print(rendered)
    return 0 if report["semantic_match"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
