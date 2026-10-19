package parser_test

import (
	"context"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/application/composition"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ddm"
)

type dimensionMergeOutbox struct{}

func (dimensionMergeOutbox) Send(context.Context, core.FederationName, core.FederateHandle, core.OutboundEvent) error {
	return nil
}

func TestDuplicateDimensionsProductionRegionContext(t *testing.T) {
	ctx := context.Background()
	const fed core.FederationName = "duplicate-dimensions"
	xml := []byte(`<objectModel><objects><objectClass><name>HLAobjectRoot</name><objectClass><name>Vehicle</name><attribute><name>Position</name><dataType>HLAinteger32BE</dataType><dimensions><dimension>X</dimension></dimensions></attribute></objectClass></objectClass></objects><dimensions><dimension><name>X</name><upperBound>100</upperBound></dimension></dimensions></objectModel>`)
	repo := composition.NewFOMRepository()
	h, err := repo.Load(ctx, []core.FOMModule{{Path: "first.xml", XML: xml}, {Path: "second.xml", XML: xml}})
	if err != nil {
		t.Fatal(err)
	}
	repo.RememberFor(fed, h)
	m, err := ddm.New(ddm.Options{FOMs: repo, Outbox: dimensionMergeOutbox{}})
	if err != nil {
		t.Fatal(err)
	}
	cls, ok := h.LookupObjectClass("Vehicle")
	if !ok {
		t.Fatal("Vehicle missing")
	}
	attr, ok := h.LookupAttribute(cls, "Position")
	if !ok {
		t.Fatal("Position missing")
	}
	required, ok := h.(core.FOMAttributeDimensions).AttributeDimensions(cls, attr)
	if !ok {
		t.Fatal("attribute dimension context missing")
	}
	space, ok := m.LookupRoutingSpace(fed, ddm.DefaultRoutingSpace)
	if !ok {
		t.Fatal("routing space missing")
	}
	dim, ok := m.LookupDimension(fed, space, "X")
	if !ok {
		t.Fatal("X missing")
	}
	region, err := m.CreateRegion(ctx, fed, 1, space, []ddm.DimensionHandle{dim})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("production dimension handles: attribute=%v DDM=%d", required, dim)
	if err := m.SetRangeBounds(fed, 1, region, dim, ddm.Range{Upper: 100}); err != nil {
		t.Fatal(err)
	}
	if err := m.CommitRegionModifications(ctx, fed, 1, []ddm.RegionHandle{region}); err != nil {
		t.Fatal(err)
	}
	tx, err := m.PrepareObjectRegistration(ctx, fed, 1, cls, map[core.AttributeHandle][]core.DDMRegionHandleCore{attr: {region}})
	if err != nil {
		t.Fatalf("identical modular dimension declarations broke region context: %v", err)
	}
	defer tx.Release()
	if len(required) != 1 || required[0] != dim {
		t.Fatalf("dimension handles differ: attribute=%v DDM=%d", required, dim)
	}
	tx.Commit(42)
}
