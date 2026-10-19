package server

import (
	"context"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ddm"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
)

func TestMOMRegionalReportsUseCommittedSubjectRange(t *testing.T) {
	d, fom, a, b, _ := newMOMCatalogDeps(t)
	ctx := context.Background()
	const fed core.FederationName = "catalog-provider"
	class, _ := fom.LookupInteractionClass(mom.ClassReportUpdatesSent)
	dimension, ok := fom.(core.FOMHandleNameLookup).LookupDimension("HLAfederate")
	dim := ddm.DimensionHandle(dimension)
	if !ok {
		t.Fatal("MIM federate dimension missing")
	}
	region, err := d.Regions.CreateRegion(ctx, fed, b, 1, []ddm.DimensionHandle{dim})
	if err != nil {
		t.Fatal(err)
	}
	set := func(value uint64) {
		t.Helper()
		if err := d.Regions.SetRangeBounds(fed, b, region, dim, ddm.Range{Lower: value, Upper: value + 1}); err != nil {
			t.Fatal(err)
		}
	}
	commit := func() {
		t.Helper()
		if err := d.Regions.CommitRegionModifications(ctx, fed, b, []ddm.RegionHandle{region}); err != nil {
			t.Fatal(err)
		}
	}
	set(uint64(a))
	commit()
	if err := d.Regions.SubscribeInteractionClassWithRegions(ctx, fed, b, class, []ddm.RegionHandle{region}); err != nil {
		t.Fatal(err)
	}
	sources, err := newMOMCatalogSources(d)
	if err != nil {
		t.Fatal(err)
	}
	check := func(want []core.FederateHandle) {
		t.Helper()
		got, err := sources.ReportRecipients(ctx, fed, a, mom.ClassReportUpdatesSent, "")
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("recipients = %v, %v; want %v", got, err, want)
		}
	}
	check([]core.FederateHandle{b})
	set(uint64(a) + 1)
	check([]core.FederateHandle{b})
	commit()
	check(nil)
	if err := d.Declarations.SubscribeInteractionClass(ctx, fed, b, class); err != nil {
		t.Fatal(err)
	}
	check([]core.FederateHandle{b})
	set(uint64(a))
	commit()
	check([]core.FederateHandle{b})
}

func TestMOMRegionalServiceReportFiltersBothDimensions(t *testing.T) {
	d, fom, a, b, _ := newMOMCatalogDeps(t)
	ctx := context.Background()
	const fed core.FederationName = "catalog-provider"
	class, _ := fom.LookupInteractionClass(mom.ClassReportServiceInvocation)
	names := fom.(core.FOMHandleNameLookup)
	federateDimension, _ := names.LookupDimension("HLAfederate")
	serviceDimension, _ := names.LookupDimension("HLAserviceGroup")
	fedDim, groupDim := ddm.DimensionHandle(federateDimension), ddm.DimensionHandle(serviceDimension)
	region, err := d.Regions.CreateRegion(ctx, fed, b, 1, []ddm.DimensionHandle{fedDim, groupDim})
	if err != nil {
		t.Fatal(err)
	}
	for dim, value := range map[ddm.DimensionHandle]uint64{fedDim: uint64(a), groupDim: 2} {
		if err := d.Regions.SetRangeBounds(fed, b, region, dim, ddm.Range{Lower: value, Upper: value + 1}); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.Regions.CommitRegionModifications(ctx, fed, b, []ddm.RegionHandle{region}); err != nil {
		t.Fatal(err)
	}
	if err := d.Regions.SubscribeInteractionClassWithRegions(ctx, fed, b, class, []ddm.RegionHandle{region}); err != nil {
		t.Fatal(err)
	}
	sources, err := newMOMCatalogSources(d)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		service string
		match   bool
	}{
		{"updateAttributeValues", true}, {"reflectAttributeValues", true}, {momServiceEnableTimeRegulation, false}, {"sendInteractionWithRegions", false},
	} {
		t.Run(tc.service, func(t *testing.T) {
			got, err := sources.ReportRecipients(ctx, fed, a, mom.ClassReportServiceInvocation, tc.service)
			if err != nil || (len(got) == 1 && got[0] == b) != tc.match {
				t.Fatalf("recipients = %v, %v", got, err)
			}
		})
	}
	if _, err := sources.ReportRecipients(ctx, fed, a, mom.ClassReportServiceInvocation, "unknownService"); err == nil {
		t.Fatal("unknown service group silently fabricated")
	}
}
