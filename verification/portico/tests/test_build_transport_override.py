from __future__ import annotations

import importlib.util
import zipfile
from pathlib import Path
from xml.etree import ElementTree

MODULE_PATH = Path(__file__).resolve().parents[1] / "build_transport_override.py"
SPEC = importlib.util.spec_from_file_location("portico_transport", MODULE_PATH)
assert SPEC and SPEC.loader
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


def test_build_override_changes_only_transport_and_discovery(tmp_path: Path) -> None:
    source = tmp_path / "portico.jar"
    output = tmp_path / "override.jar"
    xml = b"""<config xmlns="urn:org:jgroups">
    <UDP enable_bundling="true" thread_pool.enabled="true"/>
    <PING timeout="2000" num_initial_members="1" break_on_coord_rsp="true"/>
    <RSVP ack_on_delivery="true"/>
    <pbcast.GMS/>
    </config>"""
    with zipfile.ZipFile(source, "w") as archive:
        archive.writestr(MODULE.RESOURCE, xml)

    MODULE.build_override(source, output)

    with zipfile.ZipFile(output) as archive:
        root = ElementTree.fromstring(archive.read(MODULE.RESOURCE))  # noqa: S314
    names = [child.tag.rsplit("}", 1)[-1] for child in root]
    assert names == ["TCP", "TCPPING", "RSVP", "pbcast.GMS"]
    assert root[0].attrib["enable_bundling"] == "true"
    assert root[0].attrib["thread_pool.enabled"] == "true"
    assert root[1].attrib["port_range"] == "0"
