package eventlog

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// The dispatch fixture now explicitly implements recovery admission. Keeping
// its expected handle counter catches mismatched records without calling the
// public Register service or changing the fixture's state on rejection.
func (s *stubObjects) ReplayObjectRegistration(_ context.Context, fed core.FederationName, record core.ObjectRegistrationRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if record.Object != s.nextObjectHandle+1 {
		return fmt.Errorf("%w: fixture expected handle %d, got %d", core.ErrObjectRegistrationReplayState, s.nextObjectHandle+1, record.Object)
	}
	s.registerCalls = append(s.registerCalls, registerCall{fed, record.Registrant, record.Class, record.Name})
	s.nextObjectHandle = record.Object
	return nil
}

func (e *errorObjects) ReplayObjectRegistration(context.Context, core.FederationName, core.ObjectRegistrationRecord) error {
	return e.err
}

func TestIndependentReplayRegistrationRequiresRecoveryPort(t *testing.T) {
	source := buildProtoSourceLog(t, "no-recovery-port", &rtiv1.Event{Body: &rtiv1.Event_ObjRegistered{
		ObjRegistered: &rtiv1.ObjectRegistered{ObjectHandle: 1, ObjectClassHandle: 1, OwnerFederateHandle: 1, ObjectName: "recorded"},
	}})
	reader, err := NewReader(bytes.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	writer, _ := newCapturingSink(t, "no-recovery-port")
	defer writer.Close()
	objects := &stubObjects{}
	// Expose ordinary services only. The replayer must not fall back to Register.
	ordinary := struct{ core.ObjectRegistry }{objects}
	replayer, err := NewReplayer(ReplayerOptions{Source: reader, Objects: ordinary, CapturingSink: writer})
	if err != nil {
		t.Fatal(err)
	}
	if err := replayer.Replay(context.Background()); err == nil {
		t.Fatal("replay without recovery admission succeeded")
	}
	if len(objects.registerCalls) != 0 {
		t.Fatal("replay fell back to the public registration service")
	}
}

func TestIndependentReplayRegistrationPassthroughPreservesTimestamp(t *testing.T) {
	source := buildProtoSourceLog(t, "registration-passthrough", &rtiv1.Event{WallNs: 987654321, Body: &rtiv1.Event_ObjRegistered{
		ObjRegistered: &rtiv1.ObjectRegistered{ObjectHandle: 17, ObjectClassHandle: 3, OwnerFederateHandle: 1, ObjectName: "recorded"},
	}})
	reader, err := NewReader(bytes.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	writer, captured := newCapturingSink(t, "registration-passthrough")
	defer writer.Close()
	replayer, err := NewReplayer(ReplayerOptions{Source: reader, CapturingSink: writer, CapturedBuffer: captured})
	if err != nil {
		t.Fatal(err)
	}
	if err := replayer.Replay(context.Background()); err != nil {
		t.Fatalf("registration frame passthrough changed source bytes: %v", err)
	}
}
