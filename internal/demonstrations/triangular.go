package demonstrations

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"axiom/internal/accounting"
	"axiom/internal/backtest"
	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/marketdata"
	"axiom/internal/reconciliation"
	"axiom/internal/replay"
	"axiom/internal/risk"
	runtimecore "axiom/internal/runtime"
	"axiom/internal/strategies/arbitrage"
	"axiom/internal/strategies/triangular"
)

// TriangularArbitrageID is the stable semantic ID of the bundled scenario.
const TriangularArbitrageID = "triangular-arbitrage-basics"

// RunTriangularArbitrage runs a fixed three-conversion candidate through the
// same allocation, central-risk, plan, simulation, accounting, and
// reconciliation boundaries used by the canonical multi-leg pipeline. The
// inputs are synthetic and deterministic; no exchange client is present.
func RunTriangularArbitrage(ctx context.Context) (Result, error) {
	if ctx == nil {
		return Result{}, fmt.Errorf("demonstration_context_invalid")
	}
	evaluation, err := guidedTriangularInput()
	if err != nil {
		return Result{}, err
	}
	input, err := guidedTriangularRecordedInput(evaluation)
	if err != nil {
		return Result{}, err
	}
	pipeline, err := newGuidedTriangularPipeline(input)
	if err != nil {
		return Result{}, err
	}
	accepted, err := processGuidedTriangular(ctx, pipeline, input)
	if err != nil || len(accepted.Orders) == 0 || len(accepted.ExecutionEvents) == 0 {
		return Result{}, fmt.Errorf("demonstration_pipeline_incomplete")
	}
	rejectedInput := input
	rejectedInput.FeeBalances = map[domain.AssetSymbol]domain.Balance{
		mustGuidedAsset("USDT"): mustGuidedBalance("0"),
		mustGuidedAsset("BTC"):  mustGuidedBalance("10"),
		mustGuidedAsset("ETH"):  mustGuidedBalance("10"),
	}
	rejectedEvaluation, evaluationErr := rejectedInput.EvaluationInput()
	if evaluationErr != nil {
		return Result{}, evaluationErr
	}
	_, rejectedErr := triangular.Evaluate(rejectedEvaluation)
	if rejectedErr == nil {
		return Result{}, fmt.Errorf("demonstration_rejection_incomplete")
	}
	result := Result{
		ID: TriangularArbitrageID, StrategyID: "triangular-arbitrage",
		StrategyVersion: "triangular-arbitrage@1.0.0", Synthetic: true,
		ConfigurationHash: input.ConfigurationHash,
		Accepted:          accepted,
		Rejected:          guidedRejectedEvent(input.Ordinal+1, "fee_capacity_rejected", rejectedErr),
		Metrics:           pipeline.Metrics(),
	}
	hash, err := resultHash(result)
	if err != nil {
		return Result{}, err
	}
	result.ResultHash = hash
	return result, nil
}

func guidedTriangularRecordedInput(source triangular.EvaluationInput) (triangular.Input, error) {
	markets := make([]triangular.MarketInput, 0, len(source.Markets))
	for _, market := range source.Markets {
		view := market.Book
		observation := view.Observation()
		markets = append(markets, triangular.MarketInput{Snapshot: exchangecontracts.BookSnapshot{
			Exchange: exchangecontracts.ExchangeID(view.Exchange()), Instrument: view.Instrument(),
			LastSequence: view.Sequence(), ReceivedAt: observation.ReceivedAt, Bids: view.Bids(), Asks: view.Asks(),
			RawPayloadHash: "sha256:guided-triangular-" + view.Instrument().Symbol(),
		}, Observation: observation, Rules: market.Rules})
	}
	now := time.Unix(11, 0).UTC()
	observations, policies, evaluatedAt, err := (demoRiskInputs{at: now}).Current()
	if err != nil {
		return triangular.Input{}, err
	}
	input := triangular.Input{Ordinal: 1, LogicalTime: source.DecisionOffsetNanos, Now: now,
		Exchange: source.Exchange, Markets: markets, FirstDetectedOffset: source.FirstDetectedOffset,
		AvailableSettlement: source.AvailableSettlement, StrategyBudget: source.StrategyBudget,
		GlobalReserveFloor: source.GlobalReserveFloor, RecoveryAllowance: source.RecoveryAllowance,
		FeeBalances: source.FeeBalances, Configuration: source.Configuration, ConfigurationHash: source.ConfigurationHash,
		InstrumentMetadataID: source.InstrumentMetadataID,
		CentralRisk:          &triangular.RiskInput{Policies: policies, Observations: observations, EvaluatedAt: evaluatedAt},
		Reduction: &triangular.ReductionInput{Reconciliation: triangular.ReconciliationInput{
			Scope: "guided-triangular/" + source.ConfigurationHash, Expected: guidedReconciliationState(),
			Actual: guidedReconciliationState(), At: now}},
		Simulation: &triangular.SimulationInput{Latency: triangular.LatencyModel{
			Version: "guided-triangular-latency-v1", LegNanos: [3]uint64{10, 20, 30}, RecoveryNanos: 10}},
	}
	for _, offset := range []uint64{input.LogicalTime + 10, input.LogicalTime + 30, input.LogicalTime + 60} {
		for _, market := range input.Markets {
			input.Simulation.Markets = append(input.Simulation.Markets, triangular.TimedMarketInput{Offset: offset, Market: market})
		}
	}
	return input, nil
}

func newGuidedTriangularPipeline(input triangular.Input) (*backtest.SagaPipelineProcessor, error) {
	claims, err := triangular.NewRecordedSagaClaimSet(input, "guided-owner")
	if err != nil {
		return nil, err
	}
	allocator, err := triangular.NewAtomicSagaAllocator(claims, "guided-owner", runtimecore.FencingToken(1))
	if err != nil {
		return nil, err
	}
	riskEngine, err := risk.NewEngine(demoRiskAudit{}, demoRiskAlerts{})
	if err != nil || riskEngine.ManualTransition(risk.StateNormal, demoRecovery(input.Now)) != nil {
		return nil, fmt.Errorf("demonstration_risk_invalid")
	}
	riskAdapter, err := triangular.NewSagaRiskAdapter(riskEngine, triangular.RecordedSagaRiskInputs{})
	if err != nil {
		return nil, err
	}
	broker, err := triangular.NewSagaSimulationBroker(triangular.RecordedSagaSimulationInputs{})
	if err != nil {
		return nil, err
	}
	runID, runErr := domain.NewRunID("guided-triangular-run")
	portfolioID, portfolioErr := domain.NewPortfolioID("guided-triangular-portfolio")
	if runErr != nil || portfolioErr != nil {
		return nil, fmt.Errorf("demonstration_identity_invalid")
	}
	journal := accounting.NewMemoryJournal()
	reconciler, err := reconciliation.NewReconciler(guidedReconciliationCases{}, guidedReconciliationIncidents{}, guidedReconciliationQuarantine{}, journal,
		reconciliation.Context{RunID: runID, PortfolioID: portfolioID, Owner: "guided-owner", ConfigurationHash: input.ConfigurationHash})
	if err != nil {
		return nil, err
	}
	provider, err := triangular.NewRecordedSagaReductionProvider(journal, reconciler, runID, portfolioID, "guided-owner", allocator)
	if err != nil {
		return nil, err
	}
	reducer, err := triangular.NewSagaReducer(provider)
	if err != nil {
		return nil, err
	}
	return backtest.NewSagaPipelineProcessor(backtest.SagaPipelineDependencies{Strategy: triangular.NewSagaStrategyAdapter(),
		Allocator: allocator, Risk: riskAdapter, Planner: triangular.NewSagaPlanner(), Broker: broker, Reducer: reducer,
		Metrics: func() backtest.Metrics { return backtest.Metrics{TotalNetReturn: "not_evaluated", Trades: 1} }})
}

func processGuidedTriangular(ctx context.Context, processor *backtest.SagaPipelineProcessor, input triangular.Input) (backtest.EventResult, error) {
	payload, err := json.Marshal(input)
	if err != nil {
		return backtest.EventResult{}, err
	}
	return processor.Process(ctx, replay.Event{Ordinal: input.Ordinal, LogicalTime: input.LogicalTime, Canonical: payload})
}

func guidedRejectedEvent(ordinal uint64, code string, cause error) backtest.EventResult {
	payload, _ := json.Marshal(map[string]string{"outcome": "rejected", "reason": code, "detail": cause.Error()})
	return backtest.EventResult{Ordinal: ordinal, Decision: payload, Orders: json.RawMessage("[]"),
		ExecutionEvents: json.RawMessage("[]"), Balances: json.RawMessage("{}")}
}

func guidedReconciliationState() reconciliation.State {
	hash := strings.Repeat("a", 64)
	return reconciliation.State{Orders: hash, Fills: hash, Reservations: hash, Balances: hash,
		Positions: hash, Ownership: hash, Journal: hash, Projections: hash}
}

type guidedReconciliationCases struct{}

// Create accepts the deterministic scenario's in-memory reconciliation case.
func (guidedReconciliationCases) Create(reconciliation.Case) error { return nil }

type guidedReconciliationIncidents struct{}

// Create records a deterministic in-memory reconciliation incident.
func (guidedReconciliationIncidents) Create(string, string, time.Time) (string, error) {
	return "guided-incident", nil
}

type guidedReconciliationQuarantine struct{}

// Block records deterministic quarantine without an external side effect.
func (guidedReconciliationQuarantine) Block(string, string) error { return nil }

func guidedTriangularInput() (triangular.EvaluationInput, error) {
	markets := make([]triangular.Market, 0, 3)
	for _, definition := range []struct {
		base, quote string
		bids, asks  [][2]string
		feeAsset    string
	}{
		{"BTC", "USDT", [][2]string{{"99", "5"}}, [][2]string{{"100", "5"}}, "USDT"},
		{"ETH", "USDT", [][2]string{{"60", "10"}}, [][2]string{{"61", "10"}}, "USDT"},
		{"ETH", "BTC", [][2]string{{"0.54", "10"}}, [][2]string{{"0.55", "10"}}, "BTC"},
	} {
		market, err := guidedTriangularMarket(definition.base, definition.quote, definition.bids, definition.asks, definition.feeAsset)
		if err != nil {
			return triangular.EvaluationInput{}, err
		}
		markets = append(markets, market)
	}
	return triangular.EvaluationInput{
		Exchange: "binance", Markets: markets, DecisionOffsetNanos: 1_000,
		FirstDetectedOffset: 900, AvailableSettlement: mustGuidedBalance("101"),
		StrategyBudget: mustGuidedBalance("101"), GlobalReserveFloor: mustGuidedBalance("0"),
		RecoveryAllowance: mustGuidedBalance("1"),
		FeeBalances: map[domain.AssetSymbol]domain.Balance{
			mustGuidedAsset("USDT"): mustGuidedBalance("10"),
			mustGuidedAsset("BTC"):  mustGuidedBalance("10"),
			mustGuidedAsset("ETH"):  mustGuidedBalance("10"),
		},
		Configuration:     triangular.DefaultConfiguration(),
		ConfigurationHash: strings.Repeat("c", 64), InstrumentMetadataID: "guided-metadata-v1",
	}, nil
}

func guidedTriangularMarket(base, quote string, bids, asks [][2]string, feeAsset string) (triangular.Market, error) {
	book, instrument, err := guidedTriangularBook(base, quote, bids, asks)
	if err != nil {
		return triangular.Market{}, err
	}
	rules, err := guidedTriangularRules(instrument, feeAsset)
	if err != nil {
		return triangular.Market{}, err
	}
	return triangular.Market{Book: book.View(), Rules: rules}, nil
}

func guidedTriangularBook(base, quote string, bids, asks [][2]string) (*marketdata.Book, domain.Instrument, error) {
	baseAsset, err := domain.ParseAssetSymbol(base)
	if err != nil {
		return nil, domain.Instrument{}, err
	}
	quoteAsset, err := domain.ParseAssetSymbol(quote)
	if err != nil {
		return nil, domain.Instrument{}, err
	}
	instrument, err := domain.NewSpotInstrument(baseAsset, quoteAsset)
	if err != nil {
		return nil, domain.Instrument{}, err
	}
	book, err := marketdata.NewBook("binance", instrument, 20, 20, nil)
	if err != nil {
		return nil, domain.Instrument{}, err
	}
	if err = book.BeginGeneration("guided-triangular", 1); err != nil {
		return nil, domain.Instrument{}, err
	}
	observation := guidedTriangularObservation()
	snapshot, err := guidedSnapshot("binance", instrument, bids, asks, observation)
	if err != nil {
		return nil, domain.Instrument{}, err
	}
	if err = book.ReplaceSnapshot(snapshot, observation); err != nil {
		return nil, domain.Instrument{}, err
	}
	return book, instrument, nil
}

func guidedTriangularObservation() marketdata.Observation {
	return marketdata.Observation{
		ReceivedAt:   domain.EventTime{UTC: time.Unix(10, 0).UTC(), Sequence: 1},
		ProcessedAt:  domain.EventTime{UTC: time.Unix(10, 1).UTC(), Sequence: 2},
		PublishedAt:  domain.EventTime{UTC: time.Unix(10, 2).UTC(), Sequence: 3},
		ConnectionID: "guided-triangular", ConnectionGeneration: 1, SourceSequence: 1,
		IngestOrdinal: 1, ReceivedOffsetNanos: 100, ProcessedOffsetNanos: 101, PublishedOffsetNanos: 102,
	}
}

func guidedTriangularRules(instrument domain.Instrument, feeAsset string) (arbitrage.InstrumentRules, error) {
	fee, feeErr := domain.ParseRate("0.0001")
	feeSymbol, feeSymbolErr := domain.ParseAssetSymbol(feeAsset)
	priceTick, tickErr := domain.ParsePrice("0.01")
	quantityStep, stepErr := domain.ParseQuantity("0.0001")
	minimumNotional, minimumErr := domain.ParseNotional("0.001")
	maximumQuantity, maximumErr := domain.ParseQuantity("10000")
	if feeErr != nil || feeSymbolErr != nil || tickErr != nil || stepErr != nil || minimumErr != nil || maximumErr != nil {
		return arbitrage.InstrumentRules{}, fmt.Errorf("demonstration_rules_invalid")
	}
	return arbitrage.InstrumentRules{
		Exchange: "binance", Metadata: domain.InstrumentMetadata{Instrument: instrument, Version: 1,
			EffectiveAt: time.Unix(9, 0).UTC(), PriceTick: priceTick, QuantityStep: quantityStep,
			MinimumQuantity: quantityStep, MinimumNotional: minimumNotional},
		MaximumQuantity: maximumQuantity, Fee: arbitrage.FeeSchedule{Version: "guided-fee-v1", Rate: fee, Asset: feeSymbol},
		Active: true, ObservedAt: time.Unix(10, 0).UTC(),
	}, nil
}

func guidedSnapshot(exchange string, instrument domain.Instrument, bids, asks [][2]string, observed marketdata.Observation) (exchangecontracts.BookSnapshot, error) {
	levels := func(values [][2]string) ([]exchangecontracts.PriceLevel, error) {
		result := make([]exchangecontracts.PriceLevel, 0, len(values))
		for _, value := range values {
			price, priceErr := domain.ParsePrice(value[0])
			quantity, quantityErr := domain.ParseQuantity(value[1])
			if priceErr != nil || quantityErr != nil {
				return nil, fmt.Errorf("demonstration_book_invalid")
			}
			result = append(result, exchangecontracts.PriceLevel{Price: price, Quantity: quantity})
		}
		return result, nil
	}
	parsedBids, err := levels(bids)
	if err != nil {
		return exchangecontracts.BookSnapshot{}, err
	}
	parsedAsks, err := levels(asks)
	if err != nil {
		return exchangecontracts.BookSnapshot{}, err
	}
	return exchangecontracts.BookSnapshot{Exchange: exchangecontracts.ExchangeID(exchange), Instrument: instrument, LastSequence: observed.SourceSequence,
		ReceivedAt: observed.ReceivedAt, Bids: parsedBids, Asks: parsedAsks, RawPayloadHash: "sha256:guided-triangular"}, nil
}

func mustGuidedAsset(value string) domain.AssetSymbol {
	result, err := domain.ParseAssetSymbol(value)
	if err != nil {
		panic(err)
	}
	return result
}

func mustGuidedBalance(value string) domain.Balance {
	result, err := domain.ParseBalance(value)
	if err != nil {
		panic(err)
	}
	return result
}
