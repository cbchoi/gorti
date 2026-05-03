"""Narrow unit tests for the FOM parser (TASK-061).

These complement pysdk/tests/spec/m4/test_spec_m4_fom_diagnostics.py — that
suite drives the FOM-NNN-named XML fixtures end-to-end against the matching
codes. The unit tests below exercise pieces of parser.py that the fixtures
don't naturally reach (empty-input, multi-module merging, MIM presence in
the success path).
"""

from __future__ import annotations

from pathlib import Path

import pytest

from rti1516e.fom import (
    BasicData,
    Diagnostic,
    ParseResult,
    parse,
)

REPO_ROOT = Path(__file__).resolve().parents[2]
FOMS_GOOD_DIR = REPO_ROOT / "tests" / "conformance" / "foms" / "good"
FOMS_BAD_DIR = REPO_ROOT / "tests" / "conformance" / "foms" / "bad"


def test_parse_empty_module_list_yields_mim_only_fom() -> None:
    """parse([]) should still load the MIM and return a non-None FOM."""
    result = parse([])
    assert isinstance(result, ParseResult)
    assert not result.diagnostics
    assert result.fom is not None
    # MIM ships HLAfloat64BE and HLAobjectRoot — both must be reachable.
    assert result.fom.find_data_type("HLAfloat64BE") is not None
    assert result.fom.find_object_class("HLAobjectRoot") is not None
    assert result.fom.find_interaction_class("HLAinteractionRoot") is not None


def test_parse_minimal_good_fom_includes_user_classes() -> None:
    """Good FOM survives parse and the user's classes appear in the result."""
    result = parse([FOMS_GOOD_DIR / "minimal.xml"])
    assert not result.diagnostics, result.diagnostics
    assert result.fom is not None
    assert result.fom.find_object_class("Vehicle") is not None
    assert result.fom.find_interaction_class("Honk") is not None


def test_parse_good_fom_data_types_are_mim_provided() -> None:
    """Good FOM reuses MIM data types; they should be present and BasicData."""
    result = parse([FOMS_GOOD_DIR / "minimal.xml"])
    assert result.fom is not None
    found = result.fom.find_data_type("HLAfloat64BE")
    assert isinstance(found, BasicData)


def test_parse_bad_fom_returns_none_fom_with_diagnostics() -> None:
    """Any bad fixture should return ParseResult with fom=None."""
    result = parse([FOMS_BAD_DIR / "FOM-001-undefined-datatype.xml"])
    assert result.fom is None
    assert result.has_code("FOM-001")


@pytest.mark.parametrize(
    ("fixture_name", "expected_code"),
    [
        ("FOM-001-undefined-datatype.xml", "FOM-001"),
        ("FOM-002-cyclic-class-hierarchy.xml", "FOM-002"),
        ("FOM-003-multiple-parents.xml", "FOM-003"),
        ("FOM-004-duplicate-attribute.xml", "FOM-004"),
        ("FOM-005-duplicate-parameter.xml", "FOM-005"),
        ("FOM-009-unknown-element.xml", "FOM-009"),
        ("FOM-011-missing-parent-class.xml", "FOM-011"),
        ("FOM-012-missing-interaction-parent.xml", "FOM-012"),
        ("FOM-013-variant-no-discriminator.xml", "FOM-013"),
        ("FOM-101-redefines-mim-type.xml", "FOM-101"),
    ],
)
def test_each_bad_fixture_emits_its_code(
    fixture_name: str, expected_code: str
) -> None:
    """Same shape as the spec test, kept in pysdk/tests/ for narrower runs."""
    result = parse([FOMS_BAD_DIR / fixture_name])
    assert result.fom is None
    assert result.has_code(expected_code), [d.code for d in result.diagnostics]


def test_parse_diagnostic_is_frozen_dataclass() -> None:
    """Diagnostic must be hashable and frozen — downstream tools rely on it."""
    d = Diagnostic(code="FOM-001", message="x", source="y", line=3)
    with pytest.raises(Exception):  # noqa: B017 - dataclasses.FrozenInstanceError
        d.code = "FOM-002"  # type: ignore[misc]
    assert hash(d) == hash(Diagnostic(code="FOM-001", message="x", source="y", line=3))


def test_parse_unknown_path_records_internal_diagnostic() -> None:
    """Missing file should not crash; surfaces as a diagnostic."""
    result = parse([REPO_ROOT / "definitely-not-a-real-fom.xml"])
    assert result.fom is None
    assert any(d.code == "FOM-INTERNAL" for d in result.diagnostics)
