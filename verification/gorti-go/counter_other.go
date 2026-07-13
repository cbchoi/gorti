//go:build !windows

package main

import "time"

var fallbackCounterOrigin = time.Now()

func performanceCounterFrequency() int64 {
	return nanosecondsPerSecond
}

func counterNow() counterStamp {
	return counterStamp(time.Since(fallbackCounterOrigin).Nanoseconds())
}
