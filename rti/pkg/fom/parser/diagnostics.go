// Diagnostic-pipeline registry. Each FOM-NNN validator lives in its own file
// (e.g. datatype_ref.go, cycle.go, ...) and registers a diagnoser via init().
// Parse runs the registered diagnosers after the structural XML walk and
// aggregates their findings into Result.Diagnostics.
//
// This is an intentionally tiny abstraction (see docs/AGENTS.md §8: "no
// abstraction layers for future flexibility"). It is justified by the count:
// at M1 we land nine diagnostic codes in parallel sub-branches, and routing
// each through a registry keeps Parse's body small and avoids merge
// conflicts when adding the tenth (TASK-009 / FOM-101 / MIM-redefine) and
// future post-M1 codes (FOM-006, 007, 008, 010, 014, 200+).
//
// Adding a new diagnoser: create parser/<short>.go, declare a type with a
// Run method matching the diagnoser interface, register it in the file's
// init(). No edit to parser.Parse needed.

package parser

import (
	"github.com/cbchoi/gorti/rti/pkg/fom/model"
)

// diagnosticInput is the read-only context handed to each diagnoser.
//
// modulePath is the caller-supplied Module.Path (informational, copied into
// each Diagnostic the pass emits). fom is the fully assembled FOM for this
// module after the structural XML walk. raw is the underlying xmlObjectModel
// produced by encoding/xml decoding (never nil at call time). xml is the
// original module bytes, available for passes that need token-level
// visibility (the structural model has flattened nesting away).
type diagnosticInput struct {
	modulePath string
	xml        []byte
	fom        *model.FOM
	raw        *xmlObjectModel
}

// diagnoser is one validation pass over a single parsed module. Run returns
// the diagnostics found, in source order; an empty/nil slice means the
// module passes this validation.
type diagnoser interface {
	Run(in diagnosticInput) []Diagnostic
}

// diagnosers is the package-level registry populated by init() in each
// validator file. Order is deterministic (init order = file name order
// within the package, per the Go spec) so diagnostic ordering is stable
// across builds.
//
// Per D-7 this is read-only after package init: the registry is built once
// at program start (via each validator file's init()) and never mutated at
// run-time. No goroutine in the parser writes to it.
var diagnosers []diagnoser

// runDiagnosers executes every registered diagnoser against in and returns
// the concatenated diagnostics in registration order.
func runDiagnosers(in diagnosticInput) []Diagnostic {
	if len(diagnosers) == 0 {
		return nil
	}
	var out []Diagnostic
	for _, d := range diagnosers {
		out = append(out, d.Run(in)...)
	}
	return out
}
