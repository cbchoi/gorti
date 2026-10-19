package composition

import (
	"context"
	"fmt"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/pkg/fom/mim"
	"github.com/cbchoi/gorti/rti/pkg/fom/model"
)

func TestRepeatedModuleContainersRemainResolvable(t *testing.T) {
	var modules []core.FOMModule
	for _, leaf := range []string{"Left", "Right"} {
		modules = append(modules, core.FOMModule{Path: leaf + ".xml", XML: []byte(fmt.Sprintf(`<objectModel>
<objects><objectClass><name>HLAobjectRoot</name><objectClass><name>Shared</name><attribute><name>Value</name><dataType>HLAinteger32BE</dataType></attribute><objectClass><name>%s</name></objectClass></objectClass></objectClass></objects>
<interactions><interactionClass><name>HLAinteractionRoot</name><interactionClass><name>Shared</name><parameter><name>Target</name><dataType>HLAinteger32BE</dataType></parameter><interactionClass><name>%s</name></interactionClass></interactionClass></interactionClass></interactions>
</objectModel>`, leaf, leaf))})
	}
	fom, err := NewFOMRepository().Load(context.Background(), modules)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"Shared", "Shared.Left", "Shared.Right"} {
		oc, ok := fom.LookupObjectClass(path)
		if !ok {
			t.Fatal("object container not resolvable", path)
		}
		if _, ok := fom.LookupAttribute(oc, "Value"); !ok {
			t.Fatal("attribute missing", path)
		}
		ic, ok := fom.LookupInteractionClass(path)
		if !ok {
			t.Fatal("interaction container not resolvable", path)
		}
		if _, ok := fom.LookupParameter(ic, "Target"); !ok {
			t.Fatal("parameter missing", path)
		}
	}
}

func TestStandardMIMQualifiedInteractionIdentity(t *testing.T) {
	m, err := mim.StandardMIMHandle()
	if err != nil {
		t.Fatal(err)
	}
	h := NewModelHandle(m)
	names := h.(core.FOMHandleNameLookup)
	paths := []string{
		"HLAmanager.HLAfederate.HLAadjust.HLAsetSwitches",
		"HLAmanager.HLAfederation.HLAadjust.HLAsetSwitches",
	}
	seen := map[core.InteractionClassHandle]bool{}
	for _, path := range paths {
		cls, ok := h.LookupInteractionClass(path)
		if !ok {
			t.Fatalf("missing %s", path)
		}
		if seen[cls] {
			t.Fatalf("different MIM paths alias handle %d", cls)
		}
		seen[cls] = true
		full, ok := h.LookupInteractionClass("HLAinteractionRoot." + path)
		if !ok || full != cls {
			t.Errorf("root-qualified lookup differs for %s", path)
		}
		name, ok := names.InteractionClassName(cls)
		if !ok || name != "HLAinteractionRoot."+path {
			t.Errorf("reverse name = %q", name)
		}
	}
	for _, invalid := range []string{"HLAsetSwitches", "NotTheRoot.HLAsetSwitches", "HLAmanager.Wrong.HLAadjust.HLAsetSwitches"} {
		if cls, ok := h.LookupInteractionClass(invalid); ok {
			t.Errorf("ambiguous/incorrect path %s resolved as %d", invalid, cls)
		}
	}
}

func TestFOMQualifiedObjectInheritance(t *testing.T) {
	repo := NewFOMRepository()
	fom, err := repo.Load(context.Background(), []core.FOMModule{{Path: "branches.xml", XML: []byte(`<objectModel><objects><objectClass><name>HLAobjectRoot</name>
<objectClass><name>BranchA</name><attribute><name>A</name><dataType>HLAinteger32BE</dataType></attribute><objectClass><name>Common</name></objectClass></objectClass>
<objectClass><name>BranchB</name><attribute><name>B</name><dataType>HLAinteger32BE</dataType></attribute><objectClass><name>Common</name></objectClass></objectClass>
</objectClass></objects></objectModel>`)}})
	if err != nil {
		t.Fatal(err)
	}
	names := fom.(core.FOMHandleNameLookup)
	seen := map[core.ObjectClassHandle]bool{}
	for _, suffix := range []string{"A", "B"} {
		path := "Branch" + suffix + ".Common"
		cls, ok := fom.LookupObjectClass(path)
		if !ok || seen[cls] {
			t.Fatalf("class not distinct: %s", path)
		}
		seen[cls] = true
		if got, _ := names.ObjectClassName(cls); got != "HLAobjectRoot."+path {
			t.Errorf("reverse = %s", got)
		}
		attr, ok := fom.LookupAttribute(cls, suffix)
		if !ok {
			t.Fatal("inherited attribute missing", suffix)
		}
		if got, _ := names.AttributeName(cls, attr); got != suffix {
			t.Errorf("attribute reverse = %s", got)
		}
		other := map[string]string{"A": "B", "B": "A"}[suffix]
		if _, ok := fom.LookupAttribute(cls, other); ok {
			t.Error("attribute inherited from another branch")
		}
		if _, ok := fom.LookupAttribute(cls, "HLAprivilegeToDeleteObject"); !ok {
			t.Error("root attribute missing")
		}
	}
	for _, path := range []string{"Common", "Wrong.Common", "Wrong.BranchA.Common"} {
		if _, ok := fom.LookupObjectClass(path); ok {
			t.Errorf("invalid or ambiguous path accepted: %s", path)
		}
	}
}

func TestMIMMergeUsesQualifiedIdentity(t *testing.T) {
	base, err := mim.StandardMIMHandle()
	if err != nil {
		t.Fatal(err)
	}
	const path = "HLAinteractionRoot.Custom.HLAsetSwitches"
	user := model.NewFOM(nil, []model.InteractionClass{{Name: "HLAsetSwitches", ParentName: "Custom", QualifiedName: path, Parameters: []model.Parameter{{Name: "CustomValue"}}}}, nil)
	merged, diags := mim.Merge(base, user)
	if len(diags) != 0 {
		t.Fatal(diags)
	}
	h := NewModelHandle(merged)
	cls, ok := h.LookupInteractionClass(path)
	if !ok {
		t.Fatal("non-MIM branch was discarded")
	}
	if _, ok := h.LookupParameter(cls, "CustomValue"); !ok {
		t.Fatal("custom parameter missing")
	}
	user = model.NewFOM(nil, []model.InteractionClass{{Name: "HLAsetSwitches", ParentName: "HLAadjust", QualifiedName: "HLAinteractionRoot.HLAmanager.HLAfederate.HLAadjust.HLAsetSwitches", Parameters: []model.Parameter{{Name: "CustomValue"}}}}, nil)
	if merged, diags := mim.Merge(base, user); merged != nil || len(diags) == 0 {
		t.Fatal("MIM class redefinition accepted")
	}
}

func TestStandardMIMInheritedParameterLookup(t *testing.T) {
	m, err := mim.StandardMIMHandle()
	if err != nil {
		t.Fatal(err)
	}
	h := NewModelHandle(m)
	names := h.(core.FOMHandleNameLookup)
	for _, path := range []string{
		"HLAmanager.HLAfederate.HLAadjust.HLAsetServiceReporting",
		"HLAmanager.HLAfederate.HLAadjust.HLAsetExceptionReporting",
	} {
		cls, ok := h.LookupInteractionClass(path)
		if !ok {
			t.Fatal(path)
		}
		seen := map[core.ParameterHandle]bool{}
		for _, name := range []string{"HLAreportingState", "HLAfederate"} {
			p, ok := h.LookupParameter(cls, name)
			if !ok || seen[p] {
				t.Fatalf("%s: missing/aliased parameter %s", path, name)
			}
			seen[p] = true
			got, ok := names.ParameterName(cls, p)
			if !ok || got != name {
				t.Errorf("reverse parameter = %q, want %s", got, name)
			}
		}
		if _, ok := names.ParameterName(cls, 10000); ok {
			t.Error("invalid parameter accepted")
		}
	}
}
