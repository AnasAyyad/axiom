package portfolio

import (
	"axiom/internal/accounting"
	"axiom/internal/domain"
	"axiom/internal/execution"
)

// ApplyFill settles one fill and updates exact spot position cost/PnL.
func (portfolio *Portfolio) ApplyFill(
	allocation Allocation,
	fill execution.FillFact,
	final bool,
) (accounting.Reservation, error) {
	portfolio.mutex.Lock()
	defer portfolio.mutex.Unlock()
	candidate := allocation.Candidate
	if fill.ID.Value() == "" || fill.Quantity.String() == "0" || fill.Price.String() == "0" ||
		fill.Quantity.Compare(candidate.Quantity) > 0 {
		return accounting.Reservation{}, portfolioError("portfolio_fill_invalid")
	}
	debitQuantity, creditAsset, creditQuantity, err := settlementAmounts(candidate, fill)
	if err != nil {
		return accounting.Reservation{}, err
	}
	position := portfolio.positions[candidate.Instrument]
	if position.Revision == 0 {
		position = emptyPosition(candidate.Instrument)
	}
	if candidate.Side == domain.SideBuy {
		position, err = applyBuy(position, fill)
	} else {
		position, err = applySell(position, fill)
	}
	if err != nil {
		return accounting.Reservation{}, err
	}
	creditKey, exists := portfolio.balances[creditAsset]
	if !exists {
		return accounting.Reservation{}, portfolioError("portfolio_settlement_failed")
	}
	reservation, err := portfolio.ledger.SettleFill(allocation.Funds.ID, allocation.Funds.Revision,
		allocation.Funds.Fence, debitQuantity, creditKey, creditQuantity, final)
	if err != nil {
		return accounting.Reservation{}, portfolioError("portfolio_settlement_failed")
	}
	portfolio.positions[candidate.Instrument] = position
	portfolio.revision++
	return reservation, nil
}

// Mark updates exact unrealized P&L without changing cost or ownership.
func (portfolio *Portfolio) Mark(instrument domain.Instrument, price domain.Price) error {
	portfolio.mutex.Lock()
	defer portfolio.mutex.Unlock()
	position, exists := portfolio.positions[instrument]
	if !exists || price.String() == "0" {
		return portfolioError("position_mark_invalid")
	}
	value, err := domain.CalculateMoney(price, position.Quantity, 18)
	zeroFee, _ := domain.ParseFee("0")
	if err != nil {
		return portfolioError("position_mark_invalid")
	}
	position.UnrealizedPnL, err = domain.MoneyDifference(value, position.Cost, zeroFee)
	if err != nil {
		return portfolioError("position_mark_invalid")
	}
	position.Revision++
	portfolio.positions[instrument], portfolio.revision = position, portfolio.revision+1
	return nil
}

func settlementAmounts(
	candidate Candidate,
	fill execution.FillFact,
) (domain.Balance, domain.AssetSymbol, domain.Balance, error) {
	if fill.FeeAsset != candidate.Instrument.Quote {
		return domain.Balance{}, "", domain.Balance{}, portfolioError("fee_asset_reservation_missing")
	}
	notional, err := domain.CalculateNotional(fill.Price, fill.Quantity, 18)
	money, parseErr := domain.ParseMoney(notional.String())
	if err != nil || parseErr != nil {
		return domain.Balance{}, "", domain.Balance{}, portfolioError("fill_notional_invalid")
	}
	fee, _ := domain.ParseMoney(fill.Fee.String())
	rebate, _ := domain.ParseMoney(fill.Rebate.String())
	if candidate.Side == domain.SideBuy {
		debit, debitErr := money.Add(fee)
		if debitErr == nil {
			debit, debitErr = debit.Subtract(rebate)
		}
		debitBalance, balanceErr := domain.ParseBalance(debit.String())
		quantity, quantityErr := domain.ParseBalance(fill.Quantity.String())
		if debitErr != nil || balanceErr != nil || quantityErr != nil {
			return domain.Balance{}, "", domain.Balance{}, portfolioError("fill_settlement_invalid")
		}
		return debitBalance, candidate.Instrument.Base, quantity, nil
	}
	proceeds, proceedsErr := money.Subtract(fee)
	if proceedsErr == nil {
		proceeds, proceedsErr = proceeds.Add(rebate)
	}
	debit, debitErr := domain.ParseBalance(fill.Quantity.String())
	credit, creditErr := domain.ParseBalance(proceeds.String())
	if proceedsErr != nil || debitErr != nil || creditErr != nil {
		return domain.Balance{}, "", domain.Balance{}, portfolioError("fill_settlement_invalid")
	}
	return debit, candidate.Instrument.Quote, credit, nil
}

func emptyPosition(instrument domain.Instrument) Position {
	quantity, _ := domain.ParseBalance("0")
	cost, _ := domain.ParseMoney("0")
	average, _ := domain.ParsePrice("0")
	pnl, _ := domain.ParsePnL("0")
	return Position{Instrument: instrument, Quantity: quantity, Cost: cost,
		WeightedAverageCost: average, RealizedPnL: pnl, UnrealizedPnL: pnl, Revision: 1}
}

func applyBuy(position Position, fill execution.FillFact) (Position, error) {
	quantity, _ := domain.ParseBalance(fill.Quantity.String())
	notional, err := domain.CalculateNotional(fill.Price, fill.Quantity, 18)
	purchase, parseErr := domain.ParseMoney(notional.String())
	if err != nil || parseErr != nil {
		return Position{}, portfolioError("position_buy_invalid")
	}
	purchase, err = purchase.AddFee(fill.Fee)
	rebate, _ := domain.ParseMoney(fill.Rebate.String())
	if err == nil {
		purchase, err = purchase.Subtract(rebate)
	}
	if err == nil {
		position.Quantity, err = position.Quantity.Add(quantity)
	}
	if err == nil {
		position.Cost, err = position.Cost.Add(purchase)
	}
	if err == nil {
		position.WeightedAverageCost, err = domain.CalculateAveragePrice(position.Cost, position.Quantity, 18)
	}
	if err != nil {
		return Position{}, portfolioError("position_buy_invalid")
	}
	position.Revision++
	return position, nil
}

func applySell(position Position, fill execution.FillFact) (Position, error) {
	sold, _ := domain.ParseBalance(fill.Quantity.String())
	if position.Quantity.Compare(sold) < 0 {
		return Position{}, portfolioError("unowned_sell_rejected")
	}
	cost, err := domain.CalculateMoney(position.WeightedAverageCost, sold, 18)
	notional, notionalErr := domain.CalculateNotional(fill.Price, fill.Quantity, 18)
	proceeds, proceedsErr := domain.ParseMoney(notional.String())
	if err != nil || notionalErr != nil || proceedsErr != nil {
		return Position{}, portfolioError("position_sell_invalid")
	}
	rebate, _ := domain.ParseMoney(fill.Rebate.String())
	if proceedsErr == nil {
		proceeds, proceedsErr = proceeds.Add(rebate)
	}
	if proceedsErr != nil {
		return Position{}, portfolioError("position_sell_invalid")
	}
	pnl, err := domain.MoneyDifference(proceeds, cost, fill.Fee)
	if err == nil {
		position.RealizedPnL, err = position.RealizedPnL.Add(pnl)
	}
	if err == nil {
		position.Quantity, err = position.Quantity.Subtract(sold)
	}
	if err == nil {
		position.Cost, err = position.Cost.Subtract(cost)
	}
	if err != nil {
		return Position{}, portfolioError("position_sell_invalid")
	}
	if position.Quantity.String() == "0" {
		position.WeightedAverageCost, _ = domain.ParsePrice("0")
	}
	position.Revision++
	return position, nil
}
