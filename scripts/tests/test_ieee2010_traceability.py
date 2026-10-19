"""Synthetic traceability records are tool tests, not conformance evidence."""

import copy
import hashlib
import importlib.util
import json
import tempfile
import unittest
from pathlib import Path


SCRIPT = Path(__file__).resolve().parents[1] / "ieee2010_traceability.py"
SPEC = importlib.util.spec_from_file_location("traceability", SCRIPT)
trace = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(trace)


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


class TraceabilityTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        (self.root / "service.go").write_text("package service\n")
        (self.root / "test_contract.py").write_text("def test_contract():\n    assert True\n")
        self.pdf = self.root / "standard.pdf"
        self.pdf.write_bytes(b"synthetic standard fixture")
        self.data = {
            "schema_version": 1, "coverage_complete": False,
            "sources": [{"id": "S1", "standard": "IEEE 1516.1-2010",
                         "filename": "standard.pdf", "sha256": digest(self.pdf)}],
            "requirements": [{
                "id": "R1", "source_id": "S1", "clause": "4.6.5(a)",
                "pdf_pages": [54], "subject": "rti", "applicability": "applicable",
                "statement": "A synthetic obligation", "conditions": "Joined caller",
                "review_status": "source-reviewed", "implementation": ["service.go"],
                "tests": [{"path": "test_contract.py", "name": "test_contract"}],
                "open_items": [],
            }],
        }

    def campaign(self, xml=None):
        binary = self.root / "server.bin"
        binary.write_bytes(b"synthetic immutable server")
        junit = self.root / "junit.xml"
        junit.write_text(xml or '<testsuites><testsuite tests="1" failures="0" errors="0" skipped="0">'
                         '<testcase classname="test_contract" name="test_contract"/>'
                         '</testsuite></testsuites>')
        return {
            "schema_version": 1, "independence": "internal-black-box", "author": "test fixture",
            "binary": {"path": str(binary), "sha256": digest(binary)},
            "test_source": {"path": "test_contract.py", "sha256": digest(self.root / "test_contract.py")},
            "junit": {"path": str(junit), "sha256": digest(junit)},
            "command": ["python", "-m", "pytest", "test_contract.py", "--junitxml=" + str(junit)], "returncode": 0,
            "requirement_tests": [{"requirement_id": "R1", "tests": ["test_contract"]}],
        }

    def audit(self, **kw):
        return trace.audit_inventory(self.data, self.root, **kw)

    def test_links_are_not_execution_or_full_conformance(self):
        result = self.audit(source_dir=self.root)
        self.assertEqual([], result["errors"])
        self.assertEqual("source-reviewed-mapped", result["requirements"][0]["trace_status"])
        self.assertFalse(result["conformance_established"])
        self.assertFalse(result["certification"])
        self.assertEqual(0, result["counts"]["independent_executed"])

    def test_missing_source_files_are_explicitly_unverified(self):
        result = self.audit()
        self.assertEqual("not-checked", result["sources"][0]["verification"])
        self.assertEqual([], result["errors"])

    def test_wrong_source_hash_fails(self):
        self.pdf.write_bytes(b"wrong edition")
        self.assertIn("SHA-256", " ".join(self.audit(source_dir=self.root)["errors"]))

    def test_missing_test_or_implementation_is_not_mapping_success(self):
        for field, value in [("implementation", ["absent.go"]),
                             ("tests", [{"path": "test_contract.py", "name": "test_missing"}])]:
            with self.subTest(field=field):
                data = copy.deepcopy(self.data)
                data["requirements"][0][field] = value
                result = trace.audit_inventory(data, self.root)
                self.assertTrue(result["errors"])
                self.assertEqual("broken-links", result["requirements"][0]["trace_status"])

    def test_escape_duplicate_and_unknown_source_rejected(self):
        mutations = [lambda d: d["requirements"].append(copy.deepcopy(d["requirements"][0])),
                     lambda d: d["requirements"][0].update(source_id="unknown"),
                     lambda d: d["requirements"][0].update(implementation=["../outside.go"]),
                     lambda d: d["requirements"][0].update(pdf_pages=[True]),
                     lambda d: d.update(coverage_complete=True)]
        for mutate in mutations:
            with self.subTest(mutate=mutate):
                data = copy.deepcopy(self.data)
                mutate(data)
                with self.assertRaises(trace.TraceError):
                    trace.validate_inventory(data, self.root)

    def test_candidate_unresolved_and_missing_test_remain_open(self):
        for changes in [{"review_status": "candidate"}, {"applicability": "unresolved"},
                        {"tests": []}, {"open_items": ["Boundary untested"]}]:
            with self.subTest(changes=changes):
                self.data["requirements"][0].update(changes)
                result = self.audit()
                self.assertNotEqual("source-reviewed-mapped", result["requirements"][0]["trace_status"])

    def test_exact_hashed_black_box_campaign_is_bounded_evidence(self):
        result = self.audit(campaign=self.campaign())
        self.assertEqual([], result["errors"])
        self.assertEqual(1, result["counts"]["independent_executed"])
        self.assertEqual("internal-black-box", result["campaign"]["independence"])
        self.assertFalse(result["campaign"]["production_source_attested"])
        self.assertFalse(result["conformance_established"])

    def test_absolute_source_selector_matches_only_the_exact_source(self):
        campaign = self.campaign()
        campaign["command"][3] = str(self.root / "test_contract.py")
        self.assertEqual([], self.audit(campaign=campaign)["errors"])
        campaign["command"][3] += ".bak"
        self.assertTrue(self.audit(campaign=campaign)["errors"])

    def test_repeatable_pytest_flags_are_valid_arguments(self):
        campaign = self.campaign()
        campaign["command"].extend(["-p", "no:cacheprovider", "-p", "pytest_asyncio.plugin"])
        self.assertEqual([], self.audit(campaign=campaign)["errors"])

    def test_exact_selected_node_is_accepted(self):
        campaign = self.campaign()
        campaign["command"][3] = "test_contract.py::test_contract"
        self.assertEqual([], self.audit(campaign=campaign)["errors"])

    def test_skips_failures_empty_missing_duplicate_and_false_counts_reject(self):
        cases = [
            '<testsuites/>',
            '<testsuite tests="1" failures="0" errors="0" skipped="1"><testcase name="test_contract"><skipped/></testcase></testsuite>',
            '<testsuite tests="1" failures="1" errors="0" skipped="0"><testcase name="test_contract"><failure/></testcase></testsuite>',
            '<testsuite tests="1" failures="0" errors="0" skipped="0"><testcase name="test_other"/></testsuite>',
            '<testsuite tests="2" failures="0" errors="0" skipped="0"><testcase name="test_contract"/><testcase name="test_contract"/></testsuite>',
            '<testsuite tests="99" failures="0" errors="0" skipped="0"><testcase name="test_contract"/></testsuite>',
        ]
        for xml in cases:
            with self.subTest(xml=xml):
                self.assertTrue(self.audit(campaign=self.campaign(xml))["errors"])

    def test_campaign_must_bind_current_test_source_binary_and_artifact(self):
        for field in ["binary", "test_source", "junit"]:
            with self.subTest(field=field):
                campaign = self.campaign()
                campaign[field]["sha256"] = "0" * 64
                self.assertTrue(self.audit(campaign=campaign)["errors"])

    def test_unknown_requirement_nonzero_exit_and_false_independence_reject(self):
        for modify in [lambda c: c.update(returncode=1),
                       lambda c: c.update(independence="certified"),
                       lambda c: c["requirement_tests"][0].update(requirement_id="unknown")]:
            with self.subTest(modify=modify):
                campaign = self.campaign()
                modify(campaign)
                self.assertTrue(self.audit(campaign=campaign)["errors"])

    def test_xml_dtd_rejected(self):
        xml = '<!DOCTYPE x [<!ENTITY x "test_contract">]><testsuites/>'
        self.assertTrue(self.audit(campaign=self.campaign(xml))["errors"])

    def test_duplicate_json_keys_are_rejected(self):
        with self.assertRaises(trace.TraceError):
            trace.decode_json('{"schema_version":1,"schema_version":1}')

    def test_regression_declarations_do_not_become_normative_execution(self):
        package = self.root / "rti/contract"
        package.mkdir(parents=True)
        (package / "contract_test.go").write_text("package contract\nfunc TestContract(t *testing.T) {}\n")
        data = {
            "requirements": [{"id": "ENG-contract", "tests": ["contract"], "clause": None}],
            "tests": [{"id": "contract", "package": "./rti/contract", "required_tests": ["TestContract"]}],
        }
        result = trace.audit_regression_links(data, self.root)
        self.assertEqual([], result["errors"])
        self.assertEqual(1, result["counts"]["test_references"])
        self.assertFalse(result["tests_executed"])
        self.assertEqual("engineering", result["requirements"][0]["kind"])
        data["tests"][0]["required_tests"] = ["TestAbsent"]
        self.assertTrue(trace.audit_regression_links(data, self.root)["errors"])

    def test_regression_missing_group_cannot_pass(self):
        data = {"requirements": [{"id": "R", "tests": ["absent"], "clause": "4"}], "tests": []}
        self.assertTrue(trace.audit_regression_links(data, self.root)["errors"])

    def test_go_campaign_requires_raw_test_events_and_exact_manifest(self):
        (self.root / "go.mod").write_text("module example.test\n")
        data = {"profile": {"source_id": "synthetic-unattested"},
                "tests": [{"id": "contract", "package": "./rti/contract", "required_tests": ["TestContract"]}]}
        events = [{"Action": "start", "Package": "example.test/rti/contract"},
                  {"Action": "run", "Package": "example.test/rti/contract", "Test": "TestContract"},
                  {"Action": "pass", "Package": "example.test/rti/contract", "Test": "TestContract"},
                  {"Action": "pass", "Package": "example.test/rti/contract"}]
        command = ["go", "test", "-json", "-count=1", "-timeout=120s", "-run", "^(TestContract)$", "./rti/contract"]
        record = {"mode": "run", "claim": "supported-profile", "verdict": "PASS", "errors": [],
                  "root": str(self.root), "profile": data["profile"], "commands_started": 1,
                  "manifest_sha256": hashlib.sha256(json.dumps(data, sort_keys=True).encode()).hexdigest(),
                  "tests": [{"id": "contract", "command": command, "returncode": 0,
                             "status": "pass", "stdout": "\n".join(json.dumps(e) for e in events)}]}
        result = trace.verify_go_campaign(data, record, self.root)
        self.assertEqual(1, result["groups_executed"])
        self.assertFalse(result["production_source_attested"])
        for mutation in [lambda r: r.update(mode="check"), lambda r: r.update(manifest_sha256="0" * 64),
                         lambda r: r["tests"][0].update(stdout=""),
                         lambda r: r["tests"][0].update(command=["go", "test"]),
                         lambda r: r.update(commands_started=0)]:
            broken = copy.deepcopy(record)
            mutation(broken)
            with self.assertRaises(trace.TraceError):
                trace.verify_go_campaign(data, broken, self.root)


if __name__ == "__main__":
    unittest.main()
