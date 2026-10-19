package savebundle

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/application/composition"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ddm"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
	timepkg "github.com/cbchoi/gorti/rti/internal/hla/time"
	"google.golang.org/protobuf/proto"
)

// Active 2010 source: S1516.1 9.1.3 (PDF pp223-224), 9.5.3(j)
// (p232), 9.10(c-d)/9.10.3(g) (pp242-243). Graph integrity and
// transactional snapshot preparation are engineering compatibility checks.
const ddmW4Fed core.FederationName = "ddm-w4-restore"
const ddmW4Sub core.FederateHandle = 1<<32 | 1
const ddmW4Pub core.FederateHandle = 1<<32 | 2

type ddmW4RestoreFixture struct {
	repo        *composition.Repository
	model       core.FOMHandle
	ddm         DDM
	decl        Declaration
	cls         core.ObjectClassHandle
	attr        core.AttributeHandle
	interaction core.InteractionClassHandle
	x, y, z     uint64
	regions     []ddm.RegionHandle
	manifest    core.CheckpointManifest
	wire        *rtiv1.DDMManagerState
}

func ddmW4RestoreManagers(t *testing.T, repo core.FOMRepository) (DDM, Declaration) {
	t.Helper()
	d := declaration.New()
	m, err := ddm.New(ddm.Options{FOMs: repo, Outbox: objectStateOutbox{},
		OnObjectSubscriptionInterest:      d.SetRegionalObjectSubscriptionInterest,
		OnInteractionSubscriptionInterest: d.SetRegionalInteractionSubscriptionInterest,
	})
	if err != nil {
		t.Fatal(err)
	}
	return DDM{Manager: m}, Declaration{Manager: d}
}

func ddmW4Fixture(t *testing.T, subset bool) *ddmW4RestoreFixture {
	t.Helper()
	ctx := context.Background()
	repo := composition.NewFOMRepository()
	model, err := repo.Load(ctx, []core.FOMModule{{Path: "ddm-w4.xml", XML: []byte(`<objectModel>
<objects><objectClass><name>HLAobjectRoot</name><objectClass><name>Vehicle</name>
<attribute><name>Position</name><dimensions><dimension>X</dimension><dimension>Y</dimension></dimensions></attribute>
</objectClass></objectClass></objects>
<interactions><interactionClass><name>HLAinteractionRoot</name><interactionClass><name>Signal</name>
<dimensions><dimension>X</dimension><dimension>Y</dimension></dimensions>
</interactionClass></interactionClass></interactions>
<dimensions><dimension><name>X</name><upperBound>100</upperBound></dimension>
<dimension><name>Y</name><upperBound>100</upperBound></dimension>
<dimension><name>Z</name><upperBound>100</upperBound></dimension></dimensions></objectModel>`)}})
	if err != nil {
		t.Fatal(err)
	}
	repo.RememberFor(ddmW4Fed, model)
	f := &ddmW4RestoreFixture{repo: repo, model: model}
	f.cls, _ = model.LookupObjectClass("HLAobjectRoot.Vehicle")
	f.attr, _ = model.LookupAttribute(f.cls, "Position")
	f.interaction, _ = model.LookupInteractionClass("HLAinteractionRoot.Signal")
	names := model.(core.FOMHandleNameLookup)
	x, _ := names.LookupDimension("X")
	y, _ := names.LookupDimension("Y")
	z, _ := names.LookupDimension("Z")
	f.x, f.y, f.z = uint64(x), uint64(y), uint64(z)
	f.ddm, f.decl = ddmW4RestoreManagers(t, repo)
	for i := 0; i < 4; i++ {
		owner := ddmW4Sub
		if i == 3 {
			owner = ddmW4Pub
		}
		dims := []ddm.DimensionHandle{ddm.DimensionHandle(x), ddm.DimensionHandle(y)}
		if subset {
			dims = dims[:1]
		}
		r, err := f.ddm.Manager.CreateRegion(ctx, ddmW4Fed, owner, 1, dims)
		if err != nil {
			t.Fatal(err)
		}
		for _, dim := range dims {
			b := core.DDMRange{Lower: 0, Upper: 100}
			if dim == ddm.DimensionHandle(x) {
				b = core.DDMRange{Lower: 10, Upper: 20}
				if i == 1 {
					b = core.DDMRange{Lower: 70, Upper: 80}
				}
			}
			if err := f.ddm.Manager.SetRangeBounds(ddmW4Fed, owner, r, dim, b); err != nil {
				t.Fatal(err)
			}
		}
		if err := f.ddm.Manager.CommitRegionModifications(ctx, ddmW4Fed, owner, []ddm.RegionHandle{r}); err != nil {
			t.Fatal(err)
		}
		f.regions = append(f.regions, r)
		if i == 3 {
			continue
		}
		if err := f.ddm.Manager.SubscribeObjectClassAttributesWithRegionsAndOptions(ctx, ddmW4Fed, ddmW4Sub, f.cls, []core.AttributeHandle{f.attr}, []ddm.RegionHandle{r}, core.SubscriptionOptions{Active: i != 1}); err != nil {
			t.Fatal(err)
		}
		if err := f.ddm.Manager.SubscribeInteractionClassWithRegionsAndOptions(ctx, ddmW4Fed, ddmW4Sub, f.interaction, []ddm.RegionHandle{r}, i != 1); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.ddm.Manager.AssociateRegionsForUpdates(ctx, ddmW4Fed, ddmW4Pub, 42, map[core.AttributeHandle][]ddm.RegionHandle{f.attr: {f.regions[3]}}); err != nil {
		t.Fatal(err)
	}
	if err := f.decl.Manager.SubscribeInteractionClass(ctx, ddmW4Fed, ddmW4Sub, f.interaction); err != nil {
		t.Fatal(err)
	}
	gen := uint64(1)
	f.manifest = core.CheckpointManifest{
		Federation: ddmW4Fed, FederationGeneration: &gen,
		Federates: []core.FederateHandle{ddmW4Sub, ddmW4Pub}, FederateNames: []string{"subscriber", "publisher"},
		ManagerSnapshots: map[string][]byte{
			"object":    graphJSON(t, objectSnapshotV1{Version: 1, State: &object.RegistryState{LastObjectHandle: 42, Objects: []object.InstanceState{{Handle: 42, Class: f.cls, Name: "vehicle", Registrant: ddmW4Pub}}}}),
			"ownership": graphProto(t, &rtiv1.OwnershipManagerState{Owners: []*rtiv1.OwnershipRecordEntry{{ObjectHandle: 42, AttributeHandle: uint64(f.attr), OwnerFederate: uint64(ddmW4Pub)}}}),
			"time":      graphJSON(t, timeSnapshot{Version: 1}), "sync": nil, "mom": nil,
			"retractions": graphJSON(t, retractionSnapshot{Version: 1, History: &timepkg.RetractionHistory{Complete: true}}),
		},
	}
	f.manifest.ManagerSnapshots["ddm"], err = f.ddm.Marshal(ddmW4Fed)
	if err != nil {
		t.Fatal(err)
	}
	f.manifest.ManagerSnapshots["declaration"], err = f.decl.Marshal(ddmW4Fed)
	if err != nil {
		t.Fatal(err)
	}
	f.wire = &rtiv1.DDMManagerState{}
	if err := proto.Unmarshal(f.manifest.ManagerSnapshots["ddm"], f.wire); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestDDMW4RestoreDisjointInteractionRowsAndSubsets(t *testing.T) {
	for _, subset := range []bool{false, true} {
		for _, reverse := range []bool{false, true} {
			name := map[bool]string{false: "full", true: "subset"}[subset] + map[bool]string{false: "/forward", true: "/reverse"}[reverse]
			t.Run(name, func(t *testing.T) {
				f := ddmW4Fixture(t, subset)
				wire := proto.Clone(f.wire).(*rtiv1.DDMManagerState)
				if reverse {
					slices.Reverse(wire.InteractionSubscriptions)
					slices.Reverse(wire.ObjectSubscriptions)
				}
				f.manifest.ManagerSnapshots["ddm"] = graphProto(t, wire)
				dst, decl := ddmW4RestoreManagers(t, f.repo)
				graph := RestoreGraph{Snapshots: map[string]core.CheckpointSnapshotter{"ddm": dst, "declaration": decl}, FOMs: f.repo}
				if err := graph.Validate(ddmW4Fed, f.manifest, f.model); err != nil {
					t.Fatalf("valid disjoint option groups/subsets rejected: %v", err)
				}
				ddmBefore, declBefore := dst.Manager.ExportState(ddmW4Fed), decl.Manager.ExportState(ddmW4Fed)
				commitDDM, err := dst.PrepareRestore(ddmW4Fed, f.manifest.ManagerSnapshots["ddm"])
				if err != nil {
					t.Fatal(err)
				}
				commitDecl, err := decl.PrepareRestore(ddmW4Fed, f.manifest.ManagerSnapshots["declaration"])
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(ddmBefore, dst.Manager.ExportState(ddmW4Fed)) || !reflect.DeepEqual(declBefore, decl.Manager.ExportState(ddmW4Fed)) {
					t.Fatal("preparation published live state")
				}
				commitDDM()
				commitDecl()
				if !reflect.DeepEqual(f.ddm.Manager.ExportState(ddmW4Fed), dst.Manager.ExportState(ddmW4Fed)) {
					t.Fatal("restored DDM groups/options differ from saved state")
				}
				if !reflect.DeepEqual(f.decl.Manager.ExportState(ddmW4Fed), decl.Manager.ExportState(ddmW4Fed)) {
					t.Fatal("restored ordinary/regional declarations differ")
				}
				assertInterest := func(active bool) {
					t.Helper()
					rows := decl.Manager.ExportState(ddmW4Fed).RegionalInteractionInterests
					if active && (len(rows) != 1 || rows[0].Class != f.interaction || !slices.Equal(rows[0].Federates, []core.FederateHandle{ddmW4Sub})) || !active && len(rows) != 0 {
						t.Fatalf("regional active union = %+v, want active=%v", rows, active)
					}
				}
				ctx := context.Background()
				assertInterest(true)
				if got, err := dst.Manager.InteractionSubscribersWithRegions(ctx, ddmW4Fed, ddmW4Pub, f.interaction, f.regions[3:]); err != nil || !slices.Equal(got, []core.FederateHandle{ddmW4Sub}) {
					t.Fatalf("restored interaction routing = %v, %v", got, err)
				}
				if got := dst.Manager.SubscribersForUpdate(ddmW4Fed, f.cls, f.attr, dst.Manager.PublisherRegionsFor(ddmW4Fed, 42, f.attr)); !slices.Equal(got, []core.FederateHandle{ddmW4Sub}) {
					t.Fatalf("restored object routing = %v", got)
				}
				for i, r := range []ddm.RegionHandle{f.regions[0], f.regions[2]} {
					if err := dst.Manager.UnsubscribeInteractionClassWithRegions(ctx, ddmW4Fed, ddmW4Sub, f.interaction, []ddm.RegionHandle{r}); err != nil {
						t.Fatal(err)
					}
					assertInterest(i == 0)
				}
				if err := dst.Manager.SetRangeBounds(ddmW4Fed, ddmW4Pub, f.regions[3], ddm.DimensionHandle(f.x), core.DDMRange{Lower: 70, Upper: 80}); err != nil {
					t.Fatal(err)
				}
				if err := dst.Manager.CommitRegionModifications(ctx, ddmW4Fed, ddmW4Pub, f.regions[3:]); err != nil {
					t.Fatal(err)
				}
				if got, err := dst.Manager.InteractionSubscribersWithRegions(ctx, ddmW4Fed, ddmW4Pub, f.interaction, f.regions[3:]); err != nil || !slices.Equal(got, []core.FederateHandle{ddmW4Sub}) {
					t.Fatalf("restored passive routing = %v, %v", got, err)
				}
				if got := decl.Manager.InteractionSubscribersFor(ctx, ddmW4Fed, f.interaction); !slices.Equal(got, []core.FederateHandle{ddmW4Sub}) {
					t.Fatalf("regional removal affected ordinary subscription: %v", got)
				}
				f.manifest.ManagerSnapshots["ddm"], err = dst.Marshal(ddmW4Fed)
				if err != nil {
					t.Fatal(err)
				}
				f.manifest.ManagerSnapshots["declaration"], err = decl.Marshal(ddmW4Fed)
				if err != nil {
					t.Fatal(err)
				}
				if err := graph.Validate(ddmW4Fed, f.manifest, f.model); err != nil {
					t.Fatalf("resaved passive-only state rejected: %v", err)
				}
			})
		}
	}
}

func TestDDMW4RestoreSubsetContexts(t *testing.T) {
	for _, scope := range []string{"object-subscription", "interaction-subscription", "object-publication"} {
		t.Run(scope, func(t *testing.T) {
			f := ddmW4Fixture(t, true)
			decl := declarationSnapshot{Version: 3}
			wire := proto.Clone(f.wire).(*rtiv1.DDMManagerState)
			wire.ObjectSubscriptions, wire.InteractionSubscriptions, wire.ObjectPublications = nil, nil, nil
			switch scope {
			case "object-subscription":
				wire.ObjectSubscriptions = f.wire.ObjectSubscriptions[:1]
				decl.RegionalObjectInterests = []declarationObjectRecord{{Class: f.cls, Attribute: f.attr, Federates: []core.FederateHandle{ddmW4Sub}}}
			case "interaction-subscription":
				wire.InteractionSubscriptions = f.wire.InteractionSubscriptions[:1]
				decl.RegionalInteractionInterests = []declarationInteractionRecord{{Class: f.interaction, Federates: []core.FederateHandle{ddmW4Sub}}}
			case "object-publication":
				wire.ObjectPublications = f.wire.ObjectPublications
			}
			f.manifest.ManagerSnapshots["ddm"] = graphProto(t, wire)
			f.manifest.ManagerSnapshots["declaration"] = graphJSON(t, decl)
			if err := (RestoreGraph{}).Validate(ddmW4Fed, f.manifest, f.model); err != nil {
				t.Fatalf("proper subset rejected: %v", err)
			}
		})
	}
}

func TestDDMW4RestoreRejectsInvalidGraphWithoutMutation(t *testing.T) {
	f := ddmW4Fixture(t, false)
	graph := RestoreGraph{Snapshots: map[string]core.CheckpointSnapshotter{"ddm": f.ddm, "declaration": f.decl}, FOMs: f.repo}
	for _, tc := range []struct {
		name, want string
		mutate     func(*rtiv1.DDMManagerState)
	}{
		{"repeated-same-options", "duplicate DDM interaction subscription region", func(w *rtiv1.DDMManagerState) {
			row := proto.Clone(w.InteractionSubscriptions[0]).(*rtiv1.DDMInteractionSubscriptionEntry)
			row.RegionHandles = row.RegionHandles[:1]
			w.InteractionSubscriptions = append(w.InteractionSubscriptions, row)
		}},
		{"repeated-conflicting-options", "duplicate DDM interaction subscription region", func(w *rtiv1.DDMManagerState) {
			row := proto.Clone(w.InteractionSubscriptions[0]).(*rtiv1.DDMInteractionSubscriptionEntry)
			row.RegionHandles = row.RegionHandles[:1]
			row.Passive = !row.Passive
			w.InteractionSubscriptions = append(w.InteractionSubscriptions, row)
		}},
		{"repeated-within-row", "duplicate DDM interaction subscription region", func(w *rtiv1.DDMManagerState) {
			w.InteractionSubscriptions[0].RegionHandles = append(w.InteractionSubscriptions[0].RegionHandles, w.InteractionSubscriptions[0].RegionHandles[0])
		}},
		{"missing-region", "region association owner/reference mismatch", func(w *rtiv1.DDMManagerState) { w.InteractionSubscriptions[0].RegionHandles = []uint64{999} }},
		{"foreign-region", "region association owner/reference mismatch", func(w *rtiv1.DDMManagerState) {
			w.InteractionSubscriptions[0].RegionHandles = []uint64{uint64(f.regions[3])}
		}},
		{"empty-row", "invalid DDM interaction subscription", func(w *rtiv1.DDMManagerState) { w.InteractionSubscriptions[0].RegionHandles = nil }},
		{"unknown-class", "invalid DDM interaction subscription", func(w *rtiv1.DDMManagerState) { w.InteractionSubscriptions[0].InteractionClassHandle = 999 }},
		{"unknown-dimension", "region references unknown dimension", func(w *rtiv1.DDMManagerState) { w.Regions[0].DimensionHandles[0] = 999 }},
		{"outside-context", "region dimension context mismatch", func(w *rtiv1.DDMManagerState) {
			w.Regions[0].DimensionHandles[0] = f.z
			w.Regions[0].Committed[0].DimensionHandle = f.z
		}},
		{"duplicate-dimension", "duplicate region dimension", func(w *rtiv1.DDMManagerState) {
			w.Regions[0].DimensionHandles = append(w.Regions[0].DimensionHandles, w.Regions[0].DimensionHandles[0])
		}},
		{"missing-bounds", "committed region ranges do not cover dimensions", func(w *rtiv1.DDMManagerState) { w.Regions[0].Committed = nil }},
		{"invalid-bounds", "invalid region range", func(w *rtiv1.DDMManagerState) { w.Regions[0].Committed[0].Upper = 101 }},
		{"lost-active-union", "DDM and declaration regional interest disagree", func(w *rtiv1.DDMManagerState) {
			for _, row := range w.InteractionSubscriptions {
				row.Passive = true
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate := proto.Clone(f.wire).(*rtiv1.DDMManagerState)
			tc.mutate(candidate)
			f.manifest.ManagerSnapshots["ddm"] = graphProto(t, candidate)
			input := slices.Clone(f.manifest.ManagerSnapshots["ddm"])
			ddmBefore, declBefore := f.ddm.Manager.ExportState(ddmW4Fed), f.decl.Manager.ExportState(ddmW4Fed)
			if err := graph.Validate(ddmW4Fed, f.manifest, f.model); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
			if !slices.Equal(input, f.manifest.ManagerSnapshots["ddm"]) || !reflect.DeepEqual(ddmBefore, f.ddm.Manager.ExportState(ddmW4Fed)) || !reflect.DeepEqual(declBefore, f.decl.Manager.ExportState(ddmW4Fed)) {
				t.Fatal("invalid restore mutated input or live state")
			}
		})
	}
}
