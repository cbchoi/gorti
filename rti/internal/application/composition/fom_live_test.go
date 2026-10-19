package composition

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
)

func liveModules() (core.FOMModule, core.FOMModule) {
	return core.FOMModule{Path: "base.xml", XML: []byte(`<objectModel><objects><objectClass><name>HLAobjectRoot</name><objectClass><name>Parent</name><attribute><name>Inherited</name><dataType>HLAinteger32BE</dataType><transportation>HLAreliable</transportation><order>Receive</order><dimensions><dimension>Z</dimension></dimensions></attribute><objectClass><name>Child</name><attribute><name>Own</name><dataType>HLAinteger32BE</dataType></attribute></objectClass></objectClass></objectClass></objects><interactions><interactionClass><name>HLAinteractionRoot</name><interactionClass><name>ParentEvent</name><parameter><name>Inherited</name><dataType>HLAinteger32BE</dataType></parameter><interactionClass><name>ChildEvent</name><parameter><name>Own</name><dataType>HLAinteger32BE</dataType></parameter></interactionClass></interactionClass></interactionClass></interactions><dimensions><dimension><name>Z</name><upperBound>100</upperBound></dimension></dimensions></objectModel>`)},
		core.FOMModule{Path: "extra.xml", XML: []byte(`<objectModel>
<objects><objectClass><name>HLAobjectRoot</name>
<objectClass><name>A</name><sharing>Neither</sharing></objectClass>
<objectClass><name>Parent</name><objectClass><name>Child</name>
<objectClass><name>Extension</name><sharing>PublishSubscribe</sharing><attribute><name>Added</name><dataType>HLAinteger32BE</dataType></attribute>
<objectClass><name>Leaf</name><sharing>PublishSubscribe</sharing><attribute><name>Alpha</name><dataType>HLAinteger32BE</dataType></attribute></objectClass>
</objectClass></objectClass></objectClass></objectClass></objects>
<interactions><interactionClass><name>HLAinteractionRoot</name>
<interactionClass><name>A</name><sharing>Neither</sharing><transportation>HLAreliable</transportation><order>Receive</order></interactionClass>
<interactionClass><name>ParentEvent</name><interactionClass><name>ChildEvent</name>
<interactionClass><name>ExtensionEvent</name><sharing>PublishSubscribe</sharing><transportation>HLAreliable</transportation><order>Receive</order><parameter><name>Added</name><dataType>HLAinteger32BE</dataType></parameter>
<interactionClass><name>LeafEvent</name><sharing>PublishSubscribe</sharing><transportation>HLAreliable</transportation><order>Receive</order><parameter><name>Alpha</name><dataType>HLAinteger32BE</dataType></parameter></interactionClass>
</interactionClass></interactionClass></interactionClass></interactionClass></interactions>
<dimensions><dimension><name>A</name><upperBound>200</upperBound></dimension></dimensions></objectModel>`)}
}

func liveFixture(t *testing.T) (*Repository, *fomHandle, core.FOMModule) {
	t.Helper()
	r := NewFOMRepository()
	base, extra := liveModules()
	h, err := r.Load(context.Background(), []core.FOMModule{base})
	if err != nil {
		t.Fatal(err)
	}
	r.RememberFor("live", h)
	return r, h.(*fomHandle), extra
}

func assertStableBindings(t *testing.T, before core.FOMView, after *fomHandle) {
	t.Helper()
	for _, c := range before.ObjectClasses {
		if got, ok := after.LookupObjectClass(c.Name); !ok || got != c.Handle {
			t.Errorf("object %s moved: %d -> %d", c.Name, c.Handle, got)
		}
		if got, ok := after.ObjectClassName(c.Handle); !ok || got != c.Name {
			t.Errorf("object reverse %d: %s", c.Handle, got)
		}
		for _, a := range c.Attributes {
			if got, ok := after.LookupAttribute(c.Handle, a.Name); !ok || got != a.Handle {
				t.Errorf("attribute %s.%s moved: %d -> %d", c.Name, a.Name, a.Handle, got)
			}
			if got, ok := after.AttributeName(c.Handle, a.Handle); !ok || got != a.Name {
				t.Errorf("attribute reverse %s:%d: %s", c.Name, a.Handle, got)
			}
		}
	}
	for _, c := range before.InteractionClasses {
		if got, ok := after.LookupInteractionClass(c.Name); !ok || got != c.Handle {
			t.Errorf("interaction %s moved: %d -> %d", c.Name, c.Handle, got)
		}
		for _, p := range c.Parameters {
			if got, ok := after.LookupParameter(c.Handle, p.Name); !ok || got != p.Handle {
				t.Errorf("parameter %s.%s moved: %d -> %d", c.Name, p.Name, p.Handle, got)
			}
			if got, ok := after.ParameterName(c.Handle, p.Handle); !ok || got != p.Name {
				t.Errorf("parameter reverse %s:%d: %s", c.Name, p.Handle, got)
			}
		}
	}
	for _, d := range before.Dimensions {
		if got, ok := after.LookupDimension(d.Name); !ok || got != d.Handle {
			t.Errorf("dimension %s moved: %d -> %d", d.Name, d.Handle, got)
		}
		if got, ok := after.DimensionUpperBound(d.Handle); !ok || got != d.UpperBound {
			t.Errorf("dimension bound %s changed: %d", d.Name, got)
		}
	}
}

func TestLiveFOMExtensionPreservesEveryAssignedHandle(t *testing.T) {
	r, base, extra := liveFixture(t)
	before := base.View()
	p, err := r.PrepareLiveExtension(context.Background(), "live", []core.FOMModule{extra})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Release()
	after := p.FOM().(*fomHandle)
	assertStableBindings(t, before, after)
	if !reflect.DeepEqual(base.View(), before) {
		t.Fatal("preparation mutated old view")
	}
	if got, _ := r.Get(context.Background(), "live"); got != base {
		t.Fatal("uncommitted candidate published")
	}
	if err := p.Commit(); err != nil {
		t.Fatal(err)
	}
	if got, _ := r.Get(context.Background(), "live"); got != after {
		t.Fatal("commit did not publish candidate")
	}
	if _, ok := base.LookupObjectClass("HLAobjectRoot.A"); ok {
		t.Fatal("old immutable view acquired new class")
	}
}

func TestLiveFOMExtensionLeaseAndStaleCAS(t *testing.T) {
	r, base, extra := liveFixture(t)
	ctx := context.Background()
	first, err := r.PrepareLiveExtension(ctx, "live", []core.FOMModule{extra})
	if err != nil {
		t.Fatal(err)
	}
	second, err := r.PrepareLiveExtension(ctx, "live", []core.FOMModule{extra})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Release()
	defer second.Release()
	if err := first.PrepareCommit(); err != nil {
		t.Fatal(err)
	}
	if r.slotFor("live").write.TryLock() {
		r.slotFor("live").write.Unlock()
		t.Fatal("PrepareCommit did not lease writer slot")
	}
	read := make(chan core.FOMHandle, 1)
	go func() { h, _ := r.Get(ctx, "live"); read <- h }()
	select {
	case h := <-read:
		if h != base {
			t.Fatal("lease published before journal")
		}
	case <-time.After(time.Second):
		t.Fatal("lease blocked old-view readers")
	}
	if err := first.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := first.Commit(); err != nil {
		t.Fatal("commit not idempotent:", err)
	}
	if err := second.PrepareCommit(); !errors.Is(err, ErrFOMExtensionStale) {
		t.Fatalf("stale preparation: %v", err)
	}
	third, err := r.PrepareLiveExtension(ctx, "live", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := third.PrepareCommit(); err != nil {
		t.Fatal(err)
	}
	third.Release()
	third.Release()
	if err := third.Commit(); !errors.Is(err, ErrFOMExtensionReleased) {
		t.Fatalf("released transaction committed: %v", err)
	}
}

func TestLiveFOMExtensionRejectsConflictWithoutMutation(t *testing.T) {
	r, base, _ := liveFixture(t)
	before := base.View()
	for _, tc := range []struct{ name, xml string }{
		{"dimension", `<objectModel><dimensions><dimension><name>Z</name><upperBound>999</upperBound></dimension></dimensions></objectModel>`},
		{"attribute_growth", `<objectModel><objects><objectClass><name>HLAobjectRoot</name><objectClass><name>Parent</name><objectClass><name>Child</name><attribute><name>Own</name><dataType>HLAinteger32BE</dataType></attribute><attribute><name>Added</name><dataType>HLAinteger32BE</dataType></attribute></objectClass></objectClass></objectClass></objects></objectModel>`},
		{"parameter_growth", `<objectModel><interactions><interactionClass><name>HLAinteractionRoot</name><interactionClass><name>ParentEvent</name><interactionClass><name>ChildEvent</name><parameter><name>Own</name><dataType>HLAinteger32BE</dataType></parameter><parameter><name>Added</name><dataType>HLAinteger32BE</dataType></parameter></interactionClass></interactionClass></interactionClass></interactions></objectModel>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := core.FOMModule{Path: tc.name + ".xml", XML: []byte(tc.xml)}
			pending, err := r.PrepareLiveExtension(context.Background(), "live", []core.FOMModule{bad})
			if pending != nil {
				pending.Release()
			}
			if err == nil || pending != nil {
				t.Fatalf("invalid extension accepted: %v", err)
			}
			got, err := r.FOMView(context.Background(), "live")
			if err != nil || !reflect.DeepEqual(got, before) {
				t.Fatalf("failed extension changed live view: %v", err)
			}
			if got, _ := r.Get(context.Background(), "live"); got != base {
				t.Fatal("failed extension replaced the live handle")
			}
		})
	}
}

func TestLiveFOMExtensionResolversUseStableEffectiveMembers(t *testing.T) {
	r, base, extra := liveFixture(t)
	ctx := context.Background()
	child, _ := base.LookupObjectClass("Child")
	inherited, _ := base.LookupAttribute(child, "Inherited")
	dimension, _ := base.LookupDimension("Z")
	ancestors := base.ObjectClassAncestors(child)
	event, _ := base.LookupInteractionClass("ChildEvent")
	eventAncestors := base.InteractionClassAncestors(event)
	p, err := r.PrepareLiveExtension(ctx, "live", []core.FOMModule{extra})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Release()
	h := p.FOM().(*fomHandle)
	if got, ok := h.TransportationForAttribute(child, inherited); !ok || got != core.TransportTypeReliable {
		t.Fatalf("transport: %v %v", got, ok)
	}
	if got, ok := h.OrderForAttribute(child, inherited); !ok || got != object.OrderReceive {
		t.Fatalf("order: %v %v", got, ok)
	}
	if got, ok := h.AttributeDimensions(child, inherited); !ok || !reflect.DeepEqual(got, []core.DDMDimensionHandle{core.DDMDimensionHandle(dimension)}) {
		t.Fatalf("dimensions: %v %v", got, ok)
	}
	if !reflect.DeepEqual(h.ObjectClassAncestors(child), ancestors) || !reflect.DeepEqual(h.InteractionClassAncestors(event), eventAncestors) {
		t.Fatal("ancestry used model positions")
	}
	if !reflect.DeepEqual(h.AttributeHandles(child), base.AttributeHandles(child)) || !reflect.DeepEqual(h.effectiveParameters(event), base.effectiveParameters(event)) {
		t.Fatal("extension changed an existing class member set")
	}
	if _, ok := h.LookupAttribute(child, "Added"); ok {
		t.Fatal("new descendant attribute leaked to existing ancestor")
	}
	if _, ok := h.LookupParameter(event, "Added"); ok {
		t.Fatal("new descendant parameter leaked to existing ancestor")
	}
	leaf, ok := h.LookupObjectClass("Parent.Child.Extension.Leaf")
	if !ok {
		t.Fatal("new object descendant missing")
	}
	leafEvent, ok := h.LookupInteractionClass("ParentEvent.ChildEvent.ExtensionEvent.LeafEvent")
	if !ok {
		t.Fatal("new interaction descendant missing")
	}
	if len(h.AttributeHandles(leaf)) != len(base.AttributeHandles(child))+2 || len(h.effectiveParameters(leafEvent)) != len(base.effectiveParameters(event))+2 {
		t.Fatal("new local and inherited attributes missing")
	}
	for _, name := range []string{"Inherited", "Own", "Added", "Alpha"} {
		attribute, ok := h.LookupAttribute(leaf, name)
		if !ok {
			t.Fatal("descendant attribute missing:", name)
		}
		if got, ok := h.AttributeName(leaf, attribute); !ok || got != name {
			t.Fatalf("attribute reverse lookup: %s %q", name, got)
		}
		parameter, ok := h.LookupParameter(leafEvent, name)
		if !ok {
			t.Fatal("descendant parameter missing:", name)
		}
		if got, ok := h.ParameterName(leafEvent, parameter); !ok || got != name {
			t.Fatalf("parameter reverse lookup: %s %q", name, got)
		}
	}
	if got := h.Dimensions(); len(got) != len(base.Dimensions())+1 || got[dimension-1].Name != "Z" || got[len(got)-1].Name != "A" {
		t.Fatalf("dimension enumerator renumbered cache seeds: %v", got)
	}
	v := p.View()
	v.Modules[0].XML[0] = '!'
	v.ObjectClasses[0].Name = corruptFixtureName
	v.ObjectClasses[0].Attributes[0].Name = corruptFixtureName
	v.Dimensions[0].Name = corruptFixtureName
	if reflect.DeepEqual(v, p.View()) {
		t.Fatal("view leaked owned slices")
	}
	assertStableBindings(t, base.View(), h)
}

func TestLiveFOMSnapshotRoundTripAndFurtherExtension(t *testing.T) {
	r, base, extra := liveFixture(t)
	ctx := context.Background()
	p, err := r.PrepareLiveExtension(ctx, "live", []core.FOMModule{extra})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Commit(); err != nil {
		t.Fatal(err)
	}
	snapshot, err := r.ExportState(ctx, "live")
	if err != nil {
		t.Fatal(err)
	}
	restored := NewFOMRepository()
	imported, err := restored.PrepareImportState(ctx, "restored", snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restored.Get(ctx, "restored"); !errors.Is(err, core.ErrFederationNotFound) {
		t.Fatal("import published during validation")
	}
	if err := imported.PrepareCommit(); err != nil {
		t.Fatal(err)
	}
	if err := imported.Commit(); err != nil {
		t.Fatal(err)
	}
	view, err := restored.FOMView(ctx, "restored")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(view, snapshot.View) {
		t.Fatal("snapshot source/revision/assignments did not round trip")
	}
	snapshot.View.Modules[0].XML[0] = '!'
	snapshot.View.ObjectClasses[0].Name = corruptFixtureName
	if got, _ := restored.FOMView(ctx, "restored"); !reflect.DeepEqual(got, view) {
		t.Fatal("import aliased caller snapshot")
	}
	third := core.FOMModule{Path: "third.xml", XML: []byte(`<objectModel><objects><objectClass><name>HLAobjectRoot</name><objectClass><name>AA</name></objectClass></objectClass></objects></objectModel>`)}
	next, err := restored.PrepareLiveExtension(ctx, "restored", []core.FOMModule{third})
	if err != nil {
		t.Fatal(err)
	}
	defer next.Release()
	assertStableBindings(t, view, next.FOM().(*fomHandle))
	assertStableBindings(t, base.View(), next.FOM().(*fomHandle))
	if next.View().Revision != view.Revision+1 {
		t.Fatal("revision not advanced")
	}
}

func TestLiveFOMSnapshotRejectsInvalidAssignmentsWithoutPublication(t *testing.T) {
	r, base, _ := liveFixture(t)
	ctx := context.Background()
	cases := map[string]func(*FOMRepositoryState){
		"version":          func(s *FOMRepositoryState) { s.Version++ },
		"revision":         func(s *FOMRepositoryState) { s.View.Revision = 0 },
		"digest":           func(s *FOMRepositoryState) { s.View.Digest = "bad" },
		"class handle":     func(s *FOMRepositoryState) { s.View.ObjectClasses[0].Handle = 0 },
		"class name":       func(s *FOMRepositoryState) { s.View.ObjectClasses[0].Name = "missing" },
		"missing class":    func(s *FOMRepositoryState) { s.View.ObjectClasses = s.View.ObjectClasses[1:] },
		"attribute":        func(s *FOMRepositoryState) { s.View.ObjectClasses[0].Attributes[0].Name = "missing" },
		"member handle":    func(s *FOMRepositoryState) { s.View.ObjectClasses[0].Attributes[0].Handle = 99 },
		"bound":            func(s *FOMRepositoryState) { s.View.Dimensions[0].UpperBound++ },
		"dimension handle": func(s *FOMRepositoryState) { s.View.Dimensions[0].Handle = 99 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			snapshot, err := r.ExportState(ctx, "live")
			if err != nil {
				t.Fatal(err)
			}
			mutate(snapshot)
			if _, err := r.PrepareImportState(ctx, "live", snapshot); err == nil {
				t.Fatal("invalid snapshot accepted")
			}
			if got, _ := r.Get(ctx, "live"); got != base {
				t.Fatal("invalid import mutated live repository")
			}
		})
	}
}

func TestLiveFOMSlotGenerationPreventsABA(t *testing.T) {
	r, base, extra := liveFixture(t)
	p, err := r.PrepareLiveExtension(context.Background(), "live", []core.FOMModule{extra})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Release()
	r.ForgetFor("live")
	r.RememberFor("live", base)
	if err := p.PrepareCommit(); !errors.Is(err, ErrFOMExtensionStale) {
		t.Fatalf("ABA base accepted: %v", err)
	}
}
