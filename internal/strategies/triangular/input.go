package triangular

import (
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/marketdata"
	"axiom/internal/reconciliation"
	"axiom/internal/risk"
	"axiom/internal/strategies/arbitrage"
)

// Input is the canonical, replayable decision input for one Triangular
// evaluation. It contains recorded public market facts only. Rebuilding its
// EvaluationInput is deterministic and performs no network I/O.
type Input struct {
	Ordinal              uint64                                `json:"ordinal"`
	LogicalTime          uint64                                `json:"logical_time"`
	Now                  time.Time                             `json:"now"`
	Exchange             string                                `json:"exchange"`
	Markets              []MarketInput                         `json:"markets"`
	FirstDetectedOffset  uint64                                `json:"first_detected_offset_nanos"`
	AvailableSettlement  domain.Balance                        `json:"available_settlement"`
	StrategyBudget       domain.Balance                        `json:"strategy_budget"`
	GlobalReserveFloor   domain.Balance                        `json:"global_reserve_floor"`
	RecoveryAllowance    domain.Balance                        `json:"recovery_allowance"`
	FeeBalances          map[domain.AssetSymbol]domain.Balance `json:"fee_balances"`
	Configuration        Configuration                         `json:"configuration"`
	ConfigurationHash    string                                `json:"configuration_hash"`
	InstrumentMetadataID string                                `json:"instrument_metadata_id"`
	CentralRisk          *RiskInput                            `json:"central_risk,omitempty"`
	Simulation           *SimulationInput                      `json:"simulation,omitempty"`
	Reduction            *ReductionInput                       `json:"reduction,omitempty"`
}

// MarketInput retains one complete snapshot and its independent publication
// evidence. RawPayloadHash is retained because the market-data book rejects a
// snapshot without an immutable source identity.
type MarketInput struct {
	Snapshot    exchangecontracts.BookSnapshot `json:"snapshot"`
	Observation marketdata.Observation         `json:"observation"`
	Rules       arbitrage.InstrumentRules      `json:"rules"`
}

// SimulationInput keeps every future public book required by the deterministic
// sequential simulator alongside the decision evidence. It is optional for
// evaluation-only records, but mandatory before a recorded input can be used
// to simulate a plan.
type SimulationInput struct {
	Latency LatencyModel       `json:"latency"`
	Markets []TimedMarketInput `json:"markets"`
}

// TimedMarketInput binds one recorded executable book to the exact logical
// arrival offset at which the simulator may consume it.
type TimedMarketInput struct {
	Offset uint64      `json:"offset_nanos"`
	Market MarketInput `json:"market"`
}

// ReductionInput carries the independently captured reconciliation comparison
// required after the recorded simulation. It is optional for evaluation-only
// records, but mandatory before a record can drive the full saga reduction.
type ReductionInput struct {
	Reconciliation ReconciliationInput `json:"reconciliation"`
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

// EvaluationInput rebuilds the exact immutable views consumed by Evaluate.
// The generated views are local, healthy snapshots; invalid, stale, crossed,
// or identity-mismatched recordings fail closed before strategy evaluation.
func (input Input) EvaluationInput() (EvaluationInput, error) {
	if input.Ordinal == 0 || input.LogicalTime == 0 || input.Now.IsZero() ||
		input.Now.Location() != time.UTC || input.Exchange == "" ||
		input.FirstDetectedOffset == 0 || input.LogicalTime < input.FirstDetectedOffset ||
		len(input.Markets) != 3 || input.ConfigurationHash == "" ||
		input.InstrumentMetadataID == "" {
		return EvaluationInput{}, strategyError("decision_input_invalid")
	}
	markets := make([]Market, 0, len(input.Markets))
	for _, recorded := range input.Markets {
		market, err := recorded.market(input.Exchange)
		if err != nil {
			return EvaluationInput{}, err
		}
		markets = append(markets, market)
	}
	return EvaluationInput{
		Exchange: input.Exchange, Markets: markets, DecisionOffsetNanos: input.LogicalTime,
		FirstDetectedOffset: input.FirstDetectedOffset,
		AvailableSettlement: input.AvailableSettlement, StrategyBudget: input.StrategyBudget,
		GlobalReserveFloor: input.GlobalReserveFloor, RecoveryAllowance: input.RecoveryAllowance,
		FeeBalances: cloneFeeBalances(input.FeeBalances), Configuration: input.Configuration,
		ConfigurationHash: input.ConfigurationHash, InstrumentMetadataID: input.InstrumentMetadataID,
	}, nil
}

// RecordedSimulation restores the exact recorded future-book timeline and
// reviewed latency model for this decision. It does not access a live book.
func (input Input) RecordedSimulation() (Timeline, LatencyModel, error) {
	if _, err := input.EvaluationInput(); err != nil {
		return nil, LatencyModel{}, err
	}
	if input.Simulation == nil || len(input.Simulation.Markets) == 0 {
		return nil, LatencyModel{}, strategyError("simulation_input_unavailable")
	}
	items := make([]recordedTimedMarket, 0, len(input.Simulation.Markets))
	for _, recorded := range input.Simulation.Markets {
		if recorded.Offset == 0 {
			return nil, LatencyModel{}, strategyError("simulation_input_invalid")
		}
		market, err := recorded.Market.market(input.Exchange)
		if err != nil {
			return nil, LatencyModel{}, err
		}
		for _, prior := range items {
			if prior.offset == recorded.Offset && prior.market.Book.Instrument() == market.Book.Instrument() {
				return nil, LatencyModel{}, strategyError("simulation_input_invalid")
			}
		}
		items = append(items, recordedTimedMarket{offset: recorded.Offset, market: market})
	}
	latency := input.Simulation.Latency
	if latency.Version == "" || latency.LegNanos[0] == 0 || latency.LegNanos[1] == 0 ||
		latency.LegNanos[2] == 0 || latency.RecoveryNanos == 0 {
		return nil, LatencyModel{}, strategyError("simulation_input_invalid")
	}
	return recordedTimeline{exchange: input.Exchange, items: items}, latency, nil
}

type recordedTimedMarket struct {
	offset uint64
	market Market
}

type recordedTimeline struct {
	exchange string
	items    []recordedTimedMarket
}

// MarketAt returns the exact recorded conversion book at the requested offset.
func (timeline recordedTimeline) MarketAt(
	exchange string,
	source, target domain.AssetSymbol,
	offset uint64,
) (Market, error) {
	if exchange != timeline.exchange {
		return Market{}, strategyError("simulation_input_market_unavailable")
	}
	for _, item := range timeline.items {
		instrument := item.market.Book.Instrument()
		if item.offset == offset && ((instrument.Base == source && instrument.Quote == target) ||
			(instrument.Base == target && instrument.Quote == source)) {
			return item.market, nil
		}
	}
	return Market{}, strategyError("simulation_input_market_unavailable")
}

// RecordedSagaSimulationInputs adapts only the simulation evidence embedded
// in one canonical input to the shared saga broker. It cannot read live data.
type RecordedSagaSimulationInputs struct{}

// SimulationInput returns the input's recorded timeline and reviewed latency.
func (RecordedSagaSimulationInputs) SimulationInput(input Input) (Timeline, LatencyModel, error) {
	return input.RecordedSimulation()
}

// RecordedRiskInput restores only the central-risk evidence captured with the
// decision. It cannot substitute current policies, portfolio facts, or clock
// values when a recorded decision is processed later.
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

// RecordedReduction restores only the reconciliation evidence captured for a
// complete simulated cycle. It cannot obtain a current portfolio projection.
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

func (input MarketInput) market(exchange string) (Market, error) {
	if input.Observation.Validate() != nil || input.Snapshot.Exchange != exchangecontracts.ExchangeID(exchange) ||
		input.Snapshot.Instrument != input.Rules.Metadata.Instrument ||
		input.Snapshot.LastSequence != input.Observation.SourceSequence ||
		input.Snapshot.RawPayloadHash == "" || len(input.Snapshot.Bids) == 0 ||
		len(input.Snapshot.Asks) == 0 {
		return Market{}, strategyError("decision_input_market_invalid")
	}
	depth := len(input.Snapshot.Bids)
	if len(input.Snapshot.Asks) > depth {
		depth = len(input.Snapshot.Asks)
	}
	if depth > 1000 || input.Rules.Exchange != exchange || !input.Rules.Active {
		return Market{}, strategyError("decision_input_market_invalid")
	}
	book, err := marketdata.NewBook(exchange, input.Snapshot.Instrument, depth, depth, nil)
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

func cloneFeeBalances(values map[domain.AssetSymbol]domain.Balance) map[domain.AssetSymbol]domain.Balance {
	if len(values) == 0 {
		return nil
	}
	result := make(map[domain.AssetSymbol]domain.Balance, len(values))
	for asset, balance := range values {
		result[asset] = balance
	}
	return result
}

// ValidateEventBinding proves that the replay envelope and canonical input
// refer to the same ordered market decision.
func (input Input) ValidateEventBinding(ordinal, logicalTime uint64) error {
	if input.Ordinal != ordinal || input.LogicalTime != logicalTime {
		return strategyError("decision_input_event_mismatch")
	}
	_, err := input.EvaluationInput()
	return err
}
