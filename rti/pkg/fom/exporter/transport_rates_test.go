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
	"github.com/cbchoi/gorti/rti/pkg/fom/model"
)

type deliveryDeclaration struct {
	Name      string  `xml:"name"`
	Reliable  string  `xml:"reliable"`
	Rate      string  `xml:"rate"`
	Semantics *string `xml:"semantics"`
}

type deliveryDocument struct {
	Transports []deliveryDeclaration `xml:"transportations>transportation"`
	Rates      []deliveryDeclaration `xml:"updateRates>updateRate"`
}

func deliveryValues(t *testing.T, data []byte) map[string]deliveryDeclaration {
	t.Helper()
	var doc deliveryDocument
	if err := xml.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	values := map[string]deliveryDeclaration{}
	for _, d := range doc.Transports {
		values["transport/"+d.Name] = d
	}
	for _, d := range doc.Rates {
		values["rate/"+d.Name] = d
	}
	return values
}

func deliverySource(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/transport_rates.xml")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestTransportRateDeclarationFidelity(t *testing.T) {
	source := deliverySource(t)
	output := marshal(t, parse(t, source))
	want, got := deliveryValues(t, source), deliveryValues(t, output)
	if !reflect.DeepEqual(want, got) {
		t.Errorf("transport/rate metadata changed: source=%s\nexport=%s", source, output)
	}
	if !bytes.Equal(output, marshal(t, parse(t, output))) {
		t.Error("second export changed")
	}
}

func TestTransportExportRejectsMissingDeclaration(t *testing.T) {
	source := string(deliverySource(t))
	start, end := strings.Index(source, "  <transportations>"), strings.Index(source, "  </transportations>")+len("  </transportations>")
	f := parse(t, []byte(source[:start]+source[end:]))
	data, err := exporter.Marshal(f)
	if data != nil || !errors.Is(err, exporter.ErrUnsupported) {
		t.Fatalf("invented declarations: output=%s err=%v", data, err)
	}
}

func TestIEEE2010UpdateRateExportRejectsNonpositiveModel(t *testing.T) {
	base := parse(t, deliverySource(t))
	for _, rate := range []float64{0, -0.25} {
		f := model.NewFOMWithUpdateRates(base.ObjectClasses(), base.InteractionClasses(), base.DataTypes(), base.Dimensions(), []model.UpdateRate{{Name: "Invalid", Rate: rate}}).
			WithTransportations(base.Transportations()).WithAdvisorySwitches(base.AdvisorySwitches())
		data, err := exporter.Marshal(f)
		if data != nil || !errors.Is(err, exporter.ErrUnsupported) || !strings.Contains(err.Error(), "greater than zero") {
			t.Fatalf("nonpositive model exported: %s %v", data, err)
		}
	}
}

func TestTransportExportRetainsSuppliedReliability(t *testing.T) {
	source := bytes.Replace(deliverySource(t), []byte("<reliable>Yes</reliable>"), []byte("<reliable>No</reliable>"), 1)
	got := deliveryValues(t, marshal(t, parse(t, source)))
	if got["transport/HLAreliable"].Reliable != "No" {
		t.Fatal("export inferred reliability from the name instead of preserving the declaration")
	}
}

func transportSnapshot(t *testing.T, f *model.FOM) *model.FOM {
	t.Helper()
	data, err := json.Marshal(struct {
		Transports []model.Transportation
		Rates      []model.UpdateRate
	}{f.Transportations(), f.UpdateRates()})
	if err != nil {
		t.Fatal(err)
	}
	var fresh struct {
		Transports []model.Transportation
		Rates      []model.UpdateRate
	}
	if err := json.Unmarshal(data, &fresh); err != nil {
		t.Fatal(err)
	}
	return model.NewFOMWithUpdateRates(f.ObjectClasses(), f.InteractionClasses(), f.DataTypes(), f.Dimensions(), fresh.Rates).
		WithAdvisorySwitches(f.AdvisorySwitches()).WithAutomaticResignAction(f.AutomaticResignAction()).WithTransportations(fresh.Transports)
}

func TestTransportRateJSONSnapshotExport(t *testing.T) {
	source := deliverySource(t)
	f := parse(t, source)
	if got := marshal(t, transportSnapshot(t, f)); !reflect.DeepEqual(deliveryValues(t, source), deliveryValues(t, got)) {
		t.Fatalf("fresh JSON reconstruction lost declarations: %s", got)
	}
}

func TestTransportDescriptionsAbsentVersusEmpty(t *testing.T) {
	for _, text := range []string{"", "<semantics/>", "<semantics> </semantics>"} {
		source := bytes.Replace(deliverySource(t), []byte("<semantics/>"), []byte(text), 1)
		got := marshal(t, transportSnapshot(t, parse(t, source)))
		if !reflect.DeepEqual(deliveryValues(t, source), deliveryValues(t, got)) {
			t.Fatalf("presence changed: %s", got)
		}
	}
}

func TestTransportExportRejectsInvalidRetainedDeclarations(t *testing.T) {
	f := parse(t, deliverySource(t))
	for _, declarations := range [][]model.Transportation{
		{{Name: "HLAreliable", Reliable: ""}},
		{{Name: "HLAreliable", Reliable: "true"}},
		{{Name: "Custom", Reliable: "Yes"}},
		{{Name: "HLAreliable", Reliable: "Yes"}, {Name: "HLAreliable", Reliable: "Yes"}},
		{{Name: "HLAreliable", Reliable: "Yes", Semantics: model.OptionalString{Value: "bad\x00", Present: true}}},
	} {
		data, err := exporter.Marshal(f.WithTransportations(declarations))
		if data != nil || !errors.Is(err, exporter.ErrUnsupported) {
			t.Fatalf("invalid transport export=%s err=%v", data, err)
		}
	}
}

func TestTransportRateOfficialSchemaOracle(t *testing.T) {
	xsd, python := os.Getenv("GORTI_FDD_XSD"), os.Getenv("GORTI_FDD_PYTHON")
	if xsd == "" || python == "" {
		t.Skip("set GORTI_FDD_XSD and GORTI_FDD_PYTHON for the independent IEEE XSD oracle")
	}
	f := parse(t, deliverySource(t))
	for _, item := range []struct {
		name string
		data []byte
	}{
		{"source", deliverySource(t)}, {"export", marshal(t, f)}, {"snapshot", marshal(t, transportSnapshot(t, f))},
	} {
		t.Run(item.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "transport.xml")
			if err := os.WriteFile(path, item.data, 0600); err != nil {
				t.Fatal(err)
			}
			output, err := exec.Command(python, "testdata/transport_rates_check.py", xsd, path).CombinedOutput()
			if err != nil {
				t.Fatalf("independent transport/rate oracle: %v\n%s", err, output)
			}
			t.Log(strings.TrimSpace(string(output)))
			if dir := os.Getenv("GORTI_FDD_OUTPUT_DIR"); dir != "" {
				if err := os.WriteFile(filepath.Join(dir, "ieee2010-batch3-r1-transport-"+item.name+".xml"), item.data, 0600); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
