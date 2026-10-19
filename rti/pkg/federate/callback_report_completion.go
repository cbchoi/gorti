package federate

import "context"

// Bound asynchronous work through local completion, not only through wire ACK.
// Reserve outside checkpointMu: completion may need that lock to free a slot.
func (q *callbackReportTransport) reserveCompletion(ctx context.Context) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	for q.completionPending >= callbackReportQueueLimit {
		changed := q.changed
		q.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			q.mu.Lock()
			return ctx.Err()
		case <-q.ctx.Done():
			q.mu.Lock()
			return q.ctx.Err()
		}
		q.mu.Lock()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := q.ctx.Err(); err != nil {
		return err
	}
	q.completionPending++
	return nil
}

func (q *callbackReportTransport) releaseCompletion() {
	q.mu.Lock()
	q.completionPending--
	q.signalLocked()
	q.mu.Unlock()
}

// One session-owned worker retires reports in wire order. ACK readers remain
// independent so a service holding checkpointMu can still pass its wire barrier.
func (q *callbackReportTransport) completeReports() {
	for {
		q.mu.Lock()
		for len(q.completionQueue) == 0 {
			canceled := q.ctx.Err() != nil
			if canceled && q.completionPending == 0 {
				q.mu.Unlock()
				return
			}
			changed := q.changed
			q.mu.Unlock()
			if canceled {
				<-changed
			} else {
				select {
				case <-changed:
				case <-q.ctx.Done():
				}
			}
			q.mu.Lock()
		}
		count := min(len(q.completionQueue), callbackReportBatchSize)
		batch := append([]*callbackReportTask(nil), q.completionQueue[:count]...)
		copy(q.completionQueue, q.completionQueue[count:])
		remaining := len(q.completionQueue) - count
		clear(q.completionQueue[remaining:])
		q.completionQueue = q.completionQueue[:remaining]
		q.mu.Unlock()
		for _, task := range batch {
			<-task.ack.wait()
			task.complete(task.err)
			task.done.finish()
		}
		q.mu.Lock()
		for _, task := range batch {
			delete(q.completing, task)
		}
		q.completionPending -= len(batch)
		q.signalLocked()
		q.mu.Unlock()
	}
}
