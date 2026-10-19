package save_restore_test

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/application/server"
	"github.com/cbchoi/gorti/rti/tests/testkit/grpctest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

const serverMarker = "GORTI_SAVE_RESTORE_TEST_SERVER"

// The child uses the production entry point, not a reconstructed service graph.
func TestMain(m *testing.M) {
	if os.Getenv(serverMarker) == "1" {
		flag.CommandLine = flag.NewFlagSet("rtid", flag.ExitOnError)
		os.Args = []string{"rtid", "--listen=" + os.Getenv("GORTI_SAVE_RESTORE_TEST_LISTEN"),
			"--save-dir=" + os.Getenv("GORTI_SAVE_RESTORE_TEST_SAVES"),
			"--metrics-listen=127.0.0.1:0", "--admin-listen=" + os.Getenv("GORTI_SAVE_RESTORE_TEST_ADMIN"), "--log-level=warn"}
		server.Main()
		return
	}
	os.Exit(m.Run())
}

type callbackLog struct {
	mu     sync.Mutex
	events []*rtiv1.FederateEvent
	err    error
	wake   chan struct{}
}

func (l *callbackLog) mark() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.events)
}

func (l *callbackLog) wait(t *testing.T, from int, description string, match func(*rtiv1.FederateEvent) bool) (*rtiv1.FederateEvent, int) {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		l.mu.Lock()
		for i := from; i < len(l.events); i++ {
			if match(l.events[i]) {
				e := l.events[i]
				l.mu.Unlock()
				return e, i
			}
		}
		err := l.err
		l.mu.Unlock()
		if err != nil {
			t.Fatalf("callback stream ended while awaiting %s: %v", description, err)
		}
		select {
		case <-l.wake:
		case <-timer.C:
			t.Fatalf("callback timeout: %s", description)
		}
	}
}

type federation struct {
	t            *testing.T
	ctx          context.Context
	name         string
	conn         *grpc.ClientConn
	admin        rtiv1.AdminServiceClient
	decl         rtiv1.DeclarationServiceClient
	objects      rtiv1.ObjectServiceClient
	support      rtiv1.SupportServiceClient
	time         rtiv1.TimeServiceClient
	savepoint    rtiv1.SavepointServiceClient
	sync         rtiv1.SyncServiceClient
	handles      [2]uint64
	callbacks    [2]*callbackLog
	class, attr  uint64
	inter, param uint64
	barrierID    int
}

func check(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func startFederation(t *testing.T) *federation {
	t.Helper()
	return startFederationWithOptions(t, processOptions{})
}

type processOptions struct {
	saveDisabled bool
	adminEnabled bool
}

func startFederationWithOptions(t *testing.T, opts processOptions) *federation {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	check(t, err)
	addr := lis.Addr().String()
	adminAddr := ""
	if opts.adminEnabled {
		adminLis, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			_ = lis.Close()
			t.Fatal(err)
		}
		adminAddr = adminLis.Addr().String()
		check(t, adminLis.Close())
	}
	check(t, lis.Close())
	executable, err := os.Executable()
	check(t, err)
	dir := t.TempDir()
	saveDir := filepath.Join(dir, "saves")
	if opts.saveDisabled {
		saveDir = ""
	}
	output, err := os.Create(filepath.Join(dir, "server.log"))
	check(t, err)
	cmd := exec.Command(executable)
	cmd.Env = append(os.Environ(), serverMarker+"=1", "GORTI_SAVE_RESTORE_TEST_LISTEN="+addr,
		"GORTI_SAVE_RESTORE_TEST_SAVES="+saveDir, "GORTI_SAVE_RESTORE_TEST_ADMIN="+adminAddr, "GORTI_RESEARCH_CONFIG=")
	cmd.Stdout, cmd.Stderr = output, output
	check(t, cmd.Start())
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	t.Cleanup(func() {
		select {
		case err := <-done:
			t.Errorf("production server exited before test cleanup: %v", err)
		default:
			_ = cmd.Process.Kill()
			<-done
		}
		_ = output.Close()
		data, readErr := os.ReadFile(output.Name())
		if readErr != nil {
			t.Errorf("read production server output: %v", readErr)
		}
		if bytes.Contains(data, []byte("WARNING: DATA RACE")) {
			t.Error("race detector reported a race in the production subprocess")
		}
		if t.Failed() {
			t.Logf("production server output:\n%s", data)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	t.Cleanup(cancel)
	dialCtx, dialCancel := context.WithTimeout(ctx, 10*time.Second)
	defer dialCancel()
	conn, err := grpctest.DialReady(dialCtx, addr)
	check(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	f := &federation{t: t, ctx: ctx, name: "checkpoint", conn: conn, decl: rtiv1.NewDeclarationServiceClient(conn),
		objects: rtiv1.NewObjectServiceClient(conn), support: rtiv1.NewSupportServiceClient(conn),
		time: rtiv1.NewTimeServiceClient(conn), savepoint: rtiv1.NewSavepointServiceClient(conn), sync: rtiv1.NewSyncServiceClient(conn)}
	if opts.adminEnabled {
		adminConn, err := grpctest.DialReady(dialCtx, adminAddr)
		check(t, err)
		t.Cleanup(func() { _ = adminConn.Close() })
		f.admin = rtiv1.NewAdminServiceClient(adminConn)
	}
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate repository FOM")
	}
	fomPath := filepath.Join(filepath.Dir(file), "../../../..", "examples/ieee1516e-typed-smoke/federation.fom.xml")
	xml, err := os.ReadFile(fomPath)
	check(t, err)
	fm := rtiv1.NewFederationServiceClient(conn)
	created, err := fm.CreateFederation(ctx, &rtiv1.CreateFederationRequest{WireVersion: 1, FederationName: f.name,
		FomModules: []*rtiv1.FOMModule{{Path: fomPath, Xml: xml}}, Seed: 1701})
	check(t, err)
	for i := range f.handles {
		joined, err := fm.JoinFederation(ctx, &rtiv1.JoinFederationRequest{WireVersion: 1, FederationName: f.name,
			FederateName: fmt.Sprintf("participant-%d", i), FederateType: "checkpoint-test", ExpectedFederationGeneration: &created.FederationGeneration})
		check(t, err)
		f.handles[i] = joined.FederateHandle
		callbackContext := metadata.AppendToOutgoingContext(ctx, "gorti-session-token-bin", string(joined.SessionToken))
		stream, err := rtiv1.NewStreamServiceClient(conn).EventBatches(callbackContext, &rtiv1.EventsRequest{WireVersion: 1,
			FederationName: f.name, FederateHandle: joined.FederateHandle, ExpectedFederationGeneration: &created.FederationGeneration})
		check(t, err)
		ready, err := stream.Recv()
		check(t, err)
		if !ready.Ready || len(ready.Events) != 0 {
			t.Fatalf("callback subscription did not acknowledge readiness: %v", ready)
		}
		log := &callbackLog{wake: make(chan struct{}, 1)}
		f.callbacks[i] = log
		go func(handle uint64) {
			for {
				batch, err := stream.Recv()
				if err == nil {
					for _, event := range batch.Events {
						// The raw test client invokes protocol callbacks before exposing
						// them to waiters, including the checkpoint terminal callbacks.
						switch event.Event.(type) {
						case *rtiv1.FederateEvent_SyncAnnounced, *rtiv1.FederateEvent_SyncSynchronized,
							*rtiv1.FederateEvent_SyncRegistrationSucceeded, *rtiv1.FederateEvent_SyncRegistrationFailed,
							*rtiv1.FederateEvent_SaveInitiate, *rtiv1.FederateEvent_SaveCompleted, *rtiv1.FederateEvent_SaveFailed,
							*rtiv1.FederateEvent_RestoreRequestSucceeded, *rtiv1.FederateEvent_RestoreRequestFailed,
							*rtiv1.FederateEvent_RestoreBegun, *rtiv1.FederateEvent_RestoreInitiate,
							*rtiv1.FederateEvent_RestoreCompleted, *rtiv1.FederateEvent_RestoreFailed:
						default:
							continue
						}
						if len(event.GetCallbackReceipt()) == 0 {
							err = fmt.Errorf("protocol callback %T has no invocation receipt", event.Event)
							break
						}
						_, err = f.support.ReportCallbackInvocation(callbackContext, &rtiv1.ReportCallbackInvocationRequest{WireVersion: 1, FederationName: f.name, FederateHandle: handle, CallbackReceipt: event.CallbackReceipt, InvocationEntry: true, Success: true})
						if err != nil {
							break
						}
						_, err = f.support.ReportCallbackInvocation(callbackContext, &rtiv1.ReportCallbackInvocationRequest{WireVersion: 1, FederationName: f.name, FederateHandle: handle, CallbackReceipt: event.CallbackReceipt, Success: true})
						if err != nil {
							break
						}
					}
				}
				log.mu.Lock()
				if err == nil {
					log.events = append(log.events, batch.Events...)
				}
				log.err = err
				log.mu.Unlock()
				select {
				case log.wake <- struct{}{}:
				default:
				}
				if err != nil {
					return
				}
			}
		}(joined.FederateHandle)
	}
	cls, err := f.support.GetObjectClassHandle(ctx, &rtiv1.GetObjectClassHandleRequest{WireVersion: 1, FederationName: f.name, ClassName: "HLAobjectRoot.Vehicle"})
	check(t, err)
	f.class = cls.ClassHandle
	attr, err := f.support.GetAttributeHandle(ctx, &rtiv1.GetAttributeHandleRequest{WireVersion: 1, FederationName: f.name, ClassHandle: f.class, AttributeName: "Position"})
	check(t, err)
	f.attr = attr.AttributeHandle
	inter, err := f.support.GetInteractionClassHandle(ctx, &rtiv1.GetInteractionClassHandleRequest{WireVersion: 1, FederationName: f.name, ClassName: "HLAinteractionRoot.Honk"})
	check(t, err)
	f.inter = inter.ClassHandle
	param, err := f.support.GetParameterHandle(ctx, &rtiv1.GetParameterHandleRequest{WireVersion: 1, FederationName: f.name, ClassHandle: f.inter, ParameterName: "Volume"})
	check(t, err)
	f.param = param.ParameterHandle
	return f
}

// A synchronization callback fences earlier outbox events; negative routing
// assertions therefore do not depend on an arbitrary sleep interval.
func (f *federation) barrier() {
	f.t.Helper()
	f.barrierID++
	label := fmt.Sprintf("drain-%d", f.barrierID)
	_, err := f.sync.RegisterFederationSynchronizationPoint(f.ctx, &rtiv1.RegisterSyncPointRequest{WireVersion: 1,
		FederationName: f.name, FederateHandle: f.handles[0], Label: label})
	check(f.t, err)
	for i, h := range f.handles {
		f.callbacks[i].wait(f.t, 0, "synchronization announcement", func(e *rtiv1.FederateEvent) bool { return e.GetSyncAnnounced().GetLabel() == label })
		_, err = f.sync.SynchronizationPointAchieved(f.ctx, &rtiv1.AchieveSyncPointRequest{WireVersion: 1, FederationName: f.name, FederateHandle: h, Label: label})
		check(f.t, err)
	}
	for _, log := range f.callbacks {
		log.wait(f.t, 0, "synchronization completion", func(e *rtiv1.FederateEvent) bool { return e.GetSyncSynchronized().GetLabel() == label })
	}
}
