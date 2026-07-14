package marketdata

import (
	"encoding/json"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
)

// HealthState is the fail-closed eligibility state of one market generation.
type HealthState string

// Public market-data lifecycle states.
const (
	HealthConnecting   HealthState = "CONNECTING"
	HealthSyncing      HealthState = "SYNCING"
	HealthHealthy      HealthState = "HEALTHY"
	HealthDegraded     HealthState = "DEGRADED"
	HealthStale        HealthState = "STALE"
	HealthDisconnected HealthState = "DISCONNECTED"
	HealthPaused       HealthState = "PAUSED"
)

// Error is a bounded market-data failure with no payload or URL content.
type Error struct{ Code string }

// Error returns a bounded market-data reason code.
func (failure *Error) Error() string { return "market_data:" + failure.Code }

func marketError(code string) error { return &Error{Code: code} }

// Observation preserves the distinct A7 time and ordering facts.
type Observation struct {
	ExchangeTime         time.Time        `json:"exchange_time,omitempty"`
	ReceivedAt           domain.EventTime `json:"received_at"`
	ProcessedAt          domain.EventTime `json:"processed_at"`
	PublishedAt          domain.EventTime `json:"published_at"`
	ConnectionID         string           `json:"connection_id"`
	ConnectionGeneration uint64           `json:"connection_generation"`
	SourceSequence       uint64           `json:"source_sequence"`
	IngestOrdinal        uint64           `json:"ingest_ordinal"`
	ReceivedOffsetNanos  uint64           `json:"received_offset_nanos"`
	ProcessedOffsetNanos uint64           `json:"processed_offset_nanos"`
	PublishedOffsetNanos uint64           `json:"published_offset_nanos"`
}

// Validate rejects missing, regressing, or ambiguous event evidence.
func (value Observation) Validate() error {
	if value.ReceivedAt.Validate() != nil || value.ProcessedAt.Validate() != nil ||
		value.PublishedAt.Validate() != nil || value.ConnectionID == "" ||
		value.ConnectionGeneration == 0 || value.SourceSequence == 0 ||
		value.IngestOrdinal == 0 || value.ReceivedOffsetNanos == 0 || value.ProcessedOffsetNanos == 0 ||
		value.PublishedOffsetNanos == 0 {
		return marketError("observation_invalid")
	}
	if value.ProcessedAt.UTC.Before(value.ReceivedAt.UTC) || value.PublishedAt.UTC.Before(value.ProcessedAt.UTC) ||
		value.ProcessedAt.Sequence <= value.ReceivedAt.Sequence || value.PublishedAt.Sequence <= value.ProcessedAt.Sequence ||
		value.ProcessedOffsetNanos < value.ReceivedOffsetNanos || value.PublishedOffsetNanos < value.ProcessedOffsetNanos {
		return marketError("observation_regressed")
	}
	if !value.ExchangeTime.IsZero() && value.ExchangeTime.Location() != time.UTC {
		return marketError("exchange_time_invalid")
	}
	return nil
}

// DepthEvent couples a normalized delta to immutable ingest evidence.
type DepthEvent struct {
	Update      exchangecontracts.DepthUpdate `json:"update"`
	Observation Observation                   `json:"observation"`
}

// Defect is one generation-invalidating market-data finding.
type Defect struct {
	Code       string            `json:"code"`
	Exchange   string            `json:"exchange"`
	Instrument domain.Instrument `json:"instrument"`
	Generation uint64            `json:"generation"`
	Sequence   uint64            `json:"sequence"`
}

// DefectSink receives bounded incident facts outside the ordered book lock.
type DefectSink func(Defect)

// BookView is an immutable defensive snapshot of one book version.
type BookView struct{ record bookViewRecord }

type bookViewRecord struct {
	Exchange    string                         `json:"exchange"`
	Instrument  domain.Instrument              `json:"instrument"`
	Health      HealthState                    `json:"health"`
	Generation  uint64                         `json:"generation"`
	Sequence    uint64                         `json:"sequence"`
	Version     uint64                         `json:"version"`
	Observation Observation                    `json:"observation"`
	Bids        []exchangecontracts.PriceLevel `json:"bids"`
	Asks        []exchangecontracts.PriceLevel `json:"asks"`
}

// MarshalJSON emits the canonical immutable view record.
func (view BookView) MarshalJSON() ([]byte, error) { return json.Marshal(view.record) }

// Exchange returns the source exchange.
func (view BookView) Exchange() string { return view.record.Exchange }

// Instrument returns the canonical spot instrument.
func (view BookView) Instrument() domain.Instrument { return view.record.Instrument }

// Health returns the generation eligibility state.
func (view BookView) Health() HealthState { return view.record.Health }

// Generation returns the active connection generation.
func (view BookView) Generation() uint64 { return view.record.Generation }

// Sequence returns the last applied exchange sequence.
func (view BookView) Sequence() uint64 { return view.record.Sequence }

// Version returns the monotonically increasing local view version.
func (view BookView) Version() uint64 { return view.record.Version }

// Observation returns the publication evidence.
func (view BookView) Observation() Observation { return view.record.Observation }

// Bids returns a defensive best-to-worst bid copy.
func (view BookView) Bids() []exchangecontracts.PriceLevel { return cloneLevels(view.record.Bids) }

// Asks returns a defensive best-to-worst ask copy.
func (view BookView) Asks() []exchangecontracts.PriceLevel { return cloneLevels(view.record.Asks) }

// Eligible requires a healthy generation and bounded monotonic age.
func (view BookView) Eligible(currentOffset uint64, maximumAge time.Duration) bool {
	if view.record.Health != HealthHealthy || maximumAge <= 0 || currentOffset < view.record.Observation.PublishedOffsetNanos {
		return false
	}
	age := currentOffset - view.record.Observation.PublishedOffsetNanos
	return age <= uint64(maximumAge.Nanoseconds())
}

// MarketViewProvider returns immutable book and completed-candle views.
type MarketViewProvider interface {
	Book(exchange string, instrument domain.Instrument) (BookView, error)
	CompletedCandles(exchange string, instrument domain.Instrument, interval string) (CandleView, error)
}

// CandleView is an immutable completed-candle series version.
type CandleView struct{ record candleViewRecord }

type candleViewRecord struct {
	Exchange    string                     `json:"exchange"`
	Instrument  domain.Instrument          `json:"instrument"`
	Interval    string                     `json:"interval"`
	Version     uint64                     `json:"version"`
	Observation Observation                `json:"observation"`
	Candles     []exchangecontracts.Candle `json:"candles"`
}

// MarshalJSON emits the canonical immutable candle record.
func (view CandleView) MarshalJSON() ([]byte, error) { return json.Marshal(view.record) }

// Version returns the monotonic completed-series version.
func (view CandleView) Version() uint64 { return view.record.Version }

// Candles returns a defensive chronological copy.
func (view CandleView) Candles() []exchangecontracts.Candle {
	return append([]exchangecontracts.Candle(nil), view.record.Candles...)
}

// Observation returns the latest completed-candle publication evidence.
func (view CandleView) Observation() Observation { return view.record.Observation }
