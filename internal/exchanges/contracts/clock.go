package exchangecontracts

import (
	"math"
	"sync"
	"time"
)

const clockFusionWindow = 30 * time.Second

// ClockHealth is one immutable exchange-neutral midpoint clock estimate.
type ClockHealth struct {
	ObservedAt  time.Time     `json:"observed_at"`
	Offset      time.Duration `json:"offset"`
	Uncertainty time.Duration `json:"uncertainty"`
	Eligible    bool          `json:"eligible"`
}

// ClockEstimator conservatively intersects successive valid offset intervals.
type ClockEstimator struct {
	mutex              sync.RWMutex
	maximumUncertainty time.Duration
	initialized        bool
	fusionStartedAt    time.Time
	lowerOffset        time.Duration
	upperOffset        time.Duration
	health             ClockHealth
}

// NewClockEstimator constructs a bounded exchange-neutral estimator.
func NewClockEstimator(maximumUncertainty time.Duration) (*ClockEstimator, error) {
	if maximumUncertainty <= 0 {
		return nil, NewError(ErrorValidation, OperationCapability, 0)
	}
	return &ClockEstimator{maximumUncertainty: maximumUncertainty}, nil
}

// Observe intersects the new midpoint interval with prior compatible evidence.
func (estimator *ClockEstimator) Observe(
	sentUTC, receivedUTC, serverUTC time.Time,
	sentMonotonic, receivedMonotonic time.Duration,
) (ClockHealth, error) {
	if estimator == nil {
		return ClockHealth{}, NewError(ErrorTimestamp, OperationCapability, 0)
	}
	sample, err := EstimateClockHealth(sentUTC, receivedUTC, serverUTC,
		sentMonotonic, receivedMonotonic, estimator.maximumUncertainty)
	if err != nil {
		return ClockHealth{}, err
	}
	lower, upper := sample.Offset-sample.Uncertainty, sample.Offset+sample.Uncertainty
	estimator.mutex.Lock()
	defer estimator.mutex.Unlock()
	if !sample.Eligible {
		estimator.health = sample
		return sample, nil
	}
	if estimator.initialized && (sample.ObservedAt.Before(estimator.health.ObservedAt) ||
		sample.ObservedAt.Sub(estimator.fusionStartedAt) > clockFusionWindow) {
		estimator.initialized = false
	}
	if estimator.initialized {
		if lower < estimator.lowerOffset {
			lower = estimator.lowerOffset
		}
		if upper > estimator.upperOffset {
			upper = estimator.upperOffset
		}
	}
	if !estimator.initialized || lower > upper {
		lower, upper = sample.Offset-sample.Uncertainty, sample.Offset+sample.Uncertainty
	}
	if !estimator.initialized {
		estimator.fusionStartedAt = sample.ObservedAt
	}
	estimator.initialized, estimator.lowerOffset, estimator.upperOffset = true, lower, upper
	uncertainty := (upper - lower) / 2
	health := ClockHealth{ObservedAt: sample.ObservedAt, Offset: lower + uncertainty, Uncertainty: uncertainty,
		Eligible: uncertainty <= estimator.maximumUncertainty}
	estimator.health = health
	return health, nil
}

// Health returns the latest fused estimate. The zero value is ineligible.
func (estimator *ClockEstimator) Health() ClockHealth {
	if estimator == nil {
		return ClockHealth{}
	}
	estimator.mutex.RLock()
	defer estimator.mutex.RUnlock()
	return estimator.health
}

// EstimateClockHealth derives offset and uncertainty from a bounded monotonic
// round trip. Exchange timestamps remain evidence; local monotonic time owns age.
func EstimateClockHealth(
	sentUTC, receivedUTC, serverUTC time.Time,
	sentMonotonic, receivedMonotonic, maximumUncertainty time.Duration,
) (ClockHealth, error) {
	if sentUTC.IsZero() || receivedUTC.IsZero() || serverUTC.IsZero() ||
		sentUTC.Location() != time.UTC || receivedUTC.Location() != time.UTC || serverUTC.Location() != time.UTC ||
		receivedMonotonic < sentMonotonic || receivedUTC.Before(sentUTC) || maximumUncertainty <= 0 {
		return ClockHealth{}, NewError(ErrorTimestamp, OperationCapability, 0)
	}
	roundTrip := receivedMonotonic - sentMonotonic
	wallElapsed := receivedUTC.Sub(sentUTC)
	wallCorrection := wallElapsed - roundTrip
	if roundTrip > wallElapsed {
		wallCorrection = roundTrip - wallElapsed
	}
	if wallCorrection > time.Duration(math.MaxInt64)-roundTrip/2 {
		return ClockHealth{}, NewError(ErrorTimestamp, OperationCapability, 0)
	}
	uncertainty := roundTrip/2 + wallCorrection
	midpoint := sentUTC.Add(roundTrip / 2)
	offset := serverUTC.Sub(midpoint)
	if offset > time.Duration(math.MaxInt64)-uncertainty ||
		offset < time.Duration(math.MinInt64)+uncertainty {
		return ClockHealth{}, NewError(ErrorTimestamp, OperationCapability, 0)
	}
	return ClockHealth{ObservedAt: receivedUTC, Offset: offset, Uncertainty: uncertainty,
		Eligible: uncertainty <= maximumUncertainty}, nil
}
