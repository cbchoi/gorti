package federate

import (
	"bytes"
	"testing"
)

func TestCallbackCompletedPayloadReleasedWithoutLosingRecovery(t *testing.T) {
	f := runtimeTestFederate(t, HLAfloat64Time)
	event := reportTestData()
	key := genericReceipt(event)
	f.retainCallbackOutcome(event, false, testApplicationFailure, false)
	if f.genericCallbacks[key].event == nil {
		t.Fatal("unacknowledged outcome lost its retry event")
	}
	f.retainCallbackOutcome(event, false, testApplicationFailure, true)
	entry := f.genericCallbacks[key]
	if entry.event != nil {
		t.Fatal("acknowledged result retains the application payload")
	}
	state := runtimeTestState(t, f)
	if len(state.PendingOutcomes) != 1 || state.PendingOutcomes[0].Success || state.PendingOutcomes[0].Exception != testApplicationFailure {
		t.Fatalf("checkpoint lost completed result: %+v", state.PendingOutcomes)
	}
	// Identical authorization must not reopen a reported recovery result.
	same := callbackOutcomeRecovery{CallbackInfo{receipt: make([]byte, 32), identity: key, outcomeOnly: true}}
	if _, err := f.recoveredOutcome(same); err != nil {
		t.Fatal(err)
	}
	if !entry.reported || entry.event != nil {
		t.Fatal("duplicate recovery carrier reopened completion")
	}
	fresh := callbackOutcomeRecovery{CallbackInfo{receipt: bytes.Repeat([]byte{1}, 32), identity: key, outcomeOnly: true}}
	outcome, err := f.recoveredOutcome(fresh)
	if err != nil || outcome.Success || outcome.Exception != testApplicationFailure {
		t.Fatalf("fresh authorization lost saved failure: %+v %v", outcome, err)
	}
	if entry.reported || entry.event == nil || len(f.genericCallbackCompleted) != 0 {
		t.Fatal("new recovery receipt did not retain retry state")
	}
}

func TestCallbackCompletedPayloadWaitsForLocalEntryBookkeeping(t *testing.T) {
	f := &Federate{}
	event := reportTestData()
	key := genericReceipt(event)
	f.genericCallbacks = map[string]*genericCallbackInvocation{key: {event: event, localEntry: true}}
	f.retainCallbackOutcome(event, true, "", true)
	if f.genericCallbacks[key].event == nil {
		t.Fatal("entry retry state was released before entry bookkeeping")
	}
}
