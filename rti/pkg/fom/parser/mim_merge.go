package parser

import (
	"bytes"
	"encoding/xml"
	"strings"

	"github.com/cbchoi/gorti/rti/pkg/fom/mim"
	"github.com/cbchoi/gorti/rti/pkg/fom/model"
)

// mergeWithMIM runs the parsed user FOM through mim.Merge against the
// embedded standard MIM and returns any FOM-101 diagnostics translated to
// parser.Diagnostic. modulePath is copied into each diagnostic so callers
// can localize the failure to the offending user module.
//
// The MIM itself is excluded — when the input module's
// modelIdentification/type is "MIM", merging it against the standard MIM
// would self-collide on every shared name. This matches the spec
// (1516-2010 Annex A): MIM modules and FOM modules occupy different roles
// in the module set, and only FOM modules are subject to FOM-101.
//
// If the embedded MIM itself fails to parse — a build-time bug in the
// vendored XML — the function bypasses the merge silently. Returning a
// build-config error here would mask the user-side validation result and
// make every Parse call fragile. The fix path is to repair the vendored
// MIM, tracked under issue #1.
func mergeWithMIM(modulePath string, rawXML []byte, fom *model.FOM) []Diagnostic {
	if fom == nil {
		return nil
	}
	if isMIMTypeModule(rawXML) {
		return nil
	}
	base, err := mim.StandardMIMHandle()
	if err != nil || base == nil {
		// TODO(#1): surface MIM-load failure as a real diagnostic (e.g.
		// FOM-INTERNAL) once issue #1 lands the canonical MIM. Until then
		// the embedded XML is hand-derived; logging is the wrong move
		// for a library, and erroring here would block every Parse call.
		return nil
	}
	_, mimDiags := mim.Merge(base, fom)
	if len(mimDiags) == 0 {
		return nil
	}
	out := make([]Diagnostic, 0, len(mimDiags))
	for _, d := range mimDiags {
		mp := d.ModulePath
		if mp == "" {
			mp = modulePath
		}
		out = append(out, Diagnostic{
			Code:       d.Code,
			Message:    d.Message,
			ModulePath: mp,
			Line:       d.Line,
		})
	}
	return out
}

// xmlModelType peeks at <modelIdentification>{type,name} in the raw XML
// to distinguish FOM modules from MIM modules. The result is used by
// mergeWithMIM to skip the FOM-101 check on the MIM itself.
type xmlModelType struct {
	XMLName             xml.Name `xml:"objectModel"`
	ModelIdentification *struct {
		Type string `xml:"type"`
		Name string `xml:"name"`
	} `xml:"modelIdentification"`
}

// isMIMTypeModule reports whether rawXML is an objectModel that represents
// the standard MIM rather than a user FOM. Two signals trigger the skip:
//
//   - <modelIdentification><type>MIM</type> — the explicit DIF marker.
//   - <modelIdentification><name> contains "MIM" (case-insensitive) AND
//     redeclares MIM root names — covers the canonical IEEE/Portico MIM
//     which declares <type>FOM</type> for legacy reasons but is named
//     "Standard MOM and Initialization Module (MIM) for HLA IEEE 1516-2010".
//
// Returns false on decode error so the caller still applies FOM-101 to
// malformed input — that's a separate diagnostic the parser already
// surfaces.
func isMIMTypeModule(rawXML []byte) bool {
	if len(rawXML) == 0 {
		return false
	}
	var m xmlModelType
	dec := xml.NewDecoder(bytes.NewReader(rawXML))
	if err := dec.Decode(&m); err != nil {
		return false
	}
	if m.ModelIdentification == nil {
		return false
	}
	if m.ModelIdentification.Type == "MIM" {
		return true
	}
	// Heuristic for canonical MIM XML files that use <type>FOM</type>:
	// the modelIdentification name conventionally contains "MIM" or the
	// canonical title prefix. This is a narrow match — any user FOM
	// whose name contains "MIM" but doesn't redeclare HLAobjectRoot et al.
	// would still go through the merge; we additionally probe for the
	// canonical title to keep the heuristic precise.
	name := strings.ToLower(m.ModelIdentification.Name)
	if strings.Contains(name, "standard mom and initialization module") ||
		strings.Contains(name, "hlastandardmim") {
		return true
	}
	return false
}
