package svc

import (
	"context"
	"sync"
)

// QuotaRefillGate collapses concurrent refill attempts by seckillProductId.
// For the same product, only one caller executes refill fn; others wait for its result.
type QuotaRefillGate struct {
	states sync.Map // key: seckillProductId(int64) -> *refillState
}

type refillState struct {
	mu      sync.Mutex
	running bool
	done    chan struct{}
	lastErr error
}

func NewQuotaRefillGate() *QuotaRefillGate {
	return &QuotaRefillGate{}
}

// Do executes fn once for the given product id when no refill is in-flight.
// Returns shared=true when current caller waited for another in-flight refill.
func (g *QuotaRefillGate) Do(ctx context.Context, seckillProductId int64, fn func() error) (shared bool, err error) {
	if g == nil {
		return false, fn()
	}

	stateAny, _ := g.states.LoadOrStore(seckillProductId, &refillState{})
	state := stateAny.(*refillState)

	state.mu.Lock()
	if state.running {
		done := state.done
		state.mu.Unlock()

		select {
		case <-done:
			state.mu.Lock()
			defer state.mu.Unlock()
			return true, state.lastErr
		case <-ctx.Done():
			return true, ctx.Err()
		}
	}

	state.running = true
	state.done = make(chan struct{})
	state.mu.Unlock()

	runErr := fn()

	state.mu.Lock()
	state.lastErr = runErr
	close(state.done)
	state.running = false
	state.mu.Unlock()

	return false, runErr
}
