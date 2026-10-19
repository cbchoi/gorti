package composition

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestFDDInvalidContentHasTypedAdmissionError(t *testing.T) {
	for _, xml := range []string{
		`<objectModel><objects><objectClass><name>HLAobjectRoot</name><sharing>NotASharing</sharing></objectClass></objects></objectModel>`,
		`<objectModel><unknown/></objectModel>`,
		`<objectModel><objects></objectModel>`,
	} {
		handle, err := NewFOMRepository().Load(context.Background(), []core.FOMModule{{Path: "invalid-content.xml", XML: []byte(xml)}})
		if handle != nil || !errors.Is(err, core.ErrFOMInvalidModule) || !strings.Contains(err.Error(), "invalid-content.xml") {
			t.Fatalf("handle=%v error=%v, want typed invalid module rejection with location", handle, err)
		}
	}
}
