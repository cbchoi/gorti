package grpc

import (
	"context"
	"math"
	"strconv"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/adapters/delivery"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/adapters/savebundle/storage"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	savepoint "github.com/cbchoi/gorti/rti/internal/hla/save_restore"
	timepkg "github.com/cbchoi/gorti/rti/internal/hla/time"
	grpcgo "google.golang.org/grpc"
)

func TestFederationRoutingRealTimeSaveCallbacks(t *testing.T) {
	for _, value := range []int64{0, 1<<53 + 1, math.MaxInt64} {
		t.Run(strconv.FormatInt(value, 10), func(t *testing.T) {
			outbox := delivery.New(64)
			outbox.Bind("int", 1)
			clock := core.NewManualClock(time.Unix(0, 0))
			integers, err := timepkg.NewOf[int64](timepkg.OptionsOf[int64]{Clock: clock, Outbox: outbox})
			if err != nil {
				t.Fatal(err)
			}
			floats, err := timepkg.New(timepkg.Options{Clock: clock, Outbox: outbox})
			if err != nil {
				t.Fatal(err)
			}
			store, err := storage.NewFSStorageOf[int64](t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			saves, err := savepoint.NewOf[int64](savepoint.OptionsOf[int64]{Outbox: outbox, BundleStore: store, Members: func(core.FederationName) []core.FederateHandle { return []core.FederateHandle{1} }, ValidateSaveRequest: integers.ValidateScheduledSaveRequest, PrepareScheduledTime: integers.PrepareScheduledSave, ResumeTime: integers.ResumeScheduledSave})
			if err != nil {
				t.Fatal(err)
			}
			opts, _, _, _ := typedAssemblyFixture(core.LogicalTime(0))
			intOpts, _, _, _ := typedAssemblyFixture[int64](0)
			opts.Time, opts.Outbox, opts.Savepoint = floats, outbox, nil
			opts.Integer64 = &TypedBackends[int64]{Objects: intOpts.Objects, Time: integers, Savepoint: saves}
			opts.TimeImplementations = routingFactories{"float": float64TimeFactory, "int": integer64TimeFactory}
			server, err := NewServer(opts)
			if err != nil {
				t.Fatal(err)
			}
			conn := saveDDMExactRPCConn(t, func(gs *grpcgo.Server) {
				if err := server.Register(gs); err != nil {
					t.Fatal(err)
				}
			})
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			events, err := rtiv1.NewStreamServiceClient(conn).EventBatches(ctx, &rtiv1.EventsRequest{WireVersion: 1, FederationName: "int", FederateHandle: 1})
			if err != nil {
				t.Fatal(err)
			}
			if ready, err := events.Recv(); err != nil || !ready.GetReady() {
				t.Fatalf("callback stream readiness: %v %v", ready, err)
			}
			receive := func() (*rtiv1.FederateEvent, error) {
				batch, err := events.Recv()
				if err != nil {
					return nil, err
				}
				if len(batch.Events) != 1 {
					t.Fatalf("expected one ordered callback, got %v", batch)
				}
				return batch.Events[0], nil
			}
			times := rtiv1.NewTimeServiceClient(conn)
			_, err = times.EnableTimeRegulation(ctx, &rtiv1.EnableRegulationRequest{WireVersion: 1, FederationName: "int", FederateHandle: 1, LookaheadValue: integerTimeSelector(0)})
			if err != nil {
				t.Fatal(err)
			}
			initial, err := receive()
			if err != nil {
				t.Fatal(err)
			}
			if initial.GetTimeRegulationEnabled() == nil || initial.GetTimeRegulationEnabled().LogicalTimeValue == nil || initial.GetTimeRegulationEnabled().LogicalTimeValue.GetInteger64Value() != 0 {
				t.Fatalf("integer initial callback: %v", initial)
			}
			// A constrained participant cannot request a save at its current
			// time. The zero case instead uses a legal regulating-only save.
			if value > 0 {
				_, err = times.EnableTimeConstrained(ctx, &rtiv1.EnableConstrainedRequest{WireVersion: 1, FederationName: "int", FederateHandle: 1})
				if err != nil {
					t.Fatal(err)
				}
				initial, err = receive()
				if err != nil || initial.GetTimeConstrainedEnabled() == nil || initial.GetTimeConstrainedEnabled().GetLogicalTimeValue().GetInteger64Value() != 0 {
					t.Fatalf("integer constrained callback: %v %v", initial, err)
				}
			}
			saveClient := rtiv1.NewSavepointServiceClient(conn)
			_, err = saveClient.RequestFederationSave(ctx, &rtiv1.RequestFederationSaveRequest{WireVersion: 1, FederationName: "int", FederateHandle: 1, Label: "exact", SaveTimeValue: integerTimeSelector(value)})
			if err != nil {
				t.Fatal(err)
			}
			if value > 0 {
				_, err = times.TimeAdvanceRequest(ctx, &rtiv1.TARRequest{WireVersion: 1, FederationName: "int", FederateHandle: 1, LogicalTimeValue: integerTimeSelector(value)})
				if err != nil {
					t.Fatal(err)
				}
			}
			// The application drives checkpoint progress after the unary leases
			// end; exercise that real coordinator boundary without a polling clock.
			if err := saves.ProgressScheduledSave(ctx, "int"); err != nil {
				t.Fatal(err)
			}
			initiate, err := receive()
			if err != nil {
				t.Fatal(err)
			}
			if initiate.GetSaveInitiate() == nil || initiate.GetSaveInitiate().SaveTimeValue == nil || initiate.GetSaveInitiate().SaveTimeValue.GetInteger64Value() != value || initiate.GetSaveInitiate().SaveTime != nil {
				t.Fatalf("exact save callback: %v", initiate)
			}
			response := &rtiv1.FederateSaveResponseRequest{WireVersion: 1, FederationName: "int", FederateHandle: 1}
			if _, err := saveClient.FederateSaveBegun(ctx, response); err != nil {
				t.Fatal(err)
			}
			if _, err := saveClient.FederateSaveComplete(ctx, response); err != nil {
				t.Fatal(err)
			}
			completed, err := receive()
			if err != nil || completed.GetSaveCompleted() == nil {
				t.Fatalf("save completion callback: %v %v", completed, err)
			}
			if err := saves.ProgressScheduledSave(ctx, "int"); err != nil {
				t.Fatal(err)
			}
			if value == 0 {
				_, err = times.TimeAdvanceRequest(ctx, &rtiv1.TARRequest{WireVersion: 1, FederationName: "int", FederateHandle: 1, LogicalTimeValue: integerTimeSelector(value)})
				if err != nil {
					t.Fatal(err)
				}
			}
			granted, err := receive()
			if err != nil {
				t.Fatal(err)
			}
			if granted.GetGrant() == nil || granted.GetGrant().LogicalTimeValue == nil || granted.GetGrant().LogicalTimeValue.GetInteger64Value() != value || granted.GetGrant().LogicalTime != 0 {
				t.Fatalf("exact grant callback: %v", granted)
			}
			queried, err := times.QueryLogicalTime(ctx, &rtiv1.QueryFederateTimeRequest{WireVersion: 1, FederationName: "int", FederateHandle: 1})
			if err != nil || queried.GetLogicalTimeValue().GetInteger64Value() != value {
				t.Fatalf("exact post-grant query: %v %v", queried, err)
			}
			reader, err := store.Reader("int", "exact")
			if err != nil {
				t.Fatal(err)
			}
			defer reader.Close()
			manifest, _, err := savepoint.ReadBundleOf[int64](reader)
			if err != nil || manifest.SaveTime == nil || *manifest.SaveTime != value {
				t.Fatalf("exact persisted save: %v %v", manifest.SaveTime, err)
			}
			if snapshot := floats.Snapshot("int"); len(snapshot.Federates) != 0 {
				t.Fatal("integer services changed Float64 state")
			}
		})
	}
}
