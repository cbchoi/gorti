package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/pkg/federate"
)

func TestCallbackDispatchCompletionWaitsForReport(t *testing.T) {
	state := newEventState(config{Role: roleSubscriber, Count: 1})
	state.beginDispatch()
	result := startBlockedEventStateWait(t, state, context.Background(), time.Second,
		"callback invocation reports", func(s *eventState) bool { return s.dispatching == 0 })
	select {
	case err := <-result:
		t.Fatalf("dispatch drain returned before report completion: %v", err)
	default:
	}
	state.endDispatch()
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("dispatch drain did not wake after report completion")
	}
}

func TestSynchronizationRegistrationCallbacks(t *testing.T) {
	for _, tc := range []struct {
		name      string
		role      role
		label     string
		wantError bool
	}{
		{"subscriber-ready", roleSubscriber, readySync, false},
		{"publisher-measure", rolePublisher, measureSync, false},
		{"publisher-start", rolePublisher, startSync, false},
		{"publisher-done", rolePublisher, doneSync, false},
		{"wrong-registrar", rolePublisher, readySync, true},
		{"unknown-label", roleSubscriber, "unknown", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := newEventState(config{Role: tc.role, Count: 1, ReceiveOrder: true, CompactSummary: true})
			state.accept(federate.SynchronizationPointRegistrationSucceeded{Label: tc.label}, counterNow())
			if got := state.failure() != nil; got != tc.wantError {
				t.Fatalf("registration failure = %v, want error %t", state.failure(), tc.wantError)
			}
			if !tc.wantError {
				if state.announced[tc.label] || state.synchronized[tc.label] {
					t.Fatal("registration callback advanced the synchronization barrier")
				}
				state.accept(federate.SynchronizationPointRegistrationSucceeded{Label: tc.label}, counterNow())
				if state.failure() == nil {
					t.Fatal("duplicate registration success accepted")
				}
			}
		})
	}
	state := newEventState(config{Role: roleSubscriber, Count: 1})
	state.accept(federate.SynchronizationPointRegistrationFailed{Label: readySync, Reason: 2}, counterNow())
	if err := state.failure(); err == nil || !strings.Contains(err.Error(), "reason=2") {
		t.Fatalf("registration failure not reported: %v", err)
	}
}
