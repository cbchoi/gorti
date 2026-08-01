"""Convert a real fair-arm benchmark and transcript to launcher-result-v1."""

from __future__ import annotations

import argparse
import copy
import sys
from collections import defaultdict
from collections.abc import Mapping, Sequence
from pathlib import Path
from typing import Any, cast

PACKAGE_ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(PACKAGE_ROOT))

from fair_comparison.contract import (  # noqa: E402
    RESULT_SCHEMA,
    ContractError,
    canonical_json,
    load_json,
    loads_json_object,
    sha256_file,
    sha256_json,
    validate_result,
    validate_workload,
)

SYNC_LABELS = ("VERIFY_READY", "VERIFY_DONE")


def _load_ndjson(path: Path) -> list[dict[str, Any]]:
    records: list[dict[str, Any]] = []
    try:
        lines = path.read_text(encoding="utf-8").splitlines()
    except (OSError, UnicodeError) as error:
        raise ContractError(f"{path}: cannot read canonical transcript: {error}") from error
    if not lines:
        raise ContractError(f"{path}: canonical transcript is empty")
    for line_number, line in enumerate(lines, start=1):
        if not line.strip():
            raise ContractError(f"{path}:{line_number}: blank canonical record")
        record = loads_json_object(line, f"{path}:{line_number}")
        records.append(record)
    return records


def _events(
    records: Sequence[Mapping[str, Any]], *, actor: str, event: str
) -> list[Mapping[str, Any]]:
    return [
        record
        for record in records
        if record.get("actor") == actor and record.get("event") == event
    ]


def _data(record: Mapping[str, Any], context: str) -> Mapping[str, Any]:
    value = record.get("data")
    if not isinstance(value, dict):
        raise ContractError(f"{context} has no data object")
    return value


def _one_event(
    records: Sequence[Mapping[str, Any]], *, actor: str, event: str, context: str
) -> Mapping[str, Any]:
    matches = _events(records, actor=actor, event=event)
    if len(matches) != 1:
        raise ContractError(f"{context}: expected one {actor} {event}, found {len(matches)}")
    return matches[0]


def _indexed_payloads(
    records: Sequence[Mapping[str, Any]], *, actor: str, event: str, count: int
) -> list[str]:
    matches = _events(records, actor=actor, event=event)
    if len(matches) != count:
        raise ContractError(f"expected {count} {actor} {event} records, found {len(matches)}")
    values: dict[int, str] = {}
    for record in matches:
        data = _data(record, f"{actor} {event}")
        index = data.get("index")
        logical_time = data.get("logical_time")
        payload = data.get("payload")
        if isinstance(index, bool) or not isinstance(index, int) or index in values:
            raise ContractError(f"{actor} {event} has invalid or duplicate index")
        if logical_time != index + 1 or not isinstance(payload, str) or not payload:
            raise ContractError(f"{actor} {event} payload/time mismatch at index {index}")
        values[index] = payload
    if sorted(values) != list(range(count)):
        raise ContractError(f"{actor} {event} indexes are not contiguous")
    return [values[index] for index in range(count)]


def canonical_projection(
    records: Sequence[Mapping[str, Any]],
    *,
    count: int,
    object_class: str,
    interaction_class: str,
    object_name: str = "",
) -> list[dict[str, Any]]:
    for actor in ("publisher", "subscriber"):
        for label in SYNC_LABELS:
            synchronized = [
                record
                for record in _events(records, actor=actor, event="federation_synchronized")
                if _data(record, f"{actor} federation_synchronized").get("label") == label
            ]
            if len(synchronized) != 1:
                raise ContractError(f"{actor} did not synchronize exactly once at {label}")

    declarations = {
        ("publisher", "object_published", object_class),
        ("publisher", "interaction_published", interaction_class),
        ("subscriber", "object_subscribed", object_class),
        ("subscriber", "interaction_subscribed", interaction_class),
    }
    for actor, event, class_name in declarations:
        record = _one_event(records, actor=actor, event=event, context="declaration projection")
        if _data(record, f"{actor} {event}").get("class") != class_name:
            raise ContractError(f"{actor} {event} class differs from {class_name}")

    if object_name:
        for actor, event in (
            ("publisher", "object_registered"),
            ("subscriber", "object_discovered"),
        ):
            record = _one_event(records, actor=actor, event=event,
                                context="object identity projection")
            if _data(record, f"{actor} {event}").get("name") != object_name:
                raise ContractError(f"{actor} {event} name differs from {object_name}")

    attributes = _indexed_payloads(
        records, actor="publisher", event="attributes_updated", count=count
    )
    interactions = _indexed_payloads(
        records, actor="publisher", event="interaction_sent", count=count
    )
    if attributes != _indexed_payloads(
        records, actor="subscriber", event="attributes_reflected", count=count
    ):
        raise ContractError("attribute payload projection differs between publisher and subscriber")
    if interactions != _indexed_payloads(
        records, actor="subscriber", event="interaction_received", count=count
    ):
        raise ContractError(
            "interaction payload projection differs between publisher and subscriber"
        )

    for actor, event in (
        ("publisher", "object_registered"),
        ("subscriber", "object_discovered"),
        ("publisher", "object_deleted"),
        ("subscriber", "object_removed"),
    ):
        _one_event(records, actor=actor, event=event, context="object lifecycle projection")

    expected_grants = list(range(1, count + 2))
    for actor in ("publisher", "subscriber"):
        grants = sorted(
            int(_data(record, f"{actor} grant")["logical_time"])
            for record in _events(records, actor=actor, event="time_advance_granted")
        )
        if grants != expected_grants:
            raise ContractError(f"{actor} grant projection differs from 1..count+1")

    rows = [
        {
            "kind": "semantic",
            "seq": 0,
            "service": "FM",
            "event": "federation_lifecycle_verified",
            "actor": "verifier",
            "data": {"federates": 2, "sync_labels": list(SYNC_LABELS)},
        },
        {
            "kind": "semantic",
            "seq": 1,
            "service": "DM",
            "event": "declarations_verified",
            "actor": "verifier",
            "data": {"object_class": object_class, "interaction_class": interaction_class},
        },
        {
            "kind": "semantic",
            "seq": 2,
            "service": "OM",
            "event": "timestamped_workload_verified",
            "actor": "verifier",
            "data": {
                "count": count,
                "named_instance": True,
                "removed": True,
                "attribute_payloads": attributes,
                "interaction_payloads": interactions,
            },
        },
        {
            "kind": "semantic",
            "seq": 3,
            "service": "TM",
            "event": "time_management_verified",
            "actor": "verifier",
            "data": {"lookahead": 1, "order": "TimeStamp", "grants": expected_grants},
        },
    ]
    return [{"service": row["service"], "record": row} for row in rows]


def _validate_benchmark_workload(
    benchmark: Mapping[str, Any], workload: Mapping[str, Any], implementation: str
) -> None:
    if benchmark.get("schema") != "gorti.production-benchmark/v1":
        raise ContractError("benchmark schema must be gorti.production-benchmark/v1")
    metadata = benchmark.get("metadata")
    if not isinstance(metadata, dict) or not isinstance(metadata.get("workload"), dict):
        raise ContractError("benchmark metadata.workload is missing")
    actual = cast(Mapping[str, Any], metadata["workload"])
    common = {
        "schema": workload["schema"],
        "fom_sha256": workload["fom_sha256"],
        "count": workload["count"],
        "two_process": True,
        "choreography": workload["choreography"],
        "delivery_boundary": workload["delivery_boundary"],
        "callback": workload["callback"],
        "server_event_log": workload["server_event_log"],
    }
    for name, expected in common.items():
        if actual.get(name) != expected:
            raise ContractError(f"benchmark workload {name} differs from shared workload")
    if int(actual.get("seed", -1)) != 1516:
        raise ContractError("benchmark workload seed differs from 1516")


def _metrics(benchmark: Mapping[str, Any]) -> list[dict[str, Any]]:
    samples = benchmark.get("samples")
    if not isinstance(samples, list) or not samples:
        raise ContractError("benchmark raw samples are missing")
    grouped: dict[tuple[str, str], list[int]] = defaultdict(list)
    dimensions_by_key: dict[tuple[str, str], dict[str, Any]] = {}
    for index, sample in enumerate(samples):
        if not isinstance(sample, dict):
            raise ContractError(f"benchmark sample {index} is not an object")
        operation = sample.get("operation")
        duration = sample.get("duration_ns")
        dimensions = sample.get("dimensions")
        if not isinstance(operation, str) or not operation:
            raise ContractError(f"benchmark sample {index} operation is invalid")
        if isinstance(duration, bool) or not isinstance(duration, int) or duration < 0:
            raise ContractError(f"benchmark sample {index} duration is invalid")
        if not isinstance(dimensions, dict):
            raise ContractError(f"benchmark sample {index} dimensions are invalid")
        key = operation, canonical_json(dimensions)
        grouped[key].append(duration)
        dimensions_by_key[key] = copy.deepcopy(dimensions)
    metrics: list[dict[str, Any]] = []
    for operation, encoded_dimensions in sorted(grouped):
        key = operation, encoded_dimensions
        scope = (
            "subscriber_pre_tar_to_both_callbacks"
            if operation == "completed_delivery_batch_latency"
            else "caller_api_call"
        )
        metrics.append(
            {
                "name": operation,
                "unit": "ns",
                "direction": "lower",
                "sample_scope": scope,
                "dimensions": dimensions_by_key[key],
                "samples": grouped[key],
            }
        )
    return metrics


def convert_artifacts(
    *,
    benchmark_path: Path,
    canonical_path: Path,
    workload_path: Path,
    provenance_path: Path,
    fom_path: Path,
    implementation: str,
    run_id: str,
    object_class: str = "VerifierEntity",
    interaction_class: str = "VerifierMessage",
    object_name: str = "",
) -> dict[str, Any]:
    workload = validate_workload(load_json(workload_path))
    if sha256_file(fom_path) != workload["fom_sha256"]:
        raise ContractError("canonical FOM bytes differ from the workload hash")
    benchmark = load_json(benchmark_path)
    _validate_benchmark_workload(benchmark, workload, implementation)
    records = _load_ndjson(canonical_path)
    projection = canonical_projection(
        records,
        count=int(workload["count"]),
        object_class=object_class,
        interaction_class=interaction_class,
        object_name=object_name,
    )

    benchmark_accounting = benchmark.get("delivery_accounting")
    if not isinstance(benchmark_accounting, dict):
        raise ContractError("benchmark delivery accounting is missing")
    expected = 2 * int(workload["count"])
    if (
        benchmark_accounting.get("expected_fanout") != expected
        or benchmark_accounting.get("delivered") != expected
        or benchmark_accounting.get("explicitly_rejected") != 0
        or benchmark_accounting.get("dropped") != 0
    ):
        raise ContractError("benchmark accounting is not exact no-loss 2 * count")

    result = {
        "schema": RESULT_SCHEMA,
        "run_id": run_id,
        "implementation": implementation,
        "workload": workload,
        "semantics": {
            "normalization": "gorti.fm-dm-om-tm-projection/v1",
            "canonical_projection": projection,
            "projection_sha256": sha256_json(projection),
            "status": "pass",
        },
        "provenance": load_json(provenance_path),
        "metrics": _metrics(benchmark),
        "accounting": {
            "expected_fanout": expected,
            "delivered": expected,
            "explicitly_rejected": 0,
            "dropped": 0,
            "duplicates": 0,
            "invalid": 0,
        },
    }
    return validate_result(
        result,
        expected_workload=workload,
        expected_implementation=implementation,
        expected_run_id=run_id,
    )


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--benchmark", required=True, type=Path)
    parser.add_argument("--canonical", required=True, type=Path)
    parser.add_argument("--workload", required=True, type=Path)
    parser.add_argument("--provenance", required=True, type=Path)
    parser.add_argument("--fom", required=True, type=Path)
    parser.add_argument("--implementation", required=True, choices=("reference_rti", "go"))
    parser.add_argument("--run-id", required=True)
    parser.add_argument("--object-class", default="VerifierEntity")
    parser.add_argument("--interaction-class", default="VerifierMessage")
    parser.add_argument("--object-name", default="")
    parser.add_argument("--output", required=True, type=Path)
    return parser


def main(argv: Sequence[str] | None = None) -> int:
    args = build_parser().parse_args(argv)
    try:
        result = convert_artifacts(
            benchmark_path=args.benchmark.resolve(),
            canonical_path=args.canonical.resolve(),
            workload_path=args.workload.resolve(),
            provenance_path=args.provenance.resolve(),
            fom_path=args.fom.resolve(),
            implementation=args.implementation,
        run_id=args.run_id,
        object_class=args.object_class,
        interaction_class=args.interaction_class,
        object_name=args.object_name,
    )
        args.output.write_text(canonical_json(result) + "\n", encoding="utf-8", newline="\n")
    except ContractError as error:
        raise SystemExit(f"adapter conversion rejected: {error}") from error
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
