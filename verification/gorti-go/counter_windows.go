//go:build windows

package main

import (
	"fmt"
	"syscall"
	"unsafe"
)

var (
	kernel32QueryPerformanceCounter   = syscall.NewLazyDLL("kernel32.dll").NewProc("QueryPerformanceCounter")
	kernel32QueryPerformanceFrequency = syscall.NewLazyDLL("kernel32.dll").NewProc("QueryPerformanceFrequency")
	qpcFrequency                      = loadPerformanceCounterFrequency()
)

func performanceCounterFrequency() int64 {
	return qpcFrequency
}

func counterNow() counterStamp {
	var ticks int64
	succeeded, _, callErr := kernel32QueryPerformanceCounter.Call(uintptr(unsafe.Pointer(&ticks)))
	if succeeded == 0 {
		panic(fmt.Sprintf("QueryPerformanceCounter failed: %v", callErr))
	}
	return counterStamp(ticks)
}

func loadPerformanceCounterFrequency() int64 {
	var frequency int64
	succeeded, _, callErr := kernel32QueryPerformanceFrequency.Call(uintptr(unsafe.Pointer(&frequency)))
	if succeeded == 0 || frequency <= 0 {
		panic(fmt.Sprintf("QueryPerformanceFrequency failed: %v", callErr))
	}
	return frequency
}
