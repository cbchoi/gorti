package federate

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func TestGoSDKRejectedCapabilityCannotReportRestoreSuccessReview(t *testing.T) {
	for _, scalar := range []string{HLAfloat64Time, HLAinteger64Time} {
		t.Run(scalar, func(t *testing.T) {
			for _, state := range []string{
				`{"callback_recovery":"server-v3","pending_outcomes":[]}`,
				`{"callback_recovery":"server-v2","pending_outcomes":[],"pending_local_notifications":[]}`,
			} {
				f := runtimeTestFederate(t, scalar)
				client := &captureSavepointClient{}
				f.conn.savepoint = client
				encoded, err := f.snapshotRuntime()
				if err != nil {
					t.Fatal(err)
				}
				var envelope sdkRuntimeEnvelope
				if err := json.Unmarshal(encoded, &envelope); err != nil {
					t.Fatal(err)
				}
				envelope.State = json.RawMessage(state)
				envelope.Checksum, err = runtimeChecksum(envelope)
				if err != nil {
					t.Fatal(err)
				}
				unsupported, err := json.Marshal(envelope)
				if err != nil {
					t.Fatal(err)
				}
				// A previous valid attempt must not authorize this rejected one.
				if err := f.stageRuntimeRestore(InitiateFederateRestore{}); err != nil {
					t.Fatal(err)
				}
				event, healthy := f.prepareRestoredCallback(context.Background(), InitiateFederateRestore{Label: "unsupported", ClientState: unsupported})
				failure, ok := event.(RestoreStateError)
				if healthy || !ok {
					t.Fatalf("missing rejected staging evidence: %T healthy=%v", event, healthy)
				}
				f.drainDone = make(chan struct{})
				f.drainEvents(&restoreEventStream[rtiv1.FederateEvent]{ctx: context.Background()})
				for attempt := 0; attempt < 2; attempt++ {
					if err := f.FederateRestoreComplete(context.Background()); err == nil || !strings.Contains(err.Error(), failure.Err.Error()) {
						t.Fatalf("rejected staging allowed/lost failure: %v calls=%v", err, client.calls)
					}
				}
				if len(client.calls) != 0 {
					t.Fatalf("rejected staging sent RPCs: %v", client.calls)
				}
				if err := f.FederateRestoreNotComplete(context.Background()); err != nil {
					t.Fatal(err)
				}
				if len(client.calls) != 1 || client.calls[0] != "FederateRestoreNotComplete" {
					t.Fatalf("failure path unavailable: %v", client.calls)
				}
				if err := f.FederateRestoreComplete(context.Background()); err == nil {
					t.Fatal("failure report granted readiness")
				}
			}
		})
	}
}

func TestGoSDKRestoreReadinessLegacyAndLifecycle(t *testing.T) {
	for _, scalar := range []string{HLAfloat64Time, HLAinteger64Time} {
		t.Run(scalar, func(t *testing.T) {
			f := runtimeTestFederate(t, scalar)
			client := &captureSavepointClient{}
			f.conn.savepoint = client
			ctx := context.Background()
			assertBlocked := func() {
				t.Helper()
				before := len(client.calls)
				if err := f.FederateRestoreComplete(ctx); err == nil {
					t.Fatal("unstaged restore accepted success")
				}
				if len(client.calls) != before {
					t.Fatal("unstaged restore sent RPC")
				}
			}
			stage := func() {
				t.Helper()
				if _, healthy := f.prepareRestoredCallback(ctx, InitiateFederateRestore{Label: "legacy"}); !healthy {
					t.Fatal("empty legacy state rejected")
				}
				if f.runtimeStaged != nil {
					t.Fatal("legacy readiness must not require a prepared pointer")
				}
				if err := f.FederateRestoreComplete(ctx); err != nil {
					t.Fatal(err)
				}
			}
			assertBlocked()
			for _, event := range []Event{FederationRestoreBegun{}, RequestFederationRestoreSucceeded{}, RequestFederationRestoreFailed{}, FederationNotRestored{}, FederationRestored{}} {
				stage()
				if _, healthy := f.prepareRestoredCallback(ctx, event); !healthy {
					t.Fatalf("lifecycle failed: %T", event)
				}
				assertBlocked()
			}
			stage()
			if err := f.RequestFederationRestore(ctx, "next"); err != nil {
				t.Fatal(err)
			}
			assertBlocked()
			stage()
			if err := f.AbortFederationRestore(ctx); err != nil {
				t.Fatal(err)
			}
			assertBlocked()
			stage()
			if _, healthy := f.prepareRestoredCallback(ctx, FederationRestored{CallbackInfo: CallbackInfo{recovered: true}}); !healthy {
				t.Fatal("historic terminal failed")
			}
			if err := f.FederateRestoreComplete(ctx); err != nil {
				t.Fatalf("historic terminal cleared current readiness: %v", err)
			}
			zero := uint64(0)
			if _, healthy := f.prepareRestoredCallback(ctx, FederationRestored{FederateHandle: &zero}); healthy {
				t.Fatal("invalid terminal accepted")
			}
			assertBlocked()
			encoded, err := f.snapshotRuntime()
			if err != nil {
				t.Fatal(err)
			}
			if _, healthy := f.prepareRestoredCallback(ctx, InitiateFederateRestore{ClientState: encoded}); !healthy {
				t.Fatal("valid v2 runtime failed staging")
			}
			if err := f.FederateRestoreComplete(ctx); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestGoSDKRestoreReadinessDisconnect(t *testing.T) {
	for _, scalar := range []string{HLAfloat64Time, HLAinteger64Time} {
		for _, batch := range []bool{false, true} {
			t.Run(scalar+map[bool]string{false: "/events", true: "/batches"}[batch], func(t *testing.T) {
				f := runtimeTestFederate(t, scalar)
				client := &captureSavepointClient{}
				f.conn.savepoint = client
				f.drainDone = make(chan struct{})
				if err := f.stageRuntimeRestore(InitiateFederateRestore{}); err != nil {
					t.Fatal(err)
				}
				if batch {
					f.drainEventBatches(&restoreEventStream[rtiv1.FederateEventBatch]{ctx: context.Background()})
				} else {
					f.drainEvents(&restoreEventStream[rtiv1.FederateEvent]{ctx: context.Background()})
				}
				if err := f.FederateRestoreComplete(context.Background()); err == nil || len(client.calls) != 0 {
					t.Fatalf("disconnected restore admitted: %v %v", err, client.calls)
				}
			})
		}
		t.Run(scalar+"/closed-connection", func(t *testing.T) {
			f := runtimeTestFederate(t, scalar)
			client := &captureSavepointClient{}
			f.conn.savepoint = client
			cc, err := grpc.NewClient("passthrough:///unused", grpc.WithTransportCredentials(insecure.NewCredentials()))
			if err != nil {
				t.Fatal(err)
			}
			f.conn.cc = cc
			if err := f.stageRuntimeRestore(InitiateFederateRestore{}); err != nil {
				t.Fatal(err)
			}
			if err := f.conn.Close(); err != nil {
				t.Fatal(err)
			}
			if err := f.FederateRestoreComplete(context.Background()); err == nil || len(client.calls) != 0 {
				t.Fatalf("closed connection admitted: %v %v", err, client.calls)
			}
		})
	}
}
