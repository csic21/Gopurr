package engine

import "context"

// CancelPending interrupts the calls that are in flight at this instant. A
// native VPN host calls it before enqueueing disconnect or handling revocation,
// so a slow subscription download or latency test cannot hold up teardown.
// Future calls receive independent contexts and are not cancelled.
//
// This must remain independent of e.mu: the operation being cancelled may hold
// that mutex while blocked in network I/O. Cancel functions also run outside
// cancelMu, allowing calls to unregister as their contexts are cancelled.
func (e *Engine) CancelPending() {
	e.cancelMu.Lock()
	cancels := make([]context.CancelFunc, 0, len(e.activeCancels))
	for _, cancel := range e.activeCancels {
		cancels = append(cancels, cancel)
	}
	e.cancelMu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}

func (e *Engine) registerCall(parent context.Context) (context.Context, func()) {
	ctx, cancel := context.WithCancel(parent)
	e.cancelMu.Lock()
	if e.activeCancels == nil {
		e.activeCancels = make(map[uint64]context.CancelFunc)
	}
	e.nextCall++
	id := e.nextCall
	e.activeCancels[id] = cancel
	e.cancelMu.Unlock()
	return ctx, func() {
		e.cancelMu.Lock()
		delete(e.activeCancels, id)
		e.cancelMu.Unlock()
		cancel()
	}
}
