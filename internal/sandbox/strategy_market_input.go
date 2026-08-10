package sandbox

import (
	"context"
	"sort"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
)

// StrategyMarketRequirements describes the bounded public facts needed before
// an automatic strategy can construct its immutable decision input. The
// caller, not this reader, owns strategy-specific indicator calculations.
type StrategyMarketRequirements struct {
	CandleIntervals []string
	CandleLimit     uint32
	BookDepth       uint32
	MaximumBookAge  time.Duration
}

// Validate rejects unbounded, unsupported, or stale-input-prone requests.
func (requirements StrategyMarketRequirements) Validate() error {
	if len(requirements.CandleIntervals) == 0 || len(requirements.CandleIntervals) > 2 ||
		requirements.CandleLimit == 0 || requirements.CandleLimit > 1000 ||
		requirements.BookDepth != 50 || requirements.MaximumBookAge <= 0 ||
		requirements.MaximumBookAge > 250*time.Millisecond {
		return contractError("strategy_market_requirements_invalid")
	}
	seen := make(map[string]struct{}, len(requirements.CandleIntervals))
	for _, interval := range requirements.CandleIntervals {
		if interval != "1h" && interval != "4h" {
			return contractError("strategy_market_requirements_invalid")
		}
		if _, duplicate := seen[interval]; duplicate {
			return contractError("strategy_market_requirements_invalid")
		}
		seen[interval] = struct{}{}
	}
	return nil
}

// StrategyMarketInput is one immutable, credential-free public-data view.
// The raw source hashes and local observation time are retained so a strategy
// can bind its own decision input to the exact facts it evaluated.
type StrategyMarketInput struct {
	Instrument domain.Instrument                     `json:"instrument"`
	Metadata   exchangecontracts.InstrumentRecord    `json:"metadata"`
	Book       exchangecontracts.BookSnapshot        `json:"book"`
	Candles    map[string][]exchangecontracts.Candle `json:"candles"`
	ObservedAt domain.EventTime                      `json:"observed_at"`
}

// StrategyMarketInputReader reads a single bounded public view. It is
// intentionally constructed from StrategyMarketData, whose contract excludes
// private account, order, cancel, and private-stream operations.
type StrategyMarketInputReader struct {
	data  StrategyMarketData
	clock domain.Clock
}

// NewStrategyMarketInputReader creates a credential-free strategy-input
// reader. A system clock is used only to reject stale or future public facts.
func NewStrategyMarketInputReader(
	data StrategyMarketData,
	clock domain.Clock,
) (*StrategyMarketInputReader, error) {
	if data == nil || clock == nil {
		return nil, contractError("strategy_market_reader_invalid")
	}
	return &StrategyMarketInputReader{data: data, clock: clock}, nil
}

// Read returns one complete public input view or fails closed. It never
// derives a candle from a partial update, substitutes missing metadata, or
// treats an old book as current just because the candle history is valid.
func (reader *StrategyMarketInputReader) Read(
	ctx context.Context,
	instrument domain.Instrument,
	requirements StrategyMarketRequirements,
) (StrategyMarketInput, error) {
	if reader == nil || reader.clock == nil {
		return StrategyMarketInput{}, contractError("strategy_market_input_invalid")
	}
	return reader.ReadAt(ctx, instrument, requirements, reader.clock.Now())
}

// ReadAt binds every public fact to the scheduler's exact evaluation instant.
// This prevents multiple system-clock reads from producing a market view that
// cannot match account, arm, and risk evidence for the same decision.
func (reader *StrategyMarketInputReader) ReadAt(
	ctx context.Context,
	instrument domain.Instrument,
	requirements StrategyMarketRequirements,
	now domain.EventTime,
) (StrategyMarketInput, error) {
	if reader == nil || ctx == nil || instrument.Product != domain.ProductSpot ||
		requirements.Validate() != nil || now.Validate() != nil {
		return StrategyMarketInput{}, contractError("strategy_market_input_invalid")
	}
	metadata, err := reader.data.Instruments(ctx, []domain.Instrument{instrument})
	if err != nil || len(metadata) != 1 ||
		!validStrategyMarketMetadata(metadata[0], instrument, now.UTC) {
		return StrategyMarketInput{}, contractError("strategy_market_metadata_unavailable")
	}
	book, err := reader.data.Snapshot(ctx, exchangecontracts.SnapshotRequest{
		Instrument: instrument, Depth: requirements.BookDepth,
	})
	if err != nil || !validStrategyMarketBook(book, instrument, now.UTC, requirements.MaximumBookAge) {
		return StrategyMarketInput{}, contractError("strategy_market_book_unavailable")
	}
	candles := make(map[string][]exchangecontracts.Candle, len(requirements.CandleIntervals))
	for _, interval := range requirements.CandleIntervals {
		items, loadErr := reader.data.Candles(ctx, exchangecontracts.CandleRequest{
			HistoryRequest: exchangecontracts.HistoryRequest{Instrument: instrument,
				Start: now.UTC.Add(-strategyMarketHistoryWindow(interval, requirements.CandleLimit)),
				End:   now.UTC, Limit: requirements.CandleLimit},
			Interval: interval,
		})
		if loadErr != nil || !validStrategyMarketCandles(
			items, instrument, interval, requirements.CandleLimit, now.UTC,
		) {
			return StrategyMarketInput{}, contractError("strategy_market_candles_unavailable")
		}
		candles[interval] = append([]exchangecontracts.Candle(nil), items...)
	}
	return StrategyMarketInput{Instrument: instrument, Metadata: metadata[0], Book: book,
		Candles: candles, ObservedAt: now}, nil
}

func strategyMarketHistoryWindow(interval string, limit uint32) time.Duration {
	unit := time.Hour
	if interval == "4h" {
		unit = 4 * time.Hour
	}
	return time.Duration(limit) * unit
}

func validStrategyMarketMetadata(
	item exchangecontracts.InstrumentRecord,
	instrument domain.Instrument,
	now time.Time,
) bool {
	return item.Metadata.Instrument == instrument && item.Metadata.Validate() == nil &&
		!item.Metadata.EffectiveAt.After(now) && hash256(item.RawPayloadHash)
}

func validStrategyMarketBook(
	book exchangecontracts.BookSnapshot,
	instrument domain.Instrument,
	now time.Time,
	maximumAge time.Duration,
) bool {
	return book.Instrument == instrument && book.ReceivedAt.Validate() == nil &&
		!book.ReceivedAt.UTC.After(now) && now.Sub(book.ReceivedAt.UTC) <= maximumAge &&
		book.LastSequence > 0 && len(book.Bids) > 0 && len(book.Asks) > 0 &&
		hash256(book.RawPayloadHash)
}

func validStrategyMarketCandles(
	items []exchangecontracts.Candle,
	instrument domain.Instrument,
	interval string,
	minimumCount uint32,
	now time.Time,
) bool {
	if minimumCount == 0 || len(items) < int(minimumCount) {
		return false
	}
	copyItems := append([]exchangecontracts.Candle(nil), items...)
	sort.Slice(copyItems, func(left, right int) bool {
		return copyItems[left].OpenTime.Before(copyItems[right].OpenTime)
	})
	for index, item := range copyItems {
		if item.Instrument != instrument || item.Interval != interval || !item.Closed ||
			item.OpenTime.IsZero() || item.CloseTime.IsZero() ||
			item.OpenTime.Location() != time.UTC || item.CloseTime.Location() != time.UTC ||
			!item.OpenTime.Before(item.CloseTime) || item.CloseTime.After(now) ||
			!hash256(item.RawPayloadHash) ||
			(index > 0 && !copyItems[index-1].CloseTime.Before(item.CloseTime)) {
			return false
		}
	}
	return true
}
