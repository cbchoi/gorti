package parser

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
)

// strictValidator emits FOM-009 for any XML element name not in the
// IEEE 1516-2010 DIF Annex A whitelist. Strict per docs/srs.md FR-FOM-1
// and TASK-006: there is no permissive fallback.
//
// We walk the raw XML token stream rather than relying on encoding/xml's
// Unmarshal — Unmarshal silently discards unknown nodes, so structural
// validation against the whitelist must run independently.
type strictValidator struct{}

func init() {
	diagnosers = append(diagnosers, strictValidator{})
}

// Run scans every start-element name encountered in the source bytes; any
// name not in the whitelist becomes a FOM-009.
func (strictValidator) Run(in diagnosticInput) []Diagnostic {
	if len(in.xml) == 0 {
		return nil
	}
	var diags []Diagnostic
	dec := xml.NewDecoder(bytes.NewReader(in.xml))
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
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		name := se.Name.Local
		if _, allowed := annexAElements[name]; !allowed {
			line := 0
			if off := dec.InputOffset(); off != 0 {
				line = lineForOffset(in.xml, off)
			}
			diags = append(diags, Diagnostic{
				Code:       "FOM-009",
				Message:    fmt.Sprintf("unknown XML element %q (strict mode: not in IEEE 1516-2010 DIF Annex A)", name),
				ModulePath: in.modulePath,
				Line:       line,
			})
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
	"objectModel":         {},
	"modelIdentification": {},
	"objects":             {},
	"interactions":        {},
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
	"objectClass":      {},
	"interactionClass": {},
	"sharing":          {},
	"semantics":        {},
	"attribute":        {},
	"parameter":        {},

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
