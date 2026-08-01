// M20.6 — production emitter unit tests. Verify that
// NewProductionEmitter resolves names to handles and sends a
// ReceiveInteraction event through the supplied Outbox.

package mom

import (
	"context"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
)

// captureOutbox records every Send call so tests can assert against
// the resulting proto.
type captureOutbox struct {
	sent []capturedSend
}

type capturedSend struct {
	Fed       core.FederationName
	Recipient core.FederateHandle
	Event     core.OutboundEvent
}

func (c *captureOutbox) Send(
	_ context.Context,
	fed core.FederationName,
	recipient core.FederateHandle,
	evt core.OutboundEvent,
) error {
	c.sent = append(c.sent, capturedSend{fed, recipient, evt})
	return nil
}

func TestProductionEmitter_ResolvesNamesAndSends(t *testing.T) {
	out := &captureOutbox{}
	emit := NewProductionEmitter(out)

	fom := buildSwitchFOM() // declares HLArequest classes too
	// Use a class+params that exist in the stub FOM.
	resp := ResponseInteraction{
		ClassName: ClassFederationSetSwitches, // reuse — exists in stub
		Params: map[string][]byte{
			"HLAautoProvide": {0x01},
		},
	}
	if err := emit(context.Background(), "f1", 7, resp, fom, fom); err != nil {
		t.Fatalf("emit: %v", err)
	}
	if len(out.sent) != 1 {
		t.Fatalf("captured %d sends, want 1", len(out.sent))
	}
	got := out.sent[0]
	if got.Recipient != 7 {
		t.Errorf("recipient = %d, want 7", got.Recipient)
	}
	// Unwrap the proto.
	inner := got.Event.(interface {
		Inner() *rtiv1.FederateEvent
	}).Inner()
	r := inner.GetReceive()
	if r == nil {
		t.Fatalf("event missing ReceiveInteraction wrapper")
	}
	// Class handle should be 100 per buildSwitchFOM.
	if r.GetInteractionClassHandle() != 100 {
		t.Errorf("class handle = %d, want 100", r.GetInteractionClassHandle())
	}
	// Parameter handle for HLAautoProvide is 1; value [0x01].
	if got, ok := r.Parameters[1]; !ok || len(got) != 1 || got[0] != 0x01 {
		t.Errorf("HLAautoProvide param = %v (ok=%v), want [0x01] ok=true", got, ok)
	}
}

func TestProductionEmitter_UnknownClassSilentlyDrops(t *testing.T) {
	out := &captureOutbox{}
	emit := NewProductionEmitter(out)
	fom := buildSwitchFOM()
	resp := ResponseInteraction{
		ClassName: "HLAmanager.SomeClassNotInFOM",
	}
	if err := emit(context.Background(), "f1", 7, resp, fom, fom); err != nil {
		t.Errorf("emit with unknown class: want nil, got %v", err)
	}
	if len(out.sent) != 0 {
		t.Errorf("unknown class produced %d sends, want 0", len(out.sent))
	}
}

func TestProductionEmitter_UnknownParamErrors(t *testing.T) {
	out := &captureOutbox{}
	emit := NewProductionEmitter(out)
	fom := buildSwitchFOM()
	resp := ResponseInteraction{
		ClassName: ClassFederationSetSwitches,
		Params: map[string][]byte{
			"NotInFOM": {0x01},
		},
	}
	err := emit(context.Background(), "f1", 7, resp, fom, fom)
	if err == nil {
		t.Errorf("unknown parameter: want error, got nil")
	}
}
