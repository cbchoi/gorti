// Package parser parses IEEE 1516-2010 DIF XML FOM modules and reports
// numbered diagnostics (FOM-NNN) for validation failures.
//
// IMPORTANT: the public API surface declared here — Parse and the
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

// ErrNotImplemented is retained for callers that compare against it during
// the TDD-red phase of yet-to-implement features. The Parse happy path no
// longer returns it as of TASK-001.
var ErrNotImplemented = errors.New("parser: not implemented ( M1 deliverable)")

// Result is the outcome of parsing one or more FOM modules.
type Result struct {
	// FOM is the immutable parsed model. Nil on failure.
	// Type is intentionally left as `any` at the M0 contract layer; the model
	// package may evolve internally without breaking this signature.
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
	return parseModules(modules)
}

// ParseAdditive combines class contributions using the same admission rules as
// Parse. New classes and scaffolding completion are allowed, but a complete
// class's local member set cannot change. This is not a complete FOM validator.
func ParseAdditive(modules []Module) (Result, error) {
	return parseModules(modules)
}

func parseModules(modules []Module) (Result, error) {
	var (
		objectClasses      []model.ObjectClass
		interactionClasses []model.InteractionClass
		dataTypes          []model.DataType
		diagnostics        []Diagnostic
	)

	var dimensions []model.Dimension
	var updateRates []model.UpdateRate
	var transportations []model.Transportation
	var advisorySwitches []model.AdvisorySwitch
	var automaticResign string
	var dimensionInputs []diagnosticInput

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
		modDimensions, dimensionDiags := convertDimensions(om.Dimensions)
		var updateRateDiags []Diagnostic
		updateRates, updateRateDiags = mergeUpdateRateDeclarations(m.Path, updateRates, om.UpdateRates)
		diagnostics = append(diagnostics, updateRateDiags...)
		var transportationDiags []Diagnostic
		transportations, transportationDiags = mergeTransportationDeclarations(m.Path, transportations, om.Transportations)
		diagnostics = append(diagnostics, transportationDiags...)
		var advisoryDiags []Diagnostic
		advisorySwitches, advisoryDiags = mergeAdvisoryDeclarations(m.Path, advisorySwitches, om.Switches)
		diagnostics = append(diagnostics, advisoryDiags...)
		automaticResign, advisoryDiags = mergeAutomaticResign(m.Path, automaticResign, om.Switches)
		diagnostics = append(diagnostics, advisoryDiags...)
		for i := range dimensionDiags {
			dimensionDiags[i].ModulePath = m.Path
		}
		diagnostics = append(diagnostics, dimensionDiags...)

		modFOM := model.NewFOMWithDimensions(modObjects, modInteractions, modDataTypes, modDimensions)
		dimensionInputs = append(dimensionInputs, diagnosticInput{modulePath: m.Path, fom: modFOM})
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

		var mergeDiags []Diagnostic
		objectClasses, interactionClasses, mergeDiags = mergeClassDeclarations(m.Path, objectClasses, interactionClasses, modObjects, modInteractions)
		diagnostics = append(diagnostics, mergeDiags...)
		dataTypes = append(dataTypes, modDataTypes...)
		dimensions, dimensionDiags = mergeDimensionDeclarations(m.Path, dimensions, modDimensions)
		diagnostics = append(diagnostics, dimensionDiags...)
	}

	if len(diagnostics) > 0 {
		return Result{Diagnostics: diagnostics}, nil
	}
	fom := model.NewFOMWithUpdateRates(objectClasses, interactionClasses, dataTypes, dimensions, updateRates)
	fom = fom.WithTransportations(transportations)
	fom = fom.WithAdvisorySwitches(advisorySwitches)
	fom = fom.WithAutomaticResignAction(automaticResign)
	diagnostics = append(diagnostics, validateDimensionReferences(dimensionInputs, dimensions)...)
	if len(modules) > 1 {
		merged := diagnosticInput{modulePath: "<merged>", fom: fom}
		diagnostics = append(diagnostics, (duplicateAttributeValidator{}).Run(merged)...)
		diagnostics = append(diagnostics, (duplicateParameterValidator{}).Run(merged)...)
	}
	if len(diagnostics) > 0 {
		return Result{Diagnostics: diagnostics}, nil
	}
	return Result{FOM: fom}, nil
}

// decodeModule applies a strict-mode XML decoder to one module. Strictness
// for cut-1 means: malformed XML (mis-balanced tags, truncated input, etc.)
// returns an error. Schema-level validation (unknown elements, required
// attributes) is handled by later TASK-002..009 diagnostic passes.
func decodeModule(m Module) (*xmlObjectModel, error) {
	dec := xml.NewTokenDecoder(newSemanticXMLTokens(m.XML))
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
// Convenience helper for tests; callers may rely on this signature.
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
