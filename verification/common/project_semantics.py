from __future__ import annotations

import argparse
import hashlib
import json
from pathlib import Path
from typing import Any


def _read(path: Path) -> list[dict[str, Any]]:
    return [json.loads(line) for line in path.read_text(encoding="utf-8").splitlines() if line]


def _payload(seed: str, channel: str, index: int) -> str:
    return hashlib.sha256(f"{seed}:{channel}:{index}".encode()).hexdigest()[:16]


def _select(
    records: list[dict[str, Any]], event: str, actor: str | None = None
) -> list[dict[str, Any]]:
    return [
        record
        for record in records
        if record.get("event") == event
        and (actor is None or record.get("actor") == actor)
    ]


def _require(condition: bool, message: str) -> None:
    if not condition:
        raise ValueError(message)


def _indexed(
    records: list[dict[str, Any]], event: str, actor: str, count: int
) -> dict[int, dict[str, Any]]:
    result = {
        int(record["data"]["index"]): record["data"]
        for record in _select(records, event, actor)
    }
    _require(set(result) == set(range(count)), f"{actor}.{event}: incomplete indices")
    return result


def _validate_pitch(
    records: list[dict[str, Any]], seed: str, count: int
) -> tuple[list[str], list[str]]:
    for actor in ("publisher", "subscriber"):
        passed = [
            record
            for record in _select(records, "phase", actor)
            if record["data"].get("phase") == "reflect"
            and record["data"].get("result") == "pass"
        ]
        _require(bool(passed), f"Pitch {actor} did not reflect PASS")

    for event in ("object_published", "interaction_published"):
        _require(bool(_select(records, event, "publisher")), f"Pitch missing {event}")
    for event in ("object_subscribed", "interaction_subscribed"):
        _require(bool(_select(records, event, "subscriber")), f"Pitch missing {event}")
    _require(bool(_select(records, "object_name_reserved", "publisher")), "Pitch name not reserved")
    _require(
        bool(_select(records, "object_registered", "publisher")),
        "Pitch object not registered",
    )
    _require(
        bool(_select(records, "object_discovered", "subscriber")),
        "Pitch object not discovered",
    )
    _require(bool(_select(records, "object_deleted", "publisher")), "Pitch object not deleted")
    _require(bool(_select(records, "object_removed", "subscriber")), "Pitch object not removed")

    sent_attributes = _indexed(records, "attributes_updated", "publisher", count)
    got_attributes = _indexed(records, "attributes_reflected", "subscriber", count)
    sent_interactions = _indexed(records, "interaction_sent", "publisher", count)
    got_interactions = _indexed(records, "interaction_received", "subscriber", count)

    for label in ("VERIFY_READY", "VERIFY_DONE"):
        for actor in ("publisher", "subscriber"):
            synchronized = [
                item
                for item in _select(records, "federation_synchronized", actor)
                if item["data"].get("label") == label
            ]
            _require(bool(synchronized), f"Pitch {actor} missing synchronized {label}")

    expected_times = set(range(1, count + 2))
    for actor in ("publisher", "subscriber"):
        grants = {
            int(item["data"]["logical_time"])
            for item in _select(records, "time_advance_granted", actor)
        }
        _require(grants == expected_times, f"Pitch {actor} grant set differs: {grants}")

    attributes = []
    interactions = []
    for index in range(count):
        expected_attribute = _payload(seed, "attribute", index)
        expected_interaction = _payload(seed, "interaction", index)
        for side in (sent_attributes[index], got_attributes[index]):
            _require(side["payload"] == expected_attribute, f"Pitch attribute {index} differs")
            _require(int(side["logical_time"]) == index + 1, "Pitch attribute time differs")
        for side in (sent_interactions[index], got_interactions[index]):
            _require(side["payload"] == expected_interaction, f"Pitch interaction {index} differs")
            _require(int(side["logical_time"]) == index + 1, "Pitch interaction time differs")
        attributes.append(expected_attribute)
        interactions.append(expected_interaction)
    return attributes, interactions


def _validate_gorti(
    records: list[dict[str, Any]], seed: str, count: int
) -> tuple[list[str], list[str]]:
    for service in ("FM", "DM", "OM", "TM"):
        passed = [
            record
            for record in _select(records, "service_result", "verifier")
            if record["service"] == service and record["data"].get("status") == "pass"
        ]
        _require(bool(passed), f"gorti {service} did not reflect PASS")

    required = (
        "publishObjectClassAttributes",
        "publishInteractionClass",
        "subscribeObjectClassAttributes",
        "subscribeInteractionClass",
        "reserveObjectInstanceName",
        "registerObjectInstance",
        "discoverObjectInstance",
        "deleteObjectInstance",
        "removeObjectInstance",
    )
    for event in required:
        _require(bool(_select(records, event)), f"gorti missing {event}")

    sent_attributes = _indexed(records, "updateAttributeValues", "producer", count)
    got_attributes = _indexed(records, "reflectAttributeValues", "consumer", count)
    sent_interactions = _indexed(records, "sendInteraction", "producer", count)
    got_interactions = _indexed(records, "receiveInteraction", "consumer", count)

    for label in ("VERIFY_READY", "VERIFY_DONE"):
        for actor in ("producer", "consumer"):
            synchronized = [
                item
                for item in _select(records, "federationSynchronized", actor)
                if item["data"].get("label") == label
            ]
            _require(bool(synchronized), f"gorti {actor} missing synchronized {label}")

    expected_times = set(range(1, count + 2))
    for actor in ("producer", "consumer"):
        grants = {
            int(item["data"]["logical_time"])
            for item in _select(records, "timeAdvanceGrant", actor)
            if item["data"].get("phase") == "do"
        }
        _require(grants == expected_times, f"gorti {actor} grant set differs: {grants}")

    attributes = []
    interactions = []
    for index in range(count):
        expected_attribute = _payload(seed, "attribute", index)
        expected_interaction = _payload(seed, "interaction", index)
        for side in (sent_attributes[index], got_attributes[index]):
            _require(side["payload"] == expected_attribute, f"gorti attribute {index} differs")
            _require(int(side["logical_time"]) == index + 1, "gorti attribute time differs")
        for side in (sent_interactions[index], got_interactions[index]):
            _require(side["payload"] == expected_interaction, f"gorti interaction {index} differs")
            _require(int(side["logical_time"]) == index + 1, "gorti interaction time differs")
        attributes.append(expected_attribute)
        interactions.append(expected_interaction)
    return attributes, interactions


def project(
    records: list[dict[str, Any]], implementation: str, seed: str, count: int
) -> list[dict[str, Any]]:
    if implementation == "pitch":
        attributes, interactions = _validate_pitch(records, seed, count)
    else:
        attributes, interactions = _validate_gorti(records, seed, count)

    rows = [
        (
            "FM",
            "federation_lifecycle_verified",
            {"federates": 2, "sync_labels": ["VERIFY_READY", "VERIFY_DONE"]},
        ),
        (
            "DM",
            "declarations_verified",
            {
                "object_class": "VerifierEntity",
                "interaction_class": "VerifierMessage",
            },
        ),
        (
            "OM",
            "timestamped_workload_verified",
            {
                "count": count,
                "named_instance": True,
                "removed": True,
                "attribute_payloads": attributes,
                "interaction_payloads": interactions,
            },
        ),
        (
            "TM",
            "time_management_verified",
            {
                "lookahead": 1,
                "order": "TimeStamp",
                "grants": list(range(1, count + 2)),
            },
        ),
    ]
    return [
        {
            "kind": "semantic",
            "seq": index,
            "service": service,
            "event": event,
            "actor": "verifier",
            "data": data,
        }
        for index, (service, event, data) in enumerate(rows)
    ]


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--implementation", choices=("pitch", "gorti"), required=True)
    parser.add_argument("--seed", required=True)
    parser.add_argument("--count", type=int, required=True)
    parser.add_argument("input", type=Path)
    parser.add_argument("output", type=Path)
    args = parser.parse_args()
    rows = project(_read(args.input), args.implementation, args.seed, args.count)
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(
        "".join(json.dumps(row, sort_keys=True, separators=(",", ":")) + "\n" for row in rows),
        encoding="utf-8",
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
