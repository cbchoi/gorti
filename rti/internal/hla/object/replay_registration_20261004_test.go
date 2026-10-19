package object_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/adapters/delivery"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
)

func TestIndependentReplayRegistrationPreservesRecordedIdentity(t *testing.T) {
	for _, priorReservation := range []bool{false, true} {
		mode := "no-reservation"
		if priorReservation {
			mode = "existing-reservation"
		}
		t.Run(mode, func(t *testing.T) {
			f := newKnownFixture(t)
			f.subscribe(t, 2, f.child, f.inherited)
			f.drain(1)
			f.drain(2)
			record := core.ObjectRegistrationRecord{Object: 19, Class: f.child, Registrant: 1, Name: "recorded-object", WallNS: 5678}
			if priorReservation {
				knownOK(t, f.reg.ReserveObjectInstanceName(f.ctx, knownFed, 1, record.Name))
				f.drain(1)
			}
			before := f.reg.ExportState(knownFed)
			knownOK(t, f.reg.ReplayObjectRegistration(f.ctx, knownFed, record))
			after := f.reg.ExportState(knownFed)
			if len(after.Objects) != 1 || after.Objects[0].Handle != 19 || after.Objects[0].Name != record.Name || after.LastObjectHandle != 19 {
				t.Fatalf("recorded identity changed: %+v", after)
			}
			if !reflect.DeepEqual(before.Reservations, after.Reservations) {
				t.Fatalf("recovery changed reservation rights: %v -> %v", before.Reservations, after.Reservations)
			}
			f.assertClass(t, 1, record.Object, f.child)
			f.assertClass(t, 2, record.Object, f.child)
			events := f.drain(2)
			if len(events) != 1 || events[0].Kind != object.CallbackDiscover {
				t.Fatalf("recovery discovery callbacks = %+v", events)
			}
			// An actual owned update proves the replay path installed ownership.
			knownOK(t, f.reg.UpdateAttributes(f.ctx, knownFed, 1, record.Object, map[core.AttributeHandle][]byte{f.inherited: {0, 0, 0, 7}}, nil))
			next, _, err := f.reg.Register(f.ctx, knownFed, 1, f.child, "")
			knownOK(t, err)
			if next != 20 {
				t.Fatalf("next live handle = %d, want 20", next)
			}
		})
	}
}

func TestIndependentReplayRegistrationRejectsInvalidOrConflictingState(t *testing.T) {
	cases := []struct {
		name    string
		prepare func(*testing.T, *knownFixture, *core.ObjectRegistrationRecord)
	}{
		{"zero-handle", func(_ *testing.T, _ *knownFixture, r *core.ObjectRegistrationRecord) { r.Object = 0 }},
		{"zero-class", func(_ *testing.T, _ *knownFixture, r *core.ObjectRegistrationRecord) { r.Class = 0 }},
		{"unknown-class", func(_ *testing.T, _ *knownFixture, r *core.ObjectRegistrationRecord) { r.Class = 99999 }},
		{"zero-registrant", func(_ *testing.T, _ *knownFixture, r *core.ObjectRegistrationRecord) { r.Registrant = 0 }},
		{"empty-name", func(_ *testing.T, _ *knownFixture, r *core.ObjectRegistrationRecord) { r.Name = "" }},
		{"invalid-utf8", func(_ *testing.T, _ *knownFixture, r *core.ObjectRegistrationRecord) { r.Name = string([]byte{0xff}) }},
		{"unpublished", func(_ *testing.T, _ *knownFixture, r *core.ObjectRegistrationRecord) { r.Registrant = 2 }},
		{"existing-handle", func(t *testing.T, f *knownFixture, r *core.ObjectRegistrationRecord) { r.Object = f.register(t) }},
		{"existing-name", func(t *testing.T, f *knownFixture, r *core.ObjectRegistrationRecord) {
			handle := f.register(t)
			r.Name = f.reg.ExportState(knownFed).Objects[0].Name
			r.Object = handle + 1
		}},
		{"consumed-handle", func(t *testing.T, f *knownFixture, r *core.ObjectRegistrationRecord) {
			r.Object = f.register(t)
			knownOK(t, f.reg.Delete(f.ctx, knownFed, 1, r.Object, nil, nil))
		}},
		{"foreign-reservation", func(t *testing.T, f *knownFixture, r *core.ObjectRegistrationRecord) {
			knownOK(t, f.reg.ReserveObjectInstanceName(f.ctx, knownFed, 2, r.Name))
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newKnownFixture(t)
			r := core.ObjectRegistrationRecord{Object: 19, Class: f.child, Registrant: 1, Name: "recorded-object"}
			tc.prepare(t, f, &r)
			f.drain(1)
			f.drain(2)
			before := f.reg.ExportState(knownFed)
			if err := f.reg.ReplayObjectRegistration(f.ctx, knownFed, r); err == nil {
				t.Fatal("incompatible replay record was admitted")
			}
			if after := f.reg.ExportState(knownFed); !reflect.DeepEqual(before, after) {
				t.Fatalf("rejected replay changed state: before=%+v after=%+v", before, after)
			}
			if len(f.drain(1)) != 0 || len(f.drain(2)) != 0 {
				t.Fatal("rejected replay emitted a callback")
			}
		})
	}
}

type independentReplayWriteFailure struct {
	core.EventLog
	err error
}

func (f *independentReplayWriteFailure) Append(context.Context, core.FederationName, core.EventRecord) error {
	return f.err
}

func TestIndependentReplayRegistrationPersistenceFailureIsAtomic(t *testing.T) {
	f := newKnownFixture(t)
	writeErr := errors.New("replay persistence rejected")
	log := &independentReplayWriteFailure{err: writeErr}
	outbox := delivery.NewWithBatch(8, 1, 0)
	queue, closeQueue, err := outbox.Subscribe(f.ctx, knownFed, 1)
	knownOK(t, err)
	t.Cleanup(func() { _ = closeQueue() })
	reg, err := object.New(object.Options{FOMs: f.repo, Declarations: f.decl, Outbox: outbox, Clock: core.NewManualClock(time.Unix(0, 0)), EventLog: log})
	knownOK(t, err)
	r := core.ObjectRegistrationRecord{Object: 19, Class: f.child, Registrant: 1, Name: "replay-persistence"}
	knownOK(t, reg.ReserveObjectInstanceName(f.ctx, knownFed, 1, r.Name))
	<-queue
	before := reg.ExportState(knownFed)
	if err := reg.ReplayObjectRegistration(f.ctx, knownFed, r); !errors.Is(err, writeErr) {
		t.Fatalf("persistence error = %v, want %v", err, writeErr)
	}
	if after := reg.ExportState(knownFed); !reflect.DeepEqual(before, after) {
		t.Fatalf("failed replay changed prior state: %+v -> %+v", before, after)
	}
	log.err = nil
	knownOK(t, reg.ReplayObjectRegistration(f.ctx, knownFed, r))
	if got, ok := reg.LookupObjectInstanceByName(knownFed, r.Name); !ok || got != r.Object {
		t.Fatalf("retry did not preserve recorded identity: %d, %t", got, ok)
	}
}
