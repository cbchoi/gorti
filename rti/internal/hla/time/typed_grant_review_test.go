package time

import (
	"errors"
	"math"
	"math/big"
	"math/rand"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	lt "github.com/cbchoi/gorti/rti/pkg/logicaltime"
)

func typedReviewInteger(value int64) lt.Time {
	v, err := lt.Integer64(value)
	if err != nil {
		panic(err)
	}
	return v
}

func typedReviewInterval(value int64) lt.Interval {
	v, err := lt.Integer64Interval(value)
	if err != nil {
		panic(err)
	}
	return v
}

func typedReviewFloat(value float64) lt.Time {
	v, err := lt.Float64(value)
	if err != nil {
		panic(err)
	}
	return v
}

func TestTypedGrantReviewIntegerBoundaryProduct(t *testing.T) {
	values := []int64{0, 1, 2, 1<<53 - 1, 1 << 53, 1<<53 + 1, 1<<53 + 2, math.MaxInt64 - 2, math.MaxInt64 - 1, math.MaxInt64}
	modes := []AdvanceMode{ModeNER, ModeNMRA, ModeTAR, ModeTARA, ModeFQR}
	for _, current := range values {
		for _, requested := range values {
			if requested < current {
				continue
			}
			for boundIndex := -1; boundIndex < len(values); boundIndex++ {
				bound, unbounded := int64(0), boundIndex < 0
				if !unbounded {
					bound = values[boundIndex]
				}
				for nextIndex := -1; nextIndex < len(values); nextIndex++ {
					next, hasNext := int64(0), nextIndex >= 0
					if hasNext {
						next = values[nextIndex]
					}
					for _, mode := range modes {
						ctx := TypedGrantContext{Mode: mode, CurrentTime: typedReviewInteger(current), Requested: typedReviewInteger(requested), LBTS: TimeBound{Limit: typedReviewInteger(bound), Unbounded: unbounded}, HasTSO: hasNext}
						if hasNext {
							ctx.NextTSOTime = typedReviewInteger(next)
						}
						got, err := DecideTypedGrant(ctx)
						if err != nil {
							t.Fatal(err)
						}
						want := requested
						if (mode == ModeNER || mode == ModeNMRA) && hasNext && current <= next && next < requested {
							want = next
						}
						fire := unbounded || want < bound || want == bound && (mode == ModeNMRA || mode == ModeTARA)
						if mode == ModeFQR {
							fire = true
							for _, candidate := range []struct {
								value int64
								valid bool
							}{{bound, !unbounded}, {next, hasNext}} {
								if candidate.valid && candidate.value < want {
									want = candidate.value
								}
							}
							if want < current {
								want = current
							}
						}
						if got.Fire != fire || got.ClearPending != fire {
							t.Fatalf("%s current=%d requested=%d bound=%d unbounded=%t next=%d hasNext=%t: %+v; want fire=%t", mode, current, requested, bound, unbounded, next, hasNext, got, fire)
						}
						if fire {
							at, err := got.Time.Integer64()
							if err != nil || at != want {
								t.Fatalf("%s grant=%d error=%v; want %d", mode, at, err, want)
							}
						}
					}
				}
			}
		}
	}
}

func TestTypedGrantReviewIntegerBoundsBigIntOracle(t *testing.T) {
	values := []int64{0, 1, 1<<53 - 1, 1 << 53, 1<<53 + 1, math.MaxInt64 - 1, math.MaxInt64}
	var pairs [][2]int64
	for _, at := range values {
		for _, delta := range values {
			pairs = append(pairs, [2]int64{at, delta})
		}
	}
	random := rand.New(rand.NewSource(20260930))
	for i := 0; i < 2048; i++ {
		pairs = append(pairs, [2]int64{int64(random.Uint64() >> 1), int64(random.Uint64() >> 1)})
	}
	final := big.NewInt(math.MaxInt64)
	for index, pair := range pairs {
		sum := new(big.Int).Add(big.NewInt(pair[0]), big.NewInt(pair[1]))
		bound, err := AddTypedTimeBound(typedReviewInteger(pair[0]), typedReviewInterval(pair[1]))
		if err != nil || bound.Limit.Representation() != lt.HLAinteger64Time || bound.Unbounded != (sum.Cmp(final) > 0) {
			t.Fatalf("pair %d: %v + %v: %+v %v; exact sum=%s", index, pair[0], pair[1], bound, err, sum)
		}
		if !bound.Unbounded {
			got, err := bound.Limit.Integer64()
			if err != nil || got != sum.Int64() {
				t.Fatalf("pair %d: value=%d error=%v; want %s", index, got, err, sum)
			}
		}
	}
	// Minima are computed in arbitrary precision, including sets in which
	// every horizon exceeds finite final time. Reverse order checks reduction.
	for start := 0; start+3 < len(pairs); start += 4 {
		set := make([]TypedRegulatingFederate, 4)
		var minimum *big.Int
		for i, pair := range pairs[start : start+4] {
			set[i] = TypedRegulatingFederate{Time: typedReviewInteger(pair[0]), Lookahead: typedReviewInterval(pair[1])}
			sum := new(big.Int).Add(big.NewInt(pair[0]), big.NewInt(pair[1]))
			if minimum == nil || sum.Cmp(minimum) < 0 {
				minimum = sum
			}
		}
		for order := 0; order < 2; order++ {
			got, err := TypedLBTS(lt.HLAinteger64Time, set)
			if err != nil || got.Unbounded != (minimum.Cmp(final) > 0) {
				t.Fatalf("set %d order %d: %+v %v; minimum=%s", start, order, got, err, minimum)
			}
			if !got.Unbounded {
				value, err := got.Limit.Integer64()
				if err != nil || value != minimum.Int64() {
					t.Fatalf("set %d: value=%d error=%v; minimum=%s", start, value, err, minimum)
				}
			}
			set[0], set[3], set[1], set[2] = set[3], set[0], set[2], set[1]
		}
	}
}

func TestTypedGrantReviewFloat64AndLegacyParity(t *testing.T) {
	values := []float64{0, math.SmallestNonzeroFloat64, 1, math.Nextafter(8, 0), 8, 1 << 53, math.Nextafter(1<<53, math.Inf(1)), math.MaxFloat64}
	for _, current := range values {
		for _, requested := range values {
			if requested < current {
				continue
			}
			for _, bound := range append(append([]float64(nil), values...), math.Inf(1)) {
				for _, next := range values {
					for _, mode := range []AdvanceMode{ModeNER, ModeNMRA, ModeTAR, ModeTARA, ModeFQR} {
						ctx := TypedGrantContext{Mode: mode, CurrentTime: typedReviewFloat(current), Requested: typedReviewFloat(requested), NextTSOTime: typedReviewFloat(next), HasTSO: true}
						ctx.LBTS = TimeBound{Limit: typedReviewFloat(0), Unbounded: math.IsInf(bound, 1)}
						if !ctx.LBTS.Unbounded {
							ctx.LBTS.Limit = typedReviewFloat(bound)
						}
						got, err := DecideTypedGrant(ctx)
						legacy := decideGrant(mode, core.LogicalTime(current), core.LogicalTime(requested), core.LogicalTime(bound), false, core.LogicalTime(next), true)
						if err != nil || got.Fire != legacy.fire || got.ClearPending != legacy.clearPending {
							t.Fatalf("typed=%+v error=%v legacy=%+v", got, err, legacy)
						}
						if got.Fire {
							value, err := got.Time.Float64()
							if err != nil || value != float64(legacy.time) {
								t.Fatalf("typed time=%v error=%v legacy=%v", value, err, legacy.time)
							}
						}
					}
				}
			}
		}
	}
	for _, mode := range []AdvanceMode{ModeNER, ModeNMRA, ModeTAR, ModeTARA, ModeFQR} {
		got := decideGrant(mode, 0, core.PositiveInfinity, core.PositiveInfinity, false, 0, false)
		wantFire := mode == ModeNMRA || mode == ModeTARA || mode == ModeFQR
		if got.fire != wantFire || got.clearPending != wantFire || got.fire && !math.IsInf(float64(got.time), 1) {
			t.Fatalf("legacy infinity equality changed for %s: %+v", mode, got)
		}
	}
}

func TestTypedGrantReviewValidationAndDistinguishableFloatBound(t *testing.T) {
	integer := typedReviewInteger(7)
	float := typedReviewFloat(7)
	zero := typedReviewInterval(0)
	ctx := TypedGrantContext{Mode: ModeTAR, CurrentTime: integer, Requested: integer, LBTS: TimeBound{Limit: integer, Unbounded: true}}
	for _, mutate := range []func(*TypedGrantContext){
		func(c *TypedGrantContext) { c.CurrentTime = lt.Time{} },
		func(c *TypedGrantContext) { c.Requested = typedReviewInteger(6) },
		func(c *TypedGrantContext) { c.Requested = float },
		func(c *TypedGrantContext) { c.LBTS.Limit = float },
		func(c *TypedGrantContext) { c.NextTSOTime, c.HasTSO = lt.Time{}, true },
		func(c *TypedGrantContext) { c.Mode = AdvanceMode(255) },
	} {
		invalid := ctx
		mutate(&invalid)
		if _, err := DecideTypedGrant(invalid); err == nil {
			t.Fatalf("invalid context accepted: %+v", invalid)
		}
	}
	ctx.NextTSOTime = float
	if _, err := DecideTypedGrant(ctx); err != nil {
		t.Fatalf("absent next-message payload must be ignored: %v", err)
	}
	if _, err := TypedLBTS(lt.Unspecified, nil); !errors.Is(err, lt.ErrUnknownRepresentation) {
		t.Fatalf("untyped empty regulator set: %v", err)
	}
	if _, err := TypedLBTS(lt.HLAinteger64Time, []TypedRegulatingFederate{{Time: typedReviewInteger(0), Lookahead: zero}, {Time: float, Lookahead: zero}}); !errors.Is(err, lt.ErrRepresentationMismatch) {
		t.Fatalf("minimum zero must not skip validating later regulator: %v", err)
	}
	for _, at := range []float64{1 << 53, math.MaxFloat64} {
		for _, delta := range []float64{0, math.SmallestNonzeroFloat64, 1} {
			interval, _ := lt.Float64Interval(delta)
			got, err := AddTypedTimeBound(typedReviewFloat(at), interval)
			want := at
			if delta > 0 {
				want = math.Nextafter(at, math.Inf(1))
			}
			if err != nil || got.Unbounded != math.IsInf(want, 1) {
				t.Fatalf("Float64 bound %g + %g: %+v %v", at, delta, got, err)
			}
			if !got.Unbounded {
				value, err := got.Limit.Float64()
				if err != nil || value != want {
					t.Fatalf("Float64 bound %g + %g = %g (%v), want %g", at, delta, value, err, want)
				}
			}
		}
	}
}
