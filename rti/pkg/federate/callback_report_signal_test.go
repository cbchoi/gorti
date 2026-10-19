package federate

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestCallbackReportSignalWaitersAndCompletion(t *testing.T) {
	var signal callbackReportSignal
	first := signal.wait()
	if signal.wait() != first {
		t.Fatal("pending waiters did not share a notification")
	}
	select {
	case <-first:
		t.Fatal("unfinished report signaled completion")
	default:
	}
	signal.finish()
	signal.finish()
	for _, done := range []<-chan struct{}{first, signal.wait()} {
		select {
		case <-done:
		default:
			t.Fatal("completion lost a preceding or subsequent waiter")
		}
	}
}

func TestCallbackReportSignalConcurrentPublication(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for round := 0; round < 100; round++ {
		var signal callbackReportSignal
		var value int
		start := make(chan struct{})
		results := make(chan int, 16)
		var workers sync.WaitGroup
		for i := 0; i < cap(results); i++ {
			workers.Add(1)
			go func() {
				defer workers.Done()
				<-start
				select {
				case <-signal.wait():
					results <- value
				case <-ctx.Done():
					results <- -1
				}
			}()
		}
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			value = 42
			signal.finish()
		}()
		close(start)
		workers.Wait()
		for range cap(results) {
			if got := <-results; got != 42 {
				t.Fatalf("round %d: lost wakeup or unpublished state: %d", round, got)
			}
		}
		if signal.wait() != finishedCallbackReportWaiter.done {
			t.Fatal("late waiter allocated instead of observing terminal state")
		}
	}
}

func TestCallbackReportSignalCompletedWithoutWaitersAllocatesNothing(t *testing.T) {
	var signal callbackReportSignal
	allocs := testing.AllocsPerRun(1000, func() {
		// Only the test resets a quiescent signal; production tasks are one-shot.
		signal.state.Store(nil)
		signal.finish()
		<-signal.wait()
	})
	if allocs != 0 {
		t.Fatalf("unobserved completion allocated %.0f objects", allocs)
	}
}
