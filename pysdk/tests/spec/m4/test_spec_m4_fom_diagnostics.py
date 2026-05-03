"""FOM parser diagnostics — same FOM-NNN codes as Agent B's Go parser.

Bad-FOM fixtures live in tests/conformance/foms/bad/ and are shared with
Agent B's M1 spec tests. For each fixture, the Python parser must emit
the matching FOM-NNN code and return parser.fom == None.

Implements: FR-FOM-1 (Python side).
"""

from __future__ import annotations

from pathlib import Path

import pytest

from rti1516e.fom import parse

from .conftest import FOMS_BAD_DIR, FOMS_GOOD_DIR

# Map fixture filename prefix -> expected FOM-NNN code.
# Fixtures are named "FOM-NNN-<slug>.xml"; the prefix determines the code.
_BAD_FIXTURES: list[tuple[Path, str]] = sorted(
    (
        (p, p.stem.split("-")[0] + "-" + p.stem.split("-")[1])
        for p in FOMS_BAD_DIR.glob("FOM-*.xml")
    ),
    key=lambda t: t[0].name,
)


@pytest.mark.spec
@pytest.mark.parametrize(("fixture", "expected_code"), _BAD_FIXTURES, ids=lambda v: str(v))
def test_spec_m4_bad_fom_emits_code(fixture: Path, expected_code: str) -> None:
    result = parse([fixture])
    assert result.has_code(expected_code), (
        f"fixture {fixture.name}: expected code {expected_code!r}; "
        f"got diagnostics={[d.code for d in result.diagnostics]}"
    )
    assert result.fom is None, f"fixture {fixture.name}: parser returned a FOM despite errors"


@pytest.mark.spec
@pytest.mark.parametrize(
    "fixture", sorted(FOMS_GOOD_DIR.glob("*.xml")), ids=lambda p: p.name
)
def test_spec_m4_good_fom_accepts(fixture: Path) -> None:
    result = parse([fixture])
    assert not result.diagnostics, (
        f"fixture {fixture.name}: expected zero diagnostics, got "
        f"{[(d.code, d.message) for d in result.diagnostics]}"
    )
    assert result.fom is not None, f"fixture {fixture.name}: parser returned no FOM"


@pytest.mark.spec
def test_spec_m4_bad_fom_fixtures_present() -> None:
    """Sanity: bad-FOM fixtures are reachable."""
    assert len(_BAD_FIXTURES) > 0, "no bad-FOM fixtures found"
