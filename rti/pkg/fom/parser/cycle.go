package parser

import (
	"fmt"
	"sort"

	"github.com/cbchoi/gorti/rti/pkg/fom/model"
)

// cycleValidator emits FOM-002 when the object class parent graph contains
// a cycle. A cycle includes:
//   - a self-loop (Name == ParentName);
//   - any back-edge in the parent chain (A -> B -> ... -> A).
//
// Because the structural walk may produce multiple flat entries for the
// same class name (e.g. A nested both directly under HLAobjectRoot and
// re-declared inside B), the graph is the union: each class name maps to
// the set of declared parents observed across all entries.
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

// buildObjectClassParentSets returns name -> set-of-parent-names for every
// object class in classes. The empty parent (root) is excluded from the
// set so traversal naturally terminates at HLAobjectRoot.
func buildObjectClassParentSets(classes []model.ObjectClass) map[string]map[string]struct{} {
	out := map[string]map[string]struct{}{}
	for _, oc := range classes {
		if _, ok := out[oc.Name]; !ok {
			out[oc.Name] = map[string]struct{}{}
		}
		if oc.ParentName != "" {
			out[oc.Name][oc.ParentName] = struct{}{}
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
