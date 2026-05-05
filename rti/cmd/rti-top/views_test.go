// Render-layer tests for rti-top — exercise each view with a hand-
// built SnapshotResponse fixture and assert the rendered output
// contains the fields the design doc mocks specify.
//
// We don't snapshot-match the full output (terminal width / lipgloss
// styling produce environment-dependent ANSI bytes); instead we
// substring-match the load-bearing labels. Keeps the tests stable
// across machines while still catching regressions where a column
// disappears.

package main

import (
	"context"
	"strings"
	"testing"
	"time"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
)

// fixtureSnapshot builds a representative SnapshotResponse — two
// federations, three federates each, exercising the union of
// regulating/constrained/observer roles.
func fixtureSnapshot() *rtiv1.SnapshotResponse {
	pending := 6.0
	return &rtiv1.SnapshotResponse{
		RtidVersion:   "rtid-cut2",
		UptimeSeconds: 12222,
		Federations: []*rtiv1.FederationSnapshot{
			{
				Name:                "demo",
				Mode:                rtiv1.Mode_MODE_VERBOSE,
				FederatesJoined:     3,
				ObjectInstanceCount: 12,
				PublishedClasses:    []uint64{1, 2, 3, 4, 5},
				RegionCount:         0,
				SaveState:           rtiv1.SaveState_SAVE_STATE_IDLE,
				RestoreState:        rtiv1.RestoreState_RESTORE_STATE_IDLE,
				Time: &rtiv1.TimeSnapshot{
					Lbts: 5.5,
					PendingGrants: []*rtiv1.PendingGrant{
						{FederateHandle: 1, RequestedTime: 6.0},
					},
				},
				SyncPoints: []*rtiv1.SyncPointSnapshot{
					{Label: "start_simulation", State: rtiv1.SyncPointState_SYNC_POINT_STATE_ACHIEVED},
				},
				Federates: []*rtiv1.FederateSnapshot{
					{
						Handle: 1, Name: "generator",
						CurrentTime: 5.0, Lookahead: 1.0, Regulating: true,
						UpdatesSent: 50, OutboxCapacity: 8192,
						PendingRequestTime: &pending,
					},
					{
						Handle: 2, Name: "buffer",
						CurrentTime: 5.0, Lookahead: 0.5, Regulating: true, Constrained: true,
						UpdatesSent: 25, ReflectionsReceived: 50,
						OutboxQueueDepth: 4, OutboxCapacity: 8192, DropsTotal: 6,
					},
					{
						Handle: 3, Name: "processor",
						CurrentTime: 5.0, Lookahead: 1.0, Constrained: true,
						ReflectionsReceived: 25, OutboxCapacity: 8192,
					},
				},
			},
			{
				Name:            "benchmark",
				Mode:            rtiv1.Mode_MODE_BEST_EFFORT,
				FederatesJoined: 5,
				Time:            &rtiv1.TimeSnapshot{Lbts: 0},
			},
		},
	}
}

func newTestModel(t *testing.T) *model {
	t.Helper()
	ctx := context.Background()
	st := &rtiv1.StatusResponse{RtidVersion: "rtid-cut2", UptimeSeconds: 1}
	m := initialModel(ctx, nil, st, 1*time.Second)
	m.last = fixtureSnapshot()
	m.width = 120
	m.height = 40
	return m
}

func TestRender_FederationsView(t *testing.T) {
	m := newTestModel(t)
	out := m.renderFederationsView()
	for _, want := range []string{
		"FEDERATION", "MODE", "FEDERATES", "PUB CLASSES", "OBJECTS", "TPS",
		"demo", "verbose", "benchmark", "best-effort",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("renderFederationsView missing %q\n--- output ---\n%s", want, out)
		}
	}
}

func TestRender_DrilldownView(t *testing.T) {
	m := newTestModel(t)
	m.selFed = "demo"
	m.view = viewDrilldown
	out := m.renderDrilldownView()
	for _, want := range []string{
		"FEDERATE", "HANDLE", "TIME", "LOOKAHEAD", "ROLE", "TPS", "DROP", "AGE",
		"generator", "buffer", "processor",
		"LBTS:", "Sync points:", "Save state:", "Region count:",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("renderDrilldownView missing %q\n--- output ---\n%s", want, out)
		}
	}
}

func TestRender_FederateDetailView(t *testing.T) {
	m := newTestModel(t)
	m.selFed = "demo"
	m.federate = 1 // buffer — has reg+const role + drops
	out := m.renderFederateDetailView()
	for _, want := range []string{
		"IDENTITY", "TIME", "PUB / SUB", "WIRE STATS",
		"buffer", "reg+const", "outbox queue=4/8192", "drops_total=6",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("renderFederateDetailView missing %q\n--- output ---\n%s", want, out)
		}
	}
	// Sync section must appear since we have a sync point.
	if !strings.Contains(out, "SYNC") || !strings.Contains(out, "start_simulation") {
		t.Errorf("renderFederateDetailView missing SYNC section: %s", out)
	}
}

func TestNextRefresh_Cycles(t *testing.T) {
	d := 1 * time.Second
	for i := 0; i < len(refreshSteps)*2; i++ {
		d = nextRefresh(d)
		if d < minRefresh || d > maxRefresh {
			t.Fatalf("nextRefresh produced %s outside [%s, %s]", d, minRefresh, maxRefresh)
		}
	}
}

func TestEnterDrilldownAndEscape(t *testing.T) {
	m := newTestModel(t)
	if m.view != viewFederations {
		t.Fatalf("initial view=%v want %v", m.view, viewFederations)
	}
	m.enterDrilldown()
	if m.view != viewDrilldown || m.selFed != "demo" {
		t.Fatalf("after enter: view=%v selFed=%q", m.view, m.selFed)
	}
	m.enter()
	if m.view != viewFederateDetail {
		t.Fatalf("after second enter: view=%v want %v", m.view, viewFederateDetail)
	}
	m.escape()
	if m.view != viewDrilldown {
		t.Fatalf("after esc: view=%v want %v", m.view, viewDrilldown)
	}
	m.escape()
	if m.view != viewFederations {
		t.Fatalf("after second esc: view=%v want %v", m.view, viewFederations)
	}
}

func TestFilter_Federations(t *testing.T) {
	m := newTestModel(t)
	m.filter = "bench"
	got := m.filteredFederations()
	if len(got) != 1 || got[0].GetName() != "benchmark" {
		t.Errorf("filter=bench → %v; want [benchmark]", got)
	}
}
