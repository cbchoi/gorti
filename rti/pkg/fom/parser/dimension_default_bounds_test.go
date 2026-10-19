package parser

import (
	"fmt"
	"testing"
)

func TestDimensionDefaultBoundsAdmission(t *testing.T) {
	for _, value := range []string{"[5..5)", "[8..2)", "[0..101)", "100", "18446744073709551615"} {
		t.Run(value, func(t *testing.T) {
			r, err := Parse([]Module{dimensionModule(attributeElement, `<dimensions><dimension>X</dimension></dimensions>`, fmt.Sprintf(`<dimensions><dimension><name>X</name><upperBound>100</upperBound><value>%s</value></dimension></dimensions>`, value))})
			if err == nil && len(r.Diagnostics) == 0 {
				t.Fatal("invalid default accepted")
			}
		})
	}
	for _, value := range []string{"[5)", "99", "[0..100)", "Excluded"} {
		t.Run(value, func(t *testing.T) {
			requireDimensionFOM(t, dimensionModule(attributeElement, `<dimensions><dimension>X</dimension></dimensions>`, fmt.Sprintf(`<dimensions><dimension><name>X</name><upperBound>100</upperBound><value>%s</value></dimension></dimensions>`, value)))
		})
	}
}
