package federate

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/adapters/delivery/callbackprofile"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
)

func TestCallbackReportCompletionIsOrderedWithoutBlockingACKs(t *testing.T) {
	if !callbackprofile.Async {
		t.Skip("async completion worker")
	}
	f, ctx := newSDKReportFixture(t, &sdkReportServer{report: func(context.Context, *rtiv1.ReportCallbackInvocationRequest) error { return nil }}, true)
	q := f.callbackReportTransport()
	release, firstStarted, secondStarted := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	first, err := q.enqueue(ctx, &rtiv1.ReportCallbackInvocationRequest{}, func(error) { close(firstStarted); <-release })
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-firstStarted:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	second, err := q.enqueue(ctx, &rtiv1.ReportCallbackInvocationRequest{}, func(error) { close(secondStarted) })
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-second.ack.wait():
	case <-ctx.Done():
		t.Fatal("bookkeeping blocked wire ACK progress")
	}
	select {
	case <-secondStarted:
		t.Fatal("completion overtook preceding bookkeeping")
	case <-time.After(30 * time.Millisecond):
	}
	unblock()
	for _, task := range []*callbackReportTask{first, second} {
		select {
		case <-task.done.wait():
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
}

func TestCallbackReportCompletionCapacityPreservesControlProgress(t *testing.T) {
	if !callbackprofile.Async {
		t.Skip("async completion worker")
	}
	f, ctx := newSDKReportFixture(t, &sdkReportServer{report: func(context.Context, *rtiv1.ReportCallbackInvocationRequest) error { return nil }}, true)
	q := f.callbackReportTransport()
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	for i := 0; i < callbackReportQueueLimit; i++ {
		task, err := q.enqueue(ctx, &rtiv1.ReportCallbackInvocationRequest{}, func(error) { <-release })
		if err != nil {
			t.Fatal(err)
		}
		select {
		case <-task.ack.wait():
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	short, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()
	if _, err := q.enqueue(short, &rtiv1.ReportCallbackInvocationRequest{}, func(error) {}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("acknowledged but unfinished work escaped the capacity bound: %v", err)
	}
	control, err := q.enqueue(ctx, &rtiv1.ReportCallbackInvocationRequest{InvocationEntry: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-control.done.wait():
		if control.err != nil {
			t.Fatal(control.err)
		}
	case <-ctx.Done():
		t.Fatal("completion pressure blocked a confirmed control report")
	}
	unblock()
	if err := f.flushLocalCallbackEntries(ctx); err != nil {
		t.Fatal(err)
	}
}
