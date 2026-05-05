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
		keys += "  " + styleErr.Render("⚠ "+m.lastErr.Error())
	}
	return styleHeader.Width(m.width).Render(title) + "\n" + keys
}

// renderFooter draws the bottom keybinding hint row matching the
// design-doc mockups (§3.1..§3.5 each show their own footer).
func (m *model) renderFooter() string {
	var hint string
	switch m.view {
	case viewFederations:
		hint = " ↑↓ select  Enter drill-down  R refresh-rate  / filter  Q quit "
	case viewDrilldown:
		hint = " Esc back  ↑↓ select federate  Enter inspect  T time view  W wire view  I events "
	case viewFederateDetail:
		hint = " Esc back  T time view  W wire view  I events  Q quit "
	}
	if m.filtering {
		hint = fmt.Sprintf(" filter: %s_  (Enter accept  Esc cancel) ", m.filter)
	}
	return styleFooter.Width(m.width).Render(hint)
}

// renderBody dispatches to the per-view body renderer. Commit 2 has
// only the first three views populated; T / W / I-bound views fall
// back to a "coming soon" placeholder until commits 3 + 4 land.
func (m *model) renderBody() string {
	switch m.view {
	case viewFederations:
		return m.renderFederationsView()
	case viewDrilldown:
		return m.renderDrilldownView()
	case viewFederateDetail:
		return m.renderFederateDetailView()
	}
	return styleDim.Render("  (view not yet implemented in this commit)")
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
	feds := fed.GetFederates()
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
			"-", // age — not on the wire in Phase 1; documented in README.
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
