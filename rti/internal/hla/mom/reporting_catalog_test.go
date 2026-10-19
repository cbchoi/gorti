package mom_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
)

func TestMOMCatalogRecipientRouting(t *testing.T) {
	h := newCatalog(t)
	sources := remainingSources(t)
	calls := 0
	sources.ReportRecipients = func(_ context.Context, fed core.FederationName, subject core.FederateHandle, class, service string) ([]core.FederateHandle, error) {
		calls++
		if fed != catalogFederation || subject != catalogTarget || class != "HLAmanager.HLAfederate.HLAreport.HLAreportObjectInstancesUpdated" || service != "" {
			t.Fatal("wrong routing metadata")
		}
		return []core.FederateHandle{catalogTarget, catalogTarget}, nil
	}
	h.d.SetCatalogSources(sources)
	request := fedRequest + "HLArequestObjectInstancesUpdated"
	if err := h.request(t, request, remainingParameters(request)); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || len(h.out.sent) != 1 || h.out.sent[0].recipient != catalogTarget {
		t.Fatal("provider route ignored or duplicated")
	}
	h.out.sent = nil
	sources.ReportRecipients = func(context.Context, core.FederationName, core.FederateHandle, string, string) ([]core.FederateHandle, error) {
		return nil, nil
	}
	h.d.SetCatalogSources(sources)
	if err := h.request(t, request, remainingParameters(request)); err != nil {
		t.Fatal(err)
	}
	if len(h.out.sent) != 0 {
		t.Fatal("unsubscribed requester received report")
	}
}

func TestMOMServiceReportingExclusionAndFailure(t *testing.T) {
	h := newCatalog(t)
	sources := remainingSources(t)
	cls, _ := h.fom.LookupInteractionClass(mom.ClassReportServiceInvocation)
	sources.Subscriptions = func(context.Context, core.FederationName, core.FederateHandle) (mom.SubscriptionSnapshot, error) {
		return mom.SubscriptionSnapshot{Interactions: []mom.InteractionSubscription{{Class: cls, Active: false}}}, nil
	}
	sources.ReportRecipients = func(context.Context, core.FederationName, core.FederateHandle, string, string) ([]core.FederateHandle, error) {
		return []core.FederateHandle{7}, nil
	}
	h.d.SetCatalogSources(sources)
	p := map[string][]byte{"HLAfederate": catalogHandle(uint64(catalogTarget)), "HLAreportingState": catalogInt(1)}
	if err := h.request(t, mom.ClassSetServiceReporting, p); err == nil {
		t.Fatal("enabled service reporting for its subscriber")
	}
	if h.m.ServiceReporting(catalogFederation, catalogTarget) {
		t.Fatal("rejection changed reporting switch")
	}
	r := h.reports(t, mom.ClassReportMOMException)
	if len(r) != 1 || !bytes.Equal(r[0]["HLAparameterError"], catalogInt(0)) {
		t.Fatal("missing precondition failure report")
	}
	h.out.sent = nil
	p["HLAreportingState"] = []byte{1}
	if err := h.request(t, mom.ClassSetServiceReporting, p); err == nil {
		t.Fatal("malformed switch accepted")
	}
	r = h.reports(t, mom.ClassReportMOMException)
	if len(r) != 1 || !bytes.Equal(r[0]["HLAparameterError"], catalogInt(1)) || readCatalogString(t, r[0]["HLAservice"]) != "HLAinteractionRoot."+mom.ClassSetServiceReporting {
		t.Fatal("malformed parameter failure encoding")
	}
}

func TestMOMServiceReportsEncodingAndSerialRestore(t *testing.T) {
	h := newCatalog(t)
	sources := remainingSources(t)
	sources.ReportRecipients = func(_ context.Context, _ core.FederationName, subject core.FederateHandle, _ string, service string) ([]core.FederateHandle, error) {
		if subject != catalogTarget || service != "RegisterObjectInstance" {
			t.Fatal("service route dimensions")
		}
		return []core.FederateHandle{7}, nil
	}
	h.d.SetCatalogSources(sources)
	h.m.SetServiceReporting(catalogFederation, catalogTarget, true)
	h.m.SetExceptionReporting(catalogFederation, catalogTarget, true)
	call := func(inv mom.ServiceInvocation) {
		t.Helper()
		if err := h.d.ReportServiceInvocation(context.Background(), catalogFederation, catalogTarget, inv, h.fom, h.fom.(core.FOMHandleNameLookup)); err != nil {
			t.Fatal(err)
		}
	}
	call(mom.ServiceInvocation{Service: "RegisterObjectInstance", Success: true, SuppliedArguments: []string{"a", "bb"}, ReturnedArguments: []string{"42"}})
	r := h.reports(t, mom.ClassReportServiceInvocation)
	if len(r) != 1 || len(r[0]) != 7 || binary.BigEndian.Uint32(r[0]["HLAserialNumber"]) != 0 || readCatalogString(t, r[0]["HLAexception"]) != "" {
		t.Fatal("first service report")
	}
	if !bytes.Equal(r[0]["HLAsuppliedArguments"], []byte{0, 0, 0, 2, 0, 0, 0, 1, 0, 97, 0, 0, 0, 0, 0, 2, 0, 98, 0, 98}) {
		t.Fatalf("argument array alignment: %x", r[0]["HLAsuppliedArguments"])
	}
	state := h.m.ExportState(catalogFederation)
	commit, err := h.m.PrepareImportState(catalogFederation, state)
	if err != nil {
		t.Fatal(err)
	}
	commit()
	call(mom.ServiceInvocation{Service: "RegisterObjectInstance", Success: false, SuppliedArguments: []string{"a"}, Exception: "NameInUse"})
	r = h.reports(t, mom.ClassReportServiceInvocation)
	if len(r) != 2 || binary.BigEndian.Uint32(r[1]["HLAserialNumber"]) != 1 || !bytes.Equal(r[1]["HLAreturnedArguments"], catalogInt(0)) {
		t.Fatal("serial restore or failure results")
	}
	if len(h.reports(t, mom.ClassReportException)) != 1 {
		t.Fatal("exception reporting toggle ignored")
	}
	before := h.m.ExportState(catalogFederation)
	if err := h.d.ReportServiceInvocation(context.Background(), catalogFederation, catalogTarget, mom.ServiceInvocation{Service: "RegisterObjectInstance", Success: true, Exception: "contradiction"}, h.fom, h.fom.(core.FOMHandleNameLookup)); err == nil {
		t.Fatal("invalid service outcome accepted")
	}
	if !reflect.DeepEqual(before, h.m.ExportState(catalogFederation)) {
		t.Fatal("invalid invocation consumed serial")
	}
}

func TestMOMServiceReportingAncestorSubscriptionRejected(t *testing.T) {
	h := newCatalog(t)
	sources := remainingSources(t)
	class, ok := h.fom.LookupInteractionClass("HLAmanager.HLAfederate.HLAreport")
	if !ok {
		t.Fatal("report ancestor")
	}
	sources.Subscriptions = func(context.Context, core.FederationName, core.FederateHandle) (mom.SubscriptionSnapshot, error) {
		return mom.SubscriptionSnapshot{Interactions: []mom.InteractionSubscription{{Class: class, Active: true}}}, nil
	}
	h.d.SetCatalogSources(sources)
	if err := h.request(t, mom.ClassSetServiceReporting, map[string][]byte{"HLAfederate": catalogHandle(uint64(catalogTarget)), "HLAreportingState": catalogInt(1)}); err == nil {
		t.Fatal("ancestor subscriber reporting enabled")
	}
}
