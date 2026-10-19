package savebundle

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
	"google.golang.org/protobuf/proto"
)

func TestOwnershipConfirmationCheckpoint(t *testing.T) {
	for _, phase := range []string{"waiting", "requested", "withdrawn"} {
		t.Run(phase, func(t *testing.T) {
			ctx := context.Background()
			m, err := ownership.New(ownership.Options{Outbox: objectStateOutbox{}})
			if err != nil {
				t.Fatal(err)
			}
			attrs := []core.AttributeHandle{1}
			m.RegisterInitialOwnership("f", 1, 1, attrs)
			if err := m.NegotiatedDivestTwoPhase(ctx, "f", 1, 1, attrs, []byte("offer")); err != nil {
				t.Fatal(err)
			}
			if phase != "waiting" {
				if err := m.Acquire(ctx, "f", 2, 1, attrs, []byte("acquire")); err != nil {
					t.Fatal(err)
				}
			}
			if phase == "withdrawn" {
				if err := m.CancelAcquire(ctx, "f", 2, 1, attrs); err != nil {
					t.Fatal(err)
				}
			}
			before := m.ExportState("f")
			data, err := (Ownership{Manager: m}).Marshal("f")
			if err != nil {
				t.Fatal(err)
			}
			restored, err := ownership.New(ownership.Options{Outbox: objectStateOutbox{}})
			if err != nil {
				t.Fatal(err)
			}
			if err := (Ownership{Manager: restored}).Unmarshal("f", data); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, restored.ExportState("f")) {
				t.Error("checkpoint lost confirmation state")
			}
			err = restored.ConfirmDivestiture(ctx, "f", 1, 1, attrs)
			want := map[string]error{"waiting": core.ErrOwnershipConfirmationNotRequested, "withdrawn": core.ErrOwnershipNoAcquisitionPending}[phase]
			if !errors.Is(err, want) {
				t.Fatalf("confirm after restore: %v, want %v", err, want)
			}
		})
	}
}

func TestOwnershipConfirmationLegacyAndRebind(t *testing.T) {
	wire := &rtiv1.OwnershipManagerState{
		Owners:          []*rtiv1.OwnershipRecordEntry{{ObjectHandle: 1, AttributeHandle: 1, OwnerFederate: 1}},
		PendingDivests:  []*rtiv1.PendingDivestEntry{{ObjectHandle: 1, AttributeHandle: 1, OwnerFederate: 1, TwoPhase: true}},
		PendingAcquires: []*rtiv1.PendingAcquireEntry{{ObjectHandle: 1, AttributeHandle: 1, AcquirerFederate: 2}},
	}
	for _, phase := range []string{"legacy", "requested", "deferred"} {
		t.Run(phase, func(t *testing.T) {
			var history []bool
			if phase != "legacy" {
				history = []bool{phase == "requested"}
			}
			data, err := marshalOwnershipSnapshot(wire, history)
			if err != nil {
				t.Fatal(err)
			}
			var old rtiv1.OwnershipManagerState
			if err := proto.Unmarshal(data, &old); (err == nil) != (phase == "legacy") {
				t.Fatalf("legacy reader: %v", err)
			}
			rebound, err := (Ownership{}).RewriteFederates(data, func(h core.FederateHandle) (core.FederateHandle, error) { return h + 100, nil })
			if err != nil {
				t.Fatal(err)
			}
			m, err := ownership.New(ownership.Options{Outbox: objectStateOutbox{}})
			if err != nil {
				t.Fatal(err)
			}
			if err := (Ownership{Manager: m}).Unmarshal("f", rebound); err != nil {
				t.Fatal(err)
			}
			err = m.ConfirmDivestiture(context.Background(), "f", 101, 1, []core.AttributeHandle{1})
			if phase == "deferred" {
				if !errors.Is(err, core.ErrOwnershipConfirmationNotRequested) {
					t.Fatalf("deferred confirmation: %v", err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if h, _ := m.QueryOwnership("f", 1, 1); h != 102 {
					t.Fatalf("new owner: %d", h)
				}
			}
		})
	}
}

func TestOwnershipConfirmationCorruptCheckpointAtomic(t *testing.T) {
	wire := &rtiv1.OwnershipManagerState{PendingDivests: []*rtiv1.PendingDivestEntry{{ObjectHandle: 1, AttributeHandle: 1, OwnerFederate: 1, TwoPhase: true}}}
	valid, err := marshalOwnershipSnapshot(wire, []bool{true})
	if err != nil {
		t.Fatal(err)
	}
	wire.PendingDivests[0].TwoPhase = false
	orphan, err := marshalOwnershipSnapshot(wire, []bool{true})
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{
		"future":    bytes.Replace(valid, []byte(`"version":1`), []byte(`"version":2`), 1),
		"missing":   bytes.Replace(valid, []byte(`"confirmationRequested":[true]`), []byte(`"confirmationRequested":null`), 1),
		"count":     bytes.Replace(valid, []byte(`"confirmationRequested":[true]`), []byte(`"confirmationRequested":[]`), 1),
		"duplicate": bytes.Replace(valid, []byte(`"version":1`), []byte(`"version":1,"version":1`), 1),
		"orphan":    orphan,
	} {
		t.Run(name, func(t *testing.T) {
			m, err := ownership.New(ownership.Options{Outbox: objectStateOutbox{}})
			if err != nil {
				t.Fatal(err)
			}
			m.RegisterInitialOwnership("f", 3, 5, []core.AttributeHandle{2})
			before := m.ExportState("f")
			commit, err := (Ownership{Manager: m}).PrepareRestore("f", data)
			if err == nil || commit != nil {
				t.Fatal("corrupt state prepared for restore")
			}
			if !reflect.DeepEqual(before, m.ExportState("f")) {
				t.Fatal("failed restore mutated ownership")
			}
			if _, err := (Ownership{}).RewriteFederates(data, func(h core.FederateHandle) (core.FederateHandle, error) { return h, nil }); err == nil {
				t.Fatal("corrupt state rebound")
			}
		})
	}
}
