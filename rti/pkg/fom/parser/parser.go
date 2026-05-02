// Package parser parses IEEE 1516-2010 DIF XML FOM modules and reports
// numbered diagnostics (FOM-NNN) for validation failures.
//
// IMPORTANT (Agent B): the public API surface declared here — Parse and the
// Result/Diagnostic types — is part of the M0 contract. Bodies are stubs;
// fill them in test-first per docs/TDD.md. The signatures themselves should
// not change without a contract-change-request.
package parser

import (
	"encoding/xml"
	"errors"
	"fmt"

	"github.com/cbchoi/gorti/rti/pkg/fom/model"
)

// ErrNotImplemented is returned by stub functions until Agent B implements them.
// Spec tests in tests/spec/M1/ will fail with this error initially (as expected
// for TDD red), then turn green as functionality is added.
var ErrNotImplemented = errors.New("parser: not implemented (Agent B M1 deliverable)")

// Result is the outcome of parsing one or more FOM modules.
type Result struct {
	// FOM is the immutable parsed model. Nil on failure.
	// Type is intentionally left as `any` at the M0 contract layer; the model
	// package is owned by Agent B and may evolve without breaking this signature.
	FOM any
	// Diagnostics lists every validation issue found, in source order.
	Diagnostics []Diagnostic
}

// Diagnostic is one validation finding.
type Diagnostic struct {
	// Code is a stable identifier (e.g. "FOM-001"). See docs/idd.md §1.2.1.
	Code string
	// Message is human-readable.
	Message string
	// ModulePath identifies which FOMModule caused the diagnostic, when known.
	ModulePath string
	// Line is the 1-based source line, or 0 if unknown.
	Line int
}

// Parse parses one or more FOM modules. Each entry's Path is informational;
// XML is the bytes to validate.
//
// Returns a Result whose Diagnostics is empty on success. On any validation
// failure the FOM field is nil; callers must inspect Diagnostics. The error
// return is reserved for I/O / encoding failures unrelated to FOM content.
func Parse(modules []Module) (Result, error) {
	var (
		objectClasses      []*model.ObjectClass
		interactionClasses []*model.InteractionClass
		diagnostics        []Diagnostic
	)

	for _, m := range modules {
		diagnostics = append(diagnostics, checkStrict(m.Path, m.XML)...)

		var doc xmlObjectModel
		if err := xml.Unmarshal(m.XML, &doc); err != nil {
			return Result{}, fmt.Errorf("parser: %s: %w", m.Path, err)
		}
		moduleObjects := flattenObjectClasses(doc.Objects.Roots)
		moduleInteractions := flattenInteractionClasses(doc.Interactions.Roots)
		declared := declaredDataTypeNames(doc.DataTypes)

		diagnostics = append(diagnostics, checkMIMRedefinition(m.Path, declared)...)
		diagnostics = append(diagnostics, checkVariantDiscriminators(m.Path, doc.DataTypes.VariantRecordDataTypes)...)
		diagnostics = append(diagnostics, checkStructure(m.Path, moduleObjects, moduleInteractions, declared)...)

		objectClasses = append(objectClasses, moduleObjects...)
		interactionClasses = append(interactionClasses, moduleInteractions...)
	}

	if len(diagnostics) > 0 {
		return Result{Diagnostics: diagnostics}, nil
	}
	fom := model.NewFOM(objectClasses, interactionClasses, nil)
	return Result{FOM: fom}, nil
}

// checkStructure runs the per-module structural validation passes that all
// depend on the flattened model: cycles (FOM-002), multi-parents (FOM-003),
// attribute dups (FOM-004), parameter dups (FOM-005), object parents
// (FOM-011), interaction parents (FOM-012), dataType refs (FOM-001).
func checkStructure(path string, objects []*model.ObjectClass, interactions []*model.InteractionClass, declared map[string]struct{}) []Diagnostic {
	var out []Diagnostic
	out = append(out, checkCycles(path, objects)...)
	out = append(out, checkMultipleParents(path, objects)...)
	out = append(out, checkDuplicateAttributes(path, objects)...)
	out = append(out, checkDuplicateParameters(path, interactions)...)
	out = append(out, checkParents(path, objects)...)
	out = append(out, checkInteractionParents(path, interactions)...)
	out = append(out, checkDataTypeRefs(path, objects, interactions, declared)...)
	return out
}

// Module is one FOM XML module submitted to Parse.
type Module struct {
	Path string
	XML  []byte
}

// HasCode returns true if any diagnostic in r matches the given FOM code.
// Convenience helper for tests; agents may rely on this signature.
func (r Result) HasCode(code string) bool {
	for _, d := range r.Diagnostics {
		if d.Code == code {
			return true
		}
	}
	return false
}

// CodeCounts returns a count of each diagnostic code in r. Useful in tests
// to assert exact diagnostic shape without ordering brittleness.
func (r Result) CodeCounts() map[string]int {
	out := make(map[string]int)
	for _, d := range r.Diagnostics {
		out[d.Code]++
	}
	return out
}
