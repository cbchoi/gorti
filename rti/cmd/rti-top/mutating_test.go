// Phase 5 (docs/rtid-tui.md §7.5) — TUI keybinding tests for the
// X / D mutating-op flow. The dispatch RPCs themselves are exercised
// at the transport-grpc layer; here we verify the model surfaces /
// hides the keybindings and that the confirmation flow gates the
// dispatch correctly.

package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// teaKeyRune builds a tea.KeyMsg from a single character for the
// confirmation tests. Mirrors the pattern in views_test.go's teaKey,
// but accepts a rune so tests can poke `y` / `n` cleanly.
func teaKeyRune(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

// TestMutating_FooterHidesWhenDisabled: with mutating_enabled=false,
// neither the federations footer nor the drilldown footer mentions
// the X / D keybindings.
func TestMutating_FooterHidesWhenDisabled(t *testing.T) {
	m := newTestModel(t)
	m.mutatingEnabled = false
	for _, v := range []view{viewFederations, viewDrilldown, viewFederateDetail} {
		m.view = v
		footer := m.renderFooter()
		if strings.Contains(footer, "force-resign") || strings.Contains(footer, "destroy") {
			t.Errorf("view=%v footer should not advertise mutating keys when disabled:\n%s", v, footer)
		}
	}
}

// TestMutating_FooterShowsWhenEnabled: with mutating_enabled=true the
// drilldown footer carries both X and D keybindings.
func TestMutating_FooterShowsWhenEnabled(t *testing.T) {
	m := newTestModel(t)
	m.mutatingEnabled = true
	m.view = viewDrilldown
	m.selFed = "demo"
	footer := m.renderFooter()
	for _, want := range []string{"X force-resign", "D destroy"} {
		if !strings.Contains(footer, want) {
			t.Errorf("drilldown footer missing %q:\n%s", want, footer)
		}
	}
}

// TestMutating_StartForceResign_NoOpWhenDisabled: pressing X with
// mutating_enabled=false leaves confirmKind unset.
func TestMutating_StartForceResign_NoOpWhenDisabled(t *testing.T) {
	m := newTestModel(t)
	m.mutatingEnabled = false
	m.view = viewDrilldown
	m.selFed = "demo"
	m.federate = 0
	m.handleKey(teaKeyRune('X'))
	if m.confirmKind != confirmNone {
		t.Errorf("confirmKind: got %v want confirmNone (mutating disabled)", m.confirmKind)
	}
}

// TestMutating_StartForceResign_OpensConfirmation: in drilldown view
// with mutating enabled, X opens the ForceResign confirmation
// targeting the highlighted federate handle.
func TestMutating_StartForceResign_OpensConfirmation(t *testing.T) {
	m := newTestModel(t)
	m.mutatingEnabled = true
	m.view = viewDrilldown
	m.selFed = "demo"
	m.federate = 0
	m.handleKey(teaKeyRune('X'))
	if m.confirmKind != confirmForceResign {
		t.Fatalf("confirmKind: got %v want confirmForceResign", m.confirmKind)
	}
	if m.confirmTarget == 0 {
		t.Errorf("confirmTarget: got 0; expected first federate's handle")
	}
	footer := m.renderFooter()
	if !strings.Contains(footer, "ForceResign federate") {
		t.Errorf("footer didn't render confirmation hint:\n%s", footer)
	}
}

// TestMutating_CancelForceResign: pressing N closes the confirmation
// without dispatching.
func TestMutating_CancelForceResign(t *testing.T) {
	m := newTestModel(t)
	m.mutatingEnabled = true
	m.view = viewDrilldown
	m.selFed = "demo"
	m.federate = 0
	m.handleKey(teaKeyRune('X'))
	m.handleKey(teaKeyRune('n'))
	if m.confirmKind != confirmNone {
		t.Errorf("Cancel did not close confirmation; confirmKind=%v", m.confirmKind)
	}
	if m.statusMsg == "" {
		t.Errorf("expected statusMsg to indicate cancellation")
	}
}

// TestMutating_StartDestroyFederation_DoubleConfirm: D then y once
// keeps the overlay open (counter=1); the second y closes the
// overlay (counter clears to 0). We verify the first y bumps the
// counter — the second y dispatches the RPC, which we don't run
// through the model in this test (no live wire).
func TestMutating_StartDestroyFederation_DoubleConfirm(t *testing.T) {
	m := newTestModel(t)
	m.mutatingEnabled = true
	m.view = viewFederations
	m.fedIdx = 0
	m.handleKey(teaKeyRune('D'))
	if m.confirmKind != confirmDestroyFederation {
		t.Fatalf("confirmKind: got %v want confirmDestroyFederation", m.confirmKind)
	}
	if m.destroyFedConfirmCount != 0 {
		t.Errorf("initial destroyFedConfirmCount: got %d want 0", m.destroyFedConfirmCount)
	}
	// First y: overlay still open, counter incremented.
	m.handleKey(teaKeyRune('y'))
	if m.confirmKind != confirmDestroyFederation {
		t.Errorf("first y closed overlay early; confirmKind=%v", m.confirmKind)
	}
	if m.destroyFedConfirmCount != 1 {
		t.Errorf("after first y: counter=%d want 1", m.destroyFedConfirmCount)
	}
	// Cancel: overlay closes.
	m.handleKey(teaKeyRune('n'))
	if m.confirmKind != confirmNone {
		t.Errorf("n didn't cancel; confirmKind=%v", m.confirmKind)
	}
}

// TestMutating_OtherKeyResetsDoubleConfirmCount: typing something
// other than y/n in the middle of double-confirm resets the counter
// (so the operator can't half-confirm and then mash other keys).
func TestMutating_OtherKeyResetsDoubleConfirmCount(t *testing.T) {
	m := newTestModel(t)
	m.mutatingEnabled = true
	m.view = viewFederations
	m.fedIdx = 0
	m.handleKey(teaKeyRune('D'))
	m.handleKey(teaKeyRune('y'))
	if m.destroyFedConfirmCount != 1 {
		t.Fatalf("setup: counter=%d want 1", m.destroyFedConfirmCount)
	}
	m.handleKey(teaKeyRune('q')) // any other key
	if m.destroyFedConfirmCount != 0 {
		t.Errorf("after non-y key: counter=%d want 0 (reset)", m.destroyFedConfirmCount)
	}
	if m.confirmKind != confirmDestroyFederation {
		t.Errorf("non-y key prematurely closed confirmation; confirmKind=%v", m.confirmKind)
	}
}

// TestMutating_HandleResultMsg_UpdatesStatusLine: the result message
// dispatched by the RPC goroutine flows into statusMsg.
func TestMutating_HandleResultMsg_UpdatesStatusLine(t *testing.T) {
	m := newTestModel(t)
	m.handleMutatingResult(mutatingResultMsg{
		kind:    confirmForceResign,
		ok:      true,
		summary: "ForceResign(demo, 1): evicted",
	})
	if !strings.Contains(m.statusMsg, "ForceResign") {
		t.Errorf("statusMsg missing summary; got %q", m.statusMsg)
	}
}
