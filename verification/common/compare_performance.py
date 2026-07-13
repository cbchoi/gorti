from __future__ import annotations

import argparse
import json
import math
import statistics
from pathlib import Path
from typing import Any


def _read(path: Path) -> list[dict[str, Any]]:
    return [json.loads(line) for line in path.read_text(encoding="utf-8").splitlines() if line]


def _percentile(values: list[float], percentile: int) -> float | None:
    if not values:
        return None
    ordered = sorted(values)
    index = max(0, math.ceil(percentile / 100.0 * len(ordered)) - 1)
    return ordered[index]


def _pitch_samples(records: list[dict[str, Any]], metric: str) -> list[float]:
    return [
        float(item["value"]) / 1_000_000.0
        for item in records
        if item.get("metric") == metric and item.get("unit") == "nanoseconds"
    ]


def _gorti_value(records: list[dict[str, Any]], metric: str) -> float | None:
    for item in records:
        if item.get("metric") == metric:
            return float(item["value"])
    return None


def _row(name: str, unit: str, pitch: float | None, gorti: float | None) -> dict[str, Any]:
    ratio = None if pitch in {None, 0.0} or gorti is None else gorti / pitch
    return {
        "metric": name,
        "unit": unit,
        "pitch": pitch,
        "gorti": gorti,
        "gorti_over_pitch": ratio,
    }


def compare(pitch: list[dict[str, Any]], gorti: list[dict[str, Any]]) -> dict[str, Any]:
    rows = []
    mappings = (
        (
            "attribute_update_call_latency",
            "call_latency.update_attribute_values",
            "updateAttributeValues",
        ),
        ("interaction_send_call_latency", "call_latency.send_interaction", "sendInteraction"),
        (
            "completed_delivery_batch_latency",
            "completed_delivery_batch_latency",
            "completed_delivery_batch_latency",
        ),
    )
    for name, pitch_name, gorti_name in mappings:
        pitch_values = _pitch_samples(pitch, pitch_name)
        for suffix, function in (
            ("mean", lambda values: statistics.fmean(values) if values else None),
            ("p50", lambda values: _percentile(values, 50)),
            ("p95", lambda values: _percentile(values, 95)),
        ):
            rows.append(
                _row(
                    f"{name}.{suffix}",
                    "milliseconds",
                    function(pitch_values),
                    _gorti_value(gorti, f"{gorti_name}.{suffix}"),
                )
            )

    rows.append(
        _row(
            "sustained_throughput",
            "deliveries_per_second",
            _gorti_value(pitch, "sustained_throughput"),
            _gorti_value(gorti, "sustained_throughput"),
        )
    )
    return {
        "note": "Performance values are reported, not used as semantic pass/fail criteria.",
        "metrics": rows,
    }


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("pitch", type=Path)
    parser.add_argument("gorti", type=Path)
    parser.add_argument("--report", type=Path, required=True)
    args = parser.parse_args()
    report = compare(_read(args.pitch), _read(args.gorti))
    args.report.parent.mkdir(parents=True, exist_ok=True)
    args.report.write_text(json.dumps(report, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    print(json.dumps(report, indent=2, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
