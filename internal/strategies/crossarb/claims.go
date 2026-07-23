package crossarb

import (
	"fmt"

	"axiom/internal/domain"
	"axiom/internal/portfolio"
	"axiom/internal/strategies/arbitrage"
)

func buildClaims(
	owner string,
	buy, sell arbitrage.Result,
	restoration RestorationEconomics,
	feeBalances map[string]domain.Balance,
) ([]ClaimRequirement, error) {
	if owner == "" {
		return nil, strategyError("claim_owner_invalid")
	}
	buyCost, err := buy.Input.Subtract(buy.SourceDust)
	if err != nil {
		return nil, strategyError("claim_cost_invalid")
	}
	items := []ClaimRequirement{
		{string(portfolio.ClaimBalance), owner, buy.Exchange, "usdt", balance(buyCost.String())},
		{string(portfolio.ClaimBalance), owner, sell.Exchange,
			string(sell.Instrument.Base), balance(sell.Input.String())},
		{string(portfolio.ClaimLiquidity), owner, buy.Exchange,
			fmt.Sprintf("%s-ask-v%d", buy.Instrument.Symbol(), buy.BookVersion), balance(buy.TradeQuantity.String())},
		{string(portfolio.ClaimLiquidity), owner, sell.Exchange,
			fmt.Sprintf("%s-bid-v%d", sell.Instrument.Symbol(), sell.BookVersion), balance(sell.TradeQuantity.String())},
		{string(portfolio.ClaimRecovery), owner, "portfolio",
			"cross-exchange-usdt", balance(restoration.RecoveryAllowance.String())},
	}
	for _, leg := range []arbitrage.Result{buy, sell} {
		key := leg.Exchange + ":" + string(leg.FeeAsset)
		required := balance(leg.FeeQuantity.String())
		available, ok := feeBalances[key]
		if required.Compare(balance("0")) <= 0 || !ok || available.Compare(required) < 0 {
			return nil, strategyError("fee_buffer_insufficient")
		}
		items = append(items, ClaimRequirement{
			Kind: string(portfolio.ClaimFeeBuffer), Owner: owner, Exchange: leg.Exchange,
			Resource: string(leg.FeeAsset), Quantity: required,
		})
	}
	if restoration.RecoveryAllowance.Compare(money("0")) <= 0 {
		return nil, strategyError("recovery_allowance_invalid")
	}
	return items, nil
}
