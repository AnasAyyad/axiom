package triangular

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	"axiom/internal/domain"
	"axiom/internal/strategies/arbitrage"
)

var approvedCycles = []struct {
	id   Cycle
	path []string
}{
	{CycleUSDTBTCETHUSDT, []string{"USDT", "BTC", "ETH", "USDT"}},
	{CycleUSDTETHBTCUSDT, []string{"USDT", "ETH", "BTC", "USDT"}},
}

// Evaluate exhaustively evaluates both approved cycles at every configured or
// dynamically clipped exact size. Rejected sizes never become candidates.
func Evaluate(input EvaluationInput) ([]Candidate, error) {
	if err := validateInput(input); err != nil {
		return nil, err
	}
	capacity, err := settlementCapacity(input)
	if err != nil {
		return nil, err
	}
	sizes := evaluationSizes(input.Configuration.SizeLadder, capacity)
	markets, err := canonicalMarkets(input)
	if err != nil {
		return nil, err
	}
	candidates := make([]Candidate, 0, len(sizes)*len(approvedCycles))
	for _, cycle := range approvedCycles {
		for _, size := range sizes {
			candidate, candidateErr := evaluateCycle(input, markets, cycle.id, cycle.path, size)
			if candidateErr == nil {
				candidates = append(candidates, candidate)
			}
		}
	}
	sort.Slice(candidates, func(left, right int) bool {
		if candidates[left].Cycle != candidates[right].Cycle {
			return candidates[left].Cycle < candidates[right].Cycle
		}
		return candidates[left].Start.Compare(candidates[right].Start) < 0
	})
	if len(candidates) == 0 {
		return nil, strategyError("no_eligible_cycle")
	}
	return candidates, nil
}

func evaluateCycle(
	input EvaluationInput,
	markets []Market,
	cycle Cycle,
	path []string,
	start domain.Quantity,
) (Candidate, error) {
	legs, final, err := convertCycle(input, markets, path, start)
	if err != nil {
		return Candidate{}, err
	}
	if err = requireFeeBalances(input.FeeBalances, legs); err != nil {
		return Candidate{}, err
	}
	economics, err := calculateCycleEconomics(input.Configuration, start, final, len(legs))
	if err != nil {
		return Candidate{}, err
	}
	claims, err := claimRequirements(input, legs, start)
	if err != nil {
		return Candidate{}, err
	}
	candidate := newCandidate(input, cycle, start, legs, claims, economics)
	candidate.ID = candidateIdentity(candidate)
	return candidate, nil
}

func convertCycle(
	input EvaluationInput,
	markets []Market,
	path []string,
	start domain.Quantity,
) ([]arbitrage.Result, domain.Quantity, error) {
	current := start
	legs := make([]arbitrage.Result, 0, 3)
	for index := 0; index < 3; index++ {
		source, _ := domain.ParseAssetSymbol(path[index])
		target, _ := domain.ParseAssetSymbol(path[index+1])
		market, err := selectMarket(markets, source, target)
		if err != nil || !market.Book.Eligible(input.DecisionOffsetNanos, input.Configuration.MaximumBookAge) {
			return nil, domain.Quantity{}, strategyError("market_ineligible")
		}
		result, err := arbitrage.Convert(arbitrage.Request{
			Source: source, Target: target, Input: current, Book: market.Book, Rules: market.Rules,
		})
		if err != nil {
			return nil, domain.Quantity{}, strategyError("conversion_rejected")
		}
		legs = append(legs, result)
		current = result.NetOutput
	}
	return legs, current, nil
}

type cycleEconomics struct {
	final, worst            domain.Quantity
	expectedNet, worstNet   domain.PnL
	expectedEdge, worstEdge domain.Percent
	latencyDeterioration    domain.Money
}

func calculateCycleEconomics(
	configuration Configuration,
	start, final domain.Quantity,
	legCount int,
) (cycleEconomics, error) {
	worst := final
	var err error
	for range legCount {
		worst, err = arbitrage.HaircutQuantity(worst, configuration.LatencyDeterioration)
		if err != nil {
			return cycleEconomics{}, strategyError("latency_model_invalid")
		}
	}
	expectedEdge, err := arbitrage.PositiveEdge(final, start)
	if err != nil {
		return cycleEconomics{}, strategyError("expected_edge_nonpositive")
	}
	worstEdge, err := arbitrage.PositiveEdge(worst, start)
	if err != nil || worstEdge.Compare(configuration.AdditionalSafetyMargin) <= 0 {
		return cycleEconomics{}, strategyError("safety_margin_failed")
	}
	expectedNet, _ := arbitrage.QuantityDifference(final, start)
	worstNet, _ := arbitrage.QuantityDifference(worst, start)
	latencyQuantity, _ := final.Subtract(worst)
	latencyMoney, _ := domain.ParseMoney(latencyQuantity.String())
	return cycleEconomics{
		final: final, worst: worst, expectedNet: expectedNet, worstNet: worstNet,
		expectedEdge: expectedEdge, worstEdge: worstEdge, latencyDeterioration: latencyMoney,
	}, nil
}

func newCandidate(
	input EvaluationInput,
	cycle Cycle,
	start domain.Quantity,
	legs []arbitrage.Result,
	claims []ClaimRequirement,
	economics cycleEconomics,
) Candidate {
	return Candidate{
		Cycle: cycle, Exchange: input.Exchange, Start: start,
		Final: economics.final, WorstCaseFinal: economics.worst,
		ExpectedNet: economics.expectedNet, WorstCaseNet: economics.worstNet,
		ExpectedEdge: economics.expectedEdge, WorstCaseEdge: economics.worstEdge,
		LatencyDeterioration:   economics.latencyDeterioration,
		AdditionalSafetyMargin: input.Configuration.AdditionalSafetyMargin,
		DetectedOffsetNanos:    input.FirstDetectedOffset, DecisionOffsetNanos: input.DecisionOffsetNanos,
		ExpiresOffsetNanos:  input.FirstDetectedOffset + uint64(input.Configuration.CandidateLifetime),
		MaximumBookAgeNanos: uint64(input.Configuration.MaximumBookAge),
		Legs:                legs, Claims: claims, ConfigurationVersion: input.Configuration.StrategyVersion,
		ConfigurationHash: input.ConfigurationHash, ModelVersion: input.Configuration.ModelVersion,
		InstrumentMetadataID: input.InstrumentMetadataID,
	}
}

func canonicalMarkets(input EvaluationInput) ([]Market, error) {
	markets := append([]Market(nil), input.Markets...)
	sort.Slice(markets, func(left, right int) bool {
		return markets[left].Book.Instrument().Symbol() < markets[right].Book.Instrument().Symbol()
	})
	for index, market := range markets {
		if market.Book.Exchange() != input.Exchange || market.Rules.Exchange != input.Exchange ||
			market.Rules.Metadata.Instrument != market.Book.Instrument() ||
			(index > 0 && markets[index-1].Book.Instrument() == market.Book.Instrument()) {
			return nil, strategyError("market_set_invalid")
		}
	}
	if len(markets) != 3 {
		return nil, strategyError("market_set_invalid")
	}
	return markets, nil
}

func selectMarket(markets []Market, source, target domain.AssetSymbol) (Market, error) {
	for _, market := range markets {
		instrument := market.Book.Instrument()
		if (instrument.Base == source && instrument.Quote == target) ||
			(instrument.Base == target && instrument.Quote == source) {
			return market, nil
		}
	}
	return Market{}, strategyError("native_orientation_unavailable")
}

func validateInput(input EvaluationInput) error {
	configuration := input.Configuration
	if input.Exchange == "" || input.DecisionOffsetNanos == 0 || input.FirstDetectedOffset == 0 ||
		input.DecisionOffsetNanos < input.FirstDetectedOffset ||
		input.DecisionOffsetNanos-input.FirstDetectedOffset > uint64(configuration.CandidateLifetime) ||
		input.ConfigurationHash == "" || input.InstrumentMetadataID == "" ||
		configuration.StrategyVersion != "triangular.v1b.1" ||
		configuration.ModelVersion != "triangular-exact-depth.v1" ||
		len(configuration.SizeLadder) != 4 || configuration.MaximumBookAge <= 0 ||
		configuration.CandidateLifetime <= 0 ||
		configuration.MaximumCycleNotional.String() != "100" ||
		!configuration.DynamicSizing || configuration.MaximumRecoveryAttempts != 1 ||
		configuration.OpportunityMetricWindow != 1000 ||
		configuration.ClaimModel != "atomic-multi-resource.v1" {
		return strategyError("configuration_invalid")
	}
	wanted := []string{"10", "25", "50", "100"}
	for index, size := range configuration.SizeLadder {
		if size.String() != wanted[index] {
			return strategyError("size_ladder_invalid")
		}
	}
	zero, _ := domain.ParseBalance("0")
	if input.AvailableSettlement.Compare(zero) <= 0 || input.StrategyBudget.Compare(zero) <= 0 ||
		input.RecoveryAllowance.Compare(zero) <= 0 {
		return strategyError("capital_invalid")
	}
	return nil
}

func settlementCapacity(input EvaluationInput) (domain.Quantity, error) {
	available, err := input.AvailableSettlement.Subtract(input.GlobalReserveFloor)
	if err != nil {
		return domain.Quantity{}, strategyError("global_reserve_failed")
	}
	if available.Compare(input.StrategyBudget) > 0 {
		available = input.StrategyBudget
	}
	maximum, _ := domain.ParseBalance(input.Configuration.MaximumCycleNotional.String())
	if available.Compare(maximum) > 0 {
		available = maximum
	}
	available, err = available.Subtract(input.RecoveryAllowance)
	if err != nil {
		return domain.Quantity{}, strategyError("recovery_allowance_failed")
	}
	return domain.ParseQuantity(available.String())
}

func evaluationSizes(ladder []domain.Quantity, capacity domain.Quantity) []domain.Quantity {
	zero, _ := domain.ParseQuantity("0")
	byValue := make(map[string]domain.Quantity, len(ladder)+1)
	for _, size := range ladder {
		clipped := size
		if clipped.Compare(capacity) > 0 {
			clipped = capacity
		}
		if clipped.Compare(zero) > 0 {
			byValue[clipped.String()] = clipped
		}
	}
	if capacity.Compare(zero) > 0 {
		byValue[capacity.String()] = capacity
	}
	sizes := make([]domain.Quantity, 0, len(byValue))
	for _, size := range byValue {
		sizes = append(sizes, size)
	}
	sort.Slice(sizes, func(left, right int) bool { return sizes[left].Compare(sizes[right]) < 0 })
	return sizes
}

func requireFeeBalances(balances map[domain.AssetSymbol]domain.Balance, legs []arbitrage.Result) error {
	required := make(map[domain.AssetSymbol]domain.Balance)
	for _, leg := range legs {
		fee, err := domain.ParseBalance(leg.FeeQuantity.String())
		if err != nil {
			return strategyError("fee_invalid")
		}
		current, exists := required[leg.FeeAsset]
		if !exists {
			current, _ = domain.ParseBalance("0")
		}
		required[leg.FeeAsset], err = current.Add(fee)
		if err != nil {
			return strategyError("fee_invalid")
		}
	}
	for asset, amount := range required {
		owned, exists := balances[asset]
		if !exists || owned.Compare(amount) < 0 {
			return strategyError("fee_balance_insufficient")
		}
	}
	return nil
}

func candidateIdentity(candidate Candidate) string {
	copy := candidate
	copy.ID = ""
	encoded, _ := json.Marshal(copy)
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}
