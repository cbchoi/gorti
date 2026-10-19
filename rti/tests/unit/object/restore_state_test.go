package object_test

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
)

type restoreOutbox struct{ events []core.OutboundEvent }

func (o *restoreOutbox) Send(_ context.Context, _ core.FederationName, _ core.FederateHandle, event core.OutboundEvent) error {
	o.events = append(o.events, event)
	return nil
}

func (o *restoreOutbox) Reserve(_ context.Context, _ core.FederationName, deliveries []core.OutboxDelivery) (core.OutboxReservation, error) {
	return &restoreReservation{outbox: o, deliveries: deliveries}, nil
}

type restoreReservation struct {
	outbox     *restoreOutbox
	deliveries []core.OutboxDelivery
}

func (p *restoreReservation) Commit() error {
	for _, d := range p.deliveries {
		p.outbox.events = append(p.outbox.events, d.Event)
	}
	p.deliveries = nil
	return nil
}

func (p *restoreReservation) Release() { p.deliveries = nil }

type restoreFOMs struct{ core.FOMRepository }

func (restoreFOMs) Get(context.Context, core.FederationName) (core.FOMHandle, error) {
	return nil, nil
}

type restoreDDM struct{ subscribed bool }

func (*restoreDDM) HasObjectAssociations(core.FederationName, core.ObjectHandle) bool { return true }
func (*restoreDDM) PublisherRegionsFor(core.FederationName, core.ObjectHandle, core.AttributeHandle) []object.DDMRegionHandle {
	return []object.DDMRegionHandle{1}
}
func (d *restoreDDM) SubscribersForUpdate(core.FederationName, core.ObjectClassHandle, core.AttributeHandle, []object.DDMRegionHandle) []core.FederateHandle {
	if d.subscribed {
		return []core.FederateHandle{2}
	}
	return nil
}
func (*restoreDDM) RegionSubscribersFor(core.FederationName, core.ObjectClassHandle, core.AttributeHandle) []core.FederateHandle {
	return nil
}

func newRestoreRegistry(t *testing.T) (*object.Registry, *declaration.Manager, *restoreOutbox, *restoreDDM) {
	t.Helper()
	d := declaration.New()
	o := &restoreOutbox{}
	ddm := &restoreDDM{subscribed: true}
	r, err := object.New(object.Options{Declarations: d, Outbox: o, FOMs: restoreFOMs{}, Clock: core.NewRealClock(), DDM: ddm})
	if err != nil {
		t.Fatal(err)
	}
	for _, fed := range []core.FederationName{"fed", "other"} {
		for _, h := range []core.FederateHandle{1, 2} {
			if err := d.PublishObjectClassAttributes(context.Background(), fed, h, 7, []core.AttributeHandle{1, 2}); err != nil {
				t.Fatal(err)
			}
		}
	}
	return r, d, o, ddm
}

func TestObjectStateRestoresRuntimeBehavior(t *testing.T) {
	r, _, out, ddm := newRestoreRegistry(t)
	ctx := context.Background()
	if err := r.ReserveObjectInstanceName(ctx, "fed", 1, "saved"); err != nil {
		t.Fatal(err)
	}
	obj, name, err := r.Register(ctx, "fed", 1, 7, "saved")
	if err != nil {
		t.Fatal(err)
	}
	if err := r.UpdateAttributes(ctx, "fed", 1, obj, map[core.AttributeHandle][]byte{1: {3}}, nil); err != nil {
		t.Fatal(err)
	}
	if err := r.ReserveObjectInstanceName(ctx, "fed", 1, "reserved"); err != nil {
		t.Fatal(err)
	}
	if err := r.ChangeAttributeTransportType(ctx, "fed", 1, obj, []core.AttributeHandle{1}, core.TransportTypeBestEffort); err != nil {
		t.Fatal(err)
	}
	if err := r.ChangeInteractionTransportType(ctx, "fed", 1, 9, core.TransportTypeReliable); err != nil {
		t.Fatal(err)
	}
	saved := r.ExportState("fed")
	if saved.LastObjectHandle != uint64(obj) || len(saved.Objects) != 1 || saved.Objects[0].Registrant != 1 || len(saved.Discoveries) != 2 || len(saved.Scopes) != 1 || len(saved.Reservations) != 2 {
		t.Fatalf("incomplete state: %+v", saved)
	}
	if saved.Scopes[0].Attributes[0] != 1 {
		t.Fatalf("scope not captured: %+v", saved.Scopes)
	}
	if err := r.ReserveObjectInstanceName(ctx, "other", 1, "other"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.Register(ctx, "other", 1, 7, "other"); err != nil {
		t.Fatal(err)
	}
	other := r.ExportState("other")
	if err := r.Delete(ctx, "fed", 1, obj, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := r.ReserveObjectInstanceName(ctx, "fed", 2, "later"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.Register(ctx, "fed", 2, 7, "later"); err != nil {
		t.Fatal(err)
	}
	if err := r.ReleaseObjectInstanceName(ctx, "fed", 1, "reserved"); err != nil {
		t.Fatal(err)
	}
	if err := r.ChangeInteractionTransportType(ctx, "fed", 1, 9, core.TransportTypeBestEffort); err != nil {
		t.Fatal(err)
	}
	before := r.ExportState("fed")
	commit, err := r.PrepareImportState("fed", saved)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, r.ExportState("fed")) {
		t.Fatal("prepare changed live state")
	}
	n := len(out.events)
	commit()
	if len(out.events) != n {
		t.Fatal("restore emitted callbacks")
	}
	if !reflect.DeepEqual(saved, r.ExportState("fed")) {
		t.Fatalf("state did not round trip: got %+v want %+v", r.ExportState("fed"), saved)
	}
	if !reflect.DeepEqual(other, r.ExportState("other")) {
		t.Fatal("restore changed other federation")
	}
	if got, ok := r.LookupObjectInstanceByName("fed", name); !ok || got != obj {
		t.Fatal("name index not restored")
	}
	if got, ok := r.LookupObjectInstanceName("fed", obj); !ok || got != name {
		t.Fatal("reverse name index not restored")
	}
	if r.ClassOf("fed", obj) != 7 {
		t.Fatal("class not restored")
	}
	if _, ok := r.LookupObjectInstanceByName("fed", "later"); ok {
		t.Fatal("post-save object survived")
	}
	if r.AttributeTransportType("fed", obj, 1) != core.TransportTypeBestEffort || r.InteractionTransportType("fed", 1, 9) != core.TransportTypeReliable {
		t.Fatal("transport overrides not restored")
	}
	r.ObjectClassSubscribed(ctx, "fed", 2, 7, []core.AttributeHandle{1})
	if len(out.events) != n {
		t.Fatal("restored discovery was repeated")
	}
	if err := r.UpdateAttributes(ctx, "fed", 1, obj, map[core.AttributeHandle][]byte{1: {4}}, nil); err != nil {
		t.Fatal(err)
	}
	if len(out.events) != n+1 || out.events[n].(*object.CallbackEvent).Kind != object.CallbackReflect {
		t.Fatal("restored scope produced a duplicate in-scope advisory")
	}
	if out.events[n].Seq() != saved.LastOutboundSequence+1 {
		t.Fatal("outbound sequence not restored")
	}
	ddm.subscribed = false
	n = len(out.events)
	if err := r.UpdateAttributes(ctx, "fed", 1, obj, map[core.AttributeHandle][]byte{1: {5}}, nil); err != nil {
		t.Fatal(err)
	}
	if len(out.events) != n+1 || out.events[n].(*object.CallbackEvent).Kind != object.CallbackAttributesOutOfScope {
		t.Fatal("restored scope did not produce out-of-scope advisory")
	}
	if _, _, err := r.Register(ctx, "fed", 2, 7, "reserved"); !errors.Is(err, core.ErrObjectInstanceNameNotReserved) {
		t.Fatalf("reservation holder lost: %v", err)
	}
	n = len(out.events)
	if err := r.ReserveObjectInstanceName(ctx, "fed", 2, "saved"); err != nil {
		t.Fatal(err)
	}
	if out.events[n].(*object.CallbackEvent).Kind != object.CallbackReservationFailed {
		t.Fatal("reservation for registered name not restored")
	}
	newObject, _, err := r.Register(ctx, "fed", 1, 7, "reserved")
	if err != nil || uint64(newObject) != saved.LastObjectHandle+1 {
		t.Fatalf("next object ID: %d, %v", newObject, err)
	}
	commit()
	if r.ClassOf("fed", newObject) == 0 {
		t.Fatal("repeated commit rewound live state")
	}
}

func fullObjectState() *object.RegistryState {
	return &object.RegistryState{
		LastObjectHandle: 40, LastOutboundSequence: 90,
		Objects:               []object.InstanceState{{Handle: 10, Name: "one", Class: 7, Registrant: 1}, {Handle: 30, Name: "three", Class: 7, Registrant: 2}},
		Discoveries:           []object.DiscoveryState{{Object: 10, Federate: 1}, {Object: 10, Federate: 2}},
		Scopes:                []object.ScopeState{{Object: 10, Federate: 2, Attributes: []core.AttributeHandle{1, 2}}},
		Reservations:          []object.NameReservationState{{Name: "reserved", Holder: 1}},
		AttributeTransports:   []object.AttributeTransportState{{Object: 10, Attribute: 1, Transportation: core.TransportTypeBestEffort}},
		InteractionTransports: []object.InteractionTransportState{{Publisher: 1, Class: 9, Transportation: core.TransportTypeReliable}},
	}
}

func TestObjectStateRejectsInvalidRecordsBeforeMutation(t *testing.T) {
	cases := map[string]func(*object.RegistryState){
		"object-zero":               func(s *object.RegistryState) { s.Objects[0].Handle = 0 },
		"object-duplicate":          func(s *object.RegistryState) { s.Objects = append(s.Objects, s.Objects[0]) },
		"name-duplicate":            func(s *object.RegistryState) { s.Objects[1].Name = s.Objects[0].Name },
		"name-empty":                func(s *object.RegistryState) { s.Objects[0].Name = "" },
		"name-invalid-utf8":         func(s *object.RegistryState) { s.Objects[0].Name = string([]byte{0xff}) },
		"class-zero":                func(s *object.RegistryState) { s.Objects[0].Class = 0 },
		"registrant-zero":           func(s *object.RegistryState) { s.Objects[0].Registrant = 0 },
		"counter-before-object":     func(s *object.RegistryState) { s.LastObjectHandle = 29 },
		"object-counter-overflow":   func(s *object.RegistryState) { s.LastObjectHandle = ^uint64(0) },
		"sequence-overflow":         func(s *object.RegistryState) { s.LastOutboundSequence = ^uint64(0) },
		"discovery-duplicate":       func(s *object.RegistryState) { s.Discoveries = append(s.Discoveries, s.Discoveries[0]) },
		"discovery-unknown-object":  func(s *object.RegistryState) { s.Discoveries[0].Object = 11 },
		"discovery-zero-federate":   func(s *object.RegistryState) { s.Discoveries[0].Federate = 0 },
		"scope-duplicate":           func(s *object.RegistryState) { s.Scopes = append(s.Scopes, s.Scopes[0]) },
		"scope-unknown-object":      func(s *object.RegistryState) { s.Scopes[0].Object = 11 },
		"scope-zero-federate":       func(s *object.RegistryState) { s.Scopes[0].Federate = 0 },
		"scope-empty":               func(s *object.RegistryState) { s.Scopes[0].Attributes = nil },
		"scope-zero-attribute":      func(s *object.RegistryState) { s.Scopes[0].Attributes[0] = 0 },
		"scope-duplicate-attribute": func(s *object.RegistryState) { s.Scopes[0].Attributes[0] = 2 },
		"reservation-duplicate":     func(s *object.RegistryState) { s.Reservations = append(s.Reservations, s.Reservations[0]) },
		"reservation-empty":         func(s *object.RegistryState) { s.Reservations[0].Name = "" },
		"reservation-invalid-utf8":  func(s *object.RegistryState) { s.Reservations[0].Name = string([]byte{0xff}) },
		"reservation-zero-holder":   func(s *object.RegistryState) { s.Reservations[0].Holder = 0 },
		"attribute-transport-duplicate": func(s *object.RegistryState) {
			s.AttributeTransports = append(s.AttributeTransports, s.AttributeTransports[0])
		},
		"attribute-transport-unknown-object": func(s *object.RegistryState) { s.AttributeTransports[0].Object = 11 },
		"attribute-transport-zero-attribute": func(s *object.RegistryState) { s.AttributeTransports[0].Attribute = 0 },
		"attribute-transport-unspecified":    func(s *object.RegistryState) { s.AttributeTransports[0].Transportation = core.TransportTypeUnspecified },
		"attribute-transport-invalid":        func(s *object.RegistryState) { s.AttributeTransports[0].Transportation = 99 },
		"interaction-transport-duplicate": func(s *object.RegistryState) {
			s.InteractionTransports = append(s.InteractionTransports, s.InteractionTransports[0])
		},
		"interaction-transport-zero-publisher": func(s *object.RegistryState) { s.InteractionTransports[0].Publisher = 0 },
		"interaction-transport-zero-class":     func(s *object.RegistryState) { s.InteractionTransports[0].Class = 0 },
		"interaction-transport-invalid":        func(s *object.RegistryState) { s.InteractionTransports[0].Transportation = 99 },
	}
	for name, corrupt := range cases {
		t.Run(name, func(t *testing.T) {
			r, _, _, _ := newRestoreRegistry(t)
			commit, err := r.PrepareImportState("fed", fullObjectState())
			if err != nil {
				t.Fatal(err)
			}
			commit()
			before := r.ExportState("fed")
			invalid := fullObjectState()
			corrupt(invalid)
			commit, err = r.PrepareImportState("fed", invalid)
			if err == nil || commit != nil {
				t.Fatal("invalid state returned a commit")
			}
			if !reflect.DeepEqual(before, r.ExportState("fed")) {
				t.Fatal("invalid state changed live registry")
			}
		})
	}
}

func TestObjectStateRegisteredReservationRoundTrip(t *testing.T) {
	for _, holder := range []core.FederateHandle{1, 2} {
		r, _, _, _ := newRestoreRegistry(t)
		state := fullObjectState()
		state.Reservations = []object.NameReservationState{{Name: "one", Holder: holder}}
		before := r.ExportState("fed")
		commit, err := r.PrepareImportState("fed", state)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(before, r.ExportState("fed")) {
			t.Fatal("prepare changed live state")
		}
		commit()
		if !reflect.DeepEqual(state, r.ExportState("fed")) {
			t.Fatal("registered name and independent reservation did not round trip")
		}
		if _, _, err := r.Register(context.Background(), "fed", holder, 7, "one"); !errors.Is(err, core.ErrObjectInstanceNameInUse) {
			t.Fatalf("reservation allowed duplicate live registration: %v", err)
		}
		if !reflect.DeepEqual(state, r.ExportState("fed")) {
			t.Fatal("duplicate registration changed restored state")
		}
		if err := r.ReleaseObjectInstanceName(context.Background(), "fed", holder, "one"); err != nil {
			t.Fatal(err)
		}
		if got, ok := r.LookupObjectInstanceByName("fed", "one"); !ok || got != 10 {
			t.Fatal("releasing restored reservation removed its live object")
		}
	}
}

func TestObjectStateCopiesInputAndOutput(t *testing.T) {
	r, _, _, _ := newRestoreRegistry(t)
	s := fullObjectState()
	commit, err := r.PrepareImportState("fed", s)
	if err != nil {
		t.Fatal(err)
	}
	s.Objects[0].Name = "mutated"
	s.Discoveries[0].Federate = 99
	s.Scopes[0].Attributes[0] = 99
	s.Reservations[0].Holder = 99
	s.AttributeTransports[0].Transportation = core.TransportTypeReliable
	s.InteractionTransports[0].Publisher = 99
	commit()
	if !reflect.DeepEqual(fullObjectState(), r.ExportState("fed")) {
		t.Fatal("prepared state aliases caller input")
	}
	s = r.ExportState("fed")
	s.Objects[0].Name = "mutated"
	s.Discoveries[0].Federate = 99
	s.Scopes[0].Attributes[0] = 99
	s.Reservations[0].Holder = 99
	s.AttributeTransports[0].Transportation = core.TransportTypeReliable
	s.InteractionTransports[0].Publisher = 99
	if !reflect.DeepEqual(fullObjectState(), r.ExportState("fed")) {
		t.Fatal("export aliases live state")
	}
	for i := 0; i < 20; i++ {
		if !reflect.DeepEqual(fullObjectState(), r.ExportState("fed")) {
			t.Fatal("non-deterministic export ordering")
		}
	}
}

func TestObjectStateEmptyClearsOnlyTargetFederation(t *testing.T) {
	for _, empty := range []*object.RegistryState{nil, {}} {
		r, _, out, _ := newRestoreRegistry(t)
		for _, fed := range []core.FederationName{"fed", "other"} {
			commit, err := r.PrepareImportState(fed, fullObjectState())
			if err != nil {
				t.Fatal(err)
			}
			commit()
		}
		other := r.ExportState("other")
		commit, err := r.PrepareImportState("fed", empty)
		if err != nil {
			t.Fatal(err)
		}
		commit()
		if !reflect.DeepEqual(&object.RegistryState{}, r.ExportState("fed")) {
			t.Fatalf("state not empty: %+v", r.ExportState("fed"))
		}
		if !reflect.DeepEqual(other, r.ExportState("other")) {
			t.Fatal("other federation changed")
		}
		if err := r.ReserveObjectInstanceName(context.Background(), "fed", 1, "reserved"); err != nil {
			t.Fatal(err)
		}
		if out.events[0].(*object.CallbackEvent).Kind != object.CallbackReservationSucceeded {
			t.Fatal("cleared reservation prevented a new reservation")
		}
		h, _, err := r.Register(context.Background(), "fed", 1, 7, "reserved")
		if err != nil || h != 1 {
			t.Fatalf("cleared counter/reservation: %d %v", h, err)
		}
		if out.events[0].Seq() != 1 {
			t.Fatal("cleared outbound counter did not restart")
		}
	}
}

func TestObjectStateRestoresCounterBeyondLastLiveObject(t *testing.T) {
	r, _, _, _ := newRestoreRegistry(t)
	state := fullObjectState()
	state.LastObjectHandle = 1<<53 + 19
	state.LastOutboundSequence = 1<<53 + 29
	commit, err := r.PrepareImportState("fed", state)
	if err != nil {
		t.Fatal(err)
	}
	commit()
	if err := r.ReserveObjectInstanceName(context.Background(), "fed", 1, "new"); err != nil {
		t.Fatal(err)
	}
	h, _, err := r.Register(context.Background(), "fed", 1, 7, "new")
	if err != nil || uint64(h) != state.LastObjectHandle+1 {
		t.Fatalf("counter: %d %v", h, err)
	}
}

func TestObjectStateAfterDeleteHasNoDanglingTransport(t *testing.T) {
	r, _, _, _ := newRestoreRegistry(t)
	ctx := context.Background()
	if err := r.ReserveObjectInstanceName(ctx, "fed", 1, "delete-me"); err != nil {
		t.Fatal(err)
	}
	h, _, err := r.Register(ctx, "fed", 1, 7, "delete-me")
	if err != nil {
		t.Fatal(err)
	}
	if err := r.ChangeAttributeTransportType(ctx, "fed", 1, h, []core.AttributeHandle{1}, core.TransportTypeReliable); err != nil {
		t.Fatal(err)
	}
	if err := r.Delete(ctx, "fed", 1, h, nil, nil); err != nil {
		t.Fatal(err)
	}
	s := r.ExportState("fed")
	if len(s.AttributeTransports) != 0 {
		t.Fatal("deleted object's transport survived")
	}
	if _, err := r.PrepareImportState("fed", s); err != nil {
		t.Fatalf("live state cannot be restored: %v", err)
	}
}

func TestObjectStateCanonicalOrdering(t *testing.T) {
	r, _, _, _ := newRestoreRegistry(t)
	want := fullObjectState()
	want.Discoveries = append(want.Discoveries, object.DiscoveryState{Object: 30, Federate: 1})
	want.Scopes = append(want.Scopes, object.ScopeState{Object: 10, Federate: 3, Attributes: []core.AttributeHandle{1, 2}}, object.ScopeState{Object: 30, Federate: 1, Attributes: []core.AttributeHandle{1, 2}})
	want.Reservations = append(want.Reservations, object.NameReservationState{Name: "z-reserved", Holder: 2})
	want.AttributeTransports = append(want.AttributeTransports, object.AttributeTransportState{Object: 10, Attribute: 2, Transportation: core.TransportTypeReliable}, object.AttributeTransportState{Object: 30, Attribute: 1, Transportation: core.TransportTypeReliable})
	want.InteractionTransports = append(want.InteractionTransports, object.InteractionTransportState{Publisher: 1, Class: 10, Transportation: core.TransportTypeReliable}, object.InteractionTransportState{Publisher: 2, Class: 9, Transportation: core.TransportTypeReliable})
	commit, err := r.PrepareImportState("fed", want)
	if err != nil {
		t.Fatal(err)
	}
	commit()
	unsorted := r.ExportState("fed")
	slices.Reverse(unsorted.Objects)
	slices.Reverse(unsorted.Discoveries)
	slices.Reverse(unsorted.Scopes)
	for _, scope := range unsorted.Scopes {
		slices.Reverse(scope.Attributes)
	}
	slices.Reverse(unsorted.Reservations)
	slices.Reverse(unsorted.AttributeTransports)
	slices.Reverse(unsorted.InteractionTransports)
	commit, err = r.PrepareImportState("fed", unsorted)
	if err != nil {
		t.Fatal(err)
	}
	commit()
	for i := 0; i < 20; i++ {
		if !reflect.DeepEqual(want, r.ExportState("fed")) {
			t.Fatal("export is not canonically ordered")
		}
	}
}

type registrationJournal struct {
	core.EventLog
	failure error
	calls   int
}

func (j *registrationJournal) Append(context.Context, core.FederationName, core.EventRecord) error {
	j.calls++
	return j.failure
}

func newJournalRegistry(t *testing.T, journal core.EventLog) (*object.Registry, *restoreOutbox, *int) {
	t.Helper()
	d := declaration.New()
	for _, holder := range []core.FederateHandle{1, 2} {
		if err := d.PublishObjectClassAttributes(context.Background(), "fed", holder, 7, []core.AttributeHandle{1}); err != nil {
			t.Fatal(err)
		}
	}
	out := &restoreOutbox{}
	registrations := new(int)
	r, err := object.New(object.Options{
		Declarations: d, Outbox: out, FOMs: restoreFOMs{}, Clock: core.NewRealClock(), EventLog: journal,
		OnRegister: func(core.FederationName, core.FederateHandle, core.ObjectHandle, core.ObjectClassHandle, []core.AttributeHandle) {
			*registrations++
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return r, out, registrations
}

func TestObjectRegisterJournalFailurePreservesNameState(t *testing.T) {
	for _, tc := range []struct {
		name, supplied, canonical string
		reserved                  bool
	}{
		{name: "reserved", supplied: "held", canonical: "held", reserved: true},
		{name: "generated", canonical: "HLAobj_7_1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			failure := errors.New("injected registration journal failure")
			journal := &registrationJournal{failure: failure}
			r, out, registrations := newJournalRegistry(t, journal)
			if tc.reserved {
				if err := r.ReserveObjectInstanceName(ctx, "fed", 1, tc.canonical); err != nil {
					t.Fatal(err)
				}
			}
			if err := r.ReserveObjectInstanceName(ctx, "other", 2, "other-held"); err != nil {
				t.Fatal(err)
			}
			before, other := r.ExportState("fed"), r.ExportState("other")
			events := len(out.events)
			h, name, err := r.Register(ctx, "fed", 1, 7, tc.supplied)
			if !errors.Is(err, failure) || h != 0 || name != "" {
				t.Fatalf("registration result: %d %q %v", h, name, err)
			}
			if journal.calls != 1 {
				t.Fatalf("failed persistence attempts = %d, want 1", journal.calls)
			}
			if len(out.events) != events || *registrations != 0 {
				t.Fatal("failed registration emitted callbacks or hooks")
			}
			if !reflect.DeepEqual(before, r.ExportState("fed")) {
				t.Errorf("failed registration changed saved state: before=%+v after=%+v", before, r.ExportState("fed"))
			}
			if _, ok := r.LookupObjectInstanceByName("fed", tc.canonical); ok {
				t.Fatal("failed object remained indexed")
			}
			if !reflect.DeepEqual(other, r.ExportState("other")) {
				t.Fatal("failed registration changed another federation")
			}

			failedState := r.ExportState("fed")
			probeReservation := func() {
				t.Helper()
				before := r.ExportState("fed")
				events := len(out.events)
				err := r.ReserveObjectInstanceName(ctx, "fed", 2, tc.canonical)
				if !tc.reserved {
					if !errors.Is(err, core.ErrObjectInstanceIllegalName) || len(out.events) != events || !reflect.DeepEqual(before, r.ExportState("fed")) {
						t.Fatalf("generated name reservation must be rejected without mutation: %v", err)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if len(out.events) != events+1 || out.events[events].(*object.CallbackEvent).Kind != object.CallbackReservationFailed {
					t.Fatal("original holder's reservation was lost")
				}
			}
			probeReservation()
			commit, err := r.PrepareImportState("fed", failedState)
			if err != nil {
				t.Fatal(err)
			}
			commit()
			probeReservation()

			commit, err = r.PrepareImportState("fed", before)
			if err != nil {
				t.Fatal(err)
			}
			commit()
			journal.failure = nil
			h, name, err = r.Register(ctx, "fed", 1, 7, tc.supplied)
			if err != nil || h != 1 || name != tc.canonical {
				t.Fatalf("retry did not reuse counter/name: %d %q %v", h, name, err)
			}
			if *registrations != 1 || journal.calls != 2 {
				t.Fatal("retry hook or journal count is incorrect")
			}
			probeReservation()
			if tc.reserved {
				if err := r.ReleaseObjectInstanceName(ctx, "fed", 1, tc.canonical); err != nil {
					t.Fatalf("successful registration lost the reservation: %v", err)
				}
				if got, ok := r.LookupObjectInstanceByName("fed", tc.canonical); !ok || got != h {
					t.Fatal("reservation release removed the registered object")
				}
			} else if err := r.ReleaseObjectInstanceName(ctx, "fed", 1, tc.canonical); !errors.Is(err, core.ErrObjectInstanceNameNotReserved) {
				t.Fatalf("generated registration created a reservation: %v", err)
			}
		})
	}
}

func TestObjectRegisterJournalFailurePreservesReservationRelease(t *testing.T) {
	ctx := context.Background()
	failure := errors.New("injected registration journal failure")
	r, out, _ := newJournalRegistry(t, &registrationJournal{failure: failure})
	if err := r.ReserveObjectInstanceName(ctx, "fed", 1, "held"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.Register(ctx, "fed", 1, 7, "held"); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if err := r.ReleaseObjectInstanceName(ctx, "fed", 2, "held"); !errors.Is(err, core.ErrObjectInstanceNameReservedByOther) {
		t.Errorf("wrong holder release: %v", err)
	}
	if err := r.ReleaseObjectInstanceName(ctx, "fed", 1, "held"); err != nil {
		t.Errorf("original holder could not release reservation: %v", err)
	}
	if err := r.ReserveObjectInstanceName(ctx, "fed", 2, "held"); err != nil {
		t.Fatal(err)
	}
	if out.events[len(out.events)-1].(*object.CallbackEvent).Kind != object.CallbackReservationSucceeded {
		t.Fatal("released name could not be re-reserved")
	}
}

func TestObjectRegisterUnreservedNameDoesNotAppendJournal(t *testing.T) {
	ctx := context.Background()
	journal := &registrationJournal{failure: errors.New("must not append")}
	r, out, registrations := newJournalRegistry(t, journal)
	before := r.ExportState("fed")
	if h, name, err := r.Register(ctx, "fed", 1, 7, "named"); !errors.Is(err, core.ErrObjectInstanceNameNotReserved) || h != 0 || name != "" {
		t.Fatalf("unreserved registration: %d %q %v", h, name, err)
	}
	if journal.calls != 0 || *registrations != 0 || len(out.events) != 0 {
		t.Fatal("unreserved registration persisted or emitted callbacks/hooks")
	}
	if !reflect.DeepEqual(before, r.ExportState("fed")) {
		t.Fatal("unreserved registration changed state")
	}
	if err := r.ReserveObjectInstanceName(ctx, "fed", 1, "named"); err != nil {
		t.Fatal(err)
	}
	journal.failure = nil
	if h, name, err := r.Register(ctx, "fed", 1, 7, "named"); err != nil || h != 1 || name != "named" {
		t.Fatalf("registration after acquiring reservation: %d %q %v", h, name, err)
	}
	if journal.calls != 1 || *registrations != 1 {
		t.Fatal("valid retry did not persist exactly once")
	}
}

func TestObjectRegisterForeignReservationDoesNotAppendJournal(t *testing.T) {
	ctx := context.Background()
	journal := &registrationJournal{failure: errors.New("must not append")}
	r, _, _ := newJournalRegistry(t, journal)
	if err := r.ReserveObjectInstanceName(ctx, "fed", 2, "held"); err != nil {
		t.Fatal(err)
	}
	before := r.ExportState("fed")
	if _, _, err := r.Register(ctx, "fed", 1, 7, "held"); !errors.Is(err, core.ErrObjectInstanceNameNotReserved) {
		t.Fatalf("foreign reservation: %v", err)
	}
	if journal.calls != 0 {
		t.Fatal("rejected registration appended a journal record")
	}
	if !reflect.DeepEqual(before, r.ExportState("fed")) {
		t.Fatal("foreign reservation changed")
	}
}

type blockingRegistrationJournal struct {
	core.EventLog
	entered, proceed chan struct{}
	failure          error
}

func (j blockingRegistrationJournal) Append(context.Context, core.FederationName, core.EventRecord) error {
	close(j.entered)
	<-j.proceed
	return j.failure
}

func TestObjectRegisterJournalFailureAllowsConcurrentReservationRelease(t *testing.T) {
	ctx := context.Background()
	failure := errors.New("injected registration journal failure")
	journal := blockingRegistrationJournal{entered: make(chan struct{}), proceed: make(chan struct{}), failure: failure}
	r, out, _ := newJournalRegistry(t, journal)
	if err := r.ReserveObjectInstanceName(ctx, "fed", 1, "held"); err != nil {
		t.Fatal(err)
	}
	registered := make(chan error, 1)
	go func() {
		_, _, err := r.Register(ctx, "fed", 1, 7, "held")
		registered <- err
	}()
	select {
	case <-journal.entered:
	case <-time.After(2 * time.Second):
		close(journal.proceed)
		t.Fatal("registration did not reach journal")
	}
	released := make(chan error, 1)
	go func() { released <- r.ReleaseObjectInstanceName(ctx, "fed", 1, "held") }()
	close(journal.proceed)
	select {
	case err := <-registered:
		if !errors.Is(err, failure) {
			t.Errorf("register: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("registration did not release its locks")
	}
	select {
	case err := <-released:
		if err != nil {
			t.Errorf("release lost the caller's reservation: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("reservation release remained blocked")
	}
	if err := r.ReserveObjectInstanceName(ctx, "fed", 2, "held"); err != nil {
		t.Fatal(err)
	}
	if out.events[len(out.events)-1].(*object.CallbackEvent).Kind != object.CallbackReservationSucceeded {
		t.Fatal("released name could not be re-reserved")
	}
}
