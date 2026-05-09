// TASK-205½ (M21) — FOM-driven name → handle tables for the SDK.
//
// The wire surface for declaration / send-interaction RPCs uses
// integer handles, not strings. Build deterministic name→handle
// tables once at JoinFederation time by parsing the same FOM
// modules the server received. Server-side rti/cmd/rtid/foms.go
// uses the same parser + index-based handle assignment, so client
// + server agree on every (class, parameter, attribute) handle.

package federate

import (
	"fmt"

	"github.com/cbchoi/gorti/rti/pkg/fom/mim"
	"github.com/cbchoi/gorti/rti/pkg/fom/model"
	"github.com/cbchoi/gorti/rti/pkg/fom/parser"
)

// fomTables holds the per-federation handle resolution maps.
// Read-only after construction; safe for concurrent reads.
type fomTables struct {
	// Forward (name → handle).
	interactionByName map[string]uint64
	paramByName       map[string]map[string]uint64 // class name → parameter name → handle

	// Reverse (handle → name) — needed by the events drainer to
	// translate ReceiveInteraction's wire handles back to names.
	interactionByHandle map[uint64]string
	paramByHandle       map[uint64]map[uint64]string // class handle → parameter handle → name
}

// fomParseModule mirrors parser.Module but lives in this package so
// callers don't need to import the parser directly.
type fomParseModule struct {
	Path string
	XML  []byte
}

// buildFOMTables parses the supplied modules + applies MIM merge,
// then enumerates classes and parameters in the same order the
// rtid server's fomHandle does (parser order, post-MIM-merge).
//
// The match condition is index-based: handle = position + 1. As
// long as the server applied the same parser + MIM rules to the
// same module bytes, both sides agree.
func buildFOMTables(modules []fomParseModule) (*fomTables, error) {
	tables := &fomTables{
		interactionByName:   map[string]uint64{},
		paramByName:         map[string]map[string]uint64{},
		interactionByHandle: map[uint64]string{},
		paramByHandle:       map[uint64]map[uint64]string{},
	}
	if len(modules) == 0 {
		// Empty FOM is valid (the federation manager treats it as
		// "no FOM constraints"). Return empty tables; lookups will
		// miss and the SDK surfaces a clear error.
		return tables, nil
	}
	parserMods := make([]parser.Module, len(modules))
	for i, m := range modules {
		parserMods[i] = parser.Module{Path: m.Path, XML: m.XML}
	}
	res, err := parser.Parse(parserMods)
	if err != nil {
		return nil, err
	}
	if len(res.Diagnostics) > 0 {
		return nil, fmt.Errorf("FOM validation failed: %s", res.Diagnostics[0].Code)
	}
	fm, ok := res.FOM.(*model.FOM)
	if !ok {
		return nil, fmt.Errorf("parser returned unexpected FOM type %T", res.FOM)
	}
	mimFOM, mimErr := mim.StandardMIMHandle()
	if mimErr == nil && mimFOM != nil {
		merged, diags := mim.Merge(mimFOM, fm)
		if len(diags) > 0 {
			return nil, fmt.Errorf("MIM merge: %s", diags[0].Code)
		}
		fm = merged
	}
	for i, ic := range fm.InteractionClasses() {
		handle := uint64(i + 1)
		tables.interactionByName[ic.Name] = handle
		tables.interactionByHandle[handle] = ic.Name
		params := map[string]uint64{}
		paramRev := map[uint64]string{}
		for j, p := range ic.Parameters {
			ph := uint64(j + 1)
			params[p.Name] = ph
			paramRev[ph] = p.Name
		}
		tables.paramByName[ic.Name] = params
		tables.paramByHandle[handle] = paramRev
	}
	return tables, nil
}

// interactionHandle resolves an interaction class name to its wire handle.
func (t *fomTables) interactionHandle(name string) (uint64, bool) {
	if t == nil {
		return 0, false
	}
	h, ok := t.interactionByName[name]
	return h, ok
}

// parameterHandle resolves (className, paramName) → wire param handle.
func (t *fomTables) parameterHandle(className, paramName string) (uint64, bool) {
	if t == nil {
		return 0, false
	}
	m, ok := t.paramByName[className]
	if !ok {
		return 0, false
	}
	h, ok := m[paramName]
	return h, ok
}

// interactionName resolves a wire handle back to its name.
func (t *fomTables) interactionName(h uint64) (string, bool) {
	if t == nil {
		return "", false
	}
	n, ok := t.interactionByHandle[h]
	return n, ok
}

// parameterName resolves (classHandle, paramHandle) → param name.
func (t *fomTables) parameterName(classHandle, paramHandle uint64) (string, bool) {
	if t == nil {
		return "", false
	}
	m, ok := t.paramByHandle[classHandle]
	if !ok {
		return "", false
	}
	n, ok := m[paramHandle]
	return n, ok
}
