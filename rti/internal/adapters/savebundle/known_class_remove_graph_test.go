package savebundle

import (
	"fmt"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestKnownClassQueuedRemovalUsesRecipientClass(t *testing.T) {
	for _, tombstone := range []bool{false, true} {
		for _, ro := range []bool{false, true} {
			for _, projected := range []bool{false, true} {
				t.Run(fmt.Sprintf("tombstone=%t/ro=%t/projected=%t", tombstone, ro, projected), func(t *testing.T) {
					manifest, model, state := knownClassGraphFixture(t, tombstone)
					class := state.Discoveries[0].KnownClass
					if !projected {
						class, _ = model.LookupObjectClass("HLAobjectRoot.Base.Child")
					}
					stamp := timeSnapshotNumber(5)
					event := timeEventSnapshot{Kind: "remove", Object: 7, ObjectClass: class, Sequence: 1, LogicalTime: &stamp,
						Metadata: &core.ObjectCallbackMetadata{Producer: 1, Transportation: core.TransportTypeReliable, SentOrder: core.OrderTimestamp, ReceivedOrder: core.OrderTimestamp}}
					queued := timeQueuedSnapshot{Destination: 2, Sender: 1, Timestamp: stamp, Event: event, ReceiveOrder: ro}
					f := timeFederateSnapshot{Handle: 2, HasRegulationState: true, Constrained: true, HasAdvanceState: true, CurrentTime: 4}
					wire := timeSnapshot{Version: 1}
					if ro {
						queued.Timestamp, queued.Event.LogicalTime = 0, nil
						queued.Event.Metadata.SentOrder, queued.Event.Metadata.ReceivedOrder = core.OrderReceive, core.OrderReceive
						wire.Version, f.ROQueue = 4, []timeQueuedSnapshot{queued}
					} else {
						f.Queue = []timeQueuedSnapshot{queued}
					}
					wire.Federates = []timeFederateSnapshot{f}
					manifest.ManagerSnapshots["time"] = graphJSON(t, wire)
					err := (RestoreGraph{}).Validate("fed", manifest, model)
					if (err == nil) != projected {
						t.Fatalf("projected=%t error=%v", projected, err)
					}
				})
			}
		}
	}
}
