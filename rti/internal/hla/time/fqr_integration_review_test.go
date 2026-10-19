package time

import (
	"context"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestIEEE2010FQRUnconstrainedPendingDoesNotLowerPeerGALT(t *testing.T) {
	ctx := context.Background()
	m, out := enableTestManager(t)
	for _, h := range []core.FederateHandle{1, 2} {
		if err := m.EnableRegulation(ctx, "f", h, 1); err != nil {
			t.Fatal(err)
		}
	}
	out.fail = true
	if err := m.FlushQueueRequest(ctx, "f", 2, 20); !errors.Is(err, core.ErrFederateOverflow) {
		t.Fatalf("pending FQR=%v", err)
	}
	// The unconstrained peer grants its requested 20, never its own GALT.
	if got, defined := m.QueryGALT("f", 1); !defined || got != 21 {
		t.Fatalf("unconstrained FQR lowered GALT to (%v,%v), want (21,true)", got, defined)
	}
}
