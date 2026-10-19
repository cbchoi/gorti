package ddm_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestSetRangeBoundsRejectsInvalidWithoutMutation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		bounds core.DDMRange
	}{
		{"equal_zero", core.DDMRange{}},
		{"equal_middle", core.DDMRange{Lower: 50, Upper: 50}},
		{"equal_at_limit", core.DDMRange{Lower: 100, Upper: 100}},
		{"reversed", core.DDMRange{Lower: 40, Upper: 30}},
		{"above_dimension", core.DDMRange{Lower: 10, Upper: 101}},
		{"both_above_dimension", core.DDMRange{Lower: 101, Upper: 102}},
		{"equal_above_dimension", core.DDMRange{Lower: 101, Upper: 101}},
		{"uint64_maximum", core.DDMRange{Lower: 0, Upper: ^uint64(0)}},
	} {
		for _, pending := range []bool{false, true} {
			name := tc.name + "/no_pending"
			if pending {
				name = tc.name + "/existing_pending"
			}
			t.Run(name, func(t *testing.T) {
				m := newManager(t, registrationFOM())
				rh := newRegion(t, m, testOwner, 1, 2)
				committed := core.DDMRange{Lower: 5, Upper: 15}
				if err := m.SetRangeBounds(testFed, testOwner, rh, 1, committed); err != nil {
					t.Fatal(err)
				}
				if err := m.CommitRegionModifications(context.Background(), testFed, testOwner, []core.DDMRegionHandleCore{rh}); err != nil {
					t.Fatal(err)
				}
				want := committed
				if pending {
					want = core.DDMRange{Lower: 20, Upper: 30}
					if err := m.SetRangeBounds(testFed, testOwner, rh, 1, want); err != nil {
						t.Fatal(err)
					}
					if err := m.SetRangeBounds(testFed, testOwner, rh, 2, core.DDMRange{Lower: 40, Upper: 50}); err != nil {
						t.Fatal(err)
					}
				}
				before := m.ExportState(testFed)
				err := m.SetRangeBounds(testFed, testOwner, rh, 1, tc.bounds)
				if !errors.Is(err, core.ErrInvalidRangeBounds) {
					t.Errorf("SetRangeBounds(%+v) = %v, want ErrInvalidRangeBounds", tc.bounds, err)
				}
				if after := m.ExportState(testFed); !reflect.DeepEqual(before, after) {
					t.Error("invalid bounds changed pending or committed state")
				}
				if err := m.CommitRegionModifications(context.Background(), testFed, testOwner, []core.DDMRegionHandleCore{rh}); err != nil {
					t.Fatal(err)
				}
				if got, ok := m.QueryBounds(testFed, rh, 1); !ok || got != want {
					t.Fatalf("committed bounds = %+v, %v; want %+v", got, ok, want)
				}
			})
		}
	}
}

func TestSetRangeBoundsAcceptsNonemptyAndDimensionLimit(t *testing.T) {
	for _, bounds := range []core.DDMRange{{Lower: 0, Upper: 1}, {Lower: 50, Upper: 51}, {Lower: 99, Upper: 100}, {Lower: 0, Upper: 100}} {
		m := newManager(t, registrationFOM())
		rh := newRegion(t, m, testOwner, 1)
		if err := m.SetRangeBounds(testFed, testOwner, rh, 1, bounds); err != nil {
			t.Fatalf("SetRangeBounds(%+v): %v", bounds, err)
		}
		if err := m.CommitRegionModifications(context.Background(), testFed, testOwner, []core.DDMRegionHandleCore{rh}); err != nil {
			t.Fatal(err)
		}
		if got, ok := m.QueryBounds(testFed, rh, 1); !ok || got != bounds {
			t.Fatalf("got %+v, %v; want %+v", got, ok, bounds)
		}
	}
}

func TestSetRangeBoundsUsesDimensionSpecificLimit(t *testing.T) {
	m := newManager(t, registrationFOM())
	rh := newRegion(t, m, testOwner, 1, 2)
	if err := m.SetRangeBounds(testFed, testOwner, rh, 2, core.DDMRange{Lower: 150, Upper: 200}); err != nil {
		t.Fatal(err)
	}
	if err := m.SetRangeBounds(testFed, testOwner, rh, 1, core.DDMRange{Lower: 150, Upper: 200}); !errors.Is(err, core.ErrInvalidRangeBounds) {
		t.Fatalf("wrong dimension limit: %v", err)
	}
}

func TestSetRangeBoundsZeroFOMUpperBound(t *testing.T) {
	const maximum = ^uint64(0)
	for _, tc := range []struct {
		name   string
		bounds core.DDMRange
	}{
		{"initial_round_trip", core.DDMRange{Upper: maximum}},
		{"narrow", core.DDMRange{Upper: 1}},
		{"near_maximum", core.DDMRange{Lower: maximum - 1, Upper: maximum}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := registrationFOM()
			h.dimensions[0].UpperBound = 0 // Legacy model sentinel for an unspecified upper bound.
			m := newManager(t, h)
			rh := newRegion(t, m, testOwner, 1)
			initial, ok := m.QueryBounds(testFed, rh, 1)
			if !ok || initial != (core.DDMRange{Upper: maximum}) {
				t.Fatalf("initial unspecified bounds = %+v, %v", initial, ok)
			}
			if err := m.SetRangeBounds(testFed, testOwner, rh, 1, tc.bounds); err != nil {
				t.Fatalf("valid bounds with unspecified FOM upper bound = %v", err)
			}
			before := m.ExportState(testFed)
			if err := m.SetRangeBounds(testFed, testOwner, rh, 1, core.DDMRange{Lower: 2, Upper: 1}); !errors.Is(err, core.ErrInvalidRangeBounds) {
				t.Fatalf("reversed bounds = %v, want ErrInvalidRangeBounds", err)
			}
			if after := m.ExportState(testFed); !reflect.DeepEqual(before, after) {
				t.Fatal("reversed bounds changed pending or committed state")
			}
			if got, ok := m.QueryBounds(testFed, rh, 1); !ok || got != initial {
				t.Fatalf("uncommitted bounds = %+v, %v, want initial %+v", got, ok, initial)
			}
			if err := m.CommitRegionModifications(context.Background(), testFed, testOwner, []core.DDMRegionHandleCore{rh}); err != nil {
				t.Fatal(err)
			}
			if got, ok := m.QueryBounds(testFed, rh, 1); !ok || got != tc.bounds {
				t.Fatalf("committed bounds = %+v, %v, want %+v", got, ok, tc.bounds)
			}
		})
	}
}
