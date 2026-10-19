package mom

import (
	"bytes"
	"context"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func completeSnapshotManager(t *testing.T) *Manager {
	t.Helper()
	m, err := New(Options{Outbox: snapFakeOutbox{}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := m.FederationCreated(ctx, "fed", []core.FOMModule{{Path: "base.xml"}}); err != nil {
		t.Fatal(err)
	}
	for _, h := range []core.FederateHandle{2, 1} {
		if err := m.FederateJoined(ctx, "fed", h, itoa(int(h)), "model"); err != nil {
			t.Fatal(err)
		}
		m.SetConveyRegionDesignatorSetsSwitch("fed", h, true)
		m.SetConveyProducingFederateSwitch("fed", h, true)
		m.SetServiceReporting("fed", h, true)
		m.SetExceptionReporting("fed", h, true)
	}
	m.SetAutoProvideSwitch("fed", true)
	m.mu.Lock()
	st := m.fed["fed"]
	st.objects = momClassHandles{
		ok: true, federationCls: 10, federateCls: 11,
		attrFederationName: 1, attrFederatesInFederation: 2,
		attrFederateHandle: 3, attrFederateName: 4, attrFederateType: 5,
		federationObj: 100, federationObjName: "HLAfederation.fed",
	}
	for h, fs := range st.federates {
		fs.objectHandle = core.ObjectHandle(100 + h)
		fs.objectName = "HLAfederate." + itoa(int(h))
	}
	m.mu.Unlock()
	return m
}

func TestMOMCompleteStateRoundTrip(t *testing.T) {
	source := completeSnapshotManager(t)
	target, err := New(Options{Outbox: snapFakeOutbox{}})
	if err != nil {
		t.Fatal(err)
	}
	target.ImportState("fed", source.ExportState("fed"))
	if !reflect.DeepEqual(source.fed["fed"], target.fed["fed"]) {
		t.Fatalf("MOM runtime state was not restored: source=%+v target=%+v", source.fed["fed"], target.fed["fed"])
	}
}

func TestMOMCompleteStateNilClearsOnlyTargetFederation(t *testing.T) {
	m := completeSnapshotManager(t)
	if err := m.FederationCreated(context.Background(), "other", nil); err != nil {
		t.Fatal(err)
	}
	m.ImportState("fed", nil)
	if state := m.ExportState("fed"); state != nil {
		t.Fatalf("nil state did not clear federation: %+v", state)
	}
	if m.ExportState("other") == nil {
		t.Fatal("cleared an unrelated federation")
	}
}

func TestMOMCompleteStateDeepCopiesAndSorts(t *testing.T) {
	m := completeSnapshotManager(t)
	saved := m.ExportState("fed")
	if !reflect.DeepEqual(saved.Federation.FederateHandles, []core.FederateHandle{1, 2}) || saved.Federates[0].Handle != 1 {
		t.Fatal("export order is not deterministic")
	}
	saved.Federation.FederateHandles[0] = 99
	saved.Federation.FOMModuleNames[0] = "mutated.xml"
	saved.Federates[0].Name = "mutated"
	if got := m.ExportState("fed"); got.Federation.FederateHandles[0] != 1 || got.Federation.FOMModuleNames[0] != "base.xml" || got.Federates[0].Name != "1" {
		t.Fatal("export aliases live state")
	}
	saved = m.ExportState("fed")
	m.ImportState("fed", saved)
	saved.Federation.FederateHandles[0] = 99
	saved.Federation.FOMModuleNames[0] = "mutated.xml"
	saved.Federates[0].Name = "mutated"
	if got := m.ExportState("fed"); got.Federation.FederateHandles[0] != 1 || got.Federation.FOMModuleNames[0] != "base.xml" || got.Federates[0].Name != "1" {
		t.Fatal("import aliases caller state")
	}
}

type momRestoredOutbox struct{ events []core.OutboundEvent }

func (o *momRestoredOutbox) Send(_ context.Context, _ core.FederationName, _ core.FederateHandle, event core.OutboundEvent) error {
	o.events = append(o.events, event)
	return nil
}

type momRestoredRegistry struct {
	core.ObjectRegistry
	deleted core.ObjectHandle
	updated core.ObjectHandle
	attrs   map[core.AttributeHandle][]byte
}

func (r *momRestoredRegistry) Delete(_ context.Context, _ core.FederationName, _ core.FederateHandle, obj core.ObjectHandle, _ *core.LogicalTime, _ []byte) error {
	r.deleted = obj
	return nil
}

func (r *momRestoredRegistry) UpdateAttributes(_ context.Context, _ core.FederationName, _ core.FederateHandle, obj core.ObjectHandle, attrs map[core.AttributeHandle][]byte, _ *core.LogicalTime) error {
	r.updated, r.attrs = obj, attrs
	return nil
}

func TestMOMCompleteStateRetainsDependenciesAndInstanceBehavior(t *testing.T) {
	source := completeSnapshotManager(t)
	outbox := &momRestoredOutbox{}
	target, err := New(Options{Outbox: outbox})
	if err != nil {
		t.Fatal(err)
	}
	registry := &momRestoredRegistry{}
	target.EnableInstanceFanout(registry, struct{ core.DeclarationManagement }{}, struct{ core.FOMRepository }{})
	target.ImportState("fed", source.ExportState("fed"))
	if target.fanout.objects != registry || target.opts.Outbox != outbox {
		t.Fatal("restore replaced runtime dependencies")
	}
	ctx := context.Background()
	target.ObjectClassSubscribed(ctx, "fed", 99, 11, nil)
	if len(outbox.events) != 4 {
		t.Fatalf("late subscriber callbacks = %d, want 4", len(outbox.events))
	}
	for i := 0; i < 2; i++ {
		discover, ok := outbox.events[2*i].(*DiscoverEvent)
		if !ok || discover.Object != core.ObjectHandle(101+i) || discover.Class != 11 || discover.Name != "HLAfederate."+itoa(i+1) {
			t.Fatalf("restored discovery %d: %+v", i, outbox.events[2*i])
		}
		reflected, ok := outbox.events[2*i+1].(*ReflectEvent)
		if !ok || reflected.Object != discover.Object || !bytes.Equal(reflected.Attributes[3], encodeHLAhandle(core.FederateHandle(i+1))) {
			t.Fatalf("restored reflection %d: %+v", i, outbox.events[2*i+1])
		}
	}
	outbox.events = nil
	target.ObjectClassSubscribed(ctx, "fed", 99, 10, nil)
	if len(outbox.events) != 2 {
		t.Fatalf("federation callbacks = %d, want 2", len(outbox.events))
	}
	if discover, ok := outbox.events[0].(*DiscoverEvent); !ok || discover.Object != 100 || discover.Name != "HLAfederation.fed" || discover.Class != 10 {
		t.Fatalf("restored federation discovery: %+v", outbox.events[0])
	}
	if err := target.FederateResigned(ctx, "fed", 1); err != nil {
		t.Fatal(err)
	}
	if registry.deleted != 101 || registry.updated != 100 || !bytes.Equal(registry.attrs[2], encodeFederateHandleList([]core.FederateHandle{2})) {
		t.Fatalf("resign did not use restored object/class cache: %+v", registry)
	}
}
