package m10spec

import (
	"context"
	"sync"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// fakeOutbox + permissiveFOMRepo mirror the pattern from M3/M5/M7/M8/M11.

type sentRecord struct {
	Federation core.FederationName
	Federate   core.FederateHandle
	Event      core.OutboundEvent
}

type fakeOutbox struct {
	mu   sync.Mutex
	sent []sentRecord
}

func newFakeOutbox() *fakeOutbox { return &fakeOutbox{} }

func (o *fakeOutbox) Send(_ context.Context, fed core.FederationName, h core.FederateHandle, evt core.OutboundEvent) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.sent = append(o.sent, sentRecord{fed, h, evt})
	return nil
}

func (o *fakeOutbox) Sent() []sentRecord {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]sentRecord, len(o.sent))
	copy(out, o.sent)
	return out
}

// permissiveFOMRepo accepts every Load and resolves all lookups to
// handle 1. Cut-1 simplification: real FOM-driven routing-space
// lookups are M10 W1's job; for the spec tests' lifecycle assertions
// the stub is sufficient.

type permissiveFOMRepo struct{}

func newPermissiveFOMRepo() *permissiveFOMRepo { return &permissiveFOMRepo{} }

func (r *permissiveFOMRepo) Load(_ context.Context, _ []core.FOMModule) (core.FOMHandle, error) {
	return &permissiveFOMHandle{}, nil
}

func (r *permissiveFOMRepo) Get(_ context.Context, _ core.FederationName) (core.FOMHandle, error) {
	return &permissiveFOMHandle{}, nil
}

type permissiveFOMHandle struct{}

func (*permissiveFOMHandle) IsValid() bool                                           { return true }
func (*permissiveFOMHandle) LookupObjectClass(string) (core.ObjectClassHandle, bool) { return 1, true }
func (*permissiveFOMHandle) LookupInteractionClass(string) (core.InteractionClassHandle, bool) {
	return 1, true
}
func (*permissiveFOMHandle) LookupAttribute(core.ObjectClassHandle, string) (core.AttributeHandle, bool) {
	return 1, true
}
func (*permissiveFOMHandle) LookupParameter(core.InteractionClassHandle, string) (core.ParameterHandle, bool) {
	return 1, true
}
