"""Local gate tests; all synthetic evidence below is test-only, not IEEE proof."""

import copy
import hashlib
import importlib.util
import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch


SCRIPT = Path(__file__).resolve().parents[1] / "ieee2010_quality_gate.py"
SPEC = importlib.util.spec_from_file_location("ieee2010_quality_gate", SCRIPT)
gate = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(gate)

STANDARDS = ["IEEE 1516-2010", "IEEE 1516.1-2010", "IEEE 1516.2-2010"]
PACKAGE = "example.test/rti/contract"


def event(action, test=None, **extra):
    value = {"Action": action, "Package": PACKAGE, **extra}
    if test is not None:
        value["Test"] = test
    return value


def stream(*events):
    return "\n".join(json.dumps(value) for value in events) + "\n"


def passing():
    return stream(event("start"), event("run", "TestContract"),
                  event("pass", "TestContract"), event("pass"))


def manifest():
    return {
        "schema_version": 1,
        "claim": "supported-profile",
        "profile": {
            "id": "test-only", "scope": "One synthetic contract",
            "bindings": ["Go product binding"], "platforms": ["test host"],
            "source_id": "test-fixture", "required_requirements": ["R0"],
            "limitations": ["Not a normative inventory"],
        },
        "normative": {
            "coverage_complete": False, "standards": STANDARDS.copy(),
            "requirement_ids": [], "review_evidence": None,
        },
        "requirements": [{
            "id": "R0", "standard": STANDARDS[0], "clause": None,
            "applicability": "applicable", "rationale": "RTI regression",
            "status": "resolved", "tests": ["contract"],
        }],
        "tests": [{
            "id": "contract", "package": "./rti/contract",
            "required_tests": ["TestContract"],
        }],
        "independent_evidence": [],
    }


class GoJSONTests(unittest.TestCase):
    def parse(self, output, returncode=0, required=None):
        return gate.parse_go_test(output, returncode, PACKAGE,
                                  required or ["TestContract"])

    def test_executed_named_test_and_package_pass(self):
        result = self.parse(passing())
        self.assertEqual("pass", result["status"])
        self.assertEqual(["TestContract"], result["passed_tests"])

    def test_parallel_subtests_are_supported(self):
        output = stream(event("start"), event("run", "TestContract"),
                        event("run", "TestContract/child"),
                        event("pause", "TestContract/child"),
                        event("cont", "TestContract/child"),
                        event("pass", "TestContract/child"),
                        event("pass", "TestContract"), event("pass"))
        self.assertEqual("pass", self.parse(output)["status"])

    def test_failure_skip_and_empty_selection_never_pass(self):
        cases = {
            "empty": "",
            "no_matching_tests": stream(event("start"), event("output", Output="warning: no tests to run\n"), event("pass")),
            "no_test_files": stream(event("start"), event("skip")),
            "test_skip": stream(event("start"), event("run", "TestContract"), event("skip", "TestContract"), event("pass")),
            "subtest_skip": stream(event("start"), event("run", "TestContract"), event("run", "TestContract/child"), event("skip", "TestContract/child"), event("pass", "TestContract"), event("pass")),
            "test_fail": stream(event("start"), event("run", "TestContract"), event("fail", "TestContract"), event("pass")),
            "package_fail": passing() + stream(event("fail")),
            "build_fail": stream({"Action": "build-fail", "ImportPath": PACKAGE}),
            "pass_without_run": stream(event("start"), event("pass", "TestContract"), event("pass")),
            "truncated": stream(event("start"), event("run", "TestContract")),
            "no_package_end": stream(event("start"), event("run", "TestContract"), event("pass", "TestContract")),
            "wrong_package": passing().replace(PACKAGE, "other/package"),
            "unknown_action": passing() + stream(event("mystery")),
            "malformed_json": passing() + "not json\n",
            "scalar_json": passing() + "null\n",
            "duplicate_json_key": passing() + '{"Action":"fail","Action":"pass","Package":"example.test/rti/contract"}\n',
            "unexpected_unfinished_test": passing() + stream(event("run", "TestOther")),
            "duplicate_pass": passing() + stream(event("pass", "TestContract")),
        }
        for label, output in cases.items():
            with self.subTest(label=label):
                self.assertEqual("fail", self.parse(output)["status"])

    def test_nonzero_exit_overrides_passing_events(self):
        self.assertEqual("fail", self.parse(passing(), 1)["status"])

    def test_every_required_test_must_execute(self):
        self.assertEqual("fail", self.parse(passing(), required=["TestContract", "TestMissing"])["status"])


class GateTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        (self.root / "rti/contract").mkdir(parents=True)
        (self.root / "go.mod").write_text("module example.test\n\ngo 1.22\n", encoding="utf-8")
        self.data = manifest()

    def run_gate(self, execute=True, claim=None):
        completed = subprocess.CompletedProcess([], 0, passing(), "")
        with patch.object(gate.subprocess, "run", return_value=completed) as run:
            result = gate.evaluate(self.data, self.root, execute=execute, claim=claim)
        return result, run

    def full_fixture(self):
        self.data["claim"] = "full-2010"
        for i, standard in enumerate(STANDARDS):
            row = copy.deepcopy(self.data["requirements"][0])
            row.update(id=f"R{i}", standard=standard, clause="test-only clause")
            if i == 0:
                self.data["requirements"][0] = row
            else:
                self.data["requirements"].append(row)
        ids = ["R0", "R1", "R2"]
        self.data["normative"].update(coverage_complete=True, requirement_ids=ids,
                                     review_evidence="inventory-review")
        for evidence_id, kind in [("inventory-review", "normative-review"),
                                  ("external-campaign", "independent-test")]:
            path = self.root / (evidence_id + ".txt")
            payload = b"SYNTHETIC UNIT TEST ARTIFACT - NOT CONFORMANCE EVIDENCE\n"
            path.write_bytes(payload)
            self.data["independent_evidence"].append({
                "id": evidence_id, "kind": kind, "reviewer": "test-only reviewer",
                "independent": True, "source_id": "test-fixture", "verdict": "pass",
                "path": path.name, "sha256": hashlib.sha256(payload).hexdigest(),
                "requirement_ids": ids.copy(),
            })

    def test_bounded_run_is_not_full_conformance_or_certification(self):
        result, run = self.run_gate()
        self.assertEqual("PASS", result["verdict"])
        self.assertEqual("supported-profile", result["claim"])
        self.assertFalse(result["certification"])
        self.assertEqual("LOCAL/MANUAL", result["authority"])
        self.assertEqual(1, result["commands_started"])
        self.assertIn("bounded", result["scope"])
        run.assert_called_once()
        args, kwargs = run.call_args
        self.assertEqual(["go", "test", "-json", "-count=1", "-timeout=120s",
                          "-run", "^(TestContract)$", "./rti/contract"], args[0])
        self.assertFalse(kwargs["shell"])
        self.assertEqual(self.root, kwargs["cwd"])
        self.assertEqual("", kwargs["env"]["GOFLAGS"])
        self.assertEqual("local", kwargs["env"]["GOTOOLCHAIN"])
        self.assertEqual(passing(), result["tests"][0]["stdout"])

    def test_check_does_not_execute_or_return_a_gate_pass(self):
        result, run = self.run_gate(execute=False)
        self.assertEqual("CHECK_ONLY", result["verdict"])
        self.assertEqual(0, result["commands_started"])
        self.assertEqual("not_run", result["tests"][0]["status"])
        run.assert_not_called()

    def test_full_claim_rejects_example_missing_proof_before_execution(self):
        result, run = self.run_gate(claim="full-2010")
        self.assertEqual("FAIL", result["verdict"])
        self.assertIn("normative", " ".join(result["errors"]))
        self.assertIn("independent", " ".join(result["errors"]))
        run.assert_not_called()

    def test_full_evidence_prerequisites_can_pass_synthetic_fixture(self):
        self.full_fixture()
        result, _ = self.run_gate()
        self.assertEqual("PASS", result["verdict"])
        self.assertFalse(result["certification"])
        self.assertIn("prerequisites", result["scope"])

    def test_full_valid_check_still_does_not_claim_execution(self):
        self.full_fixture()
        result, run = self.run_gate(execute=False)
        self.assertEqual("CHECK_ONLY", result["verdict"])
        run.assert_not_called()

    def test_missing_normative_row_is_not_covered_by_complete_flag(self):
        self.full_fixture()
        self.data["normative"]["requirement_ids"].append("MISSING")
        result, run = self.run_gate()
        self.assertEqual("FAIL", result["verdict"])
        self.assertIn("MISSING", " ".join(result["errors"]))
        run.assert_not_called()

    def test_full_inventory_must_cover_all_three_standards(self):
        self.full_fixture()
        self.data["requirements"][2]["standard"] = STANDARDS[0]
        result, _ = self.run_gate()
        self.assertEqual("FAIL", result["verdict"])

    def test_unresolved_or_unverified_applicable_requirement_fails(self):
        self.full_fixture()
        good = copy.deepcopy(self.data)
        for field, value in [("status", "unresolved"), ("applicability", "unresolved"),
                             ("tests", []), ("clause", None)]:
            with self.subTest(field=field):
                self.data = copy.deepcopy(good)
                self.data["requirements"][1][field] = value
                result, run = self.run_gate()
                self.assertEqual("FAIL", result["verdict"])
                run.assert_not_called()

    def test_exclusion_requires_resolved_rationale(self):
        self.full_fixture()
        row = self.data["requirements"][1]
        row.update(applicability="not-applicable", rationale="", tests=[])
        result, _ = self.run_gate()
        self.assertEqual("FAIL", result["verdict"])
        row["rationale"] = "Synthetic federate-only obligation"
        result, _ = self.run_gate()
        self.assertEqual("PASS", result["verdict"])

    def test_applicable_requirement_also_needs_applicability_rationale(self):
        self.full_fixture()
        self.data["requirements"][1]["rationale"] = "   "
        result, run = self.run_gate()
        self.assertEqual("FAIL", result["verdict"])
        run.assert_not_called()

    def test_independent_evidence_must_be_present_bound_and_hashed(self):
        self.full_fixture()
        good = copy.deepcopy(self.data)
        for field, value in [("independent", False), ("source_id", "stale"),
                             ("verdict", "skip"), ("sha256", "0" * 64),
                             ("path", "missing.txt"), ("requirement_ids", ["R0"])]:
            with self.subTest(field=field):
                self.data = copy.deepcopy(good)
                self.data["independent_evidence"][1][field] = value
                result, run = self.run_gate()
                self.assertEqual("FAIL", result["verdict"])
                run.assert_not_called()

    def test_missing_independent_campaign_fails(self):
        self.full_fixture()
        self.data["independent_evidence"].pop()
        result, _ = self.run_gate()
        self.assertEqual("FAIL", result["verdict"])

    def test_changed_or_empty_artifact_fails(self):
        self.full_fixture()
        (self.root / "external-campaign.txt").write_bytes(b"")
        result, _ = self.run_gate()
        self.assertEqual("FAIL", result["verdict"])

    def test_required_test_failure_skip_and_missing_test_fail_gate(self):
        self.full_fixture()
        for output, code in [(passing(), 1), ("", 0),
                             (stream(event("start"), event("run", "TestContract"), event("skip", "TestContract"), event("pass")), 0)]:
            with self.subTest(output=output, code=code):
                completed = subprocess.CompletedProcess([], code, output, "diagnostic")
                with patch.object(gate.subprocess, "run", return_value=completed):
                    result = gate.evaluate(self.data, self.root, execute=True)
                self.assertEqual("FAIL", result["verdict"])

    def test_spawn_failure_and_timeout_fail_closed(self):
        for error, count in [(FileNotFoundError("go unavailable"), 0),
                             (subprocess.TimeoutExpired(["go"], 180), 1)]:
            with self.subTest(error=error):
                with patch.object(gate.subprocess, "run", side_effect=error):
                    result = gate.evaluate(self.data, self.root, execute=True)
                self.assertEqual("FAIL", result["verdict"])
                self.assertEqual(count, result["commands_started"])

    def test_decoding_failure_still_records_started_command(self):
        error = UnicodeDecodeError("utf-8", b"\xff", 0, 1, "invalid byte")
        with patch.object(gate.subprocess, "run", side_effect=error):
            result = gate.evaluate(self.data, self.root, execute=True)
        self.assertEqual("FAIL", result["verdict"])
        self.assertEqual(1, result["commands_started"])

    def test_timeout_retains_text_or_bytes_partial_output(self):
        for output in ["partial transcript", b"partial transcript"]:
            error = subprocess.TimeoutExpired(["go"], 180, output=output, stderr=output)
            with self.subTest(output=output):
                with patch.object(gate.subprocess, "run", side_effect=error):
                    result = gate.evaluate(self.data, self.root, execute=True)
                self.assertEqual("FAIL", result["verdict"])
                self.assertEqual("partial transcript", result["tests"][0]["stdout"])
                self.assertEqual("partial transcript", result["tests"][0]["stderr"])

    def test_bounded_profile_can_leave_out_of_scope_obligations_open(self):
        row = copy.deepcopy(self.data["requirements"][0])
        row.update(id="OUTSIDE", status="unresolved", tests=[])
        self.data["requirements"].append(row)
        result, _ = self.run_gate()
        self.assertEqual("PASS", result["verdict"])

    def test_profile_requirement_must_be_covered(self):
        self.data["requirements"][0]["tests"] = []
        result, run = self.run_gate()
        self.assertEqual("FAIL", result["verdict"])
        run.assert_not_called()

    def test_strict_schema_rejects_unsafe_or_ambiguous_inputs(self):
        bad = []
        for field, value in [("claim", "certified"), ("schema_version", True),
                             ("tests", []), ("extra", True)]:
            data = manifest()
            data[field] = value
            bad.append(data)
        for package in ["./../outside", "./rti/...", "-exec=bad", "./rti/contract;whoami", "C:/repo", "./rti/../contract"]:
            data = manifest()
            data["tests"][0]["package"] = package
            bad.append(data)
        for name in [".*", "TestContract|TestOther", "TestContract;whoami", ""]:
            data = manifest()
            data["tests"][0]["required_tests"] = [name]
            bad.append(data)
        for key, value in [("command", ["powershell", "-Command", "bad"]),
                           ("status", "pass"), ("required_tests", [])]:
            data = manifest()
            data["tests"][0][key] = value
            bad.append(data)
        data = manifest()
        data["tests"].append(copy.deepcopy(data["tests"][0]))
        bad.append(data)
        data = manifest()
        data["requirements"][0]["tests"] = ["UNKNOWN"]
        bad.append(data)
        for data in bad:
            with self.subTest(data=data):
                with self.assertRaises(gate.GateError):
                    gate.validate_manifest(data)

    def test_evidence_paths_cannot_escape_repository(self):
        self.full_fixture()
        self.data["independent_evidence"][0]["path"] = "../outside.txt"
        result, run = self.run_gate()
        self.assertEqual("FAIL", result["verdict"])
        run.assert_not_called()

    def test_duplicate_keys_and_nonfinite_json_are_rejected(self):
        path = self.root / "bad.json"
        for payload in ['{"claim":"full-2010","claim":"supported-profile"}',
                        '{"value": NaN}', "{", "null"]:
            path.write_text(payload, encoding="utf-8")
            with self.subTest(payload=payload):
                with self.assertRaises(gate.GateError):
                    gate.load_manifest(path)

    def test_cli_check_default_dry_run_and_full_failure_exit_codes(self):
        path = self.root / "manifest.json"
        path.write_text(json.dumps(self.data), encoding="utf-8")
        for flags, expected_code, verdict in [([], 2, "CHECK_ONLY"),
                                               (["--check"], 2, "CHECK_ONLY"),
                                               (["--dry-run"], 2, "CHECK_ONLY"),
                                               (["--claim", "full-2010"], 1, "FAIL")]:
            with self.subTest(flags=flags):
                completed = subprocess.run([sys.executable, "-B", str(SCRIPT),
                                            "--root", str(self.root), "--manifest", str(path), *flags],
                                           capture_output=True, text=True, check=False)
                self.assertEqual(expected_code, completed.returncode, completed.stderr)
                report = json.loads(completed.stdout)
                self.assertEqual(verdict, report["verdict"])
                self.assertEqual(0, report["commands_started"])


if __name__ == "__main__":
    unittest.main()
