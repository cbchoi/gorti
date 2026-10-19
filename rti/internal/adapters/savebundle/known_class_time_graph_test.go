package savebundle

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestKnownClassQueuedReflectionUsesRecipientCoordinates(t *testing.T) {
	for _, tombstone := range []bool{false, true} {
		for _, receiveOrder := range []bool{false, true} {
			for _, kind := range []string{"projected", "registered-class", "other-ancestor", "registered-attribute", "registered-attribute-list", "another-recipient", "no-discovery", "legacy-actual", "legacy-omitted-class"} {
				t.Run(fmt.Sprintf("tombstone=%t/ro=%t/%s", tombstone, receiveOrder, kind), func(t *testing.T) {
					manifest, model, state := knownClassGraphFixture(t, tombstone)
					actual, _ := model.LookupObjectClass("HLAobjectRoot.Base.Child")
					known := state.Discoveries[0].KnownClass
					knownAttr, _ := model.LookupAttribute(known, "Common")
					actualAttr, _ := model.LookupAttribute(actual, "Common")
					if knownAttr == actualAttr {
						t.Fatal("fixture must distinguish registered and known attribute coordinates")
					}
					event := timeEventSnapshot{Kind: "reflect", Sequence: 1, Object: 7, ObjectClass: known,
						Attributes: map[uint64][]byte{uint64(knownAttr): {1, 2}}, AttributeHandles: []core.AttributeHandle{knownAttr},
						Metadata: &core.ObjectCallbackMetadata{Producer: 1, Transportation: core.TransportTypeReliable, SentOrder: core.OrderTimestamp, ReceivedOrder: core.OrderTimestamp},
					}
					valid := false
					switch kind {
					case "projected":
						valid = true
					case "registered-class":
						event.ObjectClass = actual
						event.Attributes = map[uint64][]byte{uint64(actualAttr): {1, 2}}
						event.AttributeHandles = []core.AttributeHandle{actualAttr}
					case "other-ancestor":
						event.ObjectClass, _ = model.LookupObjectClass("HLAobjectRoot")
						event.Attributes = nil
						event.AttributeHandles = nil
					case "registered-attribute":
						event.Attributes = map[uint64][]byte{uint64(actualAttr): {1, 2}}
					case "registered-attribute-list":
						event.AttributeHandles = []core.AttributeHandle{actualAttr}
					case "another-recipient":
						state.Discoveries[0].Federate = 1
						if tombstone {
							state.Deletions[0].Recipients = nil
						}
					case "no-discovery":
						state.Discoveries = nil
						valid = tombstone
					case "legacy-actual", "legacy-omitted-class":
						valid = true
						state.Discoveries[0].KnownClass = 0
						if tombstone {
							state.Deletions[0].Recipients[0].KnownClass = 0
						}
						event.ObjectClass = actual
						if kind == "legacy-omitted-class" {
							event.ObjectClass = 0
						}
						event.Attributes, event.AttributeHandles = map[uint64][]byte{uint64(actualAttr): {1, 2}}, []core.AttributeHandle{actualAttr}
					}
					queued := timeQueuedSnapshot{Destination: 2, Sender: 1, ReceiveOrder: receiveOrder, Event: event}
					federate := timeFederateSnapshot{Handle: 2, HasRegulationState: true, Constrained: true, HasAdvanceState: true, CurrentTime: 4}
					wire := timeSnapshot{Version: 1}
					if receiveOrder {
						queued.Event.Metadata.SentOrder, queued.Event.Metadata.ReceivedOrder = core.OrderReceive, core.OrderReceive
						wire.Version = 4
						federate.ROQueue = []timeQueuedSnapshot{queued}
					} else {
						timestamp := timeSnapshotNumber(4.5)
						queued.Timestamp, queued.Event.LogicalTime = timestamp, &timestamp
						federate.Queue = []timeQueuedSnapshot{queued}
					}
					wire.Federates = []timeFederateSnapshot{federate}
					manifest.ManagerSnapshots["object"] = graphJSON(t, objectSnapshotV1{Version: objectSnapshotVersion(state), State: state})
					manifest.ManagerSnapshots["time"] = graphJSON(t, wire)
					err := (RestoreGraph{}).Validate("fed", manifest, model)
					if (err == nil) != valid {
						t.Fatalf("valid=%t err=%v", valid, err)
					}
					if !valid {
						return
					}
					rebound, err := (RestoreGraph{}).Rebind(manifest, 9, map[core.FederateHandle]core.FederateHandle{1: 9<<32 | 1, 2: 9<<32 | 2})
					if err != nil {
						t.Fatal(err)
					}
					if err := (RestoreGraph{}).Validate("fed", rebound, model); err != nil {
						t.Fatalf("rebound projection: %v", err)
					}
					var round timeSnapshot
					if err := json.Unmarshal(rebound.ManagerSnapshots["time"], &round); err != nil {
						t.Fatal(err)
					}
					queue := round.Federates[0].Queue
					if receiveOrder {
						queue = round.Federates[0].ROQueue
					}
					if len(queue) != 1 || queue[0].Event.ObjectClass != event.ObjectClass || len(queue[0].Event.Attributes) != 1 || len(queue[0].Event.Attributes[uint64(event.AttributeHandles[0])]) != 2 {
						t.Fatal("rebind changed callback class or attribute coordinates")
					}
				})
			}
		}
	}
}
