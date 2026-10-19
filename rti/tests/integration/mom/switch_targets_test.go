package mom_test

import (
	"context"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/application/composition"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
	"github.com/cbchoi/gorti/rti/pkg/fom/mim"
)

type unusedOutbox struct{ core.Outbox }

// Standard MIM inheritance must resolve the target independently of the
// directly declared reporting state, including opaque 64-bit handles.
func TestMOMReportingTargetWithStandardMIM(t *testing.T) {
	model, err := mim.StandardMIMHandle()
	if err != nil {
		t.Fatal(err)
	}
	fom := composition.NewModelHandle(model)
	for _, tc := range []struct {
		class string
		get   func(*mom.Manager, core.FederationName, core.FederateHandle) bool
	}{
		{mom.ClassSetServiceReporting, (*mom.Manager).ServiceReporting},
		{mom.ClassSetExceptionReporting, (*mom.Manager).ExceptionReporting},
	} {
		t.Run(tc.class, func(t *testing.T) {
			ctx := context.Background()
			m, err := mom.New(mom.Options{Outbox: unusedOutbox{}})
			if err != nil {
				t.Fatal(err)
			}
			const targetHandle core.FederateHandle = 0x8122334481020304
			for _, h := range []core.FederateHandle{7, targetHandle} {
				if err := m.FederateJoined(ctx, "switches", h, "federate", "test"); err != nil {
					t.Fatal(err)
				}
			}
			class, ok := fom.LookupInteractionClass(tc.class)
			if !ok {
				t.Fatal("MIM adjustment class missing")
			}
			state, ok := fom.LookupParameter(class, "HLAreportingState")
			if !ok {
				t.Fatal("MIM reporting-state parameter missing")
			}
			params := map[core.ParameterHandle][]byte{state: {0, 0, 0, 1}}
			target, resolved := fom.LookupParameter(class, "HLAfederate")
			if !resolved || target == state {
				t.Fatal("inherited HLAfederate must resolve separately from HLAreportingState")
			}
			params[target] = []byte{0, 0, 0, 8, 0x81, 0x22, 0x33, 0x44, 0x81, 2, 3, 4}
			err = mom.NewDispatcher(m).Dispatch(ctx, "switches", tc.class, 7, params, fom, nil)
			if err != nil || !tc.get(m, "switches", targetHandle) {
				t.Errorf("resolved target was not adjusted: %v", err)
			}
			if tc.get(m, "switches", 7) {
				t.Error("MOM adjustment silently changed sender instead of target")
			}
		})
	}
}
