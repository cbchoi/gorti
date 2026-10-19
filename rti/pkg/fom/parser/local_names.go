package parser

import (
	"encoding/xml"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/cbchoi/gorti/rti/pkg/fom/mim"
)

type localNameValidator struct{}

func init() { diagnosers = append(diagnosers, localNameValidator{}) }

type nameDeclaration struct {
	name, identity, kind, parentClass string
	line                              int
}

type nameFrame struct{ kind, name string }

var predefinedNames struct {
	sync.Once
	identities map[string]bool
}

func (localNameValidator) Run(in diagnosticInput) []Diagnostic {
	if len(in.xml) == 0 {
		return nil
	}
	predefinedNames.Do(func() {
		predefinedNames.identities = make(map[string]bool)
		// MIM decodes independently of parser, so this cannot recurse into Parse.
		for _, declaration := range declaredNames(mim.StandardMIMBytes()) {
			predefinedNames.identities[declaration.identity] = true
		}
		// Table 6's spelling is reserved too, even though Annex G uses the alias.
		predefinedNames.identities["objectClass:HLAobjectRoot/objectClass:HLAmanager/objectClass:HLAfederation/attribute:HLAAnnexSaveName"] = true
	})
	var diags []Diagnostic
	for _, declaration := range declaredNames(in.xml) {
		name := localDeclarationName(in, declaration)
		valid := xmlLocalName(name)
		if name != declaration.name {
			valid = true
			for _, part := range strings.Split(name, ".") {
				if !xmlLocalName(part) || reservedUserName(part) {
					valid = false
				}
			}
		} else if reservedUserName(name) && !predefinedNames.identities[declaration.identity] {
			valid = false
		}
		if !valid {
			diags = append(diags, Diagnostic{Code: "FOM-017", ModulePath: in.modulePath, Line: declaration.line, Message: fmt.Sprintf("invalid or reserved local %s name %q", declaration.kind, declaration.name)})
		}
	}
	return diags
}

func reservedUserName(name string) bool {
	return strings.EqualFold(name, "NA") || len(name) >= 3 && strings.EqualFold(name[:3], "HLA")
}

// Let encoding/xml apply its Unicode XML Name tables; then impose the OMT's
// additional local-name restrictions. Attribute/injection syntax is not a name.
func xmlLocalName(name string) bool {
	if name == "" || strings.ContainsAny(name, ":.") {
		return false
	}
	decoder := xml.NewDecoder(strings.NewReader("<" + name + "/>"))
	token, err := decoder.Token()
	start, ok := token.(xml.StartElement)
	if err != nil || !ok || start.Name.Space != "" || start.Name.Local != name || len(start.Attr) != 0 {
		return false
	}
	if _, err = decoder.Token(); err != nil {
		return false
	}
	_, err = decoder.Token()
	return err == io.EOF
}

func namedDefinition(kind string) bool {
	switch kind {
	case objectClassElement, interactionClassElement, attributeElement, parameterElement, "basicData", "simpleData", "enumeratedData", "enumerator", "arrayData", "fixedRecordData", "field", "variantRecordData", "alternative", "dimension", "transportation":
		return true
	}
	return false
}

func declaredNames(source []byte) []nameDeclaration {
	semantic := newSemanticXMLTokens(source)
	decoder := xml.NewTokenDecoder(semantic)
	var frames []nameFrame
	var out []nameDeclaration
	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}
		switch value := token.(type) {
		case xml.StartElement:
			if value.Name.Local != "name" || len(frames) == 0 {
				frames = append(frames, nameFrame{kind: value.Name.Local})
				continue
			}
			line := lineForOffset(source, semantic.decoder.InputOffset())
			var name string
			if err := decoder.DecodeElement(&name, &value); err != nil {
				return out
			}
			frame := &frames[len(frames)-1]
			frame.name = name
			if !namedDefinition(frame.kind) {
				continue
			}
			var identity strings.Builder
			var classNames []string
			for _, ancestor := range frames[:len(frames)-1] {
				if namedDefinition(ancestor.kind) {
					fmt.Fprintf(&identity, "%s:%s/", ancestor.kind, ancestor.name)
				}
				if ancestor.kind == frame.kind && (frame.kind == objectClassElement || frame.kind == interactionClassElement) {
					classNames = append(classNames, ancestor.name)
				}
			}
			fmt.Fprintf(&identity, "%s:%s", frame.kind, name)
			out = append(out, nameDeclaration{name: name, identity: identity.String(), kind: frame.kind, parentClass: strings.Join(classNames, "."), line: line})
		case xml.EndElement:
			if len(frames) > 0 {
				frames = frames[:len(frames)-1]
			}
		}
	}
	return out
}

func localDeclarationName(in diagnosticInput, declaration nameDeclaration) string {
	name := declaration.name
	// Namespace-free XML is the existing legacy profile. A fully qualified
	// class representation is not a local declaration containing a period.
	if in.raw != nil && in.raw.XMLName.Space == "" && (declaration.kind == objectClassElement || declaration.kind == interactionClassElement) && strings.Contains(name, ".") {
		root := objectClassRootName
		if declaration.kind == interactionClassElement {
			root = interactionClassRootName
		}
		parent := declaration.parentClass
		if parent == "" {
			parent = root
		}
		if strings.HasPrefix(name, parent+".") {
			name = strings.TrimPrefix(name, parent+".")
		}
	}
	return name
}
