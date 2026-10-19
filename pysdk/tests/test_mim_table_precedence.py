"""IEEE 1516.1-2010 Clause 11.6 precedence, without rewriting source XML."""

from pathlib import Path

import pytest

from rti1516e.fom.modules import MIMModule
from rti1516e.fom.parser import _parse_sources

ROOT = Path(__file__).resolve().parents[2]
MIM = ROOT / "rti/pkg/fom/mim/standard-mim.xml"


@pytest.mark.parametrize("spelling", [None, "HLAnextSaveName", "HLAAnnexSaveName"])
def test_mim_table_name_keeps_attribute_position(spelling):
    selected = None
    if spelling is not None:
        selected = MIMModule("selected.xml", MIM.read_bytes().replace(
            b"<name>HLAnextSaveName</name>", f"<name>{spelling}</name>".encode(), 1))
    result = _parse_sources([], mim_module=selected)
    assert result.diagnostics == []
    cls = next(c for c in result.fom.object_classes
               if c.key == "HLAobjectRoot.HLAmanager.HLAfederation")
    assert len(cls.attributes) == 12
    assert cls.attributes[9].name == "HLAAnnexSaveName"
    assert cls.attributes[9].data_type == "HLAunicodeString"
    assert cls.attributes[10].name == "HLAnextSaveTime"
    if selected is not None:
        assert result.fom.mim_module == selected


def test_mim_table_rule_does_not_rename_user_attributes():
    source = b"""<objectModel><objects><objectClass><name>HLAobjectRoot</name>
    <objectClass><name>UserObject</name><attribute><name>HLAnextSaveName</name>
    <dataType>HLAunicodeString</dataType></attribute></objectClass>
    </objectClass></objects></objectModel>"""
    result = _parse_sources([("user.xml", source)])
    assert result.diagnostics == []
    cls = next(c for c in result.fom.object_classes if c.name == "UserObject")
    assert cls.attributes[0].name == "HLAnextSaveName"
