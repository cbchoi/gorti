package logicaltime_test

import (
	"encoding/hex"
	"errors"
	"math"
	"testing"

	lt "github.com/cbchoi/gorti/rti/pkg/logicaltime"
)

func TestInteger64ExactArithmeticAndEncoding(t *testing.T) {
	for _, value := range []int64{0, 1, 1<<53 - 1, 1 << 53, 1<<53 + 1, math.MaxInt64 - 1, math.MaxInt64} {
		at, err := lt.Integer64(value)
		if err != nil {
			t.Fatal(err)
		}
		bytes, err := at.MarshalBinary()
		if err != nil || len(bytes) != 8 {
			t.Fatalf("encode %d: %x %v", value, bytes, err)
		}
		decoded, err := lt.DecodeTime(lt.HLAinteger64Time, bytes)
		if err != nil || decoded != at {
			t.Fatalf("roundtrip %d: %+v %v", value, decoded, err)
		}
		got, err := decoded.Integer64()
		if err != nil || got != value {
			t.Fatalf("exact value: %d %v", got, err)
		}
		if _, err := at.Float64(); !errors.Is(err, lt.ErrRepresentationMismatch) {
			t.Fatal("implicit lossy float conversion", err)
		}
	}
	at, _ := lt.Integer64(1<<53 + 1)
	encoded, _ := at.MarshalBinary()
	if hex.EncodeToString(encoded) != "0020000000000001" {
		t.Fatalf("not HLAinteger64BE: %x", encoded)
	}
	one, _ := lt.Integer64Interval(1)
	next, err := at.Add(one)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := next.Integer64(); got != 1<<53+2 {
		t.Fatalf("rounded integer sum: %d", got)
	}
	back, err := next.Subtract(one)
	if err != nil || back != at {
		t.Fatalf("inverse: %+v %v", back, err)
	}
	if cmp, err := at.Compare(next); err != nil || cmp != -1 {
		t.Fatalf("comparison: %d %v", cmp, err)
	}
	if distance, err := at.Distance(next); err != nil || distance != one {
		t.Fatalf("distance: %+v %v", distance, err)
	}
}

func TestFloat64EpsilonAndFiniteFinal(t *testing.T) {
	epsilon, _ := lt.HLAfloat64Time.Epsilon()
	for _, value := range []float64{0, 1, 1 << 53, math.Nextafter(math.MaxFloat64, 0)} {
		at, _ := lt.Float64(value)
		next, err := at.Add(epsilon)
		if err != nil {
			t.Fatal(err)
		}
		got, _ := next.Float64()
		if got != math.Nextafter(value, math.Inf(1)) {
			t.Fatalf("epsilon(%g)=%g", value, got)
		}
		back, err := next.Subtract(epsilon)
		if err != nil || back != at {
			t.Fatalf("predecessor(%g): %+v %v", value, back, err)
		}
	}
	final, _ := lt.HLAfloat64Time.Final()
	if value, _ := final.Float64(); value != math.MaxFloat64 {
		t.Fatal("final must be finite", value)
	}
	if _, err := final.Add(epsilon); !errors.Is(err, lt.ErrIllegalArithmetic) {
		t.Fatal("overflow accepted", err)
	}
	negativeZero, _ := lt.Float64(math.Copysign(0, -1))
	initial, _ := lt.HLAfloat64Time.Initial()
	if negativeZero != initial {
		t.Fatal("negative zero was not canonicalized")
	}
	bytes, _ := final.MarshalBinary()
	if hex.EncodeToString(bytes) != "7fefffffffffffff" {
		t.Fatalf("not HLAfloat64BE: %x", bytes)
	}
}

func TestFactoryBoundariesAndInvalidValues(t *testing.T) {
	for _, kind := range []lt.Representation{lt.HLAfloat64Time, lt.HLAinteger64Time} {
		if parsed, err := lt.ParseRepresentation(kind.String()); err != nil || parsed != kind {
			t.Fatal("factory lookup", parsed, err)
		}
		initial, _ := kind.Initial()
		final, _ := kind.Final()
		zero, _ := kind.Zero()
		epsilon, _ := kind.Epsilon()
		if !initial.IsInitial() || initial.IsFinal() || !final.IsFinal() || !zero.IsZero() {
			t.Fatal("factory endpoints")
		}
		if got, err := initial.Add(zero); err != nil || got != initial {
			t.Fatal("zero identity", err)
		}
		if _, err := initial.Subtract(epsilon); !errors.Is(err, lt.ErrIllegalArithmetic) {
			t.Fatal("underflow accepted", err)
		}
		if _, err := final.Add(epsilon); !errors.Is(err, lt.ErrIllegalArithmetic) {
			t.Fatal("overflow accepted", err)
		}
		bytes, _ := epsilon.MarshalBinary()
		if got, err := lt.DecodeInterval(kind, bytes); err != nil || got != epsilon {
			t.Fatal("interval encoding", err)
		}
		for _, bytes := range [][]byte{nil, make([]byte, 7), make([]byte, 9), {255, 255, 255, 255, 255, 255, 255, 255}} {
			if _, err := lt.DecodeTime(kind, bytes); err == nil {
				t.Fatalf("invalid bytes accepted: %x", bytes)
			}
		}
	}
	for _, bad := range []float64{-1, math.Inf(1), math.Inf(-1), math.NaN()} {
		if _, err := lt.Float64(bad); !errors.Is(err, lt.ErrInvalidValue) {
			t.Fatal("invalid time", bad, err)
		}
		if _, err := lt.Float64Interval(bad); !errors.Is(err, lt.ErrInvalidValue) {
			t.Fatal("invalid interval", bad, err)
		}
	}
	if _, err := lt.Integer64(-1); !errors.Is(err, lt.ErrInvalidValue) {
		t.Fatal(err)
	}
	if _, err := lt.Integer64Interval(-1); !errors.Is(err, lt.ErrInvalidValue) {
		t.Fatal(err)
	}
	if _, err := lt.ParseRepresentation("integer64"); !errors.Is(err, lt.ErrUnknownRepresentation) {
		t.Fatal(err)
	}
	integer, _ := lt.Integer64(1)
	floating, _ := lt.Float64(1)
	floatInterval, _ := lt.Float64Interval(1)
	if _, err := integer.Compare(floating); !errors.Is(err, lt.ErrRepresentationMismatch) {
		t.Fatal(err)
	}
	if _, err := integer.Add(floatInterval); !errors.Is(err, lt.ErrRepresentationMismatch) {
		t.Fatal(err)
	}
	if _, err := integer.Distance(floating); !errors.Is(err, lt.ErrRepresentationMismatch) {
		t.Fatal(err)
	}
	if _, err := (lt.Time{}).MarshalBinary(); err == nil {
		t.Fatal("uninitialized time encoded")
	}
	if _, err := (lt.Interval{}).MarshalBinary(); err == nil {
		t.Fatal("uninitialized interval encoded")
	}
}

func TestDistanceDoesNotApplyTimePlusIntervalRounding(t *testing.T) {
	large, _ := lt.Float64(1)
	small, _ := lt.Float64(math.SmallestNonzeroFloat64)
	want, _ := lt.Float64Interval(1)
	for _, pair := range [][2]lt.Time{{large, small}, {small, large}} {
		if got, err := pair[0].Distance(pair[1]); err != nil || got != want {
			t.Fatalf("rounded difference: %v %v", got, err)
		}
	}
}
