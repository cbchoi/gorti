package composition

import (
	"strings"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/pkg/fom/model"
)

// Separate local and qualified namespaces preserve ambiguity and prevent a
// malformed qualified path from falling back to an unrelated leaf name.
type classNameIndex struct {
	root      string
	local     map[string]int
	qualified map[string]int
}

func newClassNameIndex(root string, size int) classNameIndex {
	return classNameIndex{root: root, local: make(map[string]int, size), qualified: make(map[string]int, size)}
}

func addClassName(names map[string]int, name string, index int) {
	if previous, exists := names[name]; exists && previous != index {
		names[name] = -1
	} else {
		names[name] = index
	}
}

func (n classNameIndex) add(local, key string, index int) {
	addClassName(n.local, local, index)
	addClassName(n.local, key, index)
	addClassName(n.qualified, strings.TrimPrefix(key, n.root+"."), index)
}

func (n classNameIndex) lookup(name string) int {
	if name == "" {
		return -1
	}
	names := n.local
	if strings.Contains(name, ".") {
		names, name = n.qualified, strings.TrimPrefix(name, n.root+".")
	}
	if index, ok := names[name]; ok {
		return index
	}
	return -1
}

// Called only while building an unpublished binding table. Its indices refer
// to assigned handles, including history-dependent extension/restore ordering.
func (b *fomBindings) indexClasses() {
	b.objectNames = newClassNameIndex("HLAobjectRoot", len(b.objects))
	for i, c := range b.objects {
		b.objectNames.add(c.Name, c.Key(), i)
	}
	parents := make([]int, len(b.objects))
	for i, c := range b.objects {
		parents[i] = b.objectNames.lookup(c.ParentKey())
	}
	b.objectAncestors = indexAncestors[core.ObjectClassHandle](parents)
	b.interactionNames = newClassNameIndex("HLAinteractionRoot", len(b.interactions))
	for i, c := range b.interactions {
		b.interactionNames.add(c.Name, c.Key(), i)
	}
	parents = make([]int, len(b.interactions))
	for i, c := range b.interactions {
		parents[i] = b.interactionNames.lookup(c.ParentKey())
	}
	b.interactionAncestors = indexAncestors[core.InteractionClassHandle](parents)
}

func indexAncestors[H core.ObjectClassHandle | core.InteractionClassHandle](parents []int) [][]H {
	result := make([][]H, len(parents))
	for i := range parents {
		seen := make(map[int]bool)
		for cur := i; cur >= 0 && !seen[cur]; cur = parents[cur] {
			seen[cur] = true
			result[i] = append(result[i], H(cur+1))
		}
	}
	return result
}

type advisoryDefault struct{ enabled, present bool }

func indexAdvisoryDefaults(fom *model.FOM) [5]advisoryDefault {
	var result [5]advisoryDefault
	for _, value := range fom.AdvisorySwitches() {
		var kind core.AdvisorySwitchKind
		switch value.Name {
		case "objectClassRelevanceAdvisory":
			kind = core.AdvisoryObjectClassRelevance
		case "attributeRelevanceAdvisory":
			kind = core.AdvisoryAttributeRelevance
		case "attributeScopeAdvisory":
			kind = core.AdvisoryAttributeScope
		case "interactionRelevanceAdvisory":
			kind = core.AdvisoryInteractionRelevance
		default:
			continue
		}
		if !result[kind].present {
			result[kind] = advisoryDefault{enabled: value.Enabled, present: true}
		}
	}
	return result
}
