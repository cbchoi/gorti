"""Real-server MOM values and callbacks for the Table 6 save-label spelling."""

import asyncio
import struct
from contextlib import suppress
from pathlib import Path

import pytest

from rti1516e import FederationSpec, MIMModule, RtiConnection
from rti1516e.errors import SaveInProgress
from rti1516e.events import (
    FederationRestored,
    FederationSaved,
    InitiateFederateRestore,
    InitiateFederateSave,
    ReflectAttributeValues,
)
from tests.test_ieee2010_independent_contract_live import independent_artifacts  # noqa: F401
from tests.test_ieee2010_independent_contract_live import (
    independent_server as _server,  # noqa: F401
)
from tests.test_live_service_e2e import LiveCallbacks

ROOT = Path(__file__).resolve().parents[2]
MIM = ROOT / "rti/pkg/fom/mim/standard-mim.xml"
FOM = ROOT / "rti/pkg/fom/exporter/testdata/retained.xml"
CLASS = "HLAobjectRoot.HLAmanager.HLAfederation"
NAME = "HLAAnnexSaveName"


def unicode_value(data):
    assert len(data) == 4 + 2 * struct.unpack(">I", data[:4])[0]
    return data[4:].decode("utf-16-be")


@pytest.mark.parametrize("spelling", [None, "HLAnextSaveName", NAME])
def test_live_mim_table_precedence_save_restore_and_callbacks(independent_server, spelling):
    url, _, _, _ = independent_server
    selected = None if spelling is None else MIMModule(
        "selected.xml", MIM.read_bytes().replace(
            b"<name>HLAnextSaveName</name>", f"<name>{spelling}</name>".encode(), 1))

    async def exercise():
        async with asyncio.timeout(50), RtiConnection.connect(url) as connection:
            spec = FederationSpec("table-precedence", [str(FOM)], mim_module=selected)
            async with connection.join_federation(spec, federate_name="observer") as fed:
                cls = await fed.support.get_object_class_handle(CLASS)
                attr = await fed.support.get_attribute_handle(cls, NAME)
                assert attr == 10
                assert await fed.support.get_attribute_handle(cls, "HLAnextSaveName") == attr
                assert await fed.support.get_attribute_name(cls, attr) == NAME

                async def check(next_name, last_name):
                    state = await fed.mom.query_federation_attributes()
                    assert unicode_value(state.attribute_values[NAME]) == next_name
                    assert unicode_value(state.attribute_values["HLAlastSaveName"]) == last_name
                    assert "HLAnextSaveName" not in state.attribute_values
                    fdd = unicode_value(state.attribute_values["HLAcurrentFDD"])
                    assert f"<name>{NAME}</name>" in fdd
                    assert "<name>HLAnextSaveName</name>" not in fdd

                await check("", "")
                log = LiveCallbacks(fed)
                try:
                    await fed.subscribe_object_class(CLASS, attributes=[NAME])
                    reflected = await log.wait(
                        ReflectAttributeValues,
                        lambda event: attr in event.attribute_values
                        and unicode_value(event.attribute_values[attr]) == "",
                    )
                    assert reflected.timestamp is None
                    mark = len(log.events)
                    await fed.savepoint.request_federation_save("table-save")
                    await log.wait(InitiateFederateSave, after=mark)
                    # Ordinary services remain unavailable during active saving.
                    with pytest.raises(SaveInProgress):
                        await fed.mom.query_federation_attributes()
                    await fed.savepoint.federate_save_begun()
                    await fed.savepoint.federate_save_complete()
                    await log.wait(FederationSaved, after=mark)
                    await check("", "table-save")
                    await log.wait(
                        ReflectAttributeValues,
                        lambda event: attr in event.attribute_values
                        and unicode_value(event.attribute_values[attr]) == "",
                        after=mark,
                    )
                    mark = len(log.events)
                    await fed.savepoint.request_federation_restore("table-save")
                    await log.wait(InitiateFederateRestore, after=mark)
                    await fed.savepoint.federate_restore_complete()
                    await log.wait(FederationRestored, after=mark)
                    assert await fed.support.get_attribute_handle(cls, NAME) == attr
                    assert await fed.support.get_attribute_name(cls, attr) == NAME
                    await check("", "table-save")
                    if selected is not None:
                        assert connection.transport._mim_module == selected
                except BaseException:
                    # Let an assertion remain visible instead of a blocked resign.
                    with suppress(Exception):
                        await fed.savepoint.abort_federation_save()
                    with suppress(Exception):
                        await fed.savepoint.abort_federation_restore()
                    raise
                finally:
                    await log.close()

    asyncio.run(exercise())
