// mutating.go — Phase 5 of docs/rtid-tui.md. The X (ForceResign) and
// D (DestroyFederation) keybindings open a confirmation overlay; on
// confirmation the model dispatches the corresponding RPC against
// MutatingService.
//
// Read-only mode is still the default — these handlers are no-ops
// until the daemon's MutatingService probe succeeds (mutatingEnabled
// is set in initialModel). The daemon registers MutatingService only
// when --admin-mutating=true (docs/rtid-tui.md §7.5 + the rtid
// composition root commit).

package main

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

// confirmKind enumerates the active confirmation overlay (or absence
// thereof). 0 (= confirmNone) is the unconfirmed default.
type confirmKind int

const (
	confirmNone              confirmKind = iota
	confirmForceResign                   // X — federate eviction
	confirmDestroyFederation             // D — federation destruction
)

// mutatingResultMsg is dispatched back into the MVU loop after a
// MutatingService RPC completes. Drives the status-line update.
type mutatingResultMsg struct {
	kind    confirmKind
	ok      bool
	summary string
	err     error
}

// startForceResign opens the confirmation overlay for ForceResign on
// the currently-selected federate. Refuses to open when:
//   - MutatingService is not registered (mutatingEnabled=false), OR
//   - we're not on a view where a federate is selected, OR
//   - the snapshot has no federate at the current selection.
func (m *model) startForceResign() (tea.Model, tea.Cmd) {
	if !m.mutatingEnabled {
		return m, nil
	}
	if m.view != viewDrilldown && m.view != viewFederateDetail {
		return m, nil
	}
	fed := m.findFederation(m.selFed)
	if fed == nil {
		return m, nil
	}
	feds := fed.GetFederates()
	if m.view == viewDrilldown {
		feds = filterFederates(feds, m.filter)
	}
	if m.federate < 0 || m.federate >= len(feds) {
		return m, nil
	}
	target := feds[m.federate]
	m.confirmKind = confirmForceResign
	m.confirmTarget = target.GetHandle()
	m.destroyFedConfirmCount = 0
	return m, nil
}

// startDestroyFederation opens the confirmation overlay for
// DestroyFederation on the currently-selected federation. Refuses
// when MutatingService is not registered or no federation is
// selected.
func (m *model) startDestroyFederation() (tea.Model, tea.Cmd) {
	if !m.mutatingEnabled {
		return m, nil
	}
	target := m.selFed
	if target == "" {
		feds := m.filteredFederations()
		if len(feds) == 0 {
			return m, nil
		}
		idx := m.fedIdx
		if idx >= len(feds) {
			idx = len(feds) - 1
		}
		target = feds[idx].GetName()
	}
	m.selFed = target
	m.confirmKind = confirmDestroyFederation
	m.confirmTarget = 0
	m.destroyFedConfirmCount = 0
	return m, nil
}

// handleConfirmKey services keys while the confirmation overlay is
// open. `y` advances; for ForceResign one `y` is sufficient; for
// DestroyFederation a second `y` is required (double-safety per
// docs/rtid-tui.md §7.5). Anything else (Esc, `n`, etc.) cancels.
func (m *model) handleConfirmKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "y", "Y":
		switch m.confirmKind {
		case confirmForceResign:
			cmd := m.dispatchForceResign(m.selFed, m.confirmTarget)
			m.confirmKind = confirmNone
			m.confirmTarget = 0
			return m, cmd
		case confirmDestroyFederation:
			m.destroyFedConfirmCount++
			if m.destroyFedConfirmCount >= 2 {
				cmd := m.dispatchDestroyFederation(m.selFed)
				m.confirmKind = confirmNone
				m.destroyFedConfirmCount = 0
				return m, cmd
			}
			return m, nil
		}
	case "n", "N", "esc", "ctrl+c":
		m.confirmKind = confirmNone
		m.confirmTarget = 0
		m.destroyFedConfirmCount = 0
		m.statusMsg = "cancelled"
		return m, nil
	}
	// Any other key is treated as "I haven't confirmed yet"; reset
	// the destroy-fed counter so the user can't half-confirm and
	// then mash other keys.
	m.destroyFedConfirmCount = 0
	return m, nil
}

// dispatchForceResign returns a tea.Cmd that calls
// MutatingService.ForceResign and dispatches mutatingResultMsg back.
func (m *model) dispatchForceResign(federation string, handle uint64) tea.Cmd {
	cli := m.cli
	ctx := m.pollCtx
	return func() tea.Msg {
		resp, err := cli.ForceResign(ctx, federation, handle)
		if err != nil {
			return mutatingResultMsg{
				kind:    confirmForceResign,
				err:     err,
				summary: fmt.Sprintf("ForceResign(%s, %d) FAILED: %v", federation, handle, err),
			}
		}
		state := "evicted"
		if resp.GetAlreadyResigned() {
			state = "already-resigned"
		}
		return mutatingResultMsg{
			kind:    confirmForceResign,
			ok:      true,
			summary: fmt.Sprintf("ForceResign(%s, %d): %s", federation, handle, state),
		}
	}
}

// dispatchDestroyFederation returns a tea.Cmd that calls
// MutatingService.DestroyFederation with evict_joined_federates=true
// (the operator already double-confirmed; refusing on joined
// federates would surface as a useless second roundtrip).
func (m *model) dispatchDestroyFederation(federation string) tea.Cmd {
	cli := m.cli
	ctx := m.pollCtx
	return func() tea.Msg {
		resp, err := cli.DestroyFederation(ctx, federation, true)
		if err != nil {
			return mutatingResultMsg{
				kind:    confirmDestroyFederation,
				err:     err,
				summary: fmt.Sprintf("DestroyFederation(%s) FAILED: %v", federation, err),
			}
		}
		evicted := len(resp.GetEvictedHandles())
		return mutatingResultMsg{
			kind:    confirmDestroyFederation,
			ok:      true,
			summary: fmt.Sprintf("DestroyFederation(%s): evicted %d federate(s) + destroyed", federation, evicted),
		}
	}
}

// handleMutatingResult writes the operator-facing summary into
// statusMsg so the next Update tick renders it under the header.
func (m *model) handleMutatingResult(msg mutatingResultMsg) (tea.Model, tea.Cmd) {
	m.statusMsg = msg.summary
	return m, nil
}
