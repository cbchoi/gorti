package parser

import (
	"encoding/xml"
	"os"
	"strings"
	"testing"

	"github.com/cbchoi/gorti/rti/pkg/fom/model"
)

func metadataModule(metadata string) []byte {
	return []byte(strings.Replace(minimalFOM, "</objectModel>", metadata+"\n</objectModel>", 1))
}

func TestFDD2010MetadataSchemaFixture(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/fdd2010-metadata.xml")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Utilization struct {
			Entries []struct{ XMLName xml.Name } `xml:",any"`
		} `xml:"serviceUtilization"`
	}
	if err := xml.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Utilization.Entries) != 168 {
		t.Fatalf("official FDD service fixture has %d entries, want 168", len(fixture.Utilization.Entries))
	}
	result, err := Parse([]Module{{Path: "testdata/fdd2010-metadata.xml", XML: data}})
	if err != nil || len(result.Diagnostics) != 0 || result.FOM == nil {
		t.Fatalf("Parse official-vocabulary fixture: err=%v diagnostics=%+v", err, result.Diagnostics)
	}
	if got := result.FOM.(*model.FOM).ObjectClasses(); len(got) != 1 || got[0].Name != objectClassRootName {
		t.Fatalf("metadata altered object classes: %+v", got)
	}
}

func TestFDD2010MetadataAccepted(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, metadata string }{
		{"empty time", `<time/>`},
		{"empty utilization", `<serviceUtilization/>`},
		{"empty time fields", `<time><timeStamp/><lookahead/></time>`},
		{"MIM time types", `<time><timeStamp><dataType>HLAfloat64BE</dataType></timeStamp><lookahead><dataType>HLAinteger64BE</dataType><semantics>Interval</semantics></lookahead></time>`},
		{"local time type", `<time><timeStamp><dataType>ClockValue</dataType></timeStamp><lookahead><dataType>ClockValue</dataType></lookahead></time><dataTypes><simpleDataTypes><simpleData><name>ClockValue</name><representation>HLAfloat64BE</representation></simpleData></simpleDataTypes></dataTypes>`},
		{"optional service flag", `<serviceUtilization><connect section="4.2" isCallback="false"/></serviceUtilization>`},
		{"unordered services", `<serviceUtilization><timeAdvanceGrant section="8.13" isCallback="true" isUsed="1"/><connect section="4.2" isCallback="false" isUsed="0"/></serviceUtilization>`},
		{"schema spelling", `<serviceUtilization><removeobjectinstance section="6.15" isCallback="true" isUsed="true"/></serviceUtilization>`},
		{"common attributes", `<serviceUtilization><connect section="4.2" isCallback="false" isUsed="false" idtag="usage"/></serviceUtilization>`},
		{"prefixed metadata", `<f:time xmlns:f="http://standards.ieee.org/IEEE1516-2010"><f:lookahead><f:dataType>HLAfloat64BE</f:dataType></f:lookahead></f:time>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := Parse([]Module{{Path: "accepted.xml", XML: metadataModule(tc.metadata)}})
			if err != nil || len(result.Diagnostics) != 0 || result.FOM == nil {
				t.Fatalf("Parse: err=%v diagnostics=%+v", err, result.Diagnostics)
			}
			classes := result.FOM.(*model.FOM).ObjectClasses()
			if len(classes) != 2 || classes[1].Attributes[0].Name != "position" {
				t.Fatalf("ordinary model changed: %+v", classes)
			}
		})
	}
}

func TestFDD2010MetadataRejectsInvalidStructure(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, metadata, offender string }{
		{"unknown time child", `<time><madeUp/></time>`, "madeUp"},
		{"known but misplaced time child", `<time><attribute/></time>`, attributeElement},
		{"unknown nested time child", `<time><lookahead><madeUp/></lookahead></time>`, "madeUp"},
		{"nested reference", `<time><timeStamp><dataType><name>HLAfloat64BE</name></dataType></timeStamp></time>`, "name"},
		{"unknown service", `<serviceUtilization><madeUp section="4.2" isCallback="false"/></serviceUtilization>`, "madeUp"},
		{"known non-service", `<serviceUtilization><name/></serviceUtilization>`, "name"},
		{"service outside section", `<connect section="4.2" isCallback="false"/>`, "connect"},
		{"service nested in time", `<time><connect section="4.2" isCallback="false"/></time>`, "connect"},
		{"nested service content", `<serviceUtilization><connect section="4.2" isCallback="false"><name>x</name></connect></serviceUtilization>`, "name"},
		{"missing section", `<serviceUtilization><connect isCallback="false"/></serviceUtilization>`, "section"},
		{"wrong section", `<serviceUtilization><connect section="4.3" isCallback="false"/></serviceUtilization>`, "section"},
		{"missing callback", `<serviceUtilization><connect section="4.2"/></serviceUtilization>`, "isCallback"},
		{"wrong callback", `<serviceUtilization><connect section="4.2" isCallback="true"/></serviceUtilization>`, "isCallback"},
		{"invalid boolean", `<serviceUtilization><connect section="4.2" isCallback="false" isUsed="yes"/></serviceUtilization>`, "isUsed"},
		{"unknown attribute", `<serviceUtilization><connect section="4.2" isCallback="false" used="true"/></serviceUtilization>`, "used"},
		{"duplicate service", `<serviceUtilization><connect section="4.2" isCallback="false"/><connect section="4.2" isCallback="false"/></serviceUtilization>`, "connect"},
		{"duplicate time", `<time/><time/>`, timeElement},
		{"duplicate utilization", `<serviceUtilization/><serviceUtilization/>`, fddServicesElement},
		{"duplicate time field", `<time><lookahead/><lookahead/></time>`, "lookahead"},
		{"duplicate datatype", `<time><lookahead><dataType>HLAfloat64BE</dataType><dataType>HLAfloat64BE</dataType></lookahead></time>`, "dataType"},
		{"time field order", `<time><lookahead/><timeStamp/></time>`, "timeStamp"},
		{"reference field order", `<time><lookahead><semantics>x</semantics><dataType>HLAfloat64BE</dataType></lookahead></time>`, "dataType"},
		{"wrong namespace outside wildcard", `<time><lookahead><dataType><x:lookahead xmlns:x="urn:unknown"/></dataType></lookahead></time>`, "lookahead"},
		{"service text", `<serviceUtilization><connect section="4.2" isCallback="false">not empty</connect></serviceUtilization>`, "connect"},
		{"unrelated unknown", `<time/><unrelatedUnknown/>`, "unrelatedUnknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := Parse([]Module{{Path: "invalid-metadata.xml", XML: metadataModule(tc.metadata)}})
			if err != nil {
				t.Fatalf("unexpected XML error: %v", err)
			}
			if result.FOM != nil || !result.HasCode(invalidStructureCode) {
				t.Fatalf("want no model and FOM-009: %+v", result)
			}
			for _, d := range result.Diagnostics {
				if d.Code == invalidStructureCode && strings.Contains(d.Message, tc.offender) && d.ModulePath == "invalid-metadata.xml" && d.Line > 0 {
					return
				}
			}
			t.Fatalf("missing located diagnostic for %q: %+v", tc.offender, result.Diagnostics)
		})
	}
}

func TestFDD2010MetadataValidatesTimeReferences(t *testing.T) {
	t.Parallel()
	for _, tag := range []string{"timeStamp", "lookahead"} {
		for _, name := range []string{"MissingTimeType", ""} {
			t.Run(tag+"/"+name, func(t *testing.T) {
				metadata := "<time><" + tag + "><dataType>" + name + "</dataType></" + tag + "></time>"
				result, err := Parse([]Module{{Path: "time-ref.xml", XML: metadataModule(metadata)}})
				if err != nil || result.FOM != nil || !result.HasCode(undefinedDataTypeCode) {
					t.Fatalf("want rejected time reference: err=%v result=%+v", err, result)
				}
				for _, d := range result.Diagnostics {
					if d.Code == undefinedDataTypeCode && strings.Contains(d.Message, tag) && d.ModulePath == "time-ref.xml" {
						return
					}
				}
				t.Fatalf("missing diagnostic for time/%s: %+v", tag, result.Diagnostics)
			})
		}
	}
}
