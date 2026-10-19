package mom

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	"strconv"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestParameterIntegerPreservesSignedWireValues(t *testing.T) {
	const class, parameter = "integerRequest", "integerValue"
	fom := &stubFOMHandle{
		icByName: map[string]core.InteractionClassHandle{class: 1},
		paramByH: map[core.InteractionClassHandle]map[string]core.ParameterHandle{1: {parameter: 2}},
	}
	for _, tc := range []struct {
		wire []byte
		want int32
	}{
		{[]byte{0x80, 0, 0, 0}, math.MinInt32},
		{[]byte{0xff, 0xff, 0xff, 0xff}, -1},
		{[]byte{0, 0, 0, 0}, 0},
		{[]byte{0x7f, 0xff, 0xff, 0xff}, math.MaxInt32},
	} {
		got, err := parameterInteger(DispatchContext{FOM: fom}, class, parameter, map[core.ParameterHandle][]byte{2: tc.wire})
		if err != nil || got != tc.want {
			t.Fatalf("decode %x = %d, %v; want %d", tc.wire, got, err, tc.want)
		}
	}
}

func TestArrayLengthBounds(t *testing.T) {
	for _, count := range []int{0, 1, math.MaxInt32} {
		encoded, err := encodeArrayLength(count)
		if err != nil || uint64(binary.BigEndian.Uint32(encoded)) != uint64(count) {
			t.Fatalf("encode length %d: %x, %v", count, encoded, err)
		}
	}
	if _, err := encodeArrayLength(-1); !errors.Is(err, ErrCounterOverflow) {
		t.Fatalf("negative length: %v", err)
	}
	if strconv.IntSize == 64 {
		maximum := uint64(math.MaxUint32)
		encoded, err := encodeArrayLength(int(maximum))
		if err != nil || binary.BigEndian.Uint32(encoded) != math.MaxUint32 {
			t.Fatalf("maximum array length: %x, %v", encoded, err)
		}
		if _, err := encodeArrayLength(int(maximum + 1)); !errors.Is(err, ErrCounterOverflow) {
			t.Fatalf("oversized array length: %v", err)
		}
	}
}

func TestStringListElementAlignment(t *testing.T) {
	encoded, err := encodeStringList([]string{"a", "b"})
	want := []byte{0, 0, 0, 2, 0, 0, 0, 1, 0, 'a', 0, 0, 0, 0, 0, 1, 0, 'b'}
	if err != nil || !bytes.Equal(encoded, want) {
		t.Fatalf("aligned string list: %x, %v; want %x", encoded, err, want)
	}
}

func TestElapsedMillisecondsBounds(t *testing.T) {
	for _, tc := range []struct {
		value time.Duration
		want  uint64
		valid bool
	}{
		{-time.Nanosecond, 0, false},
		{0, 0, true},
		{time.Millisecond - time.Nanosecond, 0, true},
		{time.Millisecond, 1, true},
		{time.Duration(math.MaxInt64), uint64(math.MaxInt64) / uint64(time.Millisecond), true},
	} {
		got, valid := elapsedMilliseconds(tc.value)
		if got != tc.want || valid != tc.valid {
			t.Fatalf("elapsed %v = %d, %v; want %d, %v", tc.value, got, valid, tc.want, tc.valid)
		}
	}
}
