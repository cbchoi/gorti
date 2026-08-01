// AdminService unit tests — verify Status / Snapshot field shape and
// nil-source elision. End-to-end (real gRPC server) tests live in
// rti/cmd/rtid/server_test.go.

package grpc

import (
	"context"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/core"
	"github.com/cbchoi/gorti/rti/internal/declaration"
	"github.com/cbchoi/gorti/rti/internal/federation"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/sync"
)

// adminFakeOutbox is a no-op outbox used in admin handler tests.
type adminFakeOutbox struct{}

func (adminFakeOutbox) Send(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.OutboundEvent) error {
	return nil
}

// adminFOMs is a permissive FOM repo.
type adminFOMs struct{}

func (adminFOMs) Load(_ context.Context, _ []core.FOMModule) (core.FOMHandle, error) {
	return adminFOM{}, nil
}
func (adminFOMs) Get(_ context.Context, _ core.FederationName) (core.FOMHandle, error) {
	return adminFOM{}, nil
}

type adminFOM struct{}

func (adminFOM) IsValid() bool { return true }
func (adminFOM) LookupObjectClass(string) (core.ObjectClassHandle, bool) {
	return 1, true
}
func (adminFOM) LookupInteractionClass(string) (core.InteractionClassHandle, bool) {
	return 1, true
}
func (adminFOM) LookupAttribute(core.ObjectClassHandle, string) (core.AttributeHandle, bool) {
	return 1, true
}
func (adminFOM) LookupParameter(core.InteractionClassHandle, string) (core.ParameterHandle, bool) {
	return 1, true
}

func TestAdminService_Status_ReturnsVersionAndUptime(t *testing.T) {
	t.Parallel()
	svc := newAdminService(AdminOptions{
		Version:   "v0.test",
		StartedAt: time.Now().Add(-90 * time.Second),
	})
	resp, err := svc.Status(context.Background(), &rtiv1.StatusRequest{
		WireVersion: rtiv1.WireVersion_WIRE_VERSION_V1,
	})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if resp.GetRtidVersion() != "v0.test" {
		t.Errorf("RtidVersion = %q, want v0.test", resp.GetRtidVersion())
	}
	if got := resp.GetUptimeSeconds(); got < 89 || got > 92 {
		t.Errorf("UptimeSeconds = %d, want ~90", got)
	}
}

func TestAdminService_Status_RejectsBadWireVersion(t *testing.T) {
	t.Parallel()
	svc := newAdminService(AdminOptions{})
	_, err := svc.Status(context.Background(), &rtiv1.StatusRequest{
		WireVersion: rtiv1.WireVersion_WIRE_VERSION_UNSPECIFIED,
	})
	if err == nil {
		t.Fatalf("Status with bad wire version: want error, got nil")
	}
}

func TestAdminService_Status_NilRequest(t *testing.T) {
	t.Parallel()
	svc := newAdminService(AdminOptions{})
	if _, err := svc.Status(context.Background(), nil); err == nil {
		t.Fatalf("Status nil: want error, got nil")
	}
}

func TestAdminService_Status_DefaultVersion(t *testing.T) {
	t.Parallel()
	svc := newAdminService(AdminOptions{})
	resp, err := svc.Status(context.Background(), &rtiv1.StatusRequest{
		WireVersion: rtiv1.WireVersion_WIRE_VERSION_V1,
	})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if resp.GetRtidVersion() != "unknown" {
		t.Errorf("RtidVersion default = %q, want unknown", resp.GetRtidVersion())
	}
	if resp.GetUptimeSeconds() != 0 {
		t.Errorf("UptimeSeconds default = %d, want 0", resp.GetUptimeSeconds())
	}
}

func TestAdminService_Snapshot_AssemblesPerFederation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fedMgr, err := federation.New(federation.Options{
		Clock: core.NewFakeClock(time.Unix(0, 0)),
		FOMs:  adminFOMs{},
	})
	if err != nil {
		t.Fatalf("federation.New: %v", err)
	}
	if err := fedMgr.CreateFederation(ctx, core.CreateFederationRequest{Name: "demo", Mode: core.ModeVerbose}); err != nil {
		t.Fatalf("CreateFederation: %v", err)
	}
	if _, err := fedMgr.JoinFederation(ctx, core.JoinFederationRequest{Federation: "demo", FederateName: "alpha"}); err != nil {
		t.Fatalf("JoinFederation: %v", err)
	}
	declMgr := declaration.New()
	if err := declMgr.PublishObjectClassAttributes(ctx, "demo", 1, 7, []core.AttributeHandle{1, 2}); err != nil {
		t.Fatalf("PublishObjectClassAttributes: %v", err)
	}
	syncMgr, err := sync.New(sync.Options{Outbox: adminFakeOutbox{}})
	if err != nil {
		t.Fatalf("sync.New: %v", err)
	}
	if err := syncMgr.Register(ctx, "demo", "start", nil, []core.FederateHandle{1}); err != nil {
		t.Fatalf("sync.Register: %v", err)
	}

	svc := newAdminService(AdminOptions{
		Federations:  fedMgr,
		Declarations: declMgr,
		Sync:         syncMgr,
		Version:      "v0.admin",
		StartedAt:    time.Now().Add(-5 * time.Second),
	})
	resp, err := svc.Snapshot(ctx, &rtiv1.SnapshotRequest{
		WireVersion: rtiv1.WireVersion_WIRE_VERSION_V1,
	})
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if resp.GetRtidVersion() != "v0.admin" {
		t.Errorf("RtidVersion = %q, want v0.admin", resp.GetRtidVersion())
	}
	if got := len(resp.GetFederations()); got != 1 {
		t.Fatalf("Federations len = %d, want 1", got)
	}
	fs := resp.GetFederations()[0]
	if fs.GetName() != "demo" {
		t.Errorf("Federation name = %q, want demo", fs.GetName())
	}
	if fs.GetFederatesJoined() != 1 {
		t.Errorf("FederatesJoined = %d, want 1", fs.GetFederatesJoined())
	}
	if fs.GetMode() != rtiv1.Mode_MODE_VERBOSE {
		t.Errorf("Mode = %v, want MODE_VERBOSE", fs.GetMode())
	}
	if got := len(fs.GetFederates()); got != 1 {
		t.Fatalf("Federates len = %d, want 1", got)
	}
	fed0 := fs.GetFederates()[0]
	if fed0.GetHandle() != 1 || fed0.GetName() != "alpha" {
		t.Errorf("Federate[0] = (%d,%q), want (1,alpha)", fed0.GetHandle(), fed0.GetName())
	}
	if got := fed0.GetPublishedObjectClasses(); len(got) != 1 || got[0] != 7 {
		t.Errorf("PublishedObjectClasses = %v, want [7]", got)
	}
	if got := len(fs.GetSyncPoints()); got != 1 {
		t.Errorf("SyncPoints len = %d, want 1", got)
	} else if fs.GetSyncPoints()[0].GetLabel() != "start" {
		t.Errorf("SyncPoint[0].Label = %q, want start", fs.GetSyncPoints()[0].GetLabel())
	}
}

// TestAdminService_Snapshot_JoinUnixSeconds verifies that Phase-3's
// new FederateSnapshot.join_unix_seconds field is populated from the
// federation manager's JoinedAt stamp. rtid-TUI Phase 3 — drilldown
// `age` column source data.
func TestAdminService_Snapshot_JoinUnixSeconds(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	t0 := time.Unix(1_700_000_000, 0).UTC()
	clk := core.NewFakeClock(t0)
	fedMgr, err := federation.New(federation.Options{
		Clock: clk,
		FOMs:  adminFOMs{},
	})
	if err != nil {
		t.Fatalf("federation.New: %v", err)
	}
	if err := fedMgr.CreateFederation(ctx, core.CreateFederationRequest{Name: "demo", Mode: core.ModeVerbose}); err != nil {
		t.Fatalf("CreateFederation: %v", err)
	}
	if _, err := fedMgr.JoinFederation(ctx, core.JoinFederationRequest{Federation: "demo", FederateName: "alpha"}); err != nil {
		t.Fatalf("JoinFederation alpha: %v", err)
	}
	clk.Advance(7 * time.Second)
	if _, err := fedMgr.JoinFederation(ctx, core.JoinFederationRequest{Federation: "demo", FederateName: "beta"}); err != nil {
		t.Fatalf("JoinFederation beta: %v", err)
	}

	svc := newAdminService(AdminOptions{Federations: fedMgr})
	resp, err := svc.Snapshot(ctx, &rtiv1.SnapshotRequest{
		WireVersion: rtiv1.WireVersion_WIRE_VERSION_V1,
	})
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	feds := resp.GetFederations()[0].GetFederates()
	if len(feds) != 2 {
		t.Fatalf("Federates len = %d, want 2", len(feds))
	}
	if got := feds[0].GetJoinUnixSeconds(); got != t0.Unix() {
		t.Errorf("alpha JoinUnixSeconds = %d, want %d", got, t0.Unix())
	}
	if got, want := feds[1].GetJoinUnixSeconds(), t0.Add(7*time.Second).Unix(); got != want {
		t.Errorf("beta JoinUnixSeconds = %d, want %d", got, want)
	}
}

func TestAdminService_Snapshot_FilterByFederationName(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fedMgr, err := federation.New(federation.Options{
		Clock: core.NewFakeClock(time.Unix(0, 0)),
		FOMs:  adminFOMs{},
	})
	if err != nil {
		t.Fatalf("federation.New: %v", err)
	}
	for _, name := range []string{"demo", "benchmark"} {
		if err := fedMgr.CreateFederation(ctx, core.CreateFederationRequest{Name: core.FederationName(name), Mode: core.ModeVerbose}); err != nil {
			t.Fatalf("CreateFederation %q: %v", name, err)
		}
	}
	svc := newAdminService(AdminOptions{Federations: fedMgr})
	resp, err := svc.Snapshot(ctx, &rtiv1.SnapshotRequest{
		WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName: "demo",
	})
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if got := len(resp.GetFederations()); got != 1 {
		t.Fatalf("Federations len with filter = %d, want 1", got)
	}
	if resp.GetFederations()[0].GetName() != "demo" {
		t.Errorf("Filtered name = %q, want demo", resp.GetFederations()[0].GetName())
	}
}

func TestAdminService_Snapshot_RequiresFederationsSource(t *testing.T) {
	t.Parallel()
	svc := newAdminService(AdminOptions{})
	_, err := svc.Snapshot(context.Background(), &rtiv1.SnapshotRequest{
		WireVersion: rtiv1.WireVersion_WIRE_VERSION_V1,
	})
	if err == nil {
		t.Fatalf("Snapshot with nil Federations: want error, got nil")
	}
}

func TestAdminService_TailEvents_RequiresEventLog(t *testing.T) {
	t.Parallel()
	svc := newAdminService(AdminOptions{})
	err := svc.TailEvents(&rtiv1.TailEventsRequest{
		WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName: "demo",
	}, nil)
	if err == nil {
		t.Fatalf("TailEvents with nil EventLog: want error, got nil")
	}
}

func TestAdminService_TailEvents_RequiresFederationName(t *testing.T) {
	t.Parallel()
	svc := newAdminService(AdminOptions{EventLog: stubEventLog{}})
	err := svc.TailEvents(&rtiv1.TailEventsRequest{
		WireVersion: rtiv1.WireVersion_WIRE_VERSION_V1,
	}, nil)
	if err == nil {
		t.Fatalf("TailEvents with empty federation_name: want error, got nil")
	}
}

// stubEventLog satisfies core.EventLog with no actual reader.
type stubEventLog struct{}

func (stubEventLog) Append(_ context.Context, _ core.FederationName, _ core.EventRecord) error {
	return nil
}
func (stubEventLog) Sync(_ context.Context, _ core.FederationName) error { return nil }
func (stubEventLog) OpenReader(_ context.Context, _ string) (core.EventLogReader, error) {
	return nil, nil
}
