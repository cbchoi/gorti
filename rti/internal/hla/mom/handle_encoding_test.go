package mom

import (
	"bytes"
	"context"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// Independent literal vectors: HLAvariableArray<HLAbyte> count, followed
// by the eight BE bytes emitted by cppsdk/src/dlc/Handle.cpp::encode.
func TestMOMHandleEncodingPreservesUint64(t *testing.T) {
	for _, tc := range []struct {
		name string
		h    core.FederateHandle
		want []byte
	}{
		{"small", 7, []byte{0, 0, 0, 8, 0, 0, 0, 0, 0, 0, 0, 7}},
		{"aboveUint32", 0x100000000, []byte{0, 0, 0, 8, 0, 0, 0, 1, 0, 0, 0, 0}},
		{"highBit", 0x8122334481020304, []byte{0, 0, 0, 8, 0x81, 0x22, 0x33, 0x44, 0x81, 2, 3, 4}},
		{"maxUint64", 0xffffffffffffffff, []byte{0, 0, 0, 8, 255, 255, 255, 255, 255, 255, 255, 255}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := encodeHLAhandle(tc.h); !bytes.Equal(got, tc.want) {
				t.Errorf("encoded handle = % x, want % x", got, tc.want)
			}
			m := newTestMOM(t, "handles")
			if err := m.FederateJoined(context.Background(), "handles", tc.h, "target", "test"); err != nil {
				t.Fatal(err)
			}
			emit := &recordingEmitter{}
			d := NewDispatcher(m)
			d.SetEmitter(emit.emit)
			d.SetCounterSource(func(context.Context, core.FederationName, core.FederateHandle, CounterKind) ([]TransportationCounts, error) {
				return []TransportationCounts{{Transportation: "HLAreliable"}}, nil
			})
			fom := buildSwitchFOM()
			if err := d.Dispatch(context.Background(), "handles", ClassRequestInteractionsSent, tc.h, map[core.ParameterHandle][]byte{1: tc.want}, fom, fom); err != nil {
				t.Fatal(err)
			}
			if len(emit.emitted) != 1 {
				t.Fatalf("reports = %d, want 1", len(emit.emitted))
			}
			got := emit.emitted[0]
			if got.Recipient != tc.h || !bytes.Equal(got.Resp.Params["HLAfederate"], tc.want) {
				t.Errorf("report lost or misencoded handle: %+v", got)
			}
		})
	}
}

func TestMOMHandleListEncodingFramesEachUint64(t *testing.T) {
	want := []byte{
		0, 0, 0, 2,
		0, 0, 0, 8, 0, 0, 0, 0, 0, 0, 0, 7,
		0, 0, 0, 8, 0x81, 0x22, 0x33, 0x44, 0x81, 2, 3, 4,
	}
	if got := encodeFederateHandleList([]core.FederateHandle{7, 0x8122334481020304}); !bytes.Equal(got, want) {
		t.Errorf("encoded handle list = % x, want % x", got, want)
	}
	if got := encodeFederateHandleList(nil); !bytes.Equal(got, []byte{0, 0, 0, 0}) {
		t.Errorf("empty handle list = % x", got)
	}
}
