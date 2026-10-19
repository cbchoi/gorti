package logicaltime_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	"math/big"
	"math/rand"
	"testing"

	lt "github.com/cbchoi/gorti/rti/pkg/logicaltime"
)

// Independent integer oracle: no integer operation or expected encoding passes
// through Float64. Boundary pairs and a fixed seed make failures reproducible.
func TestReviewInteger64ArithmeticAgainstBigInt(t *testing.T) {
	values := []int64{0, 1, 2, 1<<53 - 1, 1 << 53, 1<<53 + 1, 1<<62 - 1, 1 << 62, math.MaxInt64 - 1, math.MaxInt64}
	pairs := make([][2]int64, 0, len(values)*len(values)+4096)
	for _, a := range values {
		for _, b := range values {
			pairs = append(pairs, [2]int64{a, b})
		}
	}
	rng := rand.New(rand.NewSource(15162010))
	for n := 0; n < 4096; n++ {
		pairs = append(pairs, [2]int64{rng.Int63(), rng.Int63()})
	}
	for _, pair := range pairs {
		a, b := pair[0], pair[1]
		left, err := lt.Integer64(a)
		if err != nil {
			t.Fatal(err)
		}
		right, err := lt.Integer64(b)
		if err != nil {
			t.Fatal(err)
		}
		li, _ := lt.Integer64Interval(a)
		ri, _ := lt.Integer64Interval(b)
		wantOrder := big.NewInt(a).Cmp(big.NewInt(b))
		if got, err := left.Compare(right); err != nil || got != wantOrder {
			t.Fatalf("compare(%d,%d)=%d,%v", a, b, got, err)
		}
		if got, err := li.Compare(ri); err != nil || got != wantOrder {
			t.Fatalf("interval compare(%d,%d)=%d,%v", a, b, got, err)
		}
		for _, subtract := range []bool{false, true} {
			want := new(big.Int)
			var at lt.Time
			var interval lt.Interval
			var timeErr, intervalErr error
			if subtract {
				want.Sub(big.NewInt(a), big.NewInt(b))
				at, timeErr = left.Subtract(ri)
				interval, intervalErr = li.Subtract(ri)
			} else {
				want.Add(big.NewInt(a), big.NewInt(b))
				at, timeErr = left.Add(ri)
				interval, intervalErr = li.Add(ri)
			}
			if want.Sign() < 0 || !want.IsInt64() {
				if !errors.Is(timeErr, lt.ErrIllegalArithmetic) || !errors.Is(intervalErr, lt.ErrIllegalArithmetic) {
					t.Fatalf("range(%d,%d,subtract=%t): time=%v interval=%v", a, b, subtract, timeErr, intervalErr)
				}
				continue
			}
			if timeErr != nil || intervalErr != nil {
				t.Fatalf("arithmetic(%d,%d,subtract=%t): time=%v interval=%v", a, b, subtract, timeErr, intervalErr)
			}
			gotTime, err := at.Integer64()
			gotInterval, intervalErr := interval.Integer64()
			if err != nil || intervalErr != nil || gotTime != want.Int64() || gotInterval != want.Int64() {
				t.Fatalf("inexact(%d,%d,subtract=%t): time=%d interval=%d want=%s", a, b, subtract, gotTime, gotInterval, want)
			}
			encoded, err := at.MarshalBinary()
			if err != nil || !bytes.Equal(encoded, want.FillBytes(make([]byte, 8))) {
				t.Fatalf("nonexact big-endian result: %x want=%s err=%v", encoded, want, err)
			}
			decoded, err := lt.DecodeTime(lt.HLAinteger64Time, encoded)
			if err != nil || decoded != at {
				t.Fatalf("time roundtrip: %v", err)
			}
			encoded, err = interval.MarshalBinary()
			if err != nil {
				t.Fatal(err)
			}
			decodedInterval, err := lt.DecodeInterval(lt.HLAinteger64Time, encoded)
			if err != nil || decodedInterval != interval {
				t.Fatalf("interval roundtrip: %v", err)
			}
		}
		want := new(big.Int).Sub(big.NewInt(a), big.NewInt(b))
		want.Abs(want)
		distance, err := left.Distance(right)
		if err != nil {
			t.Fatal(err)
		}
		got, err := distance.Integer64()
		if err != nil || got != want.Int64() {
			t.Fatalf("distance(%d,%d)=%d,%v want=%s", a, b, got, err, want)
		}
		back, err := right.Distance(left)
		if err != nil || back != distance {
			t.Fatalf("asymmetric distance(%d,%d)", a, b)
		}
	}
}

func reviewRoundedFloat(a, b float64, subtract bool) float64 {
	// 2200 binary digits span every finite Float64 exponent and subnormal.
	left := new(big.Float).SetPrec(2200).SetFloat64(a)
	right := new(big.Float).SetPrec(2200).SetFloat64(b)
	result := new(big.Float).SetPrec(2200).SetMode(big.ToNearestEven)
	if subtract {
		result.Sub(left, right)
	} else {
		result.Add(left, right)
	}
	rounded, _ := result.Float64()
	return rounded
}

func TestReviewFloat64ArithmeticAndDistanceAgainstExactBinaryOracle(t *testing.T) {
	values := []float64{0, math.SmallestNonzeroFloat64, math.Float64frombits(2), math.Float64frombits(1<<52 - 1), math.Float64frombits(1 << 52), math.Nextafter(1, 0), 1, math.Nextafter(1, 2), 1<<53 - 1, 1 << 53, math.Nextafter(1<<53, math.Inf(1)), math.Nextafter(math.MaxFloat64, 0), math.MaxFloat64}
	pairs := make([][2]float64, 0, len(values)*len(values)+2048)
	for _, a := range values {
		for _, b := range values {
			pairs = append(pairs, [2]float64{a, b})
		}
	}
	rng := rand.New(rand.NewSource(339340))
	for n := 0; n < 2048; n++ {
		pairs = append(pairs, [2]float64{math.Float64frombits(uint64(rng.Int63()) % 0x7ff0000000000000), math.Float64frombits(uint64(rng.Int63()) % 0x7ff0000000000000)})
	}
	for _, pair := range pairs {
		a, b := pair[0], pair[1]
		at, _ := lt.Float64(a)
		other, _ := lt.Float64(b)
		leftInterval, _ := lt.Float64Interval(a)
		interval, _ := lt.Float64Interval(b)
		for _, subtract := range []bool{false, true} {
			want := reviewRoundedFloat(a, b, subtract)
			if b != 0 && want == a {
				direction := math.Inf(1)
				if subtract {
					direction = math.Inf(-1)
				}
				want = math.Nextafter(a, direction)
			}
			var got lt.Time
			var gotInterval lt.Interval
			var err error
			var intervalErr error
			if subtract {
				got, err = at.Subtract(interval)
				gotInterval, intervalErr = leftInterval.Subtract(interval)
			} else {
				got, err = at.Add(interval)
				gotInterval, intervalErr = leftInterval.Add(interval)
			}
			if math.IsInf(want, 0) || want < 0 {
				if !errors.Is(err, lt.ErrIllegalArithmetic) || !errors.Is(intervalErr, lt.ErrIllegalArithmetic) {
					t.Fatalf("float range(%g,%g,subtract=%t): time=%v interval=%v", a, b, subtract, err, intervalErr)
				}
				continue
			}
			if err != nil {
				t.Fatalf("float arithmetic(%g,%g,subtract=%t): %v", a, b, subtract, err)
			}
			value, err := got.Float64()
			if err != nil || value != want {
				t.Fatalf("float arithmetic(%g,%g,subtract=%t)=%g,%v want=%g", a, b, subtract, value, err, want)
			}
			if intervalErr != nil {
				t.Fatalf("float interval arithmetic(%g,%g,subtract=%t): %v", a, b, subtract, intervalErr)
			}
			intervalValue, intervalErr := gotInterval.Float64()
			if intervalErr != nil || intervalValue != want {
				t.Fatalf("float interval arithmetic(%g,%g,subtract=%t)=%g,%v want=%g", a, b, subtract, intervalValue, intervalErr, want)
			}
			if b != 0 && ((!subtract && value <= a) || (subtract && value >= a)) {
				t.Fatalf("nonzero interval not distinguishable: %g,%g subtract=%t", a, b, subtract)
			}
		}
		wantDistance := math.Abs(reviewRoundedFloat(a, b, true))
		distance, err := at.Distance(other)
		if err != nil {
			t.Fatal(err)
		}
		gotDistance, err := distance.Float64()
		if err != nil || gotDistance != wantDistance {
			t.Fatalf("distance(%g,%g)=%g,%v want=%g", a, b, gotDistance, err, wantDistance)
		}
		back, err := other.Distance(at)
		if err != nil || back != distance {
			t.Fatalf("asymmetric float distance(%g,%g)", a, b)
		}
		encoded, err := at.MarshalBinary()
		if err != nil || binary.BigEndian.Uint64(encoded) != math.Float64bits(a) {
			t.Fatalf("float encoding(%g)=%x,%v", a, encoded, err)
		}
		decoded, err := lt.DecodeTime(lt.HLAfloat64Time, encoded)
		if err != nil || decoded != at {
			t.Fatalf("float roundtrip(%g): %v", a, err)
		}
	}
}

func TestReviewInvalidEncodingsSignsAndRepresentationIsolation(t *testing.T) {
	for _, bits := range []uint64{1 << 63, 1<<63 | 1, math.MaxUint64, 0x7ff0000000000000, 0xfff0000000000000, 0x7ff8000000000000, 0x7ff0000000000001} {
		encoded := make([]byte, 8)
		binary.BigEndian.PutUint64(encoded, bits)
		for _, kind := range []lt.Representation{lt.HLAfloat64Time, lt.HLAinteger64Time} {
			decoded, err := lt.DecodeTime(kind, encoded)
			interval, intervalErr := lt.DecodeInterval(kind, encoded)
			valid := bits <= math.MaxInt64
			if kind == lt.HLAfloat64Time {
				value := math.Float64frombits(bits)
				valid = value >= 0 && !math.IsInf(value, 0) && !math.IsNaN(value)
			}
			if valid {
				if err != nil || intervalErr != nil {
					t.Fatalf("valid encoding(%v,%x): %v,%v", kind, bits, err, intervalErr)
				}
				if kind == lt.HLAfloat64Time && bits == 1<<63 {
					if !decoded.IsInitial() || !interval.IsZero() {
						t.Fatal("negative zero is not canonical initial/zero")
					}
					canonical, _ := decoded.MarshalBinary()
					if binary.BigEndian.Uint64(canonical) != 0 {
						t.Fatal("negative zero was reencoded with sign")
					}
				}
			} else if !errors.Is(err, lt.ErrInvalidValue) || !errors.Is(intervalErr, lt.ErrInvalidValue) {
				t.Fatalf("invalid encoding(%v,%x): %v,%v", kind, bits, err, intervalErr)
			}
		}
	}
	floating, _ := lt.Float64(1)
	integer, _ := lt.Integer64(1)
	floatInterval, _ := lt.Float64Interval(1)
	intInterval, _ := lt.Integer64Interval(1)
	checks := []func() error{
		func() error { _, e := floating.Add(intInterval); return e },
		func() error { _, e := integer.Subtract(floatInterval); return e },
		func() error { _, e := floatInterval.Add(intInterval); return e },
		func() error { _, e := intInterval.Subtract(floatInterval); return e },
		func() error { _, e := intInterval.Compare(floatInterval); return e },
		func() error { _, e := floating.Distance(integer); return e },
		func() error { _, e := floatInterval.Integer64(); return e },
		func() error { _, e := intInterval.Float64(); return e },
	}
	for i, check := range checks {
		if err := check(); !errors.Is(err, lt.ErrRepresentationMismatch) {
			t.Fatalf("mixed representation operation %d: %v", i, err)
		}
	}
	if _, err := (lt.Time{}).Add(floatInterval); !errors.Is(err, lt.ErrUnknownRepresentation) {
		t.Fatalf("uninitialized time: %v", err)
	}
	if _, err := (lt.Interval{}).Subtract(intInterval); !errors.Is(err, lt.ErrUnknownRepresentation) {
		t.Fatalf("uninitialized interval: %v", err)
	}
}
