package exporter

import (
	"sort"
	"strings"

	"github.com/cbchoi/gorti/rti/pkg/fom/model"
)

type classNode struct {
	name, key, parent, qualified, parentName string
	element                                  element
}

func hierarchy(classes []classNode, rootName, childName string) (element, error) {
	index, err := indexClassNodes(classes)
	if err != nil {
		return element{}, err
	}
	children := make(map[int][]int)
	root := -1
	for i, c := range classes {
		if c.parent == "" {
			if c.name != rootName || root != -1 {
				return element{}, unsupported("expected one %s root", rootName)
			}
			root = i
			continue
		}
		parent, err := resolveClassParent(classes, index, c)
		if err != nil {
			return element{}, err
		}
		children[parent] = append(children[parent], i)
	}
	if root < 0 {
		return element{}, unsupported("missing %s root", rootName)
	}
	for parent := range children {
		sort.Slice(children[parent], func(i, j int) bool { return classes[children[parent][i]].key < classes[children[parent][j]].key })
	}
	visited := make(map[int]bool)
	var walk func(int) (element, error)
	walk = func(i int) (element, error) {
		if visited[i] {
			return element{}, unsupported("cyclic hierarchy at %q", classes[i].key)
		}
		visited[i] = true
		e := classes[i].element
		for _, child := range children[i] {
			c, err := walk(child)
			if err != nil {
				return element{}, err
			}
			e.Children = append(e.Children, c)
		}
		return e, nil
	}
	result, err := walk(root)
	if err != nil {
		return element{}, err
	}
	if len(visited) != len(classes) {
		return element{}, unsupported("unreachable or cyclic %s hierarchy", childName)
	}
	return result, nil
}

func (b *builder) objects(classes []model.ObjectClass) (element, error) {
	nodes := make([]classNode, 0, len(classes))
	for _, c := range classes {
		e := node("objectClass", textNode("name", c.Name))
		if !oneOf(c.Sharing, "Publish|Subscribe|PublishSubscribe|Neither") {
			return element{}, unsupported("invalid object sharing %q", c.Sharing)
		}
		e.optional("sharing", c.Sharing)
		e.descriptive("semantics", c.Semantics)
		sort.Slice(c.Attributes, func(i, j int) bool { return c.Attributes[i].Name < c.Attributes[j].Name })
		for i, a := range c.Attributes {
			if i > 0 && c.Attributes[i-1].Name == a.Name {
				return element{}, unsupported("duplicate attribute %s.%s", c.Key(), a.Name)
			}
			if err := b.dataReference(a.DataType); err != nil {
				return element{}, err
			}
			if !oneOf(a.UpdateType, "Static|Periodic|Conditional|NA") || !oneOf(a.Ownership, "DivestAcquire|Divest|Acquire|NoTransfer") || !oneOf(a.Sharing, "Publish|Subscribe|PublishSubscribe|Neither") {
				return element{}, unsupported("invalid attribute metadata %s.%s", c.Key(), a.Name)
			}
			x := node("attribute", textNode("name", a.Name))
			x.optional("dataType", a.DataType)
			x.optional("updateType", a.UpdateType)
			x.descriptive("updateCondition", a.UpdateCondition)
			x.optional("ownership", a.Ownership)
			x.optional("sharing", a.Sharing)
			if len(a.Dimensions) > 0 {
				d, err := b.dimensionsNode(a.Dimensions)
				if err != nil {
					return element{}, err
				}
				x.Children = append(x.Children, d)
			}
			if err := b.delivery(&x, a.Transportation, a.Order); err != nil {
				return element{}, err
			}
			x.descriptive("semantics", a.Semantics)
			e.Children = append(e.Children, x)
		}
		nodes = append(nodes, classNode{c.Name, c.Key(), c.ParentKey(), c.QualifiedName, c.ParentName, e})
	}
	root, err := hierarchy(nodes, "HLAobjectRoot", "objectClass")
	return node("objects", root), err
}

func (b *builder) interactions(classes []model.InteractionClass) (element, error) {
	nodes := make([]classNode, 0, len(classes))
	for _, c := range classes {
		if !oneOf(c.Sharing, "Publish|Subscribe|PublishSubscribe|Neither") {
			return element{}, unsupported("invalid interaction sharing %q", c.Sharing)
		}
		e := node("interactionClass", textNode("name", c.Name))
		e.optional("sharing", c.Sharing)
		if len(c.Dimensions) > 0 {
			d, err := b.dimensionsNode(c.Dimensions)
			if err != nil {
				return element{}, err
			}
			e.Children = append(e.Children, d)
		}
		if err := b.delivery(&e, c.Transportation, c.Order); err != nil {
			return element{}, err
		}
		e.descriptive("semantics", c.Semantics)
		sort.Slice(c.Parameters, func(i, j int) bool { return c.Parameters[i].Name < c.Parameters[j].Name })
		for i, p := range c.Parameters {
			if i > 0 && c.Parameters[i-1].Name == p.Name {
				return element{}, unsupported("duplicate parameter %s.%s", c.Key(), p.Name)
			}
			if err := b.dataReference(p.DataType); err != nil {
				return element{}, err
			}
			parameter := node("parameter", textNode("name", p.Name))
			parameter.optional("dataType", p.DataType)
			parameter.descriptive("semantics", p.Semantics)
			e.Children = append(e.Children, parameter)
		}
		nodes = append(nodes, classNode{c.Name, c.Key(), c.ParentKey(), c.QualifiedName, c.ParentName, e})
	}
	root, err := hierarchy(nodes, "HLAinteractionRoot", "interactionClass")
	return node("interactions", root), err
}

func indexClassNodes(classes []classNode) (map[string]int, error) {
	index := make(map[string]int, len(classes))
	for i, c := range classes {
		if !ncName(c.name) {
			return nil, unsupported("invalid class name %q", c.name)
		}
		if _, exists := index[c.key]; exists {
			return nil, unsupported("duplicate class %q", c.key)
		}
		index[c.key] = i
		if c.qualified != "" && (model.LocalClassName(c.qualified) != c.name || model.LocalClassName(c.parent) != c.parentName) {
			return nil, unsupported("inconsistent class ancestry %q", c.key)
		}
	}
	return index, nil
}

func resolveClassParent(classes []classNode, index map[string]int, c classNode) (int, error) {
	parent, ok := index[c.parent]
	if !ok && c.qualified == "" && !strings.Contains(c.parent, ".") {
		for j, candidate := range classes {
			if candidate.name == c.parent {
				if ok {
					return 0, unsupported("ambiguous parent %q", c.parent)
				}
				parent, ok = j, true
			}
		}
	}
	if !ok {
		return 0, unsupported("missing parent %q of %q", c.parent, c.key)
	}
	return parent, nil
}
