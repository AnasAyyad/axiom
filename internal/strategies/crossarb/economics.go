package crossarb

import (
	"axiom/internal/domain"
	"axiom/internal/strategies/arbitrage"
)

func closedCycleEconomics(
	buy, sell arbitrage.Result,
	restoration RestorationEconomics,
) (ClosedCycleEconomics, error) {
	gross, err := quantityDifference(sell.GrossOutput, quantityFromNotional(buy.Notional))
	if err != nil {
		return ClosedCycleEconomics{}, err
	}
	buySpent, err := buy.Input.Subtract(buy.SourceDust)
	if err != nil {
		return ClosedCycleEconomics{}, strategyError("buy_cost_invalid")
	}
	executionPnL, err := quantityDifference(sell.NetOutput, buySpent)
	if err != nil {
		return ClosedCycleEconomics{}, err
	}
	spreadDepth, err := buy.SpreadCost.Add(sell.SpreadCost)
	if err != nil {
		return ClosedCycleEconomics{}, strategyError("cost_overflow")
	}
	costs, err := expectedRestorationCosts(restoration)
	if err != nil {
		return ClosedCycleEconomics{}, err
	}
	expected, err := subtractMoney(executionPnL, costs)
	if err != nil {
		return ClosedCycleEconomics{}, err
	}
	worst, err := subtractMoney(expected, restoration.MaximumOneLegLoss)
	if err != nil {
		return ClosedCycleEconomics{}, err
	}
	return ClosedCycleEconomics{
		GrossSpread: gross, BuyFee: buy.FeeQuoteEquivalent, SellFee: sell.FeeQuoteEquivalent,
		SpreadDepthCost: spreadDepth, LatencyDeterioration: restoration.LatencyDeterioration,
		RecoveryAllowance: restoration.RecoveryAllowance, ExpectedExecutionPnL: executionPnL,
		MaximumOneLegLoss:             restoration.MaximumOneLegLoss,
		MarginalInventoryReplacement:  restoration.MarginalInventoryReplacement,
		NaturalReversalCost:           restoration.NaturalReversalCost,
		AdvisoryRebalancingCost:       restoration.AdvisoryRebalancingCost,
		ExchangeConcentrationPenalty:  restoration.ExchangeConcentrationPenalty,
		USDTVenueConcentrationPenalty: restoration.USDTVenueConcentrationPenalty,
		ExpectedClosedCycleProfit:     expected, WorstClosedCycleProfit: worst,
		RestorationDelayNanos: restoration.EstimatedRestorationDelayNanos,
	}, nil
}

func expectedRestorationCosts(restoration RestorationEconomics) (domain.Money, error) {
	total := money("0")
	values := []domain.Money{
		restoration.LatencyDeterioration, restoration.RecoveryAllowance,
		restoration.MarginalInventoryReplacement, restoration.NaturalReversalCost,
		restoration.AdvisoryRebalancingCost, restoration.ExchangeConcentrationPenalty,
		restoration.USDTVenueConcentrationPenalty,
	}
	for _, value := range values {
		var err error
		total, err = total.Add(value)
		if err != nil {
			return domain.Money{}, strategyError("cost_overflow")
		}
	}
	return total, nil
}

func subtractMoney(value domain.PnL, cost domain.Money) (domain.PnL, error) {
	parsed, err := domain.ParsePnL(cost.String())
	if err != nil {
		return domain.PnL{}, strategyError("cost_invalid")
	}
	result, err := value.Subtract(parsed)
	if err != nil {
		return domain.PnL{}, strategyError("cost_overflow")
	}
	return result, nil
}

func quantityDifference(left, right domain.Quantity) (domain.PnL, error) {
	value, err := arbitrage.QuantityDifference(left, right)
	if err != nil {
		return domain.PnL{}, strategyError("economics_invalid")
	}
	return value, nil
}

func quantityFromNotional(value domain.Notional) domain.Quantity {
	result, err := domain.ParseQuantity(value.String())
	if err != nil {
		panic(err)
	}
	return result
}

func money(value string) domain.Money {
	result, err := domain.ParseMoney(value)
	if err != nil {
		panic(err)
	}
	return result
}
