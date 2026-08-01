package main

import (
	"testing"
	"time"
)

func TestCounterConversionPreservesOneSecond(t *testing.T) {
	frequency := performanceCounterFrequency()
	if frequency <= 0 {
		t.Fatalf("counter frequency = %d", frequency)
	}
	if got := elapsedCounterNS(100, counterStamp(100+frequency)); got != nanosecondsPerSecond {
		t.Fatalf("one counter second = %d ns, want %d", got, nanosecondsPerSecond)
	}
}

func TestCounterDoesNotRegress(t *testing.T) {
	previous := counterNow()
	for index := 0; index < 10_000; index++ {
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
	for index := 0; index < 10_000; index++ {
		current := counterNow()
		elapsed := elapsedCounterNS(previous, current)
		if elapsed > 0 && elapsed < minimum {
			minimum = elapsed
		}
		previous = current
	}
	if minimum >= int64(100*time.Microsecond) {
		t.Fatalf("minimum positive counter delta = %d ns, want below %d ns", minimum, 100*time.Microsecond)
	}
}
