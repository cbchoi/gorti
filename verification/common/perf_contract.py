"""Production benchmark records, validation, and distribution summaries."""

from __future__ import annotations

import json
import math
import re
import statistics
import threading
from collections import defaultdict
from collections.abc import Iterable, Mapping, Sequence
from dataclasses import dataclass, field
from typing import Final

from .records import JSONScalar, JSONValue

SCHEMA_ID: Final[str] = "gorti.production-benchmark/v1"
_SHA256: Final[re.Pattern[str]] = re.compile(r"[0-9a-fA-F]{64}")


class BenchmarkContractError(ValueError):
    """BenchmarkContractError identifies an invalid benchmark artifact."""


def _require_non_empty(value: object, name: str) -> None:
    if not isinstance(value, str) or not value.strip():
        raise BenchmarkContractError(f"{name} must be a non-empty string")


def _validate_json(value: object, path: str) -> None:
    if value is None or isinstance(value, (bool, int, str)):
        return
    if isinstance(value, float):
        if not math.isfinite(value):
            raise BenchmarkContractError(f"{path} must not contain NaN or infinity")
        return
    if isinstance(value, list):
        for index, item in enumerate(value):
            _validate_json(item, f"{path}[{index}]")
        return
    if isinstance(value, dict):
        for key, item in value.items():
            if not isinstance(key, str):
                raise BenchmarkContractError(f"{path} object keys must be strings")
            _validate_json(item, f"{path}.{key}")
        return
    raise BenchmarkContractError(f"{path} is not a JSON value")


def _validate_mapping(value: Mapping[str, JSONValue], path: str) -> None:
    _validate_json(dict(value), path)


@dataclass(frozen=True, slots=True)
class RunMetadata:
    """RunMetadata captures the conditions and provenance of one measured run."""

    run_id: str
    benchmark: str
    started_at: str
    commit: str
    binary_sha256: str
    runtime_versions: Mapping[str, str]
    build_flags: Sequence[str] = ()
    environment: Mapping[str, JSONValue] = field(default_factory=dict)
    workload: Mapping[str, JSONValue] = field(default_factory=dict)

    def __post_init__(self) -> None:
        self.validate()

    def validate(self) -> None:
        """Reject missing or malformed run metadata and provenance."""
        for name in ("run_id", "benchmark", "started_at", "commit"):
            _require_non_empty(getattr(self, name), name)
        if not isinstance(self.binary_sha256, str) or not _SHA256.fullmatch(self.binary_sha256):
            raise BenchmarkContractError("binary_sha256 must be a 64-character hex digest")
        if not self.runtime_versions:
            raise BenchmarkContractError("runtime_versions must contain at least one version")
        for runtime, version in self.runtime_versions.items():
            _require_non_empty(runtime, "runtime_versions key")
            _require_non_empty(version, f"runtime_versions[{runtime!r}]")
        for index, flag in enumerate(self.build_flags):
            _require_non_empty(flag, f"build_flags[{index}]")
        _validate_mapping(self.environment, "environment")
        _validate_mapping(self.workload, "workload")

    def to_dict(self) -> dict[str, object]:
        """Return JSON-compatible metadata with provenance kept explicit."""
        self.validate()
        return {
            "run_id": self.run_id,
            "benchmark": self.benchmark,
            "started_at": self.started_at,
            "environment": dict(self.environment),
            "workload": dict(self.workload),
            "provenance": {
                "commit": self.commit,
                "binary_sha256": self.binary_sha256.lower(),
                "runtime_versions": dict(self.runtime_versions),
                "build_flags": list(self.build_flags),
            },
        }


@dataclass(frozen=True, slots=True)
class OperationSample:
    """OperationSample is one unaggregated production-path duration."""

    sequence: int
    operation: str
    duration_ns: int
    dimensions: Mapping[str, JSONScalar] = field(default_factory=dict)

    def __post_init__(self) -> None:
        self.validate()

    def validate(self) -> None:
        """Reject samples that cannot be compared or serialized exactly."""
        if isinstance(self.sequence, bool) or not isinstance(self.sequence, int):
            raise BenchmarkContractError("sample sequence must be a non-negative integer")
        if self.sequence < 0:
            raise BenchmarkContractError("sample sequence must be a non-negative integer")
        _require_non_empty(self.operation, "sample operation")
        if isinstance(self.duration_ns, bool) or not isinstance(self.duration_ns, int):
            raise BenchmarkContractError("sample duration_ns must be a non-negative integer")
        if self.duration_ns < 0:
            raise BenchmarkContractError("sample duration_ns must be a non-negative integer")
        for name, value in self.dimensions.items():
            _require_non_empty(name, "dimension name")
            if isinstance(value, (dict, list)):
                raise BenchmarkContractError("sample dimensions must contain JSON scalars")
            _validate_json(value, f"dimensions.{name}")

    def to_dict(self) -> dict[str, object]:
        """Return the exact raw sample in JSON-compatible form."""
        self.validate()
        return {
            "sequence": self.sequence,
            "operation": self.operation,
            "duration_ns": self.duration_ns,
            "dimensions": dict(self.dimensions),
        }


@dataclass(frozen=True, slots=True)
class DeliveryAccounting:
    """DeliveryAccounting proves the disposition of the full expected fanout."""

    expected_fanout: int
    delivered: int
    explicitly_rejected: int
    dropped: int

    def __post_init__(self) -> None:
        self.validate()

    @property
    def rejected(self) -> int:
        """Return the explicitly rejected count using the shorter accounting term."""
        return self.explicitly_rejected

    def validate(self) -> None:
        """Require non-negative counts and complete delivery accounting."""
        for name in ("expected_fanout", "delivered", "explicitly_rejected", "dropped"):
            value = getattr(self, name)
            if isinstance(value, bool) or not isinstance(value, int) or value < 0:
                raise BenchmarkContractError(f"{name} must be a non-negative integer")
        accounted = self.delivered + self.explicitly_rejected + self.dropped
        if accounted != self.expected_fanout:
            raise BenchmarkContractError(
                "incomplete delivery accounting: delivered + explicitly_rejected + dropped "
                f"must equal expected_fanout ({accounted} != {self.expected_fanout})"
            )

    def to_dict(self) -> dict[str, int]:
        """Return complete delivery accounting in JSON-compatible form."""
        self.validate()
        return {
            "expected_fanout": self.expected_fanout,
            "delivered": self.delivered,
            "explicitly_rejected": self.explicitly_rejected,
            "dropped": self.dropped,
        }


@dataclass(frozen=True, slots=True)
class OperationSummary:
    """OperationSummary contains deterministic statistics for one sample group."""

    operation: str
    dimensions: Mapping[str, JSONScalar]
    count: int
    median_ns: float
    p95_ns: float
    p99_ns: float

    def to_dict(self) -> dict[str, object]:
        """Return the summary in JSON-compatible form."""
        return {
            "operation": self.operation,
            "dimensions": dict(self.dimensions),
            "count": self.count,
            "median_ns": self.median_ns,
            "p95_ns": self.p95_ns,
            "p99_ns": self.p99_ns,
        }


def _nearest_rank(ordered: Sequence[int], percentile: int) -> float:
    rank = max(0, math.ceil(percentile / 100.0 * len(ordered)) - 1)
    return float(ordered[rank])


def summarize_samples(samples: Iterable[OperationSample]) -> tuple[OperationSummary, ...]:
    """Summarize samples by operation and dimensions using nearest-rank tails."""
    groups: dict[tuple[str, tuple[tuple[str, JSONScalar], ...]], list[int]] = defaultdict(list)
    dimensions_by_group: dict[
        tuple[str, tuple[tuple[str, JSONScalar], ...]], Mapping[str, JSONScalar]
    ] = {}
    for sample in samples:
        if not isinstance(sample, OperationSample):
            raise BenchmarkContractError("samples must contain OperationSample records")
        sample.validate()
        dimensions = tuple(sorted(sample.dimensions.items()))
        key = sample.operation, dimensions
        groups[key].append(sample.duration_ns)
        dimensions_by_group[key] = sample.dimensions

    summaries: list[OperationSummary] = []
    for key in sorted(groups, key=lambda item: (item[0], repr(item[1]))):
        operation, _ = key
        ordered = sorted(groups[key])
        summaries.append(
            OperationSummary(
                operation=operation,
                dimensions=dict(dimensions_by_group[key]),
                count=len(ordered),
                median_ns=float(statistics.median(ordered)),
                p95_ns=_nearest_rank(ordered, 95),
                p99_ns=_nearest_rank(ordered, 99),
            )
        )
    return tuple(summaries)


@dataclass(frozen=True, slots=True)
class BenchmarkRun:
    """BenchmarkRun is a validated, self-contained production benchmark artifact."""

    metadata: RunMetadata
    samples: Sequence[OperationSample]
    delivery_accounting: DeliveryAccounting

    def __post_init__(self) -> None:
        object.__setattr__(self, "samples", tuple(self.samples))
        self.validate()

    def validate(self) -> None:
        """Validate all records and require unique raw sample sequence numbers."""
        if not isinstance(self.metadata, RunMetadata):
            raise BenchmarkContractError("metadata must be a RunMetadata record")
        if not isinstance(self.delivery_accounting, DeliveryAccounting):
            raise BenchmarkContractError("delivery_accounting must be a DeliveryAccounting record")
        self.metadata.validate()
        self.delivery_accounting.validate()
        sequences: set[int] = set()
        for sample in self.samples:
            if not isinstance(sample, OperationSample):
                raise BenchmarkContractError("samples must contain OperationSample records")
            sample.validate()
            if sample.sequence in sequences:
                raise BenchmarkContractError(
                    f"sample sequence {sample.sequence} appears more than once"
                )
            sequences.add(sample.sequence)

    @property
    def summaries(self) -> tuple[OperationSummary, ...]:
        """Return median, p95, and p99 for each operation/dimension group."""
        return summarize_samples(self.samples)

    def to_dict(self) -> dict[str, object]:
        """Return the validated artifact with both raw and summarized values."""
        self.validate()
        return {
            "schema": SCHEMA_ID,
            "metadata": self.metadata.to_dict(),
            "samples": [sample.to_dict() for sample in self.samples],
            "delivery_accounting": self.delivery_accounting.to_dict(),
            "summaries": [summary.to_dict() for summary in self.summaries],
        }


class BenchmarkRecorder:
    """BenchmarkRecorder assigns stable sequence numbers to concurrent samples."""

    def __init__(self, metadata: RunMetadata) -> None:
        metadata.validate()
        self._metadata = metadata
        self._samples: list[OperationSample] = []
        self._finished = False
        self._lock = threading.Lock()

    def record(
        self,
        operation: str,
        duration_ns: int,
        dimensions: Mapping[str, JSONScalar] | None = None,
    ) -> OperationSample:
        """Record one completed operation and return its immutable raw sample."""
        with self._lock:
            if self._finished:
                raise BenchmarkContractError("cannot record after the benchmark is finished")
            sample = OperationSample(
                sequence=len(self._samples),
                operation=operation,
                duration_ns=duration_ns,
                dimensions={} if dimensions is None else dict(dimensions),
            )
            self._samples.append(sample)
            return sample

    def finish(self, delivery_accounting: DeliveryAccounting) -> BenchmarkRun:
        """Seal the recorder and return a validated benchmark artifact."""
        with self._lock:
            if self._finished:
                raise BenchmarkContractError("benchmark recorder is already finished")
            run = BenchmarkRun(
                metadata=self._metadata,
                samples=tuple(self._samples),
                delivery_accounting=delivery_accounting,
            )
            self._finished = True
            return run


def dumps_benchmark(run: BenchmarkRun) -> str:
    """Serialize a benchmark artifact as deterministic JSON with a final LF."""
    if not isinstance(run, BenchmarkRun):
        raise BenchmarkContractError("run must be a BenchmarkRun record")
    return (
        json.dumps(
            run.to_dict(),
            ensure_ascii=False,
            allow_nan=False,
            sort_keys=True,
            separators=(",", ":"),
        )
        + "\n"
    )


__all__ = [
    "SCHEMA_ID",
    "BenchmarkContractError",
    "BenchmarkRecorder",
    "BenchmarkRun",
    "DeliveryAccounting",
    "OperationSample",
    "OperationSummary",
    "RunMetadata",
    "dumps_benchmark",
    "summarize_samples",
]
