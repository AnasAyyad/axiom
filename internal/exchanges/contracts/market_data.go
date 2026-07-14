package exchangecontracts

import (
	"context"
	"time"

	"axiom/internal/domain"
)

// Operation identifies a narrow exchange-boundary action for policy and errors.
type Operation string

// Public operations are callable in V1A. The remaining values exist only for
// capability and retry classification and expose no transport methods.
const (
	OperationCapability Operation = "capability"
	OperationMetadata   Operation = "metadata"
	OperationSnapshot   Operation = "snapshot"
	OperationTrades     Operation = "trades"
	OperationCandles    Operation = "candles"
	OperationStream     Operation = "stream"
	OperationAccount    Operation = "account"
	OperationSubmission Operation = "submission"
	OperationCancel     Operation = "cancel"
	OperationReconcile  Operation = "reconcile"
)

// PriceLevel is one exact canonical order-book level.
type PriceLevel struct {
	Price    domain.Price    `json:"price"`
	Quantity domain.Quantity `json:"quantity"`
}

// BookSnapshot is a canonical full-depth replacement.
type BookSnapshot struct {
	Exchange       ExchangeID        `json:"exchange"`
	Instrument     domain.Instrument `json:"instrument"`
	LastSequence   uint64            `json:"last_sequence"`
	ReceivedAt     domain.EventTime  `json:"received_at"`
	Bids           []PriceLevel      `json:"bids"`
	Asks           []PriceLevel      `json:"asks"`
	RawPayloadHash string            `json:"raw_payload_hash"`
}

// DepthUpdate is a canonical incremental depth event.
type DepthUpdate struct {
	Exchange       ExchangeID        `json:"exchange"`
	Instrument     domain.Instrument `json:"instrument"`
	ExchangeTime   time.Time         `json:"exchange_time"`
	FirstSequence  uint64            `json:"first_sequence"`
	LastSequence   uint64            `json:"last_sequence"`
	ReceivedAt     domain.EventTime  `json:"received_at"`
	Bids           []PriceLevel      `json:"bids"`
	Asks           []PriceLevel      `json:"asks"`
	RawPayloadHash string            `json:"raw_payload_hash"`
}

// Trade is a canonical public trade.
type Trade struct {
	Exchange       ExchangeID        `json:"exchange"`
	Instrument     domain.Instrument `json:"instrument"`
	NativeID       string            `json:"native_id"`
	Price          domain.Price      `json:"price"`
	Quantity       domain.Quantity   `json:"quantity"`
	BuyerIsMaker   bool              `json:"buyer_is_maker"`
	ExchangeTime   time.Time         `json:"exchange_time"`
	ReceivedAt     domain.EventTime  `json:"received_at"`
	RawPayloadHash string            `json:"raw_payload_hash"`
}

// Candle is one canonical exchange candle.
type Candle struct {
	Exchange       ExchangeID        `json:"exchange"`
	Instrument     domain.Instrument `json:"instrument"`
	Interval       string            `json:"interval"`
	OpenTime       time.Time         `json:"open_time"`
	CloseTime      time.Time         `json:"close_time"`
	Open           domain.Price      `json:"open"`
	High           domain.Price      `json:"high"`
	Low            domain.Price      `json:"low"`
	Close          domain.Price      `json:"close"`
	Volume         domain.Quantity   `json:"volume"`
	Closed         bool              `json:"closed"`
	ReceivedAt     domain.EventTime  `json:"received_at"`
	RawPayloadHash string            `json:"raw_payload_hash"`
}

// InstrumentRecord combines canonical metadata with preserved native facts.
type InstrumentRecord struct {
	Exchange       ExchangeID                `json:"exchange"`
	NativeSymbol   string                    `json:"native_symbol"`
	NativeStatus   string                    `json:"native_status"`
	Metadata       domain.InstrumentMetadata `json:"metadata"`
	RawPayloadHash string                    `json:"raw_payload_hash"`
}

// SnapshotRequest asks for one bounded public order-book snapshot.
type SnapshotRequest struct {
	Instrument domain.Instrument
	Depth      uint32
}

// HistoryRequest asks for one bounded public historical window.
type HistoryRequest struct {
	Instrument domain.Instrument
	Start      time.Time
	End        time.Time
	Limit      uint32
}

// CandleRequest asks for one bounded completed-candle window.
type CandleRequest struct {
	HistoryRequest
	Interval string
}

// StreamRequest asks for one public stream generation.
type StreamRequest struct {
	Instrument domain.Instrument
	Kinds      []StreamKind
}

// StreamKind is one canonical public stream payload class.
type StreamKind string

// Supported V1A public stream kinds.
const (
	StreamDepth  StreamKind = "depth"
	StreamTrades StreamKind = "trades"
	StreamCandle StreamKind = "candle"
)

// StreamEvent is one normalized public stream result.
type StreamEvent struct {
	Kind   StreamKind   `json:"kind"`
	Depth  *DepthUpdate `json:"depth,omitempty"`
	Trade  *Trade       `json:"trade,omitempty"`
	Candle *Candle      `json:"candle,omitempty"`
}

// Stream is a bounded, caller-cancelable normalized event source.
type Stream interface {
	Receive(context.Context) (StreamEvent, error)
	Close() error
}

// MarketDataSource owns only public snapshot and stream behavior.
type MarketDataSource interface {
	Snapshot(context.Context, SnapshotRequest) (BookSnapshot, error)
	Subscribe(context.Context, StreamRequest) (Stream, error)
}

// InstrumentCatalog loads canonical public instrument metadata.
type InstrumentCatalog interface {
	Instruments(context.Context, []domain.Instrument) ([]InstrumentRecord, error)
}

// HistoricalReader loads bounded public trade and candle history.
type HistoricalReader interface {
	Trades(context.Context, HistoryRequest) ([]Trade, error)
	Candles(context.Context, CandleRequest) ([]Candle, error)
}

// CapabilitySource exposes an immutable capability descriptor.
type CapabilitySource interface {
	Capabilities() Descriptor
}

func validOperation(operation Operation) bool {
	switch operation {
	case OperationCapability, OperationMetadata, OperationSnapshot, OperationTrades,
		OperationCandles, OperationStream, OperationAccount, OperationSubmission,
		OperationCancel, OperationReconcile:
		return true
	default:
		return false
	}
}
