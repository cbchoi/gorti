from __future__ import annotations

import argparse
import ast
import json
import re
from pathlib import Path
from typing import Any

REQUIRED: dict[str, tuple[str, ...]] = {
    "FM": (
        "createFederationExecution",
        "joinFederationExecution",
        "registerFederationSynchronizationPoint",
        "synchronizationPointAchieved",
        "resignFederationExecution",
        "destroyFederationExecution",
    ),
    "DM": (
        "publishObjectClassAttributes",
        "subscribeObjectClassAttributes",
        "publishInteractionClass",
        "subscribeInteractionClass",
    ),
    "OM": (
        "reserveObjectInstanceName",
        "registerObjectInstance",
        "updateAttributeValues",
        "sendInteraction",
        "discoverObjectInstance",
        "reflectAttributeValues",
        "receiveInteraction",
        "removeObjectInstance",
    ),
    "TM": (
        "enableTimeRegulation",
        "enableTimeConstrained",
        "timeAdvanceRequest",
        "timeAdvanceGrant",
    ),
}


def _python_methods(paths: list[Path]) -> set[str]:
    methods: set[str] = set()
    for path in paths:
        tree = ast.parse(path.read_text(encoding="utf-8"), filename=str(path))
        for node in ast.walk(tree):
            if isinstance(node, ast.Attribute):
                methods.add(node.attr)
            elif isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef)):
                methods.add(node.name)
    return methods


def _java_methods(paths: list[Path]) -> set[str]:
    methods: set[str] = set()
    for path in paths:
        source = path.read_text(encoding="utf-8", errors="replace")
        source = re.sub(r"/\*.*?\*/|//[^\n]*", "", source, flags=re.DOTALL)
        methods.update(re.findall(r"\b([A-Za-z_$][\w$]*)\s*\(", source))
    return methods


def inspect(root: Path, language: str) -> dict[str, Any]:
    if language == "python":
        paths = sorted(root.rglob("*.py"))
        methods = _python_methods(paths)
    else:
        paths = sorted(root.rglob("*.java"))
        methods = _java_methods(paths)
    groups = {
        service: {
            "passed": all(method in methods for method in required),
            "methods": {method: method in methods for method in required},
        }
        for service, required in REQUIRED.items()
    }
    return {
        "language": language,
        "root": str(root),
        "files": [str(path) for path in paths],
        "groups": groups,
        "passed": bool(paths) and all(group["passed"] for group in groups.values()),
    }


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument(
        "--pitch", type=Path, default=Path(__file__).resolve().parent / "pitch"
    )
    parser.add_argument(
        "--gorti", type=Path, default=Path(__file__).resolve().parent / "gorti"
    )
    parser.add_argument("--report", type=Path)
    args = parser.parse_args()
    report = {
        "pitch": inspect(args.pitch, "java"),
        "gorti": inspect(args.gorti, "python"),
    }
    report["passed"] = report["pitch"]["passed"] and report["gorti"]["passed"]
    rendered = json.dumps(report, indent=2, sort_keys=True)
    if args.report:
        args.report.parent.mkdir(parents=True, exist_ok=True)
        args.report.write_text(rendered + "\n", encoding="utf-8")
    print(rendered)
    return 0 if report["passed"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
