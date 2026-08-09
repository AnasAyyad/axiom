package crossarb

import (
	"sort"

	"axiom/internal/domain"
	"axiom/internal/execution"
	"axiom/internal/strategies/arbitrage"
)

// VenueBalances is one exact strategy-owned available-balance projection per
// venue. Cross-Exchange deliberately keeps the two accounts separate.
type VenueBalances map[string]map[domain.AssetSymbol]domain.Balance

// PreferredCandidate returns the same deterministic winner ordering used by
// the atomic allocator.
func PreferredCandidate(candidates []Candidate) (Candidate, error) {
	if len(candidates) == 0 {
		return Candidate{}, strategyError("preferred_candidate_unavailable")
	}
	ordered := rankedCandidates(candidates)
	if ordered[0].ID == "" || ordered[0].BuyExchange == ordered[0].SellExchange ||
		len(ordered[0].Claims) == 0 {
		return Candidate{}, strategyError("preferred_candidate_invalid")
	}
	return ordered[0], nil
}

// ProjectVenueBalances applies an exact terminal two-leg virtual simulation
// without merging venue ownership. The current recorded simulator does not
// expose the exact protected-unwind fills, so recovered or quarantined results
// fail closed instead of inventing a post-recovery balance.
func ProjectVenueBalances(
	before VenueBalances,
	candidate Candidate,
	result SimulationResult,
) (VenueBalances, error) {
	if len(before) != 2 || candidate.ID == "" || result.CandidateID != candidate.ID ||
		result.CanonicalHash == "" || result.Saga.State != execution.PlanCompleted ||
		result.Outcome != OutcomeBothFilled || result.Recovery != (RecoveryResult{}) ||
		len(result.Legs) != 2 || result.ActualBuy == nil || result.ActualSell == nil {
		return nil, strategyError("portfolio_projection_invalid")
	}
	projected := cloneVenueBalances(before)
	for _, leg := range []struct {
		exchange string
		result   *arbitrage.Result
	}{{candidate.BuyExchange, result.ActualBuy}, {candidate.SellExchange, result.ActualSell}} {
		if leg.result == nil || leg.result.Exchange != leg.exchange ||
			leg.result.Instrument != candidate.Instrument {
			return nil, strategyError("portfolio_projection_invalid")
		}
		consumed, err := leg.result.Input.Subtract(leg.result.SourceDust)
		if err != nil || subtractVenueQuantity(projected, leg.exchange, leg.result.Source, consumed) != nil ||
			addVenueQuantity(projected, leg.exchange, leg.result.Target, leg.result.NetOutput) != nil {
			return nil, strategyError("portfolio_projection_invalid")
		}
		if leg.result.FeeAsset != leg.result.Source && leg.result.FeeAsset != leg.result.Target &&
			subtractVenueQuantity(projected, leg.exchange, leg.result.FeeAsset, leg.result.FeeQuantity) != nil {
			return nil, strategyError("portfolio_projection_invalid")
		}
	}
	return projected, nil
}

func cloneVenueBalances(values VenueBalances) VenueBalances {
	result := make(VenueBalances, len(values))
	for exchange, balances := range values {
		result[exchange] = make(map[domain.AssetSymbol]domain.Balance, len(balances))
		for asset, balance := range balances {
			result[exchange][asset] = balance
		}
	}
	return result
}

func subtractVenueQuantity(values VenueBalances, exchange string, asset domain.AssetSymbol,
	quantity domain.Quantity,
) error {
	amount, err := domain.ParseBalance(quantity.String())
	balances, exists := values[exchange]
	current, found := balances[asset]
	if err != nil || !exists || !found {
		return strategyError("portfolio_projection_invalid")
	}
	balances[asset], err = current.Subtract(amount)
	if err != nil {
		return strategyError("portfolio_projection_invalid")
	}
	return nil
}

func addVenueQuantity(values VenueBalances, exchange string, asset domain.AssetSymbol,
	quantity domain.Quantity,
) error {
	amount, err := domain.ParseBalance(quantity.String())
	balances, exists := values[exchange]
	current, found := balances[asset]
	if err != nil || !exists || !found {
		return strategyError("portfolio_projection_invalid")
	}
	balances[asset], err = current.Add(amount)
	if err != nil {
		return strategyError("portfolio_projection_invalid")
	}
	return nil
}

type venueBalanceFact struct {
	Exchange  string             `json:"exchange"`
	Asset     domain.AssetSymbol `json:"asset"`
	Available domain.Balance     `json:"available"`
}

func canonicalVenueBalances(values VenueBalances) []venueBalanceFact {
	result := make([]venueBalanceFact, 0, 6)
	for exchange, balances := range values {
		for asset, available := range balances {
			result = append(result, venueBalanceFact{Exchange: exchange, Asset: asset, Available: available})
		}
	}
	sort.Slice(result, func(left, right int) bool {
		if result[left].Exchange != result[right].Exchange {
			return result[left].Exchange < result[right].Exchange
		}
		return result[left].Asset < result[right].Asset
	})
	return result
}
