package mom

import (
	"context"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"math"
	"os"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type serviceCatalogXMLClass struct {
	Name       string `xml:"name"`
	Parameters []struct {
		Name string `xml:"name"`
		Type string `xml:"dataType"`
	} `xml:"parameter"`
	Classes []serviceCatalogXMLClass `xml:"interactionClass"`
}

type serviceCatalogExecutor func(context.Context, core.FederationName, core.FederateHandle, MOMServiceCommand) (ServiceInvocation, error)

func (f serviceCatalogExecutor) ExecuteMOMService(ctx context.Context, fed core.FederationName, sender core.FederateHandle, c MOMServiceCommand) (ServiceInvocation, error) {
	return f(ctx, fed, sender, c)
}

func serviceCatalogCase(leaf serviceCatalogXMLClass) (*stubFOMHandle, map[core.ParameterHandle][]byte) {
	class := ServiceClassPrefix + leaf.Name
	fom := &stubFOMHandle{icByName: map[string]core.InteractionClassHandle{class: 1}, icByH: map[core.InteractionClassHandle]string{1: class}, paramByH: map[core.InteractionClassHandle]map[string]core.ParameterHandle{1: {"HLAfederate": 1}}}
	params := map[core.ParameterHandle][]byte{1: encodeHLAhandle(42)}
	for i, p := range leaf.Parameters {
		h := core.ParameterHandle(i + 2)
		fom.paramByH[1][p.Name] = h
		switch p.Type {
		case "HLAhandle":
			params[h] = encodeHLAhandle(7)
		case "HLAhandleList":
			params[h] = encodeFederateHandleList([]core.FederateHandle{3})
		case "HLAresignAction":
			params[h] = encodeHLAcount(6)
		case "HLAboolean":
			params[h] = boolEncoding(true)
		case "HLAorderType":
			params[h] = encodeHLAcount(0)
		case "HLAunicodeString":
			params[h] = encodeHLAunicodeString("label")
		case "HLAtransportationName":
			params[h] = encodeHLAunicodeString("HLAreliable")
		case "HLAopaqueData":
			params[h] = []byte{0, 0, 0, 0}
		case "HLAlogicalTime", "HLAtimeInterval":
			b := make([]byte, 12)
			binary.BigEndian.PutUint32(b, 8)
			binary.BigEndian.PutUint64(b[4:], math.Float64bits(2))
			params[h] = b
		}
	}
	return fom, params
}

func TestMOMServiceCatalogPositiveAndMalformedEachLeaf(t *testing.T) {
	for _, leaf := range standardServiceLeaves(t) {
		t.Run(leaf.Name, func(t *testing.T) {
			d := NewDispatcher(nil)
			fom, params := serviceCatalogCase(leaf)
			calls := 0
			d.SetServiceExecutor(serviceCatalogExecutor(func(_ context.Context, fed core.FederationName, sender core.FederateHandle, c MOMServiceCommand) (ServiceInvocation, error) {
				calls++
				if fed != "f" || sender != 9 || c.Target != 42 || c.Service != serviceName(leaf.Name) {
					t.Fatalf("wrong invocation identity: %+v", c)
				}
				return ServiceInvocation{Service: c.Service, Success: true}, nil
			}))
			var reports []ResponseInteraction
			d.SetEmitter(func(_ context.Context, _ core.FederationName, _ core.FederateHandle, r ResponseInteraction, _ core.FOMHandle, _ core.FOMHandleNameLookup) error {
				reports = append(reports, r)
				return nil
			})
			if err := d.Dispatch(context.Background(), "f", ServiceClassPrefix+leaf.Name, 9, params, fom, fom); err != nil || calls != 1 {
				t.Fatalf("positive: calls=%d error=%v", calls, err)
			}
			params[1] = []byte{0}
			if err := d.Dispatch(context.Background(), "f", ServiceClassPrefix+leaf.Name, 9, params, fom, fom); err == nil || calls != 1 {
				t.Fatalf("malformed target reached executor: calls=%d error=%v", calls, err)
			}
			if len(reports) != 1 || reports[0].ClassName != ClassReportMOMException || binary.BigEndian.Uint32(reports[0].Params["HLAparameterError"]) != 1 {
				t.Fatalf("missing parameter exception report: %+v", reports)
			}
		})
	}
}

func TestMOMServiceCatalogMissingMandatoryAndExecutorFailure(t *testing.T) {
	for _, definition := range serviceDefinitions {
		for _, name := range definition.required {
			t.Run(definition.leaf+"/"+name, func(t *testing.T) {
				var leaf serviceCatalogXMLClass
				for _, candidate := range standardServiceLeaves(t) {
					if candidate.Name == definition.leaf {
						leaf = candidate
					}
				}
				fom, params := serviceCatalogCase(leaf)
				delete(params, fom.paramByH[1][name])
				d := NewDispatcher(nil)
				calls, reports := 0, 0
				d.SetServiceExecutor(serviceCatalogExecutor(func(context.Context, core.FederationName, core.FederateHandle, MOMServiceCommand) (ServiceInvocation, error) {
					calls++
					return ServiceInvocation{}, nil
				}))
				d.SetEmitter(func(context.Context, core.FederationName, core.FederateHandle, ResponseInteraction, core.FOMHandle, core.FOMHandleNameLookup) error {
					reports++
					return nil
				})
				if err := d.Dispatch(context.Background(), "f", ServiceClassPrefix+definition.leaf, 9, params, fom, fom); err == nil || calls != 0 || reports != 1 {
					t.Fatalf("missing required parameter: calls=%d reports=%d error=%v", calls, reports, err)
				}
			})
		}
	}
	for _, leaf := range standardServiceLeaves(t) {
		t.Run(leaf.Name+"/service-failure", func(t *testing.T) {
			fom, params := serviceCatalogCase(leaf)
			d := NewDispatcher(nil)
			cause := errors.New("actual service precondition failed")
			d.SetServiceExecutor(serviceCatalogExecutor(func(context.Context, core.FederationName, core.FederateHandle, MOMServiceCommand) (ServiceInvocation, error) {
				return ServiceInvocation{}, cause
			}))
			var report ResponseInteraction
			d.SetEmitter(func(_ context.Context, _ core.FederationName, _ core.FederateHandle, r ResponseInteraction, _ core.FOMHandle, _ core.FOMHandleNameLookup) error {
				report = r
				return nil
			})
			if err := d.Dispatch(context.Background(), "f", ServiceClassPrefix+leaf.Name, 9, params, fom, fom); !errors.Is(err, cause) {
				t.Fatal(err)
			}
			if report.ClassName != ClassReportMOMException || binary.BigEndian.Uint32(report.Params["HLAparameterError"]) != 0 {
				t.Fatal("service exception mistaken for parameter error")
			}
		})
	}
}

func TestMOMServiceCatalogOptionalAttributePresence(t *testing.T) {
	for _, operation := range []string{"HLAunpublishObjectClassAttributes", "HLAunsubscribeObjectClassAttributes"} {
		var leaf serviceCatalogXMLClass
		for _, c := range standardServiceLeaves(t) {
			if c.Name == operation {
				leaf = c
			}
		}
		for _, present := range []bool{false, true} {
			fom, params := serviceCatalogCase(leaf)
			h := fom.paramByH[1]["HLAattributeList"]
			if present {
				params[h] = []byte{0, 0, 0, 0}
			} else {
				delete(params, h)
			}
			d := NewDispatcher(nil)
			d.SetServiceExecutor(serviceCatalogExecutor(func(_ context.Context, _ core.FederationName, _ core.FederateHandle, c MOMServiceCommand) (ServiceInvocation, error) {
				if c.AttributesPresent != present || len(c.Attributes) != 0 {
					t.Fatalf("optional set lost presence: %+v", c)
				}
				return ServiceInvocation{}, nil
			}))
			if err := d.Dispatch(context.Background(), "f", ServiceClassPrefix+operation, 9, params, fom, fom); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestMOMServiceCatalogPresentNilOptionalArgumentIsNotOmitted(t *testing.T) {
	for _, leaf := range standardServiceLeaves(t) {
		if leaf.Name != "HLAunpublishObjectClassAttributes" && leaf.Name != "HLAunsubscribeObjectClassAttributes" {
			continue
		}
		t.Run(leaf.Name, func(t *testing.T) {
			fom, params := serviceCatalogCase(leaf)
			params[fom.paramByH[1]["HLAattributeList"]] = nil
			d := NewDispatcher(nil)
			calls := 0
			d.SetServiceExecutor(serviceCatalogExecutor(func(context.Context, core.FederationName, core.FederateHandle, MOMServiceCommand) (ServiceInvocation, error) {
				calls++
				return ServiceInvocation{}, nil
			}))
			d.SetEmitter(func(context.Context, core.FederationName, core.FederateHandle, ResponseInteraction, core.FOMHandle, core.FOMHandleNameLookup) error {
				return nil
			})
			if err := d.Dispatch(context.Background(), "f", ServiceClassPrefix+leaf.Name, 9, params, fom, fom); err == nil || calls != 0 {
				t.Fatal("explicit nil attribute list became whole-class operation")
			}
		})
	}
}

type serviceExtensionFOM struct{ *stubFOMHandle }

func (f *serviceExtensionFOM) ObjectClassAncestors(core.ObjectClassHandle) []core.ObjectClassHandle {
	return nil
}
func (f *serviceExtensionFOM) InteractionClassAncestors(h core.InteractionClassHandle) []core.InteractionClassHandle {
	if h == 2 {
		return []core.InteractionClassHandle{2, 1}
	}
	return []core.InteractionClassHandle{h}
}
func (f *serviceExtensionFOM) ParameterName(cls core.InteractionClassHandle, h core.ParameterHandle) (string, bool) {
	for n, candidate := range f.paramByH[cls] {
		if candidate == h {
			return n, true
		}
	}
	return "", false
}

func TestMOMServiceCatalogValidSubclassAndExtensionParameters(t *testing.T) {
	leaf := ServiceClassPrefix + "HLAenableAsynchronousDelivery"
	child := leaf + ".TeachingExtension"
	f := &serviceExtensionFOM{&stubFOMHandle{icByName: map[string]core.InteractionClassHandle{leaf: 1, child: 2}, paramByH: map[core.InteractionClassHandle]map[string]core.ParameterHandle{1: {"HLAfederate": 1, "extra": 9}, 2: {"HLAfederate": 101, "extra": 102}}}}
	d := NewDispatcher(nil)
	calls := 0
	d.SetServiceExecutor(serviceCatalogExecutor(func(_ context.Context, _ core.FederationName, _ core.FederateHandle, c MOMServiceCommand) (ServiceInvocation, error) {
		calls++
		if c.Target != 42 {
			t.Fatalf("subclass numeric parameter handles not projected: %+v", c)
		}
		return ServiceInvocation{}, nil
	}))
	if err := d.Dispatch(context.Background(), "f", child, 9, map[core.ParameterHandle][]byte{101: encodeHLAhandle(42), 102: {1, 2}}, f, f); err != nil {
		t.Fatal(err)
	}
	if err := d.Dispatch(context.Background(), "f", leaf, 9, map[core.ParameterHandle][]byte{1: encodeHLAhandle(42), 9: {1, 2}}, f, f); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("extension execution count=%d", calls)
	}
	if d.IsManagerClass("HLAmanager.UserManagement") {
		t.Fatal("unrelated manager class would be swallowed instead of normal fanout")
	}
	if _, ok := d.Lookup(ServiceClassPrefix + "HLAenableAsynchronousDeliveryExtra"); ok {
		t.Fatal("lexically similar unrelated class matched a leaf")
	}
}

func TestMOMServiceCatalogRejectsUndefinedExtensionParameterWithoutExecution(t *testing.T) {
	leaf := ServiceClassPrefix + "HLAenableAsynchronousDelivery"
	child := leaf + ".TeachingExtension"
	f := &serviceExtensionFOM{&stubFOMHandle{
		icByName: map[string]core.InteractionClassHandle{leaf: 1, child: 2},
		paramByH: map[core.InteractionClassHandle]map[string]core.ParameterHandle{
			1: {"HLAfederate": 1, "extra": 9},
			2: {"HLAfederate": 101, "extra": 102},
		},
	}}
	for _, tc := range []struct {
		class         string
		target, extra core.ParameterHandle
	}{{leaf, 1, 9}, {child, 101, 102}} {
		t.Run(tc.class, func(t *testing.T) {
			d := NewDispatcher(nil)
			calls := 0
			d.SetServiceExecutor(serviceCatalogExecutor(func(context.Context, core.FederationName, core.FederateHandle, MOMServiceCommand) (ServiceInvocation, error) {
				calls++
				return ServiceInvocation{}, nil
			}))
			params := map[core.ParameterHandle][]byte{tc.target: encodeHLAhandle(42), tc.extra: {1, 2}, 999: {3}}
			if err := d.Dispatch(context.Background(), "f", tc.class, 9, params, f, f); err == nil {
				t.Fatal("undefined parameter 999 was ignored as a valid extension")
			}
			if calls != 0 {
				t.Fatal("invalid parameter caused a service side effect")
			}
			delete(params, 999)
			if err := d.Dispatch(context.Background(), "f", tc.class, 9, params, f, f); err != nil || calls != 1 {
				t.Fatalf("FOM-defined extension must be ignored: calls=%d error=%v", calls, err)
			}
		})
	}
}

func standardServiceLeaves(t *testing.T) []serviceCatalogXMLClass {
	t.Helper()
	b, err := os.ReadFile("../../../pkg/fom/mim/standard-mim.xml")
	if err != nil {
		t.Fatal(err)
	}
	var model struct {
		Interactions struct {
			Classes []serviceCatalogXMLClass `xml:"interactionClass"`
		} `xml:"interactions"`
	}
	if err := xml.Unmarshal(b, &model); err != nil {
		t.Fatal(err)
	}
	var leaves []serviceCatalogXMLClass
	var visit func(serviceCatalogXMLClass)
	visit = func(c serviceCatalogXMLClass) {
		if c.Name == "HLAservice" {
			leaves = append(leaves, c.Classes...)
		}
		for _, child := range c.Classes {
			visit(child)
		}
	}
	for _, c := range model.Interactions.Classes {
		visit(c)
	}
	if len(leaves) != 32 {
		t.Fatalf("standard MIM service leaves = %d", len(leaves))
	}
	return leaves
}

func TestMOMServiceCatalogRegistersAllStandardLeaves(t *testing.T) {
	d := NewDispatcher(nil)
	for _, leaf := range standardServiceLeaves(t) {
		t.Run(leaf.Name, func(t *testing.T) {
			if _, ok := d.Lookup("HLAmanager.HLAfederate.HLAservice." + leaf.Name); !ok {
				t.Fatal("standard service leaf has no executor handler")
			}
		})
	}
}
