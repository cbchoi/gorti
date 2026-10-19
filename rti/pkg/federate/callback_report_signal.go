package federate

import "sync/atomic"

type callbackReportWaiter struct{ done chan struct{} }

var finishedCallbackReportWaiter = func() *callbackReportWaiter {
	w := &callbackReportWaiter{done: make(chan struct{})}
	close(w.done)
	return w
}()

// A one-shot signal allocates only if somebody waits before completion. The
// terminal pointer also publishes preceding writes to callers arriving later.
type callbackReportSignal struct {
	state atomic.Pointer[callbackReportWaiter]
}

func (s *callbackReportSignal) wait() <-chan struct{} {
	if w := s.state.Load(); w != nil {
		return w.done
	}
	w := &callbackReportWaiter{done: make(chan struct{})}
	if s.state.CompareAndSwap(nil, w) {
		return w.done
	}
	return s.state.Load().done
}

func (s *callbackReportSignal) finish() {
	if w := s.state.Swap(finishedCallbackReportWaiter); w != nil && w != finishedCallbackReportWaiter {
		close(w.done)
	}
}
