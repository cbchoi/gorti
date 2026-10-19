"""Validation must propagate failures and stop the server it starts."""

import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import MagicMock, patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import validate_sdks as runner


class RunnerTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.artifacts = Path(self.directory.name)
        self.binary = self.artifacts / "rtid"
        self.binary.write_bytes(b"current test binary")

    def test_environment_selects_current_binary_and_live_tests(self):
        with patch.dict(os.environ, {"GORTI_RTID": "stale", "GORTI_RESEARCH_CONFIG": "stale"}):
            env = runner.test_environment(self.binary, self.artifacts)
        self.assertEqual(env["GORTI_RTID"], str(self.binary))
        self.assertEqual(env["GORTI_LIVE_RTID"], str(self.binary))
        self.assertEqual(env["GORTI_RUN_LIVE_E2E"], "1")
        self.assertEqual(env["GORTI_RESEARCH_CONFIG"], "")
        self.assertEqual(len(env["GORTI_RTID_SHA256"]), 64)

    def test_failed_command_is_not_suppressed(self):
        with patch.object(
            runner.subprocess, "run", side_effect=subprocess.CalledProcessError(9, "test")
        ), self.assertRaises(subprocess.CalledProcessError):
            runner.run(["test"], {})

    def test_missing_tool_is_an_error(self):
        with (
            patch.object(runner.shutil, "which", return_value=None),
            self.assertRaisesRegex(RuntimeError, "Required executable"),
        ):
            runner.tool("mvn")

    def test_server_is_stopped_after_test_failure(self):
        process = MagicMock()
        process.poll.return_value = None
        with (
            patch.object(runner.subprocess, "Popen", return_value=process),
            patch.object(runner.socket, "create_connection", return_value=MagicMock()),
            self.assertRaisesRegex(RuntimeError, "test failed"),
            runner.local_server(self.binary, self.artifacts, {}),
        ):
            raise RuntimeError("test failed")
        process.terminate.assert_called_once()
        process.wait.assert_called_once_with(timeout=10)

    def test_server_start_failure_is_reported_and_reaped(self):
        process = MagicMock()
        process.poll.return_value = 1
        with (
            patch.object(runner.subprocess, "Popen", return_value=process),
            self.assertRaisesRegex(RuntimeError, "rtid exited"),
            runner.local_server(self.binary, self.artifacts, {}),
        ):
            self.fail("must not enter tests")
        process.wait.assert_called_once_with(timeout=10)

    def test_stuck_server_is_killed_and_reaped(self):
        process = MagicMock()
        process.poll.return_value = None
        process.wait.side_effect = [subprocess.TimeoutExpired("rtid", 10), 0]
        with (
            patch.object(runner.subprocess, "Popen", return_value=process),
            patch.object(runner.socket, "create_connection", return_value=MagicMock()),
            runner.local_server(self.binary, self.artifacts, {}),
        ):
            pass
        process.kill.assert_called_once()
        self.assertEqual(process.wait.call_count, 2)

    def test_python_uses_inventory_and_produces_junit(self):
        with (
            patch.object(runner, "inventory", return_value=["pysdk/tests/test_sdk.py"]),
            patch.object(runner, "run") as run,
        ):
            runner.validate_python({}, self.artifacts)
        command = run.call_args.args[0]
        self.assertIn("pysdk/tests/test_sdk.py", command)
        self.assertIn("no:cacheprovider", command)
        self.assertIn(f"--junitxml={self.artifacts / 'python.xml'}", command)


if __name__ == "__main__":
    unittest.main()
