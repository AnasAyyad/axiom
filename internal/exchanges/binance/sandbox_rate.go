package binance

import (
	"errors"
	"sync"
	"time"
)

// ErrSandboxRateBudget reports that reserved cancellation or reconciliation
// capacity would otherwise be consumed.
var ErrSandboxRateBudget = errors.New("binance_testnet_rate_budget_exhausted")

type sandboxRequestClass uint8

const (
	sandboxRequestEntry sandboxRequestClass = iota + 1
	sandboxRequestCancel
	sandboxRequestReconcile
)

// sandboxRateBudget reserves request weight for cancellation and
// reconciliation even when entry traffic reaches its local ceiling.
type sandboxRateBudget struct {
	mutex       sync.Mutex
	capacity    uint64
	reserved    uint64
	window      time.Duration
	windowStart time.Time
	used        uint64
}

func newSandboxRateBudget(
	capacity uint64,
	reserved uint64,
	window time.Duration,
) (*sandboxRateBudget, error) {
	if capacity == 0 || reserved == 0 || reserved >= capacity ||
		window <= 0 {
		return nil, ErrSandboxRateBudget
	}
	return &sandboxRateBudget{
		capacity: capacity,
		reserved: reserved,
		window:   window,
	}, nil
}

func (budget *sandboxRateBudget) acquire(
	now time.Time,
	cost uint64,
	class sandboxRequestClass,
) error {
	if budget == nil || now.IsZero() || now.Location() != time.UTC ||
		cost == 0 ||
		(class != sandboxRequestEntry &&
			class != sandboxRequestCancel &&
			class != sandboxRequestReconcile) {
		return ErrSandboxRateBudget
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
	if class == sandboxRequestEntry {
		limit -= budget.reserved
	}
	if cost > limit || budget.used > limit-cost {
		return ErrSandboxRateBudget
	}
	budget.used += cost
	return nil
}
