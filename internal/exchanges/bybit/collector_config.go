package bybit

import (
	"context"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
)

const collectorExchange = "bybit"

// CollectorConfig fixes memory, freshness, intervals, and reconnect bounds.
type CollectorConfig struct {
	Instrument      domain.Instrument
	BookDepth       int
	QueueCapacity   int
	CandleCapacity  int
	CandleIntervals []string
	MaximumBookAge  time.Duration
	HeartbeatEvery  time.Duration
	StaleCheckEvery time.Duration
	MinimumBackoff  time.Duration
	MaximumBackoff  time.Duration
	Renewal         time.Duration
}

// DefaultCollectorConfig returns conservative B1 public recording defaults.
func DefaultCollectorConfig(instrument domain.Instrument) CollectorConfig {
	return CollectorConfig{Instrument: instrument, BookDepth: 1000, QueueCapacity: 8192,
		CandleCapacity: 512, CandleIntervals: []string{"15m", "1h", "4h"},
		MaximumBookAge: 5 * time.Second, HeartbeatEvery: 20 * time.Second,
		StaleCheckEvery: time.Second, MinimumBackoff: time.Second, MaximumBackoff: time.Minute,
		Renewal: 23 * time.Hour}
}

func (config CollectorConfig) validate() error {
	if !approvedInstrument(config.Instrument) || config.BookDepth != 1000 ||
		config.QueueCapacity < config.BookDepth || config.QueueCapacity > 1<<20 ||
		config.CandleCapacity <= 0 || config.CandleCapacity > 100_000 ||
		config.MaximumBookAge <= 0 || config.HeartbeatEvery <= 0 || config.StaleCheckEvery <= 0 ||
		config.MinimumBackoff <= 0 || config.MaximumBackoff < config.MinimumBackoff ||
		config.MaximumBackoff > 5*time.Minute || config.Renewal <= 0 || config.Renewal > 24*time.Hour ||
		!validCollectorIntervals(config.CandleIntervals) {
		return validationError(exchangecontracts.OperationStream)
	}
	return nil
}

func validCollectorIntervals(intervals []string) bool {
	if len(intervals) != 3 {
		return false
	}
	wanted := []string{"15m", "1h", "4h"}
	for index := range wanted {
		if intervals[index] != wanted[index] {
			return false
		}
	}
	return true
}

type collectorSource interface {
	SubscribeRecorded(context.Context, exchangecontracts.StreamRequest,
		exchangecontracts.PublicRecorder) (ObservedStream, error)
	SampleServerTimeRecorded(context.Context, domain.Instrument, string, uint64,
		exchangecontracts.PublicRecorder) (ClockHealth, exchangecontracts.StreamRecordToken, error)
	MonotonicOffset() uint64
}
