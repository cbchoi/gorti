// Package parser parses IEEE 1516-2010 DIF XML FOM modules and reports
// numbered diagnostics (FOM-NNN) for validation failures.
//
// IMPORTANT (Agent B): the public API surface declared here — Parse and the
// Result/Diagnostic types — is part of the M0 contract. Bodies are stubs;
// fill them in test-first per docs/TDD.md. The signatures themselves should
// not change without a contract-change-request.
package parser

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"

	"github.com/cbchoi/gorti/rti/pkg/fom/model"
)

// ErrNotImplemented is retained for callers that compare against it during
// the TDD-red phase of yet-to-implement features. The Parse happy path no
// longer returns it as of TASK-001.
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
		objectClasses      []model.ObjectClass
		interactionClasses []model.InteractionClass
		dataTypes          []model.DataType
		diagnostics        []Diagnostic
	)

	for _, m := range modules {
		om, err := decodeModule(m)
		if err != nil {
			return Result{}, fmt.Errorf("parse module %q: %w", m.Path, err)
		}
		var (
			modObjects      []model.ObjectClass
			modInteractions []model.InteractionClass
		)
		if om.Objects != nil {
			flattenObjectClasses(om.Objects.ObjectClass, "", &modObjects)
		}
		if om.Interactions != nil {
			flattenInteractionClasses(om.Interactions.InteractionClass, "", &modInteractions)
		}
		modDataTypes := convertDataTypes(om.DataTypes)

		modFOM := model.NewFOM(modObjects, modInteractions, modDataTypes)
		modDiags := runDiagnosers(diagnosticInput{
			modulePath: m.Path,
			xml:        m.XML,
			fom:        modFOM,
			raw:        om,
		})
		if len(modDiags) == 0 {
			modDiags = mergeWithMIM(m.Path, m.XML, modFOM)
		}
		diagnostics = append(diagnostics, modDiags...)

		objectClasses = append(objectClasses, modObjects...)
		interactionClasses = append(interactionClasses, modInteractions...)
		dataTypes = append(dataTypes, modDataTypes...)
	}

	if len(diagnostics) > 0 {
		return Result{Diagnostics: diagnostics}, nil
	}
	fom := model.NewFOM(objectClasses, interactionClasses, dataTypes)
	return Result{FOM: fom}, nil
}

// decodeModule applies a strict-mode XML decoder to one module. Strictness
// for cut-1 means: malformed XML (mis-balanced tags, truncated input, etc.)
// returns an error. Schema-level validation (unknown elements, required
// attributes) is handled by later TASK-002..009 diagnostic passes.
func decodeModule(m Module) (*xmlObjectModel, error) {
	dec := xml.NewDecoder(bytes.NewReader(m.XML))
	var om xmlObjectModel
	if err := dec.Decode(&om); err != nil {
		return nil, err
	}
	return &om, nil
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
