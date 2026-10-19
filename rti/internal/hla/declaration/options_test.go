package declaration

import (
	"context"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type optionsOutbox struct {
	events     []*AdvisoryEvent
	recipients []core.FederateHandle
}

func (o *optionsOutbox) Send(_ context.Context, _ core.FederationName, h core.FederateHandle, event core.OutboundEvent) error {
	if e, ok := event.(*AdvisoryEvent); ok {
		o.events = append(o.events, e)
		o.recipients = append(o.recipients, h)
	}
	return nil
}

func TestRegistrationRelevanceRequiresMatchingRemoteAttribute(t *testing.T) {
	m := New()
	out := &optionsOutbox{}
	m.SetAdvisoryOutbox(out)
	ctx := context.Background()
	_ = m.PublishObjectClassAttributes(ctx, "f", 1, 10, []core.AttributeHandle{1})
	_ = m.PublishObjectClassAttributes(ctx, "f", 2, 10, []core.AttributeHandle{2})
	_ = m.SubscribeObjectClassAttributes(ctx, "f", 1, 10, []core.AttributeHandle{1})
	if len(out.events) != 0 {
		t.Fatalf("self subscription generated advisory: %v", out.recipients)
	}
	_ = m.SubscribeObjectClassAttributes(ctx, "f", 3, 10, []core.AttributeHandle{2})
	if !reflect.DeepEqual(out.recipients, []core.FederateHandle{2}) {
		t.Fatalf("advisories to %v, want only matching remote publisher 2", out.recipients)
	}
}

func TestRegistrationRelevanceOnLatePublication(t *testing.T) {
	m := New()
	out := &optionsOutbox{}
	m.SetAdvisoryOutbox(out)
	ctx := context.Background()
	_ = m.SubscribeObjectClassAttributes(ctx, "f", 2, 10, []core.AttributeHandle{1})
	_ = m.PublishObjectClassAttributes(ctx, "f", 1, 10, []core.AttributeHandle{1})
	if len(out.events) != 1 || out.events[0].Kind != AdvisoryStartRegistration {
		t.Fatalf("late publisher got %v, want start registration", out.events)
	}
}
