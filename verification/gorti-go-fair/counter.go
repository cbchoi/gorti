package main

const nanosecondsPerSecond int64 = 1_000_000_000

type counterStamp int64

func (stamp counterStamp) After(other counterStamp) bool { return stamp > other }

func counterElapsed(start counterStamp) int64 {
	return counterBetween(start, counterNow())
}

func counterBetween(start, end counterStamp) int64 {
	delta := int64(end - start)
	if delta <= 0 {
		return 0
	}
	frequency := performanceCounterFrequency()
	seconds := delta / frequency
	remainder := delta % frequency
	return seconds*nanosecondsPerSecond + remainder*nanosecondsPerSecond/frequency
}
