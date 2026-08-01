// events.go — the Events view's tail-of-event-log subscription.
//
// On entering the Events view (key `I`) the model spawns a single
// background goroutine that calls AdminService.TailEvents on the
// selected federation and forwards each TailEventsResponse onto the
// bubbletea program as eventTailMsg. Esc / view-switch cancels the
// stream context and closes the goroutine.
//
// Phase-1 limitation (PINNED in docs/rtid-tui.md §4): the
// TailEventsResponse `payload` field is currently empty — the server
// surfaces only `seq`. The view shows seq + timestamp to the extent
// the proto carries them; richer per-event content (class / sender /
// payload bytes) is a Phase-3 follow-up.

package main

import (
	"context"
	"fmt"
	"io"
	"sync"

	tea "github.com/charmbracelet/bubbletea"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
)

// eventLineCap caps the in-memory tail buffer at 500 lines — the
// view scrolls; older lines roll off.
const eventLineCap = 500

// eventsState is the per-Events-view subscription book-keeping.
type eventsState struct {
	mu       sync.Mutex
	fed      string
	cancel   context.CancelFunc
	lines    []string
	scroll   int
	paused   bool
	finished bool
	// dropped tracks the cumulative count of events the server
	// reported as overflow_skipped (the renderer was too slow to keep
	// up). Surfaced in the status line.
	dropped uint64
}

// eventTailMsg is dispatched by the streaming goroutine on each
// received TailEventsResponse (or stream end). Phase 4: a single
// response may carry multiple lines (batched) plus an overflow
// counter — the message therefore carries a slice of lines and the
// per-batch dropped count.
type eventTailMsg struct {
	lines   []string
	dropped uint64
	end     bool
	err     error
}

// programSender is the type of *tea.Program.Send injected from
// runTUI so the event-stream goroutine can push messages back
// without owning the model.
type programSender func(tea.Msg)

// senderHolder holds the program sender once runTUI installs it.
// Package-level so the streaming goroutine doesn't need to thread
// the *tea.Program through a long call chain.
var senderHolder struct {
	mu     sync.Mutex
	sender programSender
}

func setProgramSender(s programSender) {
	senderHolder.mu.Lock()
	senderHolder.sender = s
	senderHolder.mu.Unlock()
}

func send(msg tea.Msg) {
	senderHolder.mu.Lock()
	s := senderHolder.sender
	senderHolder.mu.Unlock()
	if s != nil {
		s(msg)
	}
}

// enterEventsView opens TailEvents for the currently-selected
// federation. If no federation is selected (we're on the Federations
// list) we use the highlighted row.
func (m *model) enterEventsView() (tea.Model, tea.Cmd) {
	feds := m.filteredFederations()
	if len(feds) == 0 {
		return m, nil
	}
	target := m.selFed
	if target == "" {
		idx := m.fedIdx
		if idx >= len(feds) {
			idx = len(feds) - 1
		}
		target = feds[idx].GetName()
	}
	m.stopEventsLocked()

	ctx, cancel := context.WithCancel(m.pollCtx)
	es := &eventsState{
		fed:    target,
		cancel: cancel,
	}
	m.events = es
	m.eventsOn = true
	m.view = viewEvents
	return m, m.startEventsCmd(ctx, target)
}

// startEventsCmd returns a tea.Cmd that opens the TailEvents stream
// and launches the forwarding goroutine. The goroutine pushes
// eventTailMsg into the program via the package-level senderHolder.
//
// Phase 4: the server-side TailEvents handler now batches events and
// piggybacks an overflow counter when it had to drop frames due to
// renderer lag. The forwarding goroutine forwards the entire batch
// in one message + threads the counter into the status line.
func (m *model) startEventsCmd(ctx context.Context, target string) tea.Cmd {
	cli := m.cli
	classFilter := m.filter // current filter substring is the server-side class filter
	return func() tea.Msg {
		stream, err := cli.TailEventsFiltered(ctx, target, classFilter, nil)
		if err != nil {
			return eventTailMsg{err: err, end: true}
		}
		go func() {
			for {
				if cerr := ctx.Err(); cerr != nil {
					send(eventTailMsg{end: true, err: cerr})
					return
				}
				resp, rerr := stream.Recv()
				if rerr == io.EOF {
					send(eventTailMsg{end: true})
					return
				}
				if rerr != nil {
					send(eventTailMsg{end: true, err: rerr})
					return
				}
				batch := resp.GetEvents()
				lines := make([]string, 0, len(batch))
				for _, e := range batch {
					lines = append(lines, formatEvent(e))
				}
				send(eventTailMsg{
					lines:   lines,
					dropped: resp.GetOverflowSkipped(),
				})
			}
		}()
		return nil
	}
}

// formatEvent renders one TailedEvent as a single TUI line. Phase 4
// surfaces the event class + federate handle now that the server
// classifies the body.
func formatEvent(e *rtiv1.TailedEvent) string {
	class := e.GetEventClass()
	if class == "" {
		class = "?"
	}
	if h := e.GetFederateHandle(); h != 0 {
		return fmt.Sprintf("seq=%-6d %-22s fed=%d  payload_bytes=%d",
			e.GetSeq(), class, h, len(e.GetPayload()))
	}
	return fmt.Sprintf("seq=%-6d %-22s         payload_bytes=%d",
		e.GetSeq(), class, len(e.GetPayload()))
}

// handleEventMsg appends a streamed batch of lines to the events
// state and folds the per-batch overflow counter into the running
// dropped total surfaced in the status line.
func (m *model) handleEventMsg(msg eventTailMsg) (tea.Model, tea.Cmd) {
	if m.events == nil {
		return m, nil
	}
	m.events.mu.Lock()
	defer m.events.mu.Unlock()
	if msg.end {
		m.events.finished = true
		if msg.err != nil && !isCanceled(msg.err) {
			m.events.lines = append(m.events.lines,
				fmt.Sprintf("[stream ended: %v]", msg.err))
		} else {
			m.events.lines = append(m.events.lines, "[stream ended]")
		}
		return m, nil
	}
	if msg.dropped > 0 {
		m.events.dropped += msg.dropped
	}
	if !m.events.paused && len(msg.lines) > 0 {
		m.events.lines = append(m.events.lines, msg.lines...)
		if len(m.events.lines) > eventLineCap {
			drop := len(m.events.lines) - eventLineCap
			m.events.lines = m.events.lines[drop:]
		}
	}
	return m, nil
}

// stopEventsLocked tears down any running TailEvents stream. Safe to
// call when no stream is active.
func (m *model) stopEventsLocked() {
	if m.events != nil && m.events.cancel != nil {
		m.events.cancel()
	}
	m.events = nil
	m.eventsOn = false
}

// isCanceled reports whether err is the standard context-cancel
// sentinel (the expected exit path when the user navigates away).
func isCanceled(err error) bool {
	if err == nil {
		return false
	}
	return err == context.Canceled || err == context.DeadlineExceeded
}
