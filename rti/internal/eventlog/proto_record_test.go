package eventlog

import (
	"bytes"
	"context"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
)

// productionEvent is the canonical production wrapper. It satisfies
// proto.Message by embedding *rtiv1.Event (forwarding Reset/String/
// ProtoReflect via method promotion) AND satisfies core.EventRecord by
// exposing Seq().
//
// The writer's reflection-based assignSeq finds the embedded Event's
// Seq field via Go's promotion rules and writes the monotonic seq into
// it. proto.Marshal on the wrapper then emits the wire bytes from the
// embedded Event.
type productionEvent struct {
	*rtiv1.Event
}

// Seq satisfies core.EventRecord. It reads the underlying Event's Seq
// field, which the writer's reflection mutates through the embedded
// pointer.
func (p *productionEvent) Seq() uint64 { return p.Event.GetSeq() }

// TestWriter_Append_ProductionEventPath: a record that satisfies
// proto.Message AND core.EventRecord (the production shape) is marshaled
// via proto.Marshal. Verify the assigned seq survives the round-trip.
func TestWriter_Append_ProductionEventPath(t *testing.T) {
	var buf bytes.Buffer
	w := newWriterForTest(t, "prod", &buf)
	defer w.Close()

	for i := 0; i < 3; i++ {
		evt := &productionEvent{Event: &rtiv1.Event{}}
		if err := w.Append(context.Background(), "prod", evt); err != nil {
			t.Fatalf("Append[%d]: %v", i, err)
		}
		// productionEvent's Seq() reads the embedded Event.Seq, which
		// the writer assigns via reflection on the embedded pointer.
		if got := evt.Seq(); got != uint64(i+1) {
			t.Errorf("Append[%d] Seq() = %d, want %d", i, got, i+1)
		}
	}

	// Round-trip: the wire form is proto.Marshal of *productionEvent,
	// which decodes as *rtiv1.Event since productionEvent's wire shape
	// (via the embedded Event's ProtoReflect) is identical.
	r, err := NewReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	defer r.Close()
	for i := 0; i < 3; i++ {
		rec, err := r.Next(context.Background())
		if err != nil {
			t.Fatalf("Next[%d]: %v", i, err)
		}
		if rec.Seq() != uint64(i+1) {
			t.Errorf("Next[%d].Seq = %d, want %d", i, rec.Seq(), i+1)
		}
	}
}
