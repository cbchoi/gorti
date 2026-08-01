package time

import (
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// TestEagerGrant_NameAndDeterminismFlag pins the registry key and the
// determinism claim. If either changes the docs and the registry
// pre-registration in research.Default() must be updated in lockstep.
func TestEagerGrant_NameAndDeterminismFlag(t *testing.T) {
	s := EagerGrantStrategy()
	if got := s.Name(); got != "eager" {
		t.Errorf("Name(): got %q, want %q", got, "eager")
	}
	if !s.DeterminismPreserving() {
		t.Errorf("DeterminismPreserving(): got false, want true (eager is a pure function of input)")
	}
}

// TestEagerGrant_FiresImmediatelyAtRequested verifies the defining
// behavior: every DecideGrant returns Fire=true, Time=Requested,
// ClearPending=true regardless of mode or LBTS.
func TestEagerGrant_FiresImmediatelyAtRequested(t *testing.T) {
	cases := []struct {
		name string
		ctx  GrantContext
	}{
		{"NER LBTS<req", GrantContext{Mode: ModeNER, CurrentTime: 0, Requested: 5, LBTS: 1, SolePending: false}},
		{"NMRA LBTS==req", GrantContext{Mode: ModeNMRA, CurrentTime: 0, Requested: 5, LBTS: 5, SolePending: false}},
		{"TAR LBTS>req", GrantContext{Mode: ModeTAR, CurrentTime: 0, Requested: 5, LBTS: 99, SolePending: false}},
		{"TARA LBTS<<req", GrantContext{Mode: ModeTARA, CurrentTime: 0, Requested: 100, LBTS: 0, SolePending: false}},
		{"FQR sole pending", GrantContext{Mode: ModeFQR, CurrentTime: 3, Requested: 10, LBTS: 4, SolePending: true}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := EagerGrantStrategy().DecideGrant(c.ctx)
			if !got.Fire {
				t.Errorf("Fire: got false, want true")
			}
			if got.Time != c.ctx.Requested {
				t.Errorf("Time: got %v, want Requested=%v", got.Time, c.ctx.Requested)
			}
			if !got.ClearPending {
				t.Errorf("ClearPending: got false, want true")
			}
		})
	}
}

// TestEagerGrant_DiffersFromDefault_OnNoProgressInput is the key
// pedagogical test: the default holds when LBTS produces no progress
// (LBTS==CurrentTime, NER), but eager fires anyway. The two MUST
// disagree on inputs the default rejects — that disagreement is the
// whole point of the alt.
func TestEagerGrant_DiffersFromDefault_OnNoProgressInput(t *testing.T) {
	ctx := GrantContext{Mode: ModeNER, CurrentTime: 5, Requested: 7, LBTS: 5, SolePending: true}

	def := DefaultGrantStrategy().DecideGrant(ctx)
	alt := EagerGrantStrategy().DecideGrant(ctx)

	if def.Fire {
		t.Errorf("default: fired %+v, want hold (no progress)", def)
	}
	if !alt.Fire || alt.Time != core.LogicalTime(7) {
		t.Errorf("eager: %+v, want fire@7", alt)
	}
}

// TestEagerGrant_DiffersFromDefault_OnHoldInput is the second
// disagreement: default's NER predicate holds when LBTS<requested with
// multiple peers; eager always fires.
func TestEagerGrant_DiffersFromDefault_OnHoldInput(t *testing.T) {
	ctx := GrantContext{Mode: ModeNER, CurrentTime: 0, Requested: 5, LBTS: 2, SolePending: false}

	def := DefaultGrantStrategy().DecideGrant(ctx)
	alt := EagerGrantStrategy().DecideGrant(ctx)

	if def.Fire {
		t.Errorf("default NER LBTS<req multi-peer: fired %+v, want hold", def)
	}
	if !alt.Fire || alt.Time != core.LogicalTime(5) || !alt.ClearPending {
		t.Errorf("eager: %+v, want fire@5 clearPending=true", alt)
	}
}
