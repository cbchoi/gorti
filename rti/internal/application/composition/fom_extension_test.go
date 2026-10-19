package composition

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ddm"
	"github.com/cbchoi/gorti/rti/pkg/fom/model"
)

type extensionOutbox struct{}

func (extensionOutbox) Send(context.Context, core.FederationName, core.FederateHandle, core.OutboundEvent) error {
	return nil
}

func TestFOMExtensionPreparationReportsHandleMovementWithoutPublishing(t *testing.T) {
	ctx := context.Background()
	repo := NewFOMRepository()
	base := core.FOMModule{Path: "base.xml", XML: minimalFOMXML(t)}
	h, err := repo.Load(ctx, []core.FOMModule{base})
	if err != nil {
		t.Fatal(err)
	}
	repo.RememberFor("live", h)
	before, _ := h.LookupObjectClass("Vehicle")
	// Mutating the caller's original bytes must not corrupt retained source.
	base.XML[0] = '!'
	extra := core.FOMModule{Path: "extra.xml", XML: []byte(`<objectModel><objects><objectClass><name>HLAobjectRoot</name><objectClass><name>Aardvark</name></objectClass></objectClass></objects></objectModel>`)}
	prepared, err := repo.PrepareExtension(ctx, "live", []core.FOMModule{extra})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, change := range prepared.HandleChanges() {
		if change.Kind == "objectClass" && change.Name == "HLAobjectRoot.Vehicle" {
			found = change.Before == uint64(before) && change.After != change.Before
		}
	}
	if !found {
		t.Fatalf("missing class-handle impact: %+v", prepared.HandleChanges())
	}
	if prepared.BaseDigest() == prepared.MergedDigest() {
		t.Fatal("extension digest unchanged")
	}
	got, _ := repo.Get(ctx, "live")
	if got != h {
		t.Fatal("preparation published FOM")
	}
	if _, ok := got.LookupObjectClass("Aardvark"); ok {
		t.Fatal("uncommitted class visible")
	}
	if current, _ := got.LookupObjectClass("Vehicle"); current != before {
		t.Fatal("existing assigned handle moved")
	}
	changes := prepared.HandleChanges()
	changes[0].Name = corruptFixtureName
	if prepared.HandleChanges()[0].Name == corruptFixtureName {
		t.Fatal("preparation leaked change slice")
	}
}

func TestFOMExtensionPreparationDetectsInheritedHandleMovement(t *testing.T) {
	ctx := context.Background()
	repo := NewFOMRepository()
	base := core.FOMModule{Path: "base.xml", XML: []byte(`<objectModel><objects><objectClass><name>HLAobjectRoot</name><objectClass><name>Parent</name><objectClass><name>Child</name><attribute><name>Own</name><dataType>HLAinteger32BE</dataType></attribute></objectClass></objectClass></objectClass></objects><interactions><interactionClass><name>HLAinteractionRoot</name><interactionClass><name>ParentEvent</name><parameter><name>Inherited</name><dataType>HLAinteger32BE</dataType></parameter><interactionClass><name>ChildEvent</name></interactionClass></interactionClass></interactionClass></interactions></objectModel>`)}
	h, err := repo.Load(ctx, []core.FOMModule{base})
	if err != nil {
		t.Fatal(err)
	}
	repo.RememberFor("live", h)
	extra := core.FOMModule{Path: "extra.xml", XML: []byte(`<objectModel><objects><objectClass><name>HLAobjectRoot</name><objectClass><name>Parent</name><attribute><name>Added</name><dataType>HLAinteger32BE</dataType></attribute></objectClass></objectClass></objects><interactions><interactionClass><name>HLAinteractionRoot</name><interactionClass><name>ParentEvent</name><interactionClass><name>ChildEvent</name><parameter><name>Own</name><dataType>HLAinteger32BE</dataType></parameter></interactionClass></interactionClass></interactionClass></interactions></objectModel>`)}
	prepared, err := repo.PrepareExtension(ctx, "live", []core.FOMModule{extra})
	if err != nil {
		t.Fatal(err)
	}
	attribute, parameter := false, false
	for _, change := range prepared.HandleChanges() {
		if change.Kind == "attribute" && change.Class == "HLAobjectRoot.Parent.Child" && change.Name == "HLAprivilegeToDeleteObject" {
			attribute = true
		}
		if change.Kind == "parameter" && change.Class == "HLAinteractionRoot.ParentEvent.ChildEvent" && change.Name == "Inherited" {
			parameter = true
		}
	}
	if !attribute || !parameter {
		t.Fatalf("missing inherited-handle changes: %+v", prepared.HandleChanges())
	}
}

func TestFOMExtensionPreparationLeavesInitializedDDMUnchanged(t *testing.T) {
	ctx := context.Background()
	repo := NewFOMRepository()
	base := core.FOMModule{Path: "base.xml", XML: []byte(`<objectModel><dimensions><dimension><name>Z</name><upperBound>100</upperBound></dimension></dimensions></objectModel>`)}
	h, err := repo.Load(ctx, []core.FOMModule{base})
	if err != nil {
		t.Fatal(err)
	}
	repo.RememberFor("live", h)
	manager, err := ddm.New(ddm.Options{FOMs: repo, Outbox: extensionOutbox{}})
	if err != nil {
		t.Fatal(err)
	}
	space, _ := manager.LookupRoutingSpace("live", ddm.DefaultRoutingSpace)
	old, _ := manager.LookupDimension("live", space, "Z")
	extra := core.FOMModule{Path: "extra.xml", XML: []byte(`<objectModel><dimensions><dimension><name>A</name><upperBound>100</upperBound></dimension></dimensions></objectModel>`)}
	prepared, err := repo.PrepareExtension(ctx, "live", []core.FOMModule{extra})
	if err != nil {
		t.Fatal(err)
	}
	if !prepared.RequiresDimensionRefresh() {
		t.Fatal("missing initialized-cache refresh requirement")
	}
	if got, _ := manager.LookupDimension("live", space, "Z"); got != old {
		t.Fatal("preparation changed cached dimension")
	}
	if _, ok := manager.LookupDimension("live", space, "A"); ok {
		t.Fatal("preparation changed DDM cache")
	}
	if got, _ := repo.Get(ctx, "live"); got != h {
		t.Fatal("preparation published dimension model")
	}
}

func TestFOMExtensionPreparationFailurePreservesState(t *testing.T) {
	ctx := context.Background()
	repo := NewFOMRepository()
	base := updateRateModule("Fast", "12.5")
	h, err := repo.Load(ctx, []core.FOMModule{base})
	if err != nil {
		t.Fatal(err)
	}
	repo.RememberFor("live", h)
	digest, _ := repo.DigestFor("live")
	for _, invalid := range []core.FOMModule{updateRateModule("Fast", "25"), {Path: "malformed.xml", XML: []byte(`<objectModel>`)}} {
		prepared, err := repo.PrepareExtension(ctx, "live", []core.FOMModule{invalid})
		if err == nil || prepared != nil {
			t.Fatal("invalid extension prepared")
		}
		if !strings.Contains(err.Error(), invalid.Path) {
			t.Fatalf("missing diagnostic source: %v", err)
		}
		got, _ := repo.Get(ctx, "live")
		after, _ := repo.DigestFor("live")
		if got != h || after != digest {
			t.Fatal("failed preparation changed live state")
		}
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := repo.PrepareExtension(cancelled, "live", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	if _, err := repo.PrepareExtension(ctx, "absent", nil); !errors.Is(err, core.ErrFederationNotFound) {
		t.Fatalf("missing federation: %v", err)
	}
	repo.RememberFor("programmatic", NewModelHandle(model.NewFOM(nil, nil, nil)))
	if _, err := repo.PrepareExtension(ctx, "programmatic", nil); err == nil {
		t.Fatal("prepared extension without original module bytes")
	}
	prepared, err := repo.PrepareExtension(ctx, "live", nil)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.BaseDigest() != prepared.MergedDigest() || len(prepared.HandleChanges()) != 0 || prepared.RequiresDimensionRefresh() {
		t.Fatal("empty preparation changed model")
	}
}

const corruptFixtureName = "corrupt"
