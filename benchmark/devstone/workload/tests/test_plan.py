from __future__ import annotations

import hashlib
import json
import subprocess
import sys
import tempfile
import unittest
from copy import deepcopy
from pathlib import Path

WORKLOAD_DIR = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(WORKLOAD_DIR))

from model import (  # noqa: E402
    WorkloadConfig,
    canonical_topology_identity,
    generate_workload,
)
from plan import (  # noqa: E402
    HEADER_SIZE,
    MAGIC,
    RECORD_SIZE,
    PlanValidationError,
    file_sha256,
    graph_delivery_multiplicities,
    materialize_plan,
    parse_plan,
    plan_sha256,
    validate_plan,
    write_plan,
)


class DeliveryPlanTests(unittest.TestCase):
    def test_li_graph_traversal_emits_atomic_node_order(self) -> None:
        workload = generate_workload(
            WorkloadConfig(
                topology="LI", width=4, depth=3, external_event_count=2, seed=1
            )
        )
        self.assertEqual(graph_delivery_multiplicities(workload), (1,) * 7)

        plan = materialize_plan(workload, seed=1516)
        self.assertEqual(plan.record_count, 14)
        self.assertEqual(
            [record.injected_event_sequence for record in plan.records],
            [1] * 7 + [2] * 7,
        )
        self.assertEqual(
            [record.target_node_ordinal for record in plan.records],
            list(range(7)) * 2,
        )
        self.assertTrue(
            all(record.occurrence_ordinal == 0 for record in plan.records)
        )
        self.assertEqual(validate_plan(plan.to_bytes(), workload), plan)

    def test_hi_graph_traversal_propagates_chain_multiplicity(self) -> None:
        workload = generate_workload(
            WorkloadConfig(
                topology="HI", width=4, depth=3, external_event_count=1, seed=2
            )
        )
        self.assertEqual(
            graph_delivery_multiplicities(workload),
            (1, 2, 3, 1, 2, 3, 1),
        )

        plan = materialize_plan(workload, seed=1516)
        self.assertEqual(plan.record_count, 13)
        self.assertEqual(
            [record.target_node_ordinal for record in plan.records],
            [0, 1, 1, 2, 2, 2, 3, 4, 4, 5, 5, 5, 6],
        )
        self.assertEqual(
            [record.occurrence_ordinal for record in plan.records],
            [0, 0, 1, 0, 1, 2, 0, 0, 1, 0, 1, 2, 0],
        )

    def test_fixed_binary_layout_matches_cross_language_golden_vector(self) -> None:
        vector = json.loads(
            (WORKLOAD_DIR / "tests" / "golden-vector-v1.json").read_text(
                encoding="utf-8"
            )
        )
        config = vector["workload"]
        workload = generate_workload(
            WorkloadConfig(**config, seed=0)
        )
        plan = materialize_plan(workload, vector["runtime_seed_decimal"])
        encoded = plan.to_bytes()

        self.assertEqual(MAGIC, b"DVSHLA1\0")
        self.assertEqual(HEADER_SIZE, 52)
        self.assertEqual(RECORD_SIZE, 32)
        self.assertEqual(len(encoded), vector["plan_size_bytes"])
        self.assertEqual(
            workload["topology_identity"]["digest"],
            vector["topology_identity_sha256"],
        )
        self.assertEqual(encoded[:HEADER_SIZE].hex(), vector["header_hex"])
        self.assertEqual(encoded[HEADER_SIZE:].hex(), vector["record_hex"])
        self.assertEqual(
            plan.records[0].attribute_payload.hex(),
            vector["attribute_payload_hex"],
        )
        self.assertEqual(
            plan.records[0].interaction_payload.hex(),
            vector["interaction_payload_hex"],
        )
        self.assertEqual(plan_sha256(encoded), vector["plan_sha256"])

    def test_runtime_seed_changes_payload_not_topology_or_coordinates(self) -> None:
        workload = generate_workload(
            WorkloadConfig(
                topology="HI", width=3, depth=2, external_event_count=2, seed=8
            )
        )
        first = materialize_plan(workload, 11)
        second = materialize_plan(workload, 12)

        self.assertEqual(first.topology_identity, second.topology_identity)
        first_coordinates = [
            (
                item.delivery_index,
                item.injected_event_sequence,
                item.target_node_ordinal,
                item.occurrence_ordinal,
            )
            for item in first.records
        ]
        second_coordinates = [
            (
                item.delivery_index,
                item.injected_event_sequence,
                item.target_node_ordinal,
                item.occurrence_ordinal,
            )
            for item in second.records
        ]
        self.assertEqual(first_coordinates, second_coordinates)
        self.assertNotEqual(first.records[0].attribute_payload, second.records[0].attribute_payload)
        self.assertNotEqual(plan_sha256(first), plan_sha256(second))

    def test_topology_identity_is_seed_independent_and_order_sensitive(self) -> None:
        first = generate_workload(
            WorkloadConfig(
                topology="HI", width=4, depth=3, external_event_count=2, seed=10
            )
        )
        second = generate_workload(
            WorkloadConfig(
                topology="HI", width=4, depth=3, external_event_count=2, seed=20
            )
        )
        self.assertEqual(
            first["topology_identity"]["digest"],
            second["topology_identity"]["digest"],
        )

        reordered = deepcopy(first)
        reordered["atomic_nodes"][0], reordered["atomic_nodes"][1] = (
            reordered["atomic_nodes"][1],
            reordered["atomic_nodes"][0],
        )
        self.assertNotEqual(
            canonical_topology_identity(reordered),
            first["topology_identity"]["digest"],
        )

    def test_corruption_truncation_and_trailing_bytes_are_rejected(self) -> None:
        workload = generate_workload(
            WorkloadConfig(
                topology="LI", width=2, depth=1, external_event_count=1, seed=0
            )
        )
        encoded = materialize_plan(workload, 7).to_bytes()

        with self.assertRaises(PlanValidationError):
            parse_plan(encoded[: HEADER_SIZE - 1])
        with self.assertRaises(PlanValidationError):
            parse_plan(encoded[:-1])
        with self.assertRaises(PlanValidationError):
            parse_plan(encoded + b"\0")
        with self.assertRaises(PlanValidationError):
            parse_plan(b"BROKEN!!" + encoded[8:])

        corrupted = bytearray(encoded)
        corrupted[-1] ^= 0x01
        parsed = parse_plan(corrupted)
        with self.assertRaises(PlanValidationError):
            validate_plan(parsed, workload)

    def test_plan_rejects_stale_topology_identity_and_wrong_seed(self) -> None:
        workload = generate_workload(
            WorkloadConfig(
                topology="LI", width=3, depth=2, external_event_count=1, seed=0
            )
        )
        plan = materialize_plan(workload, 99)
        with self.assertRaises(PlanValidationError):
            validate_plan(plan, workload, expected_seed=100)

        changed = deepcopy(workload)
        changed["configuration"]["width"] = 4
        with self.assertRaises(PlanValidationError):
            materialize_plan(changed, 99)

    def test_file_write_parse_sha_and_cli_round_trip(self) -> None:
        workload = generate_workload(
            WorkloadConfig(
                topology="HI", width=3, depth=2, external_event_count=2, seed=0
            )
        )
        plan = materialize_plan(workload, 42)
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            workload_path = root / "workload.json"
            plan_path = root / "sample.dvshla"
            workload_path.write_text(
                json.dumps(workload, indent=2) + "\n", encoding="utf-8"
            )
            destination = write_plan(plan_path, plan)
            self.assertEqual(destination, plan_path.resolve())
            self.assertEqual(parse_plan(plan_path), plan)
            self.assertEqual(
                file_sha256(plan_path), hashlib.sha256(plan.to_bytes()).hexdigest()
            )

            materialized = subprocess.run(  # noqa: S603
                [
                    sys.executable,
                    str(WORKLOAD_DIR / "plan.py"),
                    "materialize",
                    "--workload",
                    str(workload_path),
                    "--seed",
                    "43",
                    "--output",
                    str(plan_path),
                ],
                check=True,
                capture_output=True,
                text=True,
            )
            self.assertIn("records=", materialized.stdout)
            validated = subprocess.run(  # noqa: S603
                [
                    sys.executable,
                    str(WORKLOAD_DIR / "plan.py"),
                    "validate",
                    "--workload",
                    str(workload_path),
                    "--input",
                    str(plan_path),
                    "--seed",
                    "43",
                ],
                check=True,
                capture_output=True,
                text=True,
            )
            self.assertIn("valid ", validated.stdout)

    def test_no_generated_binary_is_checked_in(self) -> None:
        self.assertEqual(list(WORKLOAD_DIR.rglob("*.dvshla")), [])


if __name__ == "__main__":
    unittest.main()
