//go:build dds

// Stub-contract tests under //go:build dds. These RUN ONLY in the
// dds-tagged build (`go test -tags=dds ./rti/internal/transport/dds/`)
// and they document the Phase 1a contract: every primitive returns
// errors.ErrUnsupported. Phase 1b's first failing assertion here will
// be the signal that the CGo implementation has landed and these
// tests need to be rewritten as real lifecycle tests.

package dds

import (
	"errors"
	"testing"
)

// TestParticipantStub_AllMethodsReturnErrUnsupported asserts the
// Phase 1a stub is wired for every method on the Participant
// interface. Phase 1b should rewrite this test to assert real
// CGo-backed behavior.
func TestParticipantStub_AllMethodsReturnErrUnsupported(t *testing.T) {
	t.Parallel()
	p := NewParticipant()
	if p == nil {
		t.Fatal("NewParticipant returned nil — the stub must construct an object")
	}
	if err := p.Join(0); !errors.Is(err, errors.ErrUnsupported) {
		t.Errorf("Join: got %v; want errors.ErrUnsupported", err)
	}
	topic, err := p.CreateTopic("gorti/fed/interaction/1", FromHLA(TransportationReliable, OrderReceive))
	if !errors.Is(err, errors.ErrUnsupported) {
		t.Errorf("CreateTopic err: got %v; want errors.ErrUnsupported", err)
	}
	if topic != nil {
		t.Errorf("CreateTopic returned non-nil topic %v on stub", topic)
	}
	if err := p.Close(); !errors.Is(err, errors.ErrUnsupported) {
		t.Errorf("Close: got %v; want errors.ErrUnsupported", err)
	}
}

// TestTopicStub_HoldsConfig asserts the stub Topic preserves the
// (name, qos) pair the CreateTopic caller would have supplied. Even
// in Phase 1a, this lets the federation runtime build a topic
// catalog ahead of Phase 1b lighting up the lifecycle.
func TestTopicStub_HoldsConfig(t *testing.T) {
	t.Parallel()
	qos := FromHLA(TransportationBestEffort, OrderTimeStamp)
	tp := &defaultTopic{name: "gorti/fed/interaction/42", qos: qos}
	if got := tp.Name(); got != "gorti/fed/interaction/42" {
		t.Errorf("Name=%q; want gorti/fed/interaction/42", got)
	}
	if got := tp.QoS(); got != qos {
		t.Errorf("QoS=%+v; want %+v", got, qos)
	}
	if _, err := tp.CreateWriter(); !errors.Is(err, errors.ErrUnsupported) {
		t.Errorf("CreateWriter: got %v; want errors.ErrUnsupported", err)
	}
	if _, err := tp.CreateReader(); !errors.Is(err, errors.ErrUnsupported) {
		t.Errorf("CreateReader: got %v; want errors.ErrUnsupported", err)
	}
	if err := tp.Close(); !errors.Is(err, errors.ErrUnsupported) {
		t.Errorf("Close: got %v; want errors.ErrUnsupported", err)
	}
}

// TestWriterStub_AllMethodsReturnErrUnsupported.
func TestWriterStub_AllMethodsReturnErrUnsupported(t *testing.T) {
	t.Parallel()
	var w Writer = &defaultWriter{}
	if err := w.Write([]byte{1, 2, 3}); !errors.Is(err, errors.ErrUnsupported) {
		t.Errorf("Write: got %v; want errors.ErrUnsupported", err)
	}
	if err := w.Close(); !errors.Is(err, errors.ErrUnsupported) {
		t.Errorf("Close: got %v; want errors.ErrUnsupported", err)
	}
}

// TestReaderStub_AllMethodsReturnErrUnsupported.
func TestReaderStub_AllMethodsReturnErrUnsupported(t *testing.T) {
	t.Parallel()
	var r Reader = &defaultReader{}
	samples, err := r.Take(8)
	if !errors.Is(err, errors.ErrUnsupported) {
		t.Errorf("Take err: got %v; want errors.ErrUnsupported", err)
	}
	if samples != nil {
		t.Errorf("Take samples: got %v; want nil", samples)
	}
	if err := r.Close(); !errors.Is(err, errors.ErrUnsupported) {
		t.Errorf("Close: got %v; want errors.ErrUnsupported", err)
	}
}

// TestSampleStruct_HoldsFields asserts the Sample value object
// preserves Payload + SourceTimestampNS. Phase 1b's CGo Take loop
// will populate these from sample_info_t.
func TestSampleStruct_HoldsFields(t *testing.T) {
	t.Parallel()
	s := Sample{Payload: []byte{0xAA, 0xBB}, SourceTimestampNS: 12345}
	if string(s.Payload) != "\xAA\xBB" {
		t.Errorf("Sample.Payload bytes mismatch: got %v", s.Payload)
	}
	if s.SourceTimestampNS != 12345 {
		t.Errorf("Sample.SourceTimestampNS=%d; want 12345", s.SourceTimestampNS)
	}
}
