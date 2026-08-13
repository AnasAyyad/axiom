package triangular

import (
	"axiom/internal/domain"
	"axiom/internal/execution"
	"axiom/internal/strategies/arbitrage"
)

// PreferredCandidate returns the same deterministic winner ordering used by
// the atomic allocator. Runtimes use it only to prepare replay and projection
// evidence before the shared pipeline independently repeats the selection.
func PreferredCandidate(candidates []Candidate) (Candidate, error) {
	if len(candidates) == 0 {
		return Candidate{}, strategyError("preferred_candidate_unavailable")
	}
	ordered := rankedCandidates(candidates)
	if ordered[0].ID == "" || len(ordered[0].Legs) != 3 {
		return Candidate{}, strategyError("preferred_candidate_invalid")
	}
	return ordered[0], nil
}

// ProjectAvailableBalances applies one terminal virtual simulation to exact
// owned balances. It preserves source dust and unresolved inventory rather
// than folding either into USDT P&L.
func ProjectAvailableBalances(
	before map[domain.AssetSymbol]domain.Balance,
	candidate Candidate,
	result SimulationResult,
) (map[domain.AssetSymbol]domain.Balance, error) {
	if len(before) == 0 || candidate.ID == "" || result.CandidateID != candidate.ID ||
		result.CanonicalHash == "" || result.Saga.State == "" {
		return nil, strategyError("portfolio_projection_invalid")
	}
	projected := cloneBalances(before)
	settlement, _ := domain.ParseAssetSymbol("USDT")
	if err := subtractProjectedBalance(projected, settlement, candidate.Start); err != nil {
		return nil, err
	}
	for _, leg := range result.Legs {
		if err := addProjectedBalance(projected, leg.Source, leg.SourceDust); err != nil {
			return nil, err
		}
	}
	if result.Recovery.Leg != nil {
		if err := addProjectedBalance(projected, result.Recovery.Leg.Source,
			result.Recovery.Leg.SourceDust); err != nil {
			return nil, err
		}
	}
	if result.Recovery.Quarantined || result.Saga.State == execution.PlanQuarantined {
		if !result.Recovery.Quarantined || result.Recovery.Asset == "" ||
			result.Recovery.Input.String() == "0" {
			return nil, strategyError("portfolio_projection_invalid")
		}
		if err := addProjectedBalance(projected, result.Recovery.Asset, result.Recovery.Input); err != nil {
			return nil, err
		}
		if err := subtractDetachedProjectedFees(projected, result.Legs); err != nil {
			return nil, err
		}
		return projected, nil
	}
	if result.FinalUSDT.String() == "0" {
		return nil, strategyError("portfolio_projection_invalid")
	}
	if err := addProjectedBalance(projected, settlement, result.FinalUSDT); err != nil {
		return nil, err
	}
	return projected, nil
}

func cloneBalances(values map[domain.AssetSymbol]domain.Balance) map[domain.AssetSymbol]domain.Balance {
	result := make(map[domain.AssetSymbol]domain.Balance, len(values)+3)
	for asset, balance := range values {
		result[asset] = balance
	}
	return result
}

func addProjectedBalance(values map[domain.AssetSymbol]domain.Balance,
	asset domain.AssetSymbol, quantity domain.Quantity) error {
	amount, err := domain.ParseBalance(quantity.String())
	if err != nil {
		return strategyError("portfolio_projection_invalid")
	}
	current, exists := values[asset]
	if !exists {
		current, _ = domain.ParseBalance("0")
	}
	values[asset], err = current.Add(amount)
	if err != nil {
		return strategyError("portfolio_projection_invalid")
	}
	return nil
}

func subtractProjectedBalance(values map[domain.AssetSymbol]domain.Balance,
	asset domain.AssetSymbol, quantity domain.Quantity) error {
	amount, err := domain.ParseBalance(quantity.String())
	current, exists := values[asset]
	if err != nil || !exists {
		return strategyError("portfolio_projection_invalid")
	}
	values[asset], err = current.Subtract(amount)
	if err != nil {
		return strategyError("portfolio_projection_invalid")
	}
	return nil
}

func subtractDetachedProjectedFees(
	values map[domain.AssetSymbol]domain.Balance,
	legs []arbitrage.Result,
) error {
	for _, leg := range legs {
		if leg.FeeAsset == leg.Source || leg.FeeAsset == leg.Target {
			continue
		}
		if err := subtractProjectedBalance(values, leg.FeeAsset, leg.FeeQuantity); err != nil {
			return err
		}
	}
	return nil
}
