package savebundle

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
	"google.golang.org/protobuf/proto"
)

type ownershipSolicitationOutbox struct {
	events []*ownership.CallbackEvent
}

func (o *ownershipSolicitationOutbox) Send(_ context.Context, _ core.FederationName, _ core.FederateHandle, event core.OutboundEvent) error {
	if callback, ok := event.(*ownership.CallbackEvent); ok && callback.Kind == ownership.CallbackAssumption {
		o.events = append(o.events, callback)
	}
	return nil
}

func TestOwnershipSolicitationCheckpoint(t *testing.T) {
	for _, phase := range []string{"only-unowned", testMixed, "departed-divester"} {
		t.Run(phase, func(t *testing.T) {
			ctx := context.Background()
			manager, err := ownership.New(ownership.Options{Outbox: objectStateOutbox{}})
			if err != nil {
				t.Fatal(err)
			}
			attrs := []core.AttributeHandle{1}
			if phase == testMixed {
				attrs = append(attrs, 2)
			}
			manager.RegisterInitialOwnership("f", 1, 10, attrs)
			if err := manager.UnconditionalDivest(ctx, "f", 1, 10, []core.AttributeHandle{1}); err != nil {
				t.Fatal(err)
			}
			if phase == testMixed {
				if err := manager.NegotiatedDivestTwoPhase(ctx, "f", 1, 10, []core.AttributeHandle{2}, []byte{7, 0, 255}); err != nil {
					t.Fatal(err)
				}
			}
			if phase == "departed-divester" {
				plan, err := manager.PrepareResign("f", 1, core.ResignActionNoAction, nil)
				if err != nil {
					t.Fatal(err)
				}
				plan.Commit()
			}
			before := manager.ExportState("f")
			data, err := (Ownership{Manager: manager}).Marshal("f")
			if err != nil {
				t.Fatal(err)
			}
			wire, _, offers, err := decodeOwnershipSnapshotState(data)
			if err != nil || len(offers) != 1 {
				t.Fatal("lost unowned continuation", offers, err)
			}
			if phase != testMixed && proto.Size(wire) != 0 {
				t.Fatal("unowned-only snapshot unexpectedly contains legacy state")
			}
			if err := proto.Unmarshal(data, &rtiv1.OwnershipManagerState{}); err == nil {
				t.Fatal("legacy protobuf reader silently accepted extended state")
			}
			if _, _, err := decodeOwnershipSnapshot(data); err == nil {
				t.Fatal("partial-state decoder silently dropped unowned offers")
			}
			out := &ownershipSolicitationOutbox{}
			restored, err := ownership.New(ownership.Options{Outbox: out, Subscribers: func(context.Context, core.FederationName, core.ObjectHandle, []core.AttributeHandle) []core.FederateHandle {
				return []core.FederateHandle{1, 2}
			}})
			if err != nil {
				t.Fatal(err)
			}
			restored.RegisterInitialOwnership("f", 3, 99, []core.AttributeHandle{9})
			live := restored.ExportState("f")
			commit, err := (Ownership{Manager: restored}).PrepareRestore("f", data)
			if err != nil || commit == nil {
				t.Fatal("restore did not prepare", err)
			}
			if !reflect.DeepEqual(live, restored.ExportState("f")) {
				t.Fatal("restore preparation mutated live state")
			}
			commit()
			if !reflect.DeepEqual(before, restored.ExportState("f")) {
				t.Fatal("checkpoint lost solicitation state")
			}
			roundTrip, err := (Ownership{Manager: restored}).Marshal("f")
			if err != nil || !bytes.Equal(data, roundTrip) {
				t.Fatal("checkpoint encoding is not stable", err)
			}
			if phase != "departed-divester" {
				restored.ObjectClassPublished(ctx, "f", 1, 7, attrs)
				if len(out.events) != 0 {
					t.Fatal("former divester solicited after restore")
				}
			}
			restored.ObjectClassPublished(ctx, "f", 2, 7, attrs)
			if len(out.events) != len(attrs) || !reflect.DeepEqual(out.events[0].Attributes, []core.AttributeHandle{1}) || len(out.events[0].Tag) != 0 {
				t.Fatal("unowned solicitation did not continue after restore", out.events)
			}
			if phase == testMixed && (!reflect.DeepEqual(out.events[1].Attributes, []core.AttributeHandle{2}) || !bytes.Equal(out.events[1].Tag, []byte{7, 0, 255})) {
				t.Fatal("negotiated and unconditional requests were merged or retagged")
			}
			if err := restored.AcquireIfAvailable(ctx, "f", 2, 10, []core.AttributeHandle{1}, nil); err != nil {
				t.Fatal(err)
			}
			if len(restored.ExportState("f").UnownedOffers) != 0 {
				t.Fatal("acquisition retained continuation")
			}
		})
	}
}

func TestOwnershipSolicitationCorruptCheckpointAtomic(t *testing.T) {
	valid := ownership.UnownedOfferState{Object: 10, Attribute: 1, Divester: 1}
	for _, name := range []string{"object", "attribute", "rti-divester", "duplicate", "owned", "zero-owner-record", "negotiated", "version", "missing-history"} {
		t.Run(name, func(t *testing.T) {
			wire := &rtiv1.OwnershipManagerState{}
			envelope := ownershipSnapshotEnvelope{Version: 2, ConfirmationRequested: []bool{}, UnownedOffers: []ownership.UnownedOfferState{valid}}
			switch name {
			case "object":
				envelope.UnownedOffers[0].Object = 0
			case "attribute":
				envelope.UnownedOffers[0].Attribute = 0
			case "rti-divester":
				envelope.UnownedOffers[0].Divester = ^core.FederateHandle(0)
			case "duplicate":
				envelope.UnownedOffers = append(envelope.UnownedOffers, valid)
			case "owned", "zero-owner-record":
				owner := uint64(2)
				if name == "zero-owner-record" {
					owner = 0
				}
				wire.Owners = []*rtiv1.OwnershipRecordEntry{{ObjectHandle: 10, AttributeHandle: 1, OwnerFederate: owner}}
			case "negotiated":
				wire.PendingDivests = []*rtiv1.PendingDivestEntry{{ObjectHandle: 10, AttributeHandle: 1, OwnerFederate: 1, TwoPhase: true}}
				envelope.ConfirmationRequested = []bool{false}
			case "version":
				envelope.Version = 1
			case "missing-history":
				envelope.ConfirmationRequested = nil
			}
			var err error
			envelope.LegacyProtoBytes, err = proto.Marshal(wire)
			if err != nil {
				t.Fatal(err)
			}
			payload, err := json.Marshal(envelope)
			if err != nil {
				t.Fatal(err)
			}
			data := append([]byte(ownershipSnapshotMagic), payload...)
			manager, err := ownership.New(ownership.Options{Outbox: objectStateOutbox{}})
			if err != nil {
				t.Fatal(err)
			}
			manager.RegisterInitialOwnership("f", 3, 99, []core.AttributeHandle{9})
			before := manager.ExportState("f")
			commit, err := (Ownership{Manager: manager}).PrepareRestore("f", data)
			if err == nil || commit != nil {
				t.Fatal("invalid continuation prepared for restore")
			}
			if !reflect.DeepEqual(before, manager.ExportState("f")) {
				t.Fatal("invalid restore mutated live state")
			}
		})
	}
}

const testMixed = "mixed"
