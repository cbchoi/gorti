package savebundle

import (
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/object"
)

func TestApplicableFMRegisteredNameReservationSurvivesSaveGraph(t *testing.T) {
	manifest, model, state := knownClassGraphFixture(t, false)
	state.Reservations = []object.NameReservationState{{Name: "saved", Holder: 1}}
	manifest.ManagerSnapshots["object"] = graphJSON(t, objectSnapshotV1{Version: 4, State: state})
	if err := (RestoreGraph{}).Validate("fed", manifest, model); err != nil {
		t.Fatalf("registration must retain its holder's reservation in a valid checkpoint: %v", err)
	}
}
