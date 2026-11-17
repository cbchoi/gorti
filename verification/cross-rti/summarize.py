"""Check published file hashes and recount JUnit outcomes without running an RTI."""

import argparse
import hashlib
import json
from pathlib import Path
import xml.etree.ElementTree as ET
import subprocess


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--git-index", action="store_true", help="Also verify bytes staged for publication")
    args = parser.parse_args()
    root = Path(__file__).resolve().parent / "results"
    checked = 0
    for run in sorted(root.iterdir()):
        manifest = json.loads((run / "publication-manifest.json").read_text())
        for entry in manifest["files"]:
            payload = (run / entry["path"]).read_bytes()
            if hashlib.sha256(payload).hexdigest() != entry["public_sha256"]:
                raise ValueError("Public hash mismatch: " + str(run / entry["path"]))
            if args.git_index:
                repo = root.parents[2]
                relative = (run / entry["path"]).relative_to(repo).as_posix()
                staged = subprocess.check_output(["git", "show", ":" + relative], cwd=repo)
                if staged != payload:
                    raise ValueError("Staged bytes differ: " + relative)
            checked += 1
        recorded = json.loads((run / "results.json").read_text())["runs"]
        actual = []
        for item in recorded:
            cases = []
            for report in (run / item["class"] / "reports").glob("TEST-*.xml"):
                for case in ET.parse(report).getroot().iter("testcase"):
                    status = "fail" if case.find("failure") is not None or case.find("error") is not None else (
                        "skip" if case.find("skipped") is not None else "pass")
                    cases.append((case.get("classname"), case.get("name"), status))
            expected = [(case["class"], case["name"], case["status"]) for case in item["outcomes"]]
            if sorted(cases) != sorted(expected):
                raise ValueError("JUnit/summary mismatch: " + str(run))
            actual.extend(cases)
        counts = {state: sum(row[2] == state for row in actual) for state in ("pass", "fail", "skip")}
        print(run.name, json.dumps(counts))
    print(f"Verified {checked} published file hashes and all recorded outcome identities")


if __name__ == "__main__":
    main()
