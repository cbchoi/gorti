package savebundle

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
	savepoint "github.com/cbchoi/gorti/rti/internal/hla/save_restore"
)

var _ savepoint.ManagerSnapshotter = Object{}
var _ savepoint.PreparedSnapshotter = Object{}

type objectStateOutbox struct{}

func (objectStateOutbox) Send(context.Context, core.FederationName, core.FederateHandle, core.OutboundEvent) error {
	return nil
}

type objectStateFOMs struct{ core.FOMRepository }

func objectStateCodec(t *testing.T) Object {
	t.Helper()
	r, err := object.New(object.Options{Declarations: declaration.New(), Outbox: objectStateOutbox{}, FOMs: objectStateFOMs{}, Clock: core.NewRealClock()})
	if err != nil {
		t.Fatal(err)
	}
	return Object{Manager: r}
}

func codecObjectState() *object.RegistryState {
	return &object.RegistryState{
		LastObjectHandle: 1<<53 + 7, LastOutboundSequence: 1<<53 + 11,
		Objects:               []object.InstanceState{{Handle: 1<<53 + 7, Name: "saved", Class: 3, Registrant: ^core.FederateHandle(0)}},
		Discoveries:           []object.DiscoveryState{{Object: 1<<53 + 7, Federate: 2}},
		Scopes:                []object.ScopeState{{Object: 1<<53 + 7, Federate: 2, Attributes: []core.AttributeHandle{1, 5}}},
		Reservations:          []object.NameReservationState{{Name: "reserved", Holder: 2}},
		AttributeTransports:   []object.AttributeTransportState{{Object: 1<<53 + 7, Attribute: 5, Transportation: core.TransportTypeReliable}},
		InteractionTransports: []object.InteractionTransportState{{Publisher: 2, Class: 4, Transportation: core.TransportTypeBestEffort}},
	}
}

func seedObjectCodec(t *testing.T, s Object) {
	t.Helper()
	commit, err := s.Manager.PrepareImportState("fed", codecObjectState())
	if err != nil {
		t.Fatal(err)
	}
	commit()
}

func TestObjectSnapshotJSONRoundTripAndDeterminism(t *testing.T) {
	s := objectStateCodec(t)
	seedObjectCodec(t, s)
	data, err := s.Marshal("fed")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		next, err := s.Marshal("fed")
		if err != nil || !bytes.Equal(data, next) {
			t.Fatalf("non-deterministic JSON: %v", err)
		}
	}
	var envelope struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil || envelope.Version != 1 {
		t.Fatalf("missing snapshot version: %s %v", data, err)
	}
	dst := objectStateCodec(t)
	commit, err := dst.PrepareRestore("fed", data)
	if err != nil {
		t.Fatal(err)
	}
	if len(dst.Manager.ExportState("fed").Objects) != 0 {
		t.Fatal("prepare mutated registry")
	}
	clear(data)
	commit()
	if !reflect.DeepEqual(codecObjectState(), dst.Manager.ExportState("fed")) {
		t.Fatal("round trip lost state or integer precision")
	}
}

func TestObjectSnapshotRejectsCorruptionBeforeMutation(t *testing.T) {
	valid, _ := objectStateCodec(t).Marshal("fed")
	cases := map[string][]byte{
		"garbage": {0xff}, "truncated": []byte(`{"version":1,`),
		"unsupported-version":  []byte(`{"version":5,"state":{}}`),
		"missing-version":      []byte(`{"state":{}}`),
		"missing-state":        []byte(`{"version":1}`),
		"null-state":           []byte(`{"version":1,"state":null}`),
		"null-envelope":        []byte(`null`),
		"trailing-document":    append(append([]byte(nil), valid...), []byte(` {}`)...),
		"unknown-field":        []byte(`{"version":1,"state":{},"typo":1}`),
		"unknown-state-field":  []byte(`{"version":1,"state":{"Unexpected":1}}`),
		"duplicate-key":        []byte(`{"version":2,"version":1,"state":{}}`),
		"duplicate-case-alias": []byte(`{"version":2,"Version":1,"state":{}}`),
		"duplicate-nested-key": []byte(`{"version":1,"state":{"LastObjectHandle":1,"lastobjecthandle":0}}`),
		"invalid-counter":      []byte(`{"version":1,"state":{"LastObjectHandle":-1}}`),
		"duplicate-record":     []byte(`{"version":1,"state":{"Reservations":[{"Name":"a","Holder":1},{"Name":"a","Holder":1}]}}`),
		"invalid-record":       []byte(`{"version":1,"state":{"Reservations":[{"Name":"","Holder":1}]}}`),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			s := objectStateCodec(t)
			seedObjectCodec(t, s)
			before := s.Manager.ExportState("fed")
			commit, err := s.PrepareRestore("fed", data)
			if err == nil || commit != nil {
				t.Fatal("corrupt snapshot accepted")
			}
			if !reflect.DeepEqual(before, s.Manager.ExportState("fed")) {
				t.Fatal("rejected restore changed state")
			}
			if err := s.Unmarshal("fed", data); err == nil {
				t.Fatal("Unmarshal accepted corruption")
			}
			if !reflect.DeepEqual(before, s.Manager.ExportState("fed")) {
				t.Fatal("rejected Unmarshal changed state")
			}
		})
	}
}

func TestObjectSnapshotEmptyClearsState(t *testing.T) {
	for _, data := range [][]byte{nil, {}, []byte(`{"version":1,"state":{}}`)} {
		s := objectStateCodec(t)
		seedObjectCodec(t, s)
		if err := s.Unmarshal("fed", data); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(&object.RegistryState{}, s.Manager.ExportState("fed")) {
			t.Fatal("empty snapshot did not clear state")
		}
	}
}

func TestObjectSnapshotNilRegistryReturnsError(t *testing.T) {
	s := Object{}
	if _, err := s.Marshal("fed"); err == nil {
		t.Fatal("nil registry accepted")
	}
	if commit, err := s.PrepareRestore("fed", nil); err == nil || commit != nil {
		t.Fatal("nil registry accepted")
	}
	if err := s.Unmarshal("fed", nil); err == nil {
		t.Fatal("nil registry accepted")
	}
}

func TestObjectSnapshotMarshalRejectsInvalidLiveState(t *testing.T) {
	s := objectStateCodec(t)
	// This legacy entry point permits an unknown enum. A save must fail rather
	// than produce an image which restore would reject.
	if err := s.Manager.ChangeInteractionTransportType(context.Background(), "fed", 1, 2, 99); err != nil {
		t.Fatal(err)
	}
	if data, err := s.Marshal("fed"); err == nil || data != nil {
		t.Fatal("invalid live state produced a save image")
	}
}
