"""Validation shared by the DEVStone-HLA benchmark analysis commands."""

from __future__ import annotations

import json
import math
import re
from collections import Counter, defaultdict
from collections.abc import Iterable
from datetime import datetime
from pathlib import Path
from typing import Any

RESULT_SCHEMA_VERSION = "gorti.benchmark.results/v1"
ANALYSIS_SCHEMA_VERSION = "gorti.benchmark.analysis/v1"
RUNS_PER_IMPLEMENTATION = 30
PAIR_COUNT = 30

_ORDER_PATTERNS: dict[str, tuple[tuple[int, ...], ...]] = {
    "paired-balanced-ab-ba": ((0, 1), (1, 0)),
}

_IDENTIFIER = re.compile(r"^[A-Za-z][A-Za-z0-9._-]{0,63}$")
_SHA256 = re.compile(r"^[0-9a-f]{64}$")


class ResultValidationError(ValueError):
    """Raised when a structured benchmark result violates the v1 contract."""

    def __init__(self, errors: Iterable[str]):
        self.errors = tuple(errors)
        super().__init__("\n".join(self.errors))


def _is_int(value: Any) -> bool:
    return isinstance(value, int) and not isinstance(value, bool)


def _is_finite_number(value: Any) -> bool:
    return (
        isinstance(value, (int, float))
        and not isinstance(value, bool)
        and math.isfinite(float(value))
    )


def _exact_object(
    value: Any,
    path: str,
    required: set[str],
    errors: list[str],
) -> dict[str, Any] | None:
    if not isinstance(value, dict):
        errors.append(f"{path}: expected an object")
        return None
    missing = sorted(required - set(value))
    extra = sorted(set(value) - required)
    for key in missing:
        errors.append(f"{path}: missing required field {key!r}")
    for key in extra:
        errors.append(f"{path}: unexpected field {key!r}")
    return value


def _nonempty_string(
    value: Any,
    path: str,
    errors: list[str],
    *,
    maximum: int = 256,
) -> bool:
    if not isinstance(value, str) or not value or len(value) > maximum:
        errors.append(f"{path}: expected a non-empty string of at most {maximum} characters")
        return False
    return True


def _identifier(value: Any, path: str, errors: list[str]) -> bool:
    if not isinstance(value, str) or _IDENTIFIER.fullmatch(value) is None:
        errors.append(f"{path}: expected an identifier matching {_IDENTIFIER.pattern}")
        return False
    return True


def _sha256(value: Any, path: str, errors: list[str]) -> bool:
    if not isinstance(value, str) or _SHA256.fullmatch(value) is None:
        errors.append(f"{path}: expected a lowercase SHA-256 digest")
        return False
    return True


def _validate_benchmark(value: Any, errors: list[str]) -> None:
    keys = {"name", "version", "profile", "configuration_sha256"}
    obj = _exact_object(value, "benchmark", keys, errors)
    if obj is None:
        return
    if obj.get("name") != "DEVStone-HLA":
        errors.append("benchmark.name: expected 'DEVStone-HLA'")
    _nonempty_string(obj.get("version"), "benchmark.version", errors, maximum=64)
    _nonempty_string(obj.get("profile"), "benchmark.profile", errors, maximum=128)
    _sha256(obj.get("configuration_sha256"), "benchmark.configuration_sha256", errors)


def _validate_experiment(value: Any, errors: list[str]) -> None:
    keys = {
        "id",
        "created_at",
        "design",
        "runs_per_implementation",
        "warmup_runs",
        "base_seed",
        "fom_sha256",
        "completion_boundary",
        "process_model",
    }
    obj = _exact_object(value, "experiment", keys, errors)
    if obj is None:
        return
    _identifier(obj.get("id"), "experiment.id", errors)
    created_at = obj.get("created_at")
    if _nonempty_string(created_at, "experiment.created_at", errors, maximum=64):
        try:
            parsed = datetime.fromisoformat(created_at.replace("Z", "+00:00"))
            if parsed.tzinfo is None:
                errors.append("experiment.created_at: timezone information is required")
        except ValueError:
            errors.append("experiment.created_at: expected an ISO 8601 date-time")
    if obj.get("design") != "paired-balanced-ab-ba":
        errors.append("experiment.design: expected 'paired-balanced-ab-ba'")
    if obj.get("runs_per_implementation") != RUNS_PER_IMPLEMENTATION:
        errors.append("experiment.runs_per_implementation: expected exactly 30")
    warmups = obj.get("warmup_runs")
    if not _is_int(warmups) or warmups < 0:
        errors.append("experiment.warmup_runs: expected a non-negative integer")
    base_seed = obj.get("base_seed")
    if not _is_int(base_seed) or base_seed < 0:
        errors.append("experiment.base_seed: expected a non-negative integer")
    _sha256(obj.get("fom_sha256"), "experiment.fom_sha256", errors)
    _nonempty_string(
        obj.get("completion_boundary"),
        "experiment.completion_boundary",
        errors,
        maximum=256,
    )
    if obj.get("process_model") != "fresh-process-per-run":
        errors.append("experiment.process_model: expected 'fresh-process-per-run'")


def _validate_environment(value: Any, errors: list[str]) -> None:
    keys = {"host_id", "os", "cpu", "logical_cores", "memory_bytes", "python_version"}
    obj = _exact_object(value, "environment", keys, errors)
    if obj is None:
        return
    _identifier(obj.get("host_id"), "environment.host_id", errors)
    _nonempty_string(obj.get("os"), "environment.os", errors)
    _nonempty_string(obj.get("cpu"), "environment.cpu", errors)
    for key in ("logical_cores", "memory_bytes"):
        number = obj.get(key)
        if not _is_int(number) or number < 1:
            errors.append(f"environment.{key}: expected a positive integer")
    _nonempty_string(obj.get("python_version"), "environment.python_version", errors, maximum=64)


def _validate_implementations(value: Any, errors: list[str]) -> list[str]:
    if not isinstance(value, list):
        errors.append("implementations: expected an array")
        return []
    if len(value) != 2:
        errors.append("implementations: expected exactly two implementations")
    keys = {"id", "label", "version", "runtime", "artifact_sha256", "source_revision"}
    identifiers: list[str] = []
    for index, item in enumerate(value):
        path = f"implementations[{index}]"
        obj = _exact_object(item, path, keys, errors)
        if obj is None:
            continue
        implementation_id = obj.get("id")
        if _identifier(implementation_id, f"{path}.id", errors):
            identifiers.append(implementation_id)
        _nonempty_string(obj.get("label"), f"{path}.label", errors, maximum=128)
        _nonempty_string(obj.get("version"), f"{path}.version", errors, maximum=128)
        _nonempty_string(obj.get("runtime"), f"{path}.runtime", errors, maximum=128)
        _sha256(obj.get("artifact_sha256"), f"{path}.artifact_sha256", errors)
        _nonempty_string(
            obj.get("source_revision"),
            f"{path}.source_revision",
            errors,
            maximum=128,
        )
    if len(set(identifiers)) != len(identifiers):
        errors.append("implementations: implementation ids must be unique")
    return identifiers


def _validate_metrics(value: Any, errors: list[str]) -> list[str]:
    if not isinstance(value, list) or not value:
        errors.append("metrics: expected a non-empty array")
        return []
    keys = {"id", "label", "unit", "direction"}
    identifiers: list[str] = []
    for index, item in enumerate(value):
        path = f"metrics[{index}]"
        obj = _exact_object(item, path, keys, errors)
        if obj is None:
            continue
        metric_id = obj.get("id")
        if _identifier(metric_id, f"{path}.id", errors):
            identifiers.append(metric_id)
        _nonempty_string(obj.get("label"), f"{path}.label", errors, maximum=128)
        _nonempty_string(obj.get("unit"), f"{path}.unit", errors, maximum=64)
        if obj.get("direction") not in {"lower-is-better", "higher-is-better"}:
            errors.append(
                f"{path}.direction: expected 'lower-is-better' or 'higher-is-better'"
            )
    if len(set(identifiers)) != len(identifiers):
        errors.append("metrics: metric ids must be unique")
    return identifiers


def _validate_runs(
    value: Any,
    implementation_ids: list[str],
    metric_ids: list[str],
    experiment_design: Any,
    expected_workload_sha256: Any,
    expected_fom_sha256: Any,
    errors: list[str],
) -> None:
    if not isinstance(value, list):
        errors.append("runs: expected an array")
        return
    if len(value) != RUNS_PER_IMPLEMENTATION * 2:
        errors.append("runs: expected exactly 60 measured runs")

    keys = {
        "run_id",
        "pair_id",
        "pair_index",
        "implementation",
        "position",
        "seed",
        "status",
        "measurements",
        "evidence",
    }
    run_ids: list[str] = []
    implementation_counts: Counter[str] = Counter()
    pairs: defaultdict[int, list[dict[str, Any]]] = defaultdict(list)
    static_workload_hashes: list[str] = []
    static_fom_hashes: list[str] = []

    for index, item in enumerate(value):
        path = f"runs[{index}]"
        obj = _exact_object(item, path, keys, errors)
        if obj is None:
            continue
        run_id = obj.get("run_id")
        if _identifier(run_id, f"{path}.run_id", errors):
            run_ids.append(run_id)
        _identifier(obj.get("pair_id"), f"{path}.pair_id", errors)

        pair_index = obj.get("pair_index")
        if not _is_int(pair_index) or not 1 <= pair_index <= PAIR_COUNT:
            errors.append(f"{path}.pair_index: expected an integer from 1 through 30")
        else:
            pairs[pair_index].append(obj)

        implementation = obj.get("implementation")
        if implementation not in implementation_ids:
            errors.append(f"{path}.implementation: unknown implementation {implementation!r}")
        else:
            implementation_counts[implementation] += 1

        position = obj.get("position")
        if not _is_int(position) or position not in {1, 2}:
            errors.append(f"{path}.position: expected 1 or 2")
        seed = obj.get("seed")
        if not _is_int(seed) or seed < 0:
            errors.append(f"{path}.seed: expected a non-negative integer")
        if obj.get("status") != "ok":
            errors.append(f"{path}.status: only completed 'ok' runs may be analyzed")

        measurements = obj.get("measurements")
        if not isinstance(measurements, dict):
            errors.append(f"{path}.measurements: expected an object")
            continue
        missing_metrics = sorted(set(metric_ids) - set(measurements))
        extra_metrics = sorted(set(measurements) - set(metric_ids))
        for metric_id in missing_metrics:
            errors.append(f"{path}.measurements: missing metric {metric_id!r}")
        for metric_id in extra_metrics:
            errors.append(f"{path}.measurements: undeclared metric {metric_id!r}")
        for metric_id, measurement in measurements.items():
            if not _is_finite_number(measurement) or float(measurement) < 0:
                errors.append(
                    f"{path}.measurements.{metric_id}: expected a finite, non-negative number"
                )

        evidence_keys = {
            "fom_sha256",
            "workload_sha256",
            "workload_instance_sha256",
            "expected_callbacks",
            "delivered_callbacks",
            "rejected_callbacks",
            "dropped_callbacks",
            "unexpected_callbacks",
            "duplicate_callbacks",
            "invalid_callbacks",
            "ready_synchronized",
            "start_synchronized",
            "measure_synchronized",
            "done_synchronized",
            "attribute_callback_sha256",
            "interaction_callback_sha256",
            "callback_trace_sha256",
            "terminal_state_sha256",
        }
        evidence = _exact_object(
            obj.get("evidence"), f"{path}.evidence", evidence_keys, errors
        )
        if evidence is not None:
            for key in (
                "fom_sha256",
                "workload_sha256",
                "workload_instance_sha256",
                "attribute_callback_sha256",
                "interaction_callback_sha256",
                "callback_trace_sha256",
                "terminal_state_sha256",
            ):
                valid_digest = _sha256(
                    evidence.get(key), f"{path}.evidence.{key}", errors
                )
                if key == "workload_sha256" and valid_digest:
                    static_workload_hashes.append(evidence[key])
                if key == "fom_sha256" and valid_digest:
                    static_fom_hashes.append(evidence[key])
            counters: dict[str, int] = {}
            for key in (
                "expected_callbacks",
                "delivered_callbacks",
                "rejected_callbacks",
                "dropped_callbacks",
                "unexpected_callbacks",
                "duplicate_callbacks",
                "invalid_callbacks",
            ):
                value = evidence.get(key)
                if not _is_int(value) or value < 0:
                    errors.append(
                        f"{path}.evidence.{key}: expected a non-negative integer"
                    )
                else:
                    counters[key] = value
            if counters.get("expected_callbacks", 0) < 1:
                errors.append(
                    f"{path}.evidence.expected_callbacks: expected at least one callback"
                )
            if (
                "expected_callbacks" in counters
                and "delivered_callbacks" in counters
                and counters["delivered_callbacks"] != counters["expected_callbacks"]
            ):
                errors.append(
                    f"{path}.evidence: delivered_callbacks must equal expected_callbacks"
                )
            for key in (
                "rejected_callbacks",
                "dropped_callbacks",
                "unexpected_callbacks",
                "duplicate_callbacks",
                "invalid_callbacks",
            ):
                if counters.get(key, 0) != 0:
                    errors.append(f"{path}.evidence.{key}: expected zero")
            for key in (
                "ready_synchronized",
                "start_synchronized",
                "measure_synchronized",
                "done_synchronized",
            ):
                if evidence.get(key) is not True:
                    errors.append(f"{path}.evidence.{key}: expected true")

    if len(set(run_ids)) != len(run_ids):
        errors.append("runs: run_id values must be unique")
    if static_workload_hashes and len(set(static_workload_hashes)) != 1:
        errors.append(
            "runs: workload_sha256 must identify one static workload across all runs"
        )
    if static_workload_hashes and any(
        digest != expected_workload_sha256 for digest in static_workload_hashes
    ):
        errors.append(
            "runs: workload_sha256 must match benchmark.configuration_sha256"
        )
    if static_fom_hashes and any(
        digest != expected_fom_sha256 for digest in static_fom_hashes
    ):
        errors.append("runs: fom_sha256 must match experiment.fom_sha256")
    for implementation_id in implementation_ids:
        count = implementation_counts[implementation_id]
        if count != RUNS_PER_IMPLEMENTATION:
            errors.append(
                f"runs: implementation {implementation_id!r} has {count} runs; expected 30"
            )

    expected_indices = set(range(1, PAIR_COUNT + 1))
    if set(pairs) != expected_indices:
        missing = sorted(expected_indices - set(pairs))
        errors.append(f"runs: pair_index values must cover 1 through 30; missing {missing}")

    first_counts: Counter[str] = Counter()
    pair_ids: list[str] = []
    pair_seeds: list[int] = []
    workload_instance_hashes: list[str] = []
    order_pattern = _ORDER_PATTERNS.get(experiment_design)
    for pair_index in sorted(pairs):
        pair = pairs[pair_index]
        path = f"pair[{pair_index}]"
        if len(pair) != 2:
            errors.append(f"{path}: expected exactly two runs")
            continue
        ids = [run.get("pair_id") for run in pair]
        if len(set(ids)) != 1:
            errors.append(f"{path}: both runs must use the same pair_id")
        elif isinstance(ids[0], str):
            pair_ids.append(ids[0])
        implementations = [run.get("implementation") for run in pair]
        if set(implementations) != set(implementation_ids):
            errors.append(f"{path}: expected one run from each implementation")
        positions = [run.get("position") for run in pair]
        if set(positions) != {1, 2}:
            errors.append(f"{path}: positions must be exactly 1 and 2")
        else:
            ordered_pair = sorted(pair, key=lambda run: run["position"])
            first = ordered_pair[0]
            if first.get("implementation") in implementation_ids:
                first_counts[first["implementation"]] += 1
            if order_pattern is not None and len(implementation_ids) == 2:
                pattern_entry = order_pattern[(pair_index - 1) % len(order_pattern)]
                expected_order = tuple(
                    implementation_ids[index] for index in pattern_entry
                )
                actual_order = tuple(
                    run.get("implementation") for run in ordered_pair
                )
                if actual_order != expected_order:
                    errors.append(
                        f"{path}: execution order must follow {experiment_design!r}; "
                        f"expected {expected_order}, got {actual_order}"
                    )
        seeds = [run.get("seed") for run in pair]
        if len(set(seeds)) != 1:
            errors.append(f"{path}: paired runs must use the same workload seed")
        elif _is_int(seeds[0]):
            pair_seeds.append(seeds[0])
        evidence = [run.get("evidence") for run in pair]
        if all(isinstance(item, dict) for item in evidence):
            for key in (
                "fom_sha256",
                "workload_sha256",
                "workload_instance_sha256",
                "expected_callbacks",
                "delivered_callbacks",
                "attribute_callback_sha256",
                "interaction_callback_sha256",
                "callback_trace_sha256",
                "terminal_state_sha256",
            ):
                if evidence[0].get(key) != evidence[1].get(key):
                    errors.append(f"{path}: paired evidence differs for {key}")
            instance_hash = evidence[0].get("workload_instance_sha256")
            if (
                instance_hash == evidence[1].get("workload_instance_sha256")
                and isinstance(instance_hash, str)
                and _SHA256.fullmatch(instance_hash) is not None
            ):
                workload_instance_hashes.append(instance_hash)

    if len(set(pair_ids)) != len(pair_ids):
        errors.append("runs: pair_id values must be unique across pairs")
    if len(set(pair_seeds)) != len(pair_seeds):
        errors.append("runs: workload seeds must be unique across the 30 independent pairs")
    if (
        len(workload_instance_hashes) != PAIR_COUNT
        or len(set(workload_instance_hashes)) != PAIR_COUNT
    ):
        errors.append(
            "runs: workload_instance_sha256 must be unique across the 30 pairs"
        )
    if len(implementation_ids) == 2:
        for implementation_id in implementation_ids:
            if first_counts[implementation_id] != PAIR_COUNT // 2:
                errors.append(
                    "runs: execution order must be balanced; "
                    f"{implementation_id!r} is first in {first_counts[implementation_id]} pairs, "
                    "expected 15"
                )


def validate_result_document(document: Any) -> dict[str, Any]:
    """Validate a parsed v1 result document and return it unchanged."""

    errors: list[str] = []
    top_keys = {
        "schema_version",
        "benchmark",
        "experiment",
        "environment",
        "implementations",
        "metrics",
        "runs",
    }
    obj = _exact_object(document, "$", top_keys, errors)
    if obj is None:
        raise ResultValidationError(errors)

    if obj.get("schema_version") != RESULT_SCHEMA_VERSION:
        errors.append(f"schema_version: expected {RESULT_SCHEMA_VERSION!r}")
    _validate_benchmark(obj.get("benchmark"), errors)
    _validate_experiment(obj.get("experiment"), errors)
    _validate_environment(obj.get("environment"), errors)
    implementation_ids = _validate_implementations(obj.get("implementations"), errors)
    metric_ids = _validate_metrics(obj.get("metrics"), errors)
    benchmark = obj.get("benchmark")
    experiment = obj.get("experiment")
    expected_workload_sha256 = (
        benchmark.get("configuration_sha256") if isinstance(benchmark, dict) else None
    )
    expected_fom_sha256 = (
        experiment.get("fom_sha256") if isinstance(experiment, dict) else None
    )
    experiment_design = (
        experiment.get("design") if isinstance(experiment, dict) else None
    )
    _validate_runs(
        obj.get("runs"),
        implementation_ids,
        metric_ids,
        experiment_design,
        expected_workload_sha256,
        expected_fom_sha256,
        errors,
    )

    if errors:
        raise ResultValidationError(errors)
    return obj


def load_result_document(path: str | Path) -> dict[str, Any]:
    """Load and validate a structured result document from a regular file."""

    result_path = Path(path)
    if not result_path.is_file():
        raise ResultValidationError([f"{result_path}: expected a result JSON file"])
    try:
        with result_path.open("r", encoding="utf-8") as stream:
            document = json.load(stream)
    except (OSError, json.JSONDecodeError) as exc:
        raise ResultValidationError([f"{result_path}: cannot read JSON: {exc}"]) from exc
    return validate_result_document(document)
