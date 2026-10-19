package exporter_test

import (
	"bytes"
	"encoding/xml"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/cbchoi/gorti/rti/pkg/fom/exporter"
	"github.com/cbchoi/gorti/rti/pkg/fom/mim"
	"github.com/cbchoi/gorti/rti/pkg/fom/model"
	"github.com/cbchoi/gorti/rti/pkg/fom/parser"
)

func parsedFixture(t *testing.T) *model.FOM {
	t.Helper()
	data, err := os.ReadFile("testdata/retained.xml")
	if err != nil {
		t.Fatal(err)
	}
	return parse(t, data)
}

func parse(t *testing.T, data []byte) *model.FOM {
	t.Helper()
	result, err := parser.Parse([]parser.Module{{Path: "roundtrip.xml", XML: data}})
	if err != nil || len(result.Diagnostics) != 0 {
		t.Fatalf("parse: %v %+v", err, result.Diagnostics)
	}
	return result.FOM.(*model.FOM)
}

func marshal(t *testing.T, f *model.FOM) []byte {
	t.Helper()
	data, err := exporter.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestRetainedModelSemanticRoundTrip(t *testing.T) {
	f := parsedFixture(t)
	data := marshal(t, f)
	got := parse(t, data)
	objects, gotObjects := f.ObjectClasses(), got.ObjectClasses()
	sort.Slice(objects, func(i, j int) bool { return objects[i].Key() < objects[j].Key() })
	sort.Slice(gotObjects, func(i, j int) bool { return gotObjects[i].Key() < gotObjects[j].Key() })
	if !reflect.DeepEqual(objects, gotObjects) || !reflect.DeepEqual(f.InteractionClasses(), got.InteractionClasses()) || !reflect.DeepEqual(f.DataTypes(), got.DataTypes()) || !reflect.DeepEqual(f.Dimensions(), got.Dimensions()) || !reflect.DeepEqual(f.UpdateRates(), got.UpdateRates()) || !reflect.DeepEqual(f.AdvisorySwitches(), got.AdvisorySwitches()) || f.AutomaticResignAction() != got.AutomaticResignAction() {
		t.Fatal("retained model changed after export/parse round trip")
	}
	if !bytes.Equal(data, marshal(t, got)) {
		t.Fatal("canonical output changed on second round trip")
	}
	if !bytes.Contains(data, []byte("1.0000000000000000000000000001")) {
		t.Fatal("exact decimal rounded")
	}
	if bytes.Contains(data, []byte("<values>")) {
		t.Fatal("legacy plural enumerator emitted")
	}
}

// This decoder is independent of both the production exporter and FOM parser.
type document struct {
	XMLName      xml.Name
	Objects      []objectNode      `xml:"objects>objectClass"`
	Interactions []interactionNode `xml:"interactions>interactionClass"`
	Enums        []struct {
		Name   string `xml:"name"`
		Values []struct {
			Name   string   `xml:"name"`
			Values []string `xml:"value"`
		} `xml:"enumerator"`
	} `xml:"dataTypes>enumeratedDataTypes>enumeratedData"`
}
type objectNode struct {
	Name       string `xml:"name"`
	Attributes []struct {
		Name string `xml:"name"`
	} `xml:"attribute"`
	Children []objectNode `xml:"objectClass"`
}
type interactionNode struct {
	Name       string `xml:"name"`
	Parameters []struct {
		Name string `xml:"name"`
	} `xml:"parameter"`
	Children []interactionNode `xml:"interactionClass"`
}

func TestIndependentMergedMIMXMLAndInheritance(t *testing.T) {
	base, err := mim.StandardMIMHandle()
	if err != nil {
		t.Fatal(err)
	}
	f, diagnostics := mim.Merge(base, parsedFixture(t))
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	data := marshal(t, f)
	var got document
	if err := xml.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.XMLName.Space != exporter.Namespace || got.XMLName.Local != "objectModel" || len(got.Objects) != 1 || len(got.Interactions) != 1 {
		t.Fatalf("invalid FDD root: %+v", got.XMLName)
	}
	classes := map[string][]string{}
	var walk func(objectNode, string)
	walk = func(c objectNode, parent string) {
		path := c.Name
		if parent != "" {
			path = parent + "." + c.Name
		}
		classes[path] = nil
		for _, a := range c.Attributes {
			classes[path] = append(classes[path], a.Name)
		}
		for _, child := range c.Children {
			walk(child, path)
		}
	}
	walk(got.Objects[0], "")
	if !reflect.DeepEqual(classes["HLAobjectRoot.Parent"], []string{"Inherited"}) || !reflect.DeepEqual(classes["HLAobjectRoot.Parent.Leaf"], []string{"Local"}) {
		t.Fatal("local/inherited object declarations changed", classes)
	}
	if _, ok := classes["HLAobjectRoot.Other.Leaf"]; !ok {
		t.Fatal("repeated local class name collapsed")
	}
	if _, ok := classes["HLAobjectRoot.HLAmanager.HLAfederation"]; !ok {
		t.Fatal("merged MIM missing")
	}
	for _, e := range got.Enums {
		if e.Name == "HLAboolean" {
			for _, v := range e.Values {
				if v.Name == "HLAtrue" && reflect.DeepEqual(v.Values, []string{"1"}) {
					return
				}
			}
		}
	}
	t.Fatal("MIM enum semantics missing from independent decoder")
}

func TestExportDeterministicConcurrentAndDoesNotMutate(t *testing.T) {
	f := parsedFixture(t)
	want := marshal(t, f)
	objects, interactions, types := f.ObjectClasses(), f.InteractionClasses(), f.DataTypes()
	for i, j := 0, len(objects)-1; i < j; i, j = i+1, j-1 {
		objects[i], objects[j] = objects[j], objects[i]
	}
	shuffled := model.NewFOMWithUpdateRates(objects, interactions, types, f.Dimensions(), f.UpdateRates()).WithAdvisorySwitches(f.AdvisorySwitches()).WithAutomaticResignAction(f.AutomaticResignAction()).WithTransportations(f.Transportations())
	if !bytes.Equal(want, marshal(t, shuffled)) {
		t.Fatal("input ordering affects output")
	}
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			data, err := exporter.Marshal(f)
			if err != nil || !bytes.Equal(data, want) {
				t.Errorf("concurrent export changed: %v", err)
			}
		}()
	}
	wg.Wait()
	if !reflect.DeepEqual(f, parsedFixture(t)) {
		t.Fatal("export mutated input")
	}
}

func TestExportRejectsUnsupportedWithoutPartialDocument(t *testing.T) {
	for _, name := range []string{"nil", "missing-switch", "unknown-switch", "duplicate-switch", "unknown-transport", "missing-order", "orphan", "duplicate-class", "undefined-type", "undefined-dimension", "conflicting-type", "invalid-text", "invalid-name", "mutated-rate", "invalid-sharing", "invalid-cardinality", "cycle", "nil-type"} {
		t.Run(name, func(t *testing.T) {
			f := parsedFixture(t)
			objects, interactions, types, rates := f.ObjectClasses(), f.InteractionClasses(), f.DataTypes(), f.UpdateRates()
			for i := range objects {
				if objects[i].Name == "Parent" {
					switch name {
					case "unknown-transport":
						objects[i].Attributes[0].Transportation = "PrivateTransport"
					case "missing-order":
						objects[i].Attributes[0].Order = ""
					case "undefined-type":
						objects[i].Attributes[0].DataType = "Unknown"
					case "undefined-dimension":
						objects[i].Attributes[0].Dimensions = []string{"Unknown"}
					case "orphan":
						objects[i].QualifiedName = "Missing.Parent"
						objects[i].ParentName = "Missing"
					case "duplicate-class":
						objects = append(objects, objects[i])
					case "invalid-name":
						objects[i].Attributes[0].Name = "bad:name"
					case "invalid-sharing":
						objects[i].Attributes[0].Sharing = "Publish|Subscribe"
					}
				}
			}
			switch name {
			case "conflicting-type":
				types = append(types, &model.BasicData{NameField: "Scalar", Size: 64})
			case "invalid-text":
				rates[0].Semantics = "invalid\x00text"
			case "mutated-rate":
				rates[0].Rate = 42
			case "invalid-cardinality":
				types = append(types, &model.ArrayData{NameField: "BadArray", DataType: "Scalar", Cardinality: "unbounded"})
			case "cycle":
				objects = append(objects, model.ObjectClass{Name: "CycleA", ParentName: "CycleB"}, model.ObjectClass{Name: "CycleB", ParentName: "CycleA"})
			}
			f = model.NewFOMWithUpdateRates(objects, interactions, types, f.Dimensions(), rates).WithAdvisorySwitches(f.AdvisorySwitches()).WithAutomaticResignAction(f.AutomaticResignAction()).WithTransportations(f.Transportations())
			switch name {
			case "nil":
				f = nil
			case "missing-switch":
				f = f.WithAdvisorySwitches(nil)
			case "unknown-switch":
				f = f.WithAdvisorySwitches(append(f.AdvisorySwitches(), model.AdvisorySwitch{Name: "futureSwitch"}))
			case "duplicate-switch":
				f = f.WithAdvisorySwitches(append(f.AdvisorySwitches(), f.AdvisorySwitches()[0]))
			case "nil-type":
				f = model.NewFOM(nil, nil, []model.DataType{(*model.BasicData)(nil)})
			}
			data, err := exporter.Marshal(f)
			if !errors.Is(err, exporter.ErrUnsupported) || data != nil {
				t.Fatalf("got %d document bytes, err=%v", len(data), err)
			}
		})
	}
}

func TestOfficialFDDSchemaIndependentOracle(t *testing.T) {
	xsd, python := os.Getenv("GORTI_FDD_XSD"), os.Getenv("GORTI_FDD_PYTHON")
	if xsd == "" || python == "" {
		t.Skip("set GORTI_FDD_XSD and GORTI_FDD_PYTHON for the external IEEE XSD oracle")
	}
	f := parsedFixture(t)
	base, err := mim.StandardMIMHandle()
	if err != nil {
		t.Fatal(err)
	}
	merged, diags := mim.Merge(base, f)
	if len(diags) != 0 {
		t.Fatal(diags)
	}
	for _, item := range []struct {
		name string
		fom  *model.FOM
	}{{"retained", f}, {"merged", merged}} {
		t.Run(item.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), item.name+".xml")
			data := marshal(t, item.fom)
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(python, "testdata/schema_check.py", xsd, path, item.name)
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("independent schema/semantic oracle: %v\n%s", err, output)
			} else {
				t.Log(strings.TrimSpace(string(output)))
			}
			if dir := os.Getenv("GORTI_FDD_OUTPUT_DIR"); dir != "" {
				if err := os.WriteFile(filepath.Join(dir, item.name+".xml"), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
