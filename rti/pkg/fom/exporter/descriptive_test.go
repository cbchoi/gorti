package exporter_test

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cbchoi/gorti/rti/pkg/fom/exporter"
	"github.com/cbchoi/gorti/rti/pkg/fom/mim"
	"github.com/cbchoi/gorti/rti/pkg/fom/model"
	"github.com/cbchoi/gorti/rti/pkg/fom/parser"
)

// This independent XML tree records element presence as well as text. Names
// identify declarations without depending on production flattening or ordering.
type descriptiveNode struct {
	XMLName  xml.Name
	Text     string            `xml:",chardata"`
	Children []descriptiveNode `xml:",any"`
}

// This legacy fixture omits transport declarations. Supply actual selected
// MIM declarations explicitly; the exporter must not infer them from references.
func descriptiveFixture(t *testing.T, source []byte) *model.FOM {
	t.Helper()
	base, err := mim.StandardMIMHandle()
	if err != nil {
		t.Fatal(err)
	}
	return parse(t, source).WithTransportations(base.Transportations())
}

func TestDescriptiveMetadataOfficialSchemaOracle(t *testing.T) {
	xsd, python := os.Getenv("GORTI_FDD_XSD"), os.Getenv("GORTI_FDD_PYTHON")
	if xsd == "" || python == "" {
		t.Skip("set GORTI_FDD_XSD and GORTI_FDD_PYTHON for the external IEEE XSD oracle")
	}
	source, err := os.ReadFile("testdata/descriptive.xml")
	if err != nil {
		t.Fatal(err)
	}
	f := descriptiveFixture(t, source)
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
	}{{"descriptive", f}, {"merged", merged}} {
		t.Run(item.name, func(t *testing.T) {
			data := marshal(t, item.fom)
			path := filepath.Join(t.TempDir(), item.name+".xml")
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(python, "testdata/descriptive_check.py", xsd, path, item.name)
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("independent R1 oracle: %v\n%s", err, output)
			}
			t.Log(strings.TrimSpace(string(output)))
			if dir := os.Getenv("GORTI_FDD_OUTPUT_DIR"); dir != "" {
				if err := os.WriteFile(filepath.Join(dir, "r1-"+item.name+".xml"), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestDescriptiveMetadataDatatypeConflictsAtExport(t *testing.T) {
	source, err := os.ReadFile("testdata/descriptive.xml")
	if err != nil {
		t.Fatal(err)
	}
	for _, original := range []string{"Measured distance", "Ordered observations", "Record description", "  all samples  ", "Variant description", "Record alternative"} {
		t.Run(strings.TrimSpace(original), func(t *testing.T) {
			other := strings.Replace(string(source), original, "different", 1)
			result, err := parser.Parse([]parser.Module{{XML: source}, {XML: []byte(other)}})
			if err != nil || len(result.Diagnostics) != 0 {
				t.Fatalf("parse: %v %+v", err, result.Diagnostics)
			}
			data, err := exporter.Marshal(result.FOM.(*model.FOM))
			if data != nil || !errors.Is(err, exporter.ErrUnsupported) || !strings.Contains(err.Error(), "conflicting datatype") {
				t.Fatalf("datatype conflict lost: %s %v", data, err)
			}
		})
	}
}

func descriptiveValues(t *testing.T, data []byte) map[string]string {
	t.Helper()
	var root descriptiveNode
	if err := xml.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	values := map[string]string{}
	var walk func(descriptiveNode, string, string)
	walk = func(n descriptiveNode, path, parent string) {
		path += "/" + n.XMLName.Local
		for _, child := range n.Children {
			if child.XMLName.Local == "name" {
				path += "[" + child.Text + "]"
				break
			}
		}
		name := n.XMLName.Local
		described := parent == "objectClass" || parent == "attribute" || parent == "interactionClass" || parent == "parameter" || parent == "simpleData" || parent == "enumeratedData" || parent == "arrayData" || parent == "fixedRecordData" || parent == "field" || parent == "variantRecordData" || parent == "alternative"
		if name == "semantics" && described || name == "updateCondition" && parent == "attribute" || parent == "dimension" && (name == "dataType" || name == "normalization" || name == "value") {
			values[path] = n.Text
		}
		for _, child := range n.Children {
			walk(child, path, name)
		}
	}
	walk(root, "", "")
	return values
}

func requireDescriptions(t *testing.T, source, output []byte) {
	t.Helper()
	want, got := descriptiveValues(t, source), descriptiveValues(t, output)
	if len(want) == 0 {
		t.Fatal("empty oracle")
	}
	for path, value := range want {
		actual, present := got[path]
		if !present || actual != value {
			t.Errorf("%s: got %q (present=%v), want %q", path, actual, present, value)
		}
	}
}

func TestDescriptiveMetadataRoundTripAndSnapshot(t *testing.T) {
	source, err := os.ReadFile("testdata/descriptive.xml")
	if err != nil {
		t.Fatal(err)
	}
	f := descriptiveFixture(t, source)
	data := marshal(t, f)
	requireDescriptions(t, source, data)
	if bytes.Contains(data, []byte("<defaultValue>")) {
		t.Fatal("invented dimension vocabulary")
	}
	roundtrip := marshal(t, parse(t, data))
	if !bytes.Equal(data, roundtrip) {
		t.Fatal("semantic roundtrip changed export")
	}

	objects, interactions, dimensions := f.ObjectClasses(), f.InteractionClasses(), f.Dimensions()
	for _, value := range []any{&objects, &interactions, &dimensions} {
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		reflect.ValueOf(value).Elem().Set(reflect.Zero(reflect.TypeOf(value).Elem()))
		if err := json.Unmarshal(encoded, value); err != nil {
			t.Fatal(err)
		}
	}
	types := f.DataTypes()
	for i, dt := range types {
		encoded, err := json.Marshal(dt)
		if err != nil {
			t.Fatal(err)
		}
		decoded := reflect.New(reflect.TypeOf(dt).Elem()).Interface()
		if err := json.Unmarshal(encoded, decoded); err != nil {
			t.Fatal(err)
		}
		types[i] = decoded.(model.DataType)
	}
	restored := model.NewFOMWithDimensions(objects, interactions, types, dimensions).WithAdvisorySwitches(f.AdvisorySwitches()).WithTransportations(f.Transportations())
	requireDescriptions(t, source, marshal(t, restored))

	base, err := mim.StandardMIMHandle()
	if err != nil {
		t.Fatal(err)
	}
	merged, diags := mim.Merge(base, restored)
	if len(diags) != 0 {
		t.Fatal(diags)
	}
	output := marshal(t, merged)
	requireDescriptions(t, source, output)
	// The XML source retains the Annex G spelling; effective FDD uses Table 6.
	expectedMIM := bytes.Replace(mim.StandardMIMBytes(), []byte("<name>HLAnextSaveName</name>"), []byte("<name>HLAAnnexSaveName</name>"), 1)
	requireDescriptions(t, expectedMIM, output)
}
