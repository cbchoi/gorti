package object

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestSentTransportCapturesMixedAttributePartitionsBeforeCommitHooks(t *testing.T) {
	for _, subscribers := range []bool{false, true} {
		t.Run(map[bool]string{false: "no-recipients", true: "fanout"}[subscribers], func(t *testing.T) {
			f := newTransportationFixture(t)
			ctx := context.Background()
			transportationOK(t, f.r.RequestAttributeTransportationTypeChange(ctx, "fed", 1, f.obj, []core.AttributeHandle{1}, core.TransportTypeBestEffort))
			if !subscribers {
				for _, h := range []core.FederateHandle{2, 3} {
					transportationOK(t, f.decl.UnsubscribeObjectClassAttributes(ctx, "fed", h, 1, nil))
				}
			}
			var observed []core.TransportType
			f.r.opts.OnUpdateSent = func(fed core.FederationName, h core.FederateHandle) {
				transportationOK(t, f.r.RequestAttributeTransportationTypeChange(ctx, fed, h, f.obj, []core.AttributeHandle{1}, core.TransportTypeReliable))
			}
			f.r.opts.OnObjectSentTransport = func(fed core.FederationName, h core.FederateHandle, class core.ObjectClassHandle, transport core.TransportType) {
				if fed != "fed" || h != 1 || class != 1 {
					t.Fatal("wrong source identity")
				}
				observed = append(observed, transport)
			}
			transportationOK(t, f.r.UpdateAttributes(ctx, "fed", 1, f.obj, map[core.AttributeHandle][]byte{1: {1}, 2: {2}}, nil))
			if !reflect.DeepEqual(observed, []core.TransportType{core.TransportTypeReliable, core.TransportTypeBestEffort}) {
				t.Fatal("fanout multiplication or mutable postcommit transport read", observed)
			}
		})
	}
}

func TestSentTransportInteractionUsesAcceptedChoiceNotPostcommitOverride(t *testing.T) {
	f := newTransportationFixture(t)
	ctx := context.Background()
	for _, h := range []core.FederateHandle{2, 3} {
		transportationOK(t, f.decl.UnsubscribeInteractionClass(ctx, "fed", h, 7))
	}
	var observed []core.TransportType
	f.r.opts.OnInteractionSent = func(fed core.FederationName, h core.FederateHandle) {
		transportationOK(t, f.r.RequestInteractionTransportationTypeChange(ctx, fed, h, 7, core.TransportTypeBestEffort))
	}
	f.r.opts.OnInteractionSentTransport = func(_ core.FederationName, _ core.FederateHandle, _ core.InteractionClassHandle, transport core.TransportType) {
		observed = append(observed, transport)
	}
	transportationOK(t, f.r.SendInteraction(ctx, "fed", 1, 7, map[core.ParameterHandle][]byte{1: {1}}, nil))
	if !reflect.DeepEqual(observed, []core.TransportType{core.TransportTypeReliable}) {
		t.Fatal("accepted transport changed by later override", observed)
	}
}

func TestSentTransportFailedAdmissionDoesNotCount(t *testing.T) {
	f := newTransportationFixture(t)
	ctx := context.Background()
	want := errors.New("admission refused")
	f.r.opts.Outbox = &atomicInteractionOutbox{reserveErr: want}
	count := 0
	f.r.opts.OnObjectSentTransport = func(core.FederationName, core.FederateHandle, core.ObjectClassHandle, core.TransportType) { count++ }
	f.r.opts.OnInteractionSentTransport = func(core.FederationName, core.FederateHandle, core.InteractionClassHandle, core.TransportType) {
		count++
	}
	if err := f.r.UpdateAttributes(ctx, "fed", 1, f.obj, map[core.AttributeHandle][]byte{1: {1}}, nil); !errors.Is(err, want) {
		t.Fatal("wrong failed-update result", err)
	}
	if err := f.r.SendInteraction(ctx, "fed", 1, 7, map[core.ParameterHandle][]byte{1: {1}}, nil); !errors.Is(err, want) {
		t.Fatal("wrong failed-send result", err)
	}
	if count != 0 {
		t.Fatal("failed send counted", count)
	}
}
