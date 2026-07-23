package meanreversion

import (
	"bytes"
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"axiom/internal/config"
	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/replay"
)

func TestBaselineEntryUsesExactRegimeThresholdsAndPostLatencyPrice(t *testing.T) {
	evaluator, _ := evaluatorFixture(t)
	input := baselineInput(t)
	decision, err := evaluator.Evaluate(input)
	if err != nil || decision.Action != ActionEntry || decision.ReasonCode != ReasonEntryAccepted || decision.Candidate == nil {
		t.Fatalf("entry decision = %#v, %v (adx=%s z=%s)", decision, err,
			decision.Explanation.ADX14, decision.Explanation.ZScore)
	}
	if decision.Candidate.LimitPrice.Compare(input.PrimaryCandles[len(input.PrimaryCandles)-1].Close) == 0 ||
		decision.Candidate.LimitPrice.String() != input.Sizing.FirstExecutablePrice.String() ||
		decision.Explanation.RiskBudget.String() != "1.25" || decision.Explanation.Regime != "range_or_constructive" {
		t.Fatalf("entry sizing/evidence = %#v", decision)
	}
}

func TestConfigurationRequiresCompleteApprovedMetadataAndATRStopUsesActualFill(t *testing.T) {
	source := config.DefaultV1BConfiguration().MeanReversion
	source.Parameters[0].ApprovalActor = ""
	if _, err := NewConfiguration(source); err == nil {
		t.Fatal("incomplete parameter approval metadata accepted")
	}
	_, configuration := evaluatorFixture(t)
	entry, _ := domain.ParsePrice("100")
	atr, _ := domain.ParsePrice("2")
	quantity, _ := domain.ParseQuantity("0.2")
	position, err := OpenPosition(entry, atr, quantity, configuration)
	if err != nil || position.InitialStop.String() != "95" {
		t.Fatalf("actual-fill ATR stop = %s, %v", position.InitialStop.String(), err)
	}
}

func TestEveryEntryThresholdEqualityAndOneUnitEitherSide(t *testing.T) {
	evaluator, configuration := evaluatorFixture(t)
	input := baselineInput(t)
	safe := indicatorSnapshot{regime: "range_or_constructive"}
	safe.zscore, _ = parseDecimal("-2")
	safe.adx, _ = parseDecimal("24.999999999999999999")
	if reason := evaluator.entryAdmission(input, safe); reason != "" {
		t.Fatalf("entry equality rejected: %s", reason)
	}
	safe.zscore, _ = parseDecimal("-1.999999999999999999")
	if reason := evaluator.entryAdmission(input, safe); reason != ReasonEntryThreshold {
		t.Fatalf("entry one unit above = %s", reason)
	}
	safe.zscore, _ = parseDecimal("-2.000000000000000001")
	if reason := evaluator.entryAdmission(input, safe); reason != "" {
		t.Fatalf("entry one unit below = %s", reason)
	}
	safe.zscore = configuration.EntryZScore
	for _, row := range []struct {
		adx, reason string
	}{{"24.999999999999999999", ""}, {"25", ReasonADX}, {"25.000000000000000001", ReasonADX}} {
		safe.adx, _ = parseDecimal(row.adx)
		if reason := evaluator.entryAdmission(input, safe); reason != row.reason {
			t.Fatalf("ADX %s = %s, want %s", row.adx, reason, row.reason)
		}
	}
	input.Spread, _ = domain.ParsePercent("0.001")
	safe.adx, _ = parseDecimal("24")
	if reason := evaluator.entryAdmission(input, safe); reason != "" {
		t.Fatalf("spread equality rejected: %s", reason)
	}
	input.Spread, _ = domain.ParsePercent("0.001000000000000001")
	if reason := evaluator.entryAdmission(input, safe); reason != ReasonSpread {
		t.Fatalf("spread one unit above = %s", reason)
	}
}

func TestProtectiveNormalAndHoldingExitPrecedenceAtEveryBoundary(t *testing.T) {
	evaluator, configuration := evaluatorFixture(t)
	input := baselineInput(t)
	latest := input.PrimaryCandles[len(input.PrimaryCandles)-1]
	input.Position = openPositionFixture(t)
	explanation := Explanation{Attributes: map[string]string{}}
	snapshot := indicatorSnapshot{}

	// The low equals the stop while the close is normalized: protection wins.
	input.Position.InitialStop = latest.Low
	snapshot.zscore, _ = parseDecimal("-0.25")
	decision := evaluator.evaluateExit(input, latest, snapshot, explanation)
	if decision.ReasonCode != ReasonExitATRStop || decision.CooldownStart != 3 {
		t.Fatalf("ambiguous exit precedence = %#v", decision)
	}
	oneUnitLower, _ := domain.ParsePrice("93.999999999999999999")
	input.Position.InitialStop = oneUnitLower
	snapshot.zscore, _ = parseDecimal("-3.5")
	decision = evaluator.evaluateExit(input, latest, snapshot, explanation)
	if decision.ReasonCode != ReasonExitProtectiveZScore || decision.CooldownStart != 3 {
		t.Fatalf("protective z equality = %#v", decision)
	}
	snapshot.zscore, _ = parseDecimal("-3.499999999999999999")
	decision = evaluator.evaluateExit(input, latest, snapshot, explanation)
	if decision.Action != ActionNone {
		t.Fatalf("protective z one unit safe exited = %#v", decision)
	}
	snapshot.zscore = configuration.NormalExitZScore
	decision = evaluator.evaluateExit(input, latest, snapshot, explanation)
	if decision.ReasonCode != ReasonExitNormalZScore || decision.CooldownStart != 0 {
		t.Fatalf("normal equality = %#v", decision)
	}
	snapshot.zscore, _ = parseDecimal("-0.250000000000000001")
	input.Position.HeldCandles = 11
	decision = evaluator.evaluateExit(input, latest, snapshot, explanation)
	if decision.Action != ActionNone {
		t.Fatalf("normal one unit below or holding 11 exited = %#v", decision)
	}
	input.Position.HeldCandles = 12
	decision = evaluator.evaluateExit(input, latest, snapshot, explanation)
	if decision.ReasonCode != ReasonExitMaximumHolding {
		t.Fatalf("holding equality = %#v", decision)
	}
}

func TestExactlyThreeCooldownCandlesAndNoAveragingDecision(t *testing.T) {
	_, configuration := evaluatorFixture(t)
	remaining := CooldownAfterProtectiveExit(configuration)
	if remaining != 3 {
		t.Fatalf("cooldown = %d", remaining)
	}
	for expected := uint64(2); ; expected-- {
		remaining = AdvanceCooldown(remaining)
		if remaining != expected {
			t.Fatalf("cooldown = %d, want %d", remaining, expected)
		}
		if expected == 0 {
			break
		}
	}
	evaluator, _ := evaluatorFixture(t)
	input := baselineInput(t)
	input.Position = openPositionFixture(t)
	decision, err := evaluator.Evaluate(input)
	if err != nil || decision.Action == ActionEntry {
		t.Fatalf("open position averaged down: %#v, %v", decision, err)
	}
}

func TestDualTimeframeAdmissionFinalityGapRegressionConflictDuplicateAndAlignment(t *testing.T) {
	_, configuration := evaluatorFixture(t)
	baseline := baselineInput(t)
	if _, _, reason := admitCandles(baseline, configuration); reason != "" {
		t.Fatalf("baseline admission = %s", reason)
	}
	identical := baseline
	identical.PrimaryCandles = append(append([]exchangecontracts.Candle(nil), baseline.PrimaryCandles...), baseline.PrimaryCandles[len(baseline.PrimaryCandles)-1])
	if primary, _, reason := admitCandles(identical, configuration); reason != "" || len(primary) != len(baseline.PrimaryCandles) {
		t.Fatalf("identical duplicate = %d/%s", len(primary), reason)
	}
	tests := []struct {
		name   string
		alter  func(*Input)
		reason string
	}{
		{name: "gap", alter: func(in *Input) { in.PrimaryCandles = append(in.PrimaryCandles[:10], in.PrimaryCandles[11:]...) }, reason: ReasonCandleGap},
		{name: "regression", alter: func(in *Input) { in.PrimaryCandles[10] = in.PrimaryCandles[8] }, reason: ReasonCandleOrder},
		{name: "conflict", alter: func(in *Input) {
			duplicate := in.PrimaryCandles[len(in.PrimaryCandles)-1]
			duplicate.Close, _ = domain.ParsePrice("96")
			in.PrimaryCandles = append(in.PrimaryCandles, duplicate)
		}, reason: ReasonCandleConflict},
		{name: "incomplete", alter: func(in *Input) { in.PrimaryCandles[len(in.PrimaryCandles)-1].Closed = false }, reason: ReasonCandleFinality},
		{name: "unobserved history", alter: func(in *Input) {
			in.PrimaryCandles[0].ReceivedAt.UTC = in.Now.Add(time.Nanosecond)
		}, reason: ReasonCandleFinality},
		{name: "alignment", alter: func(in *Input) {
			in.HigherCandles[len(in.HigherCandles)-1].OpenTime = in.HigherCandles[len(in.HigherCandles)-1].OpenTime.Add(4 * time.Hour)
			in.HigherCandles[len(in.HigherCandles)-1].CloseTime = in.HigherCandles[len(in.HigherCandles)-1].CloseTime.Add(4 * time.Hour)
			in.HigherCandles[len(in.HigherCandles)-1].ReceivedAt.UTC = in.HigherCandles[len(in.HigherCandles)-1].ReceivedAt.UTC.Add(4 * time.Hour)
		}, reason: ReasonCandleGap},
		{name: "finalization", alter: func(in *Input) { in.Now = in.PrimaryCandles[len(in.PrimaryCandles)-1].ReceivedAt.UTC.Add(time.Second) }, reason: ReasonCandleFinality},
		{name: "stale", alter: func(in *Input) {
			in.Now = in.PrimaryCandles[len(in.PrimaryCandles)-1].ReceivedAt.UTC.Add(configuration.FinalizationDelay + configuration.EvaluationWindow)
		}, reason: ReasonStaleSignal},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := cloneInput(baseline)
			test.alter(&input)
			if _, _, reason := admitCandles(input, configuration); reason != test.reason {
				t.Fatalf("reason = %s, want %s", reason, test.reason)
			}
		})
	}
}

func TestMarketQualityStalenessPauseSpreadAndDangerousRegimeFailClosed(t *testing.T) {
	evaluator, _ := evaluatorFixture(t)
	snapshot := indicatorSnapshot{regime: "range_or_constructive"}
	snapshot.zscore, _ = parseDecimal("-2")
	snapshot.adx, _ = parseDecimal("24")
	baseline := baselineInput(t)
	tests := []struct {
		name   string
		alter  func(*Input, *indicatorSnapshot)
		reason string
	}{
		{name: "health", alter: func(in *Input, _ *indicatorSnapshot) { in.MarketHealthy = false }, reason: ReasonUnhealthyMarket},
		{name: "stale", alter: func(in *Input, _ *indicatorSnapshot) { in.BookAge = 250 * time.Millisecond }, reason: ReasonUnhealthyMarket},
		{name: "quality", alter: func(in *Input, _ *indicatorSnapshot) { in.MarketDataQualityPass = false }, reason: ReasonMarketQuality},
		{name: "pause", alter: func(in *Input, _ *indicatorSnapshot) { in.ExchangeRiskPaused = true }, reason: ReasonRiskPause},
		{name: "spread", alter: func(in *Input, _ *indicatorSnapshot) { in.Spread, _ = domain.ParsePercent("0.001000000000000001") }, reason: ReasonSpread},
		{name: "regime", alter: func(_ *Input, state *indicatorSnapshot) { state.regime = "dangerous_decline" }, reason: ReasonDangerousRegime},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input, state := cloneInput(baseline), snapshot
			test.alter(&input, &state)
			if reason := evaluator.entryAdmission(input, state); reason != test.reason {
				t.Fatalf("reason = %s, want %s", reason, test.reason)
			}
		})
	}
}

func TestSizingFeesGapSlippageReserveFiltersRoundingAndPostLatencyBoundaries(t *testing.T) {
	evaluator, _ := evaluatorFixture(t)
	baseline := baselineInput(t)
	tests := []struct {
		name  string
		alter func(*Input)
	}{
		{name: "signal close fill", alter: func(in *Input) {
			in.Sizing.FirstExecutableAt = in.PrimaryCandles[len(in.PrimaryCandles)-1].OpenTime.Add(time.Hour)
		}},
		{name: "future executable observation", alter: func(in *Input) {
			in.Sizing.FirstExecutableAt = in.Now.Add(time.Nanosecond)
		}},
		{name: "gap cap", alter: func(in *Input) { in.Sizing.GapAllowance, _ = domain.ParsePrice("1") }},
		{name: "slippage cap", alter: func(in *Input) { in.Sizing.SlippageAllowance, _ = domain.ParsePrice("0.5") }},
		{name: "reserve", alter: func(in *Input) { in.Sizing.MinimumReserve, _ = domain.ParseMoney("501") }},
		{name: "notional limit", alter: func(in *Input) { in.Sizing.NotionalLimits[0], _ = domain.ParseMoney("0") }},
		{name: "minimum quantity", alter: func(in *Input) { in.Sizing.InstrumentMetadata.MinimumQuantity, _ = domain.ParseQuantity("10") }},
		{name: "minimum notional", alter: func(in *Input) { in.Sizing.InstrumentMetadata.MinimumNotional, _ = domain.ParseNotional("1000") }},
		{name: "central risk", alter: func(in *Input) { in.Sizing.CentralRiskEligible = false }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := cloneInput(baseline)
			test.alter(&input)
			decision, err := evaluator.Evaluate(input)
			if err != nil {
				t.Fatal(err)
			}
			if decision.Action == ActionEntry {
				t.Fatalf("invalid sizing produced entry: %#v", decision.Candidate)
			}
		})
	}
}

func TestTenIdenticalHashesConcurrentEvaluationAndModeIndependentAdapterDecisions(t *testing.T) {
	evaluator, _ := evaluatorFixture(t)
	input := baselineInput(t)
	first, err := evaluator.Evaluate(input)
	if err != nil {
		t.Fatal(err)
	}
	want := first.CanonicalHash()
	for index := 0; index < 10; index++ {
		decision, evaluateErr := evaluator.Evaluate(cloneInput(input))
		if evaluateErr != nil || decision.CanonicalHash() != want {
			t.Fatalf("run %d hash = %s, %v", index, decision.CanonicalHash(), evaluateErr)
		}
	}
	var group sync.WaitGroup
	for index := 0; index < 64; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			decision, evaluateErr := evaluator.Evaluate(cloneInput(input))
			if evaluateErr != nil || decision.CanonicalHash() != want {
				t.Errorf("concurrent hash = %s, %v", decision.CanonicalHash(), evaluateErr)
			}
		}()
	}
	group.Wait()
	payload, _ := json.Marshal(input)
	event := replay.Event{Ordinal: input.Ordinal, LogicalTime: input.LogicalTime, Canonical: payload}
	var modePayload []byte
	for _, mode := range []string{"backtest", "replay", "paper", "shadow"} {
		adapter, _ := NewAdapter(evaluator)
		candidate, candidateErr := adapter.Evaluate(context.Background(), event)
		if candidateErr != nil {
			t.Fatalf("%s adapter: %v", mode, candidateErr)
		}
		if modePayload == nil {
			modePayload = candidate.Payload
		} else if !bytes.Equal(modePayload, candidate.Payload) {
			t.Fatalf("%s decision payload differs", mode)
		}
	}
}

func evaluatorFixture(t *testing.T) (*Evaluator, Configuration) {
	t.Helper()
	configuration, err := NewConfiguration(config.DefaultV1BConfiguration().MeanReversion)
	if err != nil {
		t.Fatal(err)
	}
	evaluator, err := NewEvaluator(configuration)
	if err != nil {
		t.Fatal(err)
	}
	return evaluator, configuration
}

func baselineInput(t *testing.T) Input {
	t.Helper()
	instrument, _ := domain.NewSpotInstrument("BTC", "USDT")
	signalEnd := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	primary, higher := baselineCandles(t, instrument, signalEnd)
	spread, _ := domain.ParsePercent("0.0005")
	configuration, _ := NewConfiguration(config.DefaultV1BConfiguration().MeanReversion)
	return Input{Ordinal: 1, LogicalTime: 100, Now: signalEnd.Add(3100 * time.Millisecond),
		Instrument: instrument, PrimaryCandles: primary, HigherCandles: higher,
		MarketHealthy: true, MarketDataQualityPass: true, Spread: spread, BookAge: 10 * time.Millisecond,
		Sizing: baselineSizing(instrument, signalEnd), Evidence: baselineEvidence(configuration)}
}

func baselineCandles(t *testing.T, instrument domain.Instrument,
	signalEnd time.Time,
) ([]exchangecontracts.Candle, []exchangecontracts.Candle) {
	t.Helper()
	primary := make([]exchangecontracts.Candle, 28)
	for index := range primary {
		closeValue := 100 + index%2
		if index == len(primary)-1 {
			closeValue = 95
		}
		primary[index] = strategyCandle(t, instrument, "1h", signalEnd.Add(time.Duration(index-len(primary))*time.Hour),
			time.Hour, closeValue, index+1, "primary-")
	}
	higher := make([]exchangecontracts.Candle, 210)
	for index := range higher {
		higher[index] = strategyCandle(t, instrument, "4h", signalEnd.Add(time.Duration(index-len(higher))*4*time.Hour),
			4*time.Hour, 100, index+1, "higher-")
	}
	return primary, higher
}

func baselineSizing(instrument domain.Instrument, signalEnd time.Time) SizingState {
	equity, _ := domain.ParseMoney("500")
	available, _ := domain.ParseMoney("500")
	reserve, _ := domain.ParseMoney("75")
	limit, _ := domain.ParseMoney("75")
	executable, _ := domain.ParsePrice("95.1")
	gap, _ := domain.ParsePrice("0.1")
	slippage, _ := domain.ParsePrice("0.1")
	fee, _ := domain.ParseRate("0.001")
	tick, _ := domain.ParsePrice("0.01")
	step, _ := domain.ParseQuantity("0.00001")
	minimumQuantity, _ := domain.ParseQuantity("0.00001")
	minimumNotional, _ := domain.ParseNotional("10")
	return SizingState{Equity: equity, AvailableCash: available, MinimumReserve: reserve,
		NotionalLimits: []domain.Money{limit}, FirstExecutablePrice: executable,
		FirstExecutableAt: signalEnd.Add(100 * time.Millisecond), GapAllowance: gap,
		SlippageAllowance: slippage, EntryFeeRate: fee, ExitFeeRate: fee,
		InstrumentMetadata: domain.InstrumentMetadata{Instrument: instrument, Version: 1, PriceTick: tick,
			EffectiveAt: signalEnd.Add(-24 * time.Hour), QuantityStep: step,
			MinimumQuantity: minimumQuantity, MinimumNotional: minimumNotional},
		CentralRiskEligible: true, LiquidityDomain: "combined-book", FencingToken: 1}
}

func baselineEvidence(configuration Configuration) InputEvidence {
	return InputEvidence{PrimaryCandleViewID: "primary-view", PrimaryCandleViewRevision: 1,
		HigherCandleViewID: "higher-view", HigherCandleViewRevision: 1, MarketViewID: "market-view",
		MarketViewRevision: 1, CoherentViewID: strings.Repeat("a", 64),
		CoherentVersionVectorHash: strings.Repeat("a", 64), InstrumentMetadataID: "metadata-b3",
		AssetEligibilityVersion: 1, ConfigurationSnapshotID: "configuration-b3",
		ConfigurationVersion: "axiom.config.v1b.2", ConfigurationHash: configuration.Hash,
		StrategyVersion: configuration.Version, StrategyHash: strings.Repeat("b", 64),
		PortfolioRevision: 1, PositionRevision: 1, RiskPolicyID: "risk-b3", RiskPolicyVersion: 1,
		RiskPolicyHash: strings.Repeat("c", 64), FeeModelID: "fixed-bps-v1",
		LatencyModelID: "fixed-zero-v1", FillModelID: "fill-v1", SlippageModelID: "slippage-v1",
		GapModelID: "gap-v1", CorrelationModelID: "correlation-v1", CorrelationID: "correlation-b3",
		CausationID: "causation-b3"}
}

func strategyCandle(t *testing.T, instrument domain.Instrument, interval string, openTime time.Time,
	duration time.Duration, closeValue, sequence int, prefix string) exchangecontracts.Candle {
	t.Helper()
	closeText := strconv.Itoa(closeValue)
	open, _ := domain.ParsePrice(closeText)
	high, _ := domain.ParsePrice(strconv.Itoa(closeValue + 1))
	low, _ := domain.ParsePrice(strconv.Itoa(closeValue - 1))
	closePrice, _ := domain.ParsePrice(closeText)
	volume, _ := domain.ParseQuantity("1")
	return exchangecontracts.Candle{Exchange: "binance", Instrument: instrument, Interval: interval,
		OpenTime: openTime, CloseTime: openTime.Add(duration), Open: open, High: high, Low: low, Close: closePrice,
		Volume: volume, Closed: true, ReceivedAt: domain.EventTime{UTC: openTime.Add(duration + 100*time.Millisecond),
			Sequence: uint64(sequence)}, RawPayloadHash: prefix + strconv.Itoa(sequence)}
}

func openPositionFixture(t *testing.T) PositionState {
	t.Helper()
	quantity, _ := domain.ParseQuantity("0.2")
	entry, _ := domain.ParsePrice("96")
	stop, _ := domain.ParsePrice("94")
	return PositionState{Open: true, Quantity: quantity, ActualEntryPrice: entry, InitialStop: stop}
}

func cloneInput(input Input) Input {
	cloned := input
	cloned.PrimaryCandles = append([]exchangecontracts.Candle(nil), input.PrimaryCandles...)
	cloned.HigherCandles = append([]exchangecontracts.Candle(nil), input.HigherCandles...)
	cloned.Sizing.NotionalLimits = append([]domain.Money(nil), input.Sizing.NotionalLimits...)
	return cloned
}
