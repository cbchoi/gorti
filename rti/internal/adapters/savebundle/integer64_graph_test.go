package savebundle

import (
	"encoding/json"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
	timepkg "github.com/cbchoi/gorti/rti/internal/hla/time"
)

func integerGraphFixture(t *testing.T) (core.CheckpointManifestOf[int64], core.FOMHandle) {
	t.Helper()
	base, model, floatState := knownClassGraphFixture(t, true)
	timestamp := int64(1<<53 + 7)
	current := integerSnapshotNumber(timestamp - 1)
	state := &object.RegistryStateOf[int64]{LastObjectHandle: floatState.LastObjectHandle, Discoveries: floatState.Discoveries, Deletions: []object.DeletionStateOf[int64]{{Object: 7, Producer: 1, Timestamp: timestamp, Committed: true, ObjectName: "saved", ObjectClass: floatState.Deletions[0].ObjectClass, Recipients: floatState.Deletions[0].Recipients}}}
	snapshots := base.ManagerSnapshots
	snapshots["object"] = graphJSON(t, objectSnapshotV1Of[int64]{Version: 5, LogicalTimeImplementationName: "HLAinteger64Time", State: state})
	snapshots["time"] = graphJSON(t, timeSnapshotOf[integerSnapshotNumber]{Version: 6, LogicalTimeImplementationName: "HLAinteger64Time", Federates: []timeFederateSnapshotOf[integerSnapshotNumber]{{Handle: 2, HasRegulationState: true, HasAdvanceState: true, Constrained: true, CurrentTime: current}}})
	var err error
	snapshots["retractions"], err = writeRetractionsOf(&timepkg.RetractionHistoryOf[int64]{Complete: true})
	if err != nil {
		t.Fatal(err)
	}
	return core.CheckpointManifestOf[int64]{Federation: base.Federation, FederationGeneration: base.FederationGeneration, Federates: base.Federates, FederateNames: base.FederateNames, LogicalTimeImplementationName: "HLAinteger64Time", SaveTime: &timestamp, ManagerSnapshots: snapshots}, model
}

func TestInteger64RestoreGraphExactAdjacentTimeAndGeneration(t *testing.T) {
	manifest, model := integerGraphFixture(t)
	graph := IntegerRestoreGraph{}
	if err := graph.Validate("fed", manifest, model); err != nil {
		t.Fatal(err)
	}
	rebound, err := graph.Rebind(manifest, 9, map[core.FederateHandle]core.FederateHandle{1: 9<<32 | 1, 2: 9<<32 | 2})
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.Validate("fed", rebound, model); err != nil {
		t.Fatal(err)
	}
	if *rebound.SaveTime != 1<<53+7 {
		t.Fatal("manifest save time rounded")
	}
	var timeWire timeSnapshotOf[integerSnapshotNumber]
	if err := json.Unmarshal(rebound.ManagerSnapshots["time"], &timeWire); err != nil {
		t.Fatal(err)
	}
	if timeWire.Federates[0].CurrentTime != 1<<53+6 {
		t.Fatal("rebind rounded receiver time")
	}
	timeWire.Federates[0].CurrentTime++
	rebound.ManagerSnapshots["time"] = graphJSON(t, timeWire)
	if err := graph.Validate("fed", rebound, model); err == nil {
		t.Fatal("adjacent integer deletion boundary accepted")
	}
}

func TestInteger64RestoreGraphRejectsManifestFactoryMismatch(t *testing.T) {
	for _, name := range []string{"", "HLAfloat64Time", "unknown"} {
		manifest, model := integerGraphFixture(t)
		manifest.LogicalTimeImplementationName = name
		if err := (IntegerRestoreGraph{}).Validate("fed", manifest, model); err == nil {
			t.Fatalf("invalid factory accepted: %q", name)
		}
		if _, err := (IntegerRestoreGraph{}).Rebind(manifest, 9, map[core.FederateHandle]core.FederateHandle{1: 9<<32 | 1, 2: 9<<32 | 2}); err == nil {
			t.Fatalf("invalid factory rebound: %q", name)
		}
	}
}
