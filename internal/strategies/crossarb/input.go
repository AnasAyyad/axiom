package crossarb

import (
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/execution"
	"axiom/internal/marketdata"
	"axiom/internal/reconciliation"
	"axiom/internal/risk"
	runtimecore "axiom/internal/runtime"
	"axiom/internal/strategies/arbitrage"
)

// Input is the canonical replayable decision evidence for one Cross-Exchange
// evaluation. It contains recorded public books and the exact coherent-view
// vector that selected them, plus the immutable inventory and economics facts
// used by the pure evaluator. It never fetches market or account data.
type Input struct {
	Ordinal                   uint64                    `json:"ordinal"`
	LogicalTime               uint64                    `json:"logical_time"`
	Now                       time.Time                 `json:"now"`
	Markets                   []MarketInput             `json:"markets"`
	Coherent                  CoherentViewInput         `json:"coherent_view"`
	Inventory                 []VenueInventory          `json:"inventory"`
	QuoteBudget               domain.Balance            `json:"quote_budget"`
	FeeBalances               map[string]domain.Balance `json:"fee_balances"`
	Configuration             Configuration             `json:"configuration"`
	ConfigurationHash         string                    `json:"configuration_hash"`
	InstrumentMetadataSetHash string                    `json:"instrument_metadata_set_hash"`
	Restoration               RestorationEconomics      `json:"restoration"`
	CentralRisk               *RiskInput                `json:"central_risk,omitempty"`
	Simulation                *SimulationInput          `json:"simulation,omitempty"`
	Reduction                 *ReductionInput           `json:"reduction,omitempty"`
}

// CoherentViewInput is the persisted form of the opaque coherent as-of view.
// RestoreCoherentView recomputes and verifies its identity before the strategy
// verifies that each member still matches the reconstructed book.
type CoherentViewInput struct {
	Identity string                      `json:"identity"`
	Policy   runtimecore.CoherentPolicy  `json:"policy"`
	Trigger  runtimecore.AsOfTrigger     `json:"trigger"`
	Members  []runtimecore.ViewReference `json:"members"`
}

// MarketInput preserves one recorded executable book and its evaluated filter
// set. RawPayloadHash is kept because the market-data boundary refuses a
// snapshot without an immutable source identity.
type MarketInput struct {
	Snapshot    exchangecontracts.BookSnapshot `json:"snapshot"`
	Observation marketdata.Observation         `json:"observation"`
	Rules       arbitrage.InstrumentRules      `json:"rules"`
}

// SimulationInput keeps every future public book and simulated venue response
// required by the deterministic concurrent simulator. It is optional for
// evaluation-only records, but mandatory before a canonical input can drive a
// virtual execution plan.
type SimulationInput struct {
	Latency    LatencyDistribution `json:"latency"`
	Recovery   RecoveryPolicy      `json:"recovery_policy"`
	Markets    []TimedMarketInput  `json:"markets"`
	Directives []TimedDirective    `json:"directives"`
}

// TimedMarketInput binds one recorded venue book to the exact logical arrival
// or recovery offset at which the simulator may consume it.
type TimedMarketInput struct {
	Offset uint64      `json:"offset_nanos"`
	Market MarketInput `json:"market"`
}

// TimedDirective binds a recorded simulated venue response to one exact phase
// and logical offset. It prevents replay from inventing an order response.
type TimedDirective struct {
	Exchange  string        `json:"exchange"`
	Phase     TimelinePhase `json:"phase"`
	Offset    uint64        `json:"offset_nanos"`
	Directive LegDirective  `json:"directive"`
}

// ReductionInput carries the independently captured economic attribution and
// reconciliation comparison required after a recorded two-venue simulation.
// It is optional for evaluation-only records, but mandatory before a record
// can drive the full saga reduction.
type ReductionInput struct {
	Attribution    PortfolioAttribution `json:"attribution"`
	Reconciliation ReconciliationInput  `json:"reconciliation"`
}

// ReconciliationInput fixes one expected-versus-observed comparison at an
// immutable time. It deliberately contains projections rather than a live
// reconciliation client.
type ReconciliationInput struct {
	Scope    string               `json:"scope"`
	Expected reconciliation.State `json:"expected"`
	Actual   reconciliation.State `json:"actual"`
	At       time.Time            `json:"at"`
}

// EvaluationInput rebuilds the exact local views and coherent reference used
// by Evaluate. The result is entirely in-process and fail-closed.
func (input Input) EvaluationInput() (EvaluationInput, error) {
	if input.Ordinal == 0 || input.LogicalTime == 0 || input.Now.IsZero() ||
		input.Now.Location() != time.UTC || len(input.Markets) != 2 ||
		input.ConfigurationHash == "" || input.InstrumentMetadataSetHash == "" ||
		input.Coherent.Identity == "" {
		return EvaluationInput{}, strategyError("decision_input_invalid")
	}
	markets := make([]Market, 0, len(input.Markets))
	for _, recorded := range input.Markets {
		market, err := recorded.market()
		if err != nil {
			return EvaluationInput{}, err
		}
		markets = append(markets, market)
	}
	view, err := runtimecore.RestoreCoherentView(input.Coherent.Identity, input.Coherent.Policy,
		input.Coherent.Trigger, input.Coherent.Members)
	if err != nil || view.Trigger().MonotonicNanos != input.LogicalTime {
		return EvaluationInput{}, strategyError("decision_input_coherent_invalid")
	}
	return EvaluationInput{CoherentView: view, Markets: markets,
		Inventory: append([]VenueInventory(nil), input.Inventory...), QuoteBudget: input.QuoteBudget,
		FeeBalances: cloneFeeBalances(input.FeeBalances), DecisionOffsetNanos: input.LogicalTime,
		Configuration: input.Configuration, ConfigurationHash: input.ConfigurationHash,
		InstrumentMetadataSetHash: input.InstrumentMetadataSetHash, Restoration: input.Restoration}, nil
}

// RecordedSimulation restores the exact future books, venue directives,
// reviewed latency distribution, and recovery policy for this decision. It
// performs no exchange or live-market read.
func (input Input) RecordedSimulation() (Timeline, LatencyDistribution, RecoveryPolicy, error) {
	if _, err := input.EvaluationInput(); err != nil {
		return nil, LatencyDistribution{}, RecoveryPolicy{}, err
	}
	if input.Simulation == nil || len(input.Simulation.Markets) == 0 || len(input.Simulation.Directives) == 0 {
		return nil, LatencyDistribution{}, RecoveryPolicy{}, strategyError("simulation_input_unavailable")
	}
	markets := make([]recordedTimedMarket, 0, len(input.Simulation.Markets))
	for _, recorded := range input.Simulation.Markets {
		if recorded.Offset == 0 {
			return nil, LatencyDistribution{}, RecoveryPolicy{}, strategyError("simulation_input_invalid")
		}
		market, err := recorded.Market.market()
		if err != nil {
			return nil, LatencyDistribution{}, RecoveryPolicy{}, err
		}
		for _, prior := range markets {
			if prior.offset == recorded.Offset && prior.market.Book.Exchange() == market.Book.Exchange() &&
				prior.market.Book.Instrument() == market.Book.Instrument() {
				return nil, LatencyDistribution{}, RecoveryPolicy{}, strategyError("simulation_input_invalid")
			}
		}
		markets = append(markets, recordedTimedMarket{offset: recorded.Offset, market: market})
	}
	directives := make([]recordedTimedDirective, 0, len(input.Simulation.Directives))
	for _, recorded := range input.Simulation.Directives {
		if recorded.Exchange == "" || recorded.Offset == 0 || !validTimelinePhase(recorded.Phase) ||
			!validDirectiveState(recorded.Directive.State) {
			return nil, LatencyDistribution{}, RecoveryPolicy{}, strategyError("simulation_input_invalid")
		}
		for _, prior := range directives {
			if prior.exchange == recorded.Exchange && prior.phase == recorded.Phase && prior.offset == recorded.Offset {
				return nil, LatencyDistribution{}, RecoveryPolicy{}, strategyError("simulation_input_invalid")
			}
		}
		directives = append(directives, recordedTimedDirective{exchange: recorded.Exchange, phase: recorded.Phase,
			offset: recorded.Offset, directive: recorded.Directive})
	}
	latency := input.Simulation.Latency
	if _, err := schedule(Candidate{BuyExchange: string(input.Markets[0].Snapshot.Exchange),
		SellExchange: string(input.Markets[1].Snapshot.Exchange), DecisionOffsetNanos: input.LogicalTime}, latency); err != nil {
		return nil, LatencyDistribution{}, RecoveryPolicy{}, strategyError("simulation_input_invalid")
	}
	policy := input.Simulation.Recovery
	if policy.MaximumRetries > 1 || (policy.RiskAllowsRetry && policy.MaximumRetries != 1) ||
		(!policy.RiskAllowsRetry && policy.MaximumRetries != 0) {
		return nil, LatencyDistribution{}, RecoveryPolicy{}, strategyError("simulation_input_invalid")
	}
	return recordedTimeline{markets: markets, directives: directives}, latency, policy, nil
}

type recordedTimedMarket struct {
	offset uint64
	market Market
}

type recordedTimedDirective struct {
	exchange  string
	phase     TimelinePhase
	offset    uint64
	directive LegDirective
}

type recordedTimeline struct {
	markets    []recordedTimedMarket
	directives []recordedTimedDirective
}

// MarketAt returns the exact recorded venue book at the requested offset.
func (timeline recordedTimeline) MarketAt(exchange string, instrument domain.Instrument, offset uint64) (Market, error) {
	for _, item := range timeline.markets {
		if item.offset == offset && item.market.Book.Exchange() == exchange && item.market.Book.Instrument() == instrument {
			return item.market, nil
		}
	}
	return Market{}, strategyError("simulation_input_market_unavailable")
}

// DirectiveAt returns the deterministic recorded leg directive for a phase.
func (timeline recordedTimeline) DirectiveAt(exchange string, phase TimelinePhase, offset uint64) (LegDirective, error) {
	for _, item := range timeline.directives {
		if item.exchange == exchange && item.phase == phase && item.offset == offset {
			return item.directive, nil
		}
	}
	return LegDirective{}, strategyError("simulation_input_directive_unavailable")
}

func validTimelinePhase(phase TimelinePhase) bool {
	return phase == PhaseArrival || phase == PhaseVerification || phase == PhaseRetry
}

func validDirectiveState(state execution.OrderState) bool {
	switch state {
	case execution.OrderCreated, execution.OrderPartiallyFilled, execution.OrderFilled, execution.OrderCanceled,
		execution.OrderRejected, execution.OrderExpired, execution.OrderUnknown, execution.OrderRecoveryRequired:
		return true
	default:
		return false
	}
}

// RecordedSagaSimulationInputs adapts only simulation evidence embedded in a
// canonical input to the shared saga broker. It cannot read live data.
type RecordedSagaSimulationInputs struct{}

// SimulationInput returns the input's recorded timeline, latency, and policy.
func (RecordedSagaSimulationInputs) SimulationInput(input Input) (Timeline, LatencyDistribution, RecoveryPolicy, error) {
	return input.RecordedSimulation()
}

// RecordedRiskInput restores only the central-risk evidence captured with the
// decision. It cannot substitute newer policies, inventory facts, or clock
// values when recorded evidence is processed later.
func (input Input) RecordedRiskInput() (RiskInput, error) {
	if _, err := input.EvaluationInput(); err != nil {
		return RiskInput{}, err
	}
	if input.CentralRisk == nil || len(input.CentralRisk.Policies) == 0 ||
		input.CentralRisk.EvaluatedAt.IsZero() || input.CentralRisk.EvaluatedAt.Location() != time.UTC {
		return RiskInput{}, strategyError("risk_input_unavailable")
	}
	return cloneRiskInput(*input.CentralRisk), nil
}

// RecordedSagaRiskInputs adapts the immutable central-risk evidence embedded
// in a canonical input to the shared saga risk stage. It has no live source.
type RecordedSagaRiskInputs struct{}

// RiskInput returns a defensive copy of the recorded central-risk evidence.
func (RecordedSagaRiskInputs) RiskInput(input Input) (RiskInput, error) {
	return input.RecordedRiskInput()
}

// RecordedReduction restores only the attribution and reconciliation evidence
// captured for a complete simulated pair. It cannot obtain a current venue or
// portfolio projection.
func (input Input) RecordedReduction() (ReductionInput, error) {
	if _, err := input.EvaluationInput(); err != nil {
		return ReductionInput{}, err
	}
	if input.Reduction == nil || input.Reduction.Reconciliation.Scope == "" ||
		input.Reduction.Reconciliation.At.IsZero() || input.Reduction.Reconciliation.At.Location() != time.UTC {
		return ReductionInput{}, strategyError("reduction_input_unavailable")
	}
	return cloneReductionInput(*input.Reduction), nil
}

func cloneReductionInput(input ReductionInput) ReductionInput {
	result := input
	result.Reconciliation.Expected = cloneReconciliationState(input.Reconciliation.Expected)
	result.Reconciliation.Actual = cloneReconciliationState(input.Reconciliation.Actual)
	return result
}

func cloneReconciliationState(input reconciliation.State) reconciliation.State {
	result := input
	result.Duplicates = append([]string(nil), input.Duplicates...)
	result.Differences = append([]reconciliation.Discrepancy(nil), input.Differences...)
	return result
}

func cloneRiskInput(input RiskInput) RiskInput {
	result := input
	result.Policies = append([]risk.Policy(nil), input.Policies...)
	result.Observations = cloneRiskObservations(input.Observations)
	return result
}

func cloneRiskObservations(input risk.Observations) risk.Observations {
	result := input
	result.AccountDrawdown = cloneRiskPointer(input.AccountDrawdown)
	result.UTCDayLoss = cloneRiskPointer(input.UTCDayLoss)
	result.Rolling24HourLoss = cloneRiskPointer(input.Rolling24HourLoss)
	result.StrategyLoss = cloneRiskPointer(input.StrategyLoss)
	result.AssetExposure = cloneRiskPointer(input.AssetExposure)
	result.CombinedExposure = cloneRiskPointer(input.CombinedExposure)
	result.ExchangeExposure = cloneRiskPointer(input.ExchangeExposure)
	result.Reserve = cloneRiskPointer(input.Reserve)
	result.ReservedCapital = cloneRiskPointer(input.ReservedCapital)
	result.Spread = cloneRiskPointer(input.Spread)
	result.Slippage = cloneRiskPointer(input.Slippage)
	result.OpenOrders = cloneRiskPointer(input.OpenOrders)
	result.BookAge = cloneRiskPointer(input.BookAge)
	result.QueueLag = cloneRiskPointer(input.QueueLag)
	result.ClockDrift = cloneRiskPointer(input.ClockDrift)
	result.QualityScore = cloneRiskPointer(input.QualityScore)
	result.Health.Gap = cloneRiskPointer(input.Health.Gap)
	result.Health.StaleData = cloneRiskPointer(input.Health.StaleData)
	result.Health.ReconciliationFault = cloneRiskPointer(input.Health.ReconciliationFault)
	result.Health.AccountingFault = cloneRiskPointer(input.Health.AccountingFault)
	result.Health.UnknownOrder = cloneRiskPointer(input.Health.UnknownOrder)
	result.Health.PersistenceFault = cloneRiskPointer(input.Health.PersistenceFault)
	result.Health.DiskFault = cloneRiskPointer(input.Health.DiskFault)
	result.Health.APIError = cloneRiskPointer(input.Health.APIError)
	result.Health.LeaseLost = cloneRiskPointer(input.Health.LeaseLost)
	return result
}

func cloneRiskPointer[T any](value *T) *T {
	if value == nil {
		return nil
	}
	result := *value
	return &result
}

func (input MarketInput) market() (Market, error) {
	if input.Observation.Validate() != nil || input.Snapshot.Exchange == "" ||
		input.Snapshot.Instrument != input.Rules.Metadata.Instrument ||
		input.Snapshot.LastSequence != input.Observation.SourceSequence ||
		input.Snapshot.RawPayloadHash == "" || len(input.Snapshot.Bids) == 0 ||
		len(input.Snapshot.Asks) == 0 || input.Rules.Exchange != string(input.Snapshot.Exchange) ||
		!input.Rules.Active {
		return Market{}, strategyError("decision_input_market_invalid")
	}
	depth := len(input.Snapshot.Bids)
	if len(input.Snapshot.Asks) > depth {
		depth = len(input.Snapshot.Asks)
	}
	if depth > 1000 {
		return Market{}, strategyError("decision_input_market_invalid")
	}
	book, err := marketdata.NewBook(string(input.Snapshot.Exchange), input.Snapshot.Instrument, depth, depth, nil)
	if err != nil || book.BeginGeneration(input.Observation.ConnectionID, input.Observation.ConnectionGeneration) != nil ||
		book.ReplaceSnapshot(input.Snapshot, input.Observation) != nil {
		return Market{}, strategyError("decision_input_market_invalid")
	}
	view := book.View()
	if view.Health() != marketdata.HealthHealthy || view.Version() != 1 ||
		view.Sequence() != input.Snapshot.LastSequence {
		return Market{}, strategyError("decision_input_market_invalid")
	}
	return Market{Book: view, Rules: input.Rules}, nil
}

func cloneFeeBalances(values map[string]domain.Balance) map[string]domain.Balance {
	if len(values) == 0 {
		return nil
	}
	result := make(map[string]domain.Balance, len(values))
	for key, balance := range values {
		result[key] = balance
	}
	return result
}

// ValidateEventBinding rejects a replay envelope that is not the exact
// canonical decision input being evaluated.
func (input Input) ValidateEventBinding(ordinal, logicalTime uint64) error {
	if input.Ordinal != ordinal || input.LogicalTime != logicalTime {
		return strategyError("decision_input_event_mismatch")
	}
	_, err := input.EvaluationInput()
	return err
}
