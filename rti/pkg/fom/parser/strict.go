package parser

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
)

// strictValidator emits FOM-009 for unknown XML elements. The legacy
// vocabulary is supplemented by context-specific FDD 2010 metadata rules;
// neither pass is a complete XML schema validator.
//
// We walk the raw XML token stream rather than relying on encoding/xml's
// Unmarshal — Unmarshal silently discards unknown nodes, so structural
// validation against the whitelist must run independently.
type strictValidator struct{}

func init() {
	diagnosers = append(diagnosers, strictValidator{})
}

// Run validates HLA vocabulary, sharing values, and the placement of opaque
// foreign subtrees against the original XML, before admitting a semantic model.
func (strictValidator) Run(in diagnosticInput) []Diagnostic {
	if len(in.xml) == 0 {
		return nil
	}
	var diags []Diagnostic
	dec := xml.NewDecoder(bytes.NewReader(in.xml))
	var stack []strictElement
	report := func(message string) {
		diags = append(diags, Diagnostic{
			Code:       invalidStructureCode,
			Message:    message,
			ModulePath: in.modulePath,
			Line:       lineForOffset(in.xml, dec.InputOffset()),
		})
	}
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			// Malformed XML is reported elsewhere (decodeModule); strict
			// validation stops here without a synthetic diagnostic.
			return diags
		}
		switch se := tok.(type) {
		case xml.StartElement:
			var valid bool
			stack, valid = validateStrictStart(dec, stack, se, report)
			if !valid {
				return diags
			}
		case xml.EndElement:
			validateSharingEnd(stack, report)
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if len(stack) > 0 && stack[len(stack)-1].name.Local == sharingElement {
				stack[len(stack)-1].text += string(se)
			}
			if problem := validateFDDMetadataText(stack, string(se)); problem != "" {
				report(problem)
			}
		}
	}
	return diags
}

// lineForOffset returns the 1-based line number containing byte offset off.
// Decoder's InputOffset is the byte position just past the most recent
// token; we walk from start-of-file counting newlines up to off.
func lineForOffset(src []byte, off int64) int {
	if off > int64(len(src)) {
		off = int64(len(src))
	}
	line := 1
	for i := int64(0); i < off; i++ {
		if src[i] == '\n' {
			line++
		}
	}
	return line
}

// annexAElements is the closed whitelist of element names defined in
// IEEE 1516-2010 DIF Annex A. Adding to this set is a contract change.
//
// Mapping is to struct{} for membership test; values intentionally empty.
var annexAElements = map[string]struct{}{
	// Root + top-level sections.
	objectModelElement:    {},
	"modelIdentification": {},
	objectsElement:        {},
	interactionsElement:   {},
	"dimensions":          {},
	"timeRepresentation":  {},
	"userSuppliedTags":    {},
	"tags":                {},
	"synchronizations":    {},
	"transportations":     {},
	"switches":            {},
	"updateRates":         {},
	"dataTypes":           {},
	"notes":               {},

	// Identification block.
	"name":                   {},
	"type":                   {},
	"version":                {},
	"modificationDate":       {},
	"securityClassification": {},
	"releaseRestriction":     {},
	"purpose":                {},
	"applicationDomain":      {},
	"description":            {},
	"useLimitation":          {},
	"useHistory":             {},
	"keyword":                {},
	"taxonomy":               {},
	"keywordValue":           {},
	"poc":                    {},
	"pocType":                {},
	"pocName":                {},
	"pocOrg":                 {},
	"pocTelephone":           {},
	"pocEmail":               {},
	"reference":              {},
	"referenceType":          {},
	"identification":         {},
	"other":                  {},
	"glyph":                  {},

	// Annotations: <notes> (the container) is in the top-level section
	// list above; <note> is each individual annotation entry, allowed
	// across most parent elements per DIF Annex A.
	"note": {},

	// Objects / interactions.
	objectClassElement:      {},
	interactionClassElement: {},
	sharingElement:          {},
	"semantics":             {},
	attributeElement:        {},
	parameterElement:        {},

	// Per-attribute / per-interaction descriptors.
	"dataType":        {},
	"updateType":      {},
	"updateCondition": {},
	"ownership":       {},
	"transportation":  {},
	"order":           {},
	"dimensionRefs":   {},
	"dimensionRef":    {},
	"dimension":       {},
	"upperBound":      {},
	"normalization":   {},
	"value":           {},

	// Data types: containers + items.
	"basicDataRepresentations": {},
	"basicData":                {},
	"size":                     {},
	"interpretation":           {},
	"endian":                   {},
	"encoding":                 {},

	"simpleDataTypes": {},
	"simpleData":      {},
	"representation":  {},
	"units":           {},
	"resolution":      {},
	"accuracy":        {},

	"enumeratedDataTypes": {},
	"enumeratedData":      {},
	"enumerator":          {},
	"values":              {},

	"arrayDataTypes": {},
	"arrayData":      {},
	"cardinality":    {},

	"fixedRecordDataTypes": {},
	"fixedRecordData":      {},
	"field":                {},

	"variantRecordDataTypes": {},
	"variantRecordData":      {},
	"discriminant":           {},
	"alternative":            {},

	// Time representation.
	"timeStamp":    {},
	"timeInterval": {},
	"epoch":        {},

	// Synchronizations / transportations / switches.
	"synchronization":             {},
	"capability":                  {},
	"label":                       {},
	"tag":                         {},
	"updateReflectTag":            {},
	"sendReceiveTag":              {},
	"deleteRemoveTag":             {},
	"divestitureRequestTag":       {},
	"divestitureCompletionTag":    {},
	"acquisitionRequestTag":       {},
	"requestUpdateTag":            {},
	"transportType":               {},
	"reliable":                    {},
	"autoProvide":                 {},
	"conveyRegionDesignatorSets":  {},
	"conveyProducingFederate":     {},
	"serviceReporting":            {},
	"exceptionReporting":          {},
	"delaySubscriptionEvaluation": {},
	"automaticResignAction":       {},
	// Advisory switches per IEEE 1516.2-2010 §F.10
	"attributeScopeAdvisory":       {},
	"attributeRelevanceAdvisory":   {},
	"objectClassRelevanceAdvisory": {},
	"interactionRelevanceAdvisory": {},

	// Update rate.
	"updateRate": {},
	"rate":       {},
}

func validateStrictStart(dec *xml.Decoder, stack []strictElement, se xml.StartElement, report func(string)) ([]strictElement, bool) {
	content := ""
	if len(stack) == 0 {
		if se.Name.Local == objectModelElement && se.Name.Space == fddNamespace {
			content = objectModelElement
		}
	} else {
		parent := &stack[len(stack)-1]
		if parent.name.Local == sharingElement {
			report("sharing enumeration does not allow child elements")
		}
		if se.Name.Space != stack[0].name.Space {
			problem := parent.foreignElementProblem(se.Name)
			if se.Name.Space == "" || stack[0].name.Space != fddNamespace {
				problem = fmt.Sprintf("element %q has an invalid namespace for an FDD extension", se.Name.Local)
			}
			if problem != "" {
				report(problem)
			}
			parent.extensionSeen = true
			if err := dec.Skip(); err != nil {
				return stack, false
			}
			return stack, true
		}
		if parent.extensionSeen {
			report(fmt.Sprintf("element %q follows the final FDD extension slot inside %q", se.Name.Local, parent.name.Local))
		}
		content = parent.fddChild(se.Name.Local)
	}
	validateStrictVocabulary(stack, se, report)
	stack = append(stack, strictElement{name: se.Name, content: content})
	return stack, true
}

func validateSharingEnd(stack []strictElement, report func(string)) {
	if len(stack) >= 2 && stack[len(stack)-1].name.Local == sharingElement {
		parent := stack[len(stack)-2].name.Local
		if parent == objectClassElement || parent == interactionClassElement || parent == attributeElement {
			value := stack[len(stack)-1].text
			switch value {
			case "Publish", "Subscribe", sharingPublishSubscribe, "Neither":
			case "":
				if parent != objectClassElement {
					report("empty sharing enumeration")
				}
			default:
				report(fmt.Sprintf("invalid sharing enumeration %q", value))
			}
		}
	}
}

func validateStrictVocabulary(stack []strictElement, se xml.StartElement, report func(string)) {
	scoped, problem := validateFDDMetadataElement(stack, se)
	if problem != "" {
		report(problem)
	} else if !scoped {
		if _, allowed := annexAElements[se.Name.Local]; !allowed {
			report(fmt.Sprintf("unknown XML element %q (strict mode: not in supported IEEE 1516-2010 vocabulary)", se.Name.Local))
		}
	}
}
