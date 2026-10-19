package server

import (
	"context"
	"encoding/binary"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
)

func TestMOMCallbackReceiptDoesNotReportItsOwnTelemetry(t *testing.T) {
	d, fom, a, b, out := newMOMCatalogDeps(t)
	ctx := context.Background()
	const fed core.FederationName = "catalog-provider"
	manager, _ := mom.New(mom.Options{Outbox: out})
	d.Manager = manager
	dispatcher := mom.NewDispatcher(manager)
	dispatcher.SetEmitter(mom.NewProductionEmitter(out))
	if err := configureMOMCatalog(dispatcher, d); err != nil {
		t.Fatal(err)
	}
	observer, receipts, err := newMOMRuntimeObservers(momObservationDependencies{Manager: manager, Dispatcher: dispatcher, FOMs: d.FOMs, Authenticate: func(context.Context, core.FederationName, core.FederateHandle) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range []core.FederateHandle{a, b} {
		if err := manager.FederateJoined(ctx, fed, h, "member", "test"); err != nil {
			t.Fatal(err)
		}
	}
	state := manager.ExportState(fed)
	for i := range state.Federates {
		state.Federates[i].ServiceReportingEnabled = true
	}
	commit, err := manager.PrepareImportState(fed, state)
	if err != nil {
		t.Fatal(err)
	}
	commit()
	report, _ := fom.LookupInteractionClass(mom.ClassReportServiceInvocation)
	if err := d.Declarations.SubscribeInteractionClass(ctx, fed, b, report); err != nil {
		t.Fatal(err)
	}
	observer.Observe(ctx, rtiv1.ObjectService_UpdateAttributeValues_FullMethodName, &rtiv1.UpdateAttributeValuesRequest{FederationName: string(fed), FederateHandle: uint64(a), ObjectHandle: 42}, &rtiv1.Empty{}, nil)
	if len(out.events) != 1 {
		t.Fatalf("service reports=%d", len(out.events))
	}
	actual, ok := out.events[0].(*mom.InteractionEvent)
	if !ok {
		t.Fatal("not production MOM interaction")
	}
	parameters := map[uint64][]byte{}
	for h, v := range actual.Parameters {
		parameters[uint64(h)] = v
	}
	callback := &rtiv1.FederateEvent{Event: &rtiv1.FederateEvent_Receive{Receive: &rtiv1.ReceiveInteraction{InteractionClassHandle: uint64(actual.Class), Parameters: parameters}}}
	ticket, sent, err := receipts.Prepare(ctx, fed, b, callback)
	if err != nil {
		t.Fatal(err)
	}
	sent(true)
	if err := receipts.Report(ctx, fed, b, ticket, true, ""); err != nil {
		t.Fatal(err)
	}
	if len(out.events) != 1 {
		t.Fatalf("telemetry callback generated recursive report: %d", len(out.events))
	}
	pulse, _ := fom.LookupInteractionClass("Pulse")
	callback.Event = &rtiv1.FederateEvent_Receive{Receive: &rtiv1.ReceiveInteraction{InteractionClassHandle: uint64(pulse)}}
	ticket, sent, err = receipts.Prepare(ctx, fed, b, callback)
	if err != nil {
		t.Fatal(err)
	}
	sent(true)
	if err := receipts.Report(ctx, fed, b, ticket, true, ""); err != nil {
		t.Fatal(err)
	}
	if len(out.events) != 2 {
		t.Fatalf("ordinary callback report suppressed: %d", len(out.events))
	}
	values, err := manager.FederateMetricValues(ctx, fed, b)
	if err != nil || binary.BigEndian.Uint32(values.Values["HLAinteractionsReceived"]) != 2 {
		t.Fatal("telemetry callback not counted as actual invocation", values, err)
	}
}
