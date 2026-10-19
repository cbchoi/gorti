package requirementsomt_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/cbchoi/gorti/rti/pkg/fom/model"
	"github.com/cbchoi/gorti/rti/pkg/fom/parser"
)

func TestIEEE2010CaseDistinctFDDNames(t *testing.T) {
	for _, parse := range []func([]parser.Module) (parser.Result, error){parser.Parse, parser.ParseAdditive} {
		xml := `<objectModel><objects><objectClass><name>HLAobjectRoot</name><objectClass><name>Entity</name><attribute><name>Value</name></attribute><attribute><name>value</name></attribute></objectClass><objectClass><name>entity</name></objectClass></objectClass></objects><interactions><interactionClass><name>HLAinteractionRoot</name><interactionClass><name>Signal</name><parameter><name>Value</name></parameter><parameter><name>value</name></parameter></interactionClass><interactionClass><name>signal</name></interactionClass></interactionClass></interactions><dimensions><dimension><name>X</name><upperBound>10</upperBound></dimension><dimension><name>x</name><upperBound>20</upperBound></dimension></dimensions></objectModel>`
		result, err := parse([]parser.Module{{Path: "case.xml", XML: []byte(xml)}})
		if err != nil || result.FOM == nil || len(result.Diagnostics) != 0 {
			t.Fatalf("case-distinct declarations rejected: %v %+v", err, result.Diagnostics)
		}
		f := result.FOM.(*model.FOM)
		objects, interactions := map[string]int{}, map[string]int{}
		for _, c := range f.ObjectClasses() {
			objects[c.Name] = len(c.Attributes)
			if c.Name == "Entity" && (len(c.Attributes) != 2 || c.Attributes[0].Name == c.Attributes[1].Name) {
				t.Fatal("case-distinct attributes collapsed")
			}
		}
		for _, c := range f.InteractionClasses() {
			interactions[c.Name] = len(c.Parameters)
			if c.Name == "Signal" && (len(c.Parameters) != 2 || c.Parameters[0].Name == c.Parameters[1].Name) {
				t.Fatal("case-distinct parameters collapsed")
			}
		}
		if !reflect.DeepEqual(objects, map[string]int{"HLAobjectRoot": 0, "Entity": 2, "entity": 0}) ||
			!reflect.DeepEqual(interactions, map[string]int{"HLAinteractionRoot": 0, "Signal": 2, "signal": 0}) {
			t.Fatalf("case-distinct classes changed: %v %v", objects, interactions)
		}
		dimensions := f.Dimensions()
		if len(dimensions) != 2 || dimensions[0].Name != "X" || dimensions[0].UpperBound != 10 || dimensions[1].Name != "x" || dimensions[1].UpperBound != 20 {
			t.Fatalf("case-distinct dimensions changed: %+v", dimensions)
		}
	}
}

func TestIEEE2010DimensionReferencesAreCaseSensitive(t *testing.T) {
	for _, kind := range []string{"attribute", "interaction"} {
		for _, reference := range []string{"X", "x"} {
			t.Run(kind+"/"+reference, func(t *testing.T) {
				fragment := `<objects><objectClass><name>HLAobjectRoot</name><objectClass><name>Entity</name><attribute><name>Value</name><dimensions><dimension>` + reference + `</dimension></dimensions></attribute></objectClass></objectClass></objects>`
				if kind == "interaction" {
					fragment = `<interactions><interactionClass><name>HLAinteractionRoot</name><interactionClass><name>Signal</name><dimensions><dimension>` + reference + `</dimension></dimensions></interactionClass></interactionClass></interactions>`
				}
				xml := `<objectModel>` + fragment + `<dimensions><dimension><name>X</name><upperBound>10</upperBound></dimension></dimensions></objectModel>`
				for _, parse := range []func([]parser.Module) (parser.Result, error){parser.Parse, parser.ParseAdditive} {
					result, err := parse([]parser.Module{{XML: []byte(xml)}})
					if err != nil {
						t.Fatal(err)
					}
					if reference == "X" {
						if result.FOM == nil || len(result.Diagnostics) != 0 {
							t.Fatalf("exact reference rejected: %+v", result.Diagnostics)
						}
					} else if result.FOM != nil || !result.HasCode("FOM-015") {
						t.Fatalf("wrong-case reference accepted: %+v", result.Diagnostics)
					}
				}
			})
		}
	}
}

func TestIEEE2010RepeatedSwitchDefinitions(t *testing.T) {
	for _, name := range []string{"autoProvide", "conveyRegionDesignatorSets", "conveyProducingFederate", "serviceReporting", "exceptionReporting", "delaySubscriptionEvaluation", "objectClassRelevanceAdvisory", "attributeRelevanceAdvisory", "attributeScopeAdvisory", "interactionRelevanceAdvisory"} {
		t.Run(name, func(t *testing.T) {
			xml := fmt.Sprintf(`<objectModel><switches><%s isEnabled="true"/></switches></objectModel>`, name)
			for _, parse := range []func([]parser.Module) (parser.Result, error){parser.Parse, parser.ParseAdditive} {
				for _, equal := range []bool{true, false} {
					second := xml
					if !equal {
						second = strings.Replace(xml, `isEnabled="true"`, `isEnabled="false"`, 1)
					}
					for _, pair := range [][2]string{{xml, second}, {second, xml}} {
						result, err := parse([]parser.Module{{Path: "a.xml", XML: []byte(pair[0])}, {Path: "b.xml", XML: []byte(pair[1])}})
						if err != nil {
							t.Fatal(err)
						}
						if equal {
							if result.FOM == nil || len(result.Diagnostics) != 0 || !reflect.DeepEqual(result.FOM.(*model.FOM).AdvisorySwitches(), []model.AdvisorySwitch{{Name: name, Enabled: true}}) {
								t.Fatalf("identical switch lost or rejected: %+v", result)
							}
						} else if result.FOM != nil || !result.HasCode("FOM-017") {
							t.Fatalf("conflicting switch accepted: %+v", result)
						}
					}
				}
			}
		})
	}
}
