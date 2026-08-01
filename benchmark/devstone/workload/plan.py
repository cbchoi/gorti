"""Materialize and validate deterministic DEVStone-HLA binary delivery plans.

The format is deliberately small and language-neutral. Java and Go clients can
read the same fixed-width records without implementing DEVStone graph logic.
"""

from __future__ import annotations

import argparse
import hashlib
import heapq
import json
import os
import struct
from collections.abc import Iterator, Mapping, Sequence
from dataclasses import dataclass
from pathlib import Path
from typing import Any

try:
    from .model import validate_topology_identity
except ImportError:  # Direct execution: python path/to/plan.py
    from model import (  # type: ignore[no-redef]
        validate_topology_identity,
    )


MAGIC = b"DVSHLA1\0"
PLAN_VERSION = 1
PAYLOAD_DOMAIN = b"gorti.devstone-hla.payload\0"
HEADER_STRUCT = struct.Struct(">8sIQ32s")
RECORD_STRUCT = struct.Struct(">IIII8s8s")
HEADER_SIZE = HEADER_STRUCT.size
RECORD_SIZE = RECORD_STRUCT.size
UINT32_MAX = (1 << 32) - 1
UINT64_MAX = (1 << 64) - 1


class PlanValidationError(ValueError):
    """Raised when a workload or binary delivery plan violates the contract."""


@dataclass(frozen=True)
class DeliveryRecord:
    delivery_index: int
    injected_event_sequence: int
    target_node_ordinal: int
    occurrence_ordinal: int
    attribute_payload: bytes
    interaction_payload: bytes

    def to_bytes(self) -> bytes:
        _require_uint("delivery_index", self.delivery_index, 32)
        _require_uint("injected_event_sequence", self.injected_event_sequence, 32)
        _require_uint("target_node_ordinal", self.target_node_ordinal, 32)
        _require_uint("occurrence_ordinal", self.occurrence_ordinal, 32)
        _require_payload("attribute_payload", self.attribute_payload)
        _require_payload("interaction_payload", self.interaction_payload)
        return RECORD_STRUCT.pack(
            self.delivery_index,
            self.injected_event_sequence,
            self.target_node_ordinal,
            self.occurrence_ordinal,
            self.attribute_payload,
            self.interaction_payload,
        )


@dataclass(frozen=True)
class DeliveryPlan:
    seed: int
    topology_identity: bytes
    records: tuple[DeliveryRecord, ...]

    @property
    def record_count(self) -> int:
        return len(self.records)

    def to_bytes(self) -> bytes:
        _require_uint("record_count", self.record_count, 32)
        _require_uint("seed", self.seed, 64)
        if len(self.topology_identity) != 32:
            raise PlanValidationError("topology_identity must contain 32 bytes")
        header = HEADER_STRUCT.pack(
            MAGIC,
            self.record_count,
            self.seed,
            self.topology_identity,
        )
        return header + b"".join(record.to_bytes() for record in self.records)


@dataclass(frozen=True)
class _DeliveryCoordinate:
    delivery_index: int
    injected_event_sequence: int
    target_node_ordinal: int
    target_node_id: str
    occurrence_ordinal: int


def _require_uint(name: str, value: object, bits: int) -> int:
    maximum = (1 << bits) - 1
    if isinstance(value, bool) or not isinstance(value, int):
        raise PlanValidationError(f"{name} must be an unsigned {bits}-bit integer")
    if value < 0 or value > maximum:
        raise PlanValidationError(f"{name} must be between 0 and {maximum}")
    return value


def _require_payload(name: str, value: object) -> bytes:
    if not isinstance(value, bytes) or len(value) != 8:
        raise PlanValidationError(f"{name} must contain exactly 8 bytes")
    return value


def _topology_identity_bytes(workload: Mapping[str, Any]) -> bytes:
    if not validate_topology_identity(workload):
        raise PlanValidationError("workload topology_identity does not validate")
    digest = workload["topology_identity"]["digest"]
    try:
        value = bytes.fromhex(digest)
    except (TypeError, ValueError) as error:
        raise PlanValidationError("topology_identity digest is not hexadecimal") from error
    if len(value) != 32:
        raise PlanValidationError("topology_identity digest must contain 32 bytes")
    return value


def load_workload(path: str | Path) -> dict[str, Any]:
    """Load a workload JSON object without accepting non-object roots."""

    source = Path(path)
    try:
        value = json.loads(source.read_text(encoding="utf-8"))
    except (OSError, UnicodeError, json.JSONDecodeError) as error:
        raise PlanValidationError(f"cannot read workload {source}: {error}") from error
    if not isinstance(value, dict):
        raise PlanValidationError("workload JSON root must be an object")
    return value


def graph_delivery_multiplicities(workload: Mapping[str, Any]) -> tuple[int, ...]:
    """Propagate one injected event through the flattened acyclic graph.

    Every external-input edge contributes one arrival at its target. Each
    arrival is then forwarded over every ordered internal coupling. Kahn's
    algorithm makes the calculation independent of document topological order
    while rejecting cycles.
    """

    raw_nodes = workload.get("atomic_nodes")
    raw_couplings = workload.get("directed_couplings")
    if not isinstance(raw_nodes, Sequence) or isinstance(raw_nodes, (str, bytes)):
        raise PlanValidationError("atomic_nodes must be an array")
    if not isinstance(raw_couplings, Sequence) or isinstance(
        raw_couplings, (str, bytes)
    ):
        raise PlanValidationError("directed_couplings must be an array")

    node_ids: list[str] = []
    ordinal_by_id: dict[str, int] = {}
    for ordinal, node in enumerate(raw_nodes):
        if not isinstance(node, Mapping) or not isinstance(node.get("id"), str):
            raise PlanValidationError(f"atomic_nodes[{ordinal}] has no string id")
        node_id = node["id"]
        if not node_id:
            raise PlanValidationError(f"atomic_nodes[{ordinal}] has an empty id")
        if node_id in ordinal_by_id:
            raise PlanValidationError(f"duplicate atomic node id {node_id!r}")
        _require_uint("target_node_ordinal", ordinal, 32)
        ordinal_by_id[node_id] = ordinal
        node_ids.append(node_id)
    if not node_ids:
        raise PlanValidationError("atomic_nodes must not be empty")

    multiplicities = [0] * len(node_ids)
    indegree = [0] * len(node_ids)
    outgoing: list[list[int]] = [[] for _ in node_ids]
    for coupling_index, coupling in enumerate(raw_couplings):
        if not isinstance(coupling, Mapping):
            raise PlanValidationError(
                f"directed_couplings[{coupling_index}] must be an object"
            )
        kind = coupling.get("kind")
        source = coupling.get("source")
        target = coupling.get("target")
        if not isinstance(source, Mapping) or not isinstance(target, Mapping):
            raise PlanValidationError(
                f"directed_couplings[{coupling_index}] has invalid endpoints"
            )
        source_id = source.get("node")
        target_id = target.get("node")
        if not isinstance(target_id, str) or target_id not in ordinal_by_id:
            raise PlanValidationError(
                f"coupling targets unknown atomic node {target_id!r}"
            )
        target_ordinal = ordinal_by_id[target_id]
        if kind == "external_input":
            if source_id != "__injection__":
                raise PlanValidationError(
                    "external_input coupling source must be '__injection__'"
                )
            multiplicities[target_ordinal] += 1
        elif kind == "internal":
            if not isinstance(source_id, str) or source_id not in ordinal_by_id:
                raise PlanValidationError(
                    f"coupling uses unknown atomic node {source_id!r}"
                )
            source_ordinal = ordinal_by_id[source_id]
            outgoing[source_ordinal].append(target_ordinal)
            indegree[target_ordinal] += 1
        else:
            raise PlanValidationError(f"unknown coupling kind {kind!r}")

    ready = [ordinal for ordinal, degree in enumerate(indegree) if degree == 0]
    heapq.heapify(ready)
    visited = 0
    while ready:
        source_ordinal = heapq.heappop(ready)
        visited += 1
        source_multiplicity = multiplicities[source_ordinal]
        for target_ordinal in outgoing[source_ordinal]:
            multiplicities[target_ordinal] += source_multiplicity
            indegree[target_ordinal] -= 1
            if indegree[target_ordinal] == 0:
                heapq.heappush(ready, target_ordinal)

    if visited != len(node_ids):
        raise PlanValidationError("internal directed_couplings contain a cycle")
    if any(value == 0 for value in multiplicities):
        raise PlanValidationError("every atomic node must receive the injected event")
    return tuple(multiplicities)


def _delivery_coordinates(
    workload: Mapping[str, Any],
) -> Iterator[_DeliveryCoordinate]:
    nodes = workload["atomic_nodes"]
    multiplicities = graph_delivery_multiplicities(workload)
    try:
        event_count = workload["configuration"]["external_event_count"]
    except (KeyError, TypeError) as error:
        raise PlanValidationError(
            "configuration.external_event_count is required"
        ) from error
    _require_uint("external_event_count", event_count, 32)
    if event_count == 0:
        raise PlanValidationError("external_event_count must be at least one")

    total = sum(multiplicities) * event_count
    _require_uint("record_count", total, 32)
    try:
        declared_total = workload["expected_counts"]["total"][
            "atomic_event_deliveries"
        ]
    except (KeyError, TypeError) as error:
        raise PlanValidationError(
            "expected_counts.total.atomic_event_deliveries is required"
        ) from error
    if declared_total != total:
        raise PlanValidationError(
            "graph-derived record count does not match "
            "expected_counts.total.atomic_event_deliveries"
        )

    delivery_index = 0
    for event_sequence in range(1, event_count + 1):
        for node_ordinal, multiplicity in enumerate(multiplicities):
            node_id = nodes[node_ordinal]["id"]
            for occurrence_ordinal in range(multiplicity):
                yield _DeliveryCoordinate(
                    delivery_index=delivery_index,
                    injected_event_sequence=event_sequence,
                    target_node_ordinal=node_ordinal,
                    target_node_id=node_id,
                    occurrence_ordinal=occurrence_ordinal,
                )
                delivery_index += 1


def payload_material(
    *,
    channel: str,
    seed: int,
    injected_event_sequence: int,
    target_node_id: str,
    target_node_ordinal: int,
    occurrence_ordinal: int,
    delivery_index: int,
    topology_identity: bytes,
) -> bytes:
    """Return the exact domain-separated bytes hashed for one payload."""

    if channel not in ("attribute", "interaction"):
        raise PlanValidationError("channel must be 'attribute' or 'interaction'")
    _require_uint("seed", seed, 64)
    _require_uint("injected_event_sequence", injected_event_sequence, 32)
    _require_uint("target_node_ordinal", target_node_ordinal, 32)
    _require_uint("occurrence_ordinal", occurrence_ordinal, 32)
    _require_uint("delivery_index", delivery_index, 32)
    if not isinstance(target_node_id, str) or not target_node_id:
        raise PlanValidationError("target_node_id must be a non-empty string")
    node_id = target_node_id.encode("utf-8")
    _require_uint("target_node_id byte length", len(node_id), 32)
    if not isinstance(topology_identity, bytes) or len(topology_identity) != 32:
        raise PlanValidationError("topology_identity must contain 32 bytes")

    return b"".join(
        (
            PAYLOAD_DOMAIN,
            MAGIC,
            channel.encode("ascii"),
            b"\0",
            struct.pack(">Q", seed),
            struct.pack(">I", injected_event_sequence),
            struct.pack(">I", len(node_id)),
            node_id,
            struct.pack(">I", target_node_ordinal),
            struct.pack(">I", occurrence_ordinal),
            struct.pack(">I", delivery_index),
            topology_identity,
        )
    )


def derive_payload(**fields: Any) -> bytes:
    """Return the first eight SHA-256 bytes for one plan channel."""

    return hashlib.sha256(payload_material(**fields)).digest()[:8]


def materialize_plan(workload: Mapping[str, Any], seed: int) -> DeliveryPlan:
    """Traverse the workload and materialize one runtime-seeded plan."""

    _require_uint("seed", seed, 64)
    topology_identity = _topology_identity_bytes(workload)
    records: list[DeliveryRecord] = []
    for coordinate in _delivery_coordinates(workload):
        common = {
            "seed": seed,
            "injected_event_sequence": coordinate.injected_event_sequence,
            "target_node_id": coordinate.target_node_id,
            "target_node_ordinal": coordinate.target_node_ordinal,
            "occurrence_ordinal": coordinate.occurrence_ordinal,
            "delivery_index": coordinate.delivery_index,
            "topology_identity": topology_identity,
        }
        records.append(
            DeliveryRecord(
                delivery_index=coordinate.delivery_index,
                injected_event_sequence=coordinate.injected_event_sequence,
                target_node_ordinal=coordinate.target_node_ordinal,
                occurrence_ordinal=coordinate.occurrence_ordinal,
                attribute_payload=derive_payload(channel="attribute", **common),
                interaction_payload=derive_payload(channel="interaction", **common),
            )
        )
    return DeliveryPlan(
        seed=seed,
        topology_identity=topology_identity,
        records=tuple(records),
    )


def _read_plan_bytes(source: bytes | bytearray | memoryview | str | Path) -> bytes:
    if isinstance(source, bytes):
        return source
    if isinstance(source, (bytearray, memoryview)):
        return bytes(source)
    try:
        return Path(source).read_bytes()
    except OSError as error:
        raise PlanValidationError(f"cannot read plan {source}: {error}") from error


def parse_plan(
    source: bytes | bytearray | memoryview | str | Path,
) -> DeliveryPlan:
    """Parse framing and fixed-width records; semantic checks are separate."""

    data = _read_plan_bytes(source)
    if len(data) < HEADER_SIZE:
        raise PlanValidationError(
            f"truncated plan header: expected {HEADER_SIZE} bytes, got {len(data)}"
        )
    magic, record_count, seed, topology_identity = HEADER_STRUCT.unpack_from(data)
    if magic != MAGIC:
        raise PlanValidationError(f"invalid plan magic {magic!r}")
    expected_size = HEADER_SIZE + record_count * RECORD_SIZE
    if len(data) != expected_size:
        raise PlanValidationError(
            f"invalid plan size: header declares {expected_size} bytes, got {len(data)}"
        )

    records: list[DeliveryRecord] = []
    offset = HEADER_SIZE
    for _ in range(record_count):
        fields = RECORD_STRUCT.unpack_from(data, offset)
        records.append(DeliveryRecord(*fields))
        offset += RECORD_SIZE
    return DeliveryPlan(seed, topology_identity, tuple(records))


def validate_plan(
    source: DeliveryPlan | bytes | bytearray | memoryview | str | Path,
    workload: Mapping[str, Any],
    *,
    expected_seed: int | None = None,
) -> DeliveryPlan:
    """Validate topology, ordering, graph count, metadata, and both payloads."""

    plan = source if isinstance(source, DeliveryPlan) else parse_plan(source)
    topology_identity = _topology_identity_bytes(workload)
    if plan.topology_identity != topology_identity:
        raise PlanValidationError("plan topology_identity does not match workload")
    if expected_seed is not None:
        _require_uint("expected_seed", expected_seed, 64)
        if plan.seed != expected_seed:
            raise PlanValidationError(
                f"plan seed {plan.seed} does not match expected seed {expected_seed}"
            )

    coordinates = _delivery_coordinates(workload)
    for record_index, (actual, coordinate) in enumerate(
        zip(plan.records, coordinates, strict=False)
    ):
        common = {
            "seed": plan.seed,
            "injected_event_sequence": coordinate.injected_event_sequence,
            "target_node_id": coordinate.target_node_id,
            "target_node_ordinal": coordinate.target_node_ordinal,
            "occurrence_ordinal": coordinate.occurrence_ordinal,
            "delivery_index": coordinate.delivery_index,
            "topology_identity": topology_identity,
        }
        expected = DeliveryRecord(
            delivery_index=coordinate.delivery_index,
            injected_event_sequence=coordinate.injected_event_sequence,
            target_node_ordinal=coordinate.target_node_ordinal,
            occurrence_ordinal=coordinate.occurrence_ordinal,
            attribute_payload=derive_payload(channel="attribute", **common),
            interaction_payload=derive_payload(channel="interaction", **common),
        )
        if actual != expected:
            raise PlanValidationError(
                f"record {record_index} does not match the workload delivery plan"
            )

    expected_count = workload["expected_counts"]["total"][
        "atomic_event_deliveries"
    ]
    if plan.record_count != expected_count:
        raise PlanValidationError(
            f"plan has {plan.record_count} records; workload requires {expected_count}"
        )
    return plan


def plan_sha256(source: bytes | bytearray | memoryview | DeliveryPlan) -> str:
    """Return SHA-256 for serialized plan bytes."""

    data = source.to_bytes() if isinstance(source, DeliveryPlan) else bytes(source)
    return hashlib.sha256(data).hexdigest()


def file_sha256(path: str | Path) -> str:
    """Return a file SHA-256 without loading the entire file into memory."""

    digest = hashlib.sha256()
    with Path(path).open("rb") as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(block)
    return digest.hexdigest()


def write_plan(path: str | Path, plan: DeliveryPlan) -> Path:
    """Atomically write a generated plan and return its resolved path."""

    destination = Path(path)
    destination.parent.mkdir(parents=True, exist_ok=True)
    temporary = destination.with_name(f".{destination.name}.{os.getpid()}.tmp")
    try:
        temporary.write_bytes(plan.to_bytes())
        os.replace(temporary, destination)
    finally:
        temporary.unlink(missing_ok=True)
    return destination.resolve()


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description="Materialize or validate a deterministic DEVStone-HLA plan."
    )
    commands = parser.add_subparsers(dest="command", required=True)

    materialize = commands.add_parser("materialize", help="write one binary plan")
    materialize.add_argument("--workload", type=Path, required=True)
    materialize.add_argument("--seed", type=int, required=True)
    materialize.add_argument("--output", type=Path, required=True)

    validate = commands.add_parser("validate", help="validate one binary plan")
    validate.add_argument("--workload", type=Path, required=True)
    validate.add_argument("--input", type=Path, required=True)
    validate.add_argument("--seed", type=int)
    return parser


def main(argv: list[str] | None = None) -> int:
    args = _parser().parse_args(argv)
    workload = load_workload(args.workload)
    try:
        if args.command == "materialize":
            plan = materialize_plan(workload, args.seed)
            destination = write_plan(args.output, plan)
            print(
                f"wrote {destination} records={plan.record_count} "
                f"sha256={file_sha256(destination)}"
            )
        else:
            plan = validate_plan(
                args.input,
                workload,
                expected_seed=args.seed,
            )
            print(
                f"valid {Path(args.input).resolve()} records={plan.record_count} "
                f"seed={plan.seed} sha256={file_sha256(args.input)}"
            )
    except PlanValidationError as error:
        _parser().error(str(error))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
