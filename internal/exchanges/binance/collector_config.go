package binance

import (
	"context"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
)

const collectorExchange = "binance"

// CollectorConfig fixes all memory, freshness, and lifecycle bounds for one instrument.
type CollectorConfig struct {
	Instrument       domain.Instrument
	SnapshotDepth    uint32
	BookDepth        int
	QueueCapacity    int
	CandleCapacity   int
	CandleIntervals  []string
	MaximumBookAge   time.Duration
	ClockSyncEvery   time.Duration
	StaleCheckEvery  time.Duration
	ConnectionPolicy ConnectionPolicy
}

// DefaultCollectorConfig returns conservative A7 production-public defaults.
func DefaultCollectorConfig(instrument domain.Instrument) CollectorConfig {
	return CollectorConfig{Instrument: instrument, SnapshotDepth: 5000, BookDepth: 1000,
		QueueCapacity: 8192, CandleCapacity: 512, CandleIntervals: []string{"4h"}, MaximumBookAge: 5 * time.Second,
		ClockSyncEvery: 30 * time.Second, StaleCheckEvery: time.Second,
		ConnectionPolicy: ConnectionPolicy{MinimumBackoff: time.Second, MaximumBackoff: time.Minute,
			Renewal: 23 * time.Hour, Seed: "binance-" + instrument.Symbol()}}
}

func (config CollectorConfig) validate() error {
	if !approvedInstrument(config.Instrument) || !validSnapshotDepth(config.SnapshotDepth) ||
		config.BookDepth <= 0 || config.BookDepth > int(config.SnapshotDepth) ||
		config.QueueCapacity < config.BookDepth || config.QueueCapacity > 1<<20 ||
		config.CandleCapacity <= 0 || config.CandleCapacity > 100_000 || !validCandleIntervals(config.CandleIntervals) ||
		config.MaximumBookAge <= 0 ||
		config.ClockSyncEvery <= 0 || config.StaleCheckEvery <= 0 || config.ConnectionPolicy.Validate() != nil {
		return exchangecontracts.NewError(exchangecontracts.ErrorValidation, exchangecontracts.OperationStream, 0)
	}
	return nil
}

func validCandleIntervals(intervals []string) bool {
	if len(intervals) == 0 || len(intervals) > 3 {
		return false
	}
	seen := make(map[string]struct{}, len(intervals))
	for _, interval := range intervals {
		if !supportedCandleInterval(interval) {
			return false
		}
		if _, duplicate := seen[interval]; duplicate {
			return false
		}
		seen[interval] = struct{}{}
	}
	return true
}

type collectorSource interface {
	SubscribeRecorded(context.Context, exchangecontracts.StreamRequest, PublicRecorder) (ObservedStream, error)
	SnapshotRecorded(context.Context, exchangecontracts.SnapshotRequest, string, uint64, PublicRecorder) (
		exchangecontracts.BookSnapshot, StreamRecordToken, error)
	SampleServerTimeRecorded(context.Context, domain.Instrument, string, uint64, PublicRecorder) (
		TimeHealth, StreamRecordToken, error)
	MonotonicOffset() uint64
}
