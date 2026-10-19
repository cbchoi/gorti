package parser

import (
	"fmt"
	"testing"
)

func TestModuleClassMergeRejectsConflicts(t *testing.T) {
	for _, kind := range []string{objectKind, interactionKind} {
		t.Run(kind, func(t *testing.T) {
			makeModule := func(member string) Module {
				class, root, collection, field := objectClassElement, objectClassRootName, objectsElement, attributeElement
				if kind == interactionKind {
					class, root, collection, field = interactionClassElement, interactionClassRootName, interactionsElement, parameterElement
				}
				return Module{Path: member + ".xml", XML: []byte(fmt.Sprintf(`<objectModel><%s><%s><name>%s</name><%s><name>Shared</name><%s><name>%s</name><dataType>HLAinteger32BE</dataType></%s></%s></%s></%s></objectModel>`, collection, class, root, class, field, member, field, class, class, collection))}
			}
			res, err := Parse([]Module{makeModule("First"), makeModule("Second")})
			if err != nil {
				t.Fatal(err)
			}
			if res.FOM != nil || !res.HasCode("FOM-014") {
				t.Fatalf("conflict not rejected: %+v", res.Diagnostics)
			}
			res, err = Parse([]Module{makeModule("First"), makeModule("First")})
			if err != nil || res.FOM == nil || len(res.Diagnostics) != 0 {
				t.Fatalf("identical declarations rejected: %v %+v", err, res.Diagnostics)
			}
		})
	}
}

func TestModuleClassMergeRejectsDuplicateSiblings(t *testing.T) {
	for _, kind := range []string{objectKind, interactionKind} {
		class, root, collection := objectClassElement, objectClassRootName, objectsElement
		if kind == interactionKind {
			class, root, collection = interactionClassElement, interactionClassRootName, interactionsElement
		}
		xml := fmt.Sprintf(`<objectModel><%s><%s><name>%s</name><%s><name>Same</name></%s><%s><name>Same</name></%s></%s></%s></objectModel>`, collection, class, root, class, class, class, class, class, collection)
		res, err := Parse([]Module{{Path: kind + ".xml", XML: []byte(xml)}})
		if err != nil {
			t.Fatal(err)
		}
		if res.FOM != nil || !res.HasCode("FOM-014") {
			t.Fatalf("duplicate sibling not rejected: %+v", res.Diagnostics)
		}
	}
}

func TestModuleClassMergeChecksInheritedMembers(t *testing.T) {
	modules := []Module{
		{Path: "child.xml", XML: []byte(`<objectModel><interactions><interactionClass><name>HLAinteractionRoot</name><interactionClass><name>Shared</name><interactionClass><name>Child</name><parameter><name>Value</name><dataType>HLAinteger32BE</dataType></parameter></interactionClass></interactionClass></interactionClass></interactions></objectModel>`)},
		{Path: "parent.xml", XML: []byte(`<objectModel><interactions><interactionClass><name>HLAinteractionRoot</name><interactionClass><name>Shared</name><parameter><name>Value</name><dataType>HLAinteger32BE</dataType></parameter></interactionClass></interactionClass></interactions></objectModel>`)},
	}
	for _, order := range [][]Module{modules, {modules[1], modules[0]}} {
		res, err := Parse(order)
		if err != nil {
			t.Fatal(err)
		}
		if res.FOM != nil || !res.HasCode("FOM-005") {
			t.Fatalf("merged inherited duplicate accepted: %+v", res.Diagnostics)
		}
	}
}
