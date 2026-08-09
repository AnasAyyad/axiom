package postgres

import (
	"fmt"
	"sort"
	"strconv"
	"time"

	"axiom/internal/domain"
)

func sandboxTriangularQuoteMovement(
	notional domain.Notional,
	fee, rebate domain.Fee,
	applies bool,
	side domain.Side,
) (domain.Balance, error) {
	gross, err := domain.ParseBalance(notional.String())
	if err != nil || !applies {
		return gross, err
	}
	feeBalance, feeErr := domain.ParseBalance(fee.String())
	rebateBalance, rebateErr := domain.ParseBalance(rebate.String())
	if feeErr != nil || rebateErr != nil {
		return domain.Balance{}, fmt.Errorf("sandbox_accounting_projection_invalid")
	}
	if side == domain.SideBuy {
		gross, err = gross.Add(feeBalance)
		if err == nil {
			gross, err = gross.Subtract(rebateBalance)
		}
	} else {
		gross, err = gross.Add(rebateBalance)
		if err == nil {
			gross, err = gross.Subtract(feeBalance)
		}
	}
	return gross, err
}

func applySandboxTriangularBuy(
	state *sandboxTriangularAccountingState,
	base, quote domain.AssetSymbol,
	baseQuantity, quoteQuantity domain.Balance,
) error {
	var acquisitionCost domain.Money
	var err error
	if quote == "USDT" {
		acquisitionCost, err = domain.ParseMoney(quoteQuantity.String())
	} else {
		var remaining sandboxTriangularAccountingLot
		remaining, acquisitionCost, err = removeSandboxTriangularLot(state.Lots[quote], quoteQuantity)
		if err == nil {
			state.Lots[quote] = remaining
		}
	}
	if err != nil {
		markSandboxTriangularValuation(state, sandboxAccountingValuationUnresolved)
		return nil
	}
	state.Lots[base], err = addSandboxTriangularLot(state.Lots[base], baseQuantity, acquisitionCost)
	if err != nil {
		markSandboxTriangularValuation(state, sandboxAccountingValuationUnresolved)
	}
	return nil
}

func applySandboxTriangularSell(
	state *sandboxTriangularAccountingState,
	base, quote domain.AssetSymbol,
	baseQuantity, quoteQuantity domain.Balance,
) error {
	remaining, removedCost, err := removeSandboxTriangularLot(state.Lots[base], baseQuantity)
	if err != nil {
		markSandboxTriangularValuation(state, sandboxAccountingValuationUnresolved)
		return nil
	}
	state.Lots[base] = remaining
	if quote == "USDT" {
		proceeds, parseErr := domain.ParseMoney(quoteQuantity.String())
		zeroFee, _ := domain.ParseFee("0")
		pnl, pnlErr := domain.MoneyDifference(proceeds, removedCost, zeroFee)
		if parseErr != nil || pnlErr != nil {
			markSandboxTriangularValuation(state, sandboxAccountingValuationUnresolved)
			return nil
		}
		state.RealizedPnL, err = state.RealizedPnL.Add(pnl)
	} else {
		state.Lots[quote], err = addSandboxTriangularLot(state.Lots[quote], quoteQuantity, removedCost)
	}
	if err != nil {
		markSandboxTriangularValuation(state, sandboxAccountingValuationUnresolved)
	}
	return nil
}

func addSandboxTriangularLot(
	lot sandboxTriangularAccountingLot,
	quantity domain.Balance,
	cost domain.Money,
) (sandboxTriangularAccountingLot, error) {
	lot = initializedSandboxTriangularLot(lot)
	zeroBalance, _ := domain.ParseBalance("0")
	zeroMoney, _ := domain.ParseMoney("0")
	if quantity.Compare(zeroBalance) <= 0 || cost.Compare(zeroMoney) <= 0 {
		return sandboxTriangularAccountingLot{}, fmt.Errorf("sandbox_accounting_projection_invalid")
	}
	totalQuantity, err := lot.Quantity.Add(quantity)
	if err != nil {
		return sandboxTriangularAccountingLot{}, err
	}
	totalCost, err := lot.TotalCost.Add(cost)
	if err != nil {
		return sandboxTriangularAccountingLot{}, err
	}
	average, err := domain.CalculateAveragePrice(totalCost, totalQuantity, 18)
	if err != nil {
		return sandboxTriangularAccountingLot{}, err
	}
	return sandboxTriangularAccountingLot{Quantity: totalQuantity, TotalCost: totalCost,
		AverageCost: average}, nil
}

func removeSandboxTriangularLot(
	lot sandboxTriangularAccountingLot,
	quantity domain.Balance,
) (sandboxTriangularAccountingLot, domain.Money, error) {
	lot = initializedSandboxTriangularLot(lot)
	zeroBalance, _ := domain.ParseBalance("0")
	if quantity.Compare(zeroBalance) <= 0 || quantity.Compare(lot.Quantity) > 0 {
		return sandboxTriangularAccountingLot{}, domain.Money{},
			fmt.Errorf("sandbox_accounting_projection_oversell")
	}
	remainingQuantity, err := lot.Quantity.Subtract(quantity)
	if err != nil {
		return sandboxTriangularAccountingLot{}, domain.Money{}, err
	}
	removedCost, err := domain.CalculateMoney(lot.AverageCost, quantity, 18)
	if err != nil {
		return sandboxTriangularAccountingLot{}, domain.Money{}, err
	}
	if quantity.Compare(lot.Quantity) == 0 {
		removedCost = lot.TotalCost
	}
	remainingCost, err := lot.TotalCost.Subtract(removedCost)
	if err != nil {
		return sandboxTriangularAccountingLot{}, domain.Money{}, err
	}
	if remainingQuantity.Compare(zeroBalance) == 0 {
		return initializedSandboxTriangularLot(sandboxTriangularAccountingLot{}), removedCost, nil
	}
	return sandboxTriangularAccountingLot{Quantity: remainingQuantity,
		TotalCost: remainingCost, AverageCost: lot.AverageCost}, removedCost, nil
}

func initializedSandboxTriangularLot(
	lot sandboxTriangularAccountingLot,
) sandboxTriangularAccountingLot {
	if lot.Quantity.String() != "" {
		return lot
	}
	zeroBalance, _ := domain.ParseBalance("0")
	zeroMoney, _ := domain.ParseMoney("0")
	zeroPrice, _ := domain.ParsePrice("0")
	return sandboxTriangularAccountingLot{Quantity: zeroBalance, TotalCost: zeroMoney,
		AverageCost: zeroPrice}
}

func markSandboxTriangularValuation(
	state *sandboxTriangularAccountingState,
	want string,
) {
	if state.ValuationState == sandboxAccountingValuationUnresolved {
		return
	}
	if want == sandboxAccountingValuationUnresolved ||
		(state.ValuationState == sandboxAccountingValuationComplete &&
			want == sandboxAccountingValuationUnvaluedFee) {
		state.ValuationState = want
	}
}

func finalizeSandboxTriangularAccountingState(state *sandboxTriangularAccountingState) {
	if state.ValuationState != sandboxAccountingValuationComplete {
		return
	}
	zero, _ := domain.ParseBalance("0")
	for asset, lot := range state.Lots {
		lot = initializedSandboxTriangularLot(lot)
		if asset != state.PrimaryAsset && lot.Quantity.Compare(zero) > 0 {
			state.ValuationState = sandboxAccountingValuationCrossAsset
			return
		}
	}
}

func appendSandboxTriangularProjectionHash(
	hashParts *[]string,
	state sandboxTriangularAccountingState,
	projection sandboxAccountingPositionProjection,
) {
	assets := make([]string, 0, len(state.Lots))
	for asset := range state.Lots {
		assets = append(assets, string(asset))
	}
	sort.Strings(assets)
	*hashParts = append(*hashParts, projection.Quantity.String(), projection.TotalCost.String(),
		projection.WeightedAverageCost.String(), projection.RealizedPnL.String(),
		projection.ValuationState, strconv.FormatUint(projection.SourceTransactionCount, 10),
		projection.LastTransactionID, projection.LastOccurredAt.Format(time.RFC3339Nano),
		projection.UpdatedAt.Format(time.RFC3339Nano))
	for _, asset := range assets {
		lot := initializedSandboxTriangularLot(state.Lots[domain.AssetSymbol(asset)])
		*hashParts = append(*hashParts, asset, lot.Quantity.String(), lot.TotalCost.String(),
			lot.AverageCost.String())
	}
	feeAssets := make([]string, 0, len(state.Fees))
	for asset := range state.Fees {
		feeAssets = append(feeAssets, string(asset))
	}
	sort.Strings(feeAssets)
	for _, asset := range feeAssets {
		total := state.Fees[domain.AssetSymbol(asset)]
		*hashParts = append(*hashParts, asset, total.Fee.String(), total.Rebate.String())
	}
}
