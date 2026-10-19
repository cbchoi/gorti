package mim

import (
	"github.com/cbchoi/gorti/rti/pkg/fom/model"
	"testing"
)

func TestMergePreservesNamedRatesAndAdvisoryPresence(t *testing.T) {
	rate, err := model.NewUpdateRate("Fast", "12.5", "example")
	if err != nil {
		t.Fatal(err)
	}
	f := model.NewFOMWithUpdateRates(nil, nil, nil, nil, []model.UpdateRate{rate}).WithAdvisorySwitches([]model.AdvisorySwitch{{Name: "attributeScopeAdvisory", Enabled: true}})
	for _, pair := range [][2]*model.FOM{{nil, f}, {f, nil}, {model.NewFOM(nil, nil, nil), f}} {
		merged, diags := Merge(pair[0], pair[1])
		if len(diags) != 0 {
			t.Fatal(diags)
		}
		if len(merged.UpdateRates()) != 1 || merged.UpdateRates()[0].Rate != 12.5 || len(merged.AdvisorySwitches()) != 1 || !merged.AdvisorySwitches()[0].Enabled {
			t.Fatal("MIM merge discarded FDD metadata")
		}
	}
	otherRate, err := model.NewUpdateRate("Fast", "25", "example")
	if err != nil {
		t.Fatal(err)
	}
	other := model.NewFOMWithUpdateRates(nil, nil, nil, nil, []model.UpdateRate{otherRate})
	if merged, diags := Merge(f, other); merged != nil || len(diags) == 0 {
		t.Fatal("MIM merge accepted conflicting named rate")
	}
}
