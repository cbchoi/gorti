package parser

import (
	"fmt"
	"sort"

	"github.com/cbchoi/gorti/rti/pkg/fom/model"
)

// cycleValidator emits FOM-002 when the object class parent graph contains
// a cycle. A cycle includes:
//   - a self-loop (Key() == ParentKey());
//   - any back-edge in the parent chain (A -> B -> ... -> A).
//
// Each class key maps to the union of parents across its declarations.
// Equal XML leaf names in different branches have distinct keys; legacy
// programmatic Name/ParentName graphs can still declare cyclic edges.
type cycleValidator struct{}

func init() {
	diagnosers = append(diagnosers, cycleValidator{})
}

func (cycleValidator) Run(in diagnosticInput) []Diagnostic {
	if in.fom == nil {
		return nil
	}
	parents := buildObjectClassParentSets(in.fom.ObjectClasses())
	var diags []Diagnostic
	seenCycle := map[string]struct{}{}
	for _, name := range sortedKeys(parents) {
		if _, dup := seenCycle[name]; dup {
			continue
		}
		if cycleStart, ok := findCycle(name, parents); ok {
			seenCycle[cycleStart] = struct{}{}
			diags = append(diags, Diagnostic{
				Code:       "FOM-002",
				Message:    fmt.Sprintf("object class hierarchy contains a cycle through %q", cycleStart),
				ModulePath: in.modulePath,
			})
		}
	}
	return diags
}

// buildObjectClassParentSets returns class key -> set-of-parent-keys for every
// object class in classes. The empty parent (root) is excluded from the
// set so traversal naturally terminates at HLAobjectRoot.
func buildObjectClassParentSets(classes []model.ObjectClass) map[string]map[string]struct{} {
	out := map[string]map[string]struct{}{}
	for _, oc := range classes {
		classKey, parentKey := oc.Key(), oc.ParentKey()
		if _, ok := out[classKey]; !ok {
			out[classKey] = map[string]struct{}{}
		}
		if parentKey != "" {
			out[classKey][parentKey] = struct{}{}
		}
	}
	return out
}

// findCycle does DFS from start through every parent edge; if it ever
// re-encounters a node already on the active stack, returns the offending
// name. Self-loops also count.
func findCycle(start string, parents map[string]map[string]struct{}) (string, bool) {
	stack := map[string]struct{}{}
	var visit func(node string) (string, bool)
	visit = func(node string) (string, bool) {
		if _, on := stack[node]; on {
			return node, true
		}
		stack[node] = struct{}{}
		defer delete(stack, node)
		for _, p := range sortedKeys(parents[node]) {
			if hit, ok := visit(p); ok {
				return hit, true
			}
		}
		return "", false
	}
	return visit(start)
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
