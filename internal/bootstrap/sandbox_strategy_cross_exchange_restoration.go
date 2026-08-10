package bootstrap

import (
	"fmt"

	"axiom/internal/config"
	"axiom/internal/domain"
	"axiom/internal/strategies/crossarb"
)

// sandboxCrossExchangeRestoration implements the code-owned deterministic
// sandbox restoration model. Every monetary charge is derived from the exact
// synchronized books, reviewed fee model, current risk measurements, and
// strategy-owned 50/50 inventory facts; no optimistic constant is injected.
func sandboxCrossExchangeRestoration(
	product config.Configuration,
	market SandboxCrossExchangeMarketInput,
	inventories []crossarb.VenueInventory,
	budget domain.Balance,
	riskSpread domain.Percent,
	riskSlippage domain.Percent,
) (crossarb.RestorationEconomics, error) {
	if len(market.Markets) != 2 || len(inventories) != 2 ||
		product.Models.Fee == "" || product.Models.Latency == "" {
		return crossarb.RestorationEconomics{}, fmt.Errorf("sandbox_cross_exchange_restoration_invalid")
	}
	configuration, err := crossarb.ConfigurationFromReviewed(product.CrossExchange)
	if err != nil {
		return crossarb.RestorationEconomics{}, fmt.Errorf("sandbox_cross_exchange_restoration_invalid")
	}
	rates, err := calculateSandboxCrossRestorationRates(market, inventories, riskSpread, riskSlippage)
	if err != nil {
		return crossarb.RestorationEconomics{}, err
	}
	costs, err := sandboxCrossRestorationCosts(budget, rates)
	if err != nil || costs.recovery.String() == "0" || costs.oneLegLoss.String() == "0" {
		return crossarb.RestorationEconomics{}, fmt.Errorf("sandbox_cross_exchange_restoration_invalid")
	}
	zeroBalance, _ := domain.ParseBalance("0")
	naturalReverse := true
	for _, inventory := range inventories {
		naturalReverse = naturalReverse && inventory.OwnedBase.Compare(zeroBalance) > 0 &&
			inventory.OwnedUSDT.Compare(zeroBalance) > 0
	}
	zeroPercent, _ := domain.ParsePercent("0")
	return crossarb.RestorationEconomics{ModelVersion: "closed-inventory-cycle.v1",
		LatencyModelVersion:         product.Models.Latency + "+observed-slippage.v1",
		RecoveryModelVersion:        product.Models.Fee + "+cross-exchange-recovery-allowance.v1",
		InventoryShadowPriceVersion: "coherent-spread-shadow-price.v1",
		ConcentrationModelVersion:   "strategy-owned-50-50-concentration.v1",
		LatencyDeterioration:        costs.latency, RecoveryAllowance: costs.recovery,
		MarginalInventoryReplacement: costs.replacement, NaturalReversalCost: costs.natural,
		AdvisoryRebalancingCost: costs.advisory, ExchangeConcentrationPenalty: costs.exchangePenalty,
		USDTVenueConcentrationPenalty: costs.usdtPenalty, MaximumOneLegLoss: costs.oneLegLoss,
		EstimatedRestorationDelayNanos: uint64(configuration.CandidateLifetime),
		NaturalReverseAvailable:        naturalReverse,
		AdvisoryRebalancingRequired:    rates.maximumDeviation.Compare(zeroPercent) > 0}, nil
}

type sandboxCrossRestorationRates struct {
	maximumFee, maximumSpread, slippage, twoFees, recoveryPercent domain.Percent
	baseDeviation, usdtDeviation, maximumDeviation                domain.Percent
}

func calculateSandboxCrossRestorationRates(market SandboxCrossExchangeMarketInput,
	inventories []crossarb.VenueInventory, riskSpread, riskSlippage domain.Percent,
) (sandboxCrossRestorationRates, error) {
	zero, _ := domain.ParsePercent("0")
	result := sandboxCrossRestorationRates{maximumFee: zero, maximumSpread: riskSpread,
		slippage: riskSlippage}
	for _, member := range market.Markets {
		fee, err := domain.ParsePercent(member.Rules.Fee.Rate.String())
		if err != nil {
			return sandboxCrossRestorationRates{}, fmt.Errorf("sandbox_cross_exchange_restoration_invalid")
		}
		if fee.Compare(result.maximumFee) > 0 {
			result.maximumFee = fee
		}
		if len(member.Snapshot.Bids) == 0 || len(member.Snapshot.Asks) == 0 {
			return sandboxCrossRestorationRates{}, fmt.Errorf("sandbox_cross_exchange_restoration_invalid")
		}
		spread, spreadErr := domain.CalculateRelativeSpread(
			member.Snapshot.Bids[0].Price, member.Snapshot.Asks[0].Price, 18,
		)
		if spreadErr != nil {
			return sandboxCrossRestorationRates{}, fmt.Errorf("sandbox_cross_exchange_restoration_invalid")
		}
		if spread.Compare(result.maximumSpread) > 0 {
			result.maximumSpread = spread
		}
	}
	var err error
	result.twoFees, err = result.maximumFee.Add(result.maximumFee)
	if err != nil {
		return sandboxCrossRestorationRates{}, fmt.Errorf("sandbox_cross_exchange_restoration_invalid")
	}
	result.recoveryPercent, err = result.twoFees.Add(result.maximumSpread)
	if err == nil {
		result.recoveryPercent, err = result.recoveryPercent.Add(riskSlippage)
	}
	if err != nil {
		return sandboxCrossRestorationRates{}, fmt.Errorf("sandbox_cross_exchange_restoration_invalid")
	}
	result.baseDeviation, result.usdtDeviation, err = sandboxCrossExchangeConcentration(inventories)
	if err != nil {
		return sandboxCrossRestorationRates{}, fmt.Errorf("sandbox_cross_exchange_restoration_invalid")
	}
	result.maximumDeviation = result.baseDeviation
	if result.usdtDeviation.Compare(result.maximumDeviation) > 0 {
		result.maximumDeviation = result.usdtDeviation
	}
	return result, nil
}

type sandboxCrossRestorationCostSet struct {
	latency, recovery, replacement, natural, advisory domain.Money
	exchangePenalty, usdtPenalty, oneLegLoss          domain.Money
}

func sandboxCrossRestorationCosts(budget domain.Balance,
	rates sandboxCrossRestorationRates,
) (sandboxCrossRestorationCostSet, error) {
	result := sandboxCrossRestorationCostSet{}
	var err error
	result.latency, err = sandboxCrossScaleMoney(budget, rates.slippage)
	if err == nil {
		result.recovery, err = sandboxCrossScaleMoney(budget, rates.recoveryPercent)
	}
	if err == nil {
		result.replacement, err = sandboxCrossScaleMoney(budget, rates.maximumSpread)
	}
	if err == nil {
		result.natural, err = sandboxCrossScaleMoney(budget, rates.twoFees)
	}
	if err == nil {
		result.advisory, err = sandboxCrossConcentrationMoney(budget, rates.maximumDeviation, rates.maximumFee)
	}
	if err == nil {
		result.exchangePenalty, err = sandboxCrossConcentrationMoney(budget, rates.baseDeviation, rates.maximumFee)
	}
	if err == nil {
		result.usdtPenalty, err = sandboxCrossConcentrationMoney(budget, rates.usdtDeviation, rates.maximumFee)
	}
	if err == nil {
		result.oneLegLoss, err = sandboxCrossScaleMoney(budget, rates.recoveryPercent)
	}
	return result, err
}

func sandboxCrossExchangeConcentration(
	inventories []crossarb.VenueInventory,
) (domain.Percent, domain.Percent, error) {
	if len(inventories) != 2 || inventories[0].TotalEligibleBase != inventories[1].TotalEligibleBase ||
		inventories[0].TotalEligibleUSDT != inventories[1].TotalEligibleUSDT {
		return domain.Percent{}, domain.Percent{}, fmt.Errorf("sandbox_cross_exchange_concentration_invalid")
	}
	target, _ := domain.ParsePercent("0.5")
	maximumBase, _ := domain.ParsePercent("0")
	maximumUSDT, _ := domain.ParsePercent("0")
	for _, inventory := range inventories {
		baseShare, err := sandboxCrossInventoryShare(inventory.OwnedBase, inventory.TotalEligibleBase)
		if err != nil {
			return domain.Percent{}, domain.Percent{}, err
		}
		usdtShare, err := sandboxCrossInventoryShare(inventory.OwnedUSDT, inventory.TotalEligibleUSDT)
		if err != nil {
			return domain.Percent{}, domain.Percent{}, err
		}
		baseDeviation := sandboxCrossPercentDistance(baseShare, target)
		usdtDeviation := sandboxCrossPercentDistance(usdtShare, target)
		if baseDeviation.Compare(maximumBase) > 0 {
			maximumBase = baseDeviation
		}
		if usdtDeviation.Compare(maximumUSDT) > 0 {
			maximumUSDT = usdtDeviation
		}
	}
	return maximumBase, maximumUSDT, nil
}

func sandboxCrossInventoryShare(owned, total domain.Balance) (domain.Percent, error) {
	numerator, firstErr := domain.ParseMoney(owned.String())
	denominator, secondErr := domain.ParseMoney(total.String())
	if firstErr != nil || secondErr != nil || denominator.String() == "0" {
		return domain.Percent{}, fmt.Errorf("sandbox_cross_exchange_concentration_invalid")
	}
	return domain.CalculateConservativePercent(numerator, denominator, 18)
}

func sandboxCrossPercentDistance(left, right domain.Percent) domain.Percent {
	if left.Compare(right) >= 0 {
		value, _ := left.Subtract(right)
		return value
	}
	value, _ := right.Subtract(left)
	return value
}

func sandboxCrossScaleMoney(balance domain.Balance, fraction domain.Percent) (domain.Money, error) {
	value, err := domain.ScaleBalanceCeiling(balance, fraction, 18)
	if err != nil {
		return domain.Money{}, fmt.Errorf("sandbox_cross_exchange_restoration_invalid")
	}
	result, err := domain.ParseMoney(value.String())
	if err != nil {
		return domain.Money{}, fmt.Errorf("sandbox_cross_exchange_restoration_invalid")
	}
	return result, nil
}

func sandboxCrossConcentrationMoney(
	budget domain.Balance,
	deviation domain.Percent,
	fee domain.Percent,
) (domain.Money, error) {
	exposed, err := domain.ScaleBalanceCeiling(budget, deviation, 18)
	if err != nil {
		return domain.Money{}, fmt.Errorf("sandbox_cross_exchange_restoration_invalid")
	}
	return sandboxCrossScaleMoney(exposed, fee)
}
