package savebundle

import (
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type delayedGraphModel struct {
	core.FOMHandle
	core.FOMHandleNameLookup
	core.FOMClassAncestry
	enabled bool
}

func (m delayedGraphModel) DelaySubscriptionEvaluation() (bool, bool) { return m.enabled, true }

func TestG13CanonicalRestoreGraph(t *testing.T) {
	for _, mode := range []string{"known-ancestor", "unknown-candidate", "disabled", "wrong-class", "wrong-attribute", "missing-class"} {
		t.Run(mode, func(t *testing.T) {
			manifest, model, state := knownClassGraphFixture(t, false)
			actual, _ := model.LookupObjectClass("HLAobjectRoot.Base.Child")
			attribute, _ := model.LookupAttribute(actual, "Common")
			known := state.Discoveries[0].KnownClass
			policy := delayedGraphModel{model, model.(core.FOMHandleNameLookup), model.(core.FOMClassAncestry), true}
			meta := &core.ObjectCallbackMetadata{DelayedSubscription: true, RegisteredClass: actual, Producer: 1, SentOrder: core.OrderTimestamp, ReceivedOrder: core.OrderTimestamp, Transportation: core.TransportTypeReliable}
			event := timeEventSnapshot{Kind: "reflect", Sequence: 1, Object: 7, ObjectClass: known, Attributes: map[uint64][]byte{uint64(attribute): {1, 2}}, Metadata: meta}
			valid := mode == "known-ancestor" || mode == "unknown-candidate"
			switch mode {
			case "unknown-candidate":
				state.Discoveries = nil
				event.ObjectClass = 0
			case "disabled":
				policy.enabled = false
			case "wrong-class":
				meta.RegisteredClass = known
			case "wrong-attribute":
				event.Attributes = map[uint64][]byte{99999: {1}}
			case "missing-class":
				meta.RegisteredClass = 0
			}
			at := timeSnapshotNumber(4.5)
			event.LogicalTime = &at
			wire := timeSnapshot{Version: 1, Federates: []timeFederateSnapshot{{Handle: 2, HasRegulationState: true, Constrained: true, HasAdvanceState: true, CurrentTime: 4, Queue: []timeQueuedSnapshot{{Destination: 2, Sender: 1, Timestamp: at, Event: event}}}}}
			manifest.ManagerSnapshots["object"] = graphJSON(t, objectSnapshotV1{Version: objectSnapshotVersion(state), State: state})
			manifest.ManagerSnapshots["time"] = graphJSON(t, wire)
			err := (RestoreGraph{}).Validate("fed", manifest, policy)
			if (err == nil) != valid {
				t.Fatalf("valid=%t error=%v", valid, err)
			}
			if !valid {
				return
			}
			rebound, err := (RestoreGraph{}).Rebind(manifest, 9, map[core.FederateHandle]core.FederateHandle{1: 9<<32 | 1, 2: 9<<32 | 2})
			if err != nil {
				t.Fatal(err)
			}
			if err := (RestoreGraph{}).Validate("fed", rebound, policy); err != nil {
				t.Fatal(err)
			}
		})
	}
}
