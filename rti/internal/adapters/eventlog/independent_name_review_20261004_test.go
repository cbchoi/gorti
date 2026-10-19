package eventlog_test

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/adapters/eventlog"
	"github.com/cbchoi/gorti/rti/internal/application/composition"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
)

type independentNameReviewOutbox struct{}

func (independentNameReviewOutbox) Send(context.Context, core.FederationName, core.FederateHandle, core.OutboundEvent) error {
	return nil
}

func TestIndependentNameReviewReplayRealRegistryRegistration(t *testing.T) {
	for _, name := range []string{"reserved-object", ""} {
		mode := "explicit"
		if name == "" {
			mode = "generated"
		}
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			const fed core.FederationName = "name-review"
			clock := core.NewManualClock(time.Unix(1, 0))
			repo := composition.NewFOMRepository()
			model, err := repo.Load(ctx, []core.FOMModule{{Path: "name-review.xml", XML: []byte(`<objectModel><objects><objectClass><name>HLAobjectRoot</name><objectClass><name>Vehicle</name><attribute><name>Position</name><dataType>HLAinteger32BE</dataType></attribute></objectClass></objectClass></objects></objectModel>`)}})
			if err != nil {
				t.Fatal(err)
			}
			repo.RememberFor(fed, model)
			class, ok := model.LookupObjectClass("Vehicle")
			if !ok {
				t.Fatal("class missing")
			}
			attribute, ok := model.LookupAttribute(class, "Position")
			if !ok {
				t.Fatal("attribute missing")
			}
			newRegistry := func(sink *bytes.Buffer) (*object.Registry, *eventlog.Writer) {
				t.Helper()
				writer, err := eventlog.NewWriter(eventlog.WriterOptions{Sink: sink, Federation: fed, Clock: clock, Mode: core.ModeVerbose})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = writer.Close() })
				decl := declaration.New()
				decl.SetFOMRepository(repo)
				if err := decl.PublishObjectClassAttributes(ctx, fed, 1, class, []core.AttributeHandle{attribute}); err != nil {
					t.Fatal(err)
				}
				registry, err := object.New(object.Options{Clock: clock, FOMs: repo, Declarations: decl, Outbox: independentNameReviewOutbox{}, EventLog: writer})
				if err != nil {
					t.Fatal(err)
				}
				return registry, writer
			}
			var original, captured bytes.Buffer
			source, _ := newRegistry(&original)
			if name != "" {
				if err := source.ReserveObjectInstanceName(ctx, fed, 1, name); err != nil {
					t.Fatal(err)
				}
			}
			handle, canonical, err := source.Register(ctx, fed, 1, class, name)
			if err != nil {
				t.Fatal(err)
			}
			clock.Advance(time.Hour)
			target, writer := newRegistry(&captured)
			if _, _, err := target.Register(ctx, fed, 1, class, "unreserved-control"); !errors.Is(err, core.ErrObjectInstanceNameNotReserved) {
				t.Fatalf("public guard disabled: %v", err)
			}
			reader, err := eventlog.NewReader(bytes.NewReader(original.Bytes()))
			if err != nil {
				t.Fatal(err)
			}
			defer reader.Close()
			replayer, err := eventlog.NewReplayer(eventlog.ReplayerOptions{Source: reader, Objects: target, CapturingSink: writer, CapturedBuffer: &captured})
			if err != nil {
				t.Fatal(err)
			}
			if err := replayer.Replay(ctx); err != nil {
				t.Fatalf("an admitted %s registration cannot replay through the real registry: %v", mode, err)
			}
			if actual, ok := target.LookupObjectInstanceByName(fed, canonical); !ok || actual != handle {
				t.Fatalf("replayed identity=%d,%t want %d", actual, ok, handle)
			}
			state := target.ExportState(fed)
			if state.LastObjectHandle != uint64(handle) || len(state.Reservations) != 0 {
				t.Fatalf("replay invented an allocation or reservation: %+v", state)
			}
			if _, _, err := target.Register(ctx, fed, 1, class, "still-unreserved"); !errors.Is(err, core.ErrObjectInstanceNameNotReserved) {
				t.Fatalf("replay weakened subsequent public registration: %v", err)
			}
		})
	}
}
