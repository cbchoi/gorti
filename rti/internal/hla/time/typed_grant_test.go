package time

import (
	"errors"
	"math"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/pkg/logicaltime"
)

func integerTime(t *testing.T, value int64) logicaltime.Time {
	t.Helper()
	v, err := logicaltime.Integer64(value)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestTypedGrantAdjacentIntegerTimes(t *testing.T) {
	const base int64 = 1 << 53
	for _, mode := range []AdvanceMode{ModeNER, ModeNMRA, ModeTAR, ModeTARA, ModeFQR} {
		t.Run(mode.String(), func(t *testing.T) {
			c := TypedGrantContext{Mode: mode, CurrentTime: integerTime(t, base), Requested: integerTime(t, base+2), LBTS: TimeBound{Limit: integerTime(t, base+1)}, NextTSOTime: integerTime(t, base+1), HasTSO: true}
			d, err := DecideTypedGrant(c)
			if err != nil {
				t.Fatal(err)
			}
			wantFire := mode == ModeNMRA || mode == ModeFQR
			if d.Fire != wantFire {
				t.Fatalf("mode=%s fire=%t; want %t", mode, d.Fire, wantFire)
			}
			if d.Fire {
				got, _ := d.Time.Integer64()
				if got != base+1 || !d.ClearPending {
					t.Fatalf("grant=%d clear=%t", got, d.ClearPending)
				}
			}
			c.LBTS.Limit = integerTime(t, base+3)
			d, err = DecideTypedGrant(c)
			if err != nil || !d.Fire {
				t.Fatalf("grant error=%v decision=%+v", err, d)
			}
			want := base + 2
			if mode == ModeNER || mode == ModeNMRA || mode == ModeFQR {
				want = base + 1
			}
			if got, _ := d.Time.Integer64(); got != want {
				t.Fatalf("got %d want %d", got, want)
			}
		})
	}
}

func TestTypedIntegerBoundFinalIsNotUnbounded(t *testing.T) {
	initial := integerTime(t, math.MaxInt64-1)
	one, _ := logicaltime.Integer64Interval(1)
	bound, err := AddTypedTimeBound(initial, one)
	if err != nil || bound.Unbounded || !bound.Limit.IsFinal() {
		t.Fatalf("finite final=%+v err=%v", bound, err)
	}
	overflow, err := AddTypedTimeBound(bound.Limit, one)
	if err != nil || !overflow.Unbounded {
		t.Fatalf("overflow=%+v err=%v", overflow, err)
	}
	c := TypedGrantContext{Mode: ModeTAR, CurrentTime: initial, Requested: bound.Limit, LBTS: bound}
	d, err := DecideTypedGrant(c)
	if err != nil || d.Fire {
		t.Fatalf("strict grant passed finite final equality: %+v %v", d, err)
	}
	c.LBTS = overflow
	d, err = DecideTypedGrant(c)
	if err != nil || !d.Fire || d.Time != bound.Limit {
		t.Fatalf("unbounded grant=%+v err=%v", d, err)
	}
}

func TestTypedBoundAndGrantRejectMixedRepresentations(t *testing.T) {
	i := integerTime(t, 3)
	f, _ := logicaltime.Float64(3)
	floatInterval, _ := logicaltime.Float64Interval(1)
	if _, err := AddTypedTimeBound(i, floatInterval); !errors.Is(err, logicaltime.ErrRepresentationMismatch) {
		t.Fatal(err)
	}
	c := TypedGrantContext{Mode: ModeTAR, CurrentTime: i, Requested: i, LBTS: TimeBound{Limit: f}}
	if _, err := DecideTypedGrant(c); !errors.Is(err, logicaltime.ErrRepresentationMismatch) {
		t.Fatal(err)
	}
	c.LBTS.Limit = i
	c.NextTSOTime, c.HasTSO = f, true
	if _, err := DecideTypedGrant(c); !errors.Is(err, logicaltime.ErrRepresentationMismatch) {
		t.Fatal(err)
	}
	c.HasTSO = false
	if _, err := DecideTypedGrant(c); err != nil {
		t.Fatal(err)
	}
	c.Mode = ModeNone
	if _, err := DecideTypedGrant(c); err == nil {
		t.Fatal("invalid advance mode accepted")
	}
}

func TestTypedLBTSUsesExactIntegerMinimum(t *testing.T) {
	one, _ := logicaltime.Integer64Interval(1)
	set := []TypedRegulatingFederate{{Time: integerTime(t, 1<<53+1), Lookahead: one}, {Time: integerTime(t, 1<<53), Lookahead: one}}
	got, err := TypedLBTS(logicaltime.HLAinteger64Time, set)
	value, _ := got.Limit.Integer64()
	if err != nil || got.Unbounded || value != 1<<53+1 {
		t.Fatalf("LBTS=%d unbounded=%t err=%v", value, got.Unbounded, err)
	}
	got, err = TypedLBTS(logicaltime.HLAinteger64Time, nil)
	if err != nil || !got.Unbounded {
		t.Fatalf("empty LBTS=%+v err=%v", got, err)
	}
}

func TestTypedOutgoingTimestampExactFloor(t *testing.T) {
	const base int64 = 1 << 53
	one, _ := logicaltime.Integer64Interval(1)
	for _, tc := range []struct {
		value              int64
		exclusive, allowed bool
	}{
		{base, false, false}, {base + 1, false, true}, {base + 1, true, false}, {base + 2, true, true},
	} {
		err := ValidateTypedOutgoingTSO(integerTime(t, base), one, integerTime(t, tc.value), tc.exclusive)
		if (err == nil) != tc.allowed || (err != nil && !errors.Is(err, core.ErrTimeInvalidLogicalTime)) {
			t.Fatalf("timestamp=%d exclusive=%t err=%v", tc.value, tc.exclusive, err)
		}
	}
	if err := ValidateTypedOutgoingTSO(integerTime(t, math.MaxInt64), one, integerTime(t, math.MaxInt64), false); !errors.Is(err, core.ErrTimeInvalidLogicalTime) {
		t.Fatalf("overflowed floor accepted: %v", err)
	}
	floating, _ := logicaltime.Float64(1)
	if err := ValidateTypedOutgoingTSO(integerTime(t, base), one, floating, false); !errors.Is(err, logicaltime.ErrRepresentationMismatch) {
		t.Fatal(err)
	}
}
