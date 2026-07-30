package bybit

import (
	"errors"
	"sync"
	"time"
)

// ErrDemoRateBudget reports that reserved cancellation or reconciliation
// capacity would otherwise be consumed.
var ErrDemoRateBudget = errors.New("bybit_demo_rate_budget_exhausted")

type demoRequestClass uint8

const (
	demoRequestEntry demoRequestClass = iota + 1
	demoRequestCancel
	demoRequestReconcile
)

type demoRateBudget struct {
	mutex       sync.Mutex
	capacity    uint64
	reserved    uint64
	window      time.Duration
	windowStart time.Time
	used        uint64
}

func newDemoRateBudget(
	capacity uint64,
	reserved uint64,
	window time.Duration,
) (*demoRateBudget, error) {
	if capacity == 0 || reserved == 0 || reserved >= capacity ||
		window <= 0 {
		return nil, ErrDemoRateBudget
	}
	return &demoRateBudget{
		capacity: capacity,
		reserved: reserved,
		window:   window,
	}, nil
}

func (budget *demoRateBudget) acquire(
	now time.Time,
	cost uint64,
	class demoRequestClass,
) error {
	if budget == nil || now.IsZero() || now.Location() != time.UTC ||
		cost == 0 ||
		(class != demoRequestEntry &&
			class != demoRequestCancel &&
			class != demoRequestReconcile) {
		return ErrDemoRateBudget
	}
	budget.mutex.Lock()
	defer budget.mutex.Unlock()
	if budget.windowStart.IsZero() ||
		now.Before(budget.windowStart) ||
		now.Sub(budget.windowStart) >= budget.window {
		budget.windowStart = now
		budget.used = 0
	}
	limit := budget.capacity
	if class == demoRequestEntry {
		limit -= budget.reserved
	}
	if cost > limit || budget.used > limit-cost {
		return ErrDemoRateBudget
	}
	budget.used += cost
	return nil
}
