package server

import (
	"context"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/application/composition"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
)

type momPointCall struct {
	point      core.MOMAttributePoint
	subscriber core.FederateHandle
	values     map[core.AttributeHandle][]byte
}

type momPointDeliveryRecorder struct{ calls []momPointCall }

func (r *momPointDeliveryRecorder) PublishMOMAttributePoint(_ context.Context, _ core.FederationName, _ core.ObjectHandle, _ core.ObjectClassHandle, values map[core.AttributeHandle][]byte, point core.MOMAttributePoint) error {
	r.calls = append(r.calls, momPointCall{point: point, values: values})
	return nil
}

func (r *momPointDeliveryRecorder) ReflectMOMAttributePoint(_ context.Context, _ core.FederationName, subscriber core.FederateHandle, _ core.ObjectHandle, _ core.ObjectClassHandle, values map[core.AttributeHandle][]byte, point core.MOMAttributePoint) error {
	r.calls = append(r.calls, momPointCall{point: point, subscriber: subscriber, values: values})
	return nil
}

func TestMOMAttributePointAdapterExactSubjectAndDimensions(t *testing.T) {
	ctx := context.Background()
	const fed core.FederationName = "mom-point-adapter"
	const subject core.FederateHandle = 211<<32 | 19
	const receiver core.FederateHandle = 211<<32 | 20
	repo := composition.NewFOMRepository()
	fom, err := repo.Load(ctx, []core.FOMModule{{Path: "empty.xml", XML: []byte(`<objectModel><objects><objectClass><name>HLAobjectRoot</name></objectClass></objects></objectModel>`)}})
	if err != nil {
		t.Fatal(err)
	}
	repo.RememberFor(fed, fom)
	class, ok := fom.LookupObjectClass(mom.ClassHLAfederate)
	if !ok {
		t.Fatal("MIM federate class missing")
	}
	attr, ok := fom.LookupAttribute(class, "HLAfederateHandle")
	if !ok {
		t.Fatal("MIM handle attribute missing")
	}
	dimension, ok := fom.(core.FOMHandleNameLookup).LookupDimension("HLAfederate")
	if !ok {
		t.Fatal("MIM federate dimension missing")
	}
	recorder := &momPointDeliveryRecorder{}
	adapter, err := newMOMFederateAttributePublisher(recorder, repo, func(core.FederationName) []core.FederateHandle { return []core.FederateHandle{subject, receiver} })
	if err != nil {
		t.Fatal(err)
	}
	values := map[core.AttributeHandle][]byte{attr: {1, 2, 3}}
	if err := adapter.PublishFederateAttributes(ctx, fed, subject, 8, class, values); err != nil {
		t.Fatal(err)
	}
	if err := adapter.ReflectFederateAttributes(ctx, fed, subject, receiver, 8, class, values); err != nil {
		t.Fatal(err)
	}
	if len(recorder.calls) != 2 {
		t.Fatal("point delivery was not selected")
	}
	for _, call := range recorder.calls {
		if call.point.Subject != subject || call.point.Dimension != core.DDMDimensionHandle(dimension) || call.point.Coordinate != 19 {
			t.Fatalf("wrong normalized single-axis point: %+v", call.point)
		}
	}
	values[attr][0] = 99
	if recorder.calls[0].values[attr][0] != 1 {
		t.Fatal("publication retains mutable caller bytes")
	}
	if recorder.calls[1].subscriber != receiver {
		t.Fatal("late subscriber identity lost")
	}
	for _, mode := range []string{"foreign-subject", "foreign-subscriber", "wrong-class", "wrong-attribute", testCancelledLabel} {
		t.Run(mode, func(t *testing.T) {
			callCtx, caller, sub, cls := ctx, subject, receiver, class
			attrs := map[core.AttributeHandle][]byte{attr: {1}}
			switch mode {
			case "foreign-subject":
				caller += 100
			case "foreign-subscriber":
				sub += 50
			case "wrong-class":
				cls, _ = fom.LookupObjectClass(mom.ClassHLAfederation)
			case "wrong-attribute":
				attrs = map[core.AttributeHandle][]byte{^core.AttributeHandle(0): {1}}
			case testCancelledLabel:
				var cancel context.CancelFunc
				callCtx, cancel = context.WithCancel(ctx)
				cancel()
			}
			before := len(recorder.calls)
			if err := adapter.ReflectFederateAttributes(callCtx, fed, caller, sub, 8, cls, attrs); err == nil {
				t.Fatal("invalid publication accepted")
			}
			if len(recorder.calls) != before {
				t.Fatal("invalid publication reached delivery")
			}
		})
	}
}
