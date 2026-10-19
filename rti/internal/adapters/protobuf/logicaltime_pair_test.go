package protobuf

import (
	"math"
	"strconv"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	lt "github.com/cbchoi/gorti/rti/pkg/logicaltime"
)

func TestLogicalTimePairInteger64Precision(t *testing.T) {
	for _, number := range []int64{0, 1, 1<<53 + 1, math.MaxInt64} {
		t.Run(strconv.FormatInt(number, 10), func(t *testing.T) {
			at, _ := lt.Integer64(number)
			interval, _ := lt.Integer64Interval(number)
			legacy, exact, err := EncodeLogicalTimePair(lt.HLAinteger64Time, at)
			if err != nil || math.Float64bits(legacy) != 0 {
				t.Fatalf("integer legacy write: %v %v", legacy, err)
			}
			got, err := DecodeLogicalTimePair(lt.HLAinteger64Time, legacy, exact)
			if err != nil || got != at {
				t.Fatalf("precision: %v %v", got, err)
			}
			optionalLegacy, optionalExact, err := EncodeOptionalLogicalTimePair(lt.HLAinteger64Time, &at)
			if err != nil || optionalLegacy != nil {
				t.Fatalf("optional integer legacy write: %v %v", optionalLegacy, err)
			}
			optionalGot, err := DecodeOptionalLogicalTimePair(lt.HLAinteger64Time, optionalLegacy, optionalExact)
			if err != nil || optionalGot == nil || *optionalGot != at {
				t.Fatalf("optional precision: %v %v", optionalGot, err)
			}
			legacy, exact, err = EncodeLogicalTimeIntervalPair(lt.HLAinteger64Time, interval)
			if err != nil || math.Float64bits(legacy) != 0 {
				t.Fatalf("interval legacy write: %v %v", legacy, err)
			}
			gotInterval, err := DecodeLogicalTimeIntervalPair(lt.HLAinteger64Time, legacy, exact)
			if err != nil || gotInterval != interval {
				t.Fatalf("interval precision: %v %v", gotInterval, err)
			}
			optionalLegacy, optionalExact, err = EncodeOptionalLogicalTimeIntervalPair(lt.HLAinteger64Time, &interval)
			if err != nil || optionalLegacy != nil {
				t.Fatalf("optional interval legacy write: %v %v", optionalLegacy, err)
			}
			optionalInterval, err := DecodeOptionalLogicalTimeIntervalPair(lt.HLAinteger64Time, optionalLegacy, optionalExact)
			if err != nil || optionalInterval == nil || *optionalInterval != interval {
				t.Fatalf("optional interval precision: %v %v", optionalInterval, err)
			}
		})
	}
}

func TestLogicalTimePairFloat64Compatibility(t *testing.T) {
	for _, number := range []float64{0, math.Copysign(0, -1), 0.5, math.SmallestNonzeroFloat64, math.MaxFloat64} {
		at, _ := lt.Float64(number)
		interval, _ := lt.Float64Interval(number)
		legacy, exact, err := EncodeLogicalTimePair(lt.HLAfloat64Time, at)
		if err != nil || legacy != number {
			t.Fatalf("float legacy changed: %v %v", legacy, err)
		}
		for _, message := range []*rtiv1.LogicalTimeValue{nil, exact} {
			got, err := DecodeLogicalTimePair(lt.HLAfloat64Time, legacy, message)
			if err != nil || got != at {
				t.Fatalf("float time: %v %v", got, err)
			}
			gotInterval, err := DecodeLogicalTimeIntervalPair(lt.HLAfloat64Time, legacy, message)
			if err != nil || gotInterval != interval {
				t.Fatalf("float interval: %v %v", gotInterval, err)
			}
			optional, err := DecodeOptionalLogicalTimePair(lt.HLAfloat64Time, &legacy, message)
			if err != nil || optional == nil || *optional != at {
				t.Fatalf("float optional: %v %v", optional, err)
			}
		}
		// No presence on a required proto3 scalar: a default zero cannot contradict an exact value.
		got, err := DecodeLogicalTimePair(lt.HLAfloat64Time, 0, exact)
		if err != nil || got != at {
			t.Fatalf("exact-only float: %v %v", got, err)
		}
	}
}

func TestLogicalTimePairRejectsContradictions(t *testing.T) {
	integer := &rtiv1.LogicalTimeValue{Value: &rtiv1.LogicalTimeValue_Integer64Value{Integer64Value: 1<<53 + 1}}
	floating := &rtiv1.LogicalTimeValue{Value: &rtiv1.LogicalTimeValue_Float64Value{Float64Value: 2}}
	for _, test := range []struct {
		name   string
		kind   lt.Representation
		legacy float64
		exact  *rtiv1.LogicalTimeValue
	}{
		{"integer-needs-exact", lt.HLAinteger64Time, 0, nil},
		{"integer-nonzero-legacy", lt.HLAinteger64Time, 1, integer},
		{"integer-negative-zero-legacy", lt.HLAinteger64Time, math.Copysign(0, -1), integer},
		{"integer-float-selector", lt.HLAinteger64Time, 0, floating},
		{"float-integer-selector", lt.HLAfloat64Time, 0, integer},
		{"contradictory-float", lt.HLAfloat64Time, 1, floating},
		{"unknown-representation", lt.Unspecified, 0, floating},
		{"empty-selector", lt.HLAfloat64Time, 0, &rtiv1.LogicalTimeValue{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := DecodeLogicalTimePair(test.kind, test.legacy, test.exact); err == nil {
				t.Fatal("accepted invalid pair")
			}
			if _, err := DecodeLogicalTimeIntervalPair(test.kind, test.legacy, test.exact); err == nil {
				t.Fatal("accepted invalid interval pair")
			}
		})
	}
	zero := 0.0
	if _, err := DecodeOptionalLogicalTimePair(lt.HLAinteger64Time, &zero, integer); err == nil {
		t.Fatal("integer accepted explicit legacy zero")
	}
	if _, err := DecodeOptionalLogicalTimeIntervalPair(lt.HLAinteger64Time, &zero, integer); err == nil {
		t.Fatal("interval accepted explicit legacy zero")
	}
	if _, err := DecodeOptionalLogicalTimePair(lt.HLAfloat64Time, &zero, floating); err == nil {
		t.Fatal("float accepted contradictory explicit zero")
	}
	for _, invalid := range []float64{-1, math.NaN(), math.Inf(1), math.Inf(-1)} {
		bad := &rtiv1.LogicalTimeValue{Value: &rtiv1.LogicalTimeValue_Float64Value{Float64Value: invalid}}
		for _, exact := range []*rtiv1.LogicalTimeValue{nil, bad, floating} {
			if _, err := DecodeLogicalTimePair(lt.HLAfloat64Time, invalid, exact); err == nil {
				t.Fatal("accepted invalid float")
			}
			if _, err := DecodeLogicalTimeIntervalPair(lt.HLAfloat64Time, invalid, exact); err == nil {
				t.Fatal("accepted invalid float interval")
			}
		}
	}
	negative := &rtiv1.LogicalTimeValue{Value: &rtiv1.LogicalTimeValue_Integer64Value{Integer64Value: -1}}
	if _, err := DecodeLogicalTimePair(lt.HLAinteger64Time, 0, negative); err == nil {
		t.Fatal("accepted negative integer")
	}
}

func TestLogicalTimePairOptionalAbsence(t *testing.T) {
	for _, kind := range []lt.Representation{lt.HLAfloat64Time, lt.HLAinteger64Time} {
		at, err := DecodeOptionalLogicalTimePair(kind, nil, nil)
		if err != nil || at != nil {
			t.Fatalf("optional time absence: %v %v", at, err)
		}
		interval, err := DecodeOptionalLogicalTimeIntervalPair(kind, nil, nil)
		if err != nil || interval != nil {
			t.Fatalf("optional interval absence: %v %v", interval, err)
		}
		legacy, exact, err := EncodeOptionalLogicalTimePair(kind, nil)
		if err != nil || legacy != nil || exact != nil {
			t.Fatal("optional time absence encoded")
		}
		legacy, exact, err = EncodeOptionalLogicalTimeIntervalPair(kind, nil)
		if err != nil || legacy != nil || exact != nil {
			t.Fatal("optional interval absence encoded")
		}
	}
	if _, err := DecodeOptionalLogicalTimePair(lt.Unspecified, nil, nil); err == nil {
		t.Fatal("unknown representation bypassed")
	}
}

func TestLogicalTimePairEncoderValidity(t *testing.T) {
	at, _ := lt.Float64(0)
	interval, _ := lt.Float64Interval(0)
	legacy, exact, err := EncodeOptionalLogicalTimePair(lt.HLAfloat64Time, &at)
	if err != nil || legacy == nil || *legacy != 0 || exact == nil {
		t.Fatal("Float64 optional zero lost presence")
	}
	got, err := DecodeOptionalLogicalTimePair(lt.HLAfloat64Time, nil, exact)
	if err != nil || got == nil || *got != at {
		t.Fatal("exact-only optional Float64 failed")
	}
	legacy, exact, err = EncodeOptionalLogicalTimeIntervalPair(lt.HLAfloat64Time, &interval)
	if err != nil || legacy == nil || *legacy != 0 || exact == nil {
		t.Fatal("Float64 interval optional zero lost presence")
	}
	gotInterval, err := DecodeOptionalLogicalTimeIntervalPair(lt.HLAfloat64Time, nil, exact)
	if err != nil || gotInterval == nil || *gotInterval != interval {
		t.Fatal("exact-only optional Float64 interval failed")
	}
	for _, kind := range []lt.Representation{lt.HLAinteger64Time, lt.Unspecified, lt.Representation(255)} {
		if _, _, err := EncodeLogicalTimePair(kind, at); err == nil {
			t.Fatal("encoded mismatched/unknown time")
		}
		if _, _, err := EncodeLogicalTimeIntervalPair(kind, interval); err == nil {
			t.Fatal("encoded mismatched/unknown interval")
		}
		if _, _, err := EncodeOptionalLogicalTimePair(kind, &at); err == nil {
			t.Fatal("encoded mismatched/unknown optional time")
		}
		if _, _, err := EncodeOptionalLogicalTimeIntervalPair(kind, &interval); err == nil {
			t.Fatal("encoded mismatched/unknown optional interval")
		}
	}
	if _, _, err := EncodeLogicalTimePair(lt.HLAfloat64Time, lt.Time{}); err == nil {
		t.Fatal("encoded uninitialized time")
	}
	if _, _, err := EncodeLogicalTimeIntervalPair(lt.HLAfloat64Time, lt.Interval{}); err == nil {
		t.Fatal("encoded uninitialized interval")
	}
	if _, _, err := EncodeOptionalLogicalTimePair(lt.Unspecified, nil); err == nil {
		t.Fatal("nil bypassed selection validation")
	}
	if _, _, err := EncodeOptionalLogicalTimeIntervalPair(lt.Unspecified, nil); err == nil {
		t.Fatal("nil interval bypassed selection validation")
	}
}
