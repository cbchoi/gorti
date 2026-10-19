package parser

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cbchoi/gorti/rti/pkg/fom/mim"
	"github.com/cbchoi/gorti/rti/pkg/fom/model"
)

const dimensionDeclarations = `<dimensions><dimension><name>X</name><upperBound>100</upperBound></dimension><dimension><name>Y</name><upperBound>200</upperBound></dimension></dimensions>`

// IEEE1516-FDD-2010.xsd attributeType and interactionClassType use
// dimensions/dimension text references, not dimensionRef or nested names.
func dimensionModule(kind, refs, declarations string) Module {
	content := `<objects><objectClass><name>HLAobjectRoot</name><objectClass><name>Parent</name><attribute><name>Position</name><dataType>HLAinteger32BE</dataType>` + refs + `</attribute></objectClass></objectClass></objects>`
	if kind == interactionKind {
		content = `<interactions><interactionClass><name>HLAinteractionRoot</name><interactionClass><name>Signal</name><sharing>PublishSubscribe</sharing>` + refs + `</interactionClass></interactionClass></interactions>`
	}
	return Module{Path: kind + ".xml", XML: []byte(`<objectModel xmlns="http://standards.ieee.org/IEEE1516-2010">` + content + declarations + `</objectModel>`)}
}

func requireDimensionFOM(t *testing.T, modules ...Module) *model.FOM {
	t.Helper()
	res, err := Parse(modules)
	if err != nil || len(res.Diagnostics) != 0 || res.FOM == nil {
		t.Fatalf("Parse: %v, diagnostics: %+v", err, res.Diagnostics)
	}
	return res.FOM.(*model.FOM)
}

func dimensionReferences(f *model.FOM, kind string) []string {
	if kind == interactionKind {
		for _, c := range f.InteractionClasses() {
			if c.Name == "Signal" {
				return c.Dimensions
			}
		}
	}
	for _, c := range f.ObjectClasses() {
		if c.Name == "Parent" {
			return c.Attributes[0].Dimensions
		}
	}
	return nil
}

func TestDimensionReferencesOfficialXML(t *testing.T) {
	for _, kind := range []string{attributeElement, interactionKind} {
		for _, tc := range []struct {
			name, xml string
			want      []string
		}{
			{"multiple", `<dimensions><dimension notes="n1"> Y </dimension><dimension>X</dimension></dimensions>`, []string{"Y", "X"}},
			{"empty", `<dimensions/>`, nil},
			{"omitted", ``, nil},
		} {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				f := requireDimensionFOM(t, dimensionModule(kind, tc.xml, dimensionDeclarations))
				if got := dimensionReferences(f, kind); !reflect.DeepEqual(got, tc.want) {
					t.Fatalf("dimensions = %v, want %v", got, tc.want)
				}
			})
		}
	}
}

func TestDimensionReferencesInheritedAttributeAndModularCopies(t *testing.T) {
	parent := dimensionModule(attributeElement, `<dimensions><dimension>Y</dimension><dimension>X</dimension></dimensions>`, "")
	child := Module{Path: "child.xml", XML: []byte(`<objectModel><objects><objectClass><name>HLAobjectRoot</name><objectClass><name>Parent</name><objectClass><name>Child</name></objectClass></objectClass></objectClass></objects>` + dimensionDeclarations + `</objectModel>`)}
	for n, modules := range [][]Module{{parent, child, parent}, {child, parent, parent}} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			f := requireDimensionFOM(t, modules...)
			byKey := map[string]model.ObjectClass{}
			for _, c := range f.ObjectClasses() {
				byKey[c.Key()] = c
			}
			c := byKey["HLAobjectRoot.Parent.Child"]
			if c.ParentKey() != "HLAobjectRoot.Parent" || len(c.Attributes) != 0 {
				t.Fatalf("inherited container changed: %+v", c)
			}
			inherited := byKey[c.ParentKey()].Attributes[0]
			if inherited.Name != "Position" || !reflect.DeepEqual(inherited.Dimensions, []string{"Y", "X"}) {
				t.Fatalf("inherited model reference lost: %+v", inherited)
			}
			inherited.Dimensions[0] = "mutation"
			if got := dimensionReferences(f, attributeElement); !reflect.DeepEqual(got, []string{"Y", "X"}) {
				t.Fatalf("parsed model is mutable: %v", got)
			}
		})
	}
}

func TestDimensionReferencesModuleCompatibility(t *testing.T) {
	for _, kind := range []string{attributeElement, interactionKind} {
		for _, tc := range []struct {
			name, next string
			conflict   bool
		}{
			{"identical", `<dimensions><dimension>X</dimension><dimension>Y</dimension></dimensions>`, false},
			{"reordered_set", `<dimensions><dimension>Y</dimension><dimension>X</dimension></dimensions>`, false},
			{"different_set", `<dimensions><dimension>X</dimension></dimensions>`, true},
			{"empty_set", `<dimensions/>`, true},
		} {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				first := dimensionModule(kind, `<dimensions><dimension>X</dimension><dimension>Y</dimension></dimensions>`, dimensionDeclarations)
				next := dimensionModule(kind, tc.next, dimensionDeclarations)
				for _, modules := range [][]Module{{first, next}, {next, first}} {
					res, err := Parse(modules)
					if err != nil {
						t.Fatal(err)
					}
					if tc.conflict {
						if res.FOM != nil || !res.HasCode("FOM-014") {
							t.Fatalf("conflicting dimension sets accepted: %+v", res)
						}
					} else if res.FOM == nil || len(res.Diagnostics) != 0 {
						t.Fatalf("compatible copies rejected: %+v", res.Diagnostics)
					}
				}
			})
		}
	}
}

func TestDimensionReferencesInteractionContainerExtension(t *testing.T) {
	parent := dimensionModule(interactionKind, `<dimensions><dimension>X</dimension></dimensions>`, dimensionDeclarations)
	child := Module{Path: "child.xml", XML: []byte(`<objectModel><interactions><interactionClass><name>HLAinteractionRoot</name><interactionClass><name>Signal</name><interactionClass><name>Child</name></interactionClass></interactionClass></interactionClass></interactions></objectModel>`)}
	for _, modules := range [][]Module{{parent, child}, {child, parent}} {
		f := requireDimensionFOM(t, modules...)
		if got := dimensionReferences(f, interactionKind); !reflect.DeepEqual(got, []string{"X"}) {
			t.Fatalf("container extension lost dimensions: %v", got)
		}
	}
}

func TestDimensionReferencesValidateAgainstModuleSet(t *testing.T) {
	for _, kind := range []string{attributeElement, interactionKind} {
		t.Run(kind, func(t *testing.T) {
			for _, ref := range []string{"Missing", " "} {
				m := dimensionModule(kind, `<dimensions><dimension>`+ref+`</dimension></dimensions>`, dimensionDeclarations)
				res, err := Parse([]Module{m})
				if err != nil || res.FOM != nil || !res.HasCode("FOM-015") {
					t.Fatalf("unresolved reference %q: error=%v, result=%+v", ref, err, res)
				}
				if d := res.Diagnostics[0]; d.ModulePath != m.Path || !strings.Contains(d.Message, "dimension") || !strings.Contains(d.Message, strings.TrimSpace(ref)) {
					t.Fatalf("missing diagnostic context: %+v", d)
				}
			}
			m := dimensionModule(kind, `<dimensions><dimension>X</dimension></dimensions>`, "")
			definitions := Module{Path: "dimensions.xml", XML: []byte(`<objectModel>` + dimensionDeclarations + `</objectModel>`)}
			for _, modules := range [][]Module{{m, definitions}, {definitions, m}} {
				requireDimensionFOM(t, modules...)
			}
			requireDimensionFOM(t, dimensionModule(kind, `<dimensions><dimension>HLAfederate</dimension></dimensions>`, ""))
		})
	}
}

func TestDimensionReferencesStandardMIM(t *testing.T) {
	sources := []Module{{Path: "embedded-MIM.xml", XML: mim.StandardMIMBytes()}}
	if root := os.Getenv("GORTI_IEEE2010_SUPPLEMENTAL"); root != "" {
		path := filepath.Join(root, "interface", "1516.1-2010_downloads", "HLAstandardMIM.xml")
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		sources = append(sources, Module{Path: path, XML: b})
	}
	for _, source := range sources {
		t.Run(source.Path, func(t *testing.T) {
			f := requireDimensionFOM(t, source)
			var attribute, interaction bool
			for _, c := range f.ObjectClasses() {
				for _, a := range c.Attributes {
					if c.Name == "HLAfederate" && a.Name == "HLAfederateHandle" {
						attribute = reflect.DeepEqual(a.Dimensions, []string{"HLAfederate"})
					}
				}
			}
			for _, c := range f.InteractionClasses() {
				if c.Name == "HLAreportServiceInvocation" {
					interaction = reflect.DeepEqual(c.Dimensions, []string{"HLAfederate", "HLAserviceGroup"})
				}
			}
			if !attribute || !interaction {
				t.Fatalf("MIM references dropped: attribute=%v interaction=%v", attribute, interaction)
			}
		})
	}
}

func TestDimensionUpperBounds(t *testing.T) {
	for _, tc := range []struct {
		name, element string
		want          uint64
		bad           bool
	}{
		{"omitted", "", 0, false},
		{"one", "<upperBound>1</upperBound>", 1, false},
		{"positive_sign", "<upperBound> +00100 </upperBound>", 100, false},
		{"maximum_uint64", "<upperBound>18446744073709551615</upperBound>", ^uint64(0), false},
		{"zero", "<upperBound>0</upperBound>", 0, true},
		{"negative", "<upperBound>-1</upperBound>", 0, true},
		{"empty", "<upperBound/>", 0, true},
		{"blank", "<upperBound> </upperBound>", 0, true},
		{"units", "<upperBound>1000 (meters)</upperBound>", 0, true},
		{"decimal", "<upperBound>1.5</upperBound>", 0, true},
		{"exponent", "<upperBound>1e3</upperBound>", 0, true},
		{"text", "<upperBound>unbounded</upperBound>", 0, true},
		{"overflow", "<upperBound>18446744073709551616</upperBound>", 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := Module{Path: "bound.xml", XML: []byte(`<objectModel><dimensions><dimension><name>X</name>` + tc.element + `</dimension></dimensions></objectModel>`)}
			res, err := Parse([]Module{m})
			if err != nil {
				t.Fatal(err)
			}
			if tc.bad {
				if res.FOM != nil || !res.HasCode("FOM-015") {
					t.Fatalf("invalid upperBound accepted: %+v", res)
				}
				if d := res.Diagnostics[0]; d.ModulePath != m.Path || !strings.Contains(d.Message, "upperBound") || !strings.Contains(d.Message, `"X"`) {
					t.Fatalf("missing upperBound context: %+v", d)
				}
				return
			}
			if len(res.Diagnostics) != 0 || res.FOM == nil {
				t.Fatalf("valid upperBound rejected: %+v", res.Diagnostics)
			}
			if got := res.FOM.(*model.FOM).Dimensions()[0].UpperBound; got != tc.want {
				t.Fatalf("upperBound = %d, want %d", got, tc.want)
			}
		})
	}
}
