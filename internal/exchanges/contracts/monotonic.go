package exchangecontracts

import "time"

// MonotonicSource is one shared process-local ordering epoch for all adapters.
type MonotonicSource func() time.Duration

// NewProcessMonotonicSource constructs a shared positive process-local epoch.
func NewProcessMonotonicSource() MonotonicSource {
	started := time.Now()
	return func() time.Duration {
		if elapsed := time.Since(started); elapsed > 0 {
			return elapsed
		}
		return time.Nanosecond
	}
}
