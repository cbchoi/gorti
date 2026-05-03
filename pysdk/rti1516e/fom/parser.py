"""FOM XML parser. Agent C implements per TASK-061.

Same diagnostic codes as Agent B's Go parser
(rti/pkg/fom/parser/) — FOM-001 through FOM-101. The fixtures in
tests/conformance/foms/{good,bad}/ drive both implementations.

Spec test: pysdk/tests/spec/m4/test_spec_m4_fom_diagnostics.py
parametrizes over the bad-FOM fixtures and asserts the matching code is
emitted; pysdk/tests/spec/m4/test_spec_m4_fom_acceptance.py iterates
the good-FOM fixtures and asserts zero diagnostics.
"""

from __future__ import annotations

from dataclasses import dataclass, field
from pathlib import Path

from rti1516e.fom.model import FOM


@dataclass(frozen=True)
class Diagnostic:
    """A single parser diagnostic with the FOM-NNN code."""

    code: str  # e.g. "FOM-001"
    message: str  # human-readable
    source: str  # file path or module name
    line: int = 0  # 1-based; 0 = unknown


@dataclass
class ParseResult:
    """Result of parse(). When diagnostics is non-empty, fom is None."""

    fom: FOM | None = None
    diagnostics: list[Diagnostic] = field(default_factory=list)

    def has_code(self, code: str) -> bool:
        """True iff any diagnostic carries ``code``."""
        return any(d.code == code for d in self.diagnostics)


def parse(modules: list[str | Path]) -> ParseResult:
    """Parse a list of FOM module file paths and return a ParseResult.

    On success: ParseResult.fom is the merged FOM (MIM + user modules);
    diagnostics is empty.

    On failure: ParseResult.fom is None; diagnostics carries one entry
    per problem found, each with a FOM-NNN code matching the Go side.

    Raises NotImplementedError until TASK-061 wires the parser.
    """
    raise NotImplementedError("TASK-061")
