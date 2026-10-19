package parser_test

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/application/composition"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/federation"
	"github.com/cbchoi/gorti/rti/pkg/fom/mim"
	"github.com/cbchoi/gorti/rti/pkg/fom/parser"
)

func nameModule(name string) parser.Module {
	return parser.Module{Path: "names.xml", XML: []byte(`<objectModel xmlns="http://standards.ieee.org/IEEE1516-2010"><objects><objectClass><name>HLAobjectRoot</name><objectClass><name>` + name + `</name><sharing>PublishSubscribe</sharing></objectClass></objectClass></objects></objectModel>`)}
}

// IEEE 1516.2-2010 3.3.1, supplied PDF pages 31-32.
func TestIEEE2010LocalNamesAdmission(t *testing.T) {
	for _, name := range []string{"1Sensor", "Sensor:Bad", "Sensor Bad", "HLAcustom", "NA", "Sensor.Bad", "hLaCustom", "nA", "", " Sensor", "Sensor ", "-Sensor"} {
		for parseName, parse := range classAdmissionParsers() {
			t.Run(fmt.Sprintf("%s/%s", parseName, name), func(t *testing.T) {
				result, err := parse([]parser.Module{nameModule(name)})
				if err != nil || result.FOM != nil || !result.HasCode("FOM-017") {
					t.Fatalf("invalid local name admitted: %v %+v", err, result)
				}
				for _, d := range result.Diagnostics {
					if d.Code == "FOM-017" && (d.ModulePath != "names.xml" || d.Line == 0) {
						t.Fatalf("missing source location: %+v", d)
					}
				}
			})
		}
	}
	for _, name := range []string{"Sensor", "sensor", "_Sensor", "Sensor-2", "Sensor_2", "\uac10\uc9c0\uae30", "\u0394Sensor", "Cafe\u0301"} {
		t.Run("valid/"+name, func(t *testing.T) {
			result, err := parser.Parse([]parser.Module{nameModule(name)})
			if err != nil || result.FOM == nil || len(result.Diagnostics) != 0 {
				t.Fatalf("valid Unicode/XML name rejected: %v %+v", err, result)
			}
		})
	}
}

func TestIEEE2010LocalNamesDefinitionsAndMIM(t *testing.T) {
	for _, fragment := range []string{
		`<interactions><interactionClass><name>HLAinteractionRoot</name><interactionClass><name>Bad:Class</name></interactionClass></interactionClass></interactions>`,
		`<objects><objectClass><name>HLAobjectRoot</name><objectClass><name>Sensor</name><attribute><name>hLaValue</name><dataType>HLAinteger32BE</dataType></attribute></objectClass></objectClass></objects>`,
		`<interactions><interactionClass><name>HLAinteractionRoot</name><interactionClass><name>Signal</name><parameter><name>nA</name><dataType>HLAinteger32BE</dataType></parameter></interactionClass></interactionClass></interactions>`,
		`<dimensions><dimension><name>X.Y</name><upperBound>100</upperBound></dimension></dimensions>`,
		`<transportations><transportation><name>HLAcustom</name><reliable>Yes</reliable></transportation></transportations>`,
		`<dataTypes><simpleDataTypes><simpleData><name>HLAcustom</name><representation>HLAinteger32BE</representation></simpleData></simpleDataTypes></dataTypes>`,
		`<dataTypes><enumeratedDataTypes><enumeratedData><name>Mode</name><representation>HLAinteger32BE</representation><enumerator><name>Bad:Enum</name><values>0</values></enumerator></enumeratedData></enumeratedDataTypes></dataTypes>`,
		`<dataTypes><fixedRecordDataTypes><fixedRecordData><name>Record</name><field><name>Bad.Field</name><dataType>HLAinteger32BE</dataType></field></fixedRecordData></fixedRecordDataTypes></dataTypes>`,
		`<dataTypes><variantRecordDataTypes><variantRecordData><name>Variant</name><discriminant>Mode</discriminant><dataType>Mode</dataType><alternative><enumerator>Zero</enumerator><name>NA</name><dataType>HLAinteger32BE</dataType></alternative></variantRecordData></variantRecordDataTypes></dataTypes>`,
	} {
		result, err := parser.Parse([]parser.Module{{Path: "definitions.xml", XML: []byte("<objectModel>" + fragment + "</objectModel>")}})
		if err != nil || result.FOM != nil || !result.HasCode("FOM-017") {
			t.Errorf("invalid declaration admitted: %s: %v %+v", fragment, err, result)
		}
	}
	result, err := parser.Parse([]parser.Module{{Path: "standard.xml", XML: mim.StandardMIMBytes()}})
	if err != nil || result.FOM == nil || len(result.Diagnostics) != 0 {
		t.Fatalf("standard MIM rejected: %v %+v", err, result)
	}
	scaffold := []byte(`<objectModel><objects><objectClass><name>HLAobjectRoot</name><objectClass><name>HLAmanager</name><objectClass><name>HLAfederate</name><objectClass><name>UserExtension</name></objectClass></objectClass></objectClass></objectClass></objects></objectModel>`)
	result, err = parser.Parse([]parser.Module{{Path: "scaffold.xml", XML: scaffold}})
	if err != nil || result.FOM == nil || len(result.Diagnostics) != 0 {
		t.Fatalf("predefined scaffolding rejected: %v %+v", err, result)
	}
	for _, marker := range []string{`<modelIdentification><type>MIM</type></modelIdentification>`, `<modelIdentification><name>HLAstandardMIM</name></modelIdentification>`} {
		module := nameModule("HLAcustom")
		module.XML = []byte(strings.Replace(string(module.XML), "<objects>", marker+"<objects>", 1))
		result, err = parser.Parse([]parser.Module{module})
		if err != nil || result.FOM != nil || !result.HasCode("FOM-017") {
			t.Fatalf("MIM label bypassed reserved names: %v %+v", err, result)
		}
	}
}

func TestIEEE2010LocalNamesCreateAndJoinAtomicity(t *testing.T) {
	for _, invalid := range []string{"1Sensor", "Sensor:Bad", "Sensor Bad", "HLAcustom", "NA"} {
		t.Run(invalid, func(t *testing.T) {
			ctx := context.Background()
			repo := composition.NewFOMRepository()
			manager, err := federation.New(federation.Options{Clock: core.NewManualClock(time.Unix(0, 0)), FOMs: repo, PrepareJoinFOM: repo.PrepareLiveExtension})
			if err != nil {
				t.Fatal(err)
			}
			module := nameModule(invalid)
			req := core.CreateFederationRequest{Name: "names", FOMModules: []core.FOMModule{{Path: module.Path, XML: module.XML}}}
			if err = manager.CreateFederation(ctx, req); err == nil || !strings.Contains(err.Error(), "FOM-017") {
				t.Fatalf("invalid create admitted: %v", err)
			}
			if h, e := manager.FOMFor("names"); e == nil || h != nil {
				t.Fatal("failed create published FOM")
			}
			valid := nameModule("Sensor")
			req.FOMModules[0].XML = valid.XML
			if err = manager.CreateFederation(ctx, req); err != nil {
				t.Fatalf("failed create reserved name: %v", err)
			}
			before, err := manager.FOMFor("names")
			if err != nil {
				t.Fatal(err)
			}
			repo.RememberFor("names", before)
			good := nameModule("ValidExtension")
			join := core.JoinFederationRequest{Federation: "names", FederateName: "joiner", AdditionalFOMModules: []core.FOMModule{{Path: "valid.xml", XML: good.XML}, {Path: "invalid.xml", XML: module.XML}}}
			if h, e := manager.JoinFederation(ctx, join); e == nil || h != 0 || !strings.Contains(e.Error(), "FOM-017") {
				t.Fatalf("invalid extension admitted: %d %v", h, e)
			}
			after, err := manager.FOMFor("names")
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatalf("failed join changed FOM: %v", err)
			}
			if _, ok := after.LookupObjectClass("ValidExtension"); ok {
				t.Fatal("failed join retained valid sibling extension")
			}
			if len(manager.ListMembers("names")) != 0 {
				t.Fatal("failed join changed membership")
			}
			join.AdditionalFOMModules = join.AdditionalFOMModules[:1]
			if h, e := manager.JoinFederation(ctx, join); e != nil || h == 0 {
				t.Fatalf("failed join reserved member name: %d %v", h, e)
			}
		})
	}
}

func TestIEEE2010LocalNamesLegacyQualification(t *testing.T) {
	for _, name := range []string{"HLAobjectRoot.Sensor", "HLAobjectRoot.Sensor.Child"} {
		class := `<objectClass><name>` + name + `</name></objectClass>`
		if strings.HasSuffix(name, ".Child") {
			class = `<objectClass><name>HLAobjectRoot.Sensor</name><objectClass><name>Child</name></objectClass></objectClass>`
		}
		module := parser.Module{Path: "legacy.xml", XML: []byte(`<objectModel><objects>` + class + `</objects></objectModel>`)}
		result, err := parser.Parse([]parser.Module{module})
		if err != nil || result.FOM == nil || len(result.Diagnostics) != 0 {
			t.Fatalf("legacy qualified representation rejected: %v %+v", err, result)
		}
		module.XML = []byte(strings.Replace(string(module.XML), "<objectModel>", `<objectModel xmlns="http://standards.ieee.org/IEEE1516-2010">`, 1))
		result, err = parser.Parse([]parser.Module{module})
		if err != nil || result.FOM != nil || !result.HasCode("FOM-017") {
			t.Fatalf("standard local name treated as legacy qualified: %v %+v", err, result)
		}
	}
}
