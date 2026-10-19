package savebundle

import (
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ddm"
)

func copiedStateFixture(t *testing.T) *ddm.ManagerState {
	t.Helper()
	realization, err := core.NewRegionRealizationSnapshot(90, []core.RegionDimensionRange{{Dimension: 1, Bounds: core.DDMRange{Lower: 1<<53 + 1, Upper: ^uint64(0)}}})
	if err != nil {
		t.Fatal(err)
	}
	return &ddm.ManagerState{NextRegionHandle: 92, ConveyedRegions: []ddm.ConveyedRegionState{
		{Designator: 91, Realization: realization, Recipients: []ddm.ConveyedRegionRecipientState{{Federate: 2, Active: true}, {Federate: 3}}},
		{Designator: 92, Realization: realization, Recipients: []ddm.ConveyedRegionRecipientState{{Federate: 3}}},
	}}
}

func TestDDMCopiedStateValidationRejectsWholeBatch(t *testing.T) {
	for name, change := range map[string]func(*ddm.ManagerState){
		"zero designator":      func(s *ddm.ManagerState) { s.ConveyedRegions[1].Designator = 0 },
		"source designator":    func(s *ddm.ManagerState) { s.ConveyedRegions[1].Designator = 90 },
		"duplicate designator": func(s *ddm.ManagerState) { s.ConveyedRegions[1].Designator = 91 },
		"namespace overflow":   func(s *ddm.ManagerState) { s.NextRegionHandle = 91 },
		"regular collision":    func(s *ddm.ManagerState) { s.Regions = []ddm.RegionState{{Handle: 92}} },
		"zero realization":     func(s *ddm.ManagerState) { s.ConveyedRegions[1].Realization = core.RegionRealizationSnapshot{} },
		"zero receiver":        func(s *ddm.ManagerState) { s.ConveyedRegions[1].Recipients[0].Federate = 0 },
		"internal receiver":    func(s *ddm.ManagerState) { s.ConveyedRegions[1].Recipients[0].Federate = ^core.FederateHandle(0) },
		"duplicate receiver": func(s *ddm.ManagerState) {
			s.ConveyedRegions[1].Recipients = []ddm.ConveyedRegionRecipientState{{Federate: 3}, {Federate: 3, Active: true}}
		},
		"unleased cache": func(s *ddm.ManagerState) { s.ConveyedRegions[1].Recipients = nil },
	} {
		t.Run(name, func(t *testing.T) {
			state := copiedStateFixture(t)
			change(state)
			before := append([]ddm.ConveyedRegionState(nil), state.ConveyedRegions...)
			if err := validateDDMCopiedState(state); err == nil {
				t.Fatal("malformed copied state accepted")
			}
			if !reflect.DeepEqual(before, state.ConveyedRegions) {
				t.Fatal("validation mutated candidate state")
			}
		})
	}
}

func TestDDMCopiedStateHistoricalSourceAndZeroWidth(t *testing.T) {
	state := copiedStateFixture(t)
	realization, err := core.NewRegionRealizationSnapshot(9000, []core.RegionDimensionRange{{Dimension: 1, Bounds: core.DDMRange{Lower: 4, Upper: 4}}})
	if err != nil {
		t.Fatal(err)
	}
	state.ConveyedRegions[1].Realization = realization
	if err := validateDDMCopiedState(state); err != nil {
		t.Fatalf("historical source or realized zero-width rejected: %v", err)
	}
}
