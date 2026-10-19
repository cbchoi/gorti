package federate

// checkpointMu protects the arrival-ordered queue. Ordinary FIFO dispatch
// retires the head without shifting every retained payload; recovery can still
// remove any pending invocation without changing the remaining order.
func (f *Federate) removeRuntimePending(id uint64) {
	for i, pending := range f.runtimePending {
		if pending.id != id {
			continue
		}
		if i == 0 {
			f.runtimePending[0] = sdkQueuedCallback{}
			if len(f.runtimePending) == 1 {
				f.runtimePending = f.runtimePending[:0]
			} else {
				f.runtimePending = f.runtimePending[1:]
			}
		} else {
			copy(f.runtimePending[i:], f.runtimePending[i+1:])
			last := len(f.runtimePending) - 1
			f.runtimePending[last] = sdkQueuedCallback{}
			f.runtimePending = f.runtimePending[:last]
		}
		return
	}
}
