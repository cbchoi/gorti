package ddm

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/pkg/fom/model"
)

// IEEE 1516.1-2010 10.30.3(h-i): lower < upper <= dimension upper bound.
// This admission condition is separate from the 9.1.4 overlap predicate.
func TestIEEE2010SetRangeBoundsStrictAdmission(t *testing.T) {
	const maximum = ^uint64(0)
	for _, limit := range []uint64{100, maximum} {
		for _, pending := range []bool{false, true} {
			t.Run(fmt.Sprintf("limit_%d/pending_%t", limit, pending), func(t *testing.T) {
				fom := model.NewFOMWithDimensions(nil, nil, nil, []model.Dimension{
					{Name: "X", UpperBound: limit}, {Name: "Y", UpperBound: 200},
				})
				m, err := New(Options{Outbox: &fakeOutbox{}, FOMs: fomBackedRepo{fom: fom}})
				if err != nil {
					t.Fatal(err)
				}
				const fed core.FederationName = "strict-bounds"
				const owner core.FederateHandle = 1
				ctx := context.Background()
				space, _ := m.LookupRoutingSpace(fed, DefaultRoutingSpace)
				x, _ := m.LookupDimension(fed, space, "X")
				y, _ := m.LookupDimension(fed, space, "Y")
				r, err := m.CreateRegion(ctx, fed, owner, space, []DimensionHandle{x, y})
				if err != nil {
					t.Fatal(err)
				}
				want := Range{Upper: limit}
				initializeRegion(t, m, fed, owner, r, map[DimensionHandle]Range{x: {Upper: limit}, y: {Upper: 200}})
				if pending {
					want = Range{Lower: 10, Upper: 20}
					for dim, bounds := range map[DimensionHandle]Range{x: want, y: {Lower: 30, Upper: 40}} {
						if err := m.SetRangeBounds(fed, owner, r, dim, bounds); err != nil {
							t.Fatal(err)
						}
					}
				}
				invalid := []Range{{}, {Lower: 50, Upper: 50}, {Lower: limit, Upper: limit}, {Lower: 2, Upper: 1}, {Lower: maximum, Upper: 0}}
				if limit < maximum {
					invalid = append(invalid, Range{Upper: limit + 1}, Range{Lower: limit, Upper: maximum})
				}
				for _, bounds := range invalid {
					t.Run(fmt.Sprintf("reject_%d_%d", bounds.Lower, bounds.Upper), func(t *testing.T) {
						before := m.ExportState(fed)
						if err := m.SetRangeBounds(fed, owner, r, x, bounds); !errors.Is(err, core.ErrInvalidRangeBounds) {
							t.Errorf("SetRangeBounds(%+v) = %v; want ErrInvalidRangeBounds", bounds, err)
						}
						if !reflect.DeepEqual(before, m.ExportState(fed)) {
							t.Error("rejected request changed pending or committed state")
						}
					})
				}
				if err := m.CommitRegionModifications(ctx, fed, owner, []RegionHandle{r}); err != nil {
					t.Fatal(err)
				}
				if got, ok := m.QueryBounds(fed, r, x); !ok || got != want {
					t.Errorf("commit after invalid requests = %+v, %t; want %+v", got, ok, want)
				}
				for _, bounds := range []Range{{Upper: 1}, {Upper: limit}, {Lower: limit - 1, Upper: limit}} {
					before, _ := m.QueryBounds(fed, r, x)
					if err := m.SetRangeBounds(fed, owner, r, x, bounds); err != nil {
						t.Fatal(err)
					}
					if got, _ := m.QueryBounds(fed, r, x); got != before {
						t.Fatal("valid pending bounds became visible before commit")
					}
					if err := m.CommitRegionModifications(ctx, fed, owner, []RegionHandle{r}); err != nil {
						t.Fatal(err)
					}
					if got, ok := m.QueryBounds(fed, r, x); !ok || got != bounds {
						t.Fatalf("committed bounds = %+v, %t; want %+v", got, ok, bounds)
					}
				}
			})
		}
	}
}
