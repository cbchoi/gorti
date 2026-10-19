"""Adversarial record-link tests, not independent conformance certification.

Only artifact I/O is mocked. Hashing, declaration parsing, XML parsing,
campaign validation and verdict generation run through the production tool.
The rejection assertions intentionally fail while the reported defects exist.
"""

import copy
import hashlib
import importlib.util
import io
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch


SCRIPT = Path(__file__).resolve().parents[1] / "ieee2010_traceability.py"
SPEC = importlib.util.spec_from_file_location("traceability_adversarial_review", SCRIPT)
trace = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(trace)


class TraceabilityAdversarialReviewTests(unittest.TestCase):
    def setUp(self):
        self.root = SCRIPT.parent.parent / "_in_memory_traceability_review"
        self.artifacts = {}
        self.source = "tests/test_contract.py"
        self.put("service.go", "package service\n")
        self.put(self.source, "def test_contract():\n    assert True\n")
        self.put("standard.pdf", b"synthetic normative source")
        self.binary = self.put("server.bin", b"synthetic server artifact")
        self.junit = self.put("junit.xml", self.xml())
        self.inventory = {
            "schema_version": 1, "coverage_complete": False,
            "sources": [{"id": "S1", "standard": "IEEE 1516.1-2010",
                         "filename": "standard.pdf",
                         "sha256": self.sha(self.root / "standard.pdf")}],
            "requirements": [{
                "id": "R1", "source_id": "S1", "clause": "4.6.5(a)",
                "pdf_pages": [54], "subject": "rti", "applicability": "applicable",
                "statement": "Synthetic review obligation", "conditions": "Joined caller",
                "review_status": "source-reviewed", "implementation": ["service.go"],
                "tests": [{"path": self.source, "name": "test_contract"}], "open_items": [],
            }],
        }
        self.campaign = {
            "schema_version": 1, "independence": "internal-black-box", "author": "review fixture",
            "binary": {"path": str(self.binary), "sha256": self.sha(self.binary)},
            "test_source": {"path": self.source, "sha256": self.sha(self.root / self.source)},
            "junit": {"path": str(self.junit), "sha256": self.sha(self.junit)},
            "command": ["python", "-m", "pytest", self.source,
                        "--junitxml=" + str(self.junit)], "returncode": 0,
            "requirement_tests": [{"requirement_id": "R1", "tests": ["test_contract"]}],
        }
        for method, implementation in {
            "is_file": lambda path: path in self.artifacts,
            "open": lambda path, *args, **kwargs: io.BytesIO(self.payload(path)),
            "read_bytes": lambda path: self.payload(path),
            "read_text": lambda path, *args, **kwargs: self.payload(path).decode("utf-8-sig"),
            "stat": lambda path, *args, **kwargs: SimpleNamespace(st_size=len(self.payload(path))),
            "is_dir": lambda path: any(path in artifact.parents for artifact in self.artifacts),
            "glob": lambda path, pattern: iter(artifact for artifact in self.artifacts
                                                if artifact.parent == path and artifact.match(pattern)),
        }.items():
            mock = patch.object(Path, method, implementation)
            mock.start()
            self.addCleanup(mock.stop)

    def put(self, relative, payload):
        path = self.root / relative
        self.artifacts[path] = payload.encode("utf-8") if isinstance(payload, str) else payload
        return path

    def payload(self, path):
        try:
            return self.artifacts[path]
        except KeyError:
            raise FileNotFoundError(str(path)) from None

    def sha(self, path):
        return hashlib.sha256(self.payload(path)).hexdigest()

    @staticmethod
    def xml(case=None):
        case = case or '<testcase classname="tests.test_contract" file="tests/test_contract.py" name="test_contract"/>'
        return '<testsuites><testsuite tests="1" failures="0" errors="0" skipped="0">' + case + '</testsuite></testsuites>'

    def set_xml(self, xml):
        self.put("junit.xml", xml)
        self.campaign["junit"]["sha256"] = self.sha(self.junit)

    def audit(self, with_campaign=True):
        return trace.audit_inventory(
            copy.deepcopy(self.inventory), self.root, source_dir=self.root,
            campaign=copy.deepcopy(self.campaign) if with_campaign else None,
        )

    def assert_rejected(self):
        result = self.audit()
        proof = {key: result[key] for key in ("verdict", "errors", "counts")}
        self.assertEqual("FAIL", result["verdict"], proof)
        self.assertIsNone(result["campaign"])
        self.assertEqual(0, result["counts"]["independent_executed"])

    def test_control_consistent_records_are_valid_without_conformance(self):
        result = self.audit()
        self.assertEqual("RECORDS_VALID", result["verdict"])
        self.assertEqual("hash-verified", result["sources"][0]["verification"])
        self.assertEqual(1, result["counts"]["independent_executed"])
        self.assertFalse(result["coverage_complete"])
        self.assertFalse(result["conformance_established"])
        self.assertFalse(result["campaign"]["organizationally_independent"])

    def test_control_explicit_test_failure_is_rejected(self):
        self.set_xml(self.xml('<testcase name="test_contract"><failure message="control"/></testcase>'))
        self.assert_rejected()

    def test_case_from_other_source_cannot_satisfy_same_named_link(self):
        self.set_xml(self.xml('<testcase classname="tests.test_unrelated" file="tests/test_unrelated.py" name="test_contract"/>'))
        self.assert_rejected()

    def test_failed_root_aggregate_cannot_be_ignored(self):
        self.set_xml(self.xml().replace('<testsuites>', '<testsuites tests="2" failures="1" errors="0" skipped="0">'))
        self.assert_rejected()

    def test_nested_failed_suite_cannot_be_ignored(self):
        nested = ('<testsuite tests="1" failures="1" errors="0" skipped="0">'
                  '<testcase name="test_failed"><failure/></testcase></testsuite>')
        self.set_xml(self.xml().replace('</testsuite></testsuites>', nested + '</testsuite></testsuites>'))
        self.assert_rejected()

    def test_echoing_pytest_tokens_is_not_a_pytest_command(self):
        self.campaign["command"] = ["echo", "pytest", self.source]
        self.assert_rejected()

    def test_python_c_mode_cannot_masquerade_as_module_execution(self):
        self.campaign["command"] = ["python", "-c", "pass", "-m", "pytest", self.source,
                                    "--junitxml=" + str(self.junit)]
        self.assert_rejected()

    def test_source_path_substring_is_not_the_selected_source(self):
        self.campaign["command"][3] = self.source + ".bak"
        self.assert_rejected()

    def test_different_selected_node_cannot_credit_unselected_test(self):
        self.put(self.source, "def test_contract():\n    assert True\n\ndef test_other():\n    assert True\n")
        self.campaign["test_source"]["sha256"] = self.sha(self.root / self.source)
        self.campaign["command"][3] = self.source + "::test_other"
        self.assert_rejected()

    def test_selected_failing_node_cannot_be_omitted_from_report(self):
        self.put(self.source, "def test_contract():\n    assert True\n\ndef test_other():\n    assert False\n")
        self.campaign["test_source"]["sha256"] = self.sha(self.root / self.source)
        self.campaign["command"][3:4] = [self.source + "::test_contract", self.source + "::test_other"]
        self.assert_rejected()

    def test_junit_for_different_output_path_cannot_be_imported(self):
        self.campaign["command"][-1] = "--junitxml=" + str(self.root / "other-run.xml")
        self.assert_rejected()

    def test_nested_python_helper_is_not_a_collectible_test_link(self):
        self.put(self.source, "def factory():\n    def test_contract():\n        assert True\n    return test_contract\n")
        result = self.audit(with_campaign=False)
        self.assertNotEqual("source-reviewed-mapped", result["requirements"][0]["trace_status"], result)

    def test_go_comment_is_not_a_test_declaration(self):
        source = "tests/contract_test.go"
        self.put(source, "package tests\n// func TestContract(t *testing.T) {}\n")
        self.inventory["requirements"][0]["tests"] = [{"path": source, "name": "TestContract"}]
        result = self.audit(with_campaign=False)
        self.assertNotEqual("source-reviewed-mapped", result["requirements"][0]["trace_status"], result)

    def test_java_invocation_is_not_a_test_declaration(self):
        source = "tests/ContractTest.java"
        self.put(source, "class ContractTest { void helper() { testContract(); } }\n")
        self.inventory["requirements"][0]["tests"] = [{"path": source, "name": "testContract"}]
        result = self.audit(with_campaign=False)
        self.assertNotEqual("source-reviewed-mapped", result["requirements"][0]["trace_status"], result)

    def test_utf16_dtd_entity_cannot_bypass_dtd_prohibition(self):
        xml = ('<?xml version="1.0" encoding="UTF-16"?>'
               '<!DOCTYPE testsuites [<!ENTITY case "test_contract">]>'
               + self.xml().replace('name="test_contract"', 'name="&case;"'))
        self.put("junit.xml", xml.encode("utf-16"))
        self.campaign["junit"]["sha256"] = self.sha(self.junit)
        self.assert_rejected()

    def test_inactive_python_definition_cannot_supply_execution_link(self):
        self.put(self.source, "if False:\n    def test_contract():\n        assert True\n")
        self.campaign["test_source"]["sha256"] = self.sha(self.root / self.source)
        self.assert_rejected()

    def regression(self, code):
        self.put("tests/contract_test.go", code)
        manifest = {
            "tests": [{"id": "G1", "package": "./tests", "required_tests": ["TestContract"]}],
            "requirements": [{"id": "R1", "tests": ["G1"], "status": "resolved"}],
        }
        return trace.audit_regression_links(manifest, self.root)

    def test_control_regression_declaration_is_link_only_not_execution(self):
        result = self.regression("package tests\nfunc TestContract(t *testing.T) {}\n")
        self.assertEqual([], result["errors"])
        self.assertFalse(result["tests_executed"])
        self.assertFalse(result["requirements"][0]["execution_verified"])

    def test_regression_block_comment_is_not_a_declared_go_test(self):
        result = self.regression("package tests\n/*\nfunc TestContract(t *testing.T) {}\n*/\n")
        self.assertTrue(result["errors"], result)


if __name__ == "__main__":
    unittest.main()
