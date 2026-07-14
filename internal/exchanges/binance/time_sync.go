package binance

import (
	"sync"
	"time"

	exchangecontracts "axiom/internal/exchanges/contracts"
)

// TimeHealth is one immutable server-clock estimate.
type TimeHealth struct {
	ObservedAt  time.Time     `json:"observed_at"`
	Offset      time.Duration `json:"offset"`
	Uncertainty time.Duration `json:"uncertainty"`
	Eligible    bool          `json:"eligible"`
}

// TimeSynchronizer estimates offset and uncertainty from bounded round trips.
type TimeSynchronizer struct {
	mutex              sync.RWMutex
	maximumUncertainty time.Duration
	health             TimeHealth
}

// NewTimeSynchronizer constructs a fail-closed clock health estimator.
func NewTimeSynchronizer(maximumUncertainty time.Duration) (*TimeSynchronizer, error) {
	if maximumUncertainty <= 0 {
		return nil, exchangecontracts.NewError(exchangecontracts.ErrorValidation, exchangecontracts.OperationCapability, 0)
	}
	return &TimeSynchronizer{maximumUncertainty: maximumUncertainty}, nil
}

// Observe records one monotonic round trip and midpoint server offset.
func (synchronizer *TimeSynchronizer) Observe(
	sentUTC, receivedUTC, serverUTC time.Time,
	sentMonotonic, receivedMonotonic time.Duration,
) error {
	if sentUTC.IsZero() || receivedUTC.IsZero() || serverUTC.IsZero() ||
		sentUTC.Location() != time.UTC || receivedUTC.Location() != time.UTC || serverUTC.Location() != time.UTC ||
		receivedMonotonic < sentMonotonic || receivedUTC.Before(sentUTC) {
		return exchangecontracts.NewError(exchangecontracts.ErrorTimestamp, exchangecontracts.OperationCapability, 0)
	}
	roundTrip := receivedMonotonic - sentMonotonic
	wallElapsed := receivedUTC.Sub(sentUTC)
	clockCorrection := absoluteDuration(wallElapsed - roundTrip)
	uncertainty := roundTrip/2 + clockCorrection
	midpoint := sentUTC.Add(roundTrip / 2)
	health := TimeHealth{ObservedAt: receivedUTC, Offset: serverUTC.Sub(midpoint), Uncertainty: uncertainty,
		Eligible: uncertainty <= synchronizer.maximumUncertainty}
	synchronizer.mutex.Lock()
	synchronizer.health = health
	synchronizer.mutex.Unlock()
	return nil
}

// Health returns the latest estimate. The zero value is ineligible.
func (synchronizer *TimeSynchronizer) Health() TimeHealth {
	synchronizer.mutex.RLock()
	defer synchronizer.mutex.RUnlock()
	return synchronizer.health
}

func absoluteDuration(value time.Duration) time.Duration {
	if value < 0 {
		return -value
	}
	return value
}
