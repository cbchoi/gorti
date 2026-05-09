package m12spec

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	gosync "sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/cbchoi/gorti/rti/internal/core"
	"github.com/cbchoi/gorti/rti/internal/ddm"
	"github.com/cbchoi/gorti/rti/internal/declaration"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/ownership"
	"github.com/cbchoi/gorti/rti/internal/savepoint"
	syncpkg "github.com/cbchoi/gorti/rti/internal/sync"
	grpcsvc "github.com/cbchoi/gorti/rti/internal/transport/grpc"
)

// =============================================================================
// Local fixtures — independent of the (internal) test fixtures inside
// rti/internal/{sync,ownership,ddm,savepoint}, since spec tests live in
// a separate package and cannot import internal *_test.go helpers.
// =============================================================================

type fakeOutbox struct {
	mu   gosync.Mutex
	sent int
}

func (o *fakeOutbox) Send(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.OutboundEvent) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.sent++
	return nil
}

type permissiveFOMRepo struct{}

func (permissiveFOMRepo) Load(context.Context, []core.FOMModule) (core.FOMHandle, error) {
	return permissiveFOMHandle{}, nil
}
func (permissiveFOMRepo) Get(context.Context, core.FederationName) (core.FOMHandle, error) {
	return permissiveFOMHandle{}, nil
}

type permissiveFOMHandle struct{}

func (permissiveFOMHandle) IsValid() bool { return true }
func (permissiveFOMHandle) LookupObjectClass(string) (core.ObjectClassHandle, bool) {
	return 1, true
}
func (permissiveFOMHandle) LookupInteractionClass(string) (core.InteractionClassHandle, bool) {
	return 1, true
}
func (permissiveFOMHandle) LookupAttribute(core.ObjectClassHandle, string) (core.AttributeHandle, bool) {
	return 1, true
}
func (permissiveFOMHandle) LookupParameter(core.InteractionClassHandle, string) (core.ParameterHandle, bool) {
	return 1, true
}

// stubFedStore is a minimal core.FederationStore for the spec test
// composition root. Only used to satisfy NewServer's required-options
// validation; the spec tests exercise the cut-2 service handlers, not
// the federation handler.
type stubFedStore struct{}

func (stubFedStore) CreateFederation(_ context.Context, _ core.CreateFederationRequest) error {
	return nil
}
func (stubFedStore) DestroyFederation(_ context.Context, _ core.FederationName) error {
	return nil
}
func (stubFedStore) JoinFederation(_ context.Context, _ core.JoinFederationRequest) (core.FederateHandle, error) {
	return 0, nil
}
func (stubFedStore) ResignFederation(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.ResignAction) error {
	return nil
}
func (stubFedStore) List(_ context.Context) ([]core.FederationSummary, error) {
	return nil, nil
}
func (stubFedStore) Snapshot() []core.FederationRoster { return nil }
func (stubFedStore) ListMembers(_ core.FederationName) []core.FederationMember {
	return nil
}

type stubObjRegistry struct{}

func (stubObjRegistry) Register(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.ObjectClassHandle, _ string) (core.ObjectHandle, string, error) {
	return 0, "", errors.New("stub")
}
func (stubObjRegistry) UpdateAttributes(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.ObjectHandle, _ map[core.AttributeHandle][]byte, _ *core.LogicalTime) error {
	return errors.New("stub")
}
func (stubObjRegistry) SendInteraction(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.InteractionClassHandle, _ map[core.ParameterHandle][]byte, _ *core.LogicalTime) error {
	return errors.New("stub")
}
func (stubObjRegistry) Delete(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.ObjectHandle, _ *core.LogicalTime, _ []byte) error {
	return errors.New("stub")
}
func (stubObjRegistry) LocalDelete(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.ObjectHandle) error {
	return errors.New("stub")
}
func (stubObjRegistry) RequestAttributeValueUpdate(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.ObjectHandle, _ []core.AttributeHandle, _ []byte) error {
	return errors.New("stub")
}
func (stubObjRegistry) RequestClassAttributeValueUpdate(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.ObjectClassHandle, _ []core.AttributeHandle, _ []byte) error {
	return errors.New("stub")
}
func (stubObjRegistry) ChangeAttributeTransportType(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.ObjectHandle, _ []core.AttributeHandle, _ core.TransportType) error {
	return errors.New("stub")
}
func (stubObjRegistry) ChangeInteractionTransportType(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.InteractionClassHandle, _ core.TransportType) error {
	return errors.New("stub")
}
func (stubObjRegistry) Snapshot(_ core.FederationName) core.ObjectSnapshot {
	return core.ObjectSnapshot{}
}

// memStore is an in-memory savepoint.Storage backend.
type memStoreKey struct {
	fed   core.FederationName
	label string
}

type memStore struct {
	mu      gosync.Mutex
	bundles map[memStoreKey][]byte
}

func newMemStore() *memStore { return &memStore{bundles: map[memStoreKey][]byte{}} }

func (s *memStore) Writer(fed core.FederationName, label string) (io.WriteCloser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.bundles[memStoreKey{fed, label}]; exists {
		return nil, savepoint.ErrSaveBundleExists
	}
	return &memBundleWriter{store: s, key: memStoreKey{fed, label}}, nil
}
func (s *memStore) Reader(fed core.FederationName, label string) (io.ReadCloser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, ok := s.bundles[memStoreKey{fed, label}]
	if !ok {
		return nil, savepoint.ErrSaveBundleNotFound
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}
func (s *memStore) Exists(fed core.FederationName, label string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.bundles[memStoreKey{fed, label}]
	return ok
}

type memBundleWriter struct {
	store *memStore
	key   memStoreKey
	buf   bytes.Buffer
}

func (w *memBundleWriter) Write(p []byte) (int, error) { return w.buf.Write(p) }
func (w *memBundleWriter) Close() error {
	w.store.mu.Lock()
	defer w.store.mu.Unlock()
	w.store.bundles[w.key] = append([]byte(nil), w.buf.Bytes()...)
	return nil
}

// =============================================================================
// Test harness — compose all four cut-3 services into a real grpc.Server,
// dial a client, return both for the test body to drive RPCs over the wire.
// =============================================================================

type m12Harness struct {
	syncMgr *syncpkg.Manager
	ownMgr  *ownership.Manager
	ddmMgr  *ddm.Manager
	saveMgr *savepoint.Manager

	server *grpc.Server
	conn   *grpc.ClientConn
	addr   string

	syncClient      rtiv1.SyncServiceClient
	ownershipClient rtiv1.OwnershipServiceClient
	ddmClient       rtiv1.DDMServiceClient
	savepointClient rtiv1.SavepointServiceClient

	cleanup func()
}

// newM12Harness composes a real grpc.Server with all cut-3 services
// wired and a client connection ready to drive RPCs. The returned
// cleanup function MUST be deferred by the caller.
func newM12Harness(t *testing.T) *m12Harness {
	t.Helper()
	outbox := &fakeOutbox{}

	syncMgr, err := syncpkg.New(syncpkg.Options{Outbox: outbox})
	if err != nil {
		t.Fatalf("syncpkg.New: %v", err)
	}
	ownMgr, err := ownership.New(ownership.Options{Outbox: outbox})
	if err != nil {
		t.Fatalf("ownership.New: %v", err)
	}
	ddmMgr, err := ddm.New(ddm.Options{Outbox: outbox, FOMs: permissiveFOMRepo{}})
	if err != nil {
		t.Fatalf("ddm.New: %v", err)
	}
	saveMgr, err := savepoint.New(savepoint.Options{
		Outbox:      outbox,
		BundleStore: newMemStore(),
	})
	if err != nil {
		t.Fatalf("savepoint.New: %v", err)
	}

	srv, err := grpcsvc.NewServer(grpcsvc.Options{
		Federations:  stubFedStore{},
		Declarations: declaration.New(),
		Objects:      stubObjRegistry{},
		Outbox:       outbox,
		Sync:         syncMgr,
		Ownership:    ownMgr,
		DDM:          ddmMgr,
		Savepoint:    saveMgr,
	})
	if err != nil {
		t.Fatalf("grpcsvc.NewServer: %v", err)
	}

	gs := grpc.NewServer()
	if err := srv.Register(gs); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	go func() { _ = gs.Serve(ln) }()

	conn, err := grpc.NewClient(ln.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		gs.GracefulStop()
		_ = ln.Close()
		t.Fatalf("grpc.NewClient: %v", err)
	}

	h := &m12Harness{
		syncMgr:         syncMgr,
		ownMgr:          ownMgr,
		ddmMgr:          ddmMgr,
		saveMgr:         saveMgr,
		server:          gs,
		conn:            conn,
		addr:            ln.Addr().String(),
		syncClient:      rtiv1.NewSyncServiceClient(conn),
		ownershipClient: rtiv1.NewOwnershipServiceClient(conn),
		ddmClient:       rtiv1.NewDDMServiceClient(conn),
		savepointClient: rtiv1.NewSavepointServiceClient(conn),
	}
	h.cleanup = func() {
		_ = conn.Close()
		gs.GracefulStop()
		_ = ln.Close()
	}
	return h
}

func wireV1() rtiv1.WireVersion { return rtiv1.WireVersion_WIRE_VERSION_V1 }

// =============================================================================
// Spec tests
// =============================================================================

// TestSpec_M12_SyncService_GRPCRoundTrip: SyncService.RegisterFederationSynchronizationPoint
// + SynchronizationPointAchieved invokable via real gRPC.
//
// Implements: M12 — sync gRPC exposure.
func TestSpec_M12_SyncService_GRPCRoundTrip(t *testing.T) {
	h := newM12Harness(t)
	defer h.cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	const fedName = "alpha"
	const label = "phase1"

	// Register sync point with explicit federate set (1, 2).
	if _, err := h.syncClient.RegisterFederationSynchronizationPoint(ctx,
		&rtiv1.RegisterSyncPointRequest{
			WireVersion:       wireV1(),
			FederationName:    fedName,
			FederateHandle:    1,
			Label:             label,
			Tag:               []byte("test-tag"),
			RequiredFederates: []uint64{1, 2},
		}); err != nil {
		t.Fatalf("RegisterFederationSynchronizationPoint: %v", err)
	}

	// Underlying manager state should reflect the announce.
	if got, want := h.syncMgr.QueryState(fedName, label), syncpkg.StateAnnounced; got != want {
		t.Errorf("post-register state=%d, want StateAnnounced(%d)", got, want)
	}

	// Both federates achieve.
	for _, fh := range []uint64{1, 2} {
		if _, err := h.syncClient.SynchronizationPointAchieved(ctx,
			&rtiv1.AchieveSyncPointRequest{
				WireVersion:    wireV1(),
				FederationName: fedName,
				FederateHandle: fh,
				Label:          label,
			}); err != nil {
			t.Fatalf("SynchronizationPointAchieved fh=%d: %v", fh, err)
		}
	}

	// Manager state now StateAchieved.
	if got, want := h.syncMgr.QueryState(fedName, label), syncpkg.StateAchieved; got != want {
		t.Errorf("post-achieve state=%d, want StateAchieved(%d)", got, want)
	}
}

// TestSpec_M12_OwnershipService_GRPCRoundTrip: NegotiatedDivest +
// Acquire flow over real gRPC.
//
// Implements: M12 — ownership gRPC exposure.
func TestSpec_M12_OwnershipService_GRPCRoundTrip(t *testing.T) {
	h := newM12Harness(t)
	defer h.cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	const fedName = "beta"
	const objHandle = uint64(7)
	const attrHandle = uint64(11)
	const ownerFed = uint64(1)
	const acquirerFed = uint64(2)

	// Seed initial ownership in-process — RegisterInitialOwnership has
	// no proto surface (it's a runtime composition hook), so the spec
	// test calls the manager directly. The two-phase transfer still
	// goes over the wire via NegotiatedDivest + Acquire below.
	h.ownMgr.RegisterInitialOwnership(fedName, core.FederateHandle(ownerFed),
		core.ObjectHandle(objHandle), []core.AttributeHandle{core.AttributeHandle(attrHandle)})

	// Confirm seed via gRPC QueryAttributeOwnership.
	qresp, err := h.ownershipClient.QueryAttributeOwnership(ctx,
		&rtiv1.QueryOwnershipRequest{
			WireVersion:     wireV1(),
			FederationName:  fedName,
			ObjectHandle:    objHandle,
			AttributeHandle: attrHandle,
		})
	if err != nil {
		t.Fatalf("QueryAttributeOwnership: %v", err)
	}
	if !qresp.GetOwned() || qresp.GetOwnerFederateHandle() != ownerFed {
		t.Errorf("post-seed owner=%d owned=%v, want owner=%d owned=true",
			qresp.GetOwnerFederateHandle(), qresp.GetOwned(), ownerFed)
	}

	// Owner divests (negotiated) — over the wire.
	if _, err := h.ownershipClient.NegotiatedAttributeOwnershipDivestiture(ctx,
		&rtiv1.NegotiatedDivestRequest{
			WireVersion:      wireV1(),
			FederationName:   fedName,
			FederateHandle:   ownerFed,
			ObjectHandle:     objHandle,
			AttributeHandles: []uint64{attrHandle},
			Tag:              []byte("divest-tag"),
		}); err != nil {
		t.Fatalf("NegotiatedAttributeOwnershipDivestiture: %v", err)
	}

	// Acquirer acquires — completes the transfer.
	if _, err := h.ownershipClient.AttributeOwnershipAcquisition(ctx,
		&rtiv1.AcquireRequest{
			WireVersion:      wireV1(),
			FederationName:   fedName,
			FederateHandle:   acquirerFed,
			ObjectHandle:     objHandle,
			AttributeHandles: []uint64{attrHandle},
			Tag:              []byte("acquire-tag"),
		}); err != nil {
		t.Fatalf("AttributeOwnershipAcquisition: %v", err)
	}

	// QueryAttributeOwnership now reports the new owner.
	q2, err := h.ownershipClient.QueryAttributeOwnership(ctx,
		&rtiv1.QueryOwnershipRequest{
			WireVersion:     wireV1(),
			FederationName:  fedName,
			ObjectHandle:    objHandle,
			AttributeHandle: attrHandle,
		})
	if err != nil {
		t.Fatalf("QueryAttributeOwnership (post-acquire): %v", err)
	}
	if !q2.GetOwned() || q2.GetOwnerFederateHandle() != acquirerFed {
		t.Errorf("post-acquire owner=%d owned=%v, want owner=%d owned=true",
			q2.GetOwnerFederateHandle(), q2.GetOwned(), acquirerFed)
	}

	// IsAttributeOwnedByFederate cross-check.
	io1, err := h.ownershipClient.IsAttributeOwnedByFederate(ctx,
		&rtiv1.IsOwnedRequest{
			WireVersion:     wireV1(),
			FederationName:  fedName,
			FederateHandle:  acquirerFed,
			ObjectHandle:    objHandle,
			AttributeHandle: attrHandle,
		})
	if err != nil {
		t.Fatalf("IsAttributeOwnedByFederate: %v", err)
	}
	if !io1.GetOwned() {
		t.Errorf("IsAttributeOwnedByFederate(acquirer)=false, want true")
	}
}

// TestSpec_M12_DDMService_GRPCRoundTrip: CreateRegion + SetRangeBounds
// + CommitRegionModifications + QueryBounds over real gRPC.
//
// Implements: M12 — DDM gRPC exposure.
func TestSpec_M12_DDMService_GRPCRoundTrip(t *testing.T) {
	h := newM12Harness(t)
	defer h.cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	const fedName = "gamma"
	const ownerFed = uint64(1)

	// Lookup routing space + dimension via gRPC (permissive FOM:
	// any name resolves to handle 1).
	rsResp, err := h.ddmClient.LookupRoutingSpace(ctx,
		&rtiv1.LookupRoutingSpaceRequest{
			WireVersion:    wireV1(),
			FederationName: fedName,
			Name:           ddm.DefaultRoutingSpace,
		})
	if err != nil {
		t.Fatalf("LookupRoutingSpace: %v", err)
	}
	if !rsResp.GetFound() || rsResp.GetRoutingSpaceHandle() == 0 {
		t.Fatalf("LookupRoutingSpace: found=%v handle=%d, want found=true handle>0",
			rsResp.GetFound(), rsResp.GetRoutingSpaceHandle())
	}
	rsHandle := rsResp.GetRoutingSpaceHandle()

	dimResp, err := h.ddmClient.LookupDimension(ctx,
		&rtiv1.LookupDimensionRequest{
			WireVersion:        wireV1(),
			FederationName:     fedName,
			RoutingSpaceHandle: rsHandle,
			Name:               "x",
		})
	if err != nil {
		t.Fatalf("LookupDimension: %v", err)
	}
	if !dimResp.GetFound() || dimResp.GetDimensionHandle() == 0 {
		t.Fatalf("LookupDimension: found=%v handle=%d", dimResp.GetFound(), dimResp.GetDimensionHandle())
	}
	dimHandle := dimResp.GetDimensionHandle()

	// CreateRegion.
	crResp, err := h.ddmClient.CreateRegion(ctx,
		&rtiv1.CreateRegionRequest{
			WireVersion:        wireV1(),
			FederationName:     fedName,
			FederateHandle:     ownerFed,
			RoutingSpaceHandle: rsHandle,
			DimensionHandles:   []uint64{dimHandle},
		})
	if err != nil {
		t.Fatalf("CreateRegion: %v", err)
	}
	if crResp.GetRegionHandle() == 0 {
		t.Fatalf("CreateRegion: region=0, want >0")
	}
	regionHandle := crResp.GetRegionHandle()

	// SetRangeBounds — pending until Commit.
	const lower, upper uint64 = 100, 500
	if _, err := h.ddmClient.SetRangeBounds(ctx,
		&rtiv1.SetRangeBoundsRequest{
			WireVersion:     wireV1(),
			FederationName:  fedName,
			FederateHandle:  ownerFed,
			RegionHandle:    regionHandle,
			DimensionHandle: dimHandle,
			Bounds:          &rtiv1.Range{Lower: lower, Upper: upper},
		}); err != nil {
		t.Fatalf("SetRangeBounds: %v", err)
	}

	// CommitRegionModifications.
	if _, err := h.ddmClient.CommitRegionModifications(ctx,
		&rtiv1.CommitRegionRequest{
			WireVersion:    wireV1(),
			FederationName: fedName,
			FederateHandle: ownerFed,
			RegionHandles:  []uint64{regionHandle},
		}); err != nil {
		t.Fatalf("CommitRegionModifications: %v", err)
	}

	// QueryBounds — should reflect the committed [lower, upper) range.
	qbResp, err := h.ddmClient.QueryBounds(ctx,
		&rtiv1.QueryBoundsRequest{
			WireVersion:     wireV1(),
			FederationName:  fedName,
			RegionHandle:    regionHandle,
			DimensionHandle: dimHandle,
		})
	if err != nil {
		t.Fatalf("QueryBounds: %v", err)
	}
	if !qbResp.GetFound() {
		t.Fatalf("QueryBounds: found=false, want true")
	}
	if got := qbResp.GetBounds(); got == nil || got.GetLower() != lower || got.GetUpper() != upper {
		t.Errorf("QueryBounds bounds=%+v, want [%d, %d)", got, lower, upper)
	}
}

// TestSpec_M12_SavepointService_GRPCRoundTrip: RequestFederationSave
// → FederateSaveComplete aggregation over real gRPC.
//
// Implements: M12 — savepoint gRPC exposure.
func TestSpec_M12_SavepointService_GRPCRoundTrip(t *testing.T) {
	h := newM12Harness(t)
	defer h.cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	const fedName = "delta"
	const label = "snap-1"
	const fh = uint64(1)

	// RequestFederationSave (no Members resolver wired → dynamic mode).
	if _, err := h.savepointClient.RequestFederationSave(ctx,
		&rtiv1.RequestFederationSaveRequest{
			WireVersion:    wireV1(),
			FederationName: fedName,
			FederateHandle: fh,
			Label:          label,
		}); err != nil {
		t.Fatalf("RequestFederationSave: %v", err)
	}

	// QuerySaveState should report INITIATED.
	qsResp, err := h.savepointClient.QuerySaveState(ctx,
		&rtiv1.QuerySaveStateRequest{
			WireVersion:    wireV1(),
			FederationName: fedName,
			Label:          label,
		})
	if err != nil {
		t.Fatalf("QuerySaveState (post-request): %v", err)
	}
	if got, want := qsResp.GetState(), rtiv1.SaveState_SAVE_STATE_INITIATED; got != want {
		t.Errorf("post-request state=%v, want %v", got, want)
	}

	// FederateSaveComplete — in dynamic mode, this single response
	// closes out the save (the federate joins required on first call,
	// allRequiredResponded is true with one complete entry).
	if _, err := h.savepointClient.FederateSaveComplete(ctx,
		&rtiv1.FederateSaveResponseRequest{
			WireVersion:    wireV1(),
			FederationName: fedName,
			FederateHandle: fh,
		}); err != nil {
		t.Fatalf("FederateSaveComplete: %v", err)
	}

	// QuerySaveState now reports SAVED.
	qs2, err := h.savepointClient.QuerySaveState(ctx,
		&rtiv1.QuerySaveStateRequest{
			WireVersion:    wireV1(),
			FederationName: fedName,
			Label:          label,
		})
	if err != nil {
		t.Fatalf("QuerySaveState (post-complete): %v", err)
	}
	if got, want := qs2.GetState(), rtiv1.SaveState_SAVE_STATE_SAVED; got != want {
		t.Errorf("post-complete state=%v, want %v", got, want)
	}
}

// TestSpec_M12_AllServicesRegistered: rtid's grpc.Server registers all
// 8 services (the 4 cut-1 + the 4 new cut-3 ones for sync/ownership/
// DDM/savepoint). Asserted by inspecting server.GetServiceInfo().
//
// Implements: M12 — proto + gRPC handler completeness.
func TestSpec_M12_AllServicesRegistered(t *testing.T) {
	h := newM12Harness(t)
	defer h.cleanup()

	info := h.server.GetServiceInfo()
	wanted := []string{
		// cut-1
		"rti.v1.FederationService",
		"rti.v1.DeclarationService",
		"rti.v1.ObjectService",
		"rti.v1.StreamService",
		// cut-3
		"rti.v1.SyncService",
		"rti.v1.OwnershipService",
		"rti.v1.DDMService",
		"rti.v1.SavepointService",
	}
	for _, name := range wanted {
		if _, ok := info[name]; !ok {
			t.Errorf("service %q not registered; got=%v", name, keysOf(info))
		}
	}
}

func keysOf[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
