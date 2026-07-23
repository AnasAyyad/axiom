package triangular

import (
	"sort"
	"strings"

	"axiom/internal/domain"
	"axiom/internal/strategies/arbitrage"
)

func claimRequirements(input EvaluationInput, legs []arbitrage.Result, start domain.Quantity) ([]ClaimRequirement, error) {
	startBalance, _ := domain.ParseBalance(start.String())
	requirements := []ClaimRequirement{
		{Kind: "balance", Exchange: input.Exchange, Resource: "usdt", Quantity: startBalance},
		{Kind: "recovery", Exchange: input.Exchange, Resource: "usdt", Quantity: input.RecoveryAllowance},
	}
	feeTotals := make(map[domain.AssetSymbol]domain.Balance)
	for _, leg := range legs {
		fee, err := domain.ParseBalance(leg.FeeQuantity.String())
		if err != nil {
			return nil, strategyError("claim_invalid")
		}
		current, exists := feeTotals[leg.FeeAsset]
		if !exists {
			current, _ = domain.ParseBalance("0")
		}
		feeTotals[leg.FeeAsset], err = current.Add(fee)
		if err != nil {
			return nil, strategyError("claim_invalid")
		}
		liquidity, _ := domain.ParseBalance(leg.TradeQuantity.String())
		requirements = append(requirements, ClaimRequirement{
			Kind: "liquidity", Exchange: input.Exchange,
			Resource: strings.ToLower(leg.Instrument.Symbol() + "/" + string(leg.Side) + "/v" + uintString(leg.BookVersion)),
			Quantity: liquidity,
		})
	}
	for asset, amount := range feeTotals {
		if amount.String() == "0" {
			continue
		}
		requirements = append(requirements, ClaimRequirement{
			Kind: "fee_buffer", Exchange: input.Exchange, Resource: strings.ToLower(string(asset)), Quantity: amount,
		})
	}
	sort.Slice(requirements, func(left, right int) bool {
		leftKey := requirements[left].Kind + "/" + requirements[left].Resource
		rightKey := requirements[right].Kind + "/" + requirements[right].Resource
		return leftKey < rightKey
	})
	return requirements, nil
}

func uintString(value uint64) string {
	if value == 0 {
		return "0"
	}
	var buffer [20]byte
	index := len(buffer)
	for value > 0 {
		index--
		buffer[index] = byte('0' + value%10)
		value /= 10
	}
	return string(buffer[index:])
}
