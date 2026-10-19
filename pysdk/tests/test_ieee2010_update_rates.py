"""IEEE 1516.2-2010 4.11.2 / 6.2.11: maximum update rates are positive."""

from decimal import Decimal, localcontext
from pathlib import Path

import pytest

from rti1516e.fom import model
from rti1516e.fom.modules import MIMModule
from rti1516e.fom.parser import _merge_update_rates, _parse_sources, parse


def module(rate: str, semantics: str = "", name: str = "Rate") -> bytes:
    return (
        '<objectModel xmlns="http://standards.ieee.org/IEEE1516-2010">'
        f"<updateRates><updateRate><name>{name}</name><rate>{rate}</rate>"
        f"{semantics}</updateRate></updateRates></objectModel>"
    ).encode()


@pytest.mark.parametrize("rate", ["0", "-0.000", "-0.25"])
@pytest.mark.parametrize("entry", ["file", "hydration", "mim"])
def test_nonpositive_rates_rejected(rate: str, entry: str, tmp_path: Path) -> None:
    source = str(tmp_path / "rate.xml")
    if entry == "file":
        Path(source).write_bytes(module(rate))
        result = parse([source])
    elif entry == "mim":
        result = _parse_sources([], mim_module=MIMModule(source, module(rate)))
    else:
        result = _parse_sources([(source, module(rate))])
    assert result.fom is None
    assert result.has_code("FOM-016")
    assert any(d.source == source and "greater than zero" in d.message for d in result.diagnostics)


@pytest.mark.parametrize("rate", ["", "NaN", "INF", "1e-3", "1_000", "1/2"])
def test_rate_lexical_parity(rate: str) -> None:
    result = _parse_sources([("rate.xml", module(rate))])
    assert result.fom is None
    assert result.has_code("FOM-016")


@pytest.mark.parametrize("rate", [
    "0.00000000000000000000000000000000000001",
    "0." + "0" * 323 + "5",
    "1.0000000000000000000000000001",
    "+12.500", ".25",
])
def test_positive_rates_preserve_exact_decimal(rate: str, tmp_path: Path) -> None:
    source = tmp_path / "positive.xml"
    source.write_bytes(module(rate, "<semantics>  exact &amp; retained  </semantics>"))
    with localcontext() as context:
        context.prec = 3
        result = parse([source])
    assert not result.diagnostics
    assert result.fom is not None
    retained = result.fom.update_rates[0]
    assert retained.rate == Decimal(rate)
    assert retained.semantics == "  exact & retained  "


@pytest.mark.parametrize("rate", ["0", "-0.000", "-0.25", "NaN", "Infinity"])
def test_model_rejects_nonpositive_or_nonfinite_rate(rate: str) -> None:
    with pytest.raises(ValueError):
        model.UpdateRate(name="Invalid", rate=Decimal(rate))


@pytest.mark.parametrize("first,second,expected", [
    ("", "<semantics> later </semantics>", " later "),
    ("<semantics/>", "<semantics>later</semantics>", ""),
    ("<semantics>first</semantics>", "<semantics>later</semantics>", "first"),
])
def test_equivalent_rates_retain_first_present_description(
    first: str, second: str, expected: str,
) -> None:
    result = _parse_sources([
        ("first.xml", module("1.000", first)), ("second.xml", module("+1.0", second)),
    ])
    assert not result.diagnostics
    assert result.fom is not None
    assert len(result.fom.update_rates) == 1
    assert result.fom.update_rates[0].rate == Decimal(1)
    assert result.fom.update_rates[0].semantics == expected


@pytest.mark.parametrize("incoming", ["0", "-0.000", "-0.25", "1.00000000000000000002"])
def test_invalid_or_conflicting_merge_is_atomic(incoming: str) -> None:
    source = ("base.xml", module("1.00000000000000000001"))
    before = _parse_sources([source])
    assert before.fom is not None
    expected = before.fom.update_rates
    next_source = ("next.xml", module(incoming))
    for sources in ([source, next_source], [next_source, source]):
        result = _parse_sources(sources)
        assert result.fom is None
        assert result.has_code("FOM-016")
    after = _parse_sources([source])
    assert after.fom is not None
    assert after.fom.update_rates == expected


def test_selected_mim_rates_retained_and_conflicts_rejected() -> None:
    mim = MIMModule("selected.xml", module("1.00000000000000000001", "<semantics/>"))
    result = _parse_sources([("user.xml", module("2", name="Other"))], mim_module=mim)
    assert not result.diagnostics
    assert result.fom is not None
    assert [r.name for r in result.fom.update_rates] == ["Other", "Rate"]
    conflict = _parse_sources([("user.xml", module("1.00000000000000000002"))], mim_module=mim)
    assert conflict.fom is None
    assert conflict.has_code("FOM-016")


@pytest.mark.parametrize("rate,message", [
    ("0." + "0" * 323 + "1", "supported float64 range"),
    ("-0." + "0" * 323 + "1", "greater than zero"),
    ("1" + "0" * 309, "supported float64 range"),
])
def test_range_limit_is_separate_from_exact_positivity(rate: str, message: str) -> None:
    result = _parse_sources([("rate.xml", module(rate))])
    assert result.fom is None
    assert result.has_code("FOM-016")
    assert message in result.diagnostics[0].message


@pytest.mark.parametrize("rate", ["1.0", "1.00000000000000000001"])
def test_same_module_duplicate_rates_compare_exact_values(rate: str) -> None:
    import xml.etree.ElementTree as ET

    document = ET.fromstring(module("1"))  # noqa: S314 - local test vectors
    document[0].append(ET.fromstring(module(rate))[0][0])  # noqa: S314 - local test vectors
    result = _parse_sources([("duplicate.xml", ET.tostring(document))])
    if Decimal(rate) == 1:
        assert not result.diagnostics
        assert result.fom is not None
        assert len(result.fom.update_rates) == 1
    else:
        assert result.fom is None
        assert result.has_code("FOM-016")


def test_failed_merge_does_not_mutate_either_input() -> None:
    base = [model.UpdateRate("Rate", Decimal("1.00000000000000000001"))]
    incoming = [
        model.UpdateRate("Rate", base[0].rate, "fill missing description"),
        model.UpdateRate("Added", Decimal(2)),
        model.UpdateRate("Rate", Decimal("1.00000000000000000002")),
    ]
    original_base, original_incoming = base.copy(), incoming.copy()
    rates, diagnostics = _merge_update_rates(base, incoming, "next.xml")
    assert rates == []
    assert diagnostics[0].code == "FOM-016"
    assert base == original_base
    assert incoming == original_incoming


def test_absent_rates_and_missing_name() -> None:
    empty = _parse_sources([("empty.xml", b"<objectModel/>")])
    assert empty.fom is not None
    assert empty.fom.update_rates == ()
    invalid = _parse_sources([("rate.xml", module("1", name=" "))])
    assert invalid.fom is None
    assert invalid.has_code("FOM-016")
