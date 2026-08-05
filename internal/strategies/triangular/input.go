package triangular

import (
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/marketdata"
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
}

// MarketInput retains one complete snapshot and its independent publication
// evidence. RawPayloadHash is retained because the market-data book rejects a
// snapshot without an immutable source identity.
type MarketInput struct {
	Snapshot    exchangecontracts.BookSnapshot `json:"snapshot"`
	Observation marketdata.Observation         `json:"observation"`
	Rules       arbitrage.InstrumentRules      `json:"rules"`
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
