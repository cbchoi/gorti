# ruff: noqa: S101

from __future__ import annotations

import tempfile
from pathlib import Path

from verification.check_service_usage import REQUIRED, inspect


def test_checker_requires_every_service_method() -> None:
    with tempfile.TemporaryDirectory(dir=Path(__file__).resolve().parent) as temporary:
        root = Path(temporary)
        methods = "\n".join(
            f"def {method}(self):\n    self.api.{method}()"
            for required in REQUIRED.values()
            for method in required
        )
        (root / "scenario.py").write_text(methods, encoding="utf-8")
        assert inspect(root, "python")["passed"] is True

        (root / "scenario.py").write_text(
            "def createFederationExecution(): pass\n", encoding="utf-8"
        )
        assert inspect(root, "python")["passed"] is False
