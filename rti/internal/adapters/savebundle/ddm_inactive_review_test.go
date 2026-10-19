package savebundle

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"slices"
	"sort"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/application/composition"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ddm"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

type inactiveDDMReviewFixture struct {
	v                 *graphValidation
	wire              *rtiv1.DDMManagerState
	manager           *ddm.Manager
	base, child       core.ObjectClassHandle
	position, private core.AttributeHandle
	x, y              core.DimensionHandle
}

func newInactiveDDMReviewFixture(t *testing.T) inactiveDDMReviewFixture {
	t.Helper()
	repo := composition.NewFOMRepository()
	model, err := repo.Load(context.Background(), []core.FOMModule{{Path: "inactive-review.xml", XML: []byte(`<objectModel><objects><objectClass><name>HLAobjectRoot</name><objectClass><name>Base</name><attribute><name>Position</name><dimensions><dimension>X</dimension></dimensions></attribute><objectClass><name>Child</name><attribute><name>Private</name><dimensions><dimension>Y</dimension></dimensions></attribute><objectClass><name>Leaf</name></objectClass></objectClass><objectClass><name>Sibling</name></objectClass></objectClass></objectClass></objects><dimensions><dimension><name>X</name><upperBound>100</upperBound></dimension><dimension><name>Y</name><upperBound>100</upperBound></dimension></dimensions></objectModel>`)}})
	if err != nil {
		t.Fatal(err)
	}
	repo.RememberFor("fed", model)
	names := model.(core.FOMHandleNameLookup)
	base, _ := model.LookupObjectClass("Base")
	child, _ := model.LookupObjectClass("Child")
	known, _ := model.LookupAttribute(base, "Position")
	position, _ := model.LookupAttribute(child, "Position")
	private, _ := model.LookupAttribute(child, "Private")
	x, _ := names.LookupDimension("X")
	y, _ := names.LookupDimension("Y")
	if known == position || known != private {
		t.Fatalf("fixture needs class-relative collision: base=%d actual=%d private=%d", known, position, private)
	}
	for _, name := range []string{"Sibling", "Leaf"} {
		cls, found := model.LookupObjectClass(name)
		if _, visible := model.LookupAttribute(cls, "Position"); !found || !visible {
			t.Fatalf("%s fixture must expose the requested attribute name", name)
		}
	}
	v := &graphValidation{
		model: model, names: names,
		members: map[core.FederateHandle]string{1: "owner", 2: "ancestor", 3: "child"},
		objects: map[core.ObjectHandle]object.InstanceState{100: {Handle: 100, Class: child, Registrant: 1}},
		// Production keys are object first, recipient second, never the reverse.
		knownClasses: map[[2]uint64]core.ObjectClassHandle{{100, 1}: child, {100, 2}: base, {100, 3}: child},
		owners:       map[[2]uint64]core.FederateHandle{{100, uint64(position)}: 1, {100, uint64(private)}: 1},
		regions:      map[uint64]*rtiv1.DDMRegionEntry{},
	}
	region := func(handle, owner uint64, dim core.DimensionHandle) *rtiv1.DDMRegionEntry {
		return &rtiv1.DDMRegionEntry{RegionHandle: handle, OwnerFederate: owner, RoutingSpaceHandle: 1, DimensionHandles: []uint64{uint64(dim)},
			Committed: []*rtiv1.DDMRangeEntry{{DimensionHandle: uint64(dim), Upper: 100}},
			Pending:   []*rtiv1.DDMRangeEntry{{DimensionHandle: uint64(dim), Lower: 10, Upper: 90}},
		}
	}
	wire := &rtiv1.DDMManagerState{
		NextRegionHandle:   5,
		Regions:            []*rtiv1.DDMRegionEntry{region(2, 2, x), region(3, 3, x), region(4, 3, y), region(5, 1, x)},
		ObjectPublications: []*rtiv1.DDMObjectPublicationEntry{{ObjectHandle: 100, AttributeHandle: uint64(position), RegionHandles: []uint64{5}}},
		InactiveAssociations: []*rtiv1.DDMInactiveAssociationEntry{
			{ObjectHandle: 100, AttributeHandle: uint64(position), AssociatingFederate: 2, RegionHandles: []uint64{2}},
			{ObjectHandle: 100, AttributeHandle: uint64(position), AssociatingFederate: 3, RegionHandles: []uint64{3}},
			{ObjectHandle: 100, AttributeHandle: uint64(private), AssociatingFederate: 3, RegionHandles: []uint64{4}},
		},
	}
	sort.Slice(wire.InactiveAssociations, func(i, j int) bool {
		a, b := wire.InactiveAssociations[i], wire.InactiveAssociations[j]
		if a.AttributeHandle != b.AttributeHandle {
			return a.AttributeHandle < b.AttributeHandle
		}
		return a.AssociatingFederate < b.AssociatingFederate
	})
	manager, err := ddm.New(ddm.Options{FOMs: repo, Outbox: objectStateOutbox{}})
	if err != nil {
		t.Fatal(err)
	}
	return inactiveDDMReviewFixture{v, wire, manager, base, child, position, private, x, y}
}

func (f inactiveDDMReviewFixture) ancestorRow() *rtiv1.DDMInactiveAssociationEntry {
	for _, row := range f.wire.InactiveAssociations {
		if row.AssociatingFederate == 2 {
			return row
		}
	}
	panic("missing ancestor row")
}

func TestInactiveDDMReviewRoundTripPreservesMultipleCallersAndActivation(t *testing.T) {
	f := newInactiveDDMReviewFixture(t)
	data := graphProto(t, f.wire)
	if err := f.v.validateDDM(data); err != nil {
		t.Fatalf("valid known-ancestor association rejected: %v", err)
	}
	codec := DDM{Manager: f.manager}
	commit, err := codec.PrepareRestore("fed", data)
	if err != nil || commit == nil {
		t.Fatalf("prepare: %v", err)
	}
	if f.manager.ExportState("fed") != nil {
		t.Fatal("preparation mutated live DDM state")
	}
	commit()
	encoded, err := codec.Marshal("fed")
	if err != nil {
		t.Fatal(err)
	}
	var actual rtiv1.DDMManagerState
	if err := proto.Unmarshal(encoded, &actual); err != nil || !proto.Equal(f.wire, &actual) {
		t.Fatalf("roundtrip lost active/inactive callers or pending ranges: %+v %v", &actual, err)
	}
	// Restored pending associations must still activate on a later acquisition,
	// without destroying another nonowner's pending association on the same attr.
	f.manager.OwnershipChanged("fed", 100, map[core.AttributeHandle]core.FederateHandle{f.position: 1}, map[core.AttributeHandle]core.FederateHandle{f.position: 2})
	if got := f.manager.PublisherRegionsFor("fed", 100, f.position); !slices.Equal(got, []ddm.RegionHandle{2}) {
		t.Fatalf("restored ancestor association did not activate: %v", got)
	}
	state := f.manager.ExportState("fed")
	if len(state.InactiveAssociations) != 2 {
		t.Fatalf("lost other caller's pending associations: %+v", state.InactiveAssociations)
	}
	for _, entry := range state.InactiveAssociations {
		if entry.Federate != 3 {
			t.Fatalf("activated caller retained inactive state: %+v", entry)
		}
	}
	// A pre-extension snapshot remains valid and clears previously live pending
	// state at commit instead of merging it back into a legacy checkpoint.
	legacy := proto.Clone(f.wire).(*rtiv1.DDMManagerState)
	legacy.InactiveAssociations = nil
	commit, err = codec.PrepareRestore("fed", graphProto(t, legacy))
	if err != nil {
		t.Fatal(err)
	}
	if len(f.manager.ExportState("fed").InactiveAssociations) != 2 {
		t.Fatal("legacy prepare cleared pending state before commit")
	}
	commit()
	if got := f.manager.ExportState("fed").InactiveAssociations; len(got) != 0 {
		t.Fatalf("legacy restore retained stale pending associations: %+v", got)
	}
}

func TestInactiveDDMReviewRebindOnlyFederateCoordinates(t *testing.T) {
	f := newInactiveDDMReviewFixture(t)
	data := graphProto(t, f.wire)
	before := bytes.Clone(data)
	identities := map[core.FederateHandle]core.FederateHandle{1: 303, 2: 101, 3: 202}
	codec := DDM{Manager: f.manager}
	rebound, err := codec.RewriteFederates(data, func(h core.FederateHandle) (core.FederateHandle, error) { return identities[h], nil })
	if err != nil {
		t.Fatal(err)
	}
	expected := proto.Clone(f.wire).(*rtiv1.DDMManagerState)
	for _, region := range expected.Regions {
		region.OwnerFederate = uint64(identities[core.FederateHandle(region.OwnerFederate)])
	}
	for _, row := range expected.InactiveAssociations {
		row.AssociatingFederate = uint64(identities[core.FederateHandle(row.AssociatingFederate)])
	}
	var actual rtiv1.DDMManagerState
	if err := proto.Unmarshal(rebound, &actual); err != nil || !proto.Equal(expected, &actual) || !bytes.Equal(before, data) {
		t.Fatalf("rebind changed non-federate coordinates or source bytes: %+v %v", &actual, err)
	}
	f.v.members = map[core.FederateHandle]string{303: "owner", 101: "ancestor", 202: "child"}
	instance := f.v.objects[100]
	instance.Registrant = 303
	f.v.objects[100] = instance
	f.v.owners = map[[2]uint64]core.FederateHandle{{100, uint64(f.position)}: 303, {100, uint64(f.private)}: 303}
	f.v.knownClasses = map[[2]uint64]core.ObjectClassHandle{{100, 303}: f.child, {100, 101}: f.base, {100, 202}: f.child}
	if err := f.v.validateDDM(rebound); err != nil {
		t.Fatalf("correctly rebound ancestor graph rejected: %v", err)
	}
	stop := errors.New("missing federate mapping")
	failed, err := codec.RewriteFederates(data, func(h core.FederateHandle) (core.FederateHandle, error) {
		if h == 3 {
			return 0, stop
		}
		return identities[h], nil
	})
	if !errors.Is(err, stop) || failed != nil || !bytes.Equal(before, data) || f.manager.ExportState("fed") != nil {
		t.Fatalf("failed rebind exposed partial state: bytes=%x error=%v", failed, err)
	}
}

func TestInactiveDDMReviewCorruptReferences(t *testing.T) {
	for name, change := range map[string]func(*inactiveDDMReviewFixture){
		"invalid dimension context": func(f *inactiveDDMReviewFixture) {
			r := f.wire.Regions[0]
			r.DimensionHandles = []uint64{uint64(f.y)}
			r.Committed[0].DimensionHandle, r.Pending[0].DimensionHandle = uint64(f.y), uint64(f.y)
		},
		"borrow active owner's region": func(f *inactiveDDMReviewFixture) { f.ancestorRow().RegionHandles = []uint64{5} },
		"no associator":                func(f *inactiveDDMReviewFixture) { f.ancestorRow().AssociatingFederate = 0 },
		"no region":                    func(f *inactiveDDMReviewFixture) { f.ancestorRow().RegionHandles = []uint64{0} },
		"associator already owns attr": func(f *inactiveDDMReviewFixture) { f.v.owners[[2]uint64{100, uint64(f.position)}] = 2 },
		"duplicate tuple disjoint regions": func(f *inactiveDDMReviewFixture) {
			region := proto.Clone(f.wire.Regions[0]).(*rtiv1.DDMRegionEntry)
			region.RegionHandle, f.wire.NextRegionHandle = 6, 6
			f.wire.Regions = append(f.wire.Regions, region)
			row := proto.Clone(f.ancestorRow()).(*rtiv1.DDMInactiveAssociationEntry)
			row.RegionHandles = []uint64{6}
			f.wire.InactiveAssociations = append(f.wire.InactiveAssociations, row)
		},
		"empty row": func(f *inactiveDDMReviewFixture) {
			f.wire.InactiveAssociations = append(f.wire.InactiveAssociations, &rtiv1.DDMInactiveAssociationEntry{})
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newInactiveDDMReviewFixture(t)
			change(&f)
			before := f.manager.ExportState("fed")
			if err := f.v.validateDDM(graphProto(t, f.wire)); err == nil {
				t.Fatal("corrupt inactive association accepted")
			}
			if !reflect.DeepEqual(before, f.manager.ExportState("fed")) {
				t.Fatal("failed validation changed live state")
			}
		})
	}
}

func TestInactiveDDMReviewRejectsUnknownNestedRebindFields(t *testing.T) {
	f := newInactiveDDMReviewFixture(t)
	unknown := protowire.AppendTag(nil, 99, protowire.VarintType)
	unknown = protowire.AppendVarint(unknown, 2)
	f.ancestorRow().ProtoReflect().SetUnknown(unknown)
	data := graphProto(t, f.wire)
	got, err := (DDM{Manager: f.manager}).RewriteFederates(data, func(h core.FederateHandle) (core.FederateHandle, error) { return h + 100, nil })
	if err == nil || got != nil {
		t.Fatal("rebind ignored an unknown inactive-association field that might contain a federate reference")
	}
}

func TestInactiveDDMReviewRequiresAvailableKnownAttribute(t *testing.T) {
	for name, change := range map[string]func(*inactiveDDMReviewFixture){
		"no remembered object": func(f *inactiveDDMReviewFixture) { delete(f.v.knownClasses, [2]uint64{100, 2}) },
		"zero effective known class": func(f *inactiveDDMReviewFixture) {
			f.v.knownClasses[[2]uint64{100, 2}] = 0
		},
		"unknown known class": func(f *inactiveDDMReviewFixture) {
			f.v.knownClasses[[2]uint64{100, 2}] = ^core.ObjectClassHandle(0)
		},
		"sibling known class exposes same attribute name": func(f *inactiveDDMReviewFixture) {
			cls, _ := f.v.model.LookupObjectClass("Sibling")
			f.v.knownClasses[[2]uint64{100, 2}] = cls
		},
		"descendant known class exposes same attribute name": func(f *inactiveDDMReviewFixture) {
			cls, _ := f.v.model.LookupObjectClass("Leaf")
			f.v.knownClasses[[2]uint64{100, 2}] = cls
		},
		"forgotten tombstone recipient retains historical class": func(f *inactiveDDMReviewFixture) {
			f.v.deletions = map[core.ObjectHandle]object.DeletionState{100: {Object: 100, ObjectClass: f.child,
				Recipients: []object.DeletionRecipientState{{Recipient: 2, KnownClass: f.base, ReceivedTSO: true, Forgotten: true}},
			}}
		},
		"reversed knowledge key is not membership": func(f *inactiveDDMReviewFixture) {
			delete(f.v.knownClasses, [2]uint64{100, 2})
			f.v.knownClasses[[2]uint64{2, 100}] = f.base
		},
		"child-only attribute aliases parent handle": func(f *inactiveDDMReviewFixture) {
			f.ancestorRow().AttributeHandle = uint64(f.private)
			r := f.wire.Regions[0]
			r.DimensionHandles = []uint64{uint64(f.y)}
			r.Committed[0].DimensionHandle, r.Pending[0].DimensionHandle = uint64(f.y), uint64(f.y)
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newInactiveDDMReviewFixture(t)
			change(&f)
			if err := f.v.validateDDM(graphProto(t, f.wire)); err == nil {
				t.Fatal("inactive association without available known-class attribute accepted")
			}
		})
	}
}

func TestInactiveDDMReviewKnowledgeHistoryIsRecipientSpecific(t *testing.T) {
	f := newInactiveDDMReviewFixture(t)
	f.v.deletions = map[core.ObjectHandle]object.DeletionState{100: {Object: 100, ObjectClass: f.child,
		Recipients: []object.DeletionRecipientState{
			{Recipient: 2, KnownClass: f.base, ReceivedTSO: true},
			{Recipient: 3, KnownClass: f.child, ReceivedTSO: true, Forgotten: true},
		},
	}}
	// The forgotten recipient's knowledge remains useful to retraction checking,
	// but only the still-known recipient may retain inactive DDM associations.
	f.wire.InactiveAssociations = []*rtiv1.DDMInactiveAssociationEntry{f.ancestorRow()}
	if err := f.v.validateDDM(graphProto(t, f.wire)); err != nil {
		t.Fatalf("other recipient's forgotten state invalidated live ancestor knowledge: %v", err)
	}
	if f.v.inactiveAssociationKnown(100, f.position, 3) {
		t.Fatal("forgotten recipient's historical class was treated as current knowledge")
	}
}
