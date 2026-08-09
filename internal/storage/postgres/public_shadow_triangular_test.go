package postgres

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"axiom/internal/backtest"
	"axiom/internal/config"
	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/marketdata"
	"axiom/internal/risk"
	"axiom/internal/strategies/arbitrage"
	"axiom/internal/strategies/triangular"
)

func TestDecodeOwnerConsoleTriangularShadowResultRequiresExactPlanSimulationAndReduction(t *testing.T) {
	input := ownerConsoleTriangularRecordedInput(t)
	prepared, projection, err := triangular.AttachCleanRecordedReduction(input,
		"shadow/triangular/triangle-shadow-a")
	if err != nil || projection == nil {
		t.Fatalf("projection=%#v error=%v", projection, err)
	}
	claim := ownerConsoleTriangularClaim(input)
	transactions, err := ownerConsoleTriangularTransactions(claim, prepared, projection)
	if err != nil {
		t.Fatal(err)
	}
	decisionPayload, _ := json.Marshal(risk.Decision{Action: risk.ActionApprove,
		ReasonCode: "approved", EffectiveState: risk.StateNormal, EvaluatedAt: input.Now})
	planPayload, _ := json.Marshal(projection.Plan)
	simulationPayload, _ := json.Marshal(projection.Simulation)
	reductionPayload, _ := json.Marshal(ownerConsoleTriangularReductionEvidence{Transactions: transactions})
	result := backtest.EventResult{Ordinal: prepared.Ordinal, Decision: decisionPayload,
		Orders: planPayload, ExecutionEvents: simulationPayload, Balances: reductionPayload}
	accepted, _, reason, replayed, err := decodeOwnerConsoleTriangularShadowResult(claim, prepared, result)
	if err != nil || !accepted || reason != "approved" || !reflect.DeepEqual(replayed, projection) {
		t.Fatalf("accepted=%t reason=%q replayed=%#v error=%v", accepted, reason, replayed, err)
	}

	tampered := result
	plan := projection.Plan
	plan.Revision++
	tampered.Orders, _ = json.Marshal(plan)
	if _, _, _, _, err = decodeOwnerConsoleTriangularShadowResult(claim, prepared, tampered); err == nil {
		t.Fatal("tampered execution plan was accepted")
	}
	tampered = result
	transactions[0].Lines[0].Account.Owner = "other-strategy"
	tampered.Balances, _ = json.Marshal(ownerConsoleTriangularReductionEvidence{Transactions: transactions})
	if _, _, _, _, err = decodeOwnerConsoleTriangularShadowResult(claim, prepared, tampered); err == nil {
		t.Fatal("tampered journal ownership was accepted")
	}
}

func TestDecodeOwnerConsoleTriangularShadowResultPreservesNoEligibleEvaluation(t *testing.T) {
	input := ownerConsoleTriangularRecordedInput(t)
	settlement, _ := domain.ParseAssetSymbol("USDT")
	input.FeeBalances[settlement], _ = domain.ParseBalance("0")
	decision := triangular.EvaluationDecision{Action: "no_action", ReasonCode: "no_eligible_cycle",
		CandidateCount: 0, ConfigurationVersion: input.Configuration.StrategyVersion,
		ConfigurationHash: input.ConfigurationHash, InstrumentMetadataID: input.InstrumentMetadataID,
		DecisionOffsetNanos: input.LogicalTime}
	decisionPayload, _ := json.Marshal(decision)
	balancesPayload, _ := json.Marshal(ownerConsoleTriangularEvaluationBalances{
		AvailableSettlement: input.AvailableSettlement, StrategyBudget: input.StrategyBudget,
		GlobalReserveFloor: input.GlobalReserveFloor, RecoveryAllowance: input.RecoveryAllowance,
		FeeBalances: input.FeeBalances})
	claim := ownerConsoleTriangularClaim(input)
	accepted, _, reason, projection, err := decodeOwnerConsoleTriangularShadowResult(claim, input,
		backtest.EventResult{Ordinal: input.Ordinal, Decision: decisionPayload,
			Orders: json.RawMessage("[]"), ExecutionEvents: json.RawMessage("[]"), Balances: balancesPayload})
	if err != nil || accepted || reason != "no_eligible_cycle" || projection != nil {
		t.Fatalf("accepted=%t reason=%q projection=%#v error=%v", accepted, reason, projection, err)
	}
}

func ownerConsoleTriangularClaim(input triangular.Input) PublicShadowClaim {
	return PublicShadowClaim{ID: "triangle-shadow-a", RunID: "triangle-shadow-a",
		PortfolioID: "triangle-portfolio-a", StrategyID: "triangular-arbitrage-1-0-0", ExchangeID: "binance",
		Configuration: config.DefaultMultiStrategyConfiguration(), ConfigurationHash: input.ConfigurationHash}
}

func ownerConsoleTriangularRecordedInput(t *testing.T) triangular.Input {
	t.Helper()
	now := time.Date(2026, 8, 9, 20, 0, 0, 0, time.UTC)
	platform := config.DefaultMultiStrategyConfiguration()
	configuration, err := triangular.ConfigurationFromReviewed(platform.Triangular)
	if err != nil {
		t.Fatal(err)
	}
	markets := []triangular.MarketInput{
		ownerConsoleTriangularMarket(t, now, "BTC", "USDT", "99", "100", "0"),
		ownerConsoleTriangularMarket(t, now, "ETH", "BTC", "0.54", "0.55", "0.01"),
		ownerConsoleTriangularMarket(t, now, "ETH", "USDT", "60", "61", "0"),
	}
	available, _ := domain.ParseBalance("101")
	reserve, _ := domain.ParseBalance("0")
	recovery, _ := domain.ParseBalance("1")
	settlement, _ := domain.ParseAssetSymbol("USDT")
	input := triangular.Input{Ordinal: 1, LogicalTime: 1_000, Now: now, Exchange: "binance",
		Markets: markets, FirstDetectedOffset: 900, AvailableSettlement: available,
		StrategyBudget: available, GlobalReserveFloor: reserve, RecoveryAllowance: recovery,
		FeeBalances:   map[domain.AssetSymbol]domain.Balance{settlement: available},
		Configuration: configuration, ConfigurationHash: strings.Repeat("c", 64),
		InstrumentMetadataID: strings.Repeat("d", 64)}
	input.Simulation = &triangular.SimulationInput{Latency: triangular.LatencyModel{
		Version: "fixed-zero-v1", LegNanos: [3]uint64{1, 1, 1}, RecoveryNanos: 1}}
	for _, offset := range []uint64{1_001, 1_002, 1_003, 1_004} {
		for _, market := range markets {
			input.Simulation.Markets = append(input.Simulation.Markets,
				triangular.TimedMarketInput{Offset: offset, Market: market})
		}
	}
	return input
}

func ownerConsoleTriangularMarket(
	t *testing.T,
	now time.Time,
	baseText, quoteText, bidText, askText, feeMarkText string,
) triangular.MarketInput {
	t.Helper()
	base, _ := domain.ParseAssetSymbol(baseText)
	quote, _ := domain.ParseAssetSymbol(quoteText)
	instrument, _ := domain.NewSpotInstrument(base, quote)
	bid, _ := domain.ParsePrice(bidText)
	ask, _ := domain.ParsePrice(askText)
	quantity, _ := domain.ParseQuantity("1000")
	observation := marketdata.Observation{
		ReceivedAt:   domain.EventTime{UTC: now.Add(-3 * time.Millisecond), Sequence: 1},
		ProcessedAt:  domain.EventTime{UTC: now.Add(-2 * time.Millisecond), Sequence: 2},
		PublishedAt:  domain.EventTime{UTC: now.Add(-time.Millisecond), Sequence: 3},
		ConnectionID: "triangle-" + instrument.Symbol(), ConnectionGeneration: 1,
		SourceSequence: 1, IngestOrdinal: 1, ReceivedOffsetNanos: 898,
		ProcessedOffsetNanos: 899, PublishedOffsetNanos: 900,
	}
	tick, _ := domain.ParsePrice("0.0001")
	step, _ := domain.ParseQuantity("0.0001")
	minimumNotional, _ := domain.ParseNotional("0.0001")
	maximum, _ := domain.ParseQuantity("10000")
	feeRate, _ := domain.ParseRate("0.001")
	feeAsset, _ := domain.ParseAssetSymbol("USDT")
	feeMark, _ := domain.ParsePrice(feeMarkText)
	return triangular.MarketInput{Snapshot: exchangecontracts.BookSnapshot{Exchange: "binance",
		Instrument: instrument, LastSequence: 1, ReceivedAt: observation.ReceivedAt,
		Bids:           []exchangecontracts.PriceLevel{{Price: bid, Quantity: quantity}},
		Asks:           []exchangecontracts.PriceLevel{{Price: ask, Quantity: quantity}},
		RawPayloadHash: strings.Repeat("a", 64)}, Observation: observation,
		Rules: arbitrage.InstrumentRules{Exchange: "binance",
			Metadata: domain.InstrumentMetadata{Instrument: instrument, Version: 1,
				EffectiveAt: now.Add(-time.Minute), PriceTick: tick, QuantityStep: step,
				MinimumQuantity: step, MinimumNotional: minimumNotional},
			MaximumQuantity: maximum, Fee: arbitrage.FeeSchedule{Version: "fixed-bps-v1",
				Rate: feeRate, Asset: feeAsset, ThirdAssetPriceInQuote: feeMark},
			Active: true, ObservedAt: now.Add(-time.Minute)}}
}
