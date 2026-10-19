package parser

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"strings"
)

const fddNamespace = "http://standards.ieee.org/IEEE1516-2010"

type fddContentModel struct {
	sequence, required, repeated []string
}

func fddContent(sequence, required, repeated string) *fddContentModel {
	return &fddContentModel{strings.Fields(sequence), strings.Fields(required), strings.Fields(repeated)}
}

// Only these complex-content contexts have a final, single ##other element in
// IEEE1516-FDD-2010.xsd (2010-08-16). This is a wildcard-placement projection,
// not a replacement for full schema validation or a global foreign-name bypass.
var fddExtensionModels = map[string]*fddContentModel{
	objectModelElement:         fddContent("modelIdentification serviceUtilization objects interactions dimensions time tags synchronizations transportations switches updateRates dataTypes notes", "objects interactions dimensions transportations switches", ""),
	"modelIdentification":      fddContent("name type version modificationDate securityClassification releaseRestriction purpose applicationDomain description useLimitation useHistory keyword poc reference other glyph", "", "releaseRestriction useHistory keyword poc reference"),
	objectsElement:             fddContent(objectClassElement, objectClassElement, ""),
	objectClassElement:         fddContent("name sharing semantics attribute objectClass", "name", "attribute objectClass"),
	attributeElement:           fddContent("name dataType updateType updateCondition ownership sharing dimensions transportation order semantics", "name transportation order", ""),
	interactionsElement:        fddContent(interactionClassElement, interactionClassElement, ""),
	interactionClassElement:    fddContent("name sharing dimensions transportation order semantics parameter interactionClass", "name transportation order", "parameter interactionClass"),
	parameterElement:           fddContent("name dataType semantics", "name", ""),
	"dimensionRefs":            fddContent("dimension", "", "dimension"),
	"dimensions":               fddContent("dimension", "", "dimension"),
	"dimension":                fddContent("name dataType upperBound normalization value", "name", ""),
	timeElement:                fddContent("timeStamp lookahead", "", ""),
	"dataTypeSemantics":        fddContent("dataType semantics", "", ""),
	"tags":                     fddContent("updateReflectTag sendReceiveTag deleteRemoveTag divestitureRequestTag divestitureCompletionTag acquisitionRequestTag requestUpdateTag", "", ""),
	"synchronizations":         fddContent("synchronizationPoint", "", "synchronizationPoint"),
	"synchronizationPoint":     fddContent("label dataType capability semantics", "label", ""),
	"transportations":          fddContent("transportation", "", "transportation"),
	"transportation":           fddContent("name reliable semantics", "name reliable", ""),
	"switches":                 fddContent("autoProvide conveyRegionDesignatorSets conveyProducingFederate attributeScopeAdvisory attributeRelevanceAdvisory objectClassRelevanceAdvisory interactionRelevanceAdvisory serviceReporting exceptionReporting delaySubscriptionEvaluation automaticResignAction", "autoProvide conveyRegionDesignatorSets conveyProducingFederate attributeScopeAdvisory attributeRelevanceAdvisory objectClassRelevanceAdvisory interactionRelevanceAdvisory serviceReporting exceptionReporting delaySubscriptionEvaluation", ""),
	"updateRates":              fddContent("updateRate", "", "updateRate"),
	"updateRate":               fddContent("name rate semantics", "name rate", ""),
	"dataTypes":                fddContent("basicDataRepresentations simpleDataTypes enumeratedDataTypes arrayDataTypes fixedRecordDataTypes variantRecordDataTypes", "", ""),
	"basicDataRepresentations": fddContent("basicData", "", "basicData"),
	"basicData":                fddContent("name size interpretation endian encoding", "name", ""),
	"simpleDataTypes":          fddContent("simpleData", "", "simpleData"),
	"simpleData":               fddContent("name representation units resolution accuracy semantics", "name", ""),
	"enumeratedDataTypes":      fddContent("enumeratedData", "", "enumeratedData"),
	"enumeratedData":           fddContent("name representation semantics enumerator", "name", "enumerator"),
	"enumerator":               fddContent("name value", "name", "value"),
	"arrayDataTypes":           fddContent("arrayData", "", "arrayData"),
	"arrayData":                fddContent("name dataType cardinality encoding semantics", "name", ""),
	"fixedRecordDataTypes":     fddContent("fixedRecordData", "", "fixedRecordData"),
	"fixedRecordData":          fddContent("name encoding semantics field", "name", "field"),
	"field":                    fddContent("name dataType semantics", "name", ""),
	"variantRecordDataTypes":   fddContent("variantRecordData", "", "variantRecordData"),
	"variantRecordData":        fddContent("name discriminant dataType alternative encoding semantics", "name", "alternative"),
	"alternative":              fddContent("enumerator name dataType semantics", "", ""),
	"notes":                    fddContent("note", "", "note"),
	"note":                     fddContent("label semantics", "label", ""),
}

func wordIndex(words []string, name string) int {
	for i, word := range words {
		if word == name {
			return i
		}
	}
	return -1
}

func (parent *strictElement) fddChild(name string) string {
	content := fddExtensionModels[parent.content]
	if content == nil {
		return ""
	}
	position := wordIndex(content.sequence, name)
	if parent.children == nil {
		parent.children = make(map[string]int)
	}
	parent.children[name]++
	if position < 0 || position < parent.position || parent.children[name] > 1 && wordIndex(content.repeated, name) < 0 {
		parent.layoutInvalid = true
	}
	parent.position = position
	if position < 0 {
		return ""
	}
	return fddChildContent(parent.content, name)
}

func (parent *strictElement) foreignElementProblem(name xml.Name) string {
	content := fddExtensionModels[parent.content]
	if content == nil || parent.layoutInvalid || parent.extensionSeen {
		return fmt.Sprintf("foreign element %q is not in an available FDD extension slot inside %q", name.Local, parent.name.Local)
	}
	for _, required := range content.required {
		if parent.children[required] == 0 {
			return fmt.Sprintf("foreign element %q precedes required %q inside %q", name.Local, required, parent.name.Local)
		}
	}
	return ""
}

// encoding/xml otherwise matches local names regardless of namespace. Keep
// foreign payloads opaque for every semantic/name/MIM-marker decoder; strict
// validation independently checks their placement using the original bytes.
type semanticXMLTokens struct {
	decoder   *xml.Decoder
	rootSeen  bool
	namespace string
}

func newSemanticXMLTokens(data []byte) *semanticXMLTokens {
	return &semanticXMLTokens{decoder: xml.NewDecoder(bytes.NewReader(data))}
}

func (reader *semanticXMLTokens) Token() (xml.Token, error) {
	for {
		token, err := reader.decoder.Token()
		if err != nil {
			return token, err
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			return token, nil
		}
		if !reader.rootSeen {
			reader.rootSeen, reader.namespace = true, start.Name.Space
		} else if start.Name.Space != reader.namespace {
			if err := reader.decoder.Skip(); err != nil {
				return nil, err
			}
			continue
		}
		attributes := make([]xml.Attr, 0, len(start.Attr))
		for _, attr := range start.Attr {
			if attr.Name.Space == "" && attr.Name.Local != xmlNamespaceAttribute {
				attributes = append(attributes, attr)
			}
		}
		start.Attr = attributes
		return start, nil
	}
}

func fddChildContent(parentContent, name string) string {
	if parentContent == timeElement || parentContent == "tags" {
		return "dataTypeSemantics"
	}
	if name == "dimensions" && (parentContent == attributeElement || parentContent == interactionClassElement) {
		return "dimensionRefs"
	}
	if name == "dimension" && parentContent == "dimensionRefs" ||
		name == "transportation" && parentContent != "transportations" ||
		name == "enumerator" && parentContent == "alternative" {
		return ""
	}
	if fddExtensionModels[name] != nil {
		return name
	}
	return ""
}
