package server

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/adapters/delivery"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
)

type validationLease struct {
	held  bool
	reads int
}

func (v *validationLease) AcquireMemberSnapshot(context.Context, core.FederationName, core.FederateHandle, uint64) (func(), error) {
	v.held = true
	return func() { v.held = false }, nil
}

type validationFOM struct {
	view  core.FOMView
	lease *validationLease
}

func (v validationFOM) FOMView(_ context.Context, _ core.FederationName) (core.FOMView, error) {
	if !v.lease.held {
		return core.FOMView{}, errors.New("snapshot read outside lease")
	}
	v.lease.reads++
	return v.view, nil
}

type validationDeclarations struct{ state *declaration.ManagerState }

func (v validationDeclarations) ExportState(core.FederationName) *declaration.ManagerState {
	return v.state
}

type validationObjects struct{ state *object.RegistryState }

func (v validationObjects) ExportState(core.FederationName) *object.RegistryState { return v.state }

type validationOwners struct{ state *ownership.ManagerState }

func (v validationOwners) ExportState(core.FederationName) *ownership.ManagerState { return v.state }

func validationFixture(t *testing.T) (core.LocalValidationStateSource, *delivery.MultiOutbox, *validationLease) {
	t.Helper()
	lease := &validationLease{}
	out := delivery.NewWithBatch(32, 1, 0)
	out.Bind("fed", 7)
	fom := validationFOM{lease: lease, view: core.FOMView{Revision: 1, ObjectClasses: []core.FOMObjectClassBinding{{Handle: 2, Name: "Thing", Attributes: []core.FOMAttributeBinding{{Handle: 3, Name: "value"}, {Handle: 4, Name: "other"}}}}, InteractionClasses: []core.FOMInteractionClassBinding{{Handle: 9, Name: "Ping"}}}}
	decl := validationDeclarations{&declaration.ManagerState{ObjectPublications: []declaration.ObjectDeclarationState{{Class: 2, Attribute: 3, Federates: []core.FederateHandle{7}}}, InteractionPublications: []declaration.InteractionDeclarationState{{Class: 9, Federates: []core.FederateHandle{7}}}}}
	objects := validationObjects{&object.RegistryState{Objects: []object.InstanceState{{Handle: 11, Class: 2, Registrant: 7}, {Handle: 12, Class: 2, Registrant: 8}, {Handle: 13, Class: 2, Registrant: 8}}, Discoveries: []object.DiscoveryState{{Object: 11, Federate: 7}, {Object: 12, Federate: 7}}}}
	owners := validationOwners{&ownership.ManagerState{Owners: []ownership.OwnerState{{Object: 11, Attribute: 3, Owner: 8}, {Object: 12, Attribute: 4, Owner: 7}}}}
	s := newLocalValidationSource(lease, fom, decl, objects, owners, out, func(context.Context, core.FederationName, core.FederateHandle) error {
		if !lease.held {
			t.Error("authentication outside snapshot lease")
		}
		return nil
	})
	return s, out, lease
}

func TestLocalValidationSnapshotIncludesAuthoritativeKnowledgeAndBarrier(t *testing.T) {
	s, out, lease := validationFixture(t)
	ctx := context.Background()
	prior := core.LocalValidationBarrierEvent{SnapshotID: 10, Federate: 7}
	if err := out.Send(ctx, "fed", 7, prior); err != nil {
		t.Fatal(err)
	}
	state, err := s.LocalValidationState(ctx, "fed", 7, 3, 0, 11)
	if err != nil {
		t.Fatal(err)
	}
	if lease.held {
		t.Fatal("snapshot lease leaked")
	}
	if state.FederationGeneration != 3 || state.SnapshotID != 11 || state.Federate != 7 {
		t.Fatalf("identity: %+v", state)
	}
	want := []core.LocalKnownObject{{Object: 11, Class: 2}, {Object: 12, Class: 2, Owned: []core.AttributeHandle{4}}}
	if !reflect.DeepEqual(state.Objects, want) {
		t.Fatalf("known/owned state = %+v, want %+v", state.Objects, want)
	}
	if !reflect.DeepEqual(state.Publications, []core.LocalObjectPublication{{Class: 2, Attributes: []core.AttributeHandle{3}}}) || !reflect.DeepEqual(state.PublishedInteractions, []core.InteractionClassHandle{9}) {
		t.Fatalf("declarations: %+v", state)
	}
	ch, cancel, err := out.Subscribe(ctx, "fed", 7)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := cancel(); err != nil {
			t.Error(err)
		}
	}()
	if got := (<-ch)[0].(core.LocalValidationBarrierEvent).SnapshotID; got != 10 {
		t.Fatalf("prior event reordered: %d", got)
	}
	if got := (<-ch)[0].(core.LocalValidationBarrierEvent); got.SnapshotID != 11 || got.CallbackEpoch != 0 || got.Federate != 7 {
		t.Fatalf("snapshot marker: %+v", got)
	}
}

func TestLocalValidationSnapshotRejectsWithoutMarker(t *testing.T) {
	for _, kind := range []string{"auth", "epoch", "snapshot", "capacity", "knowledge"} {
		t.Run(kind, func(t *testing.T) {
			src, out, lease := validationFixture(t)
			s := src.(*localValidationSource)
			epoch, id := uint64(0), uint64(11)
			switch kind {
			case "auth":
				s.authenticate = func(context.Context, core.FederationName, core.FederateHandle) error {
					return errors.New("invalid session")
				}
			case "epoch":
				epoch = 1
			case "snapshot":
				id = 0
			case "capacity":
				out.Unbind("fed", 7)
			case "knowledge":
				s.owners = validationOwners{&ownership.ManagerState{Owners: []ownership.OwnerState{{Object: 13, Attribute: 3, Owner: 7}}}}
			}
			if _, err := s.LocalValidationState(context.Background(), "fed", 7, 3, epoch, id); err == nil {
				t.Fatal("accepted invalid snapshot")
			}
			if lease.held {
				t.Fatal("snapshot lease leaked")
			}
			if kind != "capacity" {
				ch, cancel, err := out.Subscribe(context.Background(), "fed", 7)
				if err != nil {
					t.Fatal(err)
				}
				defer func() {
					if err := cancel(); err != nil {
						t.Error(err)
					}
				}()
				select {
				case <-ch:
					t.Fatal("failed snapshot emitted marker")
				default:
				}
			}
		})
	}
}

func TestLocalValidationSnapshotDoesNotResurrectLocallyDeletedRegistrant(t *testing.T) {
	src, _, _ := validationFixture(t)
	s := src.(*localValidationSource)
	s.objects = validationObjects{&object.RegistryState{Objects: []object.InstanceState{{Handle: 11, Class: 2, Registrant: 7}}}}
	s.owners = validationOwners{&ownership.ManagerState{}}
	state, err := s.LocalValidationState(context.Background(), "fed", 7, 3, 0, 15)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Objects) != 0 {
		t.Fatalf("historical registration resurrected forgotten object: %+v", state.Objects)
	}
}
