package composition

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/pkg/fom/model"
)

func metadataFixture(depth int) *fomHandle {
	objects := []model.ObjectClass{{Name: "HLAobjectRoot"}}
	interactions := []model.InteractionClass{{Name: "HLAinteractionRoot"}}
	op, ip := "HLAobjectRoot", "HLAinteractionRoot"
	for i := 0; i < depth; i++ {
		name := fmt.Sprintf("Level%02d", i)
		op, ip = op+"."+name, ip+"."+name
		objects = append(objects, model.ObjectClass{Name: name, QualifiedName: op})
		interactions = append(interactions, model.InteractionClass{Name: name, QualifiedName: ip})
	}
	return NewModelHandle(model.NewFOM(objects, interactions, nil).WithAdvisorySwitches([]model.AdvisorySwitch{
		{Name: "attributeScopeAdvisory", Enabled: true},
	})).(*fomHandle)
}

// Independent linear oracle for the pre-index name and ancestry contract.
func linearMetadataIndex(names [][3]string, query, root string) int {
	if query == "" {
		return -1
	}
	found := -1
	for i, n := range names {
		match := query == n[0] || query == n[1]
		if strings.Contains(query, ".") {
			match = strings.TrimPrefix(query, root+".") == strings.TrimPrefix(n[1], root+".")
		}
		if match {
			if found >= 0 {
				return -1
			}
			found = i
		}
	}
	return found
}

func checkMetadataReference(t *testing.T, h *fomHandle) {
	t.Helper()
	for _, interaction := range []bool{false, true} {
		root := "HLAobjectRoot"
		var names [][3]string
		for _, c := range h.bindings.objects {
			names = append(names, [3]string{c.Name, c.Key(), c.ParentKey()})
		}
		if interaction {
			root, names = "HLAinteractionRoot", nil
			for _, c := range h.bindings.interactions {
				names = append(names, [3]string{c.Name, c.Key(), c.ParentKey()})
			}
		}
		queries := []string{"", ".", "Unknown", "Wrong.Level00", root, root + ".", root + "." + root}
		for i, n := range names {
			queries = append(queries, n[0], n[1], n[2], strings.TrimPrefix(n[1], root+"."), root+"."+n[1], "Wrong."+n[1])
			var want, got []uint64
			seen := map[int]bool{}
			for cur := i; cur >= 0 && !seen[cur]; cur = linearMetadataIndex(names, names[cur][2], root) {
				seen[cur] = true
				want = append(want, uint64(cur+1))
			}
			if interaction {
				for _, value := range h.InteractionClassAncestors(core.InteractionClassHandle(i + 1)) {
					got = append(got, uint64(value))
				}
			} else {
				for _, value := range h.ObjectClassAncestors(core.ObjectClassHandle(i + 1)) {
					got = append(got, uint64(value))
				}
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("%s ancestry %s = %v, want %v", root, n[1], got, want)
			}
		}
		for _, query := range queries {
			want := linearMetadataIndex(names, query, root)
			var got uint64
			var ok bool
			if interaction {
				var handle core.InteractionClassHandle
				handle, ok = h.LookupInteractionClass(query)
				got = uint64(handle)
			} else {
				var handle core.ObjectClassHandle
				handle, ok = h.LookupObjectClass(query)
				got = uint64(handle)
			}
			if got != uint64(want+1) || ok != (want >= 0) {
				t.Fatalf("%s lookup %q = %d,%v, want %d", root, query, got, ok, want+1)
			}
		}
	}
}

func TestFOMStaticMetadataMatchesLinearReference(t *testing.T) {
	checkMetadataReference(t, metadataFixture(16))
	// Programmatic models also permit missing/ambiguous parents and cycles.
	var objects []model.ObjectClass
	var interactions []model.InteractionClass
	for _, root := range []string{"", "HLAobjectRoot.", "HLAinteractionRoot."} {
		for _, branch := range []string{"Left", "Right"} {
			objects = append(objects, model.ObjectClass{Name: "Leaf", QualifiedName: root + branch + ".Leaf"})
			interactions = append(interactions, model.InteractionClass{Name: "Leaf", QualifiedName: root + branch + ".Leaf"})
		}
	}
	for _, pair := range [][2]string{{"A", "B"}, {"B", "A"}, {"Self", "Self"}, {"Orphan", "Missing"}, {"Child", "Leaf"}, {"", ""}} {
		objects = append(objects, model.ObjectClass{Name: pair[0], ParentName: pair[1]})
		interactions = append(interactions, model.InteractionClass{Name: pair[0], ParentName: pair[1]})
	}
	checkMetadataReference(t, NewModelHandle(model.NewFOM(objects, interactions, nil)).(*fomHandle))
}

func TestFOMStaticMetadataAdvisoryKindsAndFirstDeclaration(t *testing.T) {
	values := []model.AdvisorySwitch{
		{Name: "objectClassRelevanceAdvisory", Enabled: true},
		{Name: "attributeRelevanceAdvisory", Enabled: false},
		{Name: "attributeScopeAdvisory", Enabled: true},
		{Name: "interactionRelevanceAdvisory", Enabled: false},
		{Name: "unknown", Enabled: true},
		{Name: "attributeScopeAdvisory", Enabled: false},
	}
	h := NewModelHandle(model.NewFOM(nil, nil, nil).WithAdvisorySwitches(values)).(*fomHandle)
	for kind := 0; kind < 256; kind++ {
		enabled, present := h.AdvisorySwitchDefault(core.AdvisorySwitchKind(kind))
		if present != (kind >= 1 && kind <= 4) || enabled != (kind == 1 || kind == 3) {
			t.Fatalf("kind %d: %v,%v", kind, enabled, present)
		}
	}
}

func TestFOMStaticMetadataPublicationAndRestore(t *testing.T) {
	r, base, extra := liveFixture(t)
	ctx := context.Background()
	before, err := r.ExportState(ctx, "live")
	if err != nil {
		t.Fatal(err)
	}
	reference := r.FOMHandleReference("live")
	modules := []core.FOMModule{extra, advisoryModule(`<attributeScopeAdvisory isEnabled="true"/>`)}
	released, err := r.PrepareLiveExtension(ctx, "live", modules)
	if err != nil {
		t.Fatal(err)
	}
	checkMetadataReference(t, released.FOM().(*fomHandle))
	released.Release()
	if reference.CurrentFOMHandle() != base {
		t.Fatal("released preparation changed live metadata")
	}
	p, err := r.PrepareLiveExtension(ctx, "live", modules)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Release()
	candidate := p.FOM().(*fomHandle)
	checkMetadataReference(t, candidate)
	if enabled, present := candidate.AdvisorySwitchDefault(core.AdvisoryAttributeScope); !enabled || !present {
		t.Fatal("candidate advisory default missing")
	}
	if _, present := base.AdvisorySwitchDefault(core.AdvisoryAttributeScope); present || reference.CurrentFOMHandle() != base {
		t.Fatal("preparation mutated live metadata")
	}
	if err := p.Commit(); err != nil {
		t.Fatal(err)
	}
	after, err := r.ExportState(ctx, "live")
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []*FOMRepositoryState{before, after, before, after} {
		pending, err := r.PrepareImportState(ctx, "live", state)
		if err != nil {
			t.Fatal(err)
		}
		h := pending.FOM().(*fomHandle)
		checkMetadataReference(t, h)
		if err := pending.Commit(); err != nil {
			t.Fatal(err)
		}
		if reference.CurrentFOMHandle() != h || !reflect.DeepEqual(h.View(), state.View) {
			t.Fatal("restored metadata did not follow handle assignments/publication")
		}
		enabled, present := h.AdvisorySwitchDefault(core.AdvisoryAttributeScope)
		if enabled != (state == after) || present != (state == after) {
			t.Fatal("stale advisory default after restore")
		}
	}
	checkMetadataReference(t, base)
	checkMetadataReference(t, candidate)
}

func TestFOMStaticMetadataDefensiveConcurrentReads(t *testing.T) {
	h := metadataFixture(16)
	oc, _ := h.LookupObjectClass("Level15")
	ic, _ := h.LookupInteractionClass("Level15")
	ow, iw := h.ObjectClassAncestors(oc), h.InteractionClassAncestors(ic)
	var workers sync.WaitGroup
	for i := 0; i < 8; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for j := 0; j < 100; j++ {
				o, in := h.ObjectClassAncestors(oc), h.InteractionClassAncestors(ic)
				if !reflect.DeepEqual(o, ow) || !reflect.DeepEqual(in, iw) {
					t.Error("reader observed mutated metadata")
					return
				}
				o[0], in[0] = 999, 999
				if got, ok := h.LookupObjectClass("Level15"); !ok || got != oc {
					t.Error("lookup changed during concurrent reads")
				}
			}
		}()
	}
	workers.Wait()
	for _, invalid := range []uint64{0, 999, ^uint64(0)} {
		if h.ObjectClassAncestors(core.ObjectClassHandle(invalid)) != nil || h.InteractionClassAncestors(core.InteractionClassHandle(invalid)) != nil {
			t.Fatal("invalid class has ancestors")
		}
	}
	for _, invalid := range []*fomHandle{nil, NewModelHandle(nil).(*fomHandle)} {
		if _, ok := invalid.LookupObjectClass("Level15"); ok {
			t.Fatal("invalid FOM has class")
		}
		if _, present := invalid.AdvisorySwitchDefault(core.AdvisoryAttributeScope); present {
			t.Fatal("invalid FOM has advisory default")
		}
	}
}

var metadataObjectSink []core.ObjectClassHandle
var metadataInteractionSink []core.InteractionClassHandle
var metadataBoolSink bool

func TestFOMStaticMetadataAllocationBudget(t *testing.T) {
	h := metadataFixture(16)
	oc, _ := h.LookupObjectClass("Level15")
	ic, _ := h.LookupInteractionClass("Level15")
	for _, tc := range []struct {
		name string
		max  float64
		run  func()
	}{
		{"advisory", 0, func() { metadataBoolSink, _ = h.AdvisorySwitchDefault(core.AdvisoryAttributeScope) }},
		{"object ancestry", 1, func() { metadataObjectSink = h.ObjectClassAncestors(oc) }},
		{"interaction ancestry", 1, func() { metadataInteractionSink = h.InteractionClassAncestors(ic) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := testing.AllocsPerRun(100, tc.run); got > tc.max {
				t.Fatalf("allocations/query = %g, want <= %g", got, tc.max)
			}
		})
	}
}

func BenchmarkFOMStaticMetadata(b *testing.B) {
	h := metadataFixture(16)
	oc, _ := h.LookupObjectClass("Level15")
	ic, _ := h.LookupInteractionClass("Level15")
	for _, tc := range []struct {
		name string
		run  func()
	}{
		{"advisory", func() { metadataBoolSink, _ = h.AdvisorySwitchDefault(core.AdvisoryAttributeScope) }},
		{"object_ancestry", func() { metadataObjectSink = h.ObjectClassAncestors(oc) }},
		{"interaction_ancestry", func() { metadataInteractionSink = h.InteractionClassAncestors(ic) }},
		{"object_name", func() { _, metadataBoolSink = h.LookupObjectClass("Level15") }},
		{"interaction_name", func() { _, metadataBoolSink = h.LookupInteractionClass("Level15") }},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				tc.run()
			}
		})
	}
}
