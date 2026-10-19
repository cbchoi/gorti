package composition

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestFOMInvalidLocalNameHasTypedAdmissionError(t *testing.T) {
	for _, name := range []string{"1Sensor", "Sensor:Bad", "Sensor Bad", "HLAcustom", "NA"} {
		t.Run(name, func(t *testing.T) {
			module := core.FOMModule{Path: "invalid-name.xml", XML: []byte(`<objectModel xmlns="http://standards.ieee.org/IEEE1516-2010"><objects><objectClass><name>HLAobjectRoot</name><objectClass><name>` + name + `</name><sharing>PublishSubscribe</sharing></objectClass></objectClass></objects></objectModel>`)}
			handle, err := NewFOMRepository().Load(context.Background(), []core.FOMModule{module})
			if handle != nil || !errors.Is(err, core.ErrFOMInvalidName) || !strings.Contains(err.Error(), "FOM-017") {
				t.Fatalf("handle=%v error=%v, want typed name rejection", handle, err)
			}
		})
	}
}
