package server

import (
	"testing"

	timepkg "github.com/cbchoi/gorti/rti/internal/hla/time"
)

type wrappedDefaultLBTS struct{ timepkg.LBTSStrategy }
type wrappedDefaultGrant struct{ timepkg.GrantStrategy }

func TestIntegerProfileAcceptsResolvedDefaultsNotNamedCustomStrategies(t *testing.T) {
	resolved, err := resolveResearchConfig("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if !supportsIntegerTimeStrategies(nil, nil) || !supportsIntegerTimeStrategies(resolved.Time.LBTS, resolved.Time.Grant) {
		t.Fatal("ordinary CLI defaults rejected Integer")
	}
	if supportsIntegerTimeStrategies(wrappedDefaultLBTS{resolved.Time.LBTS}, resolved.Time.Grant) {
		t.Fatal("custom Float LBTS accepted through default name")
	}
	if supportsIntegerTimeStrategies(resolved.Time.LBTS, wrappedDefaultGrant{resolved.Time.Grant}) {
		t.Fatal("custom Float grant accepted through default name")
	}
}
