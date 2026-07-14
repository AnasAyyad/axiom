package accounting

import "axiom/internal/domain"

// CostBasis is exact weighted-average owned inventory state.
type CostBasis struct {
	Quantity     domain.Balance
	TotalCost    domain.Money
	AveragePrice domain.Price
}

// NewCostBasis constructs exact zero inventory.
func NewCostBasis() CostBasis {
	quantity, _ := domain.ParseBalance("0")
	cost, _ := domain.ParseMoney("0")
	price, _ := domain.ParsePrice("0")
	return CostBasis{Quantity: quantity, TotalCost: cost, AveragePrice: price}
}

// Buy adds quote-denominated acquisition cost and recomputes weighted average.
func (basis CostBasis) Buy(quantity domain.Balance, price domain.Price, quoteFee domain.Fee, scale uint8) (CostBasis, error) {
	if !positive(quantity) {
		return CostBasis{}, accountingError("cost_basis_quantity_invalid")
	}
	cost, err := domain.CalculateMoney(price, quantity, scale)
	if err != nil {
		return CostBasis{}, err
	}
	cost, err = cost.AddFee(quoteFee)
	if err != nil {
		return CostBasis{}, err
	}
	quantityTotal, err := basis.Quantity.Add(quantity)
	if err != nil {
		return CostBasis{}, err
	}
	costTotal, err := basis.TotalCost.Add(cost)
	if err != nil {
		return CostBasis{}, err
	}
	average, err := domain.CalculateAveragePrice(costTotal, quantityTotal, scale)
	if err != nil {
		return CostBasis{}, err
	}
	return CostBasis{Quantity: quantityTotal, TotalCost: costTotal, AveragePrice: average}, nil
}

// Sell removes cost at the prior weighted average and returns separated P&L.
func (basis CostBasis) Sell(quantity domain.Balance, proceeds domain.Money, quoteFee domain.Fee, scale uint8) (CostBasis, domain.PnL, error) {
	remaining, err := basis.Quantity.Subtract(quantity)
	if err != nil || !positive(quantity) {
		return CostBasis{}, domain.PnL{}, accountingError("cost_basis_oversell")
	}
	removedCost, err := domain.CalculateMoney(basis.AveragePrice, quantity, scale)
	if err != nil {
		return CostBasis{}, domain.PnL{}, err
	}
	if quantity.Compare(basis.Quantity) == 0 {
		removedCost = basis.TotalCost
	}
	pnl, err := domain.MoneyDifference(proceeds, removedCost, quoteFee)
	if err != nil {
		return CostBasis{}, domain.PnL{}, err
	}
	remainingCost, err := basis.TotalCost.Subtract(removedCost)
	if err != nil {
		return CostBasis{}, domain.PnL{}, accountingError("cost_basis_projection_invalid")
	}
	zero, _ := domain.ParseBalance("0")
	if remaining.Compare(zero) == 0 {
		return NewCostBasis(), pnl, nil
	}
	// A partial sale must not re-average the remaining lot. Any quantization
	// residual stays in TotalCost and is removed exactly by the final sale.
	return CostBasis{Quantity: remaining, TotalCost: remainingCost, AveragePrice: basis.AveragePrice}, pnl, nil
}
