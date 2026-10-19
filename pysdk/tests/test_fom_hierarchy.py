"""Qualified XML identity and branch-local inheritance regressions."""

from pathlib import Path

import pytest

from rti1516e.fom import InteractionClass, ObjectClass, parse
from rti1516e.fom.parser import (
    _diag_cycle,
    _diag_multiple_parents,
    _diag_object_parents,
    _ParsedModule,
)


def module(tmp_path: Path, name: str, xml: str) -> Path:
    path = tmp_path / name
    path.write_text(f"<objectModel>{xml}</objectModel>", encoding="utf-8")
    return path


def branch_xml(left: str, right: str) -> str:
    sections = []
    for section, cls, root, member in (
        ("objects", "objectClass", "HLAobjectRoot", "attribute"),
        ("interactions", "interactionClass", "HLAinteractionRoot", "parameter"),
    ):
        branches = []
        for branch, declared, leaf in (("Left", "leftValue", left), ("Right", "rightValue", right)):
            branches.append(
                f"<{cls}><name>{branch}</name><{cls}><name>Shared</name>"
                f"<{member}><name>{declared}</name><dataType>HLAinteger32BE</dataType></{member}>"
                f"<{cls}><name>Leaf</name>"
                f"<{member}><name>{leaf}</name><dataType>HLAinteger32BE</dataType></{member}>"
                f"</{cls}></{cls}></{cls}>"
            )
        sections.append(
            f"<{section}><{cls}><name>{root}</name>{''.join(branches)}</{cls}></{section}>"
        )
    return "".join(sections)


@pytest.mark.parametrize(
    ("left", "right", "bad_branches"),
    [
        ("rightValue", "leftValue", []),
        ("leftValue", "leftValue", ["Left"]),
        ("rightValue", "rightValue", ["Right"]),
        ("leftValue", "rightValue", ["Left", "Right"]),
    ],
)
def test_inherited_duplicates_follow_the_exact_branch(
    tmp_path: Path, left: str, right: str, bad_branches: list[str]
) -> None:
    result = parse([module(tmp_path, "branches.xml", branch_xml(left, right))])
    assert len(result.diagnostics) == 2 * len(bad_branches), result.diagnostics
    for code, root in (("FOM-004", "HLAobjectRoot"), ("FOM-005", "HLAinteractionRoot")):
        diagnostics = [d for d in result.diagnostics if d.code == code]
        assert len(diagnostics) == len(bad_branches)
        for branch in bad_branches:
            assert any(f"{root}.{branch}.Shared.Leaf" in d.message for d in diagnostics)
    assert (result.fom is not None) == (not bad_branches)
    if result.fom is None:
        return
    fom = result.fom
    assert fom.find_object_class("Leaf") is None
    assert fom.find_interaction_class("Shared") is None
    obj = fom.find_object_class("Left.Shared.Leaf")
    interaction = fom.find_interaction_class("HLAinteractionRoot.Right.Shared.Leaf")
    assert obj is not None and interaction is not None
    assert obj.name == "Leaf" and obj.parent == "Shared"
    assert obj.parent_key == "HLAobjectRoot.Left.Shared"
    assert fom.find_object_class("Left").name == "Left"
    assert fom.find_attribute(obj.key, "leftValue") is not None
    assert fom.find_attribute(obj.key, "missing") is None
    assert fom.find_parameter(interaction.key, "rightValue") is not None
    assert fom.find_parameter("Leaf", "rightValue") is None


def test_duplicate_sibling_identity_is_rejected(tmp_path: Path) -> None:
    xml = ""
    for section, cls, root in (
        ("objects", "objectClass", "HLAobjectRoot"),
        ("interactions", "interactionClass", "HLAinteractionRoot"),
    ):
        xml += (
            f"<{section}><{cls}><name>{root}</name>"
            + (f"<{cls}><name>Repeated</name></{cls}>" * 2)
            + f"</{cls}></{section}>"
        )
    result = parse([module(tmp_path, "siblings.xml", xml)])
    assert result.fom is None
    assert [d.code for d in result.diagnostics] == ["FOM-014", "FOM-014"]
    assert all("Repeated" in d.message for d in result.diagnostics)


def test_mim_local_names_do_not_capture_other_branches(tmp_path: Path) -> None:
    result = parse(
        [
            module(
                tmp_path,
                "names.xml",
                """
      <objects><objectClass><name>HLAobjectRoot</name>
        <objectClass><name>User</name><objectClass><name>HLAfederate</name>
          <attribute><name>value</name><dataType>HLAinteger32BE</dataType></attribute>
        </objectClass></objectClass>
      </objectClass></objects>
      <interactions><interactionClass><name>HLAinteractionRoot</name>
        <interactionClass><name>User</name><interactionClass><name>HLAreport</name>
          <parameter><name>value</name><dataType>HLAinteger32BE</dataType></parameter>
        </interactionClass></interactionClass>
      </interactionClass></interactions>
    """,
            )
        ]
    )
    assert not result.diagnostics, result.diagnostics
    fom = result.fom
    assert fom is not None
    assert fom.find_attribute("User.HLAfederate", "value") is not None
    assert fom.find_parameter("User.HLAreport", "value") is not None
    fed_report = "HLAmanager.HLAfederate.HLAreport.HLAreportFOMmoduleData"
    federation_report = "HLAmanager.HLAfederation.HLAreport.HLAreportFOMmoduleData"
    assert fom.find_interaction_class(fed_report) is not None
    assert fom.find_interaction_class(federation_report) is not None
    assert fom.find_parameter(fed_report, "HLAfederate") is not None
    assert fom.find_parameter(federation_report, "HLAfederate") is None


def test_multimodule_merge_uses_qualified_identity(tmp_path: Path) -> None:
    modules = []
    for branch in ("Left", "Right"):
        modules.append(
            module(
                tmp_path,
                f"{branch}.xml",
                f"""
          <objects><objectClass><name>HLAobjectRoot</name>
            <objectClass><name>{branch}</name><objectClass><name>Shared</name>
              <attribute><name>{branch}Value</name><dataType>HLAinteger32BE</dataType></attribute>
            </objectClass></objectClass>
          </objectClass></objects>
        """,
            )
        )
    result = parse(modules + [modules[0]])
    assert not result.diagnostics, result.diagnostics
    assert result.fom is not None
    assert result.fom.find_attribute("Left.Shared", "LeftValue") is not None
    assert result.fom.find_attribute("Left.Shared", "RightValue") is None
    assert result.fom.find_attribute("Right.Shared", "RightValue") is not None
    assert len([c for c in result.fom.object_classes if c.name == "Shared"]) == 2


def test_multimodule_conflict_is_not_silently_discarded(tmp_path: Path) -> None:
    paths = [
        module(
            tmp_path,
            f"{dtype}.xml",
            f"""
      <objects><objectClass><name>HLAobjectRoot</name>
        <objectClass><name>A</name>
          <attribute><name>value</name><dataType>{dtype}</dataType></attribute>
        </objectClass>
      </objectClass></objects>
    """,
        )
        for dtype in ("HLAinteger32BE", "HLAfloat64BE")
    ]
    result = parse(paths)
    assert result.fom is None
    assert result.has_code("FOM-014"), result.diagnostics


@pytest.mark.parametrize("member", ["unique", "inherited"])
def test_repeated_ancestor_name_retains_inheritance(tmp_path: Path, member: str) -> None:
    result = parse(
        [
            module(
                tmp_path,
                "ancestor.xml",
                f"""
      <objects><objectClass><name>HLAobjectRoot</name>
        <objectClass><name>A</name>
          <attribute><name>inherited</name><dataType>HLAinteger32BE</dataType></attribute>
          <objectClass><name>A</name>
            <attribute><name>{member}</name><dataType>HLAinteger32BE</dataType></attribute>
          </objectClass>
        </objectClass>
      </objectClass></objects>
    """,
            )
        ]
    )
    if member == "inherited":
        assert [d.code for d in result.diagnostics] == ["FOM-004"]
        assert "HLAobjectRoot.A.A" in result.diagnostics[0].message
        assert "also declared on 'HLAobjectRoot.A'" in result.diagnostics[0].message
    else:
        assert not result.diagnostics, result.diagnostics
        assert result.fom is not None
        assert result.fom.find_attribute("A.A", "inherited") is not None


def test_cross_module_inherited_member_collision_is_rejected(tmp_path: Path) -> None:
    parent = module(
        tmp_path,
        "parent.xml",
        """
      <objects><objectClass><name>HLAobjectRoot</name><objectClass><name>A</name>
        <attribute><name>value</name><dataType>HLAinteger32BE</dataType></attribute>
      </objectClass></objectClass></objects>
    """,
    )
    child = module(
        tmp_path,
        "child.xml",
        """
      <objects><objectClass><name>HLAobjectRoot</name><objectClass><name>A</name>
        <objectClass><name>B</name>
          <attribute><name>value</name><dataType>HLAinteger32BE</dataType></attribute>
        </objectClass>
      </objectClass></objectClass></objects>
    """,
    )
    for paths in ([parent, child], [child, parent]):
        result = parse(paths)
        assert result.fom is None
        assert [d.code for d in result.diagnostics] == ["FOM-004"]
        assert result.diagnostics[0].source == "<merged>"


def test_exact_mim_class_redefinition_still_rejected(tmp_path: Path) -> None:
    result = parse(
        [
            module(
                tmp_path,
                "mim.xml",
                """
      <objects><objectClass><name>HLAobjectRoot</name>
        <objectClass><name>HLAmanager</name><objectClass><name>HLAfederate</name>
          <attribute><name>newMember</name><dataType>HLAinteger32BE</dataType></attribute>
        </objectClass></objectClass>
      </objectClass></objects>
    """,
            )
        ]
    )
    assert result.fom is None
    assert [d.code for d in result.diagnostics] == ["FOM-101"]
    assert "HLAobjectRoot.HLAmanager.HLAfederate" in result.diagnostics[0].message


def test_explicit_graph_diagnostics_are_retained() -> None:
    cycle = _ParsedModule(object_classes=[ObjectClass("A", "B"), ObjectClass("B", "A")])
    assert any(d.code == "FOM-002" for d in _diag_cycle(cycle, "graph"))
    multiple = _ParsedModule(object_classes=[ObjectClass("A", "Left"), ObjectClass("A", "Right")])
    assert [d.code for d in _diag_multiple_parents(multiple, "graph")] == ["FOM-003"]


def test_qualified_parent_cannot_resolve_from_another_branch() -> None:
    pm = _ParsedModule(
        object_classes=[
            ObjectClass("Shared", "Left", qualified_name="HLAobjectRoot.Left.Shared"),
            ObjectClass("Leaf", "Shared", qualified_name="HLAobjectRoot.Right.Shared.Leaf"),
        ]
    )
    diagnostics = _diag_object_parents(pm, "graph")
    assert len(diagnostics) == 2
    assert all(d.code == "FOM-011" for d in diagnostics)
    assert any("HLAobjectRoot.Right.Shared.Leaf" in d.message for d in diagnostics)


def test_programmatic_local_name_models_remain_compatible() -> None:
    assert ObjectClass("A", "Root").key == "A"
    assert ObjectClass("A", "Root").parent_key == "Root"
    assert InteractionClass("A", "Root").key == "A"
    assert InteractionClass("A", "Root").parent_key == "Root"
