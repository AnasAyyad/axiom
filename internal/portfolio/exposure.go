package portfolio

import "axiom/internal/domain"

// ExposureSnapshot is an exact USDT-numeraire ownership and risk projection.
type ExposureSnapshot struct {
	Equity          domain.Money
	Reserve         domain.Percent
	ReservedCapital domain.Percent
	AssetExposure   map[domain.AssetSymbol]domain.Percent
	Inventory       map[domain.AssetSymbol]domain.Balance
	Combined        domain.Percent
	Exchange        domain.Percent
}

// Exposure marks owned BTC/ETH and derives exact portfolio risk fractions.
func (portfolio *Portfolio) Exposure(marks map[domain.AssetSymbol]domain.Price) (ExposureSnapshot, error) {
	snapshot := portfolio.Snapshot()
	quote := snapshot.Balances[domain.AssetSymbol(V1ANumeraire)]
	equity, err := domain.ParseMoney(quote.Available.String())
	if err != nil {
		return ExposureSnapshot{}, portfolioError("exposure_quote_invalid")
	}
	quoteReserved, _ := domain.ParseMoney(quote.Reserved.String())
	equity, err = equity.Add(quoteReserved)
	if err != nil {
		return ExposureSnapshot{}, portfolioError("exposure_equity_invalid")
	}
	assetValues, inventory, volatileValue, reservedValue, err := markVolatile(snapshot, marks, quoteReserved)
	if err != nil {
		return ExposureSnapshot{}, err
	}
	equity, err = equity.Add(volatileValue)
	if err != nil || equity.String() == "0" {
		return ExposureSnapshot{}, portfolioError("exposure_equity_invalid")
	}
	available, _ := domain.ParseMoney(quote.Available.String())
	reserve, err := domain.CalculatePercent(available, equity, 18)
	if err != nil {
		return ExposureSnapshot{}, portfolioError("exposure_reserve_invalid")
	}
	reserved, err := domain.CalculatePercent(reservedValue, equity, 18)
	if err != nil {
		return ExposureSnapshot{}, portfolioError("exposure_reserved_invalid")
	}
	assetExposure := make(map[domain.AssetSymbol]domain.Percent, len(assetValues))
	for asset, value := range assetValues {
		assetExposure[asset], err = domain.CalculatePercent(value, equity, 18)
		if err != nil {
			return ExposureSnapshot{}, portfolioError("exposure_asset_invalid")
		}
	}
	combined, err := domain.CalculatePercent(volatileValue, equity, 18)
	if err != nil {
		return ExposureSnapshot{}, portfolioError("exposure_combined_invalid")
	}
	return ExposureSnapshot{Equity: equity, Reserve: reserve, ReservedCapital: reserved,
		AssetExposure: assetExposure, Inventory: inventory, Combined: combined, Exchange: combined}, nil
}

func markVolatile(
	snapshot Snapshot,
	marks map[domain.AssetSymbol]domain.Price,
	reservedValue domain.Money,
) (map[domain.AssetSymbol]domain.Money, map[domain.AssetSymbol]domain.Balance, domain.Money, domain.Money, error) {
	assetValues := make(map[domain.AssetSymbol]domain.Money, 2)
	inventory := make(map[domain.AssetSymbol]domain.Balance, 2)
	volatileValue, _ := domain.ParseMoney("0")
	zero, _ := domain.ParseBalance("0")
	for _, asset := range []domain.AssetSymbol{"BTC", "ETH"} {
		balance := snapshot.Balances[asset]
		owned, err := balance.Available.Add(balance.Reserved)
		if err != nil {
			return nil, nil, domain.Money{}, domain.Money{}, portfolioError("exposure_inventory_invalid")
		}
		inventory[asset] = owned
		value, _ := domain.ParseMoney("0")
		if owned.Compare(zero) > 0 {
			mark, exists := marks[asset]
			if !exists || mark.String() == "0" {
				return nil, nil, domain.Money{}, domain.Money{}, portfolioError("exposure_mark_missing")
			}
			value, err = domain.CalculateMoney(mark, owned, 18)
			if err != nil {
				return nil, nil, domain.Money{}, domain.Money{}, portfolioError("exposure_mark_invalid")
			}
			reservedValue, err = addReservedMark(reservedValue, balance.Reserved, mark)
			if err != nil {
				return nil, nil, domain.Money{}, domain.Money{}, err
			}
		}
		assetValues[asset] = value
		volatileValue, err = volatileValue.Add(value)
		if err != nil {
			return nil, nil, domain.Money{}, domain.Money{}, portfolioError("exposure_combined_invalid")
		}
	}
	return assetValues, inventory, volatileValue, reservedValue, nil
}

func addReservedMark(current domain.Money, reserved domain.Balance, mark domain.Price) (domain.Money, error) {
	zero, _ := domain.ParseBalance("0")
	if reserved.Compare(zero) == 0 {
		return current, nil
	}
	value, err := domain.CalculateMoney(mark, reserved, 18)
	if err != nil {
		return domain.Money{}, portfolioError("exposure_mark_invalid")
	}
	result, err := current.Add(value)
	if err != nil {
		return domain.Money{}, portfolioError("exposure_reserved_invalid")
	}
	return result, nil
}
