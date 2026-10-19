package mom_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
)

const fedRequest = "HLAmanager.HLAfederate.HLArequest."
const federationRequest = "HLAmanager.HLAfederation.HLArequest."
const modifyState = "HLAmanager.HLAfederate.HLAadjust.HLAmodifyAttributeState"

func catalogInt(v uint32) []byte { b := make([]byte, 4); binary.BigEndian.PutUint32(b, v); return b }

func remainingSources(t *testing.T) mom.CatalogSources {
	t.Helper()
	return mom.CatalogSources{
		Subscriptions: func(_ context.Context, fed core.FederationName, h core.FederateHandle) (mom.SubscriptionSnapshot, error) {
			if fed != catalogFederation || h != catalogTarget {
				t.Fatal("subscription subject")
			}
			return mom.SubscriptionSnapshot{Objects: []mom.ObjectSubscription{
				{Class: 90, Active: true, MaxUpdateRate: "fast", Attributes: []core.AttributeHandle{12, 11}},
				{Class: 90, Active: false, MaxUpdateRate: "slow", Attributes: []core.AttributeHandle{13}},
			}, Interactions: []mom.InteractionSubscription{{Class: 70, Active: true}, {Class: 71, Active: false}}}, nil
		},
		ObjectInstanceCounts: func(_ context.Context, _ core.FederationName, h core.FederateHandle, k mom.ObjectInstanceCountKind) (map[uint64]uint32, error) {
			if h != catalogTarget {
				t.Fatal("count subject")
			}
			count := map[mom.ObjectInstanceCountKind]uint32{mom.ObjectsDeletable: 3, mom.ObjectsUpdated: 4, mom.ObjectsReflected: 5}[k]
			if count == 0 {
				t.Fatal("count kind")
			}
			return map[uint64]uint32{90: count, 91: 0}, nil
		},
		ObjectInstanceInformation: func(_ context.Context, _ core.FederationName, h core.FederateHandle, obj core.ObjectHandle) (mom.ObjectInstanceInformation, error) {
			if h != catalogTarget {
				t.Fatal("information subject")
			}
			if obj == 123 {
				return mom.ObjectInstanceInformation{Known: true, RegisteredClass: 90, KnownClass: 80, OwnedAttributes: []core.AttributeHandle{11, 12}}, nil
			}
			return mom.ObjectInstanceInformation{}, nil
		},
		FOMModules: func(_ context.Context, _ core.FederationName, h core.FederateHandle) ([]mom.FOMModule, error) {
			if h != 0 && h != catalogTarget {
				t.Fatal("module subject")
			}
			return []mom.FOMModule{{Indicator: 4, Designator: "actual.xml", Data: "<objectModel/>"}}, nil
		},
		SynchronizationPoints: func(context.Context, core.FederationName) ([]mom.SynchronizationPoint, error) {
			return []mom.SynchronizationPoint{{Name: "a", Federates: []mom.SynchronizationFederate{{Handle: catalogTarget, Status: 2}, {Handle: 7, Status: 3}}}}, nil
		},
	}
}

func remainingRequests() []string {
	return []string{fedRequest + "HLArequestSubscriptions", fedRequest + "HLArequestObjectInstancesThatCanBeDeleted", fedRequest + "HLArequestObjectInstancesUpdated", fedRequest + "HLArequestObjectInstancesReflected", fedRequest + "HLArequestObjectInstanceInformation", fedRequest + "HLArequestFOMmoduleData", federationRequest + "HLArequestFOMmoduleData", federationRequest + "HLArequestSynchronizationPoints", federationRequest + "HLArequestSynchronizationPointStatus"}
}

func remainingParameters(request string) map[string][]byte {
	v := map[string][]byte{}
	if strings.HasPrefix(request, fedRequest) {
		v["HLAfederate"] = catalogHandle(uint64(catalogTarget))
	}
	if strings.HasSuffix(request, "Information") {
		v["HLAobjectInstance"] = catalogHandle(123)
	}
	if strings.HasSuffix(request, "FOMmoduleData") {
		v["HLAFOMmoduleIndicator"] = catalogInt(4)
	}
	return v
}

func TestMOMRemainingCatalogProductionEncoding(t *testing.T) {
	for _, request := range remainingRequests() {
		t.Run(request, func(t *testing.T) {
			h := newCatalog(t)
			h.d.SetCatalogSources(remainingSources(t))
			if err := h.request(t, request, remainingParameters(request)); err != nil {
				t.Fatal(err)
			}
			report := strings.ReplaceAll(request, "HLArequest", "HLAreport")
			if strings.HasSuffix(request, "Subscriptions") {
				report = "HLAmanager.HLAfederate.HLAreport.HLAreportObjectClassSubscription"
			}
			r := h.reports(t, report)
			if len(r) == 0 {
				t.Fatal("no production report")
			}
			if strings.HasPrefix(request, fedRequest) && !bytes.Equal(r[0]["HLAfederate"], catalogHandle(uint64(catalogTarget))) {
				t.Fatal("wrong subject")
			}
			switch {
			case strings.HasSuffix(request, "Subscriptions"):
				if len(r) != 2 || len(r[0]) != 6 || !bytes.Equal(r[0]["HLAnumberOfClasses"], catalogInt(2)) {
					t.Fatalf("object subscriptions: %v", r)
				}
				for _, v := range r {
					if readCatalogString(t, v["HLAmaxUpdateRate"]) == "" {
						t.Fatal("missing actual rate")
					}
				}
				ir := h.reports(t, "HLAmanager.HLAfederate.HLAreport.HLAreportInteractionSubscription")
				want := append(catalogInt(2), catalogHandle(70)...)
				want = append(want, catalogInt(1)...)
				want = append(want, catalogHandle(71)...)
				want = append(want, catalogInt(0)...)
				if len(ir) != 1 || len(ir[0]) != 2 || !bytes.Equal(ir[0]["HLAinteractionClassList"], want) {
					t.Fatal("interaction subscription record encoding")
				}
			case strings.Contains(request, "ObjectInstances"):
				want := uint32(3)
				if strings.HasSuffix(request, "Updated") {
					want = 4
				}
				if strings.HasSuffix(request, "Reflected") {
					want = 5
				}
				b := r[0]["HLAobjectInstanceCounts"]
				if len(r[0]) != 2 || len(b) != 20 || binary.BigEndian.Uint32(b) != 1 || binary.BigEndian.Uint64(b[8:]) != 90 || binary.BigEndian.Uint32(b[16:]) != want {
					t.Fatalf("instance counts %x", b)
				}
			case strings.HasSuffix(request, "Information"):
				if len(r[0]) != 5 || !bytes.Equal(r[0]["HLAknownClass"], catalogHandle(80)) || !bytes.Equal(r[0]["HLAregisteredClass"], catalogHandle(90)) {
					t.Fatal("known/registered class")
				}
			case strings.HasSuffix(request, "FOMmoduleData"):
				if !bytes.Equal(r[0]["HLAFOMmoduleIndicator"], catalogInt(4)) || readCatalogString(t, r[0]["HLAFOMmoduleData"]) != "<objectModel/>" {
					t.Fatal("module source not preserved")
				}
			case strings.HasSuffix(request, "SynchronizationPoints"):
				if !bytes.Equal(r[0]["HLAsyncPoints"], []byte{0, 0, 0, 1, 0, 0, 0, 1, 0, 97}) {
					t.Fatalf("sync list %x", r[0]["HLAsyncPoints"])
				}
			case strings.HasSuffix(request, "Status"):
				b := r[0]["HLAsyncPointFederates"]
				if readCatalogString(t, r[0]["HLAsyncPointName"]) != "a" || len(b) != 36 || binary.BigEndian.Uint64(b[8:]) != 7 || binary.BigEndian.Uint32(b[16:]) != 3 {
					t.Fatal("sync status record encoding")
				}
			}
		})
	}
}

func TestMOMRemainingCatalogUnavailableAndMalformed(t *testing.T) {
	for _, request := range remainingRequests() {
		t.Run(request, func(t *testing.T) {
			h := newCatalog(t)
			if err := h.request(t, request, remainingParameters(request)); err == nil {
				t.Fatal("unavailable state acknowledged")
			}
			if len(h.out.sent) != 0 {
				t.Fatal("unavailable state fabricated response")
			}
			h.d.SetCatalogSources(remainingSources(t))
			if strings.HasPrefix(request, fedRequest) {
				p := remainingParameters(request)
				p["HLAfederate"] = catalogHandle(999)
				if err := h.request(t, request, p); err == nil {
					t.Fatal("unknown target accepted")
				}
			}
		})
	}
	h := newCatalog(t)
	sources := remainingSources(t)
	sources.SynchronizationPoints = func(context.Context, core.FederationName) ([]mom.SynchronizationPoint, error) {
		return []mom.SynchronizationPoint{{Name: "a"}, {Name: "b"}}, nil
	}
	h.d.SetCatalogSources(sources)
	if err := h.request(t, federationRequest+"HLArequestSynchronizationPointStatus", nil); err != nil {
		t.Fatal(err)
	}
	statuses := h.reports(t, "HLAmanager.HLAfederation.HLAreport.HLAreportSynchronizationPointStatus")
	if len(statuses) != 2 || readCatalogString(t, statuses[0]["HLAsyncPointName"]) != "a" || readCatalogString(t, statuses[1]["HLAsyncPointName"]) != "b" {
		t.Fatal("all actual synchronization points not reported", statuses)
	}
	sources = remainingSources(t)
	h.d.SetCatalogSources(sources)
	p := remainingParameters(fedRequest + "HLArequestObjectInstanceInformation")
	p["HLAobjectInstance"] = catalogHandle(999)
	if err := h.request(t, fedRequest+"HLArequestObjectInstanceInformation", p); err != nil {
		t.Fatal(err)
	}
	r := h.reports(t, "HLAmanager.HLAfederate.HLAreport.HLAreportObjectInstanceInformation")
	if len(r) != 1 || len(r[0]) != 3 || !bytes.Equal(r[0]["HLAownedInstanceAttributeList"], catalogInt(0)) {
		t.Fatal("unknown object is not NULL response")
	}
}

func TestMOMModifyAttributeStateProvider(t *testing.T) {
	h := newCatalog(t)
	calls := 0
	sources := remainingSources(t)
	sources.ModifyAttributeState = func(_ context.Context, fed core.FederationName, target core.FederateHandle, obj core.ObjectHandle, attr core.AttributeHandle, owned bool) error {
		calls++
		if fed != catalogFederation || target != catalogTarget || obj != 123 || attr != 11 || !owned {
			t.Fatal("wrong ownership transition")
		}
		return nil
	}
	h.d.SetCatalogSources(sources)
	p := map[string][]byte{"HLAfederate": catalogHandle(uint64(catalogTarget)), "HLAobjectInstance": catalogHandle(123), "HLAattribute": catalogHandle(11), "HLAattributeState": catalogInt(1)}
	if err := h.request(t, modifyState, p); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("ownership provider not called")
	}
	before := h.m.ExportState(catalogFederation)
	p["HLAattributeState"] = catalogInt(2)
	if err := h.request(t, modifyState, p); err == nil {
		t.Fatal("invalid ownership state accepted")
	}
	if calls != 1 {
		t.Fatal("malformed request mutated provider")
	}
	p["HLAattributeState"] = catalogInt(1)
	sources.ModifyAttributeState = func(context.Context, core.FederationName, core.FederateHandle, core.ObjectHandle, core.AttributeHandle, bool) error {
		return errors.New("RTI-owned attribute")
	}
	h.d.SetCatalogSources(sources)
	if err := h.request(t, modifyState, p); err == nil {
		t.Fatal("provider rejection swallowed")
	}
	if !reflect.DeepEqual(before, h.m.ExportState(catalogFederation)) {
		t.Fatal("rejection mutated MOM")
	}
}
