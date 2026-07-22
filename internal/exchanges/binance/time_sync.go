package binance

import (
	"time"

	exchangecontracts "axiom/internal/exchanges/contracts"
)

// TimeHealth is the shared immutable server-clock estimate.
type TimeHealth = exchangecontracts.ClockHealth

// TimeSynchronizer estimates offset and uncertainty from bounded round trips.
type TimeSynchronizer struct {
	estimator *exchangecontracts.ClockEstimator
}

// NewTimeSynchronizer constructs a fail-closed clock health estimator.
func NewTimeSynchronizer(maximumUncertainty time.Duration) (*TimeSynchronizer, error) {
	if maximumUncertainty <= 0 {
		return nil, exchangecontracts.NewError(exchangecontracts.ErrorValidation, exchangecontracts.OperationCapability, 0)
	}
	estimator, err := exchangecontracts.NewClockEstimator(maximumUncertainty)
	if err != nil {
		return nil, err
	}
	return &TimeSynchronizer{estimator: estimator}, nil
}

// Observe records one monotonic round trip and midpoint server offset.
func (synchronizer *TimeSynchronizer) Observe(
	sentUTC, receivedUTC, serverUTC time.Time,
	sentMonotonic, receivedMonotonic time.Duration,
) error {
	_, err := synchronizer.estimator.Observe(sentUTC, receivedUTC, serverUTC, sentMonotonic, receivedMonotonic)
	return err
}

// Health returns the latest estimate. The zero value is ineligible.
func (synchronizer *TimeSynchronizer) Health() TimeHealth {
	return synchronizer.estimator.Health()
}
