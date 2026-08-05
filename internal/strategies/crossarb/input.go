package crossarb

import (
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/marketdata"
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
