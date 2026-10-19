package eventlog

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
)

func TestReplayerComparisonUsesOriginalWireBytes(t *testing.T) {
	source := buildSyntheticSourceLog(t, "raw", 0)
	// Seq=1 in a valid, non-canonical varint representation. Remarshalling
	// the decoded source would hide this difference from the captured bytes.
	body := []byte{0x08, 0x81, 0x00}
	var prefix [4]byte
	binary.LittleEndian.PutUint32(prefix[:], uint32(len(body)))
	source = append(source, prefix[:]...)
	source = append(source, body...)
	reader, err := NewReader(bytes.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	writer, captured := newCapturingSink(t, "raw")
	t.Cleanup(func() { _ = writer.Close() })
	replayer, err := NewReplayer(ReplayerOptions{Source: reader, CapturingSink: writer, CapturedBuffer: captured})
	if err != nil {
		t.Fatal(err)
	}
	if err := replayer.Replay(context.Background()); !errors.Is(err, ErrReplayDivergence) {
		t.Fatalf("original bytes were not compared: %v", err)
	}
}

func TestReplayerComparisonRejectsSameLengthMutation(t *testing.T) {
	source := buildProtoSourceLog(t, "mutation", &rtiv1.Event{Body: &rtiv1.Event_FedJoined{FedJoined: &rtiv1.FederateJoined{FederateHandle: 1, FederateName: testAlphaName}}})
	reader, err := NewReader(bytes.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	writer, captured := newCapturingSink(t, "mutation")
	t.Cleanup(func() { _ = writer.Close() })
	writer.opts.Sink = replayMutatingSink{captured}
	replayer, err := NewReplayer(ReplayerOptions{Source: reader, CapturingSink: writer, CapturedBuffer: captured})
	if err != nil {
		t.Fatal(err)
	}
	if err := replayer.Replay(context.Background()); !errors.Is(err, ErrReplayDivergence) {
		t.Fatalf("same-length mutation accepted: %v", err)
	}
	if len(source) != captured.Len() || bytes.Equal(source, captured.Bytes()) {
		t.Fatal("fixture did not produce a same-length mutation")
	}
}

type replayMutatingSink struct{ target *bytes.Buffer }

func (s replayMutatingSink) Write(frame []byte) (int, error) {
	return s.target.Write(bytes.ReplaceAll(frame, []byte(testAlphaName), []byte("bravo")))
}

func TestReplayerComparisonRejectsSourceAfterHalt(t *testing.T) {
	source := buildProtoSourceLog(t, "trailing", &rtiv1.Event{Body: &rtiv1.Event_Halted{Halted: &rtiv1.EventFederationHalted{}}}, &rtiv1.Event{})
	reader, err := NewReader(bytes.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	writer, captured := newCapturingSink(t, "trailing")
	t.Cleanup(func() { _ = writer.Close() })
	replayer, err := NewReplayer(ReplayerOptions{Source: reader, CapturingSink: writer, CapturedBuffer: captured})
	if err != nil {
		t.Fatal(err)
	}
	if err := replayer.Replay(context.Background()); !errors.Is(err, ErrReplayDivergence) {
		t.Fatalf("source after terminal event was ignored: %v", err)
	}
}
