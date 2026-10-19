package server

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"

	grpcadapter "github.com/cbchoi/gorti/rti/internal/adapters/grpc"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ddm"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
	savepoint "github.com/cbchoi/gorti/rti/internal/hla/save_restore"
	syncpkg "github.com/cbchoi/gorti/rti/internal/hla/synchronization"
	timepkg "github.com/cbchoi/gorti/rti/internal/hla/time"
	stdgrpc "google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"
)

func restartCheckpointServer(t *testing.T, dir string, generation uint32) (*rtid, *stdgrpc.ClientConn, func()) {
	t.Helper()
	srv, err := newRTID(rtidConfig{SaveDir: dir, GenerationEpoch: generation, OutboxBatchSize: 1, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	l := bufconn.Listen(1 << 20)
	done := make(chan error, 1)
	go func() { done <- srv.grpcS.Serve(l) }()
	conn, err := stdgrpc.NewClient("passthrough:///restart", stdgrpc.WithTransportCredentials(insecure.NewCredentials()), stdgrpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return l.DialContext(ctx) }))
	if err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	stop := func() {
		once.Do(func() { _ = conn.Close(); srv.grpcS.Stop(); _ = l.Close(); <-done; _ = srv.plugins.Close() })
	}
	t.Cleanup(stop)
	return srv, conn, stop
}

func TestRestoreRestartFreshGenerationAllManagerState(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	const fed core.FederationName = "restart-state"
	dir := t.TempDir()
	source, conn, stop := restartCheckpointServer(t, dir, 7)
	connections := map[*rtid]*stdgrpc.ClientConn{source: conn}
	callbackContexts := map[*rtid]map[core.FederateHandle]context.Context{}
	create := func(c *stdgrpc.ClientConn, xml string) {
		t.Helper()
		_, err := rtiv1.NewFederationServiceClient(c).CreateFederation(ctx, &rtiv1.CreateFederationRequest{WireVersion: 1, FederationName: string(fed), FomModules: []*rtiv1.FOMModule{{Path: "model.xml", Xml: []byte(xml)}}})
		if err != nil {
			t.Fatal(err)
		}
	}
	create(conn, `<objectModel><objects><objectClass><name>HLAobjectRoot</name><objectClass><name>Vehicle</name><attribute><name>position</name><dimensions><dimension>X</dimension></dimensions></attribute></objectClass></objectClass></objects><dimensions><dimension><name>X</name><upperBound>100</upperBound></dimension></dimensions></objectModel>`)
	join := func(s *rtid, name string) core.FederateHandle {
		t.Helper()
		generation, _ := s.fedMgr.GenerationFor(fed)
		joined := momCompletionReviewJoin(ctx, t, connections[s], string(fed), name, "", generation)
		h := core.FederateHandle(joined.FederateHandle)
		if callbackContexts[s] == nil {
			callbackContexts[s] = map[core.FederateHandle]context.Context{}
		}
		callbackContexts[s][h] = metadata.AppendToOutgoingContext(ctx, "gorti-session-token-bin", string(joined.SessionToken))
		return h
	}
	acceptID := func(s *rtid, h core.FederateHandle, id uint64) {
		t.Helper()
		r, err := s.timeMgr.(*timepkg.Manager).ReserveRetractionID(ctx, fed, h, id)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Release()
		if err = r.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	historical := join(source, "retired")
	acceptID(source, historical, 31)
	if err := source.fedMgr.ResignFederation(ctx, fed, historical, core.ResignActionNoAction); err != nil {
		t.Fatal(err)
	}
	a, b := join(source, "sender"), join(source, "receiver")
	if err := source.fedMgr.SetAutomaticResignDirective(fed, a, core.ResignActionDeleteThenDivest); err != nil {
		t.Fatal(err)
	}
	model, err := source.foms.Get(ctx, fed)
	if err != nil {
		t.Fatal(err)
	}
	cls, _ := model.LookupObjectClass("HLAobjectRoot.Vehicle")
	attr, _ := model.LookupAttribute(cls, "position")
	decl := source.declMgr.(*declaration.Manager)
	if err := decl.PublishObjectClassAttributes(ctx, fed, a, cls, []core.AttributeHandle{attr}); err != nil {
		t.Fatal(err)
	}
	if err := decl.SubscribeObjectClassAttributes(ctx, fed, b, cls, []core.AttributeHandle{attr}); err != nil {
		t.Fatal(err)
	}
	obj, _, err := source.objReg.Register(ctx, fed, a, cls, "")
	if err != nil {
		t.Fatal(err)
	}
	regions := source.ddmMgr.(*ddm.Manager)
	space, _ := regions.LookupRoutingSpace(fed, "default")
	dim, _ := regions.LookupDimension(fed, space, "X")
	pub, err := regions.CreateRegion(ctx, fed, a, space, []core.DDMDimensionHandle{dim})
	if err != nil {
		t.Fatal(err)
	}
	sub, err := regions.CreateRegion(ctx, fed, b, space, []core.DDMDimensionHandle{dim})
	if err != nil {
		t.Fatal(err)
	}
	for _, region := range []struct {
		owner  core.FederateHandle
		handle ddm.RegionHandle
	}{{a, pub}, {b, sub}} {
		if err := regions.SetRangeBounds(fed, region.owner, region.handle, dim, ddm.Range{Lower: 0, Upper: 100}); err != nil {
			t.Fatal(err)
		}
		if err := regions.CommitRegionModifications(ctx, fed, region.owner, []ddm.RegionHandle{region.handle}); err != nil {
			t.Fatal(err)
		}
	}
	if err = regions.AssociateRegionsForUpdates(ctx, fed, a, obj, map[core.AttributeHandle][]ddm.RegionHandle{attr: {pub}}); err != nil {
		t.Fatal(err)
	}
	if err = regions.SubscribeObjectClassAttributesWithRegions(ctx, fed, b, cls, []core.AttributeHandle{attr}, []ddm.RegionHandle{sub}); err != nil {
		t.Fatal(err)
	}
	if err = source.timeMgr.EnableRegulation(ctx, fed, a, 1); err != nil {
		t.Fatal(err)
	}
	if err = source.timeMgr.EnableConstrained(ctx, fed, b); err != nil {
		t.Fatal(err)
	}
	ts := core.LogicalTime(10)
	if err = source.objReg.UpdateAttributesWithMetadata(ctx, fed, a, obj, map[core.AttributeHandle][]byte{attr: []byte("saved-value")}, &ts, core.ObjectMessageOptions{RetractionHandle: 7}); err != nil {
		t.Fatal(err)
	}
	synchronization := source.syncMgr.(*syncpkg.Manager)
	if err = synchronization.Register(ctx, fed, "sync", nil, []core.FederateHandle{a, b}); err != nil {
		t.Fatal(err)
	}
	sourceCallbacks, releaseSourceCallbacks, err := source.outbox.Subscribe(ctx, fed, a)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := releaseSourceCallbacks(); err != nil {
			t.Error(err)
		}
	}()
	invokeQueuedSynchronizationFixture(callbackContexts[source][a], t, conn, fed, a, sourceCallbacks)
	receiverCallbacks, releaseReceiverCallbacks, err := source.outbox.Subscribe(ctx, fed, b)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := releaseReceiverCallbacks(); err != nil {
			t.Error(err)
		}
	}()
	invokeQueuedSynchronizationFixture(callbackContexts[source][b], t, conn, fed, b, receiverCallbacks)
	if err = synchronization.AchieveWith(ctx, fed, a, "sync", false); err != nil {
		t.Fatal(err)
	}
	saves := rtiv1.NewSavepointServiceClient(conn)
	if _, err = saves.RequestFederationSave(ctx, &rtiv1.RequestFederationSaveRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: uint64(a), Label: testSavedLabel}); err != nil {
		t.Fatal(err)
	}
	for _, h := range []core.FederateHandle{a, b} {
		r := &rtiv1.FederateSaveResponseRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: uint64(h)}
		if _, err = saves.FederateSaveBegun(ctx, r); err != nil {
			t.Fatal(err)
		}
		if _, err = saves.FederateSaveComplete(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	if source.saveMgr.(*savepoint.Manager).QuerySaveState(fed, testSavedLabel) != savepoint.StateSaved {
		t.Fatal("save did not publish")
	}
	sourceGeneration, _ := source.fedMgr.GenerationFor(fed)
	stop()
	target, targetConn, _ := restartCheckpointServer(t, dir, 19)
	connections[target] = targetConn
	create(targetConn, `<objectModel/>`)
	// Reverse join order proves this is a name bijection, not ordinal reuse.
	currentB, currentA := join(target, "receiver"), join(target, "sender")
	identityA, err := target.fedMgr.MemberIdentity(fed, currentA)
	if err != nil {
		t.Fatal(err)
	}
	acceptID(target, currentA, 23)
	channels := map[core.FederateHandle]<-chan []core.OutboundEvent{}
	for _, h := range []core.FederateHandle{currentA, currentB} {
		q, release, err := target.outbox.Subscribe(ctx, fed, h)
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := release(); err != nil {
				t.Error(err)
			}
		}()
		channels[h] = q
		drainScheduledEvents(q)
	}
	saves = rtiv1.NewSavepointServiceClient(targetConn)
	req := &rtiv1.RequestFederationRestoreRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: uint64(currentA), Label: testSavedLabel, SavedFederationGeneration: &sourceGeneration}
	if _, err = saves.RequestFederationRestore(ctx, req); err != nil {
		t.Fatalf("restart source restore: %v", err)
	}
	for _, h := range []core.FederateHandle{currentA, currentB} {
		drainScheduledEvents(channels[h])
		if _, err = saves.FederateRestoreComplete(ctx, &rtiv1.FederateRestoreResponseRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: uint64(h)}); err != nil {
			t.Fatalf("restart completion: %v", err)
		}
	}
	installed := map[core.FederateHandle]core.FederateHandle{}
	for _, h := range []core.FederateHandle{currentA, currentB} {
		for _, event := range drainScheduledEvents(channels[h]) {
			if terminal, ok := event.(*savepoint.RestoredEvent); ok {
				installed[h] = terminal.FederateHandle
				if terminal.CallbackEpoch != 1 {
					t.Fatal("missing epoch rotation")
				}
				if h == currentA && terminal.NextRetractionHandle != 24 {
					t.Fatalf("merged ID hint %d", terminal.NextRetractionHandle)
				}
			}
		}
		if installed[h] == 0 || uint64(installed[h])>>32 != 19 || uint32(installed[h]) <= 2 {
			t.Fatalf("identity not fresh target generation: %d", installed[h])
		}
		if target.fedMgr.ValidateMember(fed, h) == nil {
			t.Fatal("old current handle still authorized")
		}
	}
	newA, newB := installed[currentA], installed[currentB]
	identityAfter, err := target.fedMgr.MemberIdentity(fed, newA)
	if err != nil || identityAfter.Incarnation != identityA.Incarnation {
		t.Fatalf("session incarnation changed: %+v %v", identityAfter, err)
	}
	if action, err := target.fedMgr.GetAutomaticResignDirective(fed, newA); err != nil || action != core.ResignActionDeleteThenDivest {
		t.Fatalf("saved auto directive missing: %v %v", action, err)
	}
	model, err = target.foms.Get(ctx, fed)
	if err != nil {
		t.Fatal(err)
	}
	if restored, ok := model.LookupObjectClass("HLAobjectRoot.Vehicle"); !ok || restored != cls {
		t.Fatal("saved FOM handles not installed")
	}
	objects := target.objReg.ExportState(fed)
	found := false
	for _, o := range objects.Objects {
		if o.Handle == obj {
			found = o.Registrant == newA && o.Class == cls
		}
	}
	if !found {
		t.Fatal("object identity not rebound")
	}
	own := target.ownMgr.(*ownership.Manager).ExportState(fed)
	found = false
	for _, o := range own.Owners {
		if o.Object == obj && o.Attribute == attr {
			found = o.Owner == newA
		}
	}
	if !found {
		t.Fatal("ownership not rebound")
	}
	ddmState := target.ddmMgr.(*ddm.Manager).ExportState(fed)
	for _, r := range ddmState.Regions {
		if r.Handle == pub && r.Owner != newA || r.Handle == sub && r.Owner != newB {
			t.Fatal("region ownership not rebound")
		}
	}
	tm := target.timeMgr.(*timepkg.Manager)
	if r, err := tm.ReserveRetractionID(ctx, fed, newA, 7); !errors.Is(err, core.ErrRetractionIDAlreadyUsed) {
		if r != nil {
			r.Release()
		}
		t.Fatalf("saved ID reuse accepted: %v", err)
	}
	history, err := tm.ExportRetractionHistory(fed)
	if err != nil {
		t.Fatal(err)
	}
	found = false
	for _, p := range history.Producers {
		if uint64(p.Producer)>>32 != 19 {
			t.Fatal("source generation leaked into ledger")
		}
		for _, r := range p.Accepted {
			if r.First <= 31 && 31 <= r.Last {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("historical producer ID ledger lost")
	}
	state, err := tm.ExportState(fed, func(e core.OutboundEvent) (core.OutboundEvent, error) { return e, nil })
	if err != nil {
		t.Fatal(err)
	}
	found = false
	for _, f := range state.Federates {
		for _, q := range f.Queue {
			if q.RetractionHandle == 7 {
				event := q.Event.(*object.CallbackEvent)
				found = f.Handle == newB && q.Sender == newA && event.Metadata.Producer == newA && string(event.Attributes[uint64(attr)]) == "saved-value"
			}
		}
	}
	if !found {
		t.Fatal("saved TSO identity/payload missing")
	}
	if err = target.synchronizationLifecycle.Restored(ctx, fed); err != nil {
		t.Fatal(err)
	}
	for _, event := range drainScheduledEvents(channels[currentB]) {
		wire, err := grpcadapter.EncodeOutboundEvent(event)
		if err != nil {
			t.Fatal(err)
		}
		if wire.GetSyncAnnounced() != nil {
			t.Fatal("already invoked synchronization announcement replayed")
		}
	}
	if err = target.syncMgr.(*syncpkg.Manager).Achieve(ctx, fed, newB, "sync"); err != nil {
		t.Fatal(err)
	}
	found = false
	for _, event := range drainScheduledEvents(channels[currentA]) {
		wire, convertErr := grpcadapter.EncodeOutboundEvent(event)
		if convertErr != nil {
			t.Fatal(convertErr)
		}
		if e := wire.GetSyncSynchronized(); e != nil {
			failed := e.GetFailedToSync()
			found = len(failed) == 1 && failed[0] == uint64(newA)
		}
	}
	if !found {
		t.Fatal("saved failed sync participant lost during generation translation")
	}
}
