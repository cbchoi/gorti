package ddm_registration_test

import (
	"bytes"
	"context"
	_ "embed"
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/application/server"
	"github.com/cbchoi/gorti/rti/tests/testkit/grpctest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

const serverMarker = "GORTI_DDM_REGISTRATION_TEST_SERVER"

//go:embed testdata/registration.xml
var registrationFOM []byte

// Exercise the production composition in an owned subprocess, as save_restore does.
func TestMain(m *testing.M) {
	if os.Getenv(serverMarker) == "1" {
		flag.CommandLine = flag.NewFlagSet("rtid", flag.ExitOnError)
		os.Args = []string{"rtid", "--listen=" + os.Getenv("GORTI_DDM_REGISTRATION_TEST_LISTEN"),
			"--admin-listen=" + os.Getenv("GORTI_DDM_REGISTRATION_TEST_ADMIN"),
			"--metrics-listen=127.0.0.1:0", "--save-dir=" + os.Getenv("GORTI_DDM_REGISTRATION_TEST_SAVES"),
			"--audit-replay-plugin=none", "--log-dir=", "--log-level=warn"}
		server.Main()
		return
	}
	os.Exit(m.Run())
}

type callbackLog struct {
	mu           sync.Mutex
	events       []*rtiv1.FederateEvent
	copiedBounds map[uint64]*rtiv1.Range
	err          error
	wake         chan struct{}
}

func (l *callbackLog) snapshot() []*rtiv1.FederateEvent {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]*rtiv1.FederateEvent(nil), l.events...)
}

func (l *callbackLog) wait(t *testing.T, description string, match func(*rtiv1.FederateEvent) bool) *rtiv1.FederateEvent {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		l.mu.Lock()
		for _, e := range l.events {
			if match(e) {
				l.mu.Unlock()
				return e
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
			t.Fatalf("callback deadline: %s", description)
		}
	}
}

type federation struct {
	t                                           *testing.T
	ctx                                         context.Context
	name                                        string
	admin                                       rtiv1.AdminServiceClient
	ddm                                         rtiv1.DDMServiceClient
	decl                                        rtiv1.DeclarationServiceClient
	objects                                     rtiv1.ObjectServiceClient
	support                                     rtiv1.SupportServiceClient
	sync                                        rtiv1.SyncServiceClient
	savepoint                                   rtiv1.SavepointServiceClient
	ownership                                   rtiv1.OwnershipServiceClient
	timeService                                 rtiv1.TimeServiceClient
	handles                                     [4]uint64
	queryContexts                               [4]context.Context
	regionOwners                                map[uint64]int
	callbacks                                   [4]*callbackLog
	class, position, velocity, space, dimension uint64
	barrierID                                   int
}

func check(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func startFederation(t *testing.T) *federation {
	t.Helper()
	return startFederationWithSave(t, false)
}

func startFederationWithSave(t *testing.T, saveEnabled bool) *federation {
	t.Helper()
	// Keep both reservations open until both ephemeral addresses are selected.
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	check(t, err)
	defer lis.Close()
	adminLis, err := net.Listen("tcp", "127.0.0.1:0")
	check(t, err)
	defer adminLis.Close()
	addr, adminAddr := lis.Addr().String(), adminLis.Addr().String()
	executable, err := os.Executable()
	check(t, err)
	dir := t.TempDir()
	saveDir := ""
	if saveEnabled {
		saveDir = filepath.Join(dir, "saves")
	}
	output, err := os.Create(filepath.Join(dir, "server.log"))
	check(t, err)
	t.Cleanup(func() { _ = output.Close() })
	cmd := exec.Command(executable)
	cmd.Env = append(os.Environ(), serverMarker+"=1", "GORTI_DDM_REGISTRATION_TEST_LISTEN="+addr,
		"GORTI_DDM_REGISTRATION_TEST_ADMIN="+adminAddr, "GORTI_DDM_REGISTRATION_TEST_SAVES="+saveDir, "GORTI_RESEARCH_CONFIG=")
	cmd.Stdout, cmd.Stderr = output, output
	check(t, lis.Close())
	check(t, adminLis.Close())
	check(t, cmd.Start())
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	t.Cleanup(func() {
		select {
		case err := <-done:
			t.Errorf("production server exited before cleanup: %v", err)
		default:
			_ = cmd.Process.Kill()
			<-done
		}
		_ = output.Close()
		data, err := os.ReadFile(output.Name())
		if err != nil {
			t.Errorf("read server log: %v", err)
		}
		if bytes.Contains(data, []byte("WARNING: DATA RACE")) {
			t.Error("race in production subprocess")
		}
		if t.Failed() {
			t.Logf("production server output:\n%s", data)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	t.Cleanup(cancel)
	dial := func(address string) *grpc.ClientConn {
		t.Helper()
		dialCtx, dialCancel := context.WithTimeout(ctx, 10*time.Second)
		defer dialCancel()
		conn, err := grpctest.DialReady(dialCtx, address)
		check(t, err)
		t.Cleanup(func() { _ = conn.Close() })
		return conn
	}
	conn, adminConn := dial(addr), dial(adminAddr)
	f := &federation{t: t, ctx: ctx, name: "ddm-registration", regionOwners: make(map[uint64]int), admin: rtiv1.NewAdminServiceClient(adminConn),
		ddm: rtiv1.NewDDMServiceClient(conn), decl: rtiv1.NewDeclarationServiceClient(conn),
		objects: rtiv1.NewObjectServiceClient(conn), support: rtiv1.NewSupportServiceClient(conn), sync: rtiv1.NewSyncServiceClient(conn),
		savepoint: rtiv1.NewSavepointServiceClient(conn), ownership: rtiv1.NewOwnershipServiceClient(conn), timeService: rtiv1.NewTimeServiceClient(conn)}
	fm := rtiv1.NewFederationServiceClient(conn)
	created, err := fm.CreateFederation(ctx, &rtiv1.CreateFederationRequest{WireVersion: 1, FederationName: f.name,
		FomModules: []*rtiv1.FOMModule{{Path: "registration.xml", Xml: registrationFOM}}, Seed: 1701})
	check(t, err)
	for i := range f.handles {
		joined, err := fm.JoinFederation(ctx, &rtiv1.JoinFederationRequest{WireVersion: 1, FederationName: f.name,
			FederateName: fmt.Sprintf("participant-%d", i), FederateType: "ddm-registration-test", ExpectedFederationGeneration: &created.FederationGeneration})
		check(t, err)
		f.handles[i] = joined.FederateHandle
		callbackContext := metadata.AppendToOutgoingContext(ctx, "gorti-session-token-bin", string(joined.SessionToken))
		f.queryContexts[i] = callbackContext
		stream, err := rtiv1.NewStreamServiceClient(conn).EventBatches(callbackContext, &rtiv1.EventsRequest{WireVersion: 1,
			FederationName: f.name, FederateHandle: joined.FederateHandle, ExpectedFederationGeneration: &created.FederationGeneration})
		check(t, err)
		ready, err := stream.Recv()
		check(t, err)
		if !ready.Ready || len(ready.Events) != 0 {
			t.Fatalf("callback stream not ready: %v", ready)
		}
		log := &callbackLog{wake: make(chan struct{}, 1)}
		f.callbacks[i] = log
		go func() {
			for {
				batch, err := stream.Recv()
				if err == nil {
					for _, event := range batch.Events {
						mandatory := event.GetSyncAnnounced() != nil || event.GetSyncSynchronized() != nil || event.GetSyncRegistrationSucceeded() != nil || event.GetSyncRegistrationFailed() != nil
						if mandatory && len(event.CallbackReceipt) != 32 {
							err = fmt.Errorf("mandatory synchronization callback lacks receipt")
							break
						}
						report := func(entry bool) error {
							if len(event.CallbackReceipt) == 0 {
								return nil
							}
							_, reportErr := f.support.ReportCallbackInvocation(callbackContext, &rtiv1.ReportCallbackInvocationRequest{WireVersion: 1,
								FederationName: f.name, FederateHandle: joined.FederateHandle, CallbackReceipt: event.CallbackReceipt, Success: true, InvocationEntry: entry})
							return reportErr
						}
						if err = report(true); err != nil {
							break
						}
						var received *rtiv1.ReceivedRegions
						if reflect := event.GetReflect(); reflect != nil {
							received = reflect.GetMetadata().GetReceivedRegions()
						} else if interaction := event.GetReceive(); interaction != nil {
							received = interaction.GetMetadata().GetReceivedRegions()
						}
						bounds := make(map[uint64]*rtiv1.Range)
						for _, region := range received.GetRegionHandles() {
							result, queryErr := f.ddm.QueryBounds(callbackContext, &rtiv1.QueryBoundsRequest{WireVersion: 1,
								FederationName: f.name, RegionHandle: region, DimensionHandle: f.dimension})
							if queryErr != nil || !result.GetFound() || result.GetBounds() == nil {
								err = fmt.Errorf("copied region %d query inside ambassador: %v %v", region, result, queryErr)
								break
							}
							bounds[region] = result.GetBounds()
						}
						if err != nil {
							break
						}
						// This collector is the test ambassador body, not stream delivery.
						log.mu.Lock()
						if log.copiedBounds == nil {
							log.copiedBounds = make(map[uint64]*rtiv1.Range)
						}
						for region, value := range bounds {
							log.copiedBounds[region] = value
						}
						log.events = append(log.events, event)
						log.mu.Unlock()
						if err = report(false); err != nil {
							break
						}
					}
				}
				log.mu.Lock()
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
		}()
	}
	cls, err := f.support.GetObjectClassHandle(ctx, &rtiv1.GetObjectClassHandleRequest{WireVersion: 1, FederationName: f.name, ClassName: "HLAobjectRoot.Vehicle"})
	check(t, err)
	f.class = cls.ClassHandle
	for name, dest := range map[string]*uint64{"Position": &f.position, "Velocity": &f.velocity} {
		attr, err := f.support.GetAttributeHandle(ctx, &rtiv1.GetAttributeHandleRequest{WireVersion: 1, FederationName: f.name, ClassHandle: f.class, AttributeName: name})
		check(t, err)
		*dest = attr.AttributeHandle
	}
	space, err := f.ddm.LookupRoutingSpace(ctx, &rtiv1.LookupRoutingSpaceRequest{WireVersion: 1, FederationName: f.name, Name: "default"})
	check(t, err)
	if !space.Found || space.RoutingSpaceHandle == 0 {
		t.Fatalf("missing routing space: %v", space)
	}
	f.space = space.RoutingSpaceHandle
	dim, err := f.ddm.LookupDimension(ctx, &rtiv1.LookupDimensionRequest{WireVersion: 1, FederationName: f.name, RoutingSpaceHandle: f.space, Name: "X"})
	check(t, err)
	if !dim.Found || dim.DimensionHandle == 0 {
		t.Fatalf("missing declared X dimension: %v", dim)
	}
	f.dimension = dim.DimensionHandle
	_, err = f.decl.PublishObjectClassAttributes(ctx, &rtiv1.PubObjAttrsRequest{WireVersion: 1, FederationName: f.name,
		FederateHandle: f.handles[0], ObjectClassHandle: f.class, AttributeHandles: []uint64{f.position, f.velocity}})
	check(t, err)
	return f
}

// Synchronization callbacks fence earlier outbox events on every subscriber stream.
func (f *federation) barrier() {
	f.t.Helper()
	f.barrierID++
	label := fmt.Sprintf("drain-%d", f.barrierID)
	_, err := f.sync.RegisterFederationSynchronizationPoint(f.ctx, &rtiv1.RegisterSyncPointRequest{WireVersion: 1,
		FederationName: f.name, FederateHandle: f.handles[0], Label: label})
	check(f.t, err)
	for i, h := range f.handles {
		f.callbacks[i].wait(f.t, "synchronization announcement", func(e *rtiv1.FederateEvent) bool { return e.GetSyncAnnounced().GetLabel() == label })
		_, err = f.sync.SynchronizationPointAchieved(f.ctx, &rtiv1.AchieveSyncPointRequest{WireVersion: 1, FederationName: f.name, FederateHandle: h, Label: label})
		check(f.t, err)
	}
	for _, log := range f.callbacks {
		log.wait(f.t, "synchronization completion", func(e *rtiv1.FederateEvent) bool { return e.GetSyncSynchronized().GetLabel() == label })
	}
}
