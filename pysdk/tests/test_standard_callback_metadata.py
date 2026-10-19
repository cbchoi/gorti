from rti1516e import Rti1516eAmbassador
from rti1516e.events import (
    ObjectCallbackMetadata,
    ReceiveInteraction,
    TurnUpdatesOffForObjectInstance,
    TurnUpdatesOnForObjectInstance,
)


def test_metadata_and_optional_update_rate_reach_standard_callbacks():
    class Recorder(Rti1516eAmbassador):
        def receiveInteraction(self, name, parameters, timestamp, *, tag, metadata):  # noqa: N802
            self.interaction = (name, parameters, timestamp, tag, metadata)

        def turnUpdatesOnForObjectInstance(self, obj, attrs, update_rate_designator=None):  # noqa: N802
            self.on = (obj, attrs, update_rate_designator)

        def turnUpdatesOffForObjectInstance(self, obj, attrs):  # noqa: N802
            self.off = (obj, attrs)

    amb = Recorder()
    metadata = ObjectCallbackMetadata(sent_order=1, received_order=1, producing_federate=7)
    amb._dispatch_event(
        ReceiveInteraction("Tick", {"step": b"1"}, None, tag=b"tag", metadata=metadata)
    )
    assert amb.interaction[-2:] == (b"tag", metadata)
    amb._dispatch_event(TurnUpdatesOnForObjectInstance(11, [2], "fast"))
    assert amb.on == (11, [2], "fast")
    amb._dispatch_event(TurnUpdatesOnForObjectInstance(11, [2], None))
    assert amb.on == (11, [2], None)
    amb._dispatch_event(TurnUpdatesOffForObjectInstance(11, [2]))
    assert amb.off == (11, [2])
