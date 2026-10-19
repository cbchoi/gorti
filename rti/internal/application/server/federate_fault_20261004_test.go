package server

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/adapters/auth/session"
	grpcadapter "github.com/cbchoi/gorti/rti/internal/adapters/grpc"
	wire "github.com/cbchoi/gorti/rti/internal/adapters/protobuf"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	savepoint "github.com/cbchoi/gorti/rti/internal/hla/save_restore"
	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

const faultReportClass = "HLAmanager.HLAfederate.HLAreport.HLAreportFederateLost"

func TestFederateFault20261004ReportAndVoluntaryDistinction(t *testing.T) {
	for _, mode := range []string{"regulating", "nonregulating", testVoluntaryLabel, testReconnectLabel} {
		t.Run("Float64/"+mode, func(t *testing.T) { faultProductionScenario[core.LogicalTime](t, mode, 17.5, false) })
		t.Run("Integer64/"+mode, func(t *testing.T) { faultProductionScenario[int64](t, mode, 1<<53+7, false) })
	}
}

func TestFederateFault20261004FailsInstructedSave(t *testing.T) {
	t.Run("Float64", func(t *testing.T) { faultProductionScenario[core.LogicalTime](t, "regulating", 17.5, true) })
	t.Run("Integer64", func(t *testing.T) { faultProductionScenario[int64](t, "regulating", 1<<53+7, true) })
}

func faultProductionScenario[T core.LogicalTimeScalar](t *testing.T, mode string, granted T, save bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	directory := t.TempDir()
	r, err := newRTID(rtidConfig{EnableInteger64: true, GenerationEpoch: 221, SaveDir: directory, AutoResignGrace: 80 * time.Millisecond, OutboxBatchSize: 1, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- r.grpcS.Serve(listener) }()
	conn, err := grpcgo.NewClient(listener.Addr().String(), grpcgo.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		r.sessions.Close()
		_ = conn.Close()
		r.grpcS.Stop()
		_ = listener.Close()
		<-done
		for _, fed := range r.fedMgr.Snapshot() {
			_ = r.momMgr.FederationDestroyed(context.Background(), fed.Name)
		}
		_ = r.plugins.Close()
	})
	const fed = "fault"
	federations := rtiv1.NewFederationServiceClient(conn)
	created, err := federations.CreateFederation(ctx, &rtiv1.CreateFederationRequest{WireVersion: 1, FederationName: fed, LogicalTimeImplementationName: core.ScalarRepresentation[T]().String(), FomModules: []*rtiv1.FOMModule{momCompletionReviewModule(t, "Item")}})
	if err != nil {
		t.Fatal(err)
	}
	lost := momCompletionReviewJoin(ctx, t, conn, fed, "lost", "", created.FederationGeneration)
	var survivorChannels []<-chan []core.OutboundEvent
	var survivors []uint64
	for _, name := range []string{"survivor-a", "survivor-b"} {
		member := momCompletionReviewJoin(ctx, t, conn, fed, name, "", created.FederationGeneration)
		survivors = append(survivors, member.FederateHandle)
		ch, release, err := r.outbox.Subscribe(ctx, fed, core.FederateHandle(member.FederateHandle))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = release() })
		survivorChannels = append(survivorChannels, ch)
	}
	model, err := r.foms.Get(ctx, fed)
	if err != nil {
		t.Fatal(err)
	}
	class, ok := model.LookupInteractionClass(faultReportClass)
	if !ok {
		t.Fatal("standard MIM lost report missing")
	}
	for _, survivor := range survivors {
		_, err := rtiv1.NewDeclarationServiceClient(conn).SubscribeInteractionClass(ctx, &rtiv1.SubInterRequest{WireVersion: 1, FederationName: fed, FederateHandle: survivor, InteractionClassHandle: uint64(class)})
		if err != nil {
			t.Fatal(err)
		}
	}
	auth := metadata.AppendToOutgoingContext(ctx, session.MetadataKey, string(lost.SessionToken))
	open := func() (grpcgo.ServerStreamingClient[rtiv1.FederateEventBatch], context.CancelFunc) {
		t.Helper()
		streamCtx, stop := context.WithCancel(auth)
		stream, err := rtiv1.NewStreamServiceClient(conn).EventBatches(streamCtx, &rtiv1.EventsRequest{WireVersion: 1, FederationName: fed, FederateHandle: lost.FederateHandle})
		if err != nil {
			stop()
			t.Fatal(err)
		}
		ready, err := stream.Recv()
		if err != nil || !ready.Ready {
			stop()
			t.Fatalf("stream not authenticated/ready: %v %v", ready, err)
		}
		return stream, stop
	}
	stream, disconnect := open()
	defer disconnect()
	if mode != "nonregulating" {
		interval, _ := core.ScalarIntervalValue(T(1))
		_, lookahead, _ := wire.EncodeLogicalTimeIntervalPair(core.ScalarRepresentation[T](), interval)
		times := rtiv1.NewTimeServiceClient(conn)
		if _, err := times.EnableTimeRegulation(auth, &rtiv1.EnableRegulationRequest{WireVersion: 1, FederationName: fed, FederateHandle: lost.FederateHandle, LookaheadValue: lookahead}); err != nil {
			t.Fatal(err)
		}
		if _, err := times.TimeAdvanceRequest(auth, &rtiv1.TARRequest{WireVersion: 1, FederationName: fed, FederateHandle: lost.FederateHandle, LogicalTimeValue: typedSaveWireTime(t, granted)}); err != nil {
			t.Fatal(err)
		}
		seen := false
		for !seen {
			batch, err := stream.Recv()
			if err != nil {
				t.Fatal(err)
			}
			for _, event := range batch.Events {
				if grant := event.GetGrant(); grant != nil {
					value, err := wire.DecodeLogicalTimePair(core.ScalarRepresentation[T](), grant.LogicalTime, grant.LogicalTimeValue)
					if err != nil {
						t.Fatal(err)
					}
					actual, err := core.ScalarFromTimeValue[T](value)
					if err != nil || actual != granted {
						t.Fatalf("grant=%v, want %v: %v", actual, granted, err)
					}
					seen = true
				}
			}
		}
	}
	for _, ch := range survivorChannels {
		drainScheduledEvents(ch)
	}
	if save {
		_, err := rtiv1.NewSavepointServiceClient(conn).RequestFederationSave(ctx, &rtiv1.RequestFederationSaveRequest{WireVersion: 1, FederationName: fed, FederateHandle: survivors[0], Label: "faulted-save"})
		if err != nil {
			t.Fatal(err)
		}
		batch, err := stream.Recv()
		if err != nil {
			t.Fatal(err)
		}
		if len(batch.Events) != 1 || batch.Events[0].GetSaveInitiate() == nil {
			t.Fatalf("lost member not instructed: %v", batch)
		}
		for _, ch := range survivorChannels {
			initiated := 0
			for _, e := range drainScheduledEvents(ch) {
				encoded, err := grpcadapter.EncodeOutboundEvent(e)
				if err != nil {
					t.Fatal(err)
				}
				if encoded.GetSaveInitiate() != nil {
					initiated++
				}
			}
			if initiated != 1 {
				t.Fatalf("survivor initiation count=%d", initiated)
			}
		}
	}
	if mode == testVoluntaryLabel {
		_, err = federations.ResignFederation(auth, &rtiv1.ResignFederationRequest{WireVersion: 1, FederationName: fed, FederateHandle: lost.FederateHandle, Action: rtiv1.ResignAction_RESIGN_ACTION_NO_ACTION})
		if err != nil {
			t.Fatal(err)
		}
	}
	disconnect()
	if mode == testReconnectLabel {
		// Client cancellation does not acknowledge server-side subscriber release.
		for {
			attached := false
			for _, row := range r.outbox.OutboxStats() {
				if row.Federation == fed && row.Handle == core.FederateHandle(lost.FederateHandle) {
					attached = true
					break
				}
			}
			if !attached {
				break
			}
			if err := ctx.Err(); err != nil {
				t.Fatalf("cancelled stream did not release its subscriber: %v", err)
			}
			runtime.Gosched()
		}
		_, stop := open()
		defer stop()
	}
	if mode == testReconnectLabel || mode == testVoluntaryLabel {
		time.Sleep(180 * time.Millisecond)
	} else {
		deadline := time.Now().Add(time.Second)                                                                            //nolint:forbidigo // Exercise real reconnection deadlines against the server's wall clock.
		for r.fedMgr.ValidateMember(fed, core.FederateHandle(lost.FederateHandle)) == nil && time.Now().Before(deadline) { //nolint:forbidigo // Exercise real reconnection deadlines against the server's wall clock.
			time.Sleep(5 * time.Millisecond)
		}
	}
	joined := r.fedMgr.ValidateMember(fed, core.FederateHandle(lost.FederateHandle)) == nil
	if joined != (mode == testReconnectLabel) {
		t.Fatalf("post-grace membership=%t mode=%s", joined, mode)
	}
	for i, ch := range survivorChannels {
		reports, failures := 0, 0
		for _, e := range drainScheduledEvents(ch) {
			encoded, err := grpcadapter.EncodeOutboundEvent(e)
			if err != nil {
				t.Fatal(err)
			}
			if report := encoded.GetReceive(); report != nil && report.InteractionClassHandle == uint64(class) {
				reports++
				param := func(name string) []byte {
					h, ok := model.LookupParameter(class, name)
					if !ok {
						t.Fatal(name)
					}
					return report.Parameters[uint64(h)]
				}
				if !bytes.Equal(param("HLAfederate"), catalogRPCHandle(lost.FederateHandle)) || catalogRPCString(t, param("HLAfederateName")) != "lost" {
					t.Fatal("wrong lost identity")
				}
				if catalogRPCString(t, param("HLAfaultDescription")) == "" {
					t.Fatal("empty fault description")
				}
				stamp := param("HLAtimeStamp")
				if mode == "nonregulating" {
					if !bytes.Equal(stamp, []byte{0, 0, 0, 0}) {
						t.Fatalf("fabricated nonregulating timestamp: %x", stamp)
					}
				} else {
					exact, _ := core.ScalarTimeValue(granted)
					want, _ := exact.MarshalBinary()
					if len(stamp) != 12 || binary.BigEndian.Uint32(stamp) != 8 || !bytes.Equal(stamp[4:], want) {
						t.Fatalf("last good timestamp=%x, want %x", stamp, want)
					}
				}
			}
			if failure := encoded.GetSaveFailed(); failure != nil {
				failures++
				if failure.Label != "faulted-save" || failure.Reason == nil || *failure.Reason != int32(savepoint.RTIDetectedFailureDuringSave) {
					t.Fatalf("wrong actual fault outcome: %v", failure)
				}
			}
		}
		want := 1
		if mode == testVoluntaryLabel || mode == testReconnectLabel {
			want = 0
		}
		if reports != want {
			t.Fatalf("survivor %d lost reports=%d want %d", i, reports, want)
		}
		wantFailure := 0
		if save {
			wantFailure = 1
		}
		if failures != wantFailure {
			t.Fatalf("survivor %d save failures=%d want %d", i, failures, wantFailure)
		}
	}
	if save {
		var files []string
		err := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !entry.IsDir() && strings.HasSuffix(path, ".bundle") {
				files = append(files, path)
			}
			return nil
		})
		if err != nil || len(files) != 0 {
			t.Fatalf("fault persisted bundle: %v %v", files, err)
		}
	}
	for _, survivor := range survivors {
		if err := r.fedMgr.ValidateMember(fed, core.FederateHandle(survivor)); err != nil {
			t.Fatal("survivor was removed", err)
		}
		if _, err := rtiv1.NewTimeServiceClient(conn).TimeAdvanceRequest(ctx, &rtiv1.TARRequest{WireVersion: 1, FederationName: fed, FederateHandle: survivor, LogicalTimeValue: typedSaveWireTime(t, granted+2)}); err != nil {
			t.Fatal("survivor cannot continue", err)
		}
	}
}

func TestFederateFault20261004AssignmentDoesNotCreateViableReplica(t *testing.T) {
	authority, primaryConn, stopPrimary := integerProductionServer(t, t.TempDir(), 230)
	peer, peerConn, _ := integerProductionServer(t, t.TempDir(), 231)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	const fed = "single-authority"
	generation := momCompletionReviewCreate(ctx, t, primaryConn, fed, "Item")
	member := momCompletionReviewJoin(ctx, t, primaryConn, fed, "member", "", generation)
	cluster := rtiv1.NewClusterServiceClient(peerConn)
	_, err := cluster.NotifyAssignment(ctx, &rtiv1.NotifyAssignmentRequest{WireVersion: 1, FederationName: fed, HostNodeId: "authority", HostAddress: primaryConn.Target(), ExpectedFederationGeneration: &generation})
	if err != nil {
		t.Fatal(err)
	}
	checkPeer := func() {
		t.Helper()
		assignment, err := cluster.LookupFederationHost(ctx, &rtiv1.LookupFederationHostRequest{WireVersion: 1, FederationName: fed})
		if err != nil || assignment.GetStatus() != rtiv1.LookupFederationHostResponse_REDIRECT || assignment.GetHostNodeId() != "authority" {
			t.Fatalf("directory assignment changed authority: %v %v", assignment, err)
		}
		if _, exists := peer.fedMgr.GenerationFor(fed); exists || len(peer.fedMgr.MembersOf(fed)) != 0 {
			t.Fatal("directory advertisement instantiated a replica")
		}
		if _, err := rtiv1.NewTimeServiceClient(peerConn).QueryLogicalTime(ctx, &rtiv1.QueryFederateTimeRequest{WireVersion: 1, FederationName: fed, FederateHandle: member.FederateHandle}); err == nil {
			t.Fatal("peer admitted an authority-owned member")
		}
	}
	checkPeer()
	if err := authority.fedMgr.ValidateMember(fed, core.FederateHandle(member.FederateHandle)); err != nil {
		t.Fatal(err)
	}
	stopPrimary()
	checkPeer()
}
