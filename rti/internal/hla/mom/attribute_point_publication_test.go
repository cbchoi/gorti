package mom_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/application/composition"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
)

type attributePointCall struct {
	subject, subscriber core.FederateHandle
	object              core.ObjectHandle
	values              map[core.AttributeHandle][]byte
}

type attributePointPublisher struct {
	calls []attributePointCall
	err   error
}

func (p *attributePointPublisher) PublishFederateAttributes(_ context.Context, _ core.FederationName, subject core.FederateHandle, obj core.ObjectHandle, _ core.ObjectClassHandle, values map[core.AttributeHandle][]byte) error {
	p.calls = append(p.calls, attributePointCall{subject: subject, object: obj, values: values})
	return p.err
}

func (p *attributePointPublisher) ReflectFederateAttributes(_ context.Context, _ core.FederationName, subject, subscriber core.FederateHandle, obj core.ObjectHandle, _ core.ObjectClassHandle, values map[core.AttributeHandle][]byte) error {
	p.calls = append(p.calls, attributePointCall{subject: subject, subscriber: subscriber, object: obj, values: values})
	return p.err
}

func TestMOMFederateAttributePointPublicationInitialLateAndPeriodic(t *testing.T) {
	ctx := context.Background()
	repo := composition.NewFOMRepository()
	fom, err := repo.Load(ctx, []core.FOMModule{{Path: "empty.xml", XML: []byte(`<objectModel><objects><objectClass><name>HLAobjectRoot</name></objectClass></objects></objectModel>`)}})
	if err != nil {
		t.Fatal(err)
	}
	repo.RememberFor(catalogFederation, fom)
	out := &catalogOutbox{}
	schedule := &catalogSchedule{}
	m, err := mom.New(mom.Options{Outbox: out, SchedulePeriodic: schedule.schedule})
	if err != nil {
		t.Fatal(err)
	}
	decl := declaration.New()
	reg, err := object.New(object.Options{Clock: core.NewManualClock(time.Unix(0, 0)), FOMs: repo, Declarations: decl, Outbox: out})
	if err != nil {
		t.Fatal(err)
	}
	m.EnableInstanceFanout(reg, decl, repo)
	publisher := &attributePointPublisher{}
	if err := m.SetFederateAttributePublisher(publisher); err != nil {
		t.Fatal(err)
	}
	if err := m.FederationCreated(ctx, catalogFederation, nil); err != nil {
		t.Fatal(err)
	}
	for _, subject := range []core.FederateHandle{7, catalogTarget} {
		if err := m.FederateJoined(ctx, catalogFederation, subject, "member", "test"); err != nil {
			t.Fatal(err)
		}
	}
	if len(publisher.calls) != 2 || publisher.calls[0].subject != 7 || publisher.calls[1].subject != catalogTarget {
		t.Fatal("initial federate values bypassed subject publication", publisher.calls)
	}
	class, _ := fom.LookupObjectClass(mom.ClassHLAfederate)
	nameAttr, _ := fom.LookupAttribute(class, "HLAfederateName")
	publisher.calls = nil
	m.ObjectClassSubscribed(ctx, catalogFederation, 7, class, []core.AttributeHandle{nameAttr})
	if len(publisher.calls) != 2 {
		t.Fatal("late publication did not retain each subject")
	}
	for _, call := range publisher.calls {
		if call.subscriber != 7 || len(call.values) != 1 || call.values[nameAttr] == nil {
			t.Fatal("late publication leaked unsubscribed values", call)
		}
	}
	if err := m.SetFederateAttributePublisher(&attributePointPublisher{}); err == nil {
		t.Fatal("publisher replaced during live federation")
	}
	d := mom.NewDispatcher(m)
	d.SetEmitter(mom.NewProductionEmitter(out))
	h := &catalogHarness{fom, d, m, decl, out}
	if err := h.request(t, mom.ClassSetTiming, map[string][]byte{"HLAfederate": catalogHandle(uint64(catalogTarget)), "HLAreportPeriod": catalogInt(1)}); err != nil {
		t.Fatal(err)
	}
	publisher.calls = nil
	schedule.call()
	if len(publisher.calls) != 1 || publisher.calls[0].subject != catalogTarget || len(publisher.calls[0].values) != 4 {
		t.Fatal("periodic metric publication lost its subject", publisher.calls)
	}
	publisher.err = errors.New("point publication unavailable")
	schedule.call()
	if _, err := m.PeriodicStatus(catalogFederation, catalogTarget); !errors.Is(err, publisher.err) {
		t.Fatal("point publication failure hidden", err)
	}
	if err := m.FederationDestroyed(ctx, catalogFederation); err != nil {
		t.Fatal(err)
	}
}
