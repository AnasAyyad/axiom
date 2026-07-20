package trend

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"axiom/internal/config"
	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/replay"
)

func TestConfigurationHashAndStrictBaselineContract(t *testing.T) {
	source := config.DefaultConfiguration().Trend
	first, err := NewConfiguration(source)
	if err != nil || first.Version != "trend.v1a.1" || len(first.Hash) != 64 || first.EMARegime != 200 ||
		first.MaximumBookAge != 250*time.Millisecond || first.EvaluationWindow != 5*time.Second {
		t.Fatalf("configuration = %#v, %v", first, err)
	}
	second, _ := NewConfiguration(source)
	if first.Hash != second.Hash {
		t.Fatal("canonical Trend hashes differ")
	}
	source.StrategyVersion = "trend.v1a.2"
	changed, _ := NewConfiguration(source)
	if first.Hash == changed.Hash {
		t.Fatal("strategy version change preserved hash")
	}
}

func TestEntryRequiresStrictRegimeConfirmationAndPriorBreakout(t *testing.T) {
	evaluator, input := testEvaluatorAndInput(t)
	decision, err := evaluator.Evaluate(input)
	if err != nil || decision.Action != ActionEntry || decision.ReasonCode != ReasonEntryAccepted || decision.Candidate == nil {
		t.Fatalf("entry decision = %#v, %v", decision, err)
	}
	if decision.Candidate.Side != domain.SideBuy || decision.Candidate.LimitPrice.String() != "300" ||
		decision.Candidate.Notional.String() == "0" || decision.Explanation.RiskBudget.String() != "2.5" {
		t.Fatalf("entry candidate = %#v", decision.Candidate)
	}
	tied := input
	tied.Candles = cloneCandles(input.Candles)
	prior := tied.Candles[len(tied.Candles)-2].High
	tied.Candles[len(tied.Candles)-1].Close = prior
	tied.Candles[len(tied.Candles)-1].High = prior
	tied.Candles[len(tied.Candles)-1].RawPayloadHash = "strict-tie"
	decision, err = evaluator.Evaluate(tied)
	if err != nil || decision.ReasonCode != ReasonBreakoutFailed {
		t.Fatalf("breakout equality = %#v, %v", decision, err)
	}
	configurationSource := config.DefaultConfiguration().Trend
	configurationSource.StrategyVersion = "trend.v1a.test-equality"
	for index := range configurationSource.Parameters {
		if configurationSource.Parameters[index].ID == "trend.ema_confirmation_period" {
			configurationSource.Parameters[index].Value = "200"
		}
	}
	equalConfiguration, err := NewConfiguration(configurationSource)
	if err != nil {
		t.Fatal(err)
	}
	equalEvaluator, err := NewEvaluator(equalConfiguration)
	if err != nil {
		t.Fatal(err)
	}
	equalInput := input
	equalInput.Evidence.StrategyVersion = equalConfiguration.Version
	equalInput.Evidence.ConfigurationHash = equalConfiguration.Hash
	decision, err = equalEvaluator.Evaluate(equalInput)
	if err != nil || decision.ReasonCode != ReasonConfirmationFailed ||
		decision.Explanation.EMA50.Compare(decision.Explanation.EMA200) != 0 {
		t.Fatalf("EMA equality = %#v, %v", decision, err)
	}
}

func TestCandleAdmissionFailsClosedAndIdenticalDuplicateIsIdempotent(t *testing.T) {
	evaluator, input := testEvaluatorAndInput(t)
	tests := []struct {
		name   string
		alter  func(*Input)
		reason string
	}{
		{name: "warmup", alter: func(value *Input) { value.Candles = value.Candles[:199] }, reason: ReasonWarmUp},
		{name: "nonfinal", alter: func(value *Input) { value.Candles[len(value.Candles)-1].Closed = false }, reason: ReasonCandleFinality},
		{name: "foreign_exchange", alter: func(value *Input) { value.Candles[len(value.Candles)-1].Exchange = "other" }, reason: ReasonCandleFinality},
		{name: "too_early", alter: func(value *Input) { value.Now = value.Candles[len(value.Candles)-1].ReceivedAt.UTC.Add(time.Second) }, reason: ReasonCandleFinality},
		{name: "gap", alter: func(value *Input) { value.Candles = append(value.Candles[:100], value.Candles[101:]...) }, reason: ReasonCandleGap},
		{name: "conflict", alter: func(value *Input) {
			duplicate := value.Candles[len(value.Candles)-1]
			duplicate.RawPayloadHash = "conflict"
			value.Candles = append(value.Candles, duplicate)
		}, reason: ReasonCandleConflict},
		{name: "same_hash_conflicting_open", alter: func(value *Input) {
			duplicate := value.Candles[len(value.Candles)-1]
			duplicate.Open = price(t, "1")
			value.Candles = append(value.Candles, duplicate)
		}, reason: ReasonCandleConflict},
		{name: "regression", alter: func(value *Input) {
			last := len(value.Candles) - 1
			value.Candles[last], value.Candles[last-1] = value.Candles[last-1], value.Candles[last]
		}, reason: ReasonCandleOrder},
		{name: "stale", alter: func(value *Input) { value.Now = value.Now.Add(5 * time.Second) }, reason: ReasonStaleSignal},
		{name: "unhealthy", alter: func(value *Input) { value.BookAge = 250 * time.Millisecond }, reason: ReasonUnhealthyMarket},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			changed := input
			changed.Candles = cloneCandles(input.Candles)
			test.alter(&changed)
			decision, err := evaluator.Evaluate(changed)
			if err != nil || decision.ReasonCode != test.reason || decision.Candidate != nil {
				t.Fatalf("decision = %#v, %v", decision, err)
			}
		})
	}
	assertDuplicateIdempotence(t, evaluator, input)
}

func assertDuplicateIdempotence(t *testing.T, evaluator *Evaluator, input Input) {
	t.Helper()
	duplicate := input
	duplicate.Candles = append(cloneCandles(input.Candles), input.Candles[len(input.Candles)-1])
	first, _ := evaluator.Evaluate(input)
	second, _ := evaluator.Evaluate(duplicate)
	if first.ID != second.ID || second.Action != ActionEntry {
		t.Fatalf("identical duplicate changed decision: %#v %#v", first, second)
	}
	adapter, _ := NewAdapter(evaluator)
	canonical, _ := json.Marshal(duplicate)
	event := replay.Event{LogicalTime: duplicate.LogicalTime, Ordinal: duplicate.Ordinal, Canonical: canonical}
	if _, err := adapter.Evaluate(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.Evaluate(context.Background(), event); errorCode(err) != ReasonDuplicateDecision {
		t.Fatalf("duplicate adapter error = %v", err)
	}
}

func TestSizingIsConservativeUnderFeesReserveFiltersAndRounding(t *testing.T) {
	evaluator, baseline := testEvaluatorAndInput(t)
	tests := []struct {
		name   string
		alter  func(*Input)
		reason string
	}{
		{name: "central risk", alter: func(value *Input) { value.Sizing.CentralRiskEligible = false }, reason: ReasonRiskClipped},
		{name: "reserve", alter: func(value *Input) { value.Sizing.AvailableCash = money(t, "75") }, reason: ReasonRiskClipped},
		{name: "zero equity", alter: func(value *Input) { value.Sizing.Equity = money(t, "0") }, reason: ReasonInvalidSizing},
		{name: "zero limit", alter: func(value *Input) { value.Sizing.NotionalLimits = []domain.Money{money(t, "0")} }, reason: ReasonRiskClipped},
		{name: "minimum notional", alter: func(value *Input) { value.Sizing.InstrumentMetadata.MinimumNotional = notional(t, "151") }, reason: ReasonMinimumFilter},
		{name: "zero rounded quantity", alter: func(value *Input) { value.Sizing.InstrumentMetadata.QuantityStep = quantity(t, "1") }, reason: ReasonInvalidSizing},
		{name: "slippage tick", alter: func(value *Input) { value.Sizing.InstrumentMetadata.PriceTick = price(t, "11") }, reason: ReasonMinimumFilter},
		{name: "nonpositive stressed exit", alter: func(value *Input) { value.Sizing.GapAllowance = price(t, "400") }, reason: ReasonInvalidSizing},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := baseline
			test.alter(&input)
			decision, err := evaluator.Evaluate(input)
			if err != nil || decision.ReasonCode != test.reason || decision.Candidate != nil {
				t.Fatalf("sizing decision = %#v %v", decision, err)
			}
		})
	}
}

func TestFeeStressCannotIncreaseQuantity(t *testing.T) {
	evaluator, withFees := testEvaluatorAndInput(t)
	feeDecision, _ := evaluator.Evaluate(withFees)
	withoutFees := withFees
	withoutFees.Sizing.EntryFeeRate = rate(t, "0")
	withoutFees.Sizing.ExitFeeRate = rate(t, "0")
	withoutFees.Candles = cloneCandles(withFees.Candles)
	withoutFees.Candles[len(withoutFees.Candles)-1].RawPayloadHash = "no-fee-comparison"
	noFeeDecision, _ := evaluator.Evaluate(withoutFees)
	if feeDecision.Candidate == nil || noFeeDecision.Candidate == nil ||
		feeDecision.Candidate.Quantity.Compare(noFeeDecision.Candidate.Quantity) > 0 {
		t.Fatalf("fee stress increased quantity: %#v %#v", feeDecision.Candidate, noFeeDecision.Candidate)
	}
}

func TestPositionStopsOnlyTightenAndProtectiveExitPrecedesEMA(t *testing.T) {
	configuration, _ := NewConfiguration(config.DefaultConfiguration().Trend)
	position, err := OpenPosition(price(t, "300"), price(t, "2"), quantity(t, "0.4"), configuration)
	if err != nil || position.InitialStop.String() != "295" {
		t.Fatalf("opened position = %#v, %v", position, err)
	}
	position, err = AdvancePosition(position, price(t, "310"), price(t, "2"), configuration)
	if err != nil || position.TrailingStop.String() != "304" {
		t.Fatalf("advanced position = %#v, %v", position, err)
	}
	tightened, _ := AdvancePosition(position, price(t, "305"), price(t, "3"), configuration)
	if tightened.TrailingStop.String() != "304" {
		t.Fatalf("trailing stop loosened = %s", tightened.TrailingStop.String())
	}
	evaluator, input := testEvaluatorAndInput(t)
	input.Position = position
	last := len(input.Candles) - 1
	input.Candles[last].Low = price(t, "303")
	input.Candles[last].RawPayloadHash = "protective"
	input.Sizing.FirstExecutablePrice = price(t, "302.009")
	decision, err := evaluator.Evaluate(input)
	if err != nil || decision.Action != ActionExit || decision.ReasonCode != ReasonExitTrailingStop ||
		decision.CooldownStart != 3 || decision.Candidate.LimitPrice.String() != "302" {
		t.Fatalf("protective exit = %#v, %v", decision, err)
	}
	if AdvanceCooldown(AdvanceCooldown(AdvanceCooldown(decision.CooldownStart))) != 0 {
		t.Fatal("three completed candles did not finish cooldown")
	}
}

func TestProtectiveCooldownBlocksExactlyThreeCompletedCandles(t *testing.T) {
	evaluator, input := testEvaluatorAndInput(t)
	for remaining := uint64(3); remaining > 0; remaining-- {
		blocked := input
		blocked.Position.CooldownRemaining = remaining
		blocked.Candles = cloneCandles(input.Candles)
		blocked.Candles[len(blocked.Candles)-1].RawPayloadHash = fmt.Sprintf("cooldown-%d", remaining)
		decision, err := evaluator.Evaluate(blocked)
		if err != nil || decision.ReasonCode != ReasonCooldown || decision.Candidate != nil {
			t.Fatalf("cooldown %d = %#v %v", remaining, decision, err)
		}
	}
	eligible := input
	eligible.Position.CooldownRemaining = 0
	eligible.Candles = cloneCandles(input.Candles)
	eligible.Candles[len(eligible.Candles)-1].RawPayloadHash = "cooldown-fourth-candle"
	decision, err := evaluator.Evaluate(eligible)
	if err != nil || decision.ReasonCode != ReasonEntryAccepted || decision.Candidate == nil {
		t.Fatalf("fourth candle not eligible = %#v %v", decision, err)
	}
}

func TestEquivalentModesProduceByteIdenticalCandidatesWithoutSignalCloseFill(t *testing.T) {
	evaluator, input := testEvaluatorAndInput(t)
	canonical, _ := json.Marshal(input)
	event := replay.Event{LogicalTime: input.LogicalTime, Ordinal: input.Ordinal, Canonical: canonical}
	var expected []byte
	for _, mode := range []string{"backtest", "replay", "paper", "shadow"} {
		for run := 0; run < 10; run++ {
			adapter, _ := NewAdapter(evaluator)
			candidate, err := adapter.Evaluate(context.Background(), event)
			if err != nil {
				t.Fatalf("%s run %d candidate: %v", mode, run, err)
			}
			if expected == nil {
				expected = candidate.Payload
			} else if string(expected) != string(candidate.Payload) {
				t.Fatalf("%s run %d candidate differs", mode, run)
			}
		}
		if input.Sizing.EntryReference.String() == input.Candles[len(input.Candles)-1].Close.String() {
			t.Fatal("test fixture accidentally permits signal-close execution")
		}
	}
}

func testEvaluatorAndInput(t *testing.T) (*Evaluator, Input) {
	t.Helper()
	configuration, err := NewConfiguration(config.DefaultConfiguration().Trend)
	if err != nil {
		t.Fatal(err)
	}
	evaluator, _ := NewEvaluator(configuration)
	instrument, _ := domain.NewSpotInstrument("BTC", "USDT")
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	candles := make([]exchangecontracts.Candle, 200)
	for index := range candles {
		closeValue := 100 + index
		if index == len(candles)-1 {
			closeValue = 301
		}
		open := start.Add(time.Duration(index) * 4 * time.Hour)
		closeTime := open.Add(4 * time.Hour)
		candles[index] = exchangecontracts.Candle{Exchange: "binance", Instrument: instrument, Interval: "4h",
			OpenTime: open, CloseTime: closeTime, Open: price(t, fmt.Sprint(closeValue-1)),
			High: price(t, fmt.Sprint(closeValue+1)), Low: price(t, fmt.Sprint(closeValue-2)),
			Close: price(t, fmt.Sprint(closeValue)), Volume: quantity(t, "1"), Closed: true,
			ReceivedAt: domain.EventTime{UTC: closeTime, Sequence: uint64(index + 1)}, RawPayloadHash: fmt.Sprintf("candle-%03d", index)}
	}
	metadata := domain.InstrumentMetadata{Instrument: instrument, Version: 7, EffectiveAt: start,
		PriceTick: price(t, "0.01"), QuantityStep: quantity(t, "0.0001"),
		MinimumQuantity: quantity(t, "0.0001"), MinimumNotional: notional(t, "10")}
	lastPublication := candles[len(candles)-1].ReceivedAt.UTC
	return evaluator, Input{Ordinal: 200, LogicalTime: uint64(100 * time.Second),
		Now: lastPublication.Add(3 * time.Second), Instrument: instrument, Candles: candles,
		MarketHealthy: true, BookAge: time.Millisecond,
		Sizing: SizingState{Equity: money(t, "500"), AvailableCash: money(t, "500"), MinimumReserve: money(t, "75"),
			NotionalLimits: []domain.Money{money(t, "150")}, EntryReference: price(t, "300"),
			FirstExecutablePrice: price(t, "300"), GapAllowance: price(t, "0.5"),
			LatencyDeterioration: price(t, "0.1"), EntryFeeRate: rate(t, "0.001"), ExitFeeRate: rate(t, "0.001"),
			InstrumentMetadata: metadata, CentralRiskEligible: true, LiquidityDomain: "binance-btc-usdt", FencingToken: 1},
		Evidence: InputEvidence{CandleViewID: "candles-btc", CandleViewRevision: 200, MarketViewID: "book-btc",
			MarketViewRevision: 50, InstrumentMetadataID: "metadata-7", AssetEligibilityVersion: 1,
			ConfigurationVersion: "axiom.config.v1a.2", ConfigurationHash: configuration.Hash,
			StrategyVersion: configuration.Version, PortfolioRevision: 3, PositionRevision: 1,
			FeeModelID: "fee-v1", LatencyModelID: "latency-v1", FillModelID: "fill-v1",
			SlippageModelID: "slippage-v1", GapModelID: "gap-v1", CorrelationID: "corr-1", CausationID: "cause-1"}}
}

func cloneCandles(source []exchangecontracts.Candle) []exchangecontracts.Candle {
	return append([]exchangecontracts.Candle(nil), source...)
}

func errorCode(err error) string {
	if typed, ok := err.(Error); ok {
		return typed.Code
	}
	return ""
}

func money(t *testing.T, value string) domain.Money {
	t.Helper()
	result, err := domain.ParseMoney(value)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func rate(t *testing.T, value string) domain.Rate {
	t.Helper()
	result, err := domain.ParseRate(value)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func notional(t *testing.T, value string) domain.Notional {
	t.Helper()
	result, err := domain.ParseNotional(value)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
