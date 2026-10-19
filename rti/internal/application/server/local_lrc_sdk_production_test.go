package server

import (
	"context"
	"encoding/binary"
	"fmt"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/adapters/delivery/callbackprofile"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
	"github.com/cbchoi/gorti/rti/pkg/federate"
)

func TestLocalLRCProductionSDKAuthenticatedOrderedDelivery(t *testing.T) {
	for _, implementation := range []string{float64TimeName, integer64TimeName} {
		t.Run(implementation, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			runtime, conn, _ := integerProductionServer(t, t.TempDir(), 253)
			sdk := federate.WrapGRPCClientConn(conn)
			spec := federate.FederationSpec{
				Name: "local-lrc-sdk", LogicalTimeImplementationName: implementation,
				FOMModules: []federate.FOMModule{{Path: "signal.xml", XML: []byte(conveyedProductionFOM)}},
			}
			producer, err := sdk.JoinFederation(ctx, spec, "producer")
			if err != nil {
				t.Fatal(err)
			}
			receiver, err := sdk.JoinFederation(ctx, spec, "receiver")
			if err != nil {
				t.Fatal(err)
			}
			if err := producer.SendInteraction(ctx, "Signal", map[string][]byte{"value": {0, 0, 0, 0}}, nil); err == nil {
				t.Fatal("unpublished interaction was admitted")
			}
			if producer.LocalLRCStats().Enqueued != 0 {
				t.Fatal("invalid interaction reached the delivery queue")
			}
			if err := producer.PublishInteractionClass(ctx, "Signal"); err != nil {
				t.Fatal(err)
			}
			if err := receiver.SubscribeInteractionClass(ctx, "Signal"); err != nil {
				t.Fatal(err)
			}
			if err := receiver.EnableAsynchronousDelivery(ctx); err != nil {
				t.Fatal(err)
			}
			const count = 64
			for i := uint32(0); i < count; i++ {
				payload := binary.BigEndian.AppendUint32(nil, i)
				if err := producer.SendInteraction(ctx, "Signal", map[string][]byte{"value": payload}, nil); err != nil {
					t.Fatalf("send %d: %v", i, err)
				}
			}
			if err := producer.FlushLocalLRC(ctx); err != nil {
				t.Fatal(err)
			}
			assertReceived := func(want uint64) {
				t.Helper()
				state := runtime.momMgr.(*mom.Manager).ExportState(core.FederationName(spec.Name))
				var actual uint64
				for _, participant := range state.Traffic.Federates {
					if uint64(participant.Handle) == receiver.CurrentHandle() {
						for _, traffic := range participant.Counts {
							if traffic.Kind == mom.InteractionsReceived {
								actual += uint64(traffic.Count)
							}
						}
					}
				}
				if actual != want {
					t.Fatalf("MOM received count=%d, want=%d", actual, want)
				}
			}
			assertReceived(0)
			for received := uint32(0); received < count; {
				select {
				case event, open := <-receiver.Events():
					if !open {
						t.Fatal("callback stream closed")
					}
					callbackErr, reportErr := receiver.DispatchEvent(ctx, event, func(event federate.Event) error {
						interaction, ok := event.(federate.ReceiveInteraction)
						if !ok {
							return nil
						}
						payload := interaction.Parameters["value"]
						if len(payload) != 4 || binary.BigEndian.Uint32(payload) != received || interaction.Timestamp != nil {
							return fmt.Errorf("unexpected receive-order interaction %d: %+v", received, interaction)
						}
						received++
						return nil
					})
					if callbackErr != nil || reportErr != nil {
						t.Fatalf("dispatch: %v; report: %v", callbackErr, reportErr)
					}
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			}
			if err := receiver.FlushCallbackInvocationReports(ctx); err != nil {
				t.Fatal(err)
			}
			assertReceived(count)
			reports := receiver.CallbackReportTransportStats()
			if callbackprofile.Async && reports.LocalEntries < count-1 {
				t.Fatalf("production local entry not negotiated: %+v", reports)
			}
			if !callbackprofile.Async && reports.LocalEntries != 0 {
				t.Fatalf("confirmed build used local entry: %+v", reports)
			}
			stats := producer.LocalLRCStats()
			if implementation == float64TimeName && (stats.Enqueued != count || stats.Sent != count || stats.Acked != count || stats.Failures != 0) {
				t.Fatalf("LocalLRC did not commit all operations: %+v", stats)
			}
			// Integer64 currently falls back to its confirmed service route.
			if implementation == integer64TimeName && stats.Enqueued != 0 {
				t.Fatalf("unexpected Integer64 LocalLRC route: %+v", stats)
			}
			if err := producer.Resign(ctx); err != nil {
				t.Fatal(err)
			}
			if err := receiver.Resign(ctx); err != nil {
				t.Fatal(err)
			}
		})
	}
}
