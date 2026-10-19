package parser

import (
	"fmt"
	"testing"

	"github.com/cbchoi/gorti/rti/pkg/fom/model"
)

func TestParseAdditiveMembersAndConflicts(t *testing.T) {
	for _, kind := range []string{objectKind, interactionKind} {
		t.Run(kind, func(t *testing.T) {
			class, root, collection, field := objectClassElement, objectClassRootName, objectsElement, attributeElement
			if kind == interactionKind {
				class, root, collection, field = interactionClassElement, interactionClassRootName, interactionsElement, parameterElement
			}
			module := func(className, member, datatype string) Module {
				return Module{Path: member + ".xml", XML: []byte(fmt.Sprintf(`<objectModel><%s><%s><name>%s</name><%s><name>%s</name><%s><name>%s</name><dataType>%s</dataType></%s></%s></%s></%s></objectModel>`, collection, class, root, class, className, field, member, datatype, field, class, class, collection))}
			}
			// New classes may contribute members; complete existing classes may not.
			result, err := ParseAdditive([]Module{module("Shared", "First", "HLAinteger32BE"), module("NewClass", "Second", "HLAinteger32BE")})
			if err != nil || result.FOM == nil {
				t.Fatalf("disjoint addition: %v %+v", err, result.Diagnostics)
			}
			fom := result.FOM.(*model.FOM)
			count := 0
			if kind == objectKind {
				for _, c := range fom.ObjectClasses() {
					count += len(c.Attributes)
				}
			} else {
				for _, c := range fom.InteractionClasses() {
					count += len(c.Parameters)
				}
			}
			if count != 2 {
				t.Fatalf("members=%d, want 2", count)
			}
			result, err = ParseAdditive([]Module{module("Shared", "First", "HLAinteger32BE"), module("Shared", "First", "HLAinteger64BE")})
			if err != nil || result.FOM != nil || !result.HasCode("FOM-014") {
				t.Fatalf("conflict: %v %+v", err, result.Diagnostics)
			}
		})
	}
}

func TestParseAdditiveRejectsInheritedShadowing(t *testing.T) {
	modules := []Module{
		{XML: []byte(`<objectModel><objects><objectClass><name>HLAobjectRoot</name><objectClass><name>Parent</name><objectClass><name>Child</name><attribute><name>Value</name><dataType>HLAinteger32BE</dataType></attribute></objectClass></objectClass></objectClass></objects></objectModel>`)},
		{XML: []byte(`<objectModel><objects><objectClass><name>HLAobjectRoot</name><objectClass><name>Parent</name><attribute><name>Value</name><dataType>HLAinteger32BE</dataType></attribute></objectClass></objectClass></objects></objectModel>`)},
	}
	result, err := ParseAdditive(modules)
	if err != nil || result.FOM != nil {
		t.Fatalf("inherited shadowing accepted: %v %+v", err, result.Diagnostics)
	}
}
