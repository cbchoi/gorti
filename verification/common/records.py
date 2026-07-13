"""Canonical NDJSON records used by verification drivers."""

from __future__ import annotations

import json
import math
from collections.abc import Iterable, Mapping
from dataclasses import dataclass, field
from pathlib import Path
from typing import IO, Final, TypeAlias

SCHEMA_ID: Final[str] = "gorti.verification/v1"
SERVICES: Final[frozenset[str]] = frozenset({"FM", "DM", "OM", "TM"})
DIRECTIONS: Final[frozenset[str]] = frozenset({"higher", "lower", "exact"})

JSONScalar: TypeAlias = None | bool | int | float | str
JSONValue: TypeAlias = JSONScalar | list["JSONValue"] | dict[str, "JSONValue"]
Record: TypeAlias = "SemanticEvent | PerformanceSample"

_COMMON_FIELDS = frozenset({"schema", "record_type", "sequence", "runtime", "timing"})
_EVENT_FIELDS = _COMMON_FIELDS | {"service", "event", "payload"}
_PERFORMANCE_FIELDS = _COMMON_FIELDS | {
    "metric",
    "value",
    "unit",
    "direction",
    "dimensions",
}


class NDJSONError(ValueError):
    """NDJSONError identifies malformed verification input."""


@dataclass(frozen=True, slots=True)
class SemanticEvent:
    """SemanticEvent is one ordered FM, DM, OM, or TM observation."""

    sequence: int
    service: str
    event: str
    payload: Mapping[str, JSONValue] = field(default_factory=dict)
    runtime: Mapping[str, JSONValue] = field(default_factory=dict)
    timing: Mapping[str, JSONValue] = field(default_factory=dict)

    def to_dict(self) -> dict[str, JSONValue]:
        """Return the JSON-compatible canonical record."""
        record: dict[str, JSONValue] = {
            "schema": SCHEMA_ID,
            "record_type": "event",
            "sequence": self.sequence,
            "service": self.service,
            "event": self.event,
            "payload": dict(self.payload),
        }
        if self.runtime:
            record["runtime"] = dict(self.runtime)
        if self.timing:
            record["timing"] = dict(self.timing)
        validate_record(record)
        return record


@dataclass(frozen=True, slots=True)
class PerformanceSample:
    """PerformanceSample is one benchmark measurement."""

    sequence: int
    metric: str
    value: float
    unit: str
    direction: str
    dimensions: Mapping[str, JSONScalar] = field(default_factory=dict)
    runtime: Mapping[str, JSONValue] = field(default_factory=dict)
    timing: Mapping[str, JSONValue] = field(default_factory=dict)

    def to_dict(self) -> dict[str, JSONValue]:
        """Return the JSON-compatible canonical record."""
        record: dict[str, JSONValue] = {
            "schema": SCHEMA_ID,
            "record_type": "performance",
            "sequence": self.sequence,
            "metric": self.metric,
            "value": self.value,
            "unit": self.unit,
            "direction": self.direction,
            "dimensions": dict(self.dimensions),
        }
        if self.runtime:
            record["runtime"] = dict(self.runtime)
        if self.timing:
            record["timing"] = dict(self.timing)
        validate_record(record)
        return record


def _require_keys(record: Mapping[str, object], required: set[str]) -> None:
    missing = required - record.keys()
    if missing:
        raise NDJSONError(f"record missing required fields: {', '.join(sorted(missing))}")


def _validate_json(value: object, path: str) -> None:
    if value is None or isinstance(value, (bool, int, str)):
        return
    if isinstance(value, float):
        if not math.isfinite(value):
            raise NDJSONError(f"{path} must not contain NaN or infinity")
        return
    if isinstance(value, list):
        for index, item in enumerate(value):
            _validate_json(item, f"{path}[{index}]")
        return
    if isinstance(value, dict):
        for key, item in value.items():
            if not isinstance(key, str):
                raise NDJSONError(f"{path} object keys must be strings")
            _validate_json(item, f"{path}.{key}")
        return
    raise NDJSONError(f"{path} is not a JSON value")


def _validate_common(record: Mapping[str, object]) -> None:
    if record.get("schema") != SCHEMA_ID:
        raise NDJSONError(f"schema must be {SCHEMA_ID!r}")
    sequence = record.get("sequence")
    if isinstance(sequence, bool) or not isinstance(sequence, int) or sequence < 0:
        raise NDJSONError("sequence must be a non-negative integer")
    for name in ("runtime", "timing"):
        value = record.get(name, {})
        if not isinstance(value, dict):
            raise NDJSONError(f"{name} must be an object")
        _validate_json(value, name)


def validate_record(record: Mapping[str, object]) -> None:
    """Validate one record against the canonical schema."""
    record_type = record.get("record_type")
    if record_type == "event":
        _require_keys(record, {"schema", "record_type", "sequence", "service", "event", "payload"})
        extra = record.keys() - _EVENT_FIELDS
        if extra:
            raise NDJSONError(f"event has unknown fields: {', '.join(sorted(extra))}")
        if record.get("service") not in SERVICES:
            raise NDJSONError("service must be one of FM, DM, OM, TM")
        event = record.get("event")
        if not isinstance(event, str) or not event.strip():
            raise NDJSONError("event must be a non-empty string")
        payload = record.get("payload")
        if not isinstance(payload, dict):
            raise NDJSONError("payload must be an object")
        _validate_json(payload, "payload")
    elif record_type == "performance":
        _require_keys(
            record,
            {
                "schema",
                "record_type",
                "sequence",
                "metric",
                "value",
                "unit",
                "direction",
                "dimensions",
            },
        )
        extra = record.keys() - _PERFORMANCE_FIELDS
        if extra:
            raise NDJSONError(f"performance sample has unknown fields: {', '.join(sorted(extra))}")
        for name in ("metric", "unit"):
            value = record.get(name)
            if not isinstance(value, str) or not value.strip():
                raise NDJSONError(f"{name} must be a non-empty string")
        value = record.get("value")
        if isinstance(value, bool) or not isinstance(value, (int, float)):
            raise NDJSONError("value must be a number")
        if not math.isfinite(float(value)):
            raise NDJSONError("value must be finite")
        if record.get("direction") not in DIRECTIONS:
            raise NDJSONError("direction must be one of higher, lower, exact")
        dimensions = record.get("dimensions")
        if not isinstance(dimensions, dict):
            raise NDJSONError("dimensions must be an object")
        if any(isinstance(item, (dict, list)) for item in dimensions.values()):
            raise NDJSONError("dimension values must be JSON scalars")
        _validate_json(dimensions, "dimensions")
    else:
        raise NDJSONError("record_type must be 'event' or 'performance'")
    _validate_common(record)


def parse_record(record: Mapping[str, object]) -> Record:
    """Validate and convert one JSON object to its typed record."""
    validate_record(record)
    if record["record_type"] == "event":
        return SemanticEvent(
            sequence=int(record["sequence"]),
            service=str(record["service"]),
            event=str(record["event"]),
            payload=dict(record["payload"]),  # type: ignore[arg-type]
            runtime=dict(record.get("runtime", {})),  # type: ignore[arg-type]
            timing=dict(record.get("timing", {})),  # type: ignore[arg-type]
        )
    return PerformanceSample(
        sequence=int(record["sequence"]),
        metric=str(record["metric"]),
        value=float(record["value"]),
        unit=str(record["unit"]),
        direction=str(record["direction"]),
        dimensions=dict(record["dimensions"]),  # type: ignore[arg-type]
        runtime=dict(record.get("runtime", {})),  # type: ignore[arg-type]
        timing=dict(record.get("timing", {})),  # type: ignore[arg-type]
    )


def _record_dict(record: Record | Mapping[str, object]) -> dict[str, JSONValue]:
    if isinstance(record, (SemanticEvent, PerformanceSample)):
        return record.to_dict()
    validate_record(record)
    return dict(record)  # type: ignore[return-value]


def dumps_ndjson(records: Iterable[Record | Mapping[str, object]]) -> str:
    """Serialize records as canonical NDJSON with one trailing newline."""
    lines = [
        json.dumps(
            _record_dict(record),
            ensure_ascii=False,
            allow_nan=False,
            sort_keys=True,
            separators=(",", ":"),
        )
        for record in records
    ]
    return "\n".join(lines) + ("\n" if lines else "")


def loads_ndjson(data: str) -> tuple[Record, ...]:
    """Parse and validate canonical NDJSON text."""
    records: list[Record] = []
    for line_number, line in enumerate(data.splitlines(), start=1):
        if not line.strip():
            raise NDJSONError(f"line {line_number}: blank lines are not allowed")
        try:
            value = json.loads(line)
        except json.JSONDecodeError as error:
            raise NDJSONError(f"line {line_number}: invalid JSON: {error.msg}") from error
        if not isinstance(value, dict):
            raise NDJSONError(f"line {line_number}: record must be a JSON object")
        try:
            records.append(parse_record(value))
        except NDJSONError as error:
            raise NDJSONError(f"line {line_number}: {error}") from error
    return tuple(records)


def dump_ndjson(
    records: Iterable[Record | Mapping[str, object]], destination: str | Path | IO[str]
) -> None:
    """Write canonical NDJSON to a path or text stream."""
    data = dumps_ndjson(records)
    if isinstance(destination, (str, Path)):
        Path(destination).write_text(data, encoding="utf-8", newline="\n")
    else:
        destination.write(data)


def load_ndjson(source: str | Path | IO[str]) -> tuple[Record, ...]:
    """Read canonical NDJSON from a path or text stream."""
    if isinstance(source, (str, Path)):
        return loads_ndjson(Path(source).read_text(encoding="utf-8"))
    return loads_ndjson(source.read())
