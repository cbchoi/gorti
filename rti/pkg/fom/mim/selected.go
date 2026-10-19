package mim

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"reflect"
	"strings"

	"github.com/cbchoi/gorti/rti/pkg/fom/model"
)

// ParseSelected supports the embedded MIM's ordered structure, service names,
// encodings and declarations. Existing semantics text may differ and is decoded
// from the selected source. This bounded profile is not a claim that all other
// alternate MIMs are normatively invalid. Unknown content is never discarded.
func ParseSelected(source []byte) (*model.FOM, error) {
	selected, err := readSelectedTree(source)
	if err != nil {
		return nil, fmt.Errorf("selected MIM XML: %w", err)
	}
	baseline, err := readSelectedTree(standardMIMXML)
	if err != nil {
		return nil, fmt.Errorf("embedded MIM XML: %w", err)
	}
	normalizeSelectedTableNames(baseline, "")
	normalizeSelectedTableNames(selected, "")
	if err := compareSelectedTree(baseline, selected, ""); err != nil {
		return nil, err
	}
	return decodeMIM(source)
}

// Accept both source and table spellings at this one declaration, without
// relaxing structural, encoding, namespace or other service-name checks.
func normalizeSelectedTableNames(node *selectedNode, parent string) {
	if node.name.Local == "objectClass" {
		for _, child := range node.children {
			if child.name.Local == "name" {
				parent = model.ClassPath(parent, child.text.String())
				break
			}
		}
		for _, child := range node.children {
			if child.name.Local != "attribute" {
				continue
			}
			for _, field := range child.children {
				if field.name.Local == "name" {
					name := CanonicalAttributeName(parent, field.text.String())
					field.text.Reset()
					field.text.WriteString(name)
				}
			}
		}
	}
	for _, child := range node.children {
		normalizeSelectedTableNames(child, parent)
	}
}

type selectedNode struct {
	name       xml.Name
	attributes map[xml.Name]string
	text       strings.Builder
	children   []*selectedNode
}

func readSelectedTree(source []byte) (*selectedNode, error) {
	decoder := xml.NewDecoder(bytes.NewReader(source))
	var root *selectedNode
	var stack []*selectedNode
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch value := token.(type) {
		case xml.StartElement:
			node, err := selectedElement(value)
			if err != nil {
				return nil, err
			}
			if len(stack) == 0 {
				if root != nil {
					return nil, fmt.Errorf("multiple root elements")
				}
				root = node
			} else {
				parent := stack[len(stack)-1]
				parent.children = append(parent.children, node)
			}
			stack = append(stack, node)
		case xml.EndElement:
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if err := appendSelectedText(stack, value); err != nil {
				return nil, err
			}
		case xml.Directive:
			return nil, fmt.Errorf("XML directives are unsupported")
		case xml.ProcInst:
			if unsupportedSelectedInstruction(value, root) {
				return nil, fmt.Errorf("XML processing instruction is unsupported")
			}
		}
	}
	if root == nil {
		return nil, fmt.Errorf("empty MIM")
	}
	return root, nil
}

func compareSelectedTree(base, selected *selectedNode, path string) error {
	path += "/" + base.name.Local
	if base.name != selected.name || !reflect.DeepEqual(base.attributes, selected.attributes) || len(base.children) != len(selected.children) {
		return fmt.Errorf("selected MIM unsupported structure at %s", path)
	}
	baseText, selectedText := base.text.String(), selected.text.String()
	if len(base.children) != 0 {
		baseText, selectedText = strings.TrimSpace(baseText), strings.TrimSpace(selectedText)
	}
	if base.name.Local != "semantics" && baseText != selectedText {
		return fmt.Errorf("selected MIM incompatible service declaration at %s", path)
	}
	for i := range base.children {
		if err := compareSelectedTree(base.children[i], selected.children[i], path); err != nil {
			return err
		}
	}
	return nil
}

func selectedElement(value xml.StartElement) (*selectedNode, error) {
	node := &selectedNode{name: value.Name, attributes: make(map[xml.Name]string)}
	for _, attribute := range value.Attr {
		if attribute.Name.Space == "xmlns" || attribute.Name.Space == "" && attribute.Name.Local == "xmlns" {
			continue
		}
		if _, duplicate := node.attributes[attribute.Name]; duplicate {
			return nil, fmt.Errorf("duplicate attribute %s", attribute.Name.Local)
		}
		node.attributes[attribute.Name] = attribute.Value
	}
	return node, nil
}

func appendSelectedText(stack []*selectedNode, value xml.CharData) error {
	if len(stack) == 0 {
		if strings.TrimSpace(string(value)) != "" {
			return fmt.Errorf("text outside root")
		}
	} else {
		stack[len(stack)-1].text.Write(value)
	}
	return nil
}

func unsupportedSelectedInstruction(value xml.ProcInst, root *selectedNode) bool {
	return value.Target != "xml" || root != nil
}
