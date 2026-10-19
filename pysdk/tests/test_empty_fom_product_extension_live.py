"""Product extension regression, not the standard Java FOM creation contract."""

from contextlib import ExitStack

import pytest

from rti1516e import Rti1516eAmbassador
from tests.test_ieee2010_independent_contract_live import (
    independent_artifacts,  # noqa: F401
)
from tests.test_ieee2010_independent_contract_live import (
    independent_server as _independent_server,  # noqa: F401 - named pytest fixture
)


@pytest.mark.parametrize("time_name", ["HLAfloat64Time", "HLAinteger64Time"])
def test_empty_fom_product_creation_exposes_default_mim(independent_server, time_name):
    endpoint, _, _, _ = independent_server
    with ExitStack() as cleanup:
        rti = Rti1516eAmbassador()
        rti.connect(rti, endpoint)
        cleanup.callback(rti.disconnect)
        name = "empty-fom-product-extension"
        rti.createFederationExecution(name, [], logical_time_implementation_name=time_name)
        cleanup.callback(rti.destroyFederationExecution, name)
        rti.joinFederationExecution("mim-reader", name)
        cleanup.callback(rti.resignFederationExecution, "NO_ACTION")
        object_name = "HLAobjectRoot.HLAmanager.HLAfederate"
        object_class = rti.getObjectClassHandle(object_name)
        assert rti.getObjectClassName(object_class) == object_name
        assert int(rti.getAttributeHandle(object_class, "HLAfederateName")) > 0
        interaction_name = (
            "HLAinteractionRoot.HLAmanager.HLAfederate.HLArequest.HLArequestPublications"
        )
        interaction_class = rti.getInteractionClassHandle(interaction_name)
        assert rti.getInteractionClassName(interaction_class) == interaction_name
