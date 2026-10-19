package object_test

import (
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
)

func TestIndependentNameReviewReleasedLiveNameCanBeReserved(t *testing.T) {
	for _, multiple := range []bool{false, true} {
		mode := "single"
		if multiple {
			mode = "multiple"
		}
		t.Run(mode, func(t *testing.T) {
			f := newKnownFixture(t)
			const name = "releasedWhileObjectLives"
			knownOK(t, f.decl.PublishObjectClassAttributes(f.ctx, knownFed, 2, f.child, []core.AttributeHandle{f.inherited}))
			knownOK(t, f.reg.ReserveObjectInstanceName(f.ctx, knownFed, 1, name))
			f.drain(1)
			original, _, err := f.reg.Register(f.ctx, knownFed, 1, f.child, name)
			knownOK(t, err)
			if multiple {
				knownOK(t, f.reg.ReleaseMultipleObjectInstanceNames(f.ctx, knownFed, 1, []string{name}))
				knownOK(t, f.reg.ReserveMultipleObjectInstanceNames(f.ctx, knownFed, 2, []string{name}))
			} else {
				knownOK(t, f.reg.ReleaseObjectInstanceName(f.ctx, knownFed, 1, name))
				knownOK(t, f.reg.ReserveObjectInstanceName(f.ctx, knownFed, 2, name))
			}
			want := object.CallbackReservationSucceeded
			if multiple {
				want = object.CallbackMultipleReservationSucceeded
			}
			events := f.drain(2)
			if len(events) != 1 || events[0].Kind != want {
				t.Fatalf("6.4/6.7 release must make name reservable even while its object remains: got %+v, want %v", events, want)
			}
			if _, _, err := f.reg.Register(f.ctx, knownFed, 2, f.child, name); !errors.Is(err, core.ErrObjectInstanceNameInUse) {
				t.Fatalf("reservation must not allow a second live object of the same name: %v", err)
			}
			state := f.reg.ExportState(knownFed)
			if len(state.Reservations) != 1 || state.Reservations[0].Holder != 2 || state.Objects[0].Registrant != 1 {
				t.Fatal("reservation and existing registration are distinct identities")
			}
			commit, err := f.reg.PrepareImportState(knownFed, state)
			knownOK(t, err)
			commit()
			knownOK(t, f.reg.Delete(f.ctx, knownFed, 1, original, nil, nil))
			_, _, err = f.reg.Register(f.ctx, knownFed, 2, f.child, name)
			knownOK(t, err)
		})
	}
}
