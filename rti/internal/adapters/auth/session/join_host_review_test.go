package session_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/adapters/auth/session"
	grpcadapter "github.com/cbchoi/gorti/rti/internal/adapters/grpc"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	savebundlestorage "github.com/cbchoi/gorti/rti/internal/adapters/savebundle/storage"
	"github.com/cbchoi/gorti/rti/internal/application/composition"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
	"github.com/cbchoi/gorti/rti/internal/hla/federation"
	savepoint "github.com/cbchoi/gorti/rti/internal/hla/save_restore"
	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// Only federation RPCs run in these adapter/session contract tests.
type reviewUnusedObjects struct{ core.ObjectRegistry }
type reviewOutbox struct{}

func (reviewOutbox) Send(context.Context, core.FederationName, core.FederateHandle, core.OutboundEvent) error {
	return nil
}

type reviewRegistrar struct{ federation rtiv1.FederationServiceServer }

func (r *reviewRegistrar) RegisterService(desc *grpcgo.ServiceDesc, impl any) {
	if desc.ServiceName == "rti.v1.FederationService" {
		r.federation = impl.(rtiv1.FederationServiceServer)
	}
}

func reviewFederationService(t *testing.T, prepare func(context.Context, core.FederationName) error, published func(context.Context, core.FederationName, []core.FOMModule)) (rtiv1.FederationServiceServer, *federation.Manager, *session.Manager) {
	t.Helper()
	fm, err := federation.New(federation.Options{Clock: core.NewRealClock(), FOMs: composition.NewFOMRepository(), GenerationEpoch: 37})
	if err != nil {
		t.Fatal(err)
	}
	sm, err := session.New(session.Options{Membership: fm})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sm.Close)
	srv, err := grpcadapter.NewServer(grpcadapter.Options{Federations: fm, Membership: fm, Sessions: sm, Declarations: declaration.New(), Objects: reviewUnusedObjects{}, Outbox: reviewOutbox{}, PrepareFederation: prepare, OnCreateFederationSuccess: published})
	if err != nil {
		t.Fatal(err)
	}
	var registrar reviewRegistrar
	if err := srv.Register(&registrar); err != nil {
		t.Fatal(err)
	}
	if registrar.federation == nil {
		t.Fatal("federation handler missing")
	}
	return registrar.federation, fm, sm
}

func TestHostReviewInvalidMetadataPrecedesRosterSessionAndGenerationChecks(t *testing.T) {
	svc, fm, sm := reviewFederationService(t, nil, nil)
	created, err := svc.CreateFederation(context.Background(), &rtiv1.CreateFederationRequest{WireVersion: 1, FederationName: "f"})
	if err != nil {
		t.Fatal(err)
	}
	generation := created.FederationGeneration
	for _, values := range [][]string{{""}, {"one", "two"}, {string([]byte{0xff})}, {"bad\nvalue"}, {"bad\u0085value"}, {"bad\u00a0value"}, {strings.Repeat("a", 1025)}} {
		ctx := metadata.NewIncomingContext(context.Background(), metadata.MD{"gorti-federate-host-bin": values})
		if _, err := svc.JoinFederation(ctx, &rtiv1.JoinFederationRequest{WireVersion: 1, FederationName: "f", FederateName: "same-name"}); status.Code(err) != codes.InvalidArgument {
			t.Fatalf("host %q: %v", values, err)
		}
		if len(fm.MembersOf("f")) != 0 {
			t.Fatal("invalid metadata changed roster")
		}
	}
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("gorti-federate-host-bin", strings.Repeat("x", 1024)))
	joined, err := svc.JoinFederation(ctx, &rtiv1.JoinFederationRequest{WireVersion: 1, FederationName: "f", FederateName: "same-name", ExpectedFederationGeneration: &generation})
	if err != nil {
		t.Fatal(err)
	}
	if uint32(joined.FederateHandle) != 1 {
		t.Fatal("rejected metadata consumed handle or session")
	}
	release, err := fm.AcquireMember("f", core.FederateHandle(joined.FederateHandle))
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	host, known, err := sm.Host("f", core.FederateHandle(joined.FederateHandle))
	if err != nil || !known || host != strings.Repeat("x", 1024) {
		t.Fatalf("boundary host lost: %q %v %v", host, known, err)
	}
}

func TestHostReviewCheckedPrepareFailureDoesNotPublish(t *testing.T) {
	failure := errors.New("history store unavailable")
	fail := true
	published := 0
	svc, fm, _ := reviewFederationService(t, func(context.Context, core.FederationName) error {
		if fail {
			return failure
		}
		return nil
	}, func(context.Context, core.FederationName, []core.FOMModule) { published++ })
	req := &rtiv1.CreateFederationRequest{WireVersion: 1, FederationName: "f"}
	if _, err := svc.CreateFederation(context.Background(), req); err == nil {
		t.Fatal("prepare failure hidden")
	}
	if published != 0 {
		t.Fatal("failed prepare published FOM/MOM/cluster hook")
	}
	if _, ok := fm.GenerationFor("f"); ok {
		t.Fatal("failed execution remains")
	}
	fail = false
	resp, err := svc.CreateFederation(context.Background(), req)
	if err != nil || resp.FederationGeneration != 38 || published != 1 {
		t.Fatalf("retry: %+v %v publications=%d", resp, err, published)
	}
}

func TestHostReviewCancellationAfterPrepareRollsBackBeforePublication(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	published := false
	svc, fm, _ := reviewFederationService(t, func(context.Context, core.FederationName) error { cancel(); return nil }, func(context.Context, core.FederationName, []core.FOMModule) { published = true })
	_, err := svc.CreateFederation(ctx, &rtiv1.CreateFederationRequest{WireVersion: 1, FederationName: "f"})
	if err == nil || published {
		t.Fatalf("cancelled preparation admitted publication: err=%v published=%v", err, published)
	}
	if _, exists := fm.GenerationFor("f"); exists {
		t.Fatal("cancelled creation retained partially initialized execution")
	}
}

func TestHostReviewCommittedPublicationRetainsValuesButNotCancellation(t *testing.T) {
	type requestKey struct{}
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), requestKey{}, "request-identity"))
	defer cancel()
	published := false
	svc, _, _ := reviewFederationService(t, nil, func(publication context.Context, _ core.FederationName, _ []core.FOMModule) {
		cancel()
		if publication.Err() != nil || publication.Done() != nil {
			t.Error("committed publication interrupted by client cancellation")
		}
		if publication.Value(requestKey{}) != "request-identity" {
			t.Error("publication lost request identity")
		}
		published = true
	})
	if _, err := svc.CreateFederation(ctx, &rtiv1.CreateFederationRequest{WireVersion: 1, FederationName: "f"}); err != nil || !published {
		t.Fatalf("committed publication: %v published=%v", err, published)
	}
}

func TestHostReviewRealRestoreKeepsRejoinedSessionHost(t *testing.T) {
	svc, fm, sm := reviewFederationService(t, nil, nil)
	ctx := context.Background()
	created, err := svc.CreateFederation(ctx, &rtiv1.CreateFederationRequest{WireVersion: 1, FederationName: "f"})
	if err != nil {
		t.Fatal(err)
	}
	generation := created.FederationGeneration
	join := func(host string) *rtiv1.JoinFederationResponse {
		t.Helper()
		withHost := metadata.NewIncomingContext(ctx, metadata.Pairs("gorti-federate-host-bin", host))
		result, err := svc.JoinFederation(withHost, &rtiv1.JoinFederationRequest{WireVersion: 1, FederationName: "f", FederateName: "worker", ExpectedFederationGeneration: &generation})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	old := join("saved-host.example")
	oldID, err := fm.MemberIdentity("f", core.FederateHandle(old.FederateHandle))
	if err != nil {
		t.Fatal(err)
	}
	if err := fm.ResignFederation(ctx, "f", oldID.Handle, core.ResignActionNoAction); err != nil {
		t.Fatal(err)
	}
	next := join("live-host.example")
	reservation, err := fm.PrepareRestoreIdentity("f", generation, []federation.RestoreIdentity{{Name: "worker", CurrentHandle: core.FederateHandle(next.FederateHandle), SavedHandle: oldID.Handle}})
	if err != nil {
		t.Fatal(err)
	}
	defer reservation.Release()
	if err := reservation.Commit(); err != nil {
		t.Fatal(err)
	}
	sm.ClearIdentity(oldID)
	release, err := fm.AcquireMember("f", oldID.Handle)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if host, known, err := sm.Host("f", oldID.Handle); err != nil || !known || host != "live-host.example" {
		t.Fatalf("restore resurrected old host: %q %v %v", host, known, err)
	}
	oldCtx := metadata.NewIncomingContext(ctx, metadata.Pairs(session.MetadataKey, string(old.SessionToken)))
	if err := sm.Authenticate(oldCtx, "f", oldID.Handle); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("saved session capability resurrected: %v", err)
	}
	nextCtx := metadata.NewIncomingContext(ctx, metadata.Pairs(session.MetadataKey, string(next.SessionToken)))
	if err := sm.Authenticate(nextCtx, "f", oldID.Handle); err != nil {
		t.Fatalf("live capability lost: %v", err)
	}
}

type reviewReservation struct{}

func (reviewReservation) Commit() error { return nil }
func (reviewReservation) Release()      {}

type reviewReservableOutbox struct{ reviewOutbox }

func (reviewReservableOutbox) Reserve(context.Context, core.FederationName, []core.OutboxDelivery) (core.OutboxReservation, error) {
	return reviewReservation{}, nil
}

func TestHostReviewActualHistoryStoreFailureRollsBackCreate(t *testing.T) {
	dir := t.TempDir()
	// Storage construction can succeed before a per-execution history path
	// becomes unwritable. Exercise the later failure through the prepare hook.
	store, err := savebundlestorage.NewFSStorage(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "history"), []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	var save *savepoint.Manager
	published := 0
	svc, fm, _ := reviewFederationService(t, func(_ context.Context, fed core.FederationName) error { return save.InitializeSaveHistory(fed) }, func(context.Context, core.FederationName, []core.FOMModule) { published++ })
	save, err = savepoint.New(savepoint.Options{Outbox: reviewReservableOutbox{}, BundleStore: store, HistoryStore: store, Generation: fm.GenerationFor})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	req := &rtiv1.CreateFederationRequest{WireVersion: 1, FederationName: "f"}
	if _, err := svc.CreateFederation(ctx, req); err == nil {
		t.Fatal("actual filesystem failure hidden")
	}
	if _, exists := fm.GenerationFor("f"); exists || published != 0 {
		t.Fatalf("failed creation published: exists=%v hooks=%d", exists, published)
	}
	if err := os.Remove(filepath.Join(dir, "history")); err != nil {
		t.Fatal(err)
	}
	created, err := svc.CreateFederation(ctx, req)
	if err != nil || created.FederationGeneration != 38 || published != 1 {
		t.Fatalf("creation retry: %+v %v publications=%d", created, err, published)
	}
	if metrics := save.QueryMOMMetrics("f", 0); !metrics.LastSaveHistoryKnown || metrics.LastSaveExists {
		t.Fatalf("retry did not establish fresh history: %+v", metrics)
	}
}
