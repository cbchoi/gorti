package mim

import (
	"github.com/cbchoi/gorti/rti/pkg/fom/model"
	"strings"
	"testing"
)

func TestExportRetainsMIMSwitchDeclarations(t *testing.T) {
	f, err := decodeMIM([]byte(`<objectModel><switches><autoProvide isEnabled="true"/><serviceReporting isEnabled="false"/><automaticResignAction resignAction="DeleteObjects"/></switches></objectModel>`))
	if err != nil {
		t.Fatal(err)
	}
	if len(f.AdvisorySwitches()) != 2 || f.AutomaticResignAction() != "DeleteObjects" {
		t.Fatal("MIM switch declarations lost")
	}
	if !strings.Contains(f.AdvisorySwitches()[0].Name, "autoProvide") || !f.AdvisorySwitches()[0].Enabled {
		t.Fatal("MIM switch value changed")
	}
}

func TestExportRejectsConflictingMIMResignMetadata(t *testing.T) {
	a := model.NewFOM(nil, nil, nil).WithAutomaticResignAction("DeleteObjects")
	b := model.NewFOM(nil, nil, nil).WithAutomaticResignAction("NoAction")
	merged, diags := Merge(a, b)
	if merged != nil || len(diags) == 0 {
		t.Fatal("MIM automatic resign metadata silently overwritten")
	}
}

func TestExportRetainsMIMEnumeratorValue(t *testing.T) {
	f, err := StandardMIMHandle()
	if err != nil {
		t.Fatal(err)
	}
	for _, data := range f.DataTypes() {
		if data.Name() != "HLAboolean" {
			continue
		}
		for _, value := range data.(*model.EnumeratedData).Enumerators {
			if value.Name == "HLAtrue" && value.Values == "1" {
				return
			}
		}
	}
	t.Fatal("MIM HLAboolean HLAtrue value 1 was lost during decode")
}
