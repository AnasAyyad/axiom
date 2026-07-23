package rebalancing

import "axiom/internal/domain"

// Total returns the exact sum of every cost component.
func (costs CostBreakdown) Total() (domain.Money, error) {
	result, err := costs.Fee.Add(costs.Spread)
	if err != nil {
		return domain.Money{}, routeError("cost_overflow")
	}
	for _, component := range []domain.Money{
		costs.Depth, costs.Delay, costs.NetworkFee, costs.Compatibility,
		costs.VolatilityRisk, costs.OperationalRisk,
	} {
		result, err = result.Add(component)
		if err != nil {
			return domain.Money{}, routeError("cost_overflow")
		}
	}
	return result, nil
}

func addCosts(left, right CostBreakdown) (CostBreakdown, error) {
	add := func(first, second domain.Money) (domain.Money, error) {
		value, err := first.Add(second)
		if err != nil {
			return domain.Money{}, routeError("cost_overflow")
		}
		return value, nil
	}
	result := CostBreakdown{}
	var err error
	if result.Fee, err = add(left.Fee, right.Fee); err != nil {
		return CostBreakdown{}, err
	}
	if result.Spread, err = add(left.Spread, right.Spread); err != nil {
		return CostBreakdown{}, err
	}
	if result.Depth, err = add(left.Depth, right.Depth); err != nil {
		return CostBreakdown{}, err
	}
	if result.Delay, err = add(left.Delay, right.Delay); err != nil {
		return CostBreakdown{}, err
	}
	if result.NetworkFee, err = add(left.NetworkFee, right.NetworkFee); err != nil {
		return CostBreakdown{}, err
	}
	if result.Compatibility, err = add(left.Compatibility, right.Compatibility); err != nil {
		return CostBreakdown{}, err
	}
	if result.VolatilityRisk, err = add(left.VolatilityRisk, right.VolatilityRisk); err != nil {
		return CostBreakdown{}, err
	}
	if result.OperationalRisk, err = add(left.OperationalRisk, right.OperationalRisk); err != nil {
		return CostBreakdown{}, err
	}
	return result, nil
}
