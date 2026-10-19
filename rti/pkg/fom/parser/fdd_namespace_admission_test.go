package parser

import (
	"strings"
	"testing"

	"github.com/cbchoi/gorti/rti/pkg/fom/model"
)

const namespaceAdmissionFDD = `<objectModel xmlns="http://standards.ieee.org/IEEE1516-2010" xmlns:x="urn:gorti:test:extension">
<objects><objectClass><name>HLAobjectRoot</name><objectClass><name>Vehicle</name><sharing>PublishSubscribe</sharing></objectClass></objectClass></objects>
<interactions><interactionClass><name>HLAinteractionRoot</name><transportation>HLAreliable</transportation><order>Receive</order><interactionClass><name>Probe</name><sharing>PublishSubscribe</sharing><transportation>HLAreliable</transportation><order>Receive</order><parameter><name>payload</name></parameter></interactionClass></interactionClass></interactions>
<dimensions/><transportations/><switches><autoProvide/><conveyRegionDesignatorSets/><conveyProducingFederate/><attributeScopeAdvisory/><attributeRelevanceAdvisory/><objectClassRelevanceAdvisory/><interactionRelevanceAdvisory/><serviceReporting/><exceptionReporting/><delaySubscriptionEvaluation/></switches>
</objectModel>`

func TestFDDNamespaceAuthorizedSlotsDoNotAlterSemanticModel(t *testing.T) {
	for _, tc := range []struct{ name, before, after string }{
		{"root", `</objectModel>`, `<x:review><arbitrary xmlns="urn:foreign">text</arbitrary></x:review></objectModel>`},
		{"object class", `<sharing>PublishSubscribe</sharing></objectClass>`, `<sharing>PublishSubscribe</sharing><x:review/></objectClass>`},
		{parameterElement, `<name>payload</name></parameter>`, `<name>payload</name><x:review/></parameter>`},
		{timeElement, `<dimensions/>`, `<dimensions/><time><timeStamp><x:review/></timeStamp><lookahead/><x:review/></time>`},
		{"dimensions", `<dimensions/>`, `<dimensions><x:review/></dimensions>`},
		{"foreign objectClass cannot replace root", `</objects>`, `<x:objectClass><name>Injected</name><objectClass><name>1InvalidName</name></objectClass></x:objectClass></objects>`},
		{"foreign objects cannot replace section", `</objectModel>`, `<x:objects><objectClass><name>Injected</name></objectClass></x:objects></objectModel>`},
		{"foreign interactionClass cannot inject", `</interactions>`, `<x:interactionClass><name>Injected</name></x:interactionClass></interactions>`},
		{"foreign declaration name ignored", `<name>payload</name></parameter>`, `<name>payload</name><x:name>1InvalidName</x:name></parameter>`},
		{"foreign sharing not interpreted", `<sharing>PublishSubscribe</sharing></objectClass>`, `<sharing>PublishSubscribe</sharing><x:sharing>NotASharing</x:sharing></objectClass>`},
		{"foreign MIM marker not interpreted", `</objectModel>`, `<x:modelIdentification><type>MIM</type></x:modelIdentification></objectModel>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := strings.Replace(namespaceAdmissionFDD, tc.before, tc.after, 1)
			result, err := Parse([]Module{{Path: "extension.xml", XML: []byte(data)}})
			if err != nil || result.FOM == nil || len(result.Diagnostics) != 0 {
				t.Fatalf("authorized extension rejected: err=%v result=%+v", err, result)
			}
			fom := result.FOM.(*model.FOM)
			objects, interactions := fom.ObjectClasses(), fom.InteractionClasses()
			if len(objects) != 2 || objects[0].Name != objectClassRootName || objects[1].Name != testVehicleName || objects[1].Sharing != sharingPublishSubscribe {
				t.Fatalf("extension injected or replaced objects: %+v", objects)
			}
			if len(interactions) != 2 || interactions[1].Name != "Probe" || len(interactions[1].Parameters) != 1 || interactions[1].Parameters[0].Name != "payload" {
				t.Fatalf("extension injected or replaced interactions: %+v", interactions)
			}
			if isMIMTypeModule([]byte(data)) {
				t.Fatal("foreign metadata bypassed standard-MIM conflict validation")
			}
		})
	}
}

func TestFDDNamespaceRejectsUnauthorizedSlots(t *testing.T) {
	for _, tc := range []struct{ name, before, after string }{
		{"simple leaf", `<name>Vehicle</name>`, `<name>Vehicle<x:review/></name>`},
		{"service has no wildcard", `<dimensions/>`, `<dimensions/><serviceUtilization><x:review/></serviceUtilization>`},
		{"empty switch has no wildcard", `<autoProvide/>`, `<autoProvide><x:review/></autoProvide>`},
		{"before required class name", `<name>Vehicle</name>`, `<x:review/><name>Vehicle</name>`},
		{"core content after extension", `<name>payload</name></parameter>`, `<name>payload</name><x:review/><semantics>late</semantics></parameter>`},
		{"only one wildcard", `</objectModel>`, `<x:review/><x:other/></objectModel>`},
		{"root before required sections", `<objects>`, `<x:review/><objects>`},
		{"unqualified not other namespace", `</objectModel>`, `<review xmlns=""/></objectModel>`},
		{"same namespace unknown", `</objectModel>`, `<review/></objectModel>`},
		{"known local name inside invalid slot", `<name>Vehicle</name>`, `<name>Vehicle<x:objectClass/></name>`},
		{"misplaced parent is not authorized", `</objectModel>`, `<parameter><name>fake</name><x:review/></parameter></objectModel>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := strings.Replace(namespaceAdmissionFDD, tc.before, tc.after, 1)
			result, err := Parse([]Module{{Path: "invalid-extension.xml", XML: []byte(data)}})
			if err != nil || result.FOM != nil || !result.HasCode(invalidStructureCode) {
				t.Fatalf("want located FOM-009 and no model: err=%v result=%+v", err, result)
			}
		})
	}
}

func TestFDDSharingEnumerationUsesSchemaLexicalValues(t *testing.T) {
	for _, value := range []string{"Publish", "Subscribe", sharingPublishSubscribe, "Neither", "Publ<![CDATA[ish]]>"} {
		t.Run("valid/"+value, func(t *testing.T) {
			data := strings.ReplaceAll(namespaceAdmissionFDD, sharingPublishSubscribe, value)
			result, err := Parse([]Module{{Path: "sharing.xml", XML: []byte(data)}})
			if err != nil || result.FOM == nil || len(result.Diagnostics) != 0 {
				t.Fatalf("schema enumeration rejected: err=%v result=%+v", err, result)
			}
		})
	}
	for _, value := range []string{"NotASharing", "publish", " Publish", "Publish ", "Publish Subscribe"} {
		t.Run("invalid/"+value, func(t *testing.T) {
			data := strings.ReplaceAll(namespaceAdmissionFDD, sharingPublishSubscribe, value)
			result, err := Parse([]Module{{Path: "invalid-sharing.xml", XML: []byte(data)}})
			if err != nil || result.FOM != nil || !result.HasCode(invalidStructureCode) {
				t.Fatalf("invalid sharing admitted: err=%v result=%+v", err, result)
			}
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == invalidStructureCode && strings.Contains(diagnostic.Message, sharingElement) && diagnostic.ModulePath == "invalid-sharing.xml" && diagnostic.Line > 0 {
					return
				}
			}
			t.Fatal("sharing diagnostic lacks source location")
		})
	}
}

func TestFDDForeignAttributesCannotOverrideSemanticSwitches(t *testing.T) {
	data := strings.Replace(namespaceAdmissionFDD, `<autoProvide/>`, `<autoProvide x:isEnabled="true"/>`, 1)
	result, err := Parse([]Module{{Path: "foreign-attribute.xml", XML: []byte(data)}})
	if err != nil || result.FOM == nil || len(result.Diagnostics) != 0 {
		t.Fatalf("common foreign attribute rejected: err=%v result=%+v", err, result)
	}
	for _, setting := range result.FOM.(*model.FOM).AdvisorySwitches() {
		if setting.Name == "autoProvide" {
			if setting.Enabled {
				t.Fatal("foreign isEnabled injected an HLA setting")
			}
			return
		}
	}
	t.Fatal("the actual HLA autoProvide declaration disappeared")
}

func TestFDDMalformedForeignPayloadStillFailsXMLParsing(t *testing.T) {
	data := strings.Replace(namespaceAdmissionFDD, `</objectModel>`, `<x:review><x:broken></x:review></objectModel>`, 1)
	result, err := Parse([]Module{{Path: "malformed-extension.xml", XML: []byte(data)}})
	if err == nil || result.FOM != nil {
		t.Fatalf("malformed foreign XML ignored: err=%v result=%+v", err, result)
	}
}

func TestFDDSharingRejectsNestedCoreContent(t *testing.T) {
	data := strings.Replace(namespaceAdmissionFDD, `<sharing>PublishSubscribe</sharing>`, `<sharing>Publi<semantics/>sh</sharing>`, 1)
	result, err := Parse([]Module{{Path: "nested-sharing.xml", XML: []byte(data)}})
	if err != nil || result.FOM != nil || !result.HasCode(invalidStructureCode) {
		t.Fatalf("sharing simple content accepted an HLA child: err=%v result=%+v", err, result)
	}
}
