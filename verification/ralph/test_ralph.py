from __future__ import annotations

import json
import subprocess  # noqa: S404
import sys
import tempfile
import unittest
from pathlib import Path

HERE = Path(__file__).resolve().parent
RUNNER = HERE / "ralph.py"


class RalphIntegrationTests(unittest.TestCase):
    def invoke(
        self,
        output: Path,
        pitch_args: list[str],
        gorti_args: list[str],
        *extra: str,
    ) -> subprocess.CompletedProcess[str]:
        command = [
            sys.executable,
            str(RUNNER),
            "--output-dir",
            str(output),
            "--pitch-command",
            sys.executable,
            "--pitch-args-json",
            json.dumps(pitch_args),
            "--gorti-command",
            sys.executable,
            "--gorti-args-json",
            json.dumps(gorti_args),
            *extra,
        ]
        return subprocess.run(  # noqa: S603
            command, check=False, capture_output=True, text=True
        )

    def test_matching_runs_complete_after_one_iteration(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary) / "artifacts"
            result = self.invoke(
                output,
                ["-c", "import os; print(os.environ['RALPH_SEED'])"],
                ["-c", "import os; print(os.environ['RALPH_SEED'])"],
                "--seed",
                "73",
                "--max-iterations",
                "3",
            )

            self.assertEqual(result.returncode, 0, result.stderr)
            summary = json.loads((output / "summary.json").read_text(encoding="utf-8"))
            self.assertEqual(summary["decision"], "complete")
            self.assertEqual(summary["iterations"], 1)
            self.assertEqual(
                (output / "iteration-001" / "pitch.log").read_text(encoding="utf-8"),
                "73\n",
            )

    def test_mismatch_retries_only_to_maximum(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary) / "artifacts"
            result = self.invoke(
                output,
                ["-c", "print('pitch')"],
                ["-c", "print('gorti')"],
                "--max-iterations",
                "2",
            )

            self.assertEqual(result.returncode, 1, result.stderr)
            self.assertTrue((output / "iteration-001" / "review.diff").is_file())
            reflection = json.loads(
                (output / "iteration-001" / "reflect.json").read_text(encoding="utf-8")
            )
            final_reflection = json.loads(
                (output / "iteration-002" / "reflect.json").read_text(encoding="utf-8")
            )
            self.assertEqual(reflection["decision"], "retry")
            self.assertEqual(final_reflection["decision"], "stop")
            self.assertFalse((output / "iteration-003").exists())

    def test_arguments_are_not_evaluated_by_a_shell(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary) / "artifacts"
            literal = "value; echo NOT_EVALUATED"
            child_args = ["-c", "import sys; print(sys.argv[1])", literal]
            result = self.invoke(output, child_args, child_args)

            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(
                (output / "iteration-001" / "pitch.log").read_text(encoding="utf-8"),
                literal + "\n",
            )

    def test_missing_executable_stops_in_plan(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary) / "artifacts"
            command = [
                sys.executable,
                str(RUNNER),
                "--output-dir",
                str(output),
                "--pitch-command",
                "definitely-missing-ralph-command",
                "--gorti-command",
                sys.executable,
            ]
            result = subprocess.run(  # noqa: S603
                command, check=False, capture_output=True, text=True
            )

            self.assertEqual(result.returncode, 2, result.stderr)
            plan = json.loads(
                (output / "iteration-001" / "plan.json").read_text(encoding="utf-8")
            )
            self.assertEqual(plan["status"], "blocked")
            self.assertFalse((output / "iteration-001" / "pitch.log").exists())

    def test_gorti_still_runs_when_pitch_fails(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary) / "artifacts"
            result = self.invoke(
                output,
                ["-c", "import sys; print('pitch failed'); sys.exit(7)"],
                ["-c", "print('gorti ran')"],
                "--max-iterations",
                "1",
            )

            self.assertEqual(result.returncode, 1, result.stderr)
            process = json.loads(
                (output / "iteration-001" / "gorti.process.json").read_text(
                    encoding="utf-8"
                )
            )
            self.assertEqual(process["exit_code"], 0)
            self.assertEqual(
                (output / "iteration-001" / "gorti.log").read_text(encoding="utf-8"),
                "gorti ran\n",
            )

    def test_canonical_logs_use_semantic_review_and_log_placeholder(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary) / "artifacts"
            script = (
                "import json,pathlib,sys; "
                "records=[{'kind':'semantic','service':s,'event':'ok','actor':'a',"
                "'data':{'implementation':sys.argv[2]}} for s in ['FM','DM','OM','TM']]; "
                "pathlib.Path(sys.argv[1]).write_text("
                "''.join(json.dumps(r)+'\\n' for r in records),encoding='utf-8')"
            )
            result = self.invoke(
                output,
                ["-c", script, "{log}", "pitch"],
                ["-c", script, "{log}", "gorti"],
                "--max-iterations",
                "1",
            )

            self.assertEqual(result.returncode, 0, result.stderr)
            review = json.loads(
                (output / "iteration-001" / "review.json").read_text(encoding="utf-8")
            )
            self.assertEqual(review["comparison_mode"], "semantic")
            self.assertTrue(review["passed"])
            self.assertFalse(review["logs_equal"])

    def test_timed_out_processes_are_recorded_and_bounded(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary) / "artifacts"
            child_args = ["-c", "import time; time.sleep(10)"]
            result = self.invoke(
                output,
                child_args,
                child_args,
                "--timeout",
                "0.1",
                "--max-iterations",
                "1",
            )

            self.assertEqual(result.returncode, 1, result.stderr)
            for role in ("pitch", "gorti"):
                process = json.loads(
                    (output / "iteration-001" / f"{role}.process.json").read_text(
                        encoding="utf-8"
                    )
                )
                self.assertEqual(process["status"], "timed_out")

    def test_nonempty_output_directory_is_not_overwritten(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary) / "artifacts"
            output.mkdir()
            sentinel = output / "keep.txt"
            sentinel.write_text("keep", encoding="utf-8")
            child_args = ["-c", "print('same')"]
            result = self.invoke(output, child_args, child_args)

            self.assertEqual(result.returncode, 2, result.stderr)
            self.assertEqual(sentinel.read_text(encoding="utf-8"), "keep")
            self.assertFalse((output / "run.json").exists())


if __name__ == "__main__":
    unittest.main()
