from __future__ import annotations

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
    canonical_identity,
    canonical_topology_identity,
    derive_event_deliveries_per_injection,
    generate_workload,
    validate_identity,
    validate_topology_identity,
)


class WorkloadModelTests(unittest.TestCase):
    def test_li_counts_follow_published_equation(self) -> None:
        workload = generate_workload(
            WorkloadConfig(
                topology="LI", width=4, depth=3, external_event_count=5, seed=7
            )
        )

        self.assertEqual(workload["expected_counts"]["atomic_nodes"], 7)
        self.assertEqual(workload["expected_counts"]["directed_couplings"], 7)
        self.assertEqual(
            workload["expected_counts"]["per_injected_event"]["event_values"],
            7,
        )
        self.assertEqual(
            workload["expected_counts"]["total"]["hla_interactions"], 35
        )
        self.assertEqual(
            workload["expected_counts"]["total"]["hla_attribute_updates"], 35
        )
        self.assertEqual(
            workload["expected_counts"]["total"]["hla_deliveries"], 70
        )
        self.assertFalse(
            any(
                coupling["kind"] == "internal"
                for coupling in workload["directed_couplings"]
            )
        )

    def test_hi_counts_follow_published_equation(self) -> None:
        workload = generate_workload(
            WorkloadConfig(
                topology="hi", width=4, depth=3, external_event_count=5, seed=7
            )
        )

        # 3 atoms at each of two regular levels, plus the deepest atom.
        self.assertEqual(workload["expected_counts"]["atomic_nodes"], 7)
        # Seven input fanout edges plus two chain edges at each regular level.
        self.assertEqual(workload["expected_counts"]["directed_couplings"], 11)
        self.assertEqual(
            workload["expected_counts"]["per_injected_event"]["event_values"],
            13,
        )
        self.assertEqual(
            workload["expected_counts"]["total"]["hla_interactions"], 65
        )
        self.assertEqual(
            workload["expected_counts"]["total"]["hla_deliveries"], 130
        )

    def test_graph_and_equation_agree_across_dimensions(self) -> None:
        for topology in ("LI", "HI"):
            for width, depth in ((2, 1), (2, 8), (5, 2), (8, 6)):
                with self.subTest(topology=topology, width=width, depth=depth):
                    workload = generate_workload(
                        WorkloadConfig(
                            topology=topology,
                            width=width,
                            depth=depth,
                            external_event_count=3,
                            seed=1516,
                        )
                    )
                    derived = derive_event_deliveries_per_injection(workload)
                    expected = workload["expected_counts"]["per_injected_event"][
                        "event_values"
                    ]
                    self.assertEqual(derived, expected)

    def test_depth_one_has_only_the_deepest_atomic(self) -> None:
        for topology in ("LI", "HI"):
            workload = generate_workload(
                WorkloadConfig(
                    topology=topology,
                    width=20,
                    depth=1,
                    external_event_count=2,
                    seed=1,
                )
            )
            self.assertEqual(len(workload["atomic_nodes"]), 1)
            self.assertEqual(len(workload["directed_couplings"]), 1)
            self.assertEqual(
                workload["expected_counts"]["total"]["event_values"], 2
            )

    def test_spelling_resolution_and_scope_are_explicit(self) -> None:
        workload = generate_workload()
        self.assertEqual(workload["benchmark"]["requested_spelling"], "DEVSOne")
        self.assertEqual(workload["benchmark"]["resolved_name"], "DEVStone")
        self.assertIn("not a DEVS simulator-kernel", workload["benchmark"]["scope"])
        self.assertEqual(
            workload["configuration"]["synthetic_transition_delay"],
            {"external_seconds": 0, "internal_seconds": 0},
        )

    def test_generation_and_identity_are_deterministic(self) -> None:
        config = WorkloadConfig(
            topology="HI", width=6, depth=4, external_event_count=4, seed=99
        )
        first = generate_workload(config)
        second = generate_workload(config)
        self.assertEqual(first, second)
        self.assertTrue(validate_identity(first))
        self.assertTrue(validate_topology_identity(first))
        self.assertEqual(first["identity"]["digest"], canonical_identity(first))
        self.assertEqual(
            first["topology_identity"]["digest"],
            canonical_topology_identity(first),
        )

        first["configuration"]["width"] = 7
        self.assertFalse(validate_identity(first))

    def test_seed_changes_payload_and_document_identity_but_not_topology(self) -> None:
        one = generate_workload(WorkloadConfig(seed=1))
        two = generate_workload(WorkloadConfig(seed=2))
        self.assertEqual(one["atomic_nodes"], two["atomic_nodes"])
        self.assertEqual(one["directed_couplings"], two["directed_couplings"])
        self.assertNotEqual(one["injected_events"], two["injected_events"])
        self.assertNotEqual(one["identity"]["digest"], two["identity"]["digest"])
        self.assertEqual(
            one["topology_identity"]["digest"],
            two["topology_identity"]["digest"],
        )

    def test_topology_identity_excludes_injected_payload_bytes(self) -> None:
        workload = generate_workload(
            WorkloadConfig(
                topology="HI", width=4, depth=3, external_event_count=2, seed=9
            )
        )
        modified = deepcopy(workload)
        modified["configuration"]["seed"] = 123456
        modified["injected_events"][0]["payload"]["value"] = "ff" * 16
        self.assertEqual(
            canonical_topology_identity(modified),
            workload["topology_identity"]["digest"],
        )

        modified["directed_couplings"] = list(
            reversed(modified["directed_couplings"])
        )
        self.assertNotEqual(
            canonical_topology_identity(modified),
            workload["topology_identity"]["digest"],
        )

    def test_topology_identity_binds_every_declared_scope(self) -> None:
        workload = generate_workload(
            WorkloadConfig(
                topology="HI", width=4, depth=3, external_event_count=2, seed=9
            )
        )
        digest = workload["topology_identity"]["digest"]

        mutations = []
        for key, value in (
            ("topology", "LI"),
            ("width", 5),
            ("depth", 4),
            ("external_event_count", 3),
        ):
            changed = deepcopy(workload)
            changed["configuration"][key] = value
            mutations.append(changed)

        changed = deepcopy(workload)
        changed["atomic_nodes"] = list(reversed(changed["atomic_nodes"]))
        mutations.append(changed)
        changed = deepcopy(workload)
        changed["directed_couplings"] = list(
            reversed(changed["directed_couplings"])
        )
        mutations.append(changed)
        changed = deepcopy(workload)
        changed["hla_mapping"]["profile_version"] = "changed"
        mutations.append(changed)
        changed = deepcopy(workload)
        changed["configuration"]["synthetic_transition_delay"][
            "external_seconds"
        ] = 1
        mutations.append(changed)

        for index, changed in enumerate(mutations):
            with self.subTest(mutation=index):
                self.assertNotEqual(canonical_topology_identity(changed), digest)

    def test_invalid_dimensions_are_rejected(self) -> None:
        with self.assertRaises(ValueError):
            WorkloadConfig(width=1)
        with self.assertRaises(ValueError):
            WorkloadConfig(depth=0)
        with self.assertRaises(ValueError):
            WorkloadConfig(external_event_count=0)
        with self.assertRaises(ValueError):
            WorkloadConfig(topology="HO")
        with self.assertRaises(TypeError):
            WorkloadConfig(width=True)

    def test_cli_writes_a_valid_requested_workload(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "workload.json"
            completed = subprocess.run(  # noqa: S603
                [
                    sys.executable,
                    str(WORKLOAD_DIR / "generate.py"),
                    "--topology",
                    "LI",
                    "--width",
                    "5",
                    "--depth",
                    "4",
                    "--external-events",
                    "2",
                    "--seed",
                    "42",
                    "--output",
                    str(output),
                ],
                check=True,
                capture_output=True,
                text=True,
            )
            workload = json.loads(output.read_text(encoding="utf-8"))
            self.assertEqual(workload["configuration"]["topology"], "LI")
            self.assertEqual(workload["configuration"]["width"], 5)
            self.assertTrue(validate_identity(workload))
            self.assertIn("topology_sha256=", completed.stdout)

    def test_checked_in_default_matches_generator(self) -> None:
        expected = generate_workload()
        actual = json.loads(
            (WORKLOAD_DIR / "workload.json").read_text(encoding="utf-8")
        )
        self.assertEqual(actual, expected)
        self.assertTrue(validate_identity(actual))


if __name__ == "__main__":
    unittest.main()
