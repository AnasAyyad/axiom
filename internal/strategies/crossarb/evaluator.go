package crossarb

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	"axiom/internal/domain"
	"axiom/internal/strategies/arbitrage"
)

var approvedDirections = []struct {
	id        Direction
	buy, sell string
}{
	{BuyBinanceSellBybit, "binance", "bybit"},
	{BuyBybitSellBinance, "bybit", "binance"},
}

// Evaluate exhaustively prices both approved venue directions for one BTC-USDT
// or ETH-USDT coherent view and returns only closed-cycle-positive candidates.
func Evaluate(input EvaluationInput) ([]Candidate, error) {
	if err := validateInput(input); err != nil {
		return nil, err
	}
	if err := ValidateCoherentBooks(input.CoherentView, input.Markets,
		input.DecisionOffsetNanos, input.Configuration); err != nil {
		return nil, err
	}
	candidates := make([]Candidate, 0, 2)
	for _, direction := range approvedDirections {
		candidate, err := evaluateDirection(input, direction.id, direction.buy, direction.sell)
		if err == nil {
			candidates = append(candidates, candidate)
		}
	}
	sort.Slice(candidates, func(left, right int) bool {
		return candidates[left].Direction < candidates[right].Direction
	})
	if len(candidates) == 0 {
		return nil, strategyError("no_eligible_direction")
	}
	return candidates, nil
}

// EvaluateUniverse proves both required instruments without merging their
// independent coherent version vectors.
func EvaluateUniverse(inputs []EvaluationInput) ([]Candidate, error) {
	if len(inputs) != 2 {
		return nil, strategyError("instrument_universe_invalid")
	}
	result := make([]Candidate, 0, 4)
	seen := make(map[string]bool, 2)
	for _, input := range inputs {
		candidates, err := Evaluate(input)
		if err != nil {
			return nil, err
		}
		symbol := candidates[0].Instrument.Symbol()
		if seen[symbol] {
			return nil, strategyError("instrument_universe_invalid")
		}
		seen[symbol] = true
		result = append(result, candidates...)
	}
	if !seen["BTCUSDT"] || !seen["ETHUSDT"] {
		return nil, strategyError("instrument_universe_invalid")
	}
	sort.Slice(result, func(left, right int) bool {
		if result[left].Instrument.Symbol() != result[right].Instrument.Symbol() {
			return result[left].Instrument.Symbol() < result[right].Instrument.Symbol()
		}
		return result[left].Direction < result[right].Direction
	})
	return result, nil
}

func evaluateDirection(
	input EvaluationInput,
	direction Direction,
	buyExchange, sellExchange string,
) (Candidate, error) {
	evaluation, err := evaluateDirectionExecution(input, buyExchange, sellExchange)
	if err != nil {
		return Candidate{}, err
	}
	economics, err := closedCycleEconomics(evaluation.buy, evaluation.sell, input.Restoration)
	if err != nil || !positiveEconomics(
		economics, evaluation.budget, input.Configuration.MinimumClosedCycleEdge,
	) {
		return Candidate{}, strategyError("closed_cycle_unprofitable")
	}
	claims, err := buildClaims(
		evaluation.buyInventory.Owner, evaluation.buy, evaluation.sell,
		input.Restoration, input.FeeBalances,
	)
	if err != nil {
		return Candidate{}, err
	}
	candidate := newCandidate(
		input, direction, buyExchange, sellExchange, evaluation, economics, claims,
	)
	candidate.ID = candidateIdentity(candidate)
	return candidate, nil
}

type directionEvaluation struct {
	buyInventory, sellInventory VenueInventory
	control                     InventoryControl
	budget                      domain.Balance
	buy, sell                   arbitrage.Result
}

func evaluateDirectionExecution(
	input EvaluationInput,
	buyExchange, sellExchange string,
) (directionEvaluation, error) {
	buyMarket, sellMarket, err := directionMarkets(input.Markets, buyExchange, sellExchange)
	if err != nil {
		return directionEvaluation{}, err
	}
	buyInventory, sellInventory, err := directionInventory(input.Inventory, buyExchange, sellExchange)
	if err != nil {
		return directionEvaluation{}, err
	}
	control, err := inventoryControl(buyInventory, sellInventory, input.Configuration)
	if err != nil || control.State == BandPaused {
		return directionEvaluation{}, strategyError("direction_inventory_paused")
	}
	budget := eligibleBudget(input.QuoteBudget, control, input.Configuration)
	if buyInventory.OwnedUSDT.Compare(budget) < 0 {
		return directionEvaluation{}, strategyError("buy_inventory_insufficient")
	}
	usdt, _ := domain.ParseAssetSymbol("USDT")
	baseAsset := buyMarket.Book.Instrument().Base
	buy, err := arbitrage.Convert(arbitrage.Request{
		Source: usdt, Target: baseAsset, Input: quantity(budget.String()),
		Book: buyMarket.Book, Rules: buyMarket.Rules,
	})
	if err != nil {
		return directionEvaluation{}, strategyError("buy_conversion_rejected")
	}
	if sellInventory.OwnedBase.Compare(balance(buy.NetOutput.String())) < 0 {
		return directionEvaluation{}, strategyError("sell_inventory_insufficient")
	}
	sell, err := arbitrage.Convert(arbitrage.Request{
		Source: baseAsset, Target: usdt, Input: buy.NetOutput,
		Book: sellMarket.Book, Rules: sellMarket.Rules,
	})
	if err != nil {
		return directionEvaluation{}, strategyError("sell_conversion_rejected")
	}
	return directionEvaluation{
		buyInventory: buyInventory, sellInventory: sellInventory,
		control: control, budget: budget, buy: buy, sell: sell,
	}, nil
}

func newCandidate(
	input EvaluationInput,
	direction Direction,
	buyExchange, sellExchange string,
	evaluation directionEvaluation,
	economics ClosedCycleEconomics,
	claims []ClaimRequirement,
) Candidate {
	return Candidate{
		Direction: direction, Instrument: evaluation.buy.Instrument,
		BuyExchange: buyExchange, SellExchange: sellExchange,
		Buy: evaluation.buy, Sell: evaluation.sell,
		CoherentViewID:            input.CoherentView.Identity(),
		CoherentVersionVectorHash: input.CoherentView.VersionVectorHash(),
		ViewMembers:               input.CoherentView.Members(),
		Inventory:                 evaluation.control,
		Economics:                 economics,
		Rebalancing: rebalancingNeed(
			evaluation.control, evaluation.buyInventory, evaluation.sellInventory, input.Restoration,
		),
		Claims:              claims,
		DetectedOffsetNanos: input.CoherentView.Trigger().MonotonicNanos,
		DecisionOffsetNanos: input.DecisionOffsetNanos,
		ExpiresOffsetNanos: input.DecisionOffsetNanos +
			uint64(input.Configuration.CandidateLifetime),
		ConfigurationVersion:      input.Configuration.StrategyVersion,
		ConfigurationHash:         input.ConfigurationHash,
		ModelVersion:              input.Configuration.ModelVersion,
		InstrumentMetadataSetHash: input.InstrumentMetadataSetHash,
		CreatedAt:                 input.CoherentView.Trigger().UTC,
	}
}

func validateInput(input EvaluationInput) error {
	if !validConfiguration(input.Configuration) || input.ConfigurationHash == "" ||
		input.InstrumentMetadataSetHash == "" || input.DecisionOffsetNanos == 0 ||
		len(input.Markets) != 2 || len(input.Inventory) != 2 ||
		input.Restoration.ModelVersion != "closed-inventory-cycle.v1" ||
		input.Restoration.LatencyModelVersion == "" || input.Restoration.RecoveryModelVersion == "" ||
		input.Restoration.InventoryShadowPriceVersion == "" ||
		input.Restoration.ConcentrationModelVersion == "" {
		return strategyError("configuration_invalid")
	}
	instrument := input.Markets[0].Book.Instrument()
	if input.Markets[1].Book.Instrument() != instrument ||
		(instrument.Symbol() != "BTCUSDT" && instrument.Symbol() != "ETHUSDT") {
		return strategyError("instrument_invalid")
	}
	return nil
}

func directionMarkets(markets []Market, buy, sell string) (Market, Market, error) {
	var buyMarket, sellMarket Market
	for _, market := range markets {
		switch market.Book.Exchange() {
		case buy:
			buyMarket = market
		case sell:
			sellMarket = market
		}
	}
	if buyMarket.Book.Exchange() != buy || sellMarket.Book.Exchange() != sell ||
		buyMarket.Rules.Exchange != buy || sellMarket.Rules.Exchange != sell {
		return Market{}, Market{}, strategyError("market_direction_invalid")
	}
	return buyMarket, sellMarket, nil
}

func directionInventory(inventory []VenueInventory, buy, sell string) (VenueInventory, VenueInventory, error) {
	var buyInventory, sellInventory VenueInventory
	for _, snapshot := range inventory {
		switch snapshot.Exchange {
		case buy:
			buyInventory = snapshot
		case sell:
			sellInventory = snapshot
		}
	}
	if buyInventory.Exchange != buy || sellInventory.Exchange != sell {
		return VenueInventory{}, VenueInventory{}, strategyError("inventory_missing")
	}
	return buyInventory, sellInventory, nil
}

func eligibleBudget(
	requested domain.Balance,
	control InventoryControl,
	configuration Configuration,
) domain.Balance {
	result := requested
	if result.Compare(configuration.MaximumNotional) > 0 {
		result = configuration.MaximumNotional
	}
	if control.State == BandReduced &&
		result.Compare(configuration.ReducedDirectionMaximumNotional) > 0 {
		result = configuration.ReducedDirectionMaximumNotional
	}
	return result
}

func positiveEconomics(economics ClosedCycleEconomics, budget domain.Balance, minimum domain.Percent) bool {
	zero, _ := domain.ParsePnL("0")
	if economics.ExpectedClosedCycleProfit.Compare(zero) <= 0 ||
		economics.WorstClosedCycleProfit.Compare(zero) <= 0 {
		return false
	}
	profit, _ := domain.ParseMoney(economics.WorstClosedCycleProfit.String())
	notional, _ := domain.ParseMoney(budget.String())
	edge, err := domain.CalculatePercent(profit, notional, 18)
	return err == nil && edge.Compare(minimum) > 0
}

func candidateIdentity(candidate Candidate) string {
	copy := candidate
	copy.ID = ""
	encoded, _ := json.Marshal(copy)
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func quantity(value string) domain.Quantity {
	result, err := domain.ParseQuantity(value)
	if err != nil {
		panic(err)
	}
	return result
}
