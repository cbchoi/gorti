package parser

import (
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
)

type strictElement struct {
	name          xml.Name
	seen          map[string]bool
	order         int
	content       string
	children      map[string]int
	position      int
	layoutInvalid bool
	extensionSeen bool
	text          string
}

// Metadata names are accepted only in the two FDD sections, never by widening
// the global whitelist to include service names in arbitrary model locations.
func validateFDDMetadataElement(stack []strictElement, element xml.StartElement) (bool, string) {
	name := element.Name.Local
	section := name == timeElement || name == fddServicesElement
	inMetadata := inFDDMetadata(stack)
	if !section && !inMetadata {
		return false, ""
	}
	if len(stack) == 0 || stack[0].name.Local != objectModelElement {
		return true, fmt.Sprintf("element %q requires an objectModel parent", name)
	}
	if element.Name.Space != stack[0].name.Space {
		return true, fmt.Sprintf("element %q has the wrong namespace for FDD metadata", name)
	}
	parent := &stack[len(stack)-1]
	if section && len(stack) == 1 {
		return true, metadataChild(parent, name, 0)
	}
	if inMetadata {
		switch stack[1].name.Local {
		case fddServicesElement:
			if accepted, problem := validateMetadataService(stack, element); accepted {
				return true, problem
			}
		case timeElement:
			if order := metadataTimeChildOrder(stack, name); order != 0 {
				return true, metadataChild(parent, name, order)
			}
		}
	}
	return true, fmt.Sprintf("element %q is not allowed inside %q in FDD metadata", name, parent.name.Local)
}

func metadataChild(parent *strictElement, name string, order int) string {
	if parent.seen == nil {
		parent.seen = make(map[string]bool)
	}
	if parent.seen[name] {
		return fmt.Sprintf("duplicate element %q inside %q", name, parent.name.Local)
	}
	parent.seen[name] = true
	if order < parent.order {
		return fmt.Sprintf("element %q is out of order inside %q", name, parent.name.Local)
	}
	parent.order = order
	return ""
}

func inFDDMetadata(stack []strictElement) bool {
	return len(stack) >= 2 && stack[0].name.Local == objectModelElement &&
		(stack[1].name.Local == timeElement || stack[1].name.Local == fddServicesElement)
}

func validateFDDMetadataText(stack []strictElement, value string) string {
	if !inFDDMetadata(stack) || strings.TrimSpace(value) == "" {
		return ""
	}
	name := stack[len(stack)-1].name.Local
	if len(stack) == 4 && stack[1].name.Local == timeElement && (name == "dataType" || name == "semantics") {
		return ""
	}
	return fmt.Sprintf("element %q does not allow text content in FDD metadata", name)
}

func validateServiceAttributes(element xml.StartElement, service fddService) string {
	values := make(map[string]string)
	for _, attr := range element.Attr {
		// commonAttributes permits foreign attributes and namespace declarations.
		if attr.Name.Space == xmlNamespaceAttribute || attr.Name.Local == xmlNamespaceAttribute && attr.Name.Space == "" {
			continue
		}
		if attr.Name.Space != "" && attr.Name.Space != element.Name.Space {
			continue
		}
		name := attr.Name.Local
		if attr.Name.Space != "" {
			return fmt.Sprintf("attribute %q on service %q must be unqualified", name, element.Name.Local)
		}
		switch name {
		case "section", "isCallback", "isUsed", "notes", "idtag":
		default:
			return fmt.Sprintf("unknown attribute %q on service %q", name, element.Name.Local)
		}
		if _, duplicate := values[name]; duplicate {
			return fmt.Sprintf("duplicate attribute %q on service %q", name, element.Name.Local)
		}
		values[name] = attr.Value
	}
	for _, fixed := range []struct{ name, value string }{
		{"section", service.section},
		{"isCallback", strconv.FormatBool(service.callback)},
	} {
		if values[fixed.name] != fixed.value {
			return fmt.Sprintf("service %q requires %s=%q", element.Name.Local, fixed.name, fixed.value)
		}
	}
	if used, present := values["isUsed"]; present {
		switch strings.TrimSpace(used) {
		case "true", "false", "1", "0":
		default:
			return fmt.Sprintf("service %q has invalid isUsed boolean %q", element.Name.Local, used)
		}
	}
	return ""
}

func metadataTimeChildOrder(stack []strictElement, name string) int {
	parent := &stack[len(stack)-1]
	var order int
	if len(stack) == 2 {
		switch name {
		case "timeStamp":
			order = 1
		case "lookahead":
			order = 2
		}
	} else if len(stack) == 3 && (parent.name.Local == "timeStamp" || parent.name.Local == "lookahead") {
		switch name {
		case "dataType":
			order = 1
		case "semantics":
			order = 2
		}
	}
	return order
}

const (
	xmlNamespaceAttribute   = "xmlns"
	fddServicesElement      = "serviceUtilization"
	objectClassElement      = "objectClass"
	interactionKind         = "interaction"
	sharingElement          = "sharing"
	objectsElement          = "objects"
	interactionClassElement = "interactionClass"
	objectModelElement      = "objectModel"
	parameterElement        = "parameter"
	sharingPublishSubscribe = "PublishSubscribe"
	invalidStructureCode    = "FOM-009"
	undefinedDataTypeCode   = "FOM-001"
	timeElement             = "time"
	objectKind              = "object"
)

func validateMetadataService(stack []strictElement, element xml.StartElement) (bool, string) {
	name := element.Name.Local
	parent := &stack[len(stack)-1]
	if len(stack) == 2 {
		if service, ok := fdd2010Services[name]; ok {
			if problem := metadataChild(parent, name, 0); problem != "" {
				return true, problem
			}
			return true, validateServiceAttributes(element, service)
		}
	}
	return false, ""
}

const (
	interactionsElement = "interactions"
	attributeElement    = "attribute"
)
