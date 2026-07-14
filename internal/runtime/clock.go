package runtimecore

import (
	"sync"
	"time"
)

// Clock supplies authoritative runtime logical time.
type Clock interface {
	Now() LogicalTime
}

// RealClock derives monotonic logical time from one system-clock boundary.
type RealClock struct {
	started time.Time
}

// NewRealClock starts a real-time logical clock.
func NewRealClock() *RealClock { return &RealClock{started: time.Now()} }

// Now returns monotonic elapsed nanoseconds and never returns zero.
func (clock *RealClock) Now() LogicalTime {
	elapsed := time.Since(clock.started)
	if elapsed <= 0 {
		return 1
	}
	return LogicalTime(elapsed.Nanoseconds())
}

// DeterministicClock advances only through explicit caller actions.
type DeterministicClock struct {
	mutex sync.Mutex
	now   LogicalTime
}

// NewDeterministicClock constructs a non-zero logical clock.
func NewDeterministicClock(start LogicalTime) (*DeterministicClock, error) {
	if start == 0 {
		return nil, runtimeError("invalid_clock", "start")
	}
	return &DeterministicClock{now: start}, nil
}

// Now returns the controlled logical time.
func (clock *DeterministicClock) Now() LogicalTime {
	clock.mutex.Lock()
	defer clock.mutex.Unlock()
	return clock.now
}

// Advance moves logical time forward.
func (clock *DeterministicClock) Advance(to LogicalTime) error {
	clock.mutex.Lock()
	defer clock.mutex.Unlock()
	if to < clock.now {
		return runtimeError("time_regression", "clock")
	}
	clock.now = to
	return nil
}
