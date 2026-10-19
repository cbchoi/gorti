package mom_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"math"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/cbchoi/gorti/rti/internal/application/composition"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
	"github.com/cbchoi/gorti/rti/pkg/fom/mim"
)

const catalogFederation core.FederationName = "catalog"
const catalogTarget core.FederateHandle = 0x8122334481020304
const requestPublications = "HLAmanager.HLAfederate.HLArequest.HLArequestPublications"
const requestMIM = "HLAmanager.HLAfederation.HLArequest.HLArequestMIMdata"

var counterRequests = []string{
	mom.ClassRequestInteractionsSent, mom.ClassRequestInteractionsReceived,
	mom.ClassRequestUpdatesSent, mom.ClassRequestReflectionsReceived,
}

type catalogDelivery struct {
	fed       core.FederationName
	recipient core.FederateHandle
	event     core.OutboundEvent
}

type catalogOutbox struct{ sent []catalogDelivery }

func (o *catalogOutbox) Send(_ context.Context, fed core.FederationName, recipient core.FederateHandle, event core.OutboundEvent) error {
	o.sent = append(o.sent, catalogDelivery{fed, recipient, event})
	return nil
}

type catalogHarness struct {
	fom  core.FOMHandle
	d    *mom.Dispatcher
	m    *mom.Manager
	decl *declaration.Manager
	out  *catalogOutbox
}

func newCatalog(t *testing.T) *catalogHarness {
	t.Helper()
	repo := composition.NewFOMRepository()
	fom, err := repo.Load(context.Background(), []core.FOMModule{{XML: []byte(`<objectModel><objects><objectClass><name>HLAobjectRoot</name></objectClass></objects></objectModel>`)}})
	if err != nil {
		t.Fatal(err)
	}
	out := &catalogOutbox{}
	m, err := mom.New(mom.Options{Outbox: out})
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range []core.FederateHandle{7, catalogTarget} {
		if err := m.FederateJoined(context.Background(), catalogFederation, h, "participant", "test"); err != nil {
			t.Fatal(err)
		}
	}
	decl := declaration.New()
	// Reuse the production composition dependency already available to MOM.
	m.EnableInstanceFanout(nil, decl, repo)
	d := mom.NewDispatcher(m)
	d.SetEmitter(mom.NewProductionEmitter(out))
	return &catalogHarness{fom, d, m, decl, out}
}

func catalogHandle(h uint64) []byte {
	b := make([]byte, 12)
	binary.BigEndian.PutUint32(b, 8)
	binary.BigEndian.PutUint64(b[4:], h)
	return b
}

func (h *catalogHarness) request(t *testing.T, class string, values map[string][]byte) error {
	t.Helper()
	cls, ok := h.fom.LookupInteractionClass(class)
	if !ok {
		t.Fatalf("missing standard MIM class %s", class)
	}
	params := make(map[core.ParameterHandle][]byte, len(values))
	for name, value := range values {
		p, ok := h.fom.LookupParameter(cls, name)
		if !ok {
			t.Fatalf("missing parameter %s on %s", name, class)
		}
		params[p] = value
	}
	return h.d.Dispatch(context.Background(), catalogFederation, "HLAinteractionRoot."+class, 7, params, h.fom, h.fom.(core.FOMHandleNameLookup))
}

func (h *catalogHarness) reports(t *testing.T, class string) []map[string][]byte {
	t.Helper()
	cls, ok := h.fom.LookupInteractionClass(class)
	if !ok {
		t.Fatalf("missing report %s", class)
	}
	names := h.fom.(core.FOMHandleNameLookup)
	var reports []map[string][]byte
	for _, delivery := range h.out.sent {
		if delivery.fed != catalogFederation || delivery.recipient != 7 {
			t.Fatalf("misdirected report: %+v", delivery)
		}
		event, ok := delivery.event.(*mom.InteractionEvent)
		if !ok {
			t.Fatalf("unexpected event %T", delivery.event)
		}
		if event.Class != cls {
			continue
		}
		params := make(map[string][]byte)
		for p, value := range event.Parameters {
			name, ok := names.ParameterName(event.Class, p)
			if !ok {
				t.Fatalf("unresolved parameter handle %d", p)
			}
			params[name] = value
		}
		reports = append(reports, params)
	}
	return reports
}

func TestMOMCatalogRejectsMalformedTargets(t *testing.T) {
	for _, request := range append(append([]string{}, counterRequests...), requestPublications) {
		for name, value := range map[string][]byte{
			"omitted": nil, "empty": {}, "truncated": {0, 0, 0, 8},
			"raw32": {0, 0, 0, 7}, "raw64": {0, 0, 0, 0, 0, 0, 0, 7},
			"zero": catalogHandle(0), "unknown": catalogHandle(999),
			"trailing": append(catalogHandle(uint64(catalogTarget)), 0),
		} {
			t.Run(request+"/"+name, func(t *testing.T) {
				h := newCatalog(t)
				values := map[string][]byte{}
				if name != "omitted" {
					values["HLAfederate"] = value
				}
				if err := h.request(t, request, values); err == nil || !strings.Contains(err.Error(), "HLAfederate") {
					t.Fatalf("want target validation error, got %v", err)
				}
				if len(h.out.sent) != 0 {
					t.Fatal("invalid target emitted a report")
				}
			})
		}
	}
}

func TestMOMCatalogCounterProductionEncoding(t *testing.T) {
	for i, request := range counterRequests {
		t.Run(request, func(t *testing.T) {
			h := newCatalog(t)
			kinds := []mom.CounterKind{mom.InteractionsSent, mom.InteractionsReceived, mom.UpdatesSent, mom.ReflectionsReceived}
			calls := 0
			h.d.SetCounterSource(func(_ context.Context, fed core.FederationName, target core.FederateHandle, kind mom.CounterKind) ([]mom.TransportationCounts, error) {
				calls++
				if fed != catalogFederation || target != catalogTarget || kind != kinds[i] {
					t.Fatalf("incorrect source selection: %s %d %s", fed, target, kind)
				}
				return []mom.TransportationCounts{
					{Transportation: "HLAbestEffort", Counts: map[uint64]uint32{9: 0}},
					{Transportation: "HLAreliable", Counts: map[uint64]uint32{0x100000003: 0x01020304, 7: 2, 6: 0}},
				}, nil
			})
			if err := h.request(t, request, map[string][]byte{"HLAfederate": catalogHandle(uint64(catalogTarget))}); err != nil {
				t.Fatal(err)
			}
			if calls != 1 {
				t.Fatalf("source calls = %d, want 1", calls)
			}
			reportClass := strings.ReplaceAll(request, "HLArequest", "HLAreport")
			reports := h.reports(t, reportClass)
			if len(reports) != 2 {
				t.Fatalf("reports = %d, want 2", len(reports))
			}
			parameter := "HLAinteractionCounts"
			if i == 2 {
				parameter = "HLAupdateCounts"
			}
			if i == 3 {
				parameter = "HLAreflectCounts"
			}
			for _, report := range reports {
				if len(report) != 3 || !bytes.Equal(report["HLAfederate"], catalogHandle(uint64(catalogTarget))) {
					t.Fatalf("wrong report fields/target: %v", report)
				}
				transport := readCatalogString(t, report["HLAtransportation"])
				want := []byte{0, 0, 0, 0}
				switch transport {
				case "HLAreliable":
					// Independent fixed-record vector: array length, framed handle,
					// HLAinteger32BE count, then the next record without padding.
					want = []byte{0, 0, 0, 2, 0, 0, 0, 8, 0, 0, 0, 0, 0, 0, 0, 7, 0, 0, 0, 2, 0, 0, 0, 8, 0, 0, 0, 1, 0, 0, 0, 3, 1, 2, 3, 4}
				case "HLAbestEffort":
				default:
					t.Fatalf("unexpected transportation %q", transport)
				}
				if !bytes.Equal(report[parameter], want) {
					t.Fatalf("%s %s = % x, want % x", transport, parameter, report[parameter], want)
				}
			}
		})
	}
}

func readCatalogString(t *testing.T, b []byte) string {
	t.Helper()
	if len(b) < 4 || uint64(binary.BigEndian.Uint32(b))*2+4 != uint64(len(b)) {
		t.Fatalf("invalid Unicode string: % x", b)
	}
	units := make([]uint16, (len(b)-4)/2)
	for i := range units {
		units[i] = binary.BigEndian.Uint16(b[4+i*2:])
	}
	return string(utf16.Decode(units))
}

func TestMOMCatalogPublicationsProductionEncoding(t *testing.T) {
	h := newCatalog(t)
	ctx := context.Background()
	for _, a := range []core.AttributeHandle{0x100000009, 2} {
		if err := h.decl.PublishObjectClassAttributes(ctx, catalogFederation, catalogTarget, 0x100000003, []core.AttributeHandle{a}); err != nil {
			t.Fatal(err)
		}
	}
	if err := h.decl.PublishObjectClassAttributes(ctx, catalogFederation, catalogTarget, 5, []core.AttributeHandle{6}); err != nil {
		t.Fatal(err)
	}
	if err := h.decl.PublishObjectClassAttributes(ctx, catalogFederation, 7, 99, []core.AttributeHandle{1}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []core.InteractionClassHandle{0x100000005, 9} {
		if err := h.decl.PublishInteractionClass(ctx, catalogFederation, catalogTarget, c); err != nil {
			t.Fatal(err)
		}
	}
	if err := h.request(t, requestPublications, map[string][]byte{"HLAfederate": catalogHandle(uint64(catalogTarget))}); err != nil {
		t.Fatal(err)
	}
	objects := h.reports(t, "HLAmanager.HLAfederate.HLAreport.HLAreportObjectClassPublication")
	if len(objects) != 2 {
		t.Fatalf("object reports = %d, want 2", len(objects))
	}
	for i, class := range []uint64{5, 0x100000003} {
		if len(objects[i]) != 4 || !bytes.Equal(objects[i]["HLAfederate"], catalogHandle(uint64(catalogTarget))) || !bytes.Equal(objects[i]["HLAobjectClass"], catalogHandle(class)) || !bytes.Equal(objects[i]["HLAnumberOfClasses"], []byte{0, 0, 0, 2}) {
			t.Fatalf("bad object report: %v", objects[i])
		}
	}
	wantAttrs := append([]byte{0, 0, 0, 2}, catalogHandle(2)...)
	wantAttrs = append(wantAttrs, catalogHandle(0x100000009)...)
	if !bytes.Equal(objects[1]["HLAattributeList"], wantAttrs) {
		t.Fatalf("bad attribute list: % x", objects[1]["HLAattributeList"])
	}
	interactions := h.reports(t, "HLAmanager.HLAfederate.HLAreport.HLAreportInteractionPublication")
	wantClasses := append([]byte{0, 0, 0, 2}, catalogHandle(9)...)
	wantClasses = append(wantClasses, catalogHandle(0x100000005)...)
	if len(interactions) != 1 || len(interactions[0]) != 2 || !bytes.Equal(interactions[0]["HLAinteractionClassList"], wantClasses) || !bytes.Equal(interactions[0]["HLAfederate"], catalogHandle(uint64(catalogTarget))) {
		t.Fatalf("bad interaction publication: %v", interactions)
	}
}

func TestMOMCatalogPublicationsNullResponse(t *testing.T) {
	h := newCatalog(t)
	if err := h.request(t, requestPublications, map[string][]byte{"HLAfederate": catalogHandle(uint64(catalogTarget))}); err != nil {
		t.Fatal(err)
	}
	objects := h.reports(t, "HLAmanager.HLAfederate.HLAreport.HLAreportObjectClassPublication")
	if len(objects) != 1 || len(objects[0]) != 2 || !bytes.Equal(objects[0]["HLAnumberOfClasses"], []byte{0, 0, 0, 0}) {
		t.Fatalf("bad NULL object report: %v", objects)
	}
	interactions := h.reports(t, "HLAmanager.HLAfederate.HLAreport.HLAreportInteractionPublication")
	if len(interactions) != 1 || !bytes.Equal(interactions[0]["HLAinteractionClassList"], []byte{0, 0, 0, 0}) {
		t.Fatalf("bad NULL interaction report: %v", interactions)
	}
}

func TestMOMCatalogMIMDataProductionEncoding(t *testing.T) {
	h := newCatalog(t)
	if err := h.request(t, requestMIM, nil); err != nil {
		t.Fatal(err)
	}
	reports := h.reports(t, "HLAmanager.HLAfederation.HLAreport.HLAreportMIMdata")
	if len(reports) != 1 || len(reports[0]) != 1 {
		t.Fatalf("MIM reports: %v", reports)
	}
	b := reports[0]["HLAMIMdata"]
	want := utf16.Encode([]rune(string(mim.StandardMIMBytes())))
	if len(b) != 4+2*len(want) || binary.BigEndian.Uint32(b) != uint32(len(want)) {
		t.Fatal("bad MIM string framing")
	}
	for i, unit := range want {
		if binary.BigEndian.Uint16(b[4+2*i:]) != unit {
			t.Fatalf("MIM changed at UTF-16 unit %d", i)
		}
	}
}

func TestMOMCatalogEmitterMissingClassFails(t *testing.T) {
	h := newCatalog(t)
	err := mom.NewProductionEmitter(h.out)(context.Background(), catalogFederation, 7, mom.ResponseInteraction{ClassName: "HLAmanager.Absent"}, h.fom, h.fom.(core.FOMHandleNameLookup))
	if err == nil || len(h.out.sent) != 0 {
		t.Fatal("missing report class silently succeeded")
	}
}

func TestMOMCatalogUnavailableCounterData(t *testing.T) {
	for _, request := range counterRequests {
		t.Run(request, func(t *testing.T) {
			h := newCatalog(t)
			h.m.IncrementInteractionsSent(catalogFederation, catalogTarget)
			h.m.IncrementInteractionsReceived(catalogFederation, catalogTarget)
			h.m.IncrementUpdatesSent(catalogFederation, catalogTarget)
			h.m.IncrementReflectionsReceived(catalogFederation, catalogTarget)
			params := map[string][]byte{"HLAfederate": catalogHandle(uint64(catalogTarget))}
			if err := h.request(t, request, params); !errors.Is(err, mom.ErrCounterDataUnavailable) {
				t.Fatalf("missing source: %v", err)
			}
			failure := errors.New("counter source failed")
			h.d.SetCounterSource(func(context.Context, core.FederationName, core.FederateHandle, mom.CounterKind) ([]mom.TransportationCounts, error) {
				return nil, failure
			})
			if err := h.request(t, request, params); !errors.Is(err, failure) {
				t.Fatalf("source error lost: %v", err)
			}
			h.d.SetCounterSource(func(context.Context, core.FederationName, core.FederateHandle, mom.CounterKind) ([]mom.TransportationCounts, error) {
				return nil, nil
			})
			if err := h.request(t, request, params); !errors.Is(err, mom.ErrCounterDataUnavailable) {
				t.Fatalf("empty source: %v", err)
			}
			if len(h.out.sent) != 0 {
				t.Fatal("unavailable data produced fabricated counters")
			}
		})
	}
}

func TestMOMCatalogInvalidCounterSnapshotIsAtomic(t *testing.T) {
	for name, invalid := range map[string]mom.TransportationCounts{
		"emptyTransport":     {},
		"duplicateTransport": {Transportation: "HLAreliable"},
		"zeroClass":          {Transportation: "HLAbestEffort", Counts: map[uint64]uint32{0: 1}},
		"negativeWireCount":  {Transportation: "HLAbestEffort", Counts: map[uint64]uint32{7: math.MaxUint32}},
		"unknownTransport":   {Transportation: "not-in-supported-OMT-table"},
	} {
		t.Run(name, func(t *testing.T) {
			h := newCatalog(t)
			h.d.SetCounterSource(func(context.Context, core.FederationName, core.FederateHandle, mom.CounterKind) ([]mom.TransportationCounts, error) {
				return []mom.TransportationCounts{{Transportation: "HLAreliable", Counts: map[uint64]uint32{7: 3}}, invalid}, nil
			})
			if err := h.request(t, mom.ClassRequestInteractionsSent, map[string][]byte{"HLAfederate": catalogHandle(uint64(catalogTarget))}); err == nil {
				t.Fatal("invalid snapshot accepted")
			}
			if len(h.out.sent) != 0 {
				t.Fatal("partial report emitted for invalid source")
			}
		})
	}
}

func TestMOMCatalogTargetValidationPrecedesSource(t *testing.T) {
	h := newCatalog(t)
	if err := h.m.FederateJoined(context.Background(), "other-federation", 999, "foreign", "test"); err != nil {
		t.Fatal(err)
	}
	h.d.SetCounterSource(func(context.Context, core.FederationName, core.FederateHandle, mom.CounterKind) ([]mom.TransportationCounts, error) {
		t.Fatal("invalid target reached counter source")
		return nil, nil
	})
	for _, target := range [][]byte{catalogHandle(999), {0, 0, 0, 8}} {
		if err := h.request(t, mom.ClassRequestInteractionsSent, map[string][]byte{"HLAfederate": target}); err == nil || !strings.Contains(err.Error(), "HLAfederate") {
			t.Fatalf("invalid target: %v", err)
		}
	}
}

func TestMOMCatalogMissingEmitterFails(t *testing.T) {
	h := newCatalog(t)
	h.d.SetEmitter(nil)
	if err := h.request(t, requestMIM, nil); err == nil {
		t.Fatal("missing emitter silently discarded report")
	}
}

func TestMOMCatalogMissingPublicationStateFails(t *testing.T) {
	h := newCatalog(t)
	h.m.EnableInstanceFanout(nil, nil, nil)
	if err := h.request(t, requestPublications, map[string][]byte{"HLAfederate": catalogHandle(uint64(catalogTarget))}); err == nil {
		t.Fatal("missing publication state emitted NULL counts")
	}
	if len(h.out.sent) != 0 {
		t.Fatal("unavailable publications emitted report")
	}
}
