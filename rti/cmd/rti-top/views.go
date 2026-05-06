// views.go — per-view rendering. Each render function takes the
// model and returns a string; the model dispatches in View().
//
// Style philosophy: lipgloss-styled boxes for headers + footers, a
// plain monospaced table for body content. The mockups in
// docs/rtid-tui.md §3.x are the source of truth for column ordering
// and footer text.
//
// Commit 2 ships Federations + Drilldown + FederateDetail. Commit 3
// adds Time + Wire; commit 4 adds Events.

package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
)

// --- styles -----------------------------------------------------------------

var (
	styleHeader = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("12")).
			Border(lipgloss.NormalBorder(), false, false, true, false).
			BorderForeground(lipgloss.Color("240"))

	styleFooter = lipgloss.NewStyle().
			Faint(true).
			Border(lipgloss.NormalBorder(), true, false, false, false).
			BorderForeground(lipgloss.Color("240"))

	styleColHead = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("14"))

	styleSelectedRow = lipgloss.NewStyle().
				Background(lipgloss.Color("237")).
				Foreground(lipgloss.Color("15"))

	styleErr = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))

	styleDim = lipgloss.NewStyle().Faint(true)
)

// --- header / footer --------------------------------------------------------

// renderHeader renders the always-visible top banner with version,
// uptime, federation count, and the keybinding row.
func (m *model) renderHeader() string {
	uptime := time.Duration(m.last.GetUptimeSeconds()) * time.Second
	feds := m.last.GetFederations()
	fedCount := len(feds)
	totalFedates := 0
	for _, f := range feds {
		totalFedates += int(f.GetFederatesJoined())
	}
	title := fmt.Sprintf(" gorti rtid %s — uptime %s — %d federation%s — %d federate%s — refresh %s ",
		m.last.GetRtidVersion(),
		humanDuration(uptime),
		fedCount, plural(fedCount),
		totalFedates, plural(totalFedates),
		m.refresh)
	keys := " [F]ederations  [T]ime  [W]ire  [O]bjects  [I]nteractions  [Q]uit "
	if m.lastErr != nil {
		keys += "  " + styleErr.Render("! "+m.lastErr.Error())
	}
	out := styleHeader.Width(m.width).Render(title) + "\n" + keys
	// Phase 5: surface the most recent mutating-op result as a status
	// line below the header.
	if m.statusMsg != "" {
		out += "\n " + m.statusMsg
	}
	return out
}

// renderFooter draws the bottom keybinding hint row matching the
// design-doc mockups (§3.1..§3.5 each show their own footer). Phase
// 5 augments the federation / drilldown footers with X (force-resign)
// and D (destroy federation) keybindings — but ONLY when the
// MutatingService probe at startup succeeded. Read-only daemons keep
// the footer unchanged.
func (m *model) renderFooter() string {
	var hint string
	switch m.view {
	case viewFederations:
		hint = " ↑↓ select  Enter drill-down  R refresh-rate  / filter  Q quit "
		if m.mutatingEnabled {
			hint += " D destroy "
		}
	case viewDrilldown:
		hint = " Esc back  ↑↓ select federate  Enter inspect  T time view  W wire view  I events "
		if m.mutatingEnabled {
			hint += " X force-resign  D destroy "
		}
	case viewFederateDetail:
		hint = " Esc back  T time view  W wire view  I events  Q quit "
		if m.mutatingEnabled {
			hint += " X force-resign "
		}
	case viewTime:
		hint = " Esc back  W wire view  R refresh-rate  Q quit "
	case viewWire:
		hint = " S sort  T window  C columns  / filter  R refresh-rate  Esc back  Q quit "
	case viewEvents:
		hint = " F filter  P pause/resume  Esc back  Q quit "
	}
	if m.filtering {
		hint = fmt.Sprintf(" filter: %s_  (Enter accept  Esc cancel) ", m.filter)
	}
	if m.confirmKind != confirmNone {
		hint = m.renderConfirmHint()
	}
	return styleFooter.Width(m.width).Render(hint)
}

// renderConfirmHint renders the modal confirmation footer for Phase
// 5 X / D keybindings. ForceResign accepts a single y; Destroy needs
// y typed twice (the count is rendered so the operator sees their
// progress).
func (m *model) renderConfirmHint() string {
	switch m.confirmKind {
	case confirmForceResign:
		return fmt.Sprintf(
			" ForceResign federate handle=%d on federation %q? [y]es / [n]o ",
			m.confirmTarget, m.selFed,
		)
	case confirmDestroyFederation:
		return fmt.Sprintf(
			" DestroyFederation %q (evicts %d federates)? type Y twice — confirmed %d/2  [n]o cancels ",
			m.selFed, m.federationFederateCount(m.selFed), m.destroyFedConfirmCount,
		)
	}
	return ""
}

// federationFederateCount returns the count of federates currently
// joined to the named federation in the latest snapshot. Used to
// surface the eviction count in the destroy-federation confirmation.
func (m *model) federationFederateCount(name string) int {
	fed := m.findFederation(name)
	if fed == nil {
		return 0
	}
	return len(fed.GetFederates())
}

// renderBody dispatches to the per-view body renderer.
func (m *model) renderBody() string {
	switch m.view {
	case viewFederations:
		return m.renderFederationsView()
	case viewDrilldown:
		return m.renderDrilldownView()
	case viewFederateDetail:
		return m.renderFederateDetailView()
	case viewTime:
		return m.renderTimeView()
	case viewWire:
		return m.renderWireView()
	case viewEvents:
		return m.renderEventsView()
	}
	return styleDim.Render("  (unknown view)")
}

// --- Federations view (§3.1) ------------------------------------------------

// renderFederationsView is the landing view: one row per federation,
// columns matching the §3.1 mockup.
func (m *model) renderFederationsView() string {
	feds := m.filteredFederations()
	if len(feds) == 0 {
		return styleDim.Render("  (no federations — start one to see it here)")
	}
	cols := []string{"FEDERATION", "MODE", "FEDERATES", "PUB CLASSES", "OBJECTS", "TPS"}
	widths := []int{22, 14, 10, 12, 9, 10}
	var b strings.Builder
	b.WriteString("  " + styleColHead.Render(formatRow(cols, widths)))
	b.WriteString("\n")
	for i, f := range feds {
		row := []string{
			f.GetName(),
			modeString(f.GetMode()),
			fmt.Sprintf("%d", f.GetFederatesJoined()),
			fmt.Sprintf("%d", len(f.GetPublishedClasses())),
			fmt.Sprintf("%d", f.GetObjectInstanceCount()),
			fmt.Sprintf("%.1f", aggregateTPS(f)),
		}
		line := formatRow(row, widths)
		if i == m.fedIdx {
			line = styleSelectedRow.Render("▶ " + line)
		} else {
			line = "  " + line
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

// --- Drilldown view (§3.2) --------------------------------------------------

// renderDrilldownView is the per-federation detail view with the
// PINNED column set: name, handle, current_time, lookahead, role,
// tps, queue_depth, drops_total, age (docs/rtid-tui.md §3.2).
func (m *model) renderDrilldownView() string {
	fed := m.findFederation(m.selFed)
	if fed == nil {
		return styleDim.Render("  (no federation selected)")
	}
	cols := []string{"FEDERATE", "HANDLE", "TIME", "LOOKAHEAD", "ROLE", "TPS", "Q", "DROP", "AGE"}
	widths := []int{18, 7, 9, 10, 14, 7, 5, 7, 6}
	var b strings.Builder
	b.WriteString(fmt.Sprintf(" Federation: %s — %s — federates_joined=%d\n\n",
		fed.GetName(), modeString(fed.GetMode()), fed.GetFederatesJoined()))
	b.WriteString("  " + styleColHead.Render(formatRow(cols, widths)))
	b.WriteString("\n")
	// Phase 3 — §5 filter polish: in the drilldown view, the `/` filter
	// substring matches against federate names within the current
	// federation. Empty filter shows all federates.
	feds := filterFederates(fed.GetFederates(), m.filter)
	if len(feds) == 0 {
		b.WriteString(styleDim.Render("  (no federates joined)\n"))
	}
	for i, f := range feds {
		row := []string{
			f.GetName(),
			fmt.Sprintf("%d", f.GetHandle()),
			fmt.Sprintf("%.2f", f.GetCurrentTime()),
			fmt.Sprintf("%.2f", f.GetLookahead()),
			roleString(f),
			tpsForFederate(f),
			fmt.Sprintf("%d", f.GetOutboxQueueDepth()),
			fmt.Sprintf("%d", f.GetDropsTotal()),
			formatAge(f.GetJoinUnixSeconds(), time.Now()),
		}
		line := formatRow(row, widths)
		if i == m.federate {
			line = styleSelectedRow.Render("▶ " + line)
		} else {
			line = "  " + line
		}
		b.WriteString(line)
		b.WriteString("\n")
	}

	// Footer rows from the §3.2 mockup: LBTS, sync points, save, regions.
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf(" LBTS: %s    Pending grants: %s\n",
		formatLBTS(fed.GetTime().GetLbts()),
		formatPending(fed.GetTime().GetPendingGrants())))
	b.WriteString(fmt.Sprintf(" Sync points: %s\n", formatSyncPoints(fed.GetSyncPoints())))
	b.WriteString(fmt.Sprintf(" Save state:  %s\n", saveStateLabel(fed.GetSaveState())))
	b.WriteString(fmt.Sprintf(" Region count: %d%s\n",
		fed.GetRegionCount(),
		regionSuffix(fed.GetRegionCount())))
	return b.String()
}

// --- FederateDetail view (§3.2 expanded) -----------------------------------

// renderFederateDetailView is the Enter-on-a-federate-row expanded
// panel: identity + time + pub/sub + wire stats. Save / DDM / sync
// sections are collapsed when empty per §3.2 PINNED.
func (m *model) renderFederateDetailView() string {
	fed := m.findFederation(m.selFed)
	if fed == nil {
		return styleDim.Render("  (no federation selected)")
	}
	feds := fed.GetFederates()
	if len(feds) == 0 || m.federate >= len(feds) {
		return styleDim.Render("  (no federate selected)")
	}
	f := feds[m.federate]
	var b strings.Builder
	b.WriteString(fmt.Sprintf(" Federate: %s  (handle=%d)  Federation: %s\n\n",
		f.GetName(), f.GetHandle(), fed.GetName()))

	// Identity
	b.WriteString(styleColHead.Render(" IDENTITY") + "\n")
	b.WriteString(fmt.Sprintf("   name=%s  handle=%d  role=%s\n",
		f.GetName(), f.GetHandle(), roleString(f)))
	b.WriteString("\n")

	// Time
	b.WriteString(styleColHead.Render(" TIME") + "\n")
	b.WriteString(fmt.Sprintf("   current=%.3f  lookahead=%.3f  pending=%s  contribution=%s\n",
		f.GetCurrentTime(), f.GetLookahead(),
		formatPending1(f), formatContribution(f)))
	b.WriteString("\n")

	// Pub/sub
	b.WriteString(styleColHead.Render(" PUB / SUB") + "\n")
	b.WriteString(fmt.Sprintf("   pub object classes:       %s\n", joinHandles(f.GetPublishedObjectClasses())))
	b.WriteString(fmt.Sprintf("   sub object classes:       %s\n", joinHandles(f.GetSubscribedObjectClasses())))
	b.WriteString(fmt.Sprintf("   pub interaction classes:  %s\n", joinHandles(f.GetPublishedInteractionClasses())))
	b.WriteString(fmt.Sprintf("   sub interaction classes:  %s\n", joinHandles(f.GetSubscribedInteractionClasses())))
	b.WriteString("\n")

	// Wire stats
	b.WriteString(styleColHead.Render(" WIRE STATS") + "\n")
	b.WriteString(fmt.Sprintf("   updates_sent=%d  interactions_sent=%d\n",
		f.GetUpdatesSent(), f.GetInteractionsSent()))
	b.WriteString(fmt.Sprintf("   reflections_received=%d  interactions_received=%d\n",
		f.GetReflectionsReceived(), f.GetInteractionsReceived()))
	b.WriteString(fmt.Sprintf("   outbox queue=%d/%d  drops_total=%d\n",
		f.GetOutboxQueueDepth(), f.GetOutboxCapacity(), f.GetDropsTotal()))
	b.WriteString("\n")

	// Optional sections — collapsed when empty per §3.2 PINNED.
	if fed.GetSaveState() != rtiv1.SaveState_SAVE_STATE_UNSPECIFIED &&
		fed.GetSaveState() != rtiv1.SaveState_SAVE_STATE_IDLE {
		b.WriteString(styleColHead.Render(" SAVE") + "\n")
		b.WriteString(fmt.Sprintf("   state=%s  restore=%s\n",
			saveStateLabel(fed.GetSaveState()),
			restoreStateLabel(fed.GetRestoreState())))
		b.WriteString("\n")
	}
	if fed.GetRegionCount() > 0 {
		b.WriteString(styleColHead.Render(" DDM") + "\n")
		b.WriteString(fmt.Sprintf("   federation region count=%d\n", fed.GetRegionCount()))
		b.WriteString("\n")
	}
	if len(fed.GetSyncPoints()) > 0 {
		b.WriteString(styleColHead.Render(" SYNC") + "\n")
		for _, sp := range fed.GetSyncPoints() {
			b.WriteString(fmt.Sprintf("   %s: %s  required=%d  achieved=%d\n",
				sp.GetLabel(), syncStateLabel(sp.GetState()),
				len(sp.GetRequiredHandles()), len(sp.GetAchievedHandles())))
		}
		b.WriteString("\n")
	}
	// (Ownership pending — Phase 1 SnapshotResponse does not carry
	// per-federate pending ownership lists; documented in README.)
	return b.String()
}

// --- Time view (§3.3) -------------------------------------------------------

// renderTimeView is the per-federate time-advance view with the
// ASCII sparkline of recent current_time samples.
func (m *model) renderTimeView() string {
	fed := m.findFederation(m.selFed)
	if fed == nil {
		// In the case where the user hits T from the Federations view
		// without drilling down, default to the first federation.
		feds := m.filteredFederations()
		if len(feds) > 0 {
			fed = feds[0]
		}
	}
	if fed == nil {
		return styleDim.Render("  (no federation)")
	}
	cols := []string{"FEDERATE", "CURRENT", "PENDING", "LOOKAHEAD", "CONTRIBUTION", "STATE"}
	widths := []int{16, 9, 9, 10, 13, 30}
	var b strings.Builder
	b.WriteString(fmt.Sprintf(" Time advance — Federation: %s\n\n", fed.GetName()))
	b.WriteString(styleColHead.Render(formatRow(cols, widths)))
	b.WriteString("\n")
	for _, f := range fed.GetFederates() {
		row := []string{
			f.GetName(),
			fmt.Sprintf("%.2f", f.GetCurrentTime()),
			formatPending1(f),
			fmt.Sprintf("%.2f", f.GetLookahead()),
			formatContribution(f),
			timeStateString(f),
		}
		b.WriteString(formatRow(row, widths))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf(" LBTS: %s   (= min over regulators of current+lookahead)\n",
		formatLBTS(fed.GetTime().GetLbts())))
	b.WriteString("\n")
	b.WriteString(" Time history (last 30 ticks, normalized):\n")
	perFed := m.timeHistory[fed.GetName()]
	for _, f := range fed.GetFederates() {
		ring := perFed[f.GetHandle()]
		var spark string
		if ring != nil {
			spark = renderSparkline(ring.values())
		}
		b.WriteString(fmt.Sprintf("   %-14s %s\n", f.GetName(), spark))
	}
	return b.String()
}

// timeStateString summarises a federate's time-management state for
// the rightmost column of the Time view.
func timeStateString(f *rtiv1.FederateSnapshot) string {
	if f.PendingRequestTime != nil {
		return fmt.Sprintf("awaiting LBTS≥%.2f", *f.PendingRequestTime)
	}
	if f.GetRegulating() && f.GetConstrained() {
		return "idle (no request)"
	}
	if f.GetConstrained() {
		return "constrained-only"
	}
	return "observer"
}

// --- Wire view (§3.4) -------------------------------------------------------

// wireSortColumn enumerates the sortable columns in the Wire view,
// cycled via `S`.
const (
	wireSortFederation = iota
	wireSortFederate
	wireSortSends
	wireSortRecvs
	wireSortDrops
	wireSortQueue
	wireSortColumnCount
)

func wireSortLabel(c int) string {
	switch c {
	case wireSortFederation:
		return "FEDERATION"
	case wireSortFederate:
		return "FEDERATE"
	case wireSortSends:
		return "SENDS"
	case wireSortRecvs:
		return "RECVS"
	case wireSortDrops:
		return "DROPS"
	case wireSortQueue:
		return "Q-DEPTH"
	}
	return "?"
}

// wireRow is one (federation, federate) row for the Wire view.
type wireRow struct {
	fed       string
	name      string
	sends     uint64
	recvs     uint64
	drops     uint64
	q         uint32
	qmax      uint32
	sendsRate float64
	recvsRate float64
	dropsRate float64
}

// renderWireView is the top-style table across every (federation,
// federate). Phase-3 of the rtid-TUI plan adds:
//   - per-row rate columns (`SENDS/s`, `RECVS/s`, `DROPS/s`)
//     computed from a client-side ring of the last 60 snapshot
//     ticks; the `T` key cycles the averaging window;
//   - a header status line that names the active window so the
//     operator knows what they're reading.
//
// Cumulative totals stay on the row (small font in the README's
// fixed-width sample; just additional columns here) — they're
// the source of truth that the rate columns derive from.
func (m *model) renderWireView() string {
	rows := []wireRow{}
	span := wireWindowSpan(m.wireWindow)
	for _, fed := range m.last.GetFederations() {
		perFed := m.wireRates[fed.GetName()]
		for _, f := range fed.GetFederates() {
			r := wireRow{
				fed:   fed.GetName(),
				name:  f.GetName(),
				sends: f.GetUpdatesSent() + f.GetInteractionsSent(),
				recvs: f.GetReflectionsReceived() + f.GetInteractionsReceived(),
				drops: f.GetDropsTotal(),
				q:     f.GetOutboxQueueDepth(),
				qmax:  f.GetOutboxCapacity(),
			}
			if perFed != nil {
				if ring := perFed[f.GetHandle()]; ring != nil {
					r.sendsRate, r.recvsRate, r.dropsRate = ring.rate(span)
				}
			}
			rows = append(rows, r)
		}
	}
	rows = m.wireFilterRows(rows)
	sort.SliceStable(rows, func(i, j int) bool {
		switch m.wireSort {
		case wireSortFederation:
			if rows[i].fed != rows[j].fed {
				return rows[i].fed < rows[j].fed
			}
			return rows[i].name < rows[j].name
		case wireSortFederate:
			return rows[i].name < rows[j].name
		case wireSortSends:
			return rows[i].sends > rows[j].sends
		case wireSortRecvs:
			return rows[i].recvs > rows[j].recvs
		case wireSortDrops:
			return rows[i].drops > rows[j].drops
		case wireSortQueue:
			return rows[i].q > rows[j].q
		}
		return false
	})

	cols, widths, getCell := m.wireColumnLayout()
	var b strings.Builder
	b.WriteString(fmt.Sprintf(" Wire stats — window: %s — sort: %s\n\n",
		wireWindowLabel(m.wireWindow), wireSortLabel(m.wireSort)))
	b.WriteString(styleColHead.Render(formatRow(cols, widths)))
	b.WriteString("\n")
	var totalSends, totalRecvs, totalDrops uint64
	var totalSendsRate, totalRecvsRate, totalDropsRate float64
	var maxQ, maxQMax uint32
	for _, r := range rows {
		totalSends += r.sends
		totalRecvs += r.recvs
		totalDrops += r.drops
		totalSendsRate += r.sendsRate
		totalRecvsRate += r.recvsRate
		totalDropsRate += r.dropsRate
		if r.q > maxQ {
			maxQ = r.q
		}
		if r.qmax > maxQMax {
			maxQMax = r.qmax
		}
		cells := make([]string, len(cols))
		for i := range cols {
			cells[i] = getCell(i, r)
		}
		b.WriteString(formatRow(cells, widths))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf(" Total: %d sends  %d recvs  %d drops  (%s sends/s  %s recvs/s  %s drops/s)\n",
		totalSends, totalRecvs, totalDrops,
		formatRate(totalSendsRate), formatRate(totalRecvsRate), formatRate(totalDropsRate)))
	b.WriteString(fmt.Sprintf(" Outbox utilization (max q across federates): %s\n",
		renderUtilizationBar(maxQ, maxQMax)))
	if m.wireColPick {
		b.WriteString(renderWireColumnPicker(m))
	}
	return b.String()
}

// wireFilterRows applies the `/` filter to wire rows (Phase 3 — §5
// filter polish). Matches federation OR federate name substring,
// case-insensitive. Empty filter returns rows unchanged.
func (m *model) wireFilterRows(rows []wireRow) []wireRow {
	if m.filter == "" {
		return rows
	}
	needle := strings.ToLower(m.filter)
	out := rows[:0:len(rows)]
	for _, r := range rows {
		if strings.Contains(strings.ToLower(r.fed), needle) ||
			strings.Contains(strings.ToLower(r.name), needle) {
			out = append(out, r)
		}
	}
	return out
}

// formatRate renders a /s rate value with magnitude-appropriate
// precision. Sub-1.0 rates are surfaced with two decimals so
// "0.10 drops/s" doesn't snap to 0.
func formatRate(v float64) string {
	if v == 0 {
		return "0"
	}
	if v < 10 {
		return fmt.Sprintf("%.2f", v)
	}
	if v < 1000 {
		return fmt.Sprintf("%.1f", v)
	}
	return fmt.Sprintf("%.0f", v)
}

// wireColumnLayout returns the (header labels, widths, cell-getter)
// triple for the Wire view's table, honoring the column-toggle set.
// Phase 3 (commit 3) ships the layout with all columns on; commit 4
// wires the `C` column-picker to flip bits in m.wireColumns.
func (m *model) wireColumnLayout() ([]string, []int, func(int, wireRow) string) {
	defs := wireColumnDefs()
	cols := make([]string, 0, len(defs))
	widths := make([]int, 0, len(defs))
	cellFns := make([]func(wireRow) string, 0, len(defs))
	for _, d := range defs {
		if !m.wireColumns.has(d.id) {
			continue
		}
		cols = append(cols, d.label)
		widths = append(widths, d.width)
		cellFns = append(cellFns, d.cell)
	}
	getCell := func(i int, r wireRow) string {
		return cellFns[i](r)
	}
	return cols, widths, getCell
}

// renderWireColumnPicker is the body for the C-key column-toggle
// popup (Phase 3 — added in commit 4). Renders the current toggle
// state with a highlighted cursor; the popup state machine lives
// in model.go.
func renderWireColumnPicker(m *model) string {
	defs := wireColumnDefs()
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(styleColHead.Render(" Toggle columns") + "\n")
	for i, d := range defs {
		mark := "[ ]"
		if m.wireColumns.has(d.id) {
			mark = "[x]"
		}
		line := fmt.Sprintf("   %s %s", mark, d.label)
		if i == m.wireColIdx {
			line = styleSelectedRow.Render("▶ " + line)
		} else {
			line = "  " + line
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	b.WriteString(styleDim.Render("   space toggle  enter close  esc close\n"))
	return b.String()
}

// renderSparkline draws an 8-level ASCII sparkline of the given
// samples normalised to their min/max range. Empty input returns a
// faint placeholder.
func renderSparkline(vals []float64) string {
	if len(vals) == 0 {
		return styleDim.Render("(no samples yet)")
	}
	const blocks = "▁▂▃▄▅▆▇█"
	min, max := vals[0], vals[0]
	for _, v := range vals[1:] {
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}
	span := max - min
	var b strings.Builder
	for _, v := range vals {
		var idx int
		if span <= 0 {
			idx = 0
		} else {
			idx = int(((v - min) / span) * float64(len(blocks)-1))
			if idx < 0 {
				idx = 0
			}
			if idx >= len(blocks) {
				idx = len(blocks) - 1
			}
		}
		b.WriteRune(rune(blocks[idx]))
	}
	return b.String()
}

// renderUtilizationBar draws a fixed-width 8-segment bar representing
// q/qmax. Used by the Wire view's footer.
func renderUtilizationBar(q, qmax uint32) string {
	if qmax == 0 {
		return styleDim.Render("n/a")
	}
	pct := float64(q) / float64(qmax)
	if pct > 1 {
		pct = 1
	}
	const segs = 8
	on := int(pct * float64(segs))
	bar := strings.Repeat("█", on) + strings.Repeat("░", segs-on)
	return fmt.Sprintf("%s %d%%", bar, int(pct*100))
}

// --- Events view (§3.5) -----------------------------------------------------

// renderEventsView renders the live tail of TailedEvent lines for the
// selected federation. Phase 4 surfaces the active server-side
// filter + a "renderer-lag" line whenever the server reported any
// overflow_skipped events.
func (m *model) renderEventsView() string {
	if m.events == nil {
		return styleDim.Render("  (events stream not running — press I to start)")
	}
	var b strings.Builder
	pause := ""
	if m.events.paused {
		pause = " [PAUSED]"
	}
	filterNote := ""
	if m.filter != "" {
		filterNote = fmt.Sprintf("  filter=%q (server-side)", m.filter)
	}
	b.WriteString(fmt.Sprintf(" Event log — federation: %s — tail%s%s\n",
		m.events.fed, pause, filterNote))
	m.events.mu.Lock()
	defer m.events.mu.Unlock()
	if m.events.dropped > 0 {
		b.WriteString(styleErr.Render(fmt.Sprintf(
			" %s events dropped due to renderer lag\n",
			humanCount(m.events.dropped),
		)))
	}
	b.WriteString("\n")
	// Phase 4: server-side filter is now applied at the source, so the
	// rti-top side just shows what arrived. We keep a thin client-side
	// substring check as a fallback in case the server returned older
	// events from before the filter was set.
	lines := m.events.lines
	if len(lines) == 0 {
		b.WriteString(styleDim.Render("   (waiting for events …)\n"))
	} else {
		// Show the last N lines that fit in the body height. The body
		// is height ~ m.height-6 (header 2 lines + footer 1 line + a bit).
		// Use a conservative cap.
		visible := m.height - 9
		if visible < 5 {
			visible = 5
		}
		from := 0
		if len(lines) > visible {
			from = len(lines) - visible
		}
		for _, ln := range lines[from:] {
			b.WriteString("  ")
			b.WriteString(ln)
			b.WriteString("\n")
		}
	}
	b.WriteString("\n")
	b.WriteString(styleDim.Render(
		"   Phase 4: server-side class filter via F-key + batched delivery.\n"))
	return b.String()
}

// humanCount formats large counts with K/M/G suffixes for the
// renderer-lag status line ("3.2k events dropped …"). Picked over
// raw integers so the line stays a fixed length on bursty federations.
func humanCount(n uint64) string {
	switch {
	case n < 1000:
		return fmt.Sprintf("%d", n)
	case n < 1_000_000:
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	case n < 1_000_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	default:
		return fmt.Sprintf("%.1fG", float64(n)/1_000_000_000)
	}
}

// --- helpers ----------------------------------------------------------------

func formatRow(cols []string, widths []int) string {
	parts := make([]string, len(cols))
	for i, c := range cols {
		w := widths[i]
		if i < len(widths)-1 {
			parts[i] = padRight(c, w)
		} else {
			parts[i] = c
		}
	}
	return strings.Join(parts, " ")
}

func padRight(s string, w int) string {
	if len(s) >= w {
		return s[:w]
	}
	return s + strings.Repeat(" ", w-len(s))
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func humanDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh%dm", h, m)
	}
	return fmt.Sprintf("%dm%ds", m, s)
}

func modeString(m rtiv1.Mode) string {
	switch m {
	case rtiv1.Mode_MODE_VERBOSE:
		return "verbose"
	case rtiv1.Mode_MODE_BEST_EFFORT:
		return "best-effort"
	}
	return "unspecified"
}

func roleString(f *rtiv1.FederateSnapshot) string {
	r := f.GetRegulating()
	c := f.GetConstrained()
	switch {
	case r && c:
		return "reg+const"
	case r:
		return "regulator"
	case c:
		return "constrained"
	}
	return "observer"
}

// aggregateTPS sums updates+interactions/sec across federates as a
// rough TPS proxy. Phase 2 does not yet have a rate window — we
// surface cumulative totals as a coarse activity indicator. (A
// proper rate window is documented as a Phase-3 follow-up in
// rti-top's README.)
func aggregateTPS(fed *rtiv1.FederationSnapshot) float64 {
	var sum uint64
	for _, f := range fed.GetFederates() {
		sum += f.GetUpdatesSent() + f.GetInteractionsSent()
	}
	return float64(sum)
}

func tpsForFederate(f *rtiv1.FederateSnapshot) string {
	return fmt.Sprintf("%d", f.GetUpdatesSent()+f.GetInteractionsSent())
}

func formatLBTS(v float64) string {
	if v > 1e308 {
		return "+Inf"
	}
	return fmt.Sprintf("%.2f", v)
}

func formatPending(p []*rtiv1.PendingGrant) string {
	if len(p) == 0 {
		return "(none)"
	}
	parts := make([]string, len(p))
	for i, g := range p {
		parts[i] = fmt.Sprintf("h=%d @ %.2f", g.GetFederateHandle(), g.GetRequestedTime())
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func formatPending1(f *rtiv1.FederateSnapshot) string {
	if f.PendingRequestTime == nil {
		return "—"
	}
	return fmt.Sprintf("%.2f", *f.PendingRequestTime)
}

func formatContribution(f *rtiv1.FederateSnapshot) string {
	if !f.GetRegulating() {
		return "—"
	}
	return fmt.Sprintf("%.2f", f.GetCurrentTime()+f.GetLookahead())
}

func formatSyncPoints(sps []*rtiv1.SyncPointSnapshot) string {
	if len(sps) == 0 {
		return "(none)"
	}
	parts := make([]string, len(sps))
	for i, s := range sps {
		state := "announced"
		if s.GetState() == rtiv1.SyncPointState_SYNC_POINT_STATE_ACHIEVED {
			state = "✓ achieved"
		}
		parts[i] = fmt.Sprintf("%s: %s", s.GetLabel(), state)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func saveStateLabel(s rtiv1.SaveState) string {
	switch s {
	case rtiv1.SaveState_SAVE_STATE_IDLE:
		return "IDLE"
	case rtiv1.SaveState_SAVE_STATE_INITIATED:
		return "INITIATED"
	case rtiv1.SaveState_SAVE_STATE_SAVED:
		return "SAVED"
	case rtiv1.SaveState_SAVE_STATE_NOT_SAVED:
		return "NOT_SAVED"
	}
	return "UNSPECIFIED"
}

func restoreStateLabel(s rtiv1.RestoreState) string {
	switch s {
	case rtiv1.RestoreState_RESTORE_STATE_IDLE:
		return "IDLE"
	case rtiv1.RestoreState_RESTORE_STATE_LOADING:
		return "LOADING"
	case rtiv1.RestoreState_RESTORE_STATE_INITIATED:
		return "INITIATED"
	case rtiv1.RestoreState_RESTORE_STATE_COMPLETED:
		return "COMPLETED"
	case rtiv1.RestoreState_RESTORE_STATE_FAILED:
		return "FAILED"
	}
	return "UNSPECIFIED"
}

func syncStateLabel(s rtiv1.SyncPointState) string {
	switch s {
	case rtiv1.SyncPointState_SYNC_POINT_STATE_ANNOUNCED:
		return "announced"
	case rtiv1.SyncPointState_SYNC_POINT_STATE_ACHIEVED:
		return "achieved"
	}
	return "unspecified"
}

func regionSuffix(n uint32) string {
	if n == 0 {
		return " (no DDM activity)"
	}
	return ""
}

func joinHandles(hs []uint64) string {
	if len(hs) == 0 {
		return "(none)"
	}
	parts := make([]string, len(hs))
	for i, h := range hs {
		parts[i] = fmt.Sprintf("%d", h)
	}
	return strings.Join(parts, ", ")
}

// filterFederates returns the subset of feds whose name matches the
// given substring filter (case-insensitive). Empty filter returns
// the input unchanged. rtid-TUI Phase 3 — §5 filter polish; used by
// the drilldown + federate-detail views.
func filterFederates(feds []*rtiv1.FederateSnapshot, filter string) []*rtiv1.FederateSnapshot {
	if filter == "" {
		return feds
	}
	needle := strings.ToLower(filter)
	out := make([]*rtiv1.FederateSnapshot, 0, len(feds))
	for _, f := range feds {
		if strings.Contains(strings.ToLower(f.GetName()), needle) {
			out = append(out, f)
		}
	}
	return out
}

// formatAge renders a federate's age (now - joinUnix) for the
// drilldown view's `age` column. rtid-TUI Phase 3 — replaces the
// Phase-2 "-" placeholder.
//
// Output format scales with magnitude (`5s`, `12m3s`, `2h15m`,
// `3d4h`) — chosen to fit the column's 6-char width while still
// surfacing the dominant unit. A zero joinUnix means the daemon
// did not record a join time (legacy data path or pre-Phase-3
// rtid) → render `-` so the row still aligns.
func formatAge(joinUnix int64, now time.Time) string {
	if joinUnix <= 0 {
		return "-"
	}
	d := now.Sub(time.Unix(joinUnix, 0))
	if d < 0 {
		// Clock skew between rtid and rti-top — surface as 0s rather
		// than confusing the operator with a negative duration.
		return "0s"
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		m := int(d.Minutes())
		s := int(d.Seconds()) % 60
		return fmt.Sprintf("%dm%ds", m, s)
	case d < 24*time.Hour:
		h := int(d.Hours())
		m := int(d.Minutes()) % 60
		return fmt.Sprintf("%dh%dm", h, m)
	default:
		days := int(d / (24 * time.Hour))
		h := int(d.Hours()) % 24
		return fmt.Sprintf("%dd%dh", days, h)
	}
}
