package domain

import (
	"sync"
	"sync/atomic"
	"time"
)

// EventTime pairs an externally serializable UTC time with local monotonic order.
type EventTime struct {
	UTC      time.Time `json:"utc"`
	Sequence uint64    `json:"sequence"`
}

// Validate rejects zero, non-UTC, or unordered timestamps.
func (value EventTime) Validate() error {
	if value.UTC.IsZero() || value.UTC.Location() != time.UTC || value.Sequence == 0 {
		return domainError(CodeInvalidTimestamp, "event_time")
	}
	return nil
}

// Clock supplies deterministic event timestamps without direct time access.
type Clock interface {
	Now() EventTime
}

// SystemClock supplies real UTC wall time and a process-local monotonic sequence.
type SystemClock struct {
	sequence atomic.Uint64
}

// Now returns the current UTC time with a strictly increasing local sequence.
func (clock *SystemClock) Now() EventTime {
	return EventTime{UTC: time.Now().UTC(), Sequence: clock.sequence.Add(1)}
}

// ReplayClock supplies caller-controlled deterministic replay time.
type ReplayClock struct {
	mutex    sync.Mutex
	current  time.Time
	sequence uint64
}

// NewReplayClock constructs a deterministic clock at a required UTC instant.
func NewReplayClock(start time.Time) (*ReplayClock, error) {
	if start.IsZero() || start.Location() != time.UTC {
		return nil, domainError(CodeInvalidTimestamp, "replay_clock")
	}
	return &ReplayClock{current: start}, nil
}

// Now returns the controlled time and advances only its ordering sequence.
func (clock *ReplayClock) Now() EventTime {
	clock.mutex.Lock()
	defer clock.mutex.Unlock()
	clock.sequence++
	return EventTime{UTC: clock.current, Sequence: clock.sequence}
}

// Advance moves replay time forward and rejects zero or negative movement.
func (clock *ReplayClock) Advance(duration time.Duration) error {
	clock.mutex.Lock()
	defer clock.mutex.Unlock()
	if duration <= 0 {
		return domainError(CodeInvalidTimestamp, "replay_advance")
	}
	clock.current = clock.current.Add(duration)
	return nil
}
