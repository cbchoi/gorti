package federate

import (
	"context"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/application/composition"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

const handleHierarchyFOM = `<objectModel>
<objects><objectClass><name>HLAobjectRoot</name>
  <objectClass><name>BranchA</name>
    <attribute><name>branchA</name><dataType>HLAinteger32BE</dataType></attribute>
    <objectClass><name>Common</name>
      <attribute><name>commonA</name><dataType>HLAinteger32BE</dataType></attribute>
      <objectClass><name>Leaf</name>
        <attribute><name>ownA</name><dataType>HLAinteger32BE</dataType></attribute>
      </objectClass>
    </objectClass>
  </objectClass>
  <objectClass><name>BranchB</name>
    <attribute><name>branchB</name><dataType>HLAinteger32BE</dataType></attribute>
    <objectClass><name>Common</name>
      <attribute><name>commonB</name><dataType>HLAinteger32BE</dataType></attribute>
      <objectClass><name>Leaf</name>
        <attribute><name>ownB</name><dataType>HLAinteger32BE</dataType></attribute>
      </objectClass>
    </objectClass>
  </objectClass>
</objectClass></objects>
<interactions><interactionClass><name>HLAinteractionRoot</name>
  <interactionClass><name>BranchA</name>
    <parameter><name>branchA</name><dataType>HLAinteger32BE</dataType></parameter>
    <interactionClass><name>Common</name>
      <parameter><name>commonA</name><dataType>HLAinteger32BE</dataType></parameter>
      <interactionClass><name>Leaf</name>
        <parameter><name>ownA</name><dataType>HLAinteger32BE</dataType></parameter>
      </interactionClass>
    </interactionClass>
  </interactionClass>
  <interactionClass><name>BranchB</name>
    <parameter><name>branchB</name><dataType>HLAinteger32BE</dataType></parameter>
    <interactionClass><name>Common</name>
      <parameter><name>commonB</name><dataType>HLAinteger32BE</dataType></parameter>
      <interactionClass><name>Leaf</name>
        <parameter><name>ownB</name><dataType>HLAinteger32BE</dataType></parameter>
      </interactionClass>
    </interactionClass>
  </interactionClass>
</interactionClass></interactions>
</objectModel>`

func hierarchyHandleTables(t *testing.T, xml string) (*fomTables, core.FOMHandle) {
	t.Helper()
	tables, err := buildFOMTables([]fomParseModule{{Path: "hierarchy.xml", XML: []byte(xml)}})
	if err != nil {
		t.Fatalf("buildFOMTables: %v", err)
	}
	server, err := composition.NewFOMRepository().Load(context.Background(), []core.FOMModule{{Path: "hierarchy.xml", XML: []byte(xml)}})
	if err != nil {
		t.Fatalf("server Load: %v", err)
	}
	return tables, server
}

func TestBuildFOMTablesQualifiedHierarchy(t *testing.T) {
	tables, server := hierarchyHandleTables(t, handleHierarchyFOM)
	fed := &Federate{handles: tables}
	objectHandles, interactionHandles := map[uint64]bool{}, map[uint64]bool{}
	for _, branch := range []string{"A", "B"} {
		t.Run(branch, func(t *testing.T) {
			path := "Branch" + branch + ".Common.Leaf"
			objectPath, interactionPath := "HLAobjectRoot."+path, "HLAinteractionRoot."+path
			oc, ok := fed.ObjectClassHandle(objectPath)
			serverOC, serverOK := server.LookupObjectClass(objectPath)
			if !ok || !serverOK || oc != uint64(serverOC) || objectHandles[oc] {
				t.Fatalf("object %s: SDK=(%d,%v), server=(%d,%v), already used=%v", path, oc, ok, serverOC, serverOK, objectHandles[oc])
			}
			objectHandles[oc] = true
			ic, ok := fed.InteractionClassHandle(interactionPath)
			serverIC, serverOK := server.LookupInteractionClass(interactionPath)
			if !ok || !serverOK || ic != uint64(serverIC) || interactionHandles[ic] {
				t.Fatalf("interaction %s: SDK=(%d,%v), server=(%d,%v)", path, ic, ok, serverIC, serverOK)
			}
			interactionHandles[ic] = true
			if got, ok := fed.ObjectClassHandle(path); !ok || got != oc {
				t.Errorf("root-omitted object lookup = (%d,%v), want %d", got, ok, oc)
			}
			if got, ok := fed.InteractionClassHandle(path); !ok || got != ic {
				t.Errorf("root-omitted interaction lookup = (%d,%v), want %d", got, ok, ic)
			}
			if got, _ := tables.objectClassName(oc); got != objectPath {
				t.Errorf("ambiguous object reverse = %q, want %q", got, objectPath)
			}
			if got, _ := tables.interactionName(ic); got != interactionPath {
				t.Errorf("ambiguous interaction reverse = %q, want %q", got, interactionPath)
			}
			for i, member := range []string{"own" + branch, "common" + branch, "branch" + branch} {
				want := uint64(i + 1)
				attr, ok := fed.AttributeHandle(oc, member)
				serverAttr, serverOK := server.LookupAttribute(serverOC, member)
				if !ok || !serverOK || attr != want || attr != uint64(serverAttr) {
					t.Errorf("attribute %s: SDK=(%d,%v), server=(%d,%v), want %d", member, attr, ok, serverAttr, serverOK, want)
				}
				param, ok := fed.ParameterHandle(ic, member)
				serverParam, serverOK := server.LookupParameter(serverIC, member)
				if !ok || !serverOK || param != want || param != uint64(serverParam) {
					t.Errorf("parameter %s: SDK=(%d,%v), server=(%d,%v), want %d", member, param, ok, serverParam, serverOK, want)
				}
				if got, _ := tables.attributeName(oc, attr); got != member {
					t.Errorf("attribute reverse = %q, want %q", got, member)
				}
				if got, _ := tables.parameterName(ic, param); got != member {
					t.Errorf("parameter reverse = %q, want %q", got, member)
				}
			}
			other := map[string]string{"A": "B", "B": "A"}[branch]
			for _, member := range []string{"own" + other, "common" + other, "branch" + other} {
				if _, ok := fed.AttributeHandle(oc, member); ok {
					t.Errorf("attribute leaked from other branch: %s", member)
				}
				if _, ok := fed.ParameterHandle(ic, member); ok {
					t.Errorf("parameter leaked from other branch: %s", member)
				}
			}
			if attr, ok := fed.AttributeHandle(oc, "HLAprivilegeToDeleteObject"); !ok || attr != 4 {
				t.Errorf("root attribute = (%d,%v), want (4,true)", attr, ok)
			}
			if attr, ok := tables.attributeHandle(path, "HLAprivilegeToDelete"); !ok || attr != 4 {
				t.Errorf("root-omitted attribute alias = (%d,%v), want (4,true)", attr, ok)
			}
			_, wireParams, err := fed.resolveInteractionValues(path, map[string][]byte{"own" + branch: {1}, "common" + branch: {2}, "branch" + branch: {3}})
			if err != nil || len(wireParams) != 3 {
				t.Errorf("name-based send lost inherited parameters: %v, %v", wireParams, err)
			}
		})
	}
}

func TestBuildFOMTablesRejectsAmbiguousAndIncorrectPaths(t *testing.T) {
	tables, _ := hierarchyHandleTables(t, handleHierarchyFOM)
	for _, path := range []string{"", "Common", "Leaf", "Common.Leaf", "Wrong.BranchA.Common.Leaf", "BranchA.Wrong.Leaf", "HLAinteractionRoot.BranchA.Common.Leaf"} {
		if h, ok := tables.objectClassHandle(path); ok || h != 0 {
			t.Errorf("invalid object path %q resolved (%d,%v)", path, h, ok)
		}
		if _, ok := tables.attributeHandle(path, "ownA"); ok {
			t.Errorf("invalid object path %q resolved an attribute", path)
		}
	}
	for _, path := range []string{"", "Common", "Leaf", "Common.Leaf", "Wrong.BranchA.Common.Leaf", "BranchA.Wrong.Leaf", "HLAobjectRoot.BranchA.Common.Leaf"} {
		if h, ok := tables.interactionHandle(path); ok || h != 0 {
			t.Errorf("invalid interaction path %q resolved (%d,%v)", path, h, ok)
		}
		if _, ok := tables.parameterHandle(path, "ownA"); ok {
			t.Errorf("invalid interaction path %q resolved a parameter", path)
		}
	}
}

func TestBuildFOMTablesPreservesUniqueLeafAliases(t *testing.T) {
	tables, _ := hierarchyHandleTables(t, handleHierarchyFOM)
	for _, name := range []string{"BranchA", "BranchB"} {
		oc, ok := tables.objectClassHandle(name)
		if full, fullOK := tables.objectClassHandle("HLAobjectRoot." + name); !ok || !fullOK || full != oc {
			t.Errorf("object leaf/full aliases differ for %s", name)
		}
		if got, _ := tables.objectClassName(oc); got != name {
			t.Errorf("unique object reverse = %q, want legacy %q", got, name)
		}
		ic, ok := tables.interactionHandle(name)
		if full, fullOK := tables.interactionHandle("HLAinteractionRoot." + name); !ok || !fullOK || full != ic {
			t.Errorf("interaction leaf/full aliases differ for %s", name)
		}
		if got, _ := tables.interactionName(ic); got != name {
			t.Errorf("unique interaction reverse = %q, want legacy %q", got, name)
		}
	}
}

func TestBuildFOMTablesStandardMIMInheritedParameters(t *testing.T) {
	tables, server := hierarchyHandleTables(t, `<objectModel><interactions><interactionClass><name>HLAinteractionRoot</name></interactionClass></interactions></objectModel>`)
	fed := &Federate{handles: tables}
	for _, leaf := range []string{"HLAsetServiceReporting", "HLAsetExceptionReporting"} {
		path := "HLAmanager.HLAfederate.HLAadjust." + leaf
		cls, ok := fed.InteractionClassHandle(leaf)
		serverClass, serverOK := server.LookupInteractionClass(path)
		if !ok || !serverOK || cls != uint64(serverClass) {
			t.Fatalf("MIM class %s: SDK=(%d,%v), server=(%d,%v)", leaf, cls, ok, serverClass, serverOK)
		}
		for _, alias := range []string{leaf, path, "HLAinteractionRoot." + path} {
			if got, ok := fed.InteractionClassHandle(alias); !ok || got != cls {
				t.Errorf("MIM alias %s = (%d,%v), want %d", alias, got, ok, cls)
			}
			for i, name := range []string{"HLAreportingState", "HLAfederate"} {
				param, ok := tables.parameterHandle(alias, name)
				serverParam, serverOK := server.LookupParameter(serverClass, name)
				if !ok || !serverOK || param != uint64(i+1) || param != uint64(serverParam) {
					t.Errorf("%s.%s: SDK=(%d,%v), server=(%d,%v)", alias, name, param, ok, serverParam, serverOK)
				}
				if got, _ := tables.parameterName(cls, param); got != name {
					t.Errorf("MIM parameter reverse = %q, want %q", got, name)
				}
			}
		}
		if param, ok := fed.ParameterHandle(cls, "HLAfederate"); !ok || param != 2 {
			t.Errorf("public inherited parameter = (%d,%v), want (2,true)", param, ok)
		}
	}
	if h, ok := fed.InteractionClassHandle("HLAsetSwitches"); ok || h != 0 {
		t.Errorf("ambiguous MIM leaf resolved (%d,%v)", h, ok)
	}
}
