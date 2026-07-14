package binance

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
)

type goldenExpectations struct {
	Snapshot   snapshotGolden   `json:"snapshot"`
	Depth      depthGolden      `json:"depth"`
	Trade      tradeGolden      `json:"trade"`
	Candle     candleGolden     `json:"candle"`
	Instrument instrumentGolden `json:"instrument"`
}

type snapshotGolden struct {
	LastSequence   uint64 `json:"last_sequence"`
	BidPrice       string `json:"bid_price"`
	BidQuantity    string `json:"bid_quantity"`
	AskPrice       string `json:"ask_price"`
	AskQuantity    string `json:"ask_quantity"`
	RawPayloadHash string `json:"raw_payload_hash"`
}

type depthGolden struct {
	FirstSequence  uint64 `json:"first_sequence"`
	LastSequence   uint64 `json:"last_sequence"`
	ExchangeTime   string `json:"exchange_time"`
	BidPrice       string `json:"bid_price"`
	AskPrice       string `json:"ask_price"`
	RawPayloadHash string `json:"raw_payload_hash"`
}

type tradeGolden struct {
	NativeID       string `json:"native_id"`
	Price          string `json:"price"`
	Quantity       string `json:"quantity"`
	ExchangeTime   string `json:"exchange_time"`
	RawPayloadHash string `json:"raw_payload_hash"`
}

type candleGolden struct {
	Interval       string `json:"interval"`
	Open           string `json:"open"`
	High           string `json:"high"`
	Low            string `json:"low"`
	Close          string `json:"close"`
	Volume         string `json:"volume"`
	RawPayloadHash string `json:"raw_payload_hash"`
}

type instrumentGolden struct {
	NativeSymbol    string `json:"native_symbol"`
	NativeStatus    string `json:"native_status"`
	PriceTick       string `json:"price_tick"`
	QuantityStep    string `json:"quantity_step"`
	MinimumQuantity string `json:"minimum_quantity"`
	MinimumNotional string `json:"minimum_notional"`
	RawPayloadHash  string `json:"raw_payload_hash"`
}

func TestOfficialStyleFixturesNormalizeToGolden(t *testing.T) {
	t.Parallel()
	golden := loadGolden(t)
	instrument := mustInstrument(t)
	received := domain.EventTime{UTC: time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC), Sequence: 1}

	snapshot, err := NormalizeSnapshot(loadFixture(t, "depth-snapshot.json"), instrument, received)
	if err != nil {
		t.Fatal(err)
	}
	assertSnapshotGolden(t, snapshot, golden.Snapshot)

	depth, err := NormalizeDepth(loadFixture(t, "depth-update.json"), received)
	if err != nil {
		t.Fatal(err)
	}
	assertDepthGolden(t, depth, golden.Depth)

	trades, err := NormalizeTrades(loadFixture(t, "trades.json"), instrument, received)
	if err != nil || len(trades) != 1 {
		t.Fatalf("trade normalization failed: count=%d err=%v", len(trades), err)
	}
	assertTradeGolden(t, trades[0], golden.Trade)

	candles, err := NormalizeCandleHistory(loadFixture(t, "candles.json"), instrument, "4h", received)
	if err != nil || len(candles) != 1 {
		t.Fatalf("candle normalization failed: count=%d err=%v", len(candles), err)
	}
	assertCandleGolden(t, candles[0], golden.Candle)

	records, err := NormalizeInstruments(loadFixture(t, "exchange-info.json"), received.UTC, 1)
	if err != nil || len(records) != 1 {
		t.Fatalf("metadata normalization failed: count=%d err=%v", len(records), err)
	}
	assertInstrumentGolden(t, records[0], golden.Instrument)
}

func TestUnknownNativeStatusIsPreservedAndRejected(t *testing.T) {
	t.Parallel()
	records, err := NormalizeInstruments(
		loadFixture(t, "exchange-info-unknown-status.json"),
		time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC),
		1,
	)
	if len(records) != 1 || records[0].NativeStatus != "UNRECOGNIZED_STATUS" {
		t.Fatalf("native status was not preserved: %+v", records)
	}
	var failure *exchangecontracts.Error
	if !errors.As(err, &failure) || failure.Kind != exchangecontracts.ErrorValidation {
		t.Fatalf("expected typed validation error, got %v", err)
	}
}

func TestSchemaChangeAndMalformedPayloadFailClosed(t *testing.T) {
	t.Parallel()
	instrument := mustInstrument(t)
	received := domain.EventTime{UTC: time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC), Sequence: 1}
	fixtures := [][]byte{
		[]byte(`{"lastUpdateId":1,"bids":[],"asks":[],"newField":true}`),
		[]byte(`{"lastUpdateId":1,"bids":[["1"]],"asks":[]}`),
		[]byte(`{"lastUpdateId":1,"bids":[],"asks":[]} {"trailing":true}`),
		[]byte(`not-json`),
	}
	for _, fixture := range fixtures {
		_, err := NormalizeSnapshot(fixture, instrument, received)
		if exchangecontracts.KindOf(err) != exchangecontracts.ErrorValidation {
			t.Fatalf("expected validation rejection, got %v", err)
		}
	}
}

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "exchanges", "binance", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func loadGolden(t *testing.T) goldenExpectations {
	t.Helper()
	var result goldenExpectations
	if err := json.Unmarshal(loadFixture(t, "normalized.golden.json"), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func mustInstrument(t *testing.T) domain.Instrument {
	t.Helper()
	instrument, err := domain.NewSpotInstrument("BTC", "USDT")
	if err != nil {
		t.Fatal(err)
	}
	return instrument
}

func assertSnapshotGolden(t *testing.T, actual exchangecontracts.BookSnapshot, expected snapshotGolden) {
	t.Helper()
	if actual.LastSequence != expected.LastSequence || actual.Bids[0].Price.String() != expected.BidPrice ||
		actual.Bids[0].Quantity.String() != expected.BidQuantity || actual.Asks[0].Price.String() != expected.AskPrice ||
		actual.Asks[0].Quantity.String() != expected.AskQuantity || actual.RawPayloadHash != expected.RawPayloadHash {
		t.Fatalf("snapshot mismatch: %+v", actual)
	}
}

func assertDepthGolden(t *testing.T, actual exchangecontracts.DepthUpdate, expected depthGolden) {
	t.Helper()
	if actual.FirstSequence != expected.FirstSequence || actual.LastSequence != expected.LastSequence ||
		actual.ExchangeTime.Format(time.RFC3339Nano) != expected.ExchangeTime || actual.Bids[0].Price.String() != expected.BidPrice ||
		actual.Asks[0].Price.String() != expected.AskPrice || actual.RawPayloadHash != expected.RawPayloadHash {
		t.Fatalf("depth mismatch: %+v", actual)
	}
}

func assertTradeGolden(t *testing.T, actual exchangecontracts.Trade, expected tradeGolden) {
	t.Helper()
	if actual.NativeID != expected.NativeID || actual.Price.String() != expected.Price ||
		actual.Quantity.String() != expected.Quantity || actual.ExchangeTime.Format(time.RFC3339Nano) != expected.ExchangeTime ||
		actual.RawPayloadHash != expected.RawPayloadHash {
		t.Fatalf("trade mismatch: %+v", actual)
	}
}

func assertCandleGolden(t *testing.T, actual exchangecontracts.Candle, expected candleGolden) {
	t.Helper()
	if actual.Interval != expected.Interval || actual.Open.String() != expected.Open || actual.High.String() != expected.High ||
		actual.Low.String() != expected.Low || actual.Close.String() != expected.Close || actual.Volume.String() != expected.Volume ||
		actual.RawPayloadHash != expected.RawPayloadHash {
		t.Fatalf("candle mismatch: %+v", actual)
	}
}

func assertInstrumentGolden(t *testing.T, actual exchangecontracts.InstrumentRecord, expected instrumentGolden) {
	t.Helper()
	if actual.NativeSymbol != expected.NativeSymbol || actual.NativeStatus != expected.NativeStatus ||
		actual.Metadata.PriceTick.String() != expected.PriceTick || actual.Metadata.QuantityStep.String() != expected.QuantityStep ||
		actual.Metadata.MinimumQuantity.String() != expected.MinimumQuantity ||
		actual.Metadata.MinimumNotional.String() != expected.MinimumNotional || actual.RawPayloadHash != expected.RawPayloadHash {
		t.Fatalf("instrument mismatch: %+v", actual)
	}
}
