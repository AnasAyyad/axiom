package evaluation

import "time"

// Required valid-time windows exclude unhealthy or unverifiable periods.
const (
	RequiredRecordingValidTime = 72 * time.Hour
	RequiredShadowValidTime    = 7 * 24 * time.Hour
)

// ValidTimeObservation is one bounded interval whose complete required feed,
// persistence, clock, and evidence posture has already been assessed.
type ValidTimeObservation struct {
	Start, End         time.Time
	AllFeedsHealthy    bool
	PersistenceHealthy bool
	ClockSafe          bool
	NoQueueDrops       bool
	EvidenceRecorded   bool
}

// Duration returns qualifying time. Invalid and reversed intervals fail
// closed and contribute zero.
func (value ValidTimeObservation) Duration() time.Duration {
	if value.Start.IsZero() || !value.End.After(value.Start) || !value.AllFeedsHealthy ||
		!value.PersistenceHealthy || !value.ClockSafe || !value.NoQueueDrops || !value.EvidenceRecorded {
		return 0
	}
	return value.End.Sub(value.Start)
}

// AccumulateValidTime adds a qualifying interval without allowing overflow or
// double counting. Callers persist lastEnd with the same checkpoint.
func AccumulateValidTime(current time.Duration, lastEnd time.Time, observation ValidTimeObservation) (time.Duration, time.Time, bool) {
	if current < 0 || (!lastEnd.IsZero() && observation.Start.Before(lastEnd)) {
		return current, lastEnd, false
	}
	duration := observation.Duration()
	if duration <= 0 || current > time.Duration(1<<63-1)-duration {
		return current, lastEnd, false
	}
	return current + duration, observation.End, true
}
