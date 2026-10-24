"""Focused checks for evidence classification, not RTI service tests."""

import copy
import importlib.util
import tempfile
import unittest
from pathlib import Path

SPEC = importlib.util.spec_from_file_location(
    "evidence_summary", Path(__file__).resolve().parents[1] / "summarize_requirement_results.py")
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


def fixture():
    requirements, classes = [], []
    for index, category in enumerate(MODULE.CATEGORIES):
        target = {"path": f"tests/{index}.py", "name": "test_contract"}
        oracle = f"Checks {category}"
        requirements.append({
            "id": f"R{index}", "group": "sample", "applicability": "applicable",
            "clause": "1", "tests": [dict(target, oracle=oracle)],
            "test_evidence": [dict(target, source_sha256_at_review="a" * 64,
                                   execution_status=MODULE.PASSED_LINK_STATUS,
                                   invocations=[{"identity": str(index), "status": "pass"}])],
        })
        classes.append(dict(target, source_sha256="a" * 64,
                            oracles_sha256=MODULE.oracle_digest([oracle]),
                            category=category, rationale=oracle))
    return {"source_commit": "b" * 40, "requirements": requirements,
            "execution_artifacts": []}, {"tests": classes}


class EvidenceClassificationTest(unittest.TestCase):
    def test_primary_classes_do_not_turn_reporting_into_service_evidence(self):
        data, classes = fixture()
        summary, rows, _ = MODULE.summarize(data, classes)
        for category in MODULE.CATEGORIES:
            self.assertEqual(summary["evidence_categories"][category]["all_links"], 1)
        self.assertEqual(rows[-1]["evidence_category"], "documentation-reporting")

    def test_mixed_requirement_is_counted_once_but_links_are_counted_separately(self):
        data, classes = fixture()
        data["requirements"][0]["tests"].extend(data["requirements"][1]["tests"])
        data["requirements"][0]["test_evidence"].extend(data["requirements"][1]["test_evidence"])
        summary, _, _ = MODULE.summarize(data, classes)
        profile = "api-structure+service-behavior"
        self.assertEqual(summary["applicable_requirement_profiles"][profile],
                         {"all-linked-invocations-passed": 1})
        self.assertEqual(summary["evidence_categories"]["api-structure"]["all_links"], 2)

    def test_skip_and_unresolved_are_not_promoted_to_passing(self):
        data, classes = fixture()
        test = data["requirements"][0]["test_evidence"][0]
        test["execution_status"] = "nonpassing-variant"
        test["invocations"][0]["status"] = "skip"
        data["requirements"][1]["applicability"] = "unresolved"
        summary, _, _ = MODULE.summarize(data, classes)
        self.assertEqual(summary["skipped_ids"], ["R0"])
        self.assertEqual(summary["groups"]["sample"]["unresolved"], 1)

    def test_unknown_or_duplicate_classification_is_rejected(self):
        data, classes = fixture()
        for altered in ({"tests": classes["tests"][:-1]},
                        {"tests": classes["tests"] + [classes["tests"][0]]}):
            with self.assertRaises(ValueError):
                MODULE.summarize(data, altered)

    def test_changed_source_or_oracle_requires_classification_review(self):
        data, classes = fixture()
        for field in ("source_sha256", "oracles_sha256"):
            altered = copy.deepcopy(classes)
            altered["tests"][0][field] = "c" * 64
            with self.assertRaises(ValueError):
                MODULE.summarize(data, altered)

    def test_missing_and_failed_invocations_are_separate(self):
        data, classes = fixture()
        data["requirements"][0]["test_evidence"][0]["execution_status"] = "not-found"
        data["requirements"][1]["test_evidence"][0]["invocations"][0]["status"] = "fail"
        summary, _, _ = MODULE.summarize(data, classes)
        self.assertEqual(summary["missing_ids"], ["R0"])
        self.assertEqual(summary["failed_ids"], ["R1"])

    def test_execution_artifact_hash_is_checked(self):
        data, _ = fixture()
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            path = root / "result.xml"
            path.write_bytes(b"retained-result")
            data["execution_artifacts"] = [{"filename": path.name, "sha256": MODULE.digest(path)}]
            checks = MODULE.verify_files(data, artifact_root=root)
            self.assertEqual(checks["execution_artifacts_checked"], 1)
            path.write_bytes(b"changed")
            with self.assertRaises(ValueError):
                MODULE.verify_files(data, artifact_root=root)


if __name__ == "__main__":
    unittest.main()
