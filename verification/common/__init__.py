"""Shared, deterministic verification records and comparison helpers."""

from __future__ import annotations

from importlib import import_module
from typing import TYPE_CHECKING, Final

from .payloads import DEFAULT_SEED, generate_payload, payload_envelope, verify_payload_envelope
from .records import (
    SCHEMA_ID,
    NDJSONError,
    PerformanceSample,
    SemanticEvent,
    dump_ndjson,
    dumps_ndjson,
    load_ndjson,
    loads_ndjson,
    parse_record,
    validate_record,
)

if TYPE_CHECKING:
    from .compare import (
        ComparisonResult,
        Difference,
        PerformancePolicy,
        compare_logs,
        compare_records,
    )

_COMPARE_EXPORTS: Final[frozenset[str]] = frozenset(
    {"ComparisonResult", "Difference", "PerformancePolicy", "compare_logs", "compare_records"}
)


def __getattr__(name: str) -> object:
    if name in _COMPARE_EXPORTS:
        return getattr(import_module(".compare", __name__), name)
    raise AttributeError(f"module {__name__!r} has no attribute {name!r}")


__all__ = [
    "DEFAULT_SEED",
    "SCHEMA_ID",
    "ComparisonResult",
    "Difference",
    "NDJSONError",
    "PerformancePolicy",
    "PerformanceSample",
    "SemanticEvent",
    "compare_logs",
    "compare_records",
    "dump_ndjson",
    "dumps_ndjson",
    "generate_payload",
    "load_ndjson",
    "loads_ndjson",
    "parse_record",
    "payload_envelope",
    "validate_record",
    "verify_payload_envelope",
]
