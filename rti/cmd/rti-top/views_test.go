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
	m.recordTimeHistory(m.last)
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

func TestRender_TimeView(t *testing.T) {
	m := newTestModel(t)
	m.selFed = "demo"
	out := m.renderTimeView()
	for _, want := range []string{
		"FEDERATE", "CURRENT", "PENDING", "LOOKAHEAD", "CONTRIBUTION", "STATE",
		"LBTS:", "Time history",
		"generator", "buffer", "processor",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("renderTimeView missing %q\n--- output ---\n%s", want, out)
		}
	}
}

// TestFormatAge_ScaleSelection verifies that the Phase-3 age
// formatter chooses the right human-readable unit at each magnitude.
// docs/rtid-tui.md §3.2 — the drilldown view's `age` column.
func TestFormatAge_ScaleSelection(t *testing.T) {
	now := time.Unix(2_000_000_000, 0).UTC()
	cases := []struct {
		name   string
		joinAt time.Time
		want   string
	}{
		{"sub-minute_5s", now.Add(-5 * time.Second), "5s"},
		{"sub-hour_5min", now.Add(-5 * time.Minute), "5m0s"},
		{"sub-hour_12m3s", now.Add(-(12*time.Minute + 3*time.Second)), "12m3s"},
		{"sub-day_5h", now.Add(-5 * time.Hour), "5h0m"},
		{"sub-day_2h15m", now.Add(-(2*time.Hour + 15*time.Minute)), "2h15m"},
		{"multi-day", now.Add(-(3*24*time.Hour + 4*time.Hour)), "3d4h"},
		{"zero_join_unix", time.Unix(0, 0), "-"},
		{"negative_skew", now.Add(2 * time.Second), "0s"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := formatAge(tc.joinAt.Unix(), now)
			if tc.name == "zero_join_unix" {
				got = formatAge(0, now)
			}
			if got != tc.want {
				t.Errorf("formatAge = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestRender_DrilldownView_RendersAgeColumn verifies the drilldown
// view replaces Phase-2's `-` placeholder with a populated age value
// when the snapshot carries join_unix_seconds. rtid-TUI Phase 3.
func TestRender_DrilldownView_RendersAgeColumn(t *testing.T) {
	m := newTestModel(t)
	m.selFed = "demo"
	m.view = viewDrilldown
	// Stamp a join_unix_seconds 30s in the past on every federate so
	// the age column renders as `30s`.
	now := time.Now()
	for _, f := range m.last.GetFederations()[0].GetFederates() {
		f.JoinUnixSeconds = now.Add(-30 * time.Second).Unix()
	}
	out := m.renderDrilldownView()
	// At least one populated age cell should appear (avoids matching
	// the `-` placeholder used elsewhere in the UI).
	if !strings.Contains(out, "30s") {
		t.Errorf("renderDrilldownView age column not populated\n--- output ---\n%s", out)
	}
}

func TestRender_WireView(t *testing.T) {
	m := newTestModel(t)
	out := m.renderWireView()
	for _, want := range []string{
		"FEDERATION", "FEDERATE", "SENDS", "RECVS", "DROPS", "Q-DEPTH", "Q-MAX",
		"Total:", "Outbox utilization",
		"demo", "buffer",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("renderWireView missing %q\n--- output ---\n%s", want, out)
		}
	}
}

func TestSparkline_DegenerateAndNonDegenerate(t *testing.T) {
	if got := renderSparkline(nil); !strings.Contains(got, "no samples") {
		t.Errorf("nil → %q; want placeholder", got)
	}
	if got := renderSparkline([]float64{1, 1, 1}); got == "" {
		t.Errorf("flat input rendered empty")
	}
	got := renderSparkline([]float64{0, 0.25, 0.5, 0.75, 1.0})
	if len(got) == 0 {
		t.Errorf("non-flat input rendered empty")
	}
}

func TestWireSort_CyclesColumns(t *testing.T) {
	m := newTestModel(t)
	for i := 0; i < wireSortColumnCount*2; i++ {
		_ = m.renderWireView()
		m.wireSort = (m.wireSort + 1) % wireSortColumnCount
	}
}

func TestTimeRing_Order(t *testing.T) {
	r := &timeRing{}
	for i := 0; i < timeRingCap+5; i++ {
		r.push(float64(i))
	}
	v := r.values()
	if len(v) != timeRingCap {
		t.Fatalf("len=%d want %d", len(v), timeRingCap)
	}
	if v[0] != 5 {
		t.Errorf("oldest=%v want 5 (eviction order)", v[0])
	}
	if v[len(v)-1] != float64(timeRingCap+4) {
		t.Errorf("newest=%v want %v", v[len(v)-1], timeRingCap+4)
	}
}

func TestRender_EventsView_NoStream(t *testing.T) {
	m := newTestModel(t)
	m.view = viewEvents
	out := m.renderEventsView()
	if !strings.Contains(out, "events stream not running") {
		t.Errorf("renderEventsView (no stream) missing placeholder: %q", out)
	}
}

func TestRender_EventsView_WithLines(t *testing.T) {
	m := newTestModel(t)
	m.view = viewEvents
	m.events = &eventsState{fed: "demo"}
	m.events.lines = []string{"seq=1  ts=0  payload_bytes=0", "seq=2  ts=0  payload_bytes=0"}
	out := m.renderEventsView()
	for _, want := range []string{"Event log", "demo", "seq=1", "seq=2", "Phase 2 limitation"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderEventsView missing %q\n--- output ---\n%s", want, out)
		}
	}
}

func TestEventsState_PauseToggleAndCap(t *testing.T) {
	m := newTestModel(t)
	m.events = &eventsState{fed: "demo"}
	for i := 0; i < eventLineCap+50; i++ {
		m.handleEventMsg(eventTailMsg{line: "x"})
	}
	if got := len(m.events.lines); got != eventLineCap {
		t.Errorf("cap: got %d want %d", got, eventLineCap)
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
