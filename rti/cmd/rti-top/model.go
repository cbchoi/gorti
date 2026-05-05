// model.go — the bubbletea Model that drives every view in rti-top.
//
// Architecture: a single tea.Model owns
//
//   - the Snapshot poll loop (a tea.Cmd that calls
//     client.Snapshot then dispatches snapshotMsg → next tick),
//   - the current view (Federations, Drilldown, FederateDetail,
//     Time, Wire, Events) — switched by keybindings,
//   - per-view selection + filter state.
//
// Per docs/rtid-tui.md §2.2 the data model is pull-only Phase 1 (with
// the exception of TailEvents in the Events view, which is the only
// streaming RPC).

package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/cbchoi/gorti/rti/cmd/rti-top/internal/client"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
)

// view enumerates the five PINNED views from docs/rtid-tui.md §3.x.
type view int

const (
	viewFederations    view = iota // §3.1 landing
	viewDrilldown                  // §3.2 per-federation
	viewFederateDetail             // §3.2 expanded federate
	viewTime                       // §3.3 time advance — added in commit 3
	viewWire                       // §3.4 wire stats top-table — added in commit 3
	viewEvents                     // §3.5 event log tail — added in commit 4
)

// refreshSteps is the cycle order for the `r` keybinding. Picked to
// span the PINNED [100ms, 60s] range with familiar `top` cadences.
var refreshSteps = []time.Duration{
	100 * time.Millisecond,
	500 * time.Millisecond,
	1 * time.Second,
	2 * time.Second,
	5 * time.Second,
	10 * time.Second,
	30 * time.Second,
	60 * time.Second,
}

// snapshotMsg carries one Snapshot result (or error) into Update.
type snapshotMsg struct {
	resp *rtiv1.SnapshotResponse
	err  error
	at   time.Time
}

// pollTickMsg is dispatched by the timer; on receipt the model
// kicks off the next Snapshot fetch.
type pollTickMsg time.Time

// model is the rti-top bubbletea model.
type model struct {
	// --- wiring (immutable) ---
	cli      *client.Client
	pollCtx  context.Context
	cancelFn context.CancelFunc

	// --- live state ---
	last      *rtiv1.SnapshotResponse
	lastErr   error
	lastAt    time.Time
	refresh   time.Duration
	view      view
	width     int
	height    int
	statusMsg string

	// --- selection state ---
	fedIdx    int    // index into last.Federations for drill-down
	selFed    string // pinned federation name once we drilled
	federate  int    // index into selected federation's federates
	filter    string // /-filter substring; empty = show-all
	filtering bool   // filter input mode
	wireSort  int    // wire view sort column index (commit 3)

	// --- time view sparkline state (commit 3) ---
	// timeHistory[fedName][handle] = recent current_time samples (ring).
	timeHistory map[string]map[uint64]*timeRing
}

// initialModel builds a fresh model. The first Status response seeds
// version + uptime so the header renders something on the very first
// frame (before the first Snapshot returns).
func initialModel(ctx context.Context, cli *client.Client, st *rtiv1.StatusResponse, refresh time.Duration) *model {
	cctx, cancel := context.WithCancel(ctx)
	return &model{
		cli:      cli,
		pollCtx:  cctx,
		cancelFn: cancel,
		refresh:  refresh,
		view:     viewFederations,
		last: &rtiv1.SnapshotResponse{
			RtidVersion:   st.GetRtidVersion(),
			UptimeSeconds: st.GetUptimeSeconds(),
		},
		timeHistory: map[string]map[uint64]*timeRing{},
	}
}

// Init kicks off the first poll immediately so the first frame is
// populated with real data, not an empty placeholder.
func (m *model) Init() tea.Cmd {
	return tea.Batch(m.fetchSnapshot(), m.tickAfter(m.refresh))
}

// fetchSnapshot is a tea.Cmd that calls Snapshot and emits a
// snapshotMsg. Deadline = current refresh interval so a stuck server
// can't wedge polling.
func (m *model) fetchSnapshot() tea.Cmd {
	cli := m.cli
	ctx := m.pollCtx
	dl := m.refresh
	return func() tea.Msg {
		if cli == nil {
			return snapshotMsg{at: time.Now()}
		}
		resp, err := cli.Snapshot(ctx, "", dl)
		return snapshotMsg{resp: resp, err: err, at: time.Now()}
	}
}

// tickAfter returns a tea.Cmd that emits pollTickMsg after d.
func (m *model) tickAfter(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg { return pollTickMsg(t) })
}

// Update is the MVU dispatch. Each branch is intentionally short;
// per-view rendering lives in views.go.
func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = v.Width
		m.height = v.Height
		return m, nil

	case snapshotMsg:
		m.lastAt = v.at
		if v.err != nil {
			m.lastErr = v.err
			// keep prior `m.last` so the UI doesn't blank on transient errors.
			return m, nil
		}
		m.lastErr = nil
		if v.resp != nil {
			m.last = v.resp
			m.recordTimeHistory(v.resp)
		}
		return m, nil

	case pollTickMsg:
		// Dispatch the next fetch + schedule another tick.
		return m, tea.Batch(m.fetchSnapshot(), m.tickAfter(m.refresh))

	case tea.KeyMsg:
		return m.handleKey(v)
	}
	return m, nil
}

// handleKey routes keystrokes by current view. Filter input mode
// captures everything except Esc / Enter.
func (m *model) handleKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.filtering {
		switch k.String() {
		case "esc":
			m.filtering = false
			m.filter = ""
		case "enter":
			m.filtering = false
		case "backspace":
			if len(m.filter) > 0 {
				m.filter = m.filter[:len(m.filter)-1]
			}
		default:
			if len(k.String()) == 1 {
				m.filter += k.String()
			}
		}
		return m, nil
	}

	switch k.String() {
	// global navigation
	case "q", "Q", "ctrl+c":
		m.cancelFn()
		return m, tea.Quit
	case "f", "F":
		m.view = viewFederations
		return m, nil
	case "t", "T":
		// T from any view → Time advance view for the selected
		// federation (or first federation if none selected yet).
		if m.selFed == "" {
			feds := m.filteredFederations()
			if len(feds) > 0 {
				m.selFed = feds[m.fedIdx].GetName()
			}
		}
		m.view = viewTime
		return m, nil
	case "w", "W":
		// W from any view → Wire stats view (cross-federation table).
		m.view = viewWire
		return m, nil
	case "s", "S":
		// In Wire view S cycles sort column. Outside Wire view S is
		// a no-op in commit 3 (events filter takes S in commit 4).
		if m.view == viewWire {
			m.wireSort = (m.wireSort + 1) % wireSortColumnCount
		}
		return m, nil
	case "o", "O":
		// O = drilldown into the highlighted federation. The §3.1
		// keybinding row labels it "[O]bjects" — drilldown is the
		// closest semantic match in Phase 2 (the drilldown footer
		// reports object counts).
		return m.enterDrilldown()
	case "r", "R":
		m.refresh = nextRefresh(m.refresh)
		return m, nil
	case "/":
		m.filtering = true
		m.filter = ""
		return m, nil
	case "esc":
		return m.escape()
	case "enter":
		return m.enter()
	case "up", "k":
		m.moveSelection(-1)
		return m, nil
	case "down", "j":
		m.moveSelection(+1)
		return m, nil
	}
	return m, nil
}

// enterDrilldown advances from the Federations list into the
// drill-down view for the currently-selected federation.
func (m *model) enterDrilldown() (tea.Model, tea.Cmd) {
	feds := m.filteredFederations()
	if len(feds) == 0 {
		return m, nil
	}
	if m.fedIdx >= len(feds) {
		m.fedIdx = len(feds) - 1
	}
	m.selFed = feds[m.fedIdx].GetName()
	m.federate = 0
	m.view = viewDrilldown
	return m, nil
}

// enter dispatches Enter by current view.
func (m *model) enter() (tea.Model, tea.Cmd) {
	switch m.view {
	case viewFederations:
		return m.enterDrilldown()
	case viewDrilldown:
		m.view = viewFederateDetail
		return m, nil
	}
	return m, nil
}

// escape pops back one view level.
func (m *model) escape() (tea.Model, tea.Cmd) {
	switch m.view {
	case viewFederateDetail:
		m.view = viewDrilldown
	case viewDrilldown, viewTime, viewWire:
		m.view = viewFederations
	}
	return m, nil
}

// moveSelection moves the selection cursor for the active view.
func (m *model) moveSelection(delta int) {
	switch m.view {
	case viewFederations:
		feds := m.filteredFederations()
		if len(feds) == 0 {
			return
		}
		m.fedIdx = clamp(m.fedIdx+delta, 0, len(feds)-1)
	case viewDrilldown, viewFederateDetail, viewTime:
		fed := m.findFederation(m.selFed)
		if fed == nil {
			return
		}
		m.federate = clamp(m.federate+delta, 0, max(0, len(fed.GetFederates())-1))
	case viewWire:
		// scroll-only — no per-row selection in wire view.
	}
}

// View renders the current model state.
func (m *model) View() string {
	if m.width == 0 {
		// Pre-WindowSize render — bubbletea always sends a sizing msg
		// soon after Init, so any output here is transient.
		return ""
	}
	header := m.renderHeader()
	body := m.renderBody()
	footer := m.renderFooter()
	return lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
}

// nextRefresh advances `r`-cycled refresh through refreshSteps.
func nextRefresh(cur time.Duration) time.Duration {
	idx := 0
	for i, s := range refreshSteps {
		if cur >= s {
			idx = i
		}
	}
	idx = (idx + 1) % len(refreshSteps)
	return refreshSteps[idx]
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// max is shadowed in go 1.21+ as a builtin but the project pins
// go 1.22 with no explicit guarantee, so we keep a local helper.
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// filteredFederations returns the federations matching the current
// /-filter substring (case-insensitive). Empty filter → return all.
func (m *model) filteredFederations() []*rtiv1.FederationSnapshot {
	if m.last == nil {
		return nil
	}
	all := m.last.GetFederations()
	if m.filter == "" {
		return all
	}
	needle := strings.ToLower(m.filter)
	out := make([]*rtiv1.FederationSnapshot, 0, len(all))
	for _, f := range all {
		if strings.Contains(strings.ToLower(f.GetName()), needle) {
			out = append(out, f)
		}
	}
	return out
}

// findFederation looks up a federation by name in the latest snapshot.
func (m *model) findFederation(name string) *rtiv1.FederationSnapshot {
	if m.last == nil || name == "" {
		return nil
	}
	for _, f := range m.last.GetFederations() {
		if f.GetName() == name {
			return f
		}
	}
	return nil
}

// runTUI installs the bubbletea Program and blocks until the user
// quits or ctx is cancelled. Replaces the commit-1 stub.
func runTUI(ctx context.Context, cli *client.Client, st *rtiv1.StatusResponse, refresh time.Duration) error {
	m := initialModel(ctx, cli, st, refresh)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		// Don't propagate context.Canceled — that's the expected exit
		// path when the user hits Q.
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return fmt.Errorf("rti-top: tea run: %w", err)
	}
	return nil
}
