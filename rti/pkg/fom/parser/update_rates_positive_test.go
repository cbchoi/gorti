package parser_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/application/composition"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/federation"
	"github.com/cbchoi/gorti/rti/pkg/fom/model"
	"github.com/cbchoi/gorti/rti/pkg/fom/parser"
)

func positiveRateModule(decimal string) []byte {
	return []byte(`<objectModel xmlns="http://standards.ieee.org/IEEE1516-2010"><updateRates><updateRate><name>Rate</name><rate>` + decimal + `</rate><semantics>  retained  </semantics></updateRate></updateRates></objectModel>`)
}

func TestIEEE2010UpdateRateParserAndCreateAdmission(t *testing.T) {
	for _, tc := range []struct {
		decimal string
		valid   bool
	}{
		{"0", false}, {"-0.000", false}, {"-0.25", false},
		{"0.00000000000000000000000000000000000001", true},
		{"0." + strings.Repeat("0", 323) + "5", true},
		{"1.0000000000000000000000000001", true},
	} {
		t.Run(tc.decimal, func(t *testing.T) {
			for _, parse := range []func([]parser.Module) (parser.Result, error){parser.Parse, parser.ParseAdditive} {
				result, err := parse([]parser.Module{{Path: "rate.xml", XML: positiveRateModule(tc.decimal)}})
				if err != nil {
					t.Fatal(err)
				}
				if !tc.valid {
					if result.FOM != nil || !result.HasCode("FOM-016") {
						t.Errorf("parser admitted nonpositive rate: %+v", result)
					}
				} else {
					if result.FOM == nil || len(result.Diagnostics) != 0 {
						t.Fatalf("positive rate rejected: %+v", result)
					}
					rate := result.FOM.(*model.FOM).UpdateRates()[0]
					got, err := rate.DecimalString()
					if err != nil || got != tc.decimal || rate.Semantics != "  retained  " {
						t.Fatalf("positive rate changed: %q %v", got, err)
					}
				}
			}
			manager, err := federation.New(federation.Options{
				Clock: core.NewManualClock(time.Unix(0, 0)), FOMs: composition.NewFOMRepository(),
			})
			if err != nil {
				t.Fatal(err)
			}
			request := core.CreateFederationRequest{Name: "rates", FOMModules: []core.FOMModule{{Path: "rate.xml", XML: positiveRateModule(tc.decimal)}}}
			err = manager.CreateFederation(context.Background(), request)
			if tc.valid {
				if err != nil {
					t.Fatalf("create rejected positive rate: %v", err)
				}
			} else {
				if err == nil || !strings.Contains(err.Error(), "FOM-016") {
					t.Errorf("create admitted invalid rate or wrong error: %v", err)
				}
				if handle, err := manager.FOMFor("rates"); err == nil || handle != nil {
					t.Error("failed create published a FOM")
				}
				request.FOMModules[0].XML = positiveRateModule("0.25")
				if err := manager.CreateFederation(context.Background(), request); err != nil {
					t.Errorf("failed create reserved federation name: %v", err)
				}
			}
		})
	}
}
