package bootstrap

import (
	"encoding/json"
	"errors"
	"math/big"
	"strings"
	"testing"
	"time"

	"axiom/internal/backtest"
	"axiom/internal/domain"
	"axiom/internal/evaluation"
	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/replay"
	"axiom/internal/strategies/arbitrage"
)

func TestCombinedShadowVerdictUsesLedgerNetSampleAndDrawdownGates(t *testing.T) {
	tests := []struct {
		name    string
		metrics backtest.Metrics
		verdict evaluation.Verdict
		reason  evaluation.ReasonCode
	}{
		{"continue", backtest.Metrics{TotalNetReturn: "0.01", MaximumDrawdown: "0.02", Trades: 20}, evaluation.VerdictContinue, "SHADOW_GATE_PASSED"},
		{"drawdown", backtest.Metrics{TotalNetReturn: "0.01", MaximumDrawdown: "0.0301", Trades: 40}, evaluation.VerdictReject, "SHADOW_DRAWDOWN_LIMIT_EXCEEDED"},
		{"sample", backtest.Metrics{TotalNetReturn: "0.01", MaximumDrawdown: "0.01", Trades: 19}, evaluation.VerdictImprove, "SHADOW_SAMPLE_INSUFFICIENT"},
		{"loss", backtest.Metrics{TotalNetReturn: "-0.0001", MaximumDrawdown: "0.01", Trades: 40}, evaluation.VerdictReject, "SHADOW_NET_RESULT_NOT_POSITIVE"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			payload, err := json.Marshal(test.metrics)
			if err != nil {
				t.Fatal(err)
			}
			verdict, reason := combinedShadowVerdict(evaluation.StrategyTrend, payload)
			if verdict != test.verdict || reason != test.reason {
				t.Fatalf("verdict=%s reason=%s", verdict, reason)
			}
		})
	}
	verdict, reason := combinedShadowVerdict(evaluation.StrategyTrend, []byte("not-json"))
	if verdict != evaluation.VerdictBlocked || reason != evaluation.ReasonAccountingFailed {
		t.Fatalf("invalid metrics verdict=%s reason=%s", verdict, reason)
	}
}

func TestShadowDecisionMaterialAndSharedFailureClassification(t *testing.T) {
	for _, test := range []struct {
		decision string
		material bool
	}{{"", false}, {`{"action":"observation_only"}`, false}, {`{"action":"no_action"}`, true}, {`{"action":"buy"}`, true}, {`not-json`, true}} {
		if got := shadowDecisionMaterial(backtest.EventResult{Decision: []byte(test.decision)}); got != test.material {
			t.Fatalf("decision=%q material=%t", test.decision, got)
		}
	}
	if !sharedShadowFailure(errors.New("evaluation_shadow_allocator_conflict")) ||
		!sharedShadowFailure(errors.New("dataset_hash_invalid")) ||
		sharedShadowFailure(errors.New("single_strategy_signal_invalid")) || sharedShadowFailure(nil) {
		t.Fatal("shared failure classification is not fail-closed and isolated")
	}
}

func TestFloorEvaluationDecimalNeverRoundsAboveCapital(t *testing.T) {
	value := new(big.Rat).SetFrac(big.NewInt(10), big.NewInt(3))
	floored, ok := new(big.Rat).SetString(floorEvaluationDecimal(value, 2))
	if !ok || floored.String() != "333/100" || floored.Cmp(value) > 0 {
		t.Fatalf("floored=%v ok=%t", floored, ok)
	}
	if floorEvaluationDecimal(nil, 18) != "0" || floorEvaluationDecimal(big.NewRat(-1, 1), 18) != "0" {
		t.Fatal("invalid capital was not rejected")
	}
}

func TestCombinedShadowLiquidityIsScopedToTheExactExchange(t *testing.T) {
	instruments, err := evaluationInstruments()
	if err != nil {
		t.Fatal(err)
	}
	instrument := instruments["BTCUSDT"]
	price, _ := domain.ParsePrice("100")
	askPrice, _ := domain.ParsePrice("101")
	one, _ := domain.ParseQuantity("1")
	hundred, _ := domain.ParseQuantity("100")
	processor := &evaluationMarketProcessor{books: map[string]*evaluationBook{
		evaluationBookKey("binance", instrument): {exchange: "binance", instrument: instrument, valid: true,
			bids: map[string]exchangecontracts.PriceLevel{"100": {Price: price, Quantity: one}},
			asks: map[string]exchangecontracts.PriceLevel{"101": {Price: askPrice, Quantity: one}}},
		evaluationBookKey("bybit", instrument): {exchange: "bybit", instrument: instrument, valid: true,
			bids: map[string]exchangecontracts.PriceLevel{"100": {Price: price, Quantity: hundred}},
			asks: map[string]exchangecontracts.PriceLevel{"101": {Price: askPrice, Quantity: hundred}}},
	}}
	two, _ := domain.ParseQuantity("2")
	consumption := map[string]domain.Quantity{
		combinedShadowLiquidityKey("binance", instrument, domain.SideBuy): two,
	}
	if err = validateCombinedLiquidity(processor, consumption); err == nil ||
		err.Error() != "evaluation_shadow_shared_liquidity_exceeded" {
		t.Fatalf("exchange-scoped liquidity error=%v", err)
	}
	if err = validateCombinedLiquidity(processor, map[string]domain.Quantity{"BTCUSDT:buy": one}); err == nil ||
		err.Error() != "evaluation_shadow_liquidity_key_invalid" {
		t.Fatalf("malformed liquidity key error=%v", err)
	}
}

func TestEvaluationReplayGapInvalidatesOnlyAffectedBookUntilFreshSnapshot(t *testing.T) {
	instruments, err := evaluationInstruments()
	if err != nil {
		t.Fatal(err)
	}
	instrument := instruments["ETHUSDT"]
	processor := &evaluationMarketProcessor{books: map[string]*evaluationBook{
		evaluationBookKey("binance", instrument): {exchange: "binance", instrument: instrument, valid: true, sequence: 100},
		evaluationBookKey("bybit", instrument):   {exchange: "bybit", instrument: instrument, valid: true, sequence: 200},
	}}
	now := time.Date(2030, 8, 11, 12, 0, 0, 0, time.UTC)
	gapPayload, _ := json.Marshal(exchangecontracts.SourceGap{Exchange: "binance", Instrument: instrument,
		ConnectionGeneration: 2, FirstSequence: 101, LastSequence: 104,
		StartedAt: now, EndedAt: now, Reason: "sequence_gap"})
	if _, err = processor.reducePublicEvidence(replay.Event{Ordinal: 1, LogicalTime: 1, Canonical: gapPayload}); err != nil {
		t.Fatal(err)
	}
	if processor.books[evaluationBookKey("binance", instrument)].valid ||
		!processor.books[evaluationBookKey("bybit", instrument)].valid {
		t.Fatal("gap did not isolate the affected replay book")
	}

	bid, _ := domain.ParsePrice("100")
	ask, _ := domain.ParsePrice("101")
	quantity, _ := domain.ParseQuantity("1")
	snapshotPayload, _ := json.Marshal(exchangecontracts.BookSnapshot{Exchange: "binance", Instrument: instrument,
		LastSequence: 500, ReceivedAt: domain.EventTime{UTC: now.Add(time.Second), Sequence: 1},
		Bids: []exchangecontracts.PriceLevel{{Price: bid, Quantity: quantity}},
		Asks: []exchangecontracts.PriceLevel{{Price: ask, Quantity: quantity}}, RawPayloadHash: strings.Repeat("a", 64)})
	if _, err = processor.reducePublicEvidence(replay.Event{Ordinal: 2, LogicalTime: 2, Canonical: snapshotPayload}); err != nil {
		t.Fatal(err)
	}
	book := processor.books[evaluationBookKey("binance", instrument)]
	if !book.valid || book.sequence != 500 {
		t.Fatalf("fresh snapshot did not restore replay book: %#v", book)
	}
}

func TestCombinedShadowFillVenueComesFromRecordedEvidence(t *testing.T) {
	instruments, err := evaluationInstruments()
	if err != nil {
		t.Fatal(err)
	}
	instrument := instruments["BTCUSDT"]
	candle := exchangecontracts.Candle{Exchange: "bybit", Instrument: instrument, Interval: "15m",
		OpenTime: time.Unix(1_700_000_000, 0).UTC()}
	payload, _ := json.Marshal(exchangecontracts.StreamEvent{Kind: "candle", Candle: &candle})
	exchange, err := combinedShadowOrderExchange(replay.Event{Canonical: payload})
	if err != nil || exchange != "bybit" {
		t.Fatalf("exchange=%q error=%v", exchange, err)
	}
	invalid, _ := json.Marshal(exchangecontracts.StreamEvent{Kind: "candle"})
	if _, err = combinedShadowOrderExchange(replay.Event{Canonical: invalid}); err == nil {
		t.Fatal("fill without immutable exchange evidence was accepted")
	}

	runtime := &evaluationCombinedShadowRuntime{seenFills: make(map[string]string)}
	quantity, _ := domain.ParseQuantity("0.5")
	consumption := make(map[string]domain.Quantity)
	if err = consumeCombinedArbitrageFill(runtime, "member", "fill", arbitrage.Result{
		Exchange: "bybit", Instrument: instrument, Side: domain.SideSell, TradeQuantity: quantity,
	}, consumption); err != nil {
		t.Fatal(err)
	}
	if consumption[combinedShadowLiquidityKey("bybit", instrument, domain.SideSell)].String() != "0.5" {
		t.Fatalf("venue-scoped consumption=%v", consumption)
	}
	if err = consumeCombinedArbitrageFill(runtime, "member", "bad-fill", arbitrage.Result{
		Exchange: "", Instrument: instrument, Side: domain.SideSell, TradeQuantity: quantity,
	}, consumption); err == nil {
		t.Fatal("multileg fill without an exchange was accepted")
	}
}
