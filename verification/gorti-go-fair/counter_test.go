package main

import (
	"testing"
	"time"
)

func TestCounterDoesNotRegress(t *testing.T) {
	previous := counterNow()
	for range 10_000 {
		current := counterNow()
		if current < previous {
			t.Fatalf("counter regressed from %d to %d", previous, current)
		}
		previous = current
	}
}

func TestCounterResolvesBelowOneHundredMicroseconds(t *testing.T) {
	minimum := int64(time.Second)
	previous := counterNow()
	for range 10_000 {
		current := counterNow()
		elapsed := counterBetween(previous, current)
		if elapsed > 0 && elapsed < minimum {
			minimum = elapsed
		}
		previous = current
	}
	if minimum >= int64(100*time.Microsecond) {
		t.Fatalf("minimum positive counter delta = %d ns", minimum)
	}
}
