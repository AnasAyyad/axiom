package marketdata

import (
	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
)

// BestBid returns the highest executable bid.
func (view BookView) BestBid() (exchangecontracts.PriceLevel, error) {
	if len(view.record.Bids) == 0 {
		return exchangecontracts.PriceLevel{}, marketError("book_empty")
	}
	return view.record.Bids[0], nil
}

// BestAsk returns the lowest executable ask.
func (view BookView) BestAsk() (exchangecontracts.PriceLevel, error) {
	if len(view.record.Asks) == 0 {
		return exchangecontracts.PriceLevel{}, marketError("book_empty")
	}
	return view.record.Asks[0], nil
}

// VWAPToBuyBase calculates the exact depth-consumed buy result.
func (view BookView) VWAPToBuyBase(quantity domain.Quantity, scale uint8) (domain.Price, domain.Notional, error) {
	return consume(view.record.Asks, quantity, scale)
}

// VWAPToSellBase calculates the exact depth-consumed sell result.
func (view BookView) VWAPToSellBase(quantity domain.Quantity, scale uint8) (domain.Price, domain.Notional, error) {
	return consume(view.record.Bids, quantity, scale)
}

// MaxBaseWithinSlippage returns available base quantity inside the inclusive
// best-price-relative slippage boundary.
func (view BookView) MaxBaseWithinSlippage(side domain.Side, slippage domain.Percent, scale uint8) (domain.Quantity, error) {
	var levels []exchangecontracts.PriceLevel
	var reference domain.Price
	switch side {
	case domain.SideBuy:
		levels = view.record.Asks
	case domain.SideSell:
		levels = view.record.Bids
	default:
		return domain.Quantity{}, marketError("side_invalid")
	}
	if len(levels) == 0 {
		return domain.Quantity{}, marketError("book_empty")
	}
	reference = levels[0].Price
	limit, err := domain.PriceAtSlippage(reference, slippage, side, scale)
	if err != nil {
		return domain.Quantity{}, err
	}
	return view.DepthAtPrice(side, limit)
}

// DepthAtPrice sums base quantity at or better than an inclusive price.
func (view BookView) DepthAtPrice(side domain.Side, limit domain.Price) (domain.Quantity, error) {
	zero, _ := domain.ParseQuantity("0")
	total := zero
	var levels []exchangecontracts.PriceLevel
	switch side {
	case domain.SideBuy:
		levels = view.record.Asks
	case domain.SideSell:
		levels = view.record.Bids
	default:
		return domain.Quantity{}, marketError("side_invalid")
	}
	for _, level := range levels {
		comparison := level.Price.Compare(limit)
		if (side == domain.SideBuy && comparison > 0) || (side == domain.SideSell && comparison < 0) {
			break
		}
		var err error
		total, err = total.Add(level.Quantity)
		if err != nil {
			return domain.Quantity{}, err
		}
	}
	return total, nil
}

func consume(levels []exchangecontracts.PriceLevel, requested domain.Quantity, scale uint8) (domain.Price, domain.Notional, error) {
	zeroQuantity, _ := domain.ParseQuantity("0")
	zeroNotional, _ := domain.ParseNotional("0")
	if requested.Compare(zeroQuantity) <= 0 {
		return domain.Price{}, domain.Notional{}, marketError("quantity_invalid")
	}
	remaining, filled, total := requested, zeroQuantity, zeroNotional
	for _, level := range levels {
		take := level.Quantity
		if take.Compare(remaining) > 0 {
			take = remaining
		}
		notional, err := domain.CalculateNotional(level.Price, take, 18)
		if err != nil {
			return domain.Price{}, domain.Notional{}, err
		}
		total, err = total.Add(notional)
		if err != nil {
			return domain.Price{}, domain.Notional{}, err
		}
		filled, err = filled.Add(take)
		if err != nil {
			return domain.Price{}, domain.Notional{}, err
		}
		remaining, err = remaining.Subtract(take)
		if err != nil {
			return domain.Price{}, domain.Notional{}, err
		}
		if remaining.Compare(zeroQuantity) == 0 {
			vwap, averageErr := domain.CalculateVWAP(total, filled, scale)
			return vwap, total, averageErr
		}
	}
	return domain.Price{}, domain.Notional{}, marketError("depth_insufficient")
}
