package parser

import (
	"github.com/cbchoi/gorti/rti/pkg/fom/model"
	"strings"
	"testing"
)

func TestAutomaticResignFOMSwitch(t *testing.T) {
	for _, tc := range []struct {
		xml, want string
		invalid   bool
	}{
		{`<automaticResignAction resignAction="CancelThenDeleteThenDivest"/>`, "CancelThenDeleteThenDivest", false},
		{`<automaticResignAction/>`, "NoAction", false},
		{`<automaticResignAction resignAction="invalid"/>`, "", true},
		{`<automaticResignAction/><automaticResignAction/>`, "", true},
	} {
		t.Run(tc.xml, func(t *testing.T) {
			xml := strings.Replace(minimalFOM, "</objectModel>", "<switches>"+tc.xml+"</switches></objectModel>", 1)
			result, err := Parse([]Module{{Path: "automatic.xml", XML: []byte(xml)}})
			if err != nil {
				t.Fatal(err)
			}
			if tc.invalid {
				if len(result.Diagnostics) == 0 {
					t.Fatal("invalid switch accepted")
				}
				return
			}
			if len(result.Diagnostics) != 0 {
				t.Fatalf("diagnostics: %v", result.Diagnostics)
			}
			if got := result.FOM.(*model.FOM).AutomaticResignAction(); got != tc.want {
				t.Fatalf("action %q want %q", got, tc.want)
			}
		})
	}
}
