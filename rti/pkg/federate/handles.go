// TASK-205½ (M21) — FOM-driven name → handle tables for the SDK.
//
// The wire surface for declaration / send-interaction RPCs uses
// integer handles, not strings. Build deterministic name→handle
// tables from the authoritative Join/GetFOMView mappings. Local parsing is
// retained only for legacy servers that do not return a FOM view.

package federate

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/cbchoi/gorti/rti/pkg/fom/mim"
	"github.com/cbchoi/gorti/rti/pkg/fom/model"
	"github.com/cbchoi/gorti/rti/pkg/fom/parser"
)

// fomTables holds immutable resolution maps. A stable root atomically points
// to a newer immutable view; callbacks and public lookups share that root.
type fomTables struct {
	latest               atomic.Pointer[fomTables]
	publishMu            sync.Mutex
	refreshEpoch         atomic.Uint64
	refresh              func()
	revision             uint64
	digest               string
	objectCanonical      map[uint64]string
	interactionCanonical map[uint64]string
	dimensionByName      map[string]uint64
	dimensionByHandle    map[uint64]string
	dimensionUpperBound  map[uint64]uint64
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
func newFOMTables() *fomTables {
	return &fomTables{
		objectCanonical:      map[uint64]string{},
		interactionCanonical: map[uint64]string{},
		dimensionByName:      map[string]uint64{},
		dimensionByHandle:    map[uint64]string{},
		dimensionUpperBound:  map[uint64]uint64{},
		objectByName:         map[string]uint64{},
		attrByName:           map[string]map[string]uint64{},
		objectByHandle:       map[uint64]string{},
		attrByHandle:         map[uint64]map[uint64]string{},
		interactionByName:    map[string]uint64{},
		paramByName:          map[string]map[string]uint64{},
		interactionByHandle:  map[uint64]string{},
		paramByHandle:        map[uint64]map[uint64]string{},
	}
}

func buildFOMTables(modules []fomParseModule) (*fomTables, error) {
	tables := newFOMTables()
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
	tables.installObjectClasses(fm.ObjectClasses())
	tables.installInteractionClasses(fm.InteractionClasses())
	var handle uint64 = 1
	for _, dimension := range fm.Dimensions() {
		tables.dimensionByName[dimension.Name] = handle
		tables.dimensionByHandle[handle] = dimension.Name
		tables.dimensionUpperBound[handle] = dimension.UpperBound
		handle++
	}
	return tables, nil
}

// Zero marks an ambiguous alias and remains in the index so a later class
// cannot accidentally make that alias resolvable again.
func addClassAliases(index map[string]uint64, local, key, root string, handle uint64) {
	for _, name := range []string{key, strings.TrimPrefix(key, root+"."), local} {
		if prev, exists := index[name]; exists && prev != handle {
			index[name] = 0
		} else {
			index[name] = handle
		}
	}
}

// objectClassHandle resolves an object class name to its wire handle.
func (t *fomTables) objectClassHandle(name string) (uint64, bool) {
	return lookupFOM(t, func(current *fomTables) (uint64, bool) { h, ok := current.objectByName[name]; return h, ok && h != 0 })
}

// attributeHandle resolves (className, attributeName) to a wire handle.
func (t *fomTables) attributeHandle(className, attributeName string) (uint64, bool) {
	return lookupFOM(t, func(current *fomTables) (uint64, bool) {
		class := current.objectByName[className]
		h, ok := current.attrByName[current.objectByHandle[class]][attributeName]
		return h, ok && class != 0
	})
}

// objectClassName resolves a wire object class handle back to its name.
func (t *fomTables) objectClassName(h uint64) (string, bool) {
	return lookupFOM(t, func(current *fomTables) (string, bool) { name, ok := current.objectByHandle[h]; return name, ok })
}

// attributeName resolves (classHandle, attributeHandle) to an attribute name.
func (t *fomTables) attributeName(classHandle, attributeHandle uint64) (string, bool) {
	return lookupFOM(t, func(current *fomTables) (string, bool) {
		name, ok := current.attrByHandle[classHandle][attributeHandle]
		return name, ok
	})
}

// ObjectClassHandle resolves an object class name from the FOM supplied when
// the federate joined.
func (f *Federate) ObjectClassHandle(className string) (uint64, bool) {
	return f.handles.objectClassHandle(className)
}

// AttributeHandle resolves an attribute name within an object class from the
// FOM supplied when the federate joined.
func (f *Federate) AttributeHandle(classHandle uint64, attributeName string) (uint64, bool) {
	return lookupFOM(f.handles, func(current *fomTables) (uint64, bool) {
		h, ok := current.attrByName[current.objectByHandle[classHandle]][attributeName]
		return h, ok
	})
}

// interactionHandle resolves an interaction class name to its wire handle.
func (t *fomTables) interactionHandle(name string) (uint64, bool) {
	return lookupFOM(t, func(current *fomTables) (uint64, bool) {
		h, ok := current.interactionByName[name]
		return h, ok && h != 0
	})
}

// parameterHandle resolves (className, paramName) → wire param handle.
func (t *fomTables) parameterHandle(className, paramName string) (uint64, bool) {
	return lookupFOM(t, func(current *fomTables) (uint64, bool) {
		class := current.interactionByName[className]
		h, ok := current.paramByName[current.interactionByHandle[class]][paramName]
		return h, ok && class != 0
	})
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
	return lookupFOM(f.handles, func(current *fomTables) (uint64, bool) {
		h, ok := current.paramByName[current.interactionByHandle[classHandle]][parameterName]
		return h, ok
	})
}

// interactionName resolves a wire handle back to its name.
func (t *fomTables) interactionName(h uint64) (string, bool) {
	return lookupFOM(t, func(current *fomTables) (string, bool) { name, ok := current.interactionByHandle[h]; return name, ok })
}

// parameterName resolves (classHandle, paramHandle) → param name.
func (t *fomTables) parameterName(classHandle, paramHandle uint64) (string, bool) {
	return lookupFOM(t, func(current *fomTables) (string, bool) {
		name, ok := current.paramByHandle[classHandle][paramHandle]
		return name, ok
	})
}

func (t *fomTables) installObjectClasses(classes []model.ObjectClass) {
	indices := make(map[uint64]int, len(classes))
	var handle uint64 = 1
	for i, class := range classes {
		indices[handle] = i
		t.objectCanonical[handle] = class.Key()
		addClassAliases(t.objectByName, class.Name, class.Key(), "HLAobjectRoot", handle)
		handle++
	}
	handle = 1
	for i, class := range classes {
		name := class.Key()
		if t.objectByName[class.Name] == handle {
			name = class.Name // Preserve existing callback names when unambiguous.
		}
		t.objectByHandle[handle] = name
		t.attrByName[name], t.attrByHandle[handle] = t.inheritedAttributes(classes, indices, i)
		handle++
	}
}

func (t *fomTables) inheritedAttributes(classes []model.ObjectClass, indices map[uint64]int, current int) (map[string]uint64, map[uint64]string) {
	attrs, reverse := map[string]uint64{}, map[uint64]string{}
	visited := map[int]bool{}
	var handle uint64 = 1
	for !visited[current] {
		visited[current] = true
		for _, attr := range classes[current].Attributes {
			if _, exists := attrs[attr.Name]; !exists {
				attrs[attr.Name] = handle
			}
			reverse[handle] = attr.Name
			if attr.Name == "HLAprivilegeToDeleteObject" {
				if _, exists := attrs["HLAprivilegeToDelete"]; !exists {
					attrs["HLAprivilegeToDelete"] = handle
				}
			}
			handle++
		}
		parent, ok := t.objectClassHandle(classes[current].ParentKey())
		if !ok {
			break
		}
		current, ok = indices[parent]
		if !ok {
			break
		}
	}
	return attrs, reverse
}

func (t *fomTables) installInteractionClasses(classes []model.InteractionClass) {
	indices := make(map[uint64]int, len(classes))
	var handle uint64 = 1
	for i, class := range classes {
		indices[handle] = i
		t.interactionCanonical[handle] = class.Key()
		addClassAliases(t.interactionByName, class.Name, class.Key(), "HLAinteractionRoot", handle)
		handle++
	}
	handle = 1
	for i, class := range classes {
		name := class.Key()
		if t.interactionByName[class.Name] == handle {
			name = class.Name
		}
		t.interactionByHandle[handle] = name
		t.paramByName[name], t.paramByHandle[handle] = t.inheritedParameters(classes, indices, i)
		handle++
	}
}

func (t *fomTables) inheritedParameters(classes []model.InteractionClass, indices map[uint64]int, current int) (map[string]uint64, map[uint64]string) {
	params, reverse := map[string]uint64{}, map[uint64]string{}
	visited := map[int]bool{}
	var handle uint64 = 1
	for !visited[current] {
		visited[current] = true
		for _, parameter := range classes[current].Parameters {
			if _, exists := params[parameter.Name]; !exists {
				params[parameter.Name] = handle
			}
			reverse[handle] = parameter.Name
			handle++
		}
		parent, ok := t.interactionHandle(classes[current].ParentKey())
		if !ok {
			break
		}
		current, ok = indices[parent]
		if !ok {
			break
		}
	}
	return params, reverse
}
