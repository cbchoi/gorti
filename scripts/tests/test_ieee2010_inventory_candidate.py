"""Candidate discovery safeguards; synthetic fixtures are not RTI evidence."""

import copy
import importlib.util
import os
from pathlib import Path
import json
import tempfile
import unittest
from unittest.mock import patch
import zipfile

from test_ieee2010_feature_inventory import fixture, gate


def candidate_module():
    path = Path(__file__).resolve().parents[1] / "ieee2010_inventory_candidate.py"
    spec = importlib.util.spec_from_file_location("inventory_candidate", path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


class ReconciliationRegressionTests(unittest.TestCase):
    def setUp(self):
        self.inventory = fixture()
        self.expected = {
            "schema_version": 1,
            "kind": "interim-candidate-discovery",
            "baseline": {"sha256": "b" * 64, "row_count": len(self.inventory["rows"])},
            "source_capture": {"sha256": "c" * 64},
            "inventory": self.inventory,
        }
        self.data = copy.deepcopy(self.expected)

    def assess(self):
        if os.environ.get("IEEE2010_LEGACY_CHECKER") == "1":
            # Run identical mutations through the existing schema-1 checker.
            return gate.evaluate(self.data["inventory"])
        return candidate_module().assess(self.data, self.expected)

    def test_unchanged_open_candidate_passes_structure_not_completion(self):
        result = self.assess()
        self.assertEqual("PASS", result["verdict"], result)
        self.assertFalse(result["engineering_complete"])
        self.assertFalse(result["conformance_established"])

    def test_self_rewritten_catalog_cannot_hide_removed_surface(self):
        inv = self.data["inventory"]
        inv["rows"] = [row for row in inv["rows"] if row["id"] != "surface:sample"]
        inv["expected_ids"].remove("surface:sample")
        inv["surface_digest"] = gate.digest(inv["expected_ids"])
        self.assertEqual("FAIL", self.assess()["verdict"])

    def test_same_id_contract_edit_requires_rediscovery(self):
        self.data["inventory"]["rows"][-1]["contract"] = {"parameters": "dropped_required_input"}
        self.assertEqual("FAIL", self.assess()["verdict"])

    def test_changed_baseline_reference_is_not_accepted(self):
        self.data["baseline"]["sha256"] = "f" * 64
        self.assertEqual("FAIL", self.assess()["verdict"])

    def test_facet_cannot_claim_fixed_while_row_is_open(self):
        self.data["inventory"]["rows"][0]["facets"]["inputs"] = "fixed-tested"
        self.assertEqual("FAIL", self.assess()["verdict"])


class CandidateSafeguardTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.module = candidate_module()

    def test_reconciliation_detects_contract_change_without_id_change(self):
        before, after = fixture(), fixture()
        after["rows"][-1]["contract"] = {"parameters": "new_parameter"}
        changes = self.module.reconcile(before, after)
        self.assertEqual([after["rows"][-1]["id"]], [c["id"] for c in changes if c["change"] == "contract-changed"])

    def test_cpp_line_movement_is_not_a_contract_change(self):
        before, after = fixture(), fixture()
        before["rows"][-1]["contract"] = {"name": "f", "line": 10}
        after["rows"][-1]["contract"] = {"name": "f", "line": 20}
        self.assertTrue(all(c["change"] == "unchanged-contract" for c in self.module.reconcile(before, after)))

    def test_removed_row_is_retained_and_added_row_is_not_promoted(self):
        before, after = fixture(), fixture()
        after["rows"][-1]["id"] = "surface:new"
        changes = self.module.reconcile(before, after)
        self.assertIn("surface:sample", [c["id"] for c in changes if c["change"] == "removed-unresolved"])
        self.assertIn("surface:new", [c["id"] for c in changes if c["change"] == "added"])
        self.assertTrue(all(c["disposition_transfer"].startswith("none;") for c in changes))

    def test_same_contract_with_source_change_is_recorded(self):
        before, after = fixture(), fixture()
        after["sources"][0]["sha256"] = "f" * 64
        changes = self.module.reconcile(before, after)
        self.assertTrue(all(c["change"] == "unchanged-contract" and c["source_changes"] == ["s0"] for c in changes))

    def test_duplicate_discovery_id_fails_instead_of_last_write_wins(self):
        data = fixture()
        data["rows"].append(copy.deepcopy(data["rows"][0]))
        with self.assertRaisesRegex(ValueError, "duplicate id"):
            self.module.reconcile(fixture(), data)

    def test_statuses_and_facets_are_never_transferred(self):
        before, after = fixture(), fixture()
        for row in after["rows"]:
            row.update(status="fixed-tested", reviewer="previous-reviewer", evidence=["old-pass"])
            row["facets"] = {key: "fixed-tested" for key in gate.FACETS}
        scanner = type("Scanner", (), {"source": lambda self, path: "repo:" + path})()
        self.module.sanitize_discovery(after, before, scanner)
        for row in after["rows"]:
            self.assertIn(row["status"], {"unverified", "normativeunknown"})
            self.assertEqual({row["status"]}, set(row["facets"].values()))
            self.assertFalse(row["evidence"])
            self.assertIsNone(row["reviewer"])
            self.assertFalse(row["prior_disposition"]["transferred"])

    def test_old_missing_implementation_wording_is_not_carried_forward(self):
        before, after = fixture(), fixture()
        row = after["rows"][-1]
        row.update(kind="implementation-gap", rationale="Missing host implementation")
        scanner = type("Scanner", (), {"source": lambda self, path: "repo:" + path})()
        self.module.sanitize_discovery(after, before, scanner)
        self.assertEqual("historical-followup", row["kind"])
        self.assertNotIn("Missing host implementation", row["rationale"])
        self.assertIn("no current defect", row["rationale"])

    def test_closed_facets_rejected_even_in_supplied_comparison(self):
        data = {"schema_version": 1, "kind": "interim-candidate-discovery", "inventory": fixture()}
        data["inventory"]["rows"][0]["facets"]["inputs"] = "fixed-tested"
        self.assertEqual("FAIL", self.module.assess(data, copy.deepcopy(data))["verdict"])

    def test_check_requires_discovery_and_always_rejects_completion(self):
        data = {"schema_version": 1, "kind": "interim-candidate-discovery", "inventory": fixture()}
        self.assertEqual("FAIL", self.module.assess(data, None)["verdict"])
        self.assertEqual("FAIL", self.module.assess(data, data, require_completion=True)["verdict"])

    def test_malformed_candidate_fails_closed(self):
        for value in [None, [], {}, {"schema_version": True}, {"inventory": []}]:
            with self.subTest(value=value):
                self.assertEqual("FAIL", self.module.assess(value, value)["verdict"])

    def test_source_path_rejects_cross_platform_escape_and_aliases(self):
        for value in ["../file", "/tmp/file", "C:/file", "C:file", "\\\\host\\file", "a/../b", "a\\b", "a//b", "./a"]:
            with self.subTest(value=value), self.assertRaises(ValueError):
                self.module.relative_path(value)

    def test_existing_output_is_never_overwritten(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "immutable.json"
            path.write_bytes(b"original")
            with self.assertRaises(FileExistsError):
                self.module.write_new(path, {})
            self.assertEqual(b"original", path.read_bytes())

    def test_new_output_is_parseable_and_no_execution_is_started(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "candidate.json"
            self.module.write_new(path, {"claims": False})
            self.assertEqual({"claims": False}, gate.load_json(path))

    def capture_fixture(self, directory):
        root = Path(directory)
        content = b"source content"
        manifest = {"files": {"sample.txt": self.module.hashlib.sha256(content).hexdigest()}, "exclusions": ["unavailable-input"]}
        path = root / "manifest.json"
        path.write_text(json.dumps(manifest), encoding="utf-8")
        with zipfile.ZipFile(root / "source.zip", "w") as zipped:
            zipped.writestr("sample.txt", content)
        return path

    def test_parent_archive_content_is_verified_and_exclusions_retained(self):
        with tempfile.TemporaryDirectory() as directory:
            path = self.capture_fixture(directory)
            manifest, result = self.module.verify_capture(path)
            self.assertEqual(1, result["files_verified"])
            self.assertEqual(manifest["exclusions"], result["exclusions"])
            with zipfile.ZipFile(path.with_name("source.zip"), "w") as zipped:
                zipped.writestr("sample.txt", b"tampered")
            with self.assertRaisesRegex(ValueError, "content mismatch"):
                self.module.verify_capture(path)

    def test_parent_archive_missing_or_added_members_are_not_ignored(self):
        with tempfile.TemporaryDirectory() as directory:
            path = self.capture_fixture(directory)
            with zipfile.ZipFile(path.with_name("source.zip"), "a") as zipped:
                zipped.writestr("unlisted.txt", b"extra")
            with self.assertRaisesRegex(ValueError, "members differ"):
                self.module.verify_capture(path)

    def test_source_capture_detects_added_and_changed_files(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "rti").mkdir()
            (root / "rti/example.go").write_text("package example", encoding="utf-8")
            before = {"files": {}}
            first = self.module.capture_sources(root, root / "official", before)
            (root / "rti/new_test.go").write_text("package example", encoding="utf-8")
            second = self.module.capture_sources(root, root / "official", before)
            self.assertNotEqual(first["sha256"], second["sha256"])
            (root / "rti/example.go").write_text("package changed", encoding="utf-8")
            third = self.module.capture_sources(root, root / "official", before)
            self.assertNotEqual(second["sha256"], third["sha256"])

    def test_cli_refuses_existing_candidate_before_scanning(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "candidate.json"
            path.write_text("original", encoding="utf-8")
            args = ["tool", "discover", "--root", directory, "--official-root", directory,
                    "--before-manifest", str(path), "--candidate", str(path)]
            with patch.object(self.module.sys, "argv", args), patch.object(self.module, "build") as build:
                self.assertEqual(1, self.module.main())
                build.assert_not_called()
            self.assertEqual("original", path.read_text(encoding="utf-8"))


if __name__ == "__main__":
    unittest.main()
