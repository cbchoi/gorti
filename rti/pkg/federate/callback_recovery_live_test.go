//go:build windows && runtime_checkpoint_live

package federate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type callbackOutcomeTestStream struct {
	grpc.ClientStream
	intercept     func(*rtiv1.ReportCallbackInvocationRequest) (error, error)
	mu            sync.Mutex
	receiveErrors map[uint64]error
}

func (s *callbackOutcomeTestStream) SendMsg(message any) error {
	if batch, ok := message.(*rtiv1.CallbackReportBatch); ok {
		var batchError error
		for _, report := range batch.Reports {
			sendErr, receiveErr := s.intercept(report)
			if sendErr != nil {
				return sendErr
			}
			if receiveErr != nil {
				batchError = receiveErr
			}
		}
		s.mu.Lock()
		if s.receiveErrors == nil {
			s.receiveErrors = make(map[uint64]error)
		}
		s.receiveErrors[batch.FirstSequence+uint64(len(batch.Reports))-1] = batchError
		s.mu.Unlock()
	}
	return s.ClientStream.SendMsg(message)
}

func (s *callbackOutcomeTestStream) RecvMsg(message any) error {
	if err := s.ClientStream.RecvMsg(message); err != nil {
		return err
	}
	if ack, ok := message.(*rtiv1.CallbackReportAck); ok {
		s.mu.Lock()
		err := s.receiveErrors[ack.AcknowledgedSequence]
		delete(s.receiveErrors, ack.AcknowledgedSequence)
		s.mu.Unlock()
		return err
	}
	return nil
}

func callbackOutcomeStreamInterceptor(intercept func(*rtiv1.ReportCallbackInvocationRequest) (error, error)) grpc.StreamClientInterceptor {
	return func(ctx context.Context, descriptor *grpc.StreamDesc, cc *grpc.ClientConn, method string, stream grpc.Streamer, options ...grpc.CallOption) (grpc.ClientStream, error) {
		connection, err := stream(ctx, descriptor, cc, method, options...)
		if err != nil || method != rtiv1.CallbackReportService_Exchange_FullMethodName {
			return connection, err
		}
		return &callbackOutcomeTestStream{ClientStream: connection, intercept: intercept}, nil
	}
}

// Both transports simulate unavailable outcome reporting, not application
// execution or server checkpointing. All accepted calls use actual TCP RPCs.
func TestGoSDKControlOutcomesActualServer(t *testing.T) {
	binary := os.Getenv("GORTI_GO_CONTROL_OUTCOME_LIVE_SERVER")
	if binary == "" {
		t.Fatal("GORTI_GO_CONTROL_OUTCOME_LIVE_SERVER must identify the assessed executable")
	}
	image, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(image)
	hash := hex.EncodeToString(digest[:])
	if expected := os.Getenv("GORTI_GO_CONTROL_OUTCOME_LIVE_SHA256"); expected != "" && !strings.EqualFold(expected, hash) {
		t.Fatal("control-outcomes binary hash mismatch")
	}
	t.Logf("actual control-outcomes server: %s SHA256=%s", binary, hash)
	for _, scalar := range []string{HLAfloat64Time, HLAinteger64Time} {
		t.Run(scalar, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			directory := t.TempDir()
			if root := os.Getenv("GORTI_GO_CONTROL_OUTCOME_LIVE_DIR"); root != "" {
				if err := os.MkdirAll(root, 0700); err != nil {
					t.Fatal(err)
				}
				directory, err = os.MkdirTemp(root, "go-control-"+scalar+"-")
				if err != nil {
					t.Fatal(err)
				}
			}
			module, err := os.ReadFile(filepath.Join("..", "fom", "exporter", "testdata", "retained.xml"))
			if err != nil {
				t.Fatal(err)
			}
			spec := FederationSpec{Name: "go-control-outcomes-" + scalar, FOMModules: []FOMModule{{Path: "control.xml", XML: module}}, LogicalTimeImplementationName: scalar}
			var sourceMu sync.Mutex
			blocked := map[string]bool{}
			ackLost := map[string]bool{}
			sourceInterceptor := func(ctx context.Context, method string, request, reply any, cc *grpc.ClientConn, invoke grpc.UnaryInvoker, options ...grpc.CallOption) error {
				if report, ok := request.(*rtiv1.ReportCallbackInvocationRequest); ok && !report.InvocationEntry {
					sourceMu.Lock()
					drop, loseAck := blocked[string(report.CallbackReceipt)], ackLost[string(report.CallbackReceipt)]
					sourceMu.Unlock()
					if drop {
						return status.Error(codes.Unavailable, "test transport: known outcome not sent")
					}
					if loseAck {
						if err := invoke(ctx, method, request, reply, cc, options...); err != nil {
							return err
						}
						return status.Error(codes.Unavailable, "test transport: accepted outcome acknowledgement lost")
					}
				}
				return invoke(ctx, method, request, reply, cc, options...)
			}
			address, stopSource := startSDKRuntimeServer(t, binary, directory)
			sourceStreamInterceptor := callbackOutcomeStreamInterceptor(func(report *rtiv1.ReportCallbackInvocationRequest) (error, error) {
				if report.InvocationEntry {
					return nil, nil
				}
				sourceMu.Lock()
				drop, loseAck := blocked[string(report.CallbackReceipt)], ackLost[string(report.CallbackReceipt)]
				sourceMu.Unlock()
				if drop {
					return status.Error(codes.Unavailable, "test transport: known outcome not sent"), nil
				}
				if loseAck {
					return nil, status.Error(codes.Unavailable, "test transport: accepted outcome acknowledgement lost")
				}
				return nil, nil
			})
			connection, err := ConnectWithOptions(ctx, address, ConnectOptions{ReceiveOrderTransport: ReceiveOrderTransportConfirmed, ExtraDialOptions: []grpc.DialOption{grpc.WithUnaryInterceptor(sourceInterceptor), grpc.WithStreamInterceptor(sourceStreamInterceptor)}})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = connection.Close() })
			source, err := connection.JoinFederation(ctx, spec, "control-owner")
			if err != nil {
				t.Fatal(err)
			}
			if !source.CallbackInvocationEntrySupported() {
				t.Fatal("actual server did not negotiate callback Entry")
			}
			old := map[string]Event{}
			identities := map[string]string{}
			originalReceipts := map[string][]byte{}
			for _, name := range []string{"pending-control", "success-control", "failure-control", "acklost-control"} {
				if err := source.ReserveObjectInstanceName(ctx, name); err != nil {
					t.Fatal(err)
				}
				event := waitSDKRuntimeEvent(t, ctx, source, func(event Event) bool {
					e, ok := event.(ObjectInstanceNameReservationSucceeded)
					return ok && e.ObjectName == name
				})
				info, _ := runtimeInfo(event)
				if len(info.receipt) != 32 || !validCallbackIdentity(info.identity) {
					t.Fatal("actual control missing source receipt/identity")
				}
				old[name], identities[name], originalReceipts[name] = event, info.identity, info.CallbackReceipt()
				if name == "pending-control" {
					continue
				}
				sourceMu.Lock()
				if name == "acklost-control" {
					ackLost[string(info.receipt)] = true
				} else {
					blocked[string(info.receipt)] = true
				}
				sourceMu.Unlock()
				appErr, reportErr := source.DispatchEvent(ctx, event, func(Event) error {
					if name == "failure-control" {
						return errors.New("live known callback failure")
					}
					return nil
				})
				if reportErr == nil || (name == "failure-control") != (appErr != nil) {
					t.Fatalf("source %s outcome %v/%v", name, appErr, reportErr)
				}
				if name == "acklost-control" {
					// Other retained failures can stop the general flush before this
					// row. Explicitly send this finished result to lose its real ack.
					if err := source.ReportCallbackInvocation(ctx, event, true, ""); err == nil {
						t.Fatal("ack-loss transport injection did not fire")
					}
				}
			}
			state := runtimeTestState(t, source)
			if state.Recovery != runtimeRecoveryServerV2 || len(state.PendingControls) != 1 || len(state.PendingOutcomes) != 3 {
				t.Fatalf("source pending/outcome shape: %+v", state)
			}
			if err := source.RequestFederationSave(ctx, "controls-and-outcomes", nil); err != nil {
				t.Fatal(err)
			}
			initiated := waitSDKRuntimeEvent(t, ctx, source, func(event Event) bool { _, ok := event.(InitiateFederateSave); return ok })
			if a, _ := source.DispatchEvent(ctx, initiated, func(Event) error { return nil }); a != nil {
				t.Fatalf("save-init body %v", a)
			}
			if err := source.ReportCallbackInvocation(ctx, initiated, true, ""); err != nil {
				t.Fatalf("save-init outcome %v", err)
			}
			if err := source.FederateSaveBegun(ctx); err != nil {
				t.Fatal(err)
			}
			if err := source.FederateSaveComplete(ctx); err != nil {
				t.Fatal(err)
			}
			waitSDKRuntimeEvent(t, ctx, source, func(event Event) bool { _, ok := event.(FederationSaved); return ok })
			generation, sourceHandle := source.federationGeneration, source.CurrentHandle()
			_ = connection.Close()
			stopSource()
			address, stopTarget := startSDKRuntimeServer(t, binary, directory)
			defer stopTarget()
			var targetMu sync.Mutex
			var reports []*rtiv1.ReportCallbackInvocationRequest
			recordTargetReport := func(report *rtiv1.ReportCallbackInvocationRequest) {
				targetMu.Lock()
				reports = append(reports, &rtiv1.ReportCallbackInvocationRequest{CallbackReceipt: append([]byte(nil), report.CallbackReceipt...), InvocationEntry: report.InvocationEntry, Success: report.Success, Exception: report.Exception})
				targetMu.Unlock()
			}
			targetInterceptor := func(ctx context.Context, method string, request, reply any, cc *grpc.ClientConn, invoke grpc.UnaryInvoker, options ...grpc.CallOption) error {
				if report, ok := request.(*rtiv1.ReportCallbackInvocationRequest); ok {
					recordTargetReport(report)
				}
				return invoke(ctx, method, request, reply, cc, options...)
			}
			targetStreamInterceptor := callbackOutcomeStreamInterceptor(func(report *rtiv1.ReportCallbackInvocationRequest) (error, error) {
				recordTargetReport(report)
				return nil, nil
			})
			target, err := ConnectWithOptions(ctx, address, ConnectOptions{ReceiveOrderTransport: ReceiveOrderTransportConfirmed, ExtraDialOptions: []grpc.DialOption{grpc.WithUnaryInterceptor(targetInterceptor), grpc.WithStreamInterceptor(targetStreamInterceptor)}})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = target.Close() })
			restored, err := target.JoinFederation(ctx, spec, "control-owner")
			if err != nil {
				t.Fatal(err)
			}
			if restored.federationGeneration == generation {
				t.Fatal("target did not recreate execution generation")
			}
			if err := restored.FederateRestoreComplete(ctx); err == nil {
				t.Fatal("actual TCP target accepted success before runtime staging")
			}
			if err := restored.RequestFederationRestore(ctx, "controls-and-outcomes"); err != nil {
				t.Fatal(err)
			}
			waitSDKRuntimeEvent(t, ctx, restored, func(event Event) bool { _, ok := event.(InitiateFederateRestore); return ok })
			restored.checkpointMu.Lock()
			ready := restored.runtimeRestoreReady && restored.runtimeStaged != nil && restored.runtimeStaged.state.Recovery == runtimeRecoveryServerV2
			restored.checkpointMu.Unlock()
			if !ready {
				t.Fatal("target did not stage v2 runtime before terminal")
			}
			if err := restored.FederateRestoreComplete(ctx); err != nil {
				t.Fatal(err)
			}
			waitSDKRuntimeEvent(t, ctx, restored, func(event Event) bool {
				_, ok := event.(FederationRestored)
				info, _ := runtimeInfo(event)
				return ok && !info.recovered
			})
			seen := map[string][]byte{}
			bodies := 0
			for len(seen) < 3 {
				event := waitSDKRuntimeEvent(t, ctx, restored, func(event Event) bool {
					info, _ := runtimeInfo(event)
					return info.identity == identities["pending-control"] || info.identity == identities["success-control"] || info.identity == identities["failure-control"]
				})
				info, _ := runtimeInfo(event)
				name := ""
				for candidate, identity := range identities {
					if identity == info.identity {
						name = candidate
					}
				}
				if _, duplicate := seen[name]; duplicate {
					t.Fatal("server emitted duplicate recovered carrier")
				}
				if bytes.Equal(info.receipt, originalReceipts[name]) || len(info.receipt) != 32 || info.outcomeOnly != (name != "pending-control") {
					t.Fatal("fresh capability or recovery phase mismatch")
				}
				seen[name] = info.CallbackReceipt()
				for retry := 0; retry < 2; retry++ {
					a, r := restored.DispatchEvent(ctx, event, func(event Event) error {
						if name != "pending-control" {
							t.Error("outcome-only replayed application body")
						}
						body, ok := event.(ObjectInstanceNameReservationSucceeded)
						if !ok || body.ObjectName != "pending-control" {
							t.Error("pending control body changed")
						}
						bodies++
						return nil
					})
					if a != nil || r != nil {
						t.Fatalf("target %s dispatch %v/%v", name, a, r)
					}
				}
			}
			if bodies != 1 {
				t.Fatalf("pending body invoked %d times", bodies)
			}
			for _, event := range old {
				if _, err := restored.DispatchEvent(ctx, event, func(Event) error { t.Error("old carrier invoked"); return nil }); err == nil {
					t.Fatal("old carrier accepted in recreated generation")
				}
			}
			deadline := time.Now().Add(2 * time.Second)
			for {
				state = runtimeTestState(t, restored)
				remaining := false
				for _, outcome := range state.PendingOutcomes {
					if outcome.Identity == identities["acklost-control"] {
						remaining = true
					}
				}
				if !remaining {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("ack-loss completed marker did not reconcile SDK result")
				}
				time.Sleep(10 * time.Millisecond)
			}
			targetMu.Lock()
			for name, receipt := range seen {
				entries, outcomes := 0, 0
				for _, report := range reports {
					if !bytes.Equal(report.CallbackReceipt, receipt) {
						continue
					}
					if report.InvocationEntry {
						entries++
					} else {
						outcomes++
						if report.Success != (name != "failure-control") || (name == "failure-control" && report.Exception != "live known callback failure") {
							t.Error("target changed saved result")
						}
					}
				}
				wantEntries := 0
				if name == "pending-control" {
					wantEntries = 1
				}
				if entries != wantEntries || outcomes != 1 {
					t.Errorf("%s fresh receipt Entry=%d Outcome=%d", name, entries, outcomes)
				}
			}
			targetMu.Unlock()
			t.Logf("PASS scalar=%s source_generation=%d target_generation=%d source_handle=%d target_handle=%d pending_control_bodies=1 outcome_success=1 outcome_failure=1 ackloss_marker=1 artifacts=%s", scalar, generation, restored.federationGeneration, sourceHandle, restored.CurrentHandle(), directory)
		})
	}
}
