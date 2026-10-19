package federate

import (
	"context"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
)

func TestCallbackReportEnqueueAllocatesOnlyTask(t *testing.T) {
	ctx := context.Background()
	q := &callbackReportTransport{ctx: ctx, running: true, queue: make([]*callbackReportTask, 0, 1)}
	request := &rtiv1.ReportCallbackInvocationRequest{}
	allocs := testing.AllocsPerRun(1000, func() {
		if _, err := q.enqueueReserved(ctx, request, nil); err != nil {
			t.Fatal(err)
		}
		q.queue[0] = nil
		q.queue = q.queue[:0]
	})
	if allocs > 1 {
		t.Fatalf("enqueue allocated %.0f objects; only the task should allocate before a waiter exists", allocs)
	}
}

func BenchmarkCallbackReportEnqueue(b *testing.B) {
	ctx := context.Background()
	q := &callbackReportTransport{ctx: ctx, running: true, queue: make([]*callbackReportTask, 0, 1)}
	request := &rtiv1.ReportCallbackInvocationRequest{}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := q.enqueueReserved(ctx, request, nil); err != nil {
			b.Fatal(err)
		}
		q.queue[0] = nil
		q.queue = q.queue[:0]
	}
}
