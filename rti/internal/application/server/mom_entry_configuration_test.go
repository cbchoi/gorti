package server

import "testing"

func TestMOMEntryAccountingRequiresEntryObserver(t *testing.T) {
	observer, receipts, err := newMOMRuntimeObservers(momObservationDependencies{DataCallbacksCountedAtEntry: true})
	if err == nil || observer != nil || receipts != nil {
		t.Fatalf("entry accounting without observer: observer=%v receipts=%v err=%v", observer, receipts, err)
	}
}
