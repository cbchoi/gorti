package protobuf

import (
	"bytes"
	"errors"
	"math"
	"strconv"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	lt "github.com/cbchoi/gorti/rti/pkg/logicaltime"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

func TestLogicalTimeInteger64Precision(t *testing.T) {
	for _, number := range []int64{0, 1, 1<<53 - 1, 1 << 53, 1<<53 + 1, math.MaxInt64 - 1, math.MaxInt64} {
		t.Run(strconv.FormatInt(number, 10), func(t *testing.T) {
			at, _ := lt.Integer64(number)
			interval, _ := lt.Integer64Interval(number)
			message, err := EncodeLogicalTime(lt.HLAinteger64Time, at)
			if err != nil {
				t.Fatal(err)
			}
			selected, ok := message.Value.(*rtiv1.LogicalTimeValue_Integer64Value)
			if !ok || selected.Integer64Value != number {
				t.Fatalf("wrong integer selector: %v", message)
			}
			wire, err := MarshalLogicalTime(lt.HLAinteger64Time, at)
			want := protowire.AppendVarint([]byte{0x10}, protowire.EncodeZigZag(number))
			if err != nil || !bytes.Equal(wire, want) {
				t.Fatalf("wire %x != %x: %v", wire, want, err)
			}
			decoded, err := UnmarshalLogicalTime(lt.HLAinteger64Time, wire)
			if err != nil || decoded != at {
				t.Fatalf("time lost precision: %v %v", decoded, err)
			}
			decoded, err = DecodeLogicalTime(lt.HLAinteger64Time, message)
			if err != nil || decoded != at {
				t.Fatalf("message time lost precision: %v %v", decoded, err)
			}
			intervalMessage, err := EncodeLogicalTimeInterval(lt.HLAinteger64Time, interval)
			if err != nil || !proto.Equal(intervalMessage, message) {
				t.Fatalf("interval selector differs: %v %v", intervalMessage, err)
			}
			intervalWire, err := MarshalLogicalTimeInterval(lt.HLAinteger64Time, interval)
			if err != nil || !bytes.Equal(intervalWire, wire) {
				t.Fatalf("interval bytes differ: %x %v", intervalWire, err)
			}
			decodedInterval, err := UnmarshalLogicalTimeInterval(lt.HLAinteger64Time, wire)
			if err != nil || decodedInterval != interval {
				t.Fatalf("interval lost precision: %v %v", decodedInterval, err)
			}
			decodedInterval, err = DecodeLogicalTimeInterval(lt.HLAinteger64Time, intervalMessage)
			if err != nil || decodedInterval != interval {
				t.Fatalf("message interval lost precision: %v %v", decodedInterval, err)
			}
		})
	}
}

func TestLogicalTimeFloat64Compatibility(t *testing.T) {
	for _, number := range []float64{0, math.Copysign(0, -1), math.SmallestNonzeroFloat64, 0.5, 1, math.MaxFloat64} {
		at, _ := lt.Float64(number)
		interval, _ := lt.Float64Interval(number)
		wire, err := MarshalLogicalTime(lt.HLAfloat64Time, at)
		if err != nil {
			t.Fatal(err)
		}
		canonical, _ := at.Float64()
		want := protowire.AppendFixed64([]byte{0x09}, math.Float64bits(canonical))
		if !bytes.Equal(wire, want) {
			t.Fatalf("Float64 bits changed: %x != %x", wire, want)
		}
		decoded, err := UnmarshalLogicalTime(lt.HLAfloat64Time, wire)
		if err != nil || decoded != at {
			t.Fatalf("Float64 round trip: %v %v", decoded, err)
		}
		message := &rtiv1.LogicalTimeValue{Value: &rtiv1.LogicalTimeValue_Float64Value{Float64Value: number}}
		decoded, err = DecodeLogicalTime(lt.HLAfloat64Time, message)
		if err != nil || decoded != at {
			t.Fatalf("Float64 message: %v %v", decoded, err)
		}
		intervalMessage, err := EncodeLogicalTimeInterval(lt.HLAfloat64Time, interval)
		if err != nil {
			t.Fatal(err)
		}
		decodedInterval, err := DecodeLogicalTimeInterval(lt.HLAfloat64Time, intervalMessage)
		if err != nil || decodedInterval != interval {
			t.Fatalf("Float64 interval message: %v %v", decodedInterval, err)
		}
		intervalWire, err := MarshalLogicalTimeInterval(lt.HLAfloat64Time, interval)
		if err != nil || !bytes.Equal(wire, intervalWire) {
			t.Fatalf("Float64 interval bytes: %x %v", intervalWire, err)
		}
		decodedInterval, err = UnmarshalLogicalTimeInterval(lt.HLAfloat64Time, intervalWire)
		if err != nil || decodedInterval != interval {
			t.Fatalf("Float64 interval: %v %v", decodedInterval, err)
		}
	}
}

func TestLogicalTimeRejectsInvalidMessages(t *testing.T) {
	integer := func(value int64) *rtiv1.LogicalTimeValue {
		return &rtiv1.LogicalTimeValue{Value: &rtiv1.LogicalTimeValue_Integer64Value{Integer64Value: value}}
	}
	floating := func(value float64) *rtiv1.LogicalTimeValue {
		return &rtiv1.LogicalTimeValue{Value: &rtiv1.LogicalTimeValue_Float64Value{Float64Value: value}}
	}
	unknown := integer(1)
	unknown.ProtoReflect().SetUnknown([]byte{0x18, 0x00})
	for _, tc := range []struct {
		name  string
		kind  lt.Representation
		value *rtiv1.LogicalTimeValue
		want  error
	}{
		{"nil", lt.HLAinteger64Time, nil, ErrInvalidLogicalTimeValue},
		{"missing", lt.HLAinteger64Time, &rtiv1.LogicalTimeValue{}, ErrInvalidLogicalTimeValue},
		{"unknownField", lt.HLAinteger64Time, unknown, ErrInvalidLogicalTimeValue},
		{"nilIntegerSelector", lt.HLAinteger64Time, &rtiv1.LogicalTimeValue{Value: (*rtiv1.LogicalTimeValue_Integer64Value)(nil)}, ErrInvalidLogicalTimeValue},
		{"nilFloatSelector", lt.HLAfloat64Time, &rtiv1.LogicalTimeValue{Value: (*rtiv1.LogicalTimeValue_Float64Value)(nil)}, ErrInvalidLogicalTimeValue},
		{"unspecified", lt.Unspecified, integer(0), lt.ErrUnknownRepresentation},
		{"unknownImplementation", lt.Representation(255), integer(0), lt.ErrUnknownRepresentation},
		{"floatInIntegerFederation", lt.HLAinteger64Time, floating(1), lt.ErrRepresentationMismatch},
		{"integerInFloatFederation", lt.HLAfloat64Time, integer(1), lt.ErrRepresentationMismatch},
		{"negativeInteger", lt.HLAinteger64Time, integer(-1), lt.ErrInvalidValue},
		{"minimumInteger", lt.HLAinteger64Time, integer(math.MinInt64), lt.ErrInvalidValue},
		{"negativeFloat", lt.HLAfloat64Time, floating(-1), lt.ErrInvalidValue},
		{"nan", lt.HLAfloat64Time, floating(math.NaN()), lt.ErrInvalidValue},
		{"positiveInfinity", lt.HLAfloat64Time, floating(math.Inf(1)), lt.ErrInvalidValue},
		{"negativeInfinity", lt.HLAfloat64Time, floating(math.Inf(-1)), lt.ErrInvalidValue},
	} {
		t.Run(tc.name, func(t *testing.T) {
			at, err := DecodeLogicalTime(tc.kind, tc.value)
			if !errors.Is(err, tc.want) || at.Representation() != lt.Unspecified {
				t.Fatalf("time = %v, error = %v; want %v", at, err, tc.want)
			}
			interval, err := DecodeLogicalTimeInterval(tc.kind, tc.value)
			if !errors.Is(err, tc.want) || interval.Representation() != lt.Unspecified {
				t.Fatalf("interval = %v, error = %v; want %v", interval, err, tc.want)
			}
		})
	}
}

func TestLogicalTimeRejectsInvalidOutboundRepresentation(t *testing.T) {
	for _, pair := range [][2]lt.Representation{
		{lt.HLAinteger64Time, lt.HLAfloat64Time}, {lt.HLAfloat64Time, lt.HLAinteger64Time},
		{lt.Unspecified, lt.HLAinteger64Time}, {lt.Representation(255), lt.HLAinteger64Time},
		{lt.HLAinteger64Time, lt.Unspecified},
	} {
		at, _ := pair[1].Initial()
		interval, _ := pair[1].Zero()
		if value, err := EncodeLogicalTime(pair[0], at); err == nil || value != nil {
			t.Fatal("outbound time mismatch accepted")
		}
		if value, err := EncodeLogicalTimeInterval(pair[0], interval); err == nil || value != nil {
			t.Fatal("outbound interval mismatch accepted")
		}
		if value, err := MarshalLogicalTime(pair[0], at); err == nil || value != nil {
			t.Fatal("outbound time bytes mismatch accepted")
		}
		if value, err := MarshalLogicalTimeInterval(pair[0], interval); err == nil || value != nil {
			t.Fatal("outbound interval bytes mismatch accepted")
		}
	}
}

func TestLogicalTimeStrictWireRejectsAmbiguity(t *testing.T) {
	integer := []byte{0x10, 0x02}
	floating := protowire.AppendFixed64([]byte{0x09}, math.Float64bits(1))
	for _, tc := range []struct {
		name string
		wire []byte
	}{
		{"missing", nil},
		{"bothIntegerFirst", append(append([]byte{}, integer...), floating...)},
		{"bothFloatFirst", append(append([]byte{}, floating...), integer...)},
		{"duplicateInteger", append(append([]byte{}, integer...), integer...)},
		{"duplicateFloat", append(append([]byte{}, floating...), floating...)},
		{"unknownOnly", []byte{0x18, 0x02}},
		{"unknownAfterKnown", append(append([]byte{}, integer...), 0x18, 0x00)},
		{"truncatedTag", []byte{0x80}},
		{"truncatedFloat", []byte{0x09, 0x00}},
		{"truncatedInteger", []byte{0x10, 0x80}},
		{"wrongFloatWireType", []byte{0x08, 0x02}},
		{"wrongIntegerWireType", protowire.AppendFixed64([]byte{0x11}, 1)},
		{"zeroTag", []byte{0x00}},
		{"group", []byte{0x13, 0x14}},
		{"varintOverflow", []byte{0x10, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x02}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, kind := range []lt.Representation{lt.HLAinteger64Time, lt.HLAfloat64Time} {
				if at, err := UnmarshalLogicalTime(kind, tc.wire); !errors.Is(err, ErrInvalidLogicalTimeValue) || at.Representation() != lt.Unspecified {
					t.Fatalf("ambiguous/invalid time accepted: %v %v", at, err)
				}
				if interval, err := UnmarshalLogicalTimeInterval(kind, tc.wire); !errors.Is(err, ErrInvalidLogicalTimeValue) || interval.Representation() != lt.Unspecified {
					t.Fatalf("ambiguous/invalid interval accepted: %v %v", interval, err)
				}
			}
		})
	}
}

func TestLogicalTimeStrictWireRejectsInvalidValues(t *testing.T) {
	for _, tc := range []struct {
		kind lt.Representation
		wire []byte
		want error
	}{
		{lt.HLAinteger64Time, []byte{0x10, 0x01}, lt.ErrInvalidValue},
		{lt.HLAfloat64Time, protowire.AppendFixed64([]byte{0x09}, math.Float64bits(math.NaN())), lt.ErrInvalidValue},
		{lt.HLAfloat64Time, protowire.AppendFixed64([]byte{0x09}, math.Float64bits(math.Inf(1))), lt.ErrInvalidValue},
		{lt.HLAfloat64Time, []byte{0x10, 0x00}, lt.ErrRepresentationMismatch},
		{lt.HLAinteger64Time, protowire.AppendFixed64([]byte{0x09}, 0), lt.ErrRepresentationMismatch},
		{lt.Unspecified, []byte{0x10, 0x00}, lt.ErrUnknownRepresentation},
	} {
		if _, err := UnmarshalLogicalTime(tc.kind, tc.wire); !errors.Is(err, tc.want) {
			t.Fatalf("time error = %v, want %v", err, tc.want)
		}
		if _, err := UnmarshalLogicalTimeInterval(tc.kind, tc.wire); !errors.Is(err, tc.want) {
			t.Fatalf("interval error = %v, want %v", err, tc.want)
		}
	}
}
