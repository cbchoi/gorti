package ddm

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type associationNamesFOM struct{ permissiveFOMHandle }

func (associationNamesFOM) ObjectClassName(cls core.ObjectClassHandle) (string, bool) {
	return "Item", cls == 1
}
func (associationNamesFOM) AttributeName(cls core.ObjectClassHandle, attr core.AttributeHandle) (string, bool) {
	return "value", cls == 1 && attr == 1
}

type associationNamesRepo struct{ permissiveFOMRepo }

func (associationNamesRepo) Get(context.Context, core.FederationName) (core.FOMHandle, error) {
	return associationNamesFOM{}, nil
}

func TestIEEE2010AssociationEmptyAttributeValidationWithoutDimensionMetadata(t *testing.T) {
	m, regions := groupedFixture(t)
	m.opts.FOMs = associationNamesRepo{}
	m.opts.Objects = w4Objects{}
	ctx := context.Background()
	groups := []core.AttributeRegionGroup{{Attributes: []core.AttributeHandle{1}, Regions: regions[:1]}, {Attributes: []core.AttributeHandle{999}}}
	before := m.ExportState("fed")
	for _, call := range []func() error{
		func() error { return m.AssociateRegionGroupsForUpdates(ctx, "fed", 1, 9, groups) },
		func() error { return m.UnassociateRegionGroupsForUpdates(ctx, "fed", 1, 9, groups) },
	} {
		if err := call(); !errors.Is(err, core.ErrAttributeNotFound) {
			t.Errorf("undefined empty-product attribute: %v", err)
		}
		if !reflect.DeepEqual(before, m.ExportState("fed")) {
			t.Fatal("undefined later attribute changed associations")
		}
	}
}
