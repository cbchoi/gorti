package object_test

import (
	"context"
	"strings"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/adapters/savebundle"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
)

func TestG13TypedAdvanceModes(t *testing.T) {
	t.Run("Float64", g13AdvanceModes[core.LogicalTime])
	t.Run("Integer64", g13AdvanceModes[int64])
}

func g13AdvanceModes[T core.LogicalTimeScalar](t *testing.T) {
	for _, mode := range []string{"TAR", "TARA", "NMR", "NMRA", "FQR"} {
		for _, restore := range []bool{false, true} {
			name := mode
			if restore {
				name += "-restore"
			}
			t.Run(name, func(t *testing.T) {
				f := newDelayedSubscriptionFixtureOf[T](t, delayedSubscriptionReviewXML)
				ctx := context.Background()
				at := T(10)
				if err := f.objects.SendInteraction(ctx, "delay", f.producer, f.interaction, map[core.ParameterHandle][]byte{f.parameter: []byte("canonical")}, &at); err != nil {
					t.Fatal(err)
				}
				if restore {
					codec := savebundle.TimeOf[T]{Manager: f.times}
					data, err := codec.Marshal("delay")
					if err != nil {
						t.Fatal(err)
					}
					if !strings.Contains(string(data), `"delayedSubscription":true`) {
						t.Fatal("canonical obligation absent from typed checkpoint")
					}
					if err := codec.Unmarshal("delay", data); err != nil {
						t.Fatal(err)
					}
				}
				if err := f.declarations.SubscribeInteractionClass(ctx, "delay", f.receiver, f.interaction); err != nil {
					t.Fatal(err)
				}
				var err error
				switch mode {
				case "TAR":
					err = f.times.TimeAdvanceRequest(ctx, "delay", f.receiver, at)
				case "TARA":
					err = f.times.TimeAdvanceRequestAvailable(ctx, "delay", f.receiver, at)
				case "NMR":
					err = f.times.NextMessageRequest(ctx, "delay", f.receiver, at)
				case "NMRA":
					err = f.times.NextMessageRequestAvailable(ctx, "delay", f.receiver, at)
				case "FQR":
					err = f.times.FlushQueueRequest(ctx, "delay", f.receiver, at)
				}
				if err != nil {
					t.Fatal(err)
				}
				if err := f.times.TimeAdvanceRequest(ctx, "delay", f.producer, at); err != nil {
					t.Fatal(err)
				}
				callback := f.callback(object.CallbackReceive)
				if callback == nil || string(callback.Parameters[uint64(f.parameter)]) != "canonical" {
					t.Fatal("late subscription lost canonical message", callback)
				}
				count := 0
				for _, d := range f.out.deliveries {
					if d.Recipient == f.receiver {
						if e, ok := d.Event.(*object.CallbackEventOf[T]); ok && e.Kind == object.CallbackReceive {
							count++
						}
					}
				}
				if count != 1 {
					t.Fatalf("received %d copies", count)
				}
			})
		}
	}
}

func TestG13TypedRetractionAndEligibility(t *testing.T) {
	t.Run("Float64", g13RetractionAndEligibility[core.LogicalTime])
	t.Run("Integer64", g13RetractionAndEligibility[int64])
}

func g13RetractionAndEligibility[T core.LogicalTimeScalar](t *testing.T) {
	for _, action := range []string{"retract", "unsubscribe", "no-subscription", "disabled"} {
		t.Run(action, func(t *testing.T) {
			xml := delayedSubscriptionReviewXML
			if action == "disabled" {
				xml = strings.ReplaceAll(xml, `isEnabled="true"`, `isEnabled="false"`)
			}
			f := newDelayedSubscriptionFixtureOf[T](t, xml)
			ctx := context.Background()
			at := T(10)
			if err := f.objects.SendInteractionWithMetadata(ctx, "delay", f.producer, f.interaction, map[core.ParameterHandle][]byte{f.parameter: []byte("retained")}, &at, core.ObjectMessageOptions{RetractionHandle: 77}); err != nil {
				t.Fatal(err)
			}
			if action != "no-subscription" {
				if err := f.declarations.SubscribeInteractionClass(ctx, "delay", f.receiver, f.interaction); err != nil {
					t.Fatal(err)
				}
			}
			if action == "retract" {
				if err := f.times.RetractWithError(ctx, "delay", f.producer, 77); err != nil {
					t.Fatal(err)
				}
			}
			if action == "unsubscribe" {
				if err := f.declarations.UnsubscribeInteractionClass(ctx, "delay", f.receiver, f.interaction); err != nil {
					t.Fatal(err)
				}
			}
			f.releaseTSO(t)
			if f.callback(object.CallbackReceive) != nil {
				t.Fatal("delivery ignored current interest/retraction/disabled pruning")
			}
		})
	}
}

func TestG13TypedAttributeRestore(t *testing.T) {
	t.Run("Float64", g13AttributeRestore[core.LogicalTime])
	t.Run("Integer64", g13AttributeRestore[int64])
}

func g13AttributeRestore[T core.LogicalTimeScalar](t *testing.T) {
	f := newDelayedSubscriptionFixtureOf[T](t, delayedSubscriptionReviewXML)
	ctx := context.Background()
	if err := f.declarations.SubscribeObjectClassAttributes(ctx, "delay", f.receiver, f.class, []core.AttributeHandle{f.first}); err != nil {
		t.Fatal(err)
	}
	instance, _, err := f.objects.Register(ctx, "delay", f.producer, f.class, "")
	if err != nil {
		t.Fatal(err)
	}
	f.out.deliveries = nil
	at := T(10)
	if err := f.objects.UpdateAttributes(ctx, "delay", f.producer, instance, map[core.AttributeHandle][]byte{f.first: []byte("first"), f.second: []byte("second")}, &at); err != nil {
		t.Fatal(err)
	}
	codec := savebundle.TimeOf[T]{Manager: f.times}
	data, err := codec.Marshal("delay")
	if err != nil {
		t.Fatal(err)
	}
	if err := codec.Unmarshal("delay", data); err != nil {
		t.Fatal(err)
	}
	if err := f.declarations.SubscribeObjectClassAttributes(ctx, "delay", f.receiver, f.class, []core.AttributeHandle{f.second}); err != nil {
		t.Fatal(err)
	}
	f.releaseTSO(t)
	event := f.callback(object.CallbackReflect)
	if event == nil || len(event.Attributes) != 2 {
		t.Fatal("checkpoint discarded canonical attribute candidates", event)
	}
}
