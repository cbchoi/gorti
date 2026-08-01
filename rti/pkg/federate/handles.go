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
	"strings"

	"github.com/cbchoi/gorti/rti/pkg/fom/mim"
	"github.com/cbchoi/gorti/rti/pkg/fom/model"
	"github.com/cbchoi/gorti/rti/pkg/fom/parser"
)

// fomTables holds the per-federation handle resolution maps.
// Read-only after construction; safe for concurrent reads.
type fomTables struct {
	// Forward (name → handle).
	objectByName      map[string]uint64
	attrByName        map[string]map[string]uint64
	interactionByName map[string]uint64
	paramByName       map[string]map[string]uint64 // class name → parameter name → handle

	// Reverse (handle → name) — needed by the events drainer to
	// translate ReceiveInteraction's wire handles back to names.
	objectByHandle      map[uint64]string
	attrByHandle        map[uint64]map[uint64]string
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
		objectByName:        map[string]uint64{},
		attrByName:          map[string]map[string]uint64{},
		objectByHandle:      map[uint64]string{},
		attrByHandle:        map[uint64]map[uint64]string{},
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
	objectClasses := fm.ObjectClasses()
	for i, oc := range objectClasses {
		handle := uint64(i + 1)
		tables.objectByName[oc.Name] = handle
		tables.objectByHandle[handle] = oc.Name

		attrs := map[string]uint64{}
		attrRev := map[uint64]string{}
		offset := 0
		visited := map[int]bool{}
		for cur := i; cur >= 0 && cur < len(objectClasses) && !visited[cur]; {
			visited[cur] = true
			for j, attr := range objectClasses[cur].Attributes {
				ah := uint64(offset + j + 1)
				if _, exists := attrs[attr.Name]; !exists {
					attrs[attr.Name] = ah
				}
				attrRev[ah] = attr.Name
				if attr.Name == "HLAprivilegeToDeleteObject" {
					if _, exists := attrs["HLAprivilegeToDelete"]; !exists {
						attrs["HLAprivilegeToDelete"] = ah
					}
				}
			}
			offset += len(objectClasses[cur].Attributes)
			cur = objectClassIndexByName(objectClasses, objectClasses[cur].ParentName)
		}
		tables.attrByName[oc.Name] = attrs
		tables.attrByHandle[handle] = attrRev
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

func objectClassIndexByName(classes []model.ObjectClass, name string) int {
	if name == "" {
		return -1
	}
	if dot := strings.LastIndexByte(name, '.'); dot >= 0 {
		name = name[dot+1:]
	}
	for i, class := range classes {
		if class.Name == name {
			return i
		}
	}
	return -1
}

// objectClassHandle resolves an object class name to its wire handle.
func (t *fomTables) objectClassHandle(name string) (uint64, bool) {
	if t == nil {
		return 0, false
	}
	h, ok := t.objectByName[name]
	return h, ok
}

// attributeHandle resolves (className, attributeName) to a wire handle.
func (t *fomTables) attributeHandle(className, attributeName string) (uint64, bool) {
	if t == nil {
		return 0, false
	}
	m, ok := t.attrByName[className]
	if !ok {
		return 0, false
	}
	h, ok := m[attributeName]
	return h, ok
}

// objectClassName resolves a wire object class handle back to its name.
func (t *fomTables) objectClassName(h uint64) (string, bool) {
	if t == nil {
		return "", false
	}
	n, ok := t.objectByHandle[h]
	return n, ok
}

// attributeName resolves (classHandle, attributeHandle) to an attribute name.
func (t *fomTables) attributeName(classHandle, attributeHandle uint64) (string, bool) {
	if t == nil {
		return "", false
	}
	m, ok := t.attrByHandle[classHandle]
	if !ok {
		return "", false
	}
	n, ok := m[attributeHandle]
	return n, ok
}

// ObjectClassHandle resolves an object class name from the FOM supplied when
// the federate joined.
func (f *Federate) ObjectClassHandle(className string) (uint64, bool) {
	return f.handles.objectClassHandle(className)
}

// AttributeHandle resolves an attribute name within an object class from the
// FOM supplied when the federate joined.
func (f *Federate) AttributeHandle(classHandle uint64, attributeName string) (uint64, bool) {
	className, ok := f.handles.objectClassName(classHandle)
	if !ok {
		return 0, false
	}
	return f.handles.attributeHandle(className, attributeName)
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

// InteractionClassHandle resolves an interaction class name from the FOM
// supplied when the federate joined. The returned handle can be cached and
// passed to SendInteractionByHandle.
func (f *Federate) InteractionClassHandle(className string) (uint64, bool) {
	return f.handles.interactionHandle(className)
}

// ParameterHandle resolves a parameter name within an interaction class from
// the FOM supplied when the federate joined. The returned handle can be cached
// and used as a key in the parameter map passed to SendInteractionByHandle.
func (f *Federate) ParameterHandle(classHandle uint64, parameterName string) (uint64, bool) {
	className, ok := f.handles.interactionName(classHandle)
	if !ok {
		return 0, false
	}
	return f.handles.parameterHandle(className, parameterName)
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
