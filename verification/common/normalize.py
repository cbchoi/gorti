"""Normalization of values that legitimately vary between executions."""

from __future__ import annotations

import re
from collections.abc import Hashable
from dataclasses import dataclass, field

from .records import JSONValue, SemanticEvent

_CAMEL_BOUNDARY = re.compile(r"(?<!^)(?=[A-Z])")
_LOGICAL_KEYS = frozenset(
    {"logical_time", "hla_time", "simulation_time", "requested_time", "granted_time"}
)


def _snake_case(key: str) -> str:
    return _CAMEL_BOUNDARY.sub("_", key).lower().replace("-", "_")


def _is_handle_key(key: str) -> bool:
    key = _snake_case(key)
    return key == "handle" or key.endswith(("_handle", "_handles"))


def _handle_family(key: str) -> str:
    key = _snake_case(key)
    if key.endswith("_handles"):
        return key[: -len("_handles")]
    if key.endswith("_handle"):
        return key[: -len("_handle")]
    return "generic"


def _is_timestamp_key(key: str) -> bool:
    key = _snake_case(key)
    if key in _LOGICAL_KEYS or key.startswith(("logical_", "simulation_")):
        return False
    return key in {
        "wall_time",
        "wall_time_ns",
        "wall_timestamp",
        "wall_timestamp_ns",
        "wall_clock",
        "recorded_at",
        "emitted_at",
        "observed_at",
        "created_at",
        "started_at",
        "finished_at",
    }


def _is_timing_key(key: str) -> bool:
    key = _snake_case(key)
    if key in _LOGICAL_KEYS or key.startswith(("logical_", "simulation_")):
        return False
    stems = ("duration", "elapsed", "latency")
    return key.startswith(stems) or key.endswith(("_duration", "_elapsed", "_latency"))


def _hashable(value: JSONValue) -> Hashable:
    if isinstance(value, list):
        return tuple(_hashable(item) for item in value)
    if isinstance(value, dict):
        return tuple((key, _hashable(item)) for key, item in sorted(value.items()))
    return value


@dataclass(slots=True)
class _HandleMap:
    values: dict[tuple[str, Hashable], str] = field(default_factory=dict)
    counts: dict[str, int] = field(default_factory=dict)

    def replace(self, family: str, value: JSONValue) -> str:
        identity = (family, _hashable(value))
        if identity not in self.values:
            next_value = self.counts.get(family, 0) + 1
            self.counts[family] = next_value
            self.values[identity] = f"<HANDLE:{family}:{next_value}>"
        return self.values[identity]


def _normalize_handle_value(value: JSONValue, family: str, handles: _HandleMap) -> JSONValue:
    if isinstance(value, list):
        return [_normalize_handle_value(item, family, handles) for item in value]
    if isinstance(value, dict):
        return {
            handles.replace(family, key): _normalize_value(item, "", handles)
            for key, item in sorted(value.items())
        }
    if value is None or isinstance(value, bool):
        return value
    return handles.replace(family, value)


def _normalize_value(value: JSONValue, key: str, handles: _HandleMap) -> JSONValue:
    if _is_handle_key(key):
        return _normalize_handle_value(value, _handle_family(key), handles)
    if _is_timestamp_key(key):
        return "<TIMESTAMP>"
    if _is_timing_key(key):
        return "<TIMING>"
    if isinstance(value, list):
        return [_normalize_value(item, key, handles) for item in value]
    if isinstance(value, dict):
        return {
            child_key: _normalize_value(item, child_key, handles)
            for child_key, item in sorted(value.items())
        }
    return value


def normalize_semantic_events(events: list[SemanticEvent]) -> list[dict[str, JSONValue]]:
    """Canonicalize volatile payload values while preserving event order."""
    handles = _HandleMap()
    return [
        {
            "service": event.service,
            "event": event.event,
            "payload": _normalize_value(dict(event.payload), "payload", handles),
        }
        for event in events
    ]
