package server

import (
	"context"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/pkg/federate"
)

func TestGoSDKDeferredSyncConfirmationRetainsCallback(t *testing.T) {
	for _, scalar := range []string{float64TimeName, integer64TimeName} {
		t.Run(scalar, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			_, transport, _ := integerProductionServer(t, t.TempDir(), 792)
			connection, err := federate.Connect(ctx, transport.Target())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = connection.Close() })
			spec := federate.FederationSpec{Name: "sync-checkpoint-sdk", LogicalTimeImplementationName: scalar}
			if _, err := connection.CreateFederation(ctx, spec); err != nil {
				t.Fatal(err)
			}
			participant, err := connection.JoinFederation(ctx, spec, "member")
			if err != nil {
				t.Fatal(err)
			}
			dispatch := func(event federate.Event) {
				t.Helper()
				if callbackErr, reportErr := participant.DispatchEvent(ctx, event, func(federate.Event) error { return nil }); callbackErr != nil || reportErr != nil {
					t.Fatal(callbackErr, reportErr)
				}
			}
			next := func(match func(federate.Event) bool) federate.Event {
				t.Helper()
				for {
					select {
					case event, open := <-participant.Events():
						if !open {
							t.Fatal("callback stream closed")
						}
						if match(event) {
							return event
						}
						dispatch(event)
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					}
				}
			}
			if err := participant.RegisterFederationSynchronizationPoint(ctx, "held", []byte{0, 255}, nil); err != nil {
				t.Fatal(err)
			}
			held := next(func(event federate.Event) bool {
				_, ok := event.(federate.SynchronizationPointRegistrationSucceeded)
				return ok
			})
			if err := participant.RequestFederationSave(ctx, "active", nil); err != nil {
				t.Fatal(err)
			}
			dispatch(next(func(event federate.Event) bool { _, ok := event.(federate.InitiateFederateSave); return ok }))
			calls := 0
			body := func(_ federate.Event) error { calls++; return nil }
			for retry := 0; retry < 2; retry++ {
				callbackErr, reportErr := participant.DispatchEvent(ctx, held, body)
				if callbackErr != nil || reportErr == nil || calls != 0 {
					t.Fatalf("callback entered during save: calls=%d body=%v admission=%v", calls, callbackErr, reportErr)
				}
			}
			if err := participant.FederateSaveBegun(ctx); err != nil {
				t.Fatal(err)
			}
			if err := participant.FederateSaveNotComplete(ctx); err != nil {
				t.Fatal(err)
			}
			terminal := next(func(event federate.Event) bool { _, ok := event.(federate.FederationNotSaved); return ok })
			if _, err := participant.DispatchEvent(ctx, held, body); err == nil || calls != 0 {
				t.Fatal("callback entered before terminal invocation")
			}
			dispatch(terminal)
			for retry := 0; retry < 2; retry++ {
				if callbackErr, reportErr := participant.DispatchEvent(ctx, held, body); callbackErr != nil || reportErr != nil {
					t.Fatal(callbackErr, reportErr)
				}
			}
			if calls != 1 {
				t.Fatalf("callback count=%d, want exactly one", calls)
			}
		})
	}
}
