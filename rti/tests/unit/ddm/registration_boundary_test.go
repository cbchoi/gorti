package ddm_test

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestPrepareObjectRegistrationDirectBoundaryRejectsMalformed(t *testing.T) {
	for _, tc := range []struct {
		name     string
		bindings map[core.AttributeHandle][]core.DDMRegionHandleCore
		want     error
	}{
		{"nil_region_list", map[core.AttributeHandle][]core.DDMRegionHandleCore{1: nil}, core.ErrInvalidRegionContext},
		{"empty_region_list", map[core.AttributeHandle][]core.DDMRegionHandleCore{1: {}}, core.ErrInvalidRegionContext},
		{"zero_attribute", map[core.AttributeHandle][]core.DDMRegionHandleCore{0: {1}}, core.ErrAttributeNotFound},
		{"zero_region", map[core.AttributeHandle][]core.DDMRegionHandleCore{1: {0}}, core.ErrRegionNotFound},
		{"unknown_attribute_empty_regions", map[core.AttributeHandle][]core.DDMRegionHandleCore{99: nil}, core.ErrAttributeNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := registrationFOM()
			h.attributes[0] = []core.DDMDimensionHandle{1}
			m := newManager(t, h)
			newRegion(t, m, testOwner, 1)
			before := m.ExportState(testFed)
			tx, err := registrar(t, m).PrepareObjectRegistration(context.Background(), testFed, testOwner, testClass, tc.bindings)
			if tx != nil {
				tx.Release()
				t.Error("malformed binding returned a preparation")
			}
			if !errors.Is(err, tc.want) {
				t.Errorf("Prepare = %v, want %v", err, tc.want)
			}
			if after := m.ExportState(testFed); !reflect.DeepEqual(before, after) {
				t.Error("malformed bindings changed DDM state")
			}
		})
	}
}

func TestPrepareObjectRegistrationInvalidAttributeDoesNotCreateState(t *testing.T) {
	for _, attr := range []core.AttributeHandle{0, 99} {
		h := registrationFOM()
		h.attributes[0] = []core.DDMDimensionHandle{1}
		m := newManager(t, h)
		tx, err := registrar(t, m).PrepareObjectRegistration(context.Background(), testFed, testOwner, testClass, map[core.AttributeHandle][]core.DDMRegionHandleCore{attr: {0}})
		if tx != nil {
			tx.Release()
			t.Error("invalid attribute returned a preparation")
		}
		if !errors.Is(err, core.ErrAttributeNotFound) {
			t.Errorf("attribute %d: Prepare = %v, want ErrAttributeNotFound before region validation", attr, err)
		}
		if m.ExportState(testFed) != nil {
			t.Error("invalid attribute created DDM federation state")
		}
	}
}

func TestPrepareObjectRegistrationNormalizesRegionCopies(t *testing.T) {
	m := newManager(t, registrationFOM())
	first := newRegion(t, m, testOwner, 1)
	second := newRegion(t, m, testOwner, 1)
	regions := []core.DDMRegionHandleCore{second, first, second, first}
	before := slices.Clone(regions)
	tx := prepare(t, m, map[core.AttributeHandle][]core.DDMRegionHandleCore{1: regions})
	if !slices.Equal(regions, before) {
		t.Error("preparation normalized the caller's slice in place")
	}
	regions[0] = 999
	tx.Commit(42)
	tx.Release()
	if got := m.PublisherRegionsFor(testFed, 42, 1); !slices.Equal(got, []core.DDMRegionHandleCore{first, second}) {
		t.Fatalf("regions = %v, want sorted independent set [%d %d]", got, first, second)
	}
}

func TestPrepareObjectRegistrationWholeEmptyBindings(t *testing.T) {
	for _, tc := range []struct {
		name     string
		bindings map[core.AttributeHandle][]core.DDMRegionHandleCore
	}{
		{"nil", nil},
		{"empty", map[core.AttributeHandle][]core.DDMRegionHandleCore{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newManager(t, registrationFOM())
			tx := prepare(t, m, tc.bindings)
			tx.Commit(42)
			tx.Release()
			if m.HasObjectAssociations(testFed, 42) || m.ExportState(testFed) != nil {
				t.Fatal("empty registration created DDM associations or federation state")
			}
			for _, cls := range []core.ObjectClassHandle{0, 99} {
				tx, err := registrar(t, m).PrepareObjectRegistration(context.Background(), testFed, testOwner, cls, tc.bindings)
				if tx != nil {
					tx.Release()
					t.Error("invalid class returned a preparation")
				}
				if !errors.Is(err, core.ErrObjectClassNotFound) {
					t.Errorf("empty bindings class %d: Prepare = %v, want ErrObjectClassNotFound", cls, err)
				}
				if m.ExportState(testFed) != nil {
					t.Error("invalid class created DDM federation state")
				}
			}
		})
	}
}
