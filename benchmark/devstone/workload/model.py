"""Build a deterministic DEVStone-derived HLA workload document.

The graph follows the LI and HI structures and transition/event equations from
the revisited DEVStone definition. It describes application traffic for an HLA
comparison; it does not execute or measure a DEVS simulation kernel.
"""

from __future__ import annotations

import hashlib
import json
from collections.abc import Mapping
from dataclasses import dataclass
from pathlib import Path
from typing import Any

SCHEMA_VERSION = "devstone-hla-workload/v1"
GENERATOR_VERSION = "1.1.0"
TOPOLOGY_IDENTITY_VERSION = "devstone-hla-topology/v1"
CANONICALIZATION = "sorted-key-compact-json-utf8-v1"
TOPOLOGY_IDENTITY_SCOPE = (
    "topology, width, depth, external_event_count, ordered atomic_nodes, "
    "ordered directed_couplings, hla_mapping, and synthetic transition delays"
)
DEFAULT_TOPOLOGY = "HI"
DEFAULT_WIDTH = 10
DEFAULT_DEPTH = 10
DEFAULT_EXTERNAL_EVENTS = 100
DEFAULT_SEED = 1516
SUPPORTED_TOPOLOGIES = ("LI", "HI")


@dataclass(frozen=True)
class WorkloadConfig:
    """Parameters that define one immutable workload."""

    topology: str = DEFAULT_TOPOLOGY
    width: int = DEFAULT_WIDTH
    depth: int = DEFAULT_DEPTH
    external_event_count: int = DEFAULT_EXTERNAL_EVENTS
    seed: int = DEFAULT_SEED

    def __post_init__(self) -> None:
        topology = self.topology.upper()
        object.__setattr__(self, "topology", topology)
        if topology not in SUPPORTED_TOPOLOGIES:
            raise ValueError(
                f"topology must be one of {', '.join(SUPPORTED_TOPOLOGIES)}"
            )
        _require_int("width", self.width, minimum=2)
        _require_int("depth", self.depth, minimum=1)
        _require_int(
            "external_event_count",
            self.external_event_count,
            minimum=1,
            maximum=(1 << 32) - 1,
        )
        _require_int("seed", self.seed, minimum=0, maximum=(1 << 64) - 1)


def _require_int(
    name: str, value: object, *, minimum: int, maximum: int | None = None
) -> None:
    if isinstance(value, bool) or not isinstance(value, int):
        raise TypeError(f"{name} must be an integer")
    if value < minimum or (maximum is not None and value > maximum):
        suffix = f" and <= {maximum}" if maximum is not None else ""
        raise ValueError(f"{name} must be >= {minimum}{suffix}")


def _node_id(level: int, index: int, config: WorkloadConfig) -> str:
    level_digits = max(4, len(str(config.depth - 1)))
    atom_digits = max(4, len(str(config.width - 1)))
    return f"l{level:0{level_digits}d}.a{index:0{atom_digits}d}"


def _atomic_nodes(config: WorkloadConfig) -> list[dict[str, Any]]:
    nodes: list[dict[str, Any]] = []
    for level in range(config.depth):
        count = 1 if level == config.depth - 1 else config.width - 1
        for index in range(1, count + 1):
            nodes.append(
                {
                    "id": _node_id(level, index, config),
                    "kind": "devstone_atomic",
                    "level": level,
                    "index": index,
                    "ports": {"input": "in", "output": "out"},
                }
            )
    return nodes


def _directed_couplings(
    config: WorkloadConfig, nodes: list[dict[str, Any]]
) -> list[dict[str, Any]]:
    pending: list[dict[str, Any]] = []

    # Flatten the recursive external-input couplings into one deterministic fanout.
    for node in nodes:
        pending.append(
            {
                "kind": "external_input",
                "source": {"node": "__injection__", "port": "out"},
                "target": {"node": node["id"], "port": "in"},
            }
        )

    # HI adds the shift-register chain at every non-deepest level.
    if config.topology == "HI":
        for level in range(config.depth - 1):
            for index in range(1, config.width - 1):
                pending.append(
                    {
                        "kind": "internal",
                        "source": {
                            "node": _node_id(level, index, config),
                            "port": "out",
                        },
                        "target": {
                            "node": _node_id(level, index + 1, config),
                            "port": "in",
                        },
                    }
                )

    digits = max(6, len(str(len(pending))))
    return [
        {"id": f"c{number:0{digits}d}", **coupling}
        for number, coupling in enumerate(pending, start=1)
    ]


def _injected_events(config: WorkloadConfig) -> list[dict[str, Any]]:
    digits = max(6, len(str(config.external_event_count)))
    events: list[dict[str, Any]] = []
    for sequence in range(1, config.external_event_count + 1):
        material = (
            f"{SCHEMA_VERSION}\x00seed={config.seed}\x00sequence={sequence}"
        ).encode("ascii")
        payload = hashlib.sha256(material).digest()[:16]
        events.append(
            {
                "id": f"inject-{sequence:0{digits}d}",
                "sequence": sequence,
                "logical_time": sequence,
                "payload": {
                    "encoding": "hex",
                    "size_bytes": len(payload),
                    "value": payload.hex(),
                },
            }
        )
    return events


def _atomic_count(config: WorkloadConfig) -> int:
    return (config.width - 1) * (config.depth - 1) + 1


def _event_count_per_injection(config: WorkloadConfig) -> int:
    if config.topology == "LI":
        return _atomic_count(config)
    triangular_width = config.width * (config.width - 1) // 2
    return triangular_width * (config.depth - 1) + 1


def _formula(config: WorkloadConfig) -> str:
    if config.topology == "LI":
        return "(width - 1) * (depth - 1) + 1"
    return "(width * (width - 1) / 2) * (depth - 1) + 1"


def _count_block(value: int) -> dict[str, int]:
    return {
        "atomic_event_deliveries": value,
        "event_values": value,
        "external_transitions": value,
        "internal_transitions": value,
        "hla_attribute_updates": value,
        "hla_interactions": value,
        "hla_deliveries": value * 2,
    }


def generate_workload(config: WorkloadConfig | None = None) -> dict[str, Any]:
    """Return a complete workload document including its canonical identity."""

    config = config or WorkloadConfig()
    nodes = _atomic_nodes(config)
    couplings = _directed_couplings(config, nodes)
    events = _injected_events(config)
    per_injection = _event_count_per_injection(config)
    total = per_injection * config.external_event_count

    workload: dict[str, Any] = {
        "schema_version": SCHEMA_VERSION,
        "generator": {
            "name": "gorti-devstone-hla-workload",
            "version": GENERATOR_VERSION,
        },
        "benchmark": {
            "requested_spelling": "DEVSOne",
            "resolved_name": "DEVStone",
            "topology_definition": config.topology,
            "profile": "DEVStone-HLA workload mapping",
            "scope": (
                "HLA application-traffic workload; not a DEVS simulator-kernel "
                "benchmark result"
            ),
        },
        "configuration": {
            "topology": config.topology,
            "width": config.width,
            "depth": config.depth,
            "external_event_count": config.external_event_count,
            "seed": config.seed,
            "synthetic_transition_delay": {
                "external_seconds": 0,
                "internal_seconds": 0,
            },
        },
        "hla_mapping": {
            "profile_version": "devstone-hla/v1",
            "delivery_plan_format": "DVSHLA1",
            "participants": ["producer", "consumer"],
            "atomic_node_mapping": "logical workload entity",
            "delivery_mapping": (
                "one receive-order object update and one receive-order interaction "
                "per atomic event-value delivery"
            ),
            "projection_deliveries_per_atomic_event_value": 2,
            "payload_bytes_per_channel": 8,
            "object_attribute_fields": [
                "delivery_ordinal",
                "payload",
            ],
            "interaction_fields": [
                "injected_event_id",
                "target_atomic_node_id",
                "delivery_ordinal",
                "payload",
            ],
            "materialization_order": [
                "injected_event.sequence",
                "atomic_nodes document order",
                "occurrence_ordinal",
            ],
        },
        "event_semantics": {
            "injection_interval_logical_time": 1,
            "input_fanout": "each atomic node receives each injected event once",
            "li_forwarding": "no internal atomic-to-atomic coupling",
            "hi_forwarding": (
                "each regular-level atomic forwards every received event value "
                "to its next atomic"
            ),
            "transition_delay_seconds": 0,
        },
        "atomic_nodes": nodes,
        "directed_couplings": couplings,
        "injected_events": events,
        "expected_counts": {
            "atomic_nodes": len(nodes),
            "directed_couplings": len(couplings),
            "formula_per_injected_event": _formula(config),
            "per_injected_event": _count_block(per_injection),
            "total": {
                "injected_external_events": config.external_event_count,
                **_count_block(total),
            },
        },
    }

    derived = derive_event_deliveries_per_injection(workload)
    if derived != per_injection:
        raise AssertionError(
            f"coupling graph yields {derived} deliveries; formula yields {per_injection}"
        )

    workload["topology_identity"] = {
        "algorithm": "sha256",
        "identity_version": TOPOLOGY_IDENTITY_VERSION,
        "canonicalization": CANONICALIZATION,
        "scope": TOPOLOGY_IDENTITY_SCOPE,
        "digest": canonical_topology_identity(workload),
    }
    workload["identity"] = {
        "algorithm": "sha256",
        "canonicalization": CANONICALIZATION,
        "scope": "document excluding the identity member",
        "digest": canonical_identity(workload),
    }
    return workload


def derive_event_deliveries_per_injection(workload: Mapping[str, Any]) -> int:
    """Evaluate one event through the flattened acyclic coupling graph."""

    multiplicity = {node["id"]: 0 for node in workload["atomic_nodes"]}
    internal: list[Mapping[str, Any]] = []
    for coupling in workload["directed_couplings"]:
        target = coupling["target"]["node"]
        if target not in multiplicity:
            raise ValueError(f"coupling targets unknown atomic node {target!r}")
        if coupling["kind"] == "external_input":
            multiplicity[target] += 1
        elif coupling["kind"] == "internal":
            internal.append(coupling)
        else:
            raise ValueError(f"unknown coupling kind {coupling['kind']!r}")

    for coupling in internal:
        source = coupling["source"]["node"]
        target = coupling["target"]["node"]
        if source not in multiplicity:
            raise ValueError(f"coupling uses unknown atomic node {source!r}")
        multiplicity[target] += multiplicity[source]
    return sum(multiplicity.values())


def _compact_json_bytes(value: Mapping[str, Any]) -> bytes:
    return json.dumps(
        value,
        ensure_ascii=True,
        allow_nan=False,
        sort_keys=True,
        separators=(",", ":"),
    ).encode("utf-8")


def _topology_identity_document(workload: Mapping[str, Any]) -> dict[str, Any]:
    """Return the exact seed-independent document covered by topology identity."""

    configuration = workload["configuration"]
    return {
        "identity_version": TOPOLOGY_IDENTITY_VERSION,
        "topology": configuration["topology"],
        "width": configuration["width"],
        "depth": configuration["depth"],
        "external_event_count": configuration["external_event_count"],
        "synthetic_transition_delay": configuration[
            "synthetic_transition_delay"
        ],
        "atomic_nodes": workload["atomic_nodes"],
        "directed_couplings": workload["directed_couplings"],
        "hla_mapping": workload["hla_mapping"],
    }


def canonical_topology_identity(workload: Mapping[str, Any]) -> str:
    """Compute the immutable topology SHA-256, excluding seed and payloads."""

    return hashlib.sha256(
        _compact_json_bytes(_topology_identity_document(workload))
    ).hexdigest()


def validate_topology_identity(workload: Mapping[str, Any]) -> bool:
    identity = workload.get("topology_identity")
    return bool(
        isinstance(identity, Mapping)
        and identity.get("algorithm") == "sha256"
        and identity.get("identity_version") == TOPOLOGY_IDENTITY_VERSION
        and identity.get("canonicalization") == CANONICALIZATION
        and identity.get("scope") == TOPOLOGY_IDENTITY_SCOPE
        and identity.get("digest") == canonical_topology_identity(workload)
    )


def _canonical_bytes(workload: Mapping[str, Any]) -> bytes:
    unsigned = {key: value for key, value in workload.items() if key != "identity"}
    return _compact_json_bytes(unsigned)


def canonical_identity(workload: Mapping[str, Any]) -> str:
    """Compute the workload SHA-256 over its documented canonical scope."""

    return hashlib.sha256(_canonical_bytes(workload)).hexdigest()


def validate_identity(workload: Mapping[str, Any]) -> bool:
    identity = workload.get("identity")
    return bool(
        isinstance(identity, Mapping)
        and identity.get("algorithm") == "sha256"
        and identity.get("canonicalization") == CANONICALIZATION
        and identity.get("digest") == canonical_identity(workload)
        and validate_topology_identity(workload)
    )


def write_workload(path: str | Path, workload: Mapping[str, Any]) -> Path:
    """Write stable, human-readable JSON and return its resolved path."""

    destination = Path(path)
    destination.parent.mkdir(parents=True, exist_ok=True)
    destination.write_text(
        json.dumps(workload, ensure_ascii=True, allow_nan=False, indent=2) + "\n",
        encoding="utf-8",
        newline="\n",
    )
    return destination.resolve()
