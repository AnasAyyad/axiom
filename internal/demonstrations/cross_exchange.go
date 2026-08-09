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
	"axiom/internal/execution"
	"axiom/internal/marketdata"
	"axiom/internal/reconciliation"
	"axiom/internal/replay"
	"axiom/internal/risk"
	runtimecore "axiom/internal/runtime"
	"axiom/internal/strategies/arbitrage"
	"axiom/internal/strategies/crossarb"
)

// CrossExchangeArbitrageID is the stable semantic ID of the bundled scenario.
const CrossExchangeArbitrageID = "cross-exchange-arbitrage-basics"

// RunCrossExchangeArbitrage runs a fixed coherent two-venue candidate through
// the canonical allocator, central-risk, plan, deterministic simulation,
// accounting, and reconciliation stages. It is synthetic and has no exchange
// client or credential boundary.
func RunCrossExchangeArbitrage(ctx context.Context) (Result, error) {
	if ctx == nil {
		return Result{}, fmt.Errorf("demonstration_context_invalid")
	}
	evaluation, err := guidedCrossExchangeInput()
	if err != nil {
		return Result{}, err
	}
	input, err := guidedCrossExchangeRecordedInput(evaluation)
	if err != nil {
		return Result{}, err
	}
	pipeline, err := newGuidedCrossExchangePipeline(input)
	if err != nil {
		return Result{}, err
	}
	accepted, err := processGuidedCrossExchange(ctx, pipeline, input)
	if err != nil || len(accepted.Orders) == 0 || len(accepted.ExecutionEvents) == 0 {
		return Result{}, fmt.Errorf("demonstration_pipeline_incomplete")
	}
	rejectedInput := input
	rejectedInput.Restoration.MarginalInventoryReplacement = mustGuidedMoney("100")
	rejectedEvaluation, evaluationErr := rejectedInput.EvaluationInput()
	if evaluationErr != nil {
		return Result{}, evaluationErr
	}
	_, rejectedErr := crossarb.Evaluate(rejectedEvaluation)
	if rejectedErr == nil {
		return Result{}, fmt.Errorf("demonstration_rejection_incomplete")
	}
	result := Result{
		ID: CrossExchangeArbitrageID, StrategyID: "cross-exchange-arbitrage",
		StrategyVersion: "cross-exchange-arbitrage@1.0.0", Synthetic: true,
		ConfigurationHash: input.ConfigurationHash,
		Accepted:          accepted,
		Rejected:          guidedRejectedEvent(input.Ordinal+1, "restoration_cost_rejected", rejectedErr),
		Metrics:           pipeline.Metrics(),
	}
	hash, err := resultHash(result)
	if err != nil {
		return Result{}, err
	}
	result.ResultHash = hash
	return result, nil
}

func guidedCrossExchangeRecordedInput(source crossarb.EvaluationInput) (crossarb.Input, error) {
	markets := make([]crossarb.MarketInput, 0, len(source.Markets))
	for _, market := range source.Markets {
		view := market.Book
		observation := view.Observation()
		markets = append(markets, crossarb.MarketInput{Snapshot: exchangecontracts.BookSnapshot{
			Exchange: exchangecontracts.ExchangeID(view.Exchange()), Instrument: view.Instrument(),
			LastSequence: view.Sequence(), ReceivedAt: observation.ReceivedAt, Bids: view.Bids(), Asks: view.Asks(),
			RawPayloadHash: "sha256:guided-cross-" + view.Exchange(),
		}, Observation: observation, Rules: market.Rules})
	}
	now := time.Unix(11, 0).UTC()
	observations, policies, evaluatedAt, err := (demoRiskInputs{at: now}).Current()
	if err != nil {
		return crossarb.Input{}, err
	}
	view := source.CoherentView
	input := crossarb.Input{Ordinal: 1, LogicalTime: source.DecisionOffsetNanos, Now: now, Markets: markets,
		Coherent:  crossarb.CoherentViewInput{Identity: view.Identity(), Policy: view.Policy(), Trigger: view.Trigger(), Members: view.Members()},
		Inventory: source.Inventory, QuoteBudget: source.QuoteBudget, FeeBalances: source.FeeBalances,
		Configuration: source.Configuration, ConfigurationHash: source.ConfigurationHash,
		InstrumentMetadataSetHash: source.InstrumentMetadataSetHash, Restoration: source.Restoration,
		CentralRisk: &crossarb.RiskInput{Policies: policies, Observations: observations, EvaluatedAt: evaluatedAt},
		Reduction: &crossarb.ReductionInput{Attribution: guidedCrossExchangeAttribution(),
			Reconciliation: crossarb.ReconciliationInput{Scope: "guided-cross-exchange/" + source.ConfigurationHash,
				Expected: guidedReconciliationState(), Actual: guidedReconciliationState(), At: now}},
		Simulation: &crossarb.SimulationInput{Latency: crossarb.LatencyDistribution{
			Version: "guided-cross-latency-v1", BuySamplesNanos: []uint64{10}, SellSamplesNanos: []uint64{20},
			VerificationNanos: 5, RetryNanos: 5, RecoveryNanos: 30}, Recovery: crossarb.RecoveryPolicy{}},
	}
	for _, offset := range []uint64{input.LogicalTime + 10, input.LogicalTime + 20} {
		for _, market := range input.Markets {
			input.Simulation.Markets = append(input.Simulation.Markets, crossarb.TimedMarketInput{Offset: offset, Market: market})
		}
	}
	input.Simulation.Directives = []crossarb.TimedDirective{
		{Exchange: "binance", Phase: crossarb.PhaseArrival, Offset: input.LogicalTime + 10,
			Directive: crossarb.LegDirective{State: execution.OrderFilled}},
		{Exchange: "bybit", Phase: crossarb.PhaseArrival, Offset: input.LogicalTime + 20,
			Directive: crossarb.LegDirective{State: execution.OrderFilled}},
	}
	return input, nil
}

func newGuidedCrossExchangePipeline(input crossarb.Input) (*backtest.SagaPipelineProcessor, error) {
	claims, err := crossarb.NewRecordedSagaClaimSet(input)
	if err != nil {
		return nil, err
	}
	allocator, err := crossarb.NewAtomicSagaAllocator(claims, runtimecore.FencingToken(1))
	if err != nil {
		return nil, err
	}
	riskEngine, err := risk.NewEngine(demoRiskAudit{}, demoRiskAlerts{})
	if err != nil || riskEngine.ManualTransition(risk.StateNormal, demoRecovery(input.Now)) != nil {
		return nil, fmt.Errorf("demonstration_risk_invalid")
	}
	riskAdapter, err := crossarb.NewSagaRiskAdapter(riskEngine, crossarb.RecordedSagaRiskInputs{})
	if err != nil {
		return nil, err
	}
	broker, err := crossarb.NewSagaSimulationBroker(crossarb.RecordedSagaSimulationInputs{})
	if err != nil {
		return nil, err
	}
	runID, runErr := domain.NewRunID("guided-cross-exchange-run")
	portfolioID, portfolioErr := domain.NewPortfolioID("guided-cross-exchange-portfolio")
	if runErr != nil || portfolioErr != nil {
		return nil, fmt.Errorf("demonstration_identity_invalid")
	}
	journal := accounting.NewMemoryJournal()
	reconciler, err := reconciliation.NewReconciler(guidedReconciliationCases{}, guidedReconciliationIncidents{}, guidedReconciliationQuarantine{}, journal,
		reconciliation.Context{RunID: runID, PortfolioID: portfolioID, Owner: "guided-owner", ConfigurationHash: input.ConfigurationHash})
	if err != nil {
		return nil, err
	}
	provider, err := crossarb.NewRecordedSagaReductionProvider(journal, reconciler, runID, portfolioID, "guided-owner", allocator)
	if err != nil {
		return nil, err
	}
	reducer, err := crossarb.NewSagaReducer(provider)
	if err != nil {
		return nil, err
	}
	return backtest.NewSagaPipelineProcessor(backtest.SagaPipelineDependencies{Strategy: crossarb.NewSagaStrategyAdapter(),
		Allocator: allocator, Risk: riskAdapter, Planner: crossarb.NewSagaPlanner(), Broker: broker, Reducer: reducer,
		Metrics: func() backtest.Metrics { return backtest.Metrics{TotalNetReturn: "not_evaluated", Trades: 2} }})
}

func processGuidedCrossExchange(ctx context.Context, processor *backtest.SagaPipelineProcessor, input crossarb.Input) (backtest.EventResult, error) {
	payload, err := json.Marshal(input)
	if err != nil {
		return backtest.EventResult{}, err
	}
	return processor.Process(ctx, replay.Event{Ordinal: input.Ordinal, LogicalTime: input.LogicalTime, Canonical: payload})
}

func guidedCrossExchangeAttribution() crossarb.PortfolioAttribution {
	value := func(gain bool) crossarb.AttributionValue {
		return crossarb.AttributionValue{Amount: mustGuidedBalance("0.01"), Gain: gain}
	}
	return crossarb.PortfolioAttribution{ExecutionPnL: value(true), BTCInventoryPnL: value(false),
		ETHInventoryPnL: value(true), StablecoinValuation: value(false), Fees: mustGuidedBalance("0.01"),
		Spread: mustGuidedBalance("0.01"), Slippage: mustGuidedBalance("0.01"), Latency: mustGuidedBalance("0.01"),
		Recovery: mustGuidedBalance("0.01"), Rebalancing: mustGuidedBalance("0.01"), CombinedPnL: value(true)}
}

func guidedCrossExchangeInput() (crossarb.EvaluationInput, error) {
	base, err := domain.ParseAssetSymbol("BTC")
	if err != nil {
		return crossarb.EvaluationInput{}, err
	}
	quote, err := domain.ParseAssetSymbol("USDT")
	if err != nil {
		return crossarb.EvaluationInput{}, err
	}
	instrument, err := domain.NewSpotInstrument(base, quote)
	if err != nil {
		return crossarb.EvaluationInput{}, err
	}
	binance, err := guidedCrossMarket("binance", instrument, "99", "100", 1)
	if err != nil {
		return crossarb.EvaluationInput{}, err
	}
	bybit, err := guidedCrossMarket("bybit", instrument, "104", "105", 2)
	if err != nil {
		return crossarb.EvaluationInput{}, err
	}
	markets := []crossarb.Market{binance, bybit}
	view, err := guidedCoherentView(markets, 200)
	if err != nil {
		return crossarb.EvaluationInput{}, err
	}
	return crossarb.EvaluationInput{
		CoherentView: view, Markets: markets,
		Inventory: []crossarb.VenueInventory{
			guidedCrossInventory("binance", base, "20"),
			guidedCrossInventory("bybit", base, "80"),
		},
		QuoteBudget: mustGuidedBalance("10"),
		FeeBalances: map[string]domain.Balance{
			"binance:USDT": mustGuidedBalance("10"), "bybit:USDT": mustGuidedBalance("10"),
		},
		DecisionOffsetNanos: 200, Configuration: crossarb.DefaultConfiguration(),
		ConfigurationHash: strings.Repeat("d", 64), InstrumentMetadataSetHash: strings.Repeat("e", 64),
		Restoration: crossarb.RestorationEconomics{
			ModelVersion: "closed-inventory-cycle.v1", LatencyModelVersion: "guided-latency-v1",
			RecoveryModelVersion: "guided-recovery-v1", InventoryShadowPriceVersion: "guided-shadow-price-v1",
			ConcentrationModelVersion: "guided-concentration-v1", LatencyDeterioration: mustGuidedMoney("0.005"),
			RecoveryAllowance: mustGuidedMoney("0.005"), MarginalInventoryReplacement: mustGuidedMoney("0.005"),
			NaturalReversalCost: mustGuidedMoney("0.005"), AdvisoryRebalancingCost: mustGuidedMoney("0.005"),
			ExchangeConcentrationPenalty: mustGuidedMoney("0.005"), USDTVenueConcentrationPenalty: mustGuidedMoney("0.005"),
			MaximumOneLegLoss: mustGuidedMoney("0.01"), EstimatedRestorationDelayNanos: 25_000_000,
			NaturalReverseAvailable: true, AdvisoryRebalancingRequired: true,
		},
	}, nil
}

func guidedCrossMarket(exchange string, instrument domain.Instrument, bid, ask string, ordinal uint64) (crossarb.Market, error) {
	book, err := marketdata.NewBook(exchange, instrument, 20, 20, nil)
	if err != nil {
		return crossarb.Market{}, err
	}
	if err = book.BeginGeneration("guided-"+exchange, 1); err != nil {
		return crossarb.Market{}, err
	}
	observation := guidedCrossObservation(exchange, ordinal)
	snapshot, err := guidedSnapshot(exchange, instrument, [][2]string{{bid, "100"}}, [][2]string{{ask, "100"}}, observation)
	if err != nil {
		return crossarb.Market{}, err
	}
	if err = book.ReplaceSnapshot(snapshot, observation); err != nil {
		return crossarb.Market{}, err
	}
	priceTick, priceErr := domain.ParsePrice("0.01")
	quantityStep, quantityErr := domain.ParseQuantity("0.000001")
	minimumNotional, notionalErr := domain.ParseNotional("0.01")
	maximumQuantity, maximumErr := domain.ParseQuantity("1000")
	fee, feeErr := domain.ParseRate("0.001")
	if priceErr != nil || quantityErr != nil || notionalErr != nil || maximumErr != nil || feeErr != nil {
		return crossarb.Market{}, fmt.Errorf("demonstration_input_invalid")
	}
	return crossarb.Market{Book: book.View(), Rules: arbitrage.InstrumentRules{
		Exchange: exchange, Metadata: domain.InstrumentMetadata{Instrument: instrument, Version: 1,
			EffectiveAt: time.Unix(9, 0).UTC(), PriceTick: priceTick, QuantityStep: quantityStep,
			MinimumQuantity: quantityStep, MinimumNotional: minimumNotional},
		MaximumQuantity: maximumQuantity,
		Fee:             arbitrage.FeeSchedule{Version: "guided-fee-v1", Rate: fee, Asset: mustGuidedAsset("USDT")},
		Active:          true, ObservedAt: time.Unix(10, 0).UTC(),
	}}, nil
}

func guidedCoherentView(markets []crossarb.Market, decision uint64) (runtimecore.CoherentView, error) {
	views := runtimecore.NewMarketViews()
	keys := make([]runtimecore.MarketKey, 0, len(markets))
	for _, market := range markets {
		key := runtimecore.MarketKey{Exchange: market.Book.Exchange(), Instrument: market.Book.Instrument()}
		if err := views.ActivateGeneration(key, market.Book.Generation()); err != nil {
			return runtimecore.CoherentView{}, err
		}
		input, err := marketdata.CoherentInput(market.Book, exchangecontracts.ClockHealth{
			ObservedAt: time.Unix(9, 0).UTC(), Uncertainty: time.Millisecond, Eligible: true,
		}, "guided-collector-"+market.Book.Exchange(), "guided-region")
		if err != nil {
			return runtimecore.CoherentView{}, err
		}
		if _, err = views.Publish(input); err != nil {
			return runtimecore.CoherentView{}, err
		}
		keys = append(keys, key)
	}
	return views.CoherentAsOf(keys, runtimecore.AsOfTrigger{MonotonicNanos: decision, IngestOrdinal: 100,
		UTC: time.Unix(10, 0).UTC()}, runtimecore.CoherentPolicy{Version: "axiom.coherent-view-policy.v1",
		MaximumBookAge: 250 * time.Millisecond, MaximumInterBookSkew: 250 * time.Millisecond,
		MaximumClockUncertainty: 100 * time.Millisecond})
}

func guidedCrossObservation(exchange string, ordinal uint64) marketdata.Observation {
	return marketdata.Observation{
		ReceivedAt:   domain.EventTime{UTC: time.Unix(10, 0).UTC(), Sequence: ordinal*3 - 2},
		ProcessedAt:  domain.EventTime{UTC: time.Unix(10, 0).UTC(), Sequence: ordinal*3 - 1},
		PublishedAt:  domain.EventTime{UTC: time.Unix(10, 0).UTC(), Sequence: ordinal * 3},
		ConnectionID: "guided-" + exchange, ConnectionGeneration: 1, SourceSequence: ordinal, IngestOrdinal: ordinal,
		ReceivedOffsetNanos: 100 + ordinal, ProcessedOffsetNanos: 110 + ordinal, PublishedOffsetNanos: 120 + ordinal,
	}
}

func guidedCrossInventory(exchange string, base domain.AssetSymbol, owned string) crossarb.VenueInventory {
	return crossarb.VenueInventory{Owner: "guided-owner", Exchange: exchange, BaseAsset: base,
		OwnedBase: mustGuidedBalance(owned), TotalEligibleBase: mustGuidedBalance("100"),
		OwnedUSDT: mustGuidedBalance("100"), TotalEligibleUSDT: mustGuidedBalance("200"), Revision: 1}
}

func mustGuidedMoney(value string) domain.Money {
	result, err := domain.ParseMoney(value)
	if err != nil {
		panic(err)
	}
	return result
}
