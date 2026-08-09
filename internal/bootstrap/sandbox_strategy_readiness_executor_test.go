package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"axiom/internal/config"
	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/sandbox"
)

func TestSandboxStrategyReadinessExecutorRequiresCompletePublicWarmupBeforePositionProjection(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	clock, err := domain.NewReplayClock(now)
	if err != nil {
		t.Fatal(err)
	}
	work, record := readinessWorkAndConfiguration(t, sandbox.StrategyTrend, now)
	instrument, err := domain.NewSpotInstrument("BTC", "USDT")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name     string
		candles  int
		expected string
	}{
		{name: "complete warmup", candles: 1000, expected: "waiting_for_position_projection"},
		{name: "incomplete warmup", candles: 999, expected: "waiting_for_public_market_data"},
	} {
		t.Run(test.name, func(t *testing.T) {
			executor, newErr := NewSandboxStrategyReadinessExecutor(
				newReadinessMarketData(t, instrument, now, test.candles), clock,
			)
			if newErr != nil {
				t.Fatal(newErr)
			}
			evaluation, evaluateErr := executor.EvaluateStrategySession(
				context.Background(), work, record, readinessExecutionLease(work), now,
			)
			if evaluateErr != nil || evaluation.State != sandbox.StrategySessionEvaluationWaiting ||
				evaluation.Reason != test.expected || evaluation.ValidFor(work, now) != nil {
				t.Fatalf("evaluation=%#v error=%v", evaluation, evaluateErr)
			}
		})
	}
}

func TestSandboxStrategyReadinessExecutorUsesMeanReversionDualTimeframeContract(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	clock, err := domain.NewReplayClock(now)
	if err != nil {
		t.Fatal(err)
	}
	work, record := readinessWorkAndConfiguration(t, sandbox.StrategyMeanReversion, now)
	instrument, err := domain.NewSpotInstrument("BTC", "USDT")
	if err != nil {
		t.Fatal(err)
	}
	data := newReadinessMarketData(t, instrument, now, 1000)
	data.candles["1h"] = data.candles["1h"][:999]
	executor, err := NewSandboxStrategyReadinessExecutor(data, clock)
	if err != nil {
		t.Fatal(err)
	}
	evaluation, err := executor.EvaluateStrategySession(context.Background(), work, record, readinessExecutionLease(work), now)
	if err != nil || evaluation.Reason != "waiting_for_public_market_data" ||
		evaluation.State != sandbox.StrategySessionEvaluationWaiting {
		t.Fatalf("evaluation=%#v error=%v", evaluation, err)
	}
}

func TestSandboxStrategyReadinessExecutorRejectsALeaseForAnotherAccount(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	clock, err := domain.NewReplayClock(now)
	if err != nil {
		t.Fatal(err)
	}
	work, record := readinessWorkAndConfiguration(t, sandbox.StrategyTrend, now)
	instrument, err := domain.NewSpotInstrument("BTC", "USDT")
	if err != nil {
		t.Fatal(err)
	}
	executor, err := NewSandboxStrategyReadinessExecutor(newReadinessMarketData(t, instrument, now, 1000), clock)
	if err != nil {
		t.Fatal(err)
	}
	lease := readinessExecutionLease(work)
	lease.Account = "another-account"
	if _, err = executor.EvaluateStrategySession(context.Background(), work, record, lease, now); err == nil {
		t.Fatal("foreign execution lease accepted")
	}
}

func readinessExecutionLease(work sandbox.StrategySessionWork) sandbox.StrategySessionExecutionLease {
	return sandbox.StrategySessionExecutionLease{Account: work.Account.ID, Epoch: work.Account.Epoch,
		Owner: "readiness-test-owner", Fence: 1}
}

func readinessWorkAndConfiguration(
	t *testing.T,
	strategy string,
	now time.Time,
) (sandbox.StrategySessionWork, sandbox.StrategySessionConfiguration) {
	t.Helper()
	product, err := config.DefaultSandboxConfiguration(config.ModeTestnet)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := config.NewSnapshot(product, config.SourceAdmin, "test", &domain.SystemClock{})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(product)
	if err != nil {
		t.Fatal(err)
	}
	work := sandbox.StrategySessionWork{SessionID: "session", Strategy: strategy, Instrument: "BTCUSDT",
		Account:         sandbox.StrategySessionAccount{ID: "binance-account", Epoch: 1, Exchange: sandbox.ExchangeBinance},
		ConfigurationID: "configuration", ConfigurationHash: snapshot.Hash(), StrategySetHash: strings.Repeat("a", 64),
		SessionRevision: 1, StrategyRevision: 1, ArmID: "arm", ArmRevision: 1,
		StartedAt: now.Add(-time.Minute), ArmExpiresAt: now.Add(time.Minute)}
	return work, sandbox.StrategySessionConfiguration{ID: work.ConfigurationID, Hash: snapshot.Hash(), Payload: payload}
}

type readinessMarketData struct {
	metadata exchangecontracts.InstrumentRecord
	book     exchangecontracts.BookSnapshot
	candles  map[string][]exchangecontracts.Candle
}

func (data *readinessMarketData) Snapshot(
	context.Context,
	exchangecontracts.SnapshotRequest,
) (exchangecontracts.BookSnapshot, error) {
	return data.book, nil
}

func (data *readinessMarketData) Subscribe(
	context.Context,
	exchangecontracts.StreamRequest,
) (exchangecontracts.Stream, error) {
	return nil, fmt.Errorf("not used")
}

func (data *readinessMarketData) Instruments(
	context.Context,
	[]domain.Instrument,
) ([]exchangecontracts.InstrumentRecord, error) {
	return []exchangecontracts.InstrumentRecord{data.metadata}, nil
}

func (data *readinessMarketData) Trades(
	context.Context,
	exchangecontracts.HistoryRequest,
) ([]exchangecontracts.Trade, error) {
	return nil, fmt.Errorf("not used")
}

func (data *readinessMarketData) Candles(
	_ context.Context,
	request exchangecontracts.CandleRequest,
) ([]exchangecontracts.Candle, error) {
	return append([]exchangecontracts.Candle(nil), data.candles[request.Interval]...), nil
}

func newReadinessMarketData(
	t *testing.T,
	instrument domain.Instrument,
	now time.Time,
	count int,
) *readinessMarketData {
	t.Helper()
	price, err := domain.ParsePrice("100")
	if err != nil {
		t.Fatal(err)
	}
	quantity, err := domain.ParseQuantity("1")
	if err != nil {
		t.Fatal(err)
	}
	minimum, err := domain.ParseNotional("10")
	if err != nil {
		t.Fatal(err)
	}
	return &readinessMarketData{metadata: exchangecontracts.InstrumentRecord{Metadata: domain.InstrumentMetadata{
		Instrument: instrument, Version: 1, EffectiveAt: now.Add(-time.Hour), PriceTick: price,
		QuantityStep: quantity, MinimumQuantity: quantity, MinimumNotional: minimum,
	}, RawPayloadHash: strings.Repeat("a", 64)}, book: exchangecontracts.BookSnapshot{
		Instrument: instrument, LastSequence: 1, ReceivedAt: domain.EventTime{UTC: now, Sequence: 1},
		Bids: []exchangecontracts.PriceLevel{{Price: price, Quantity: quantity}},
		Asks: []exchangecontracts.PriceLevel{{Price: price, Quantity: quantity}}, RawPayloadHash: strings.Repeat("b", 64),
	}, candles: map[string][]exchangecontracts.Candle{
		"1h": readinessCandles(instrument, "1h", now, price, count),
		"4h": readinessCandles(instrument, "4h", now, price, count),
	}}
}

func readinessCandles(
	instrument domain.Instrument,
	interval string,
	now time.Time,
	price domain.Price,
	count int,
) []exchangecontracts.Candle {
	width := time.Hour
	if interval == "4h" {
		width = 4 * time.Hour
	}
	items := make([]exchangecontracts.Candle, 0, count)
	for index := 0; index < count; index++ {
		closeTime := now.Add(-time.Nanosecond).Add(-time.Duration(count-1-index) * width)
		items = append(items, exchangecontracts.Candle{Instrument: instrument, Interval: interval,
			OpenTime: closeTime.Add(-width), CloseTime: closeTime, Open: price, High: price,
			Low: price, Close: price, Closed: true, RawPayloadHash: strings.Repeat("c", 64)})
	}
	return items
}
