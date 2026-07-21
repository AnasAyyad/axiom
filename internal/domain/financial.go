package domain

// Price is a non-negative quote-currency amount per base unit.
type Price struct{ decimalValue }

// Quantity is a non-negative base-asset amount.
type Quantity struct{ decimalValue }

// Money is a non-negative currency amount.
type Money struct{ decimalValue }

// Rate is a non-negative decimal ratio.
type Rate struct{ decimalValue }

// Percent is a non-negative percentage represented as a decimal fraction.
type Percent struct{ decimalValue }

// Fee is a non-negative currency fee amount.
type Fee struct{ decimalValue }

// Notional is a non-negative quote-currency trade value.
type Notional struct{ decimalValue }

// Balance is a non-negative owned asset amount.
type Balance struct{ decimalValue }

// PnL is a signed profit-and-loss currency amount.
type PnL struct{ decimalValue }

// ParsePrice parses a fixed-point price.
func ParsePrice(text string) (Price, error) {
	value, err := parseDecimal(text, "price", false)
	return Price{value}, err
}

// ParseQuantity parses a fixed-point quantity.
func ParseQuantity(text string) (Quantity, error) {
	value, err := parseDecimal(text, "quantity", false)
	return Quantity{value}, err
}

// ParseMoney parses a fixed-point money amount.
func ParseMoney(text string) (Money, error) {
	value, err := parseDecimal(text, "money", false)
	return Money{value}, err
}

// ParseRate parses a fixed-point rate.
func ParseRate(text string) (Rate, error) {
	value, err := parseDecimal(text, "rate", false)
	return Rate{value}, err
}

// ParsePercent parses a decimal fraction percentage.
func ParsePercent(text string) (Percent, error) {
	value, err := parseDecimal(text, "percent", false)
	return Percent{value}, err
}

// ParseFee parses a fixed-point fee.
func ParseFee(text string) (Fee, error) {
	value, err := parseDecimal(text, "fee", false)
	return Fee{value}, err
}

// ParseNotional parses a fixed-point notional.
func ParseNotional(text string) (Notional, error) {
	value, err := parseDecimal(text, "notional", false)
	return Notional{value}, err
}

// ParseBalance parses a fixed-point balance.
func ParseBalance(text string) (Balance, error) {
	value, err := parseDecimal(text, "balance", false)
	return Balance{value}, err
}

// ParsePnL parses a signed fixed-point profit-and-loss value.
func ParsePnL(text string) (PnL, error) {
	value, err := parseDecimal(text, "pnl", true)
	return PnL{value}, err
}

// Compare orders prices by numeric value.
func (value Price) Compare(other Price) int { return value.compare(other.decimalValue) }

// Compare orders quantities by numeric value.
func (value Quantity) Compare(other Quantity) int { return value.compare(other.decimalValue) }

// Compare orders balances by numeric value.
func (value Balance) Compare(other Balance) int { return value.compare(other.decimalValue) }

// Compare orders notionals by numeric value.
func (value Notional) Compare(other Notional) int { return value.compare(other.decimalValue) }

// Compare orders money amounts by numeric value.
func (value Money) Compare(other Money) int { return value.compare(other.decimalValue) }

// Compare orders fees by numeric value.
func (value Fee) Compare(other Fee) int { return value.compare(other.decimalValue) }

// Add returns an exact fee sum in the same commodity.
func (value Fee) Add(other Fee) (Fee, error) {
	result, err := addDecimal("fee_add", value.decimalValue, other.decimalValue)
	return Fee{result}, err
}

// Compare orders rates by numeric value.
func (value Rate) Compare(other Rate) int { return value.compare(other.decimalValue) }

// Add returns an exact money sum.
func (value Money) Add(other Money) (Money, error) {
	result, err := addDecimal("money_add", value.decimalValue, other.decimalValue)
	return Money{result}, err
}

// AddFee adds a fee denominated in the same asset.
func (value Money) AddFee(other Fee) (Money, error) {
	result, err := addDecimal("money_add_fee", value.decimalValue, other.decimalValue)
	return Money{result}, err
}

// Subtract returns an exact non-negative money difference.
func (value Money) Subtract(other Money) (Money, error) {
	result, err := subtractDecimal("money_subtract", value.decimalValue, other.decimalValue, false)
	return Money{result}, err
}

// Compare orders decimal percentages by numeric value.
func (value Percent) Compare(other Percent) int { return value.compare(other.decimalValue) }

// Add returns an exact percentage sum.
func (value Percent) Add(other Percent) (Percent, error) {
	result, err := addDecimal("percent_add", value.decimalValue, other.decimalValue)
	return Percent{result}, err
}

// Subtract returns an exact non-negative percentage difference.
func (value Percent) Subtract(other Percent) (Percent, error) {
	result, err := subtractDecimal("percent_subtract", value.decimalValue, other.decimalValue, false)
	return Percent{result}, err
}

// Add returns an exact quantity sum.
func (value Quantity) Add(other Quantity) (Quantity, error) {
	result, err := addDecimal("quantity_add", value.decimalValue, other.decimalValue)
	return Quantity{result}, err
}

// Subtract returns an exact, non-negative quantity difference.
func (value Quantity) Subtract(other Quantity) (Quantity, error) {
	result, err := subtractDecimal("quantity_subtract", value.decimalValue, other.decimalValue, false)
	return Quantity{result}, err
}

// Add returns an exact balance sum.
func (value Balance) Add(other Balance) (Balance, error) {
	result, err := addDecimal("balance_add", value.decimalValue, other.decimalValue)
	return Balance{result}, err
}

// Subtract returns an exact balance difference and rejects overselling.
func (value Balance) Subtract(other Balance) (Balance, error) {
	result, err := subtractDecimal("balance_subtract", value.decimalValue, other.decimalValue, false)
	return Balance{result}, err
}

// Add returns an exact notional sum.
func (value Notional) Add(other Notional) (Notional, error) {
	result, err := addDecimal("notional_add", value.decimalValue, other.decimalValue)
	return Notional{result}, err
}

// Add returns an exact PnL sum.
func (value PnL) Add(other PnL) (PnL, error) {
	result, err := addDecimal("pnl_add", value.decimalValue, other.decimalValue)
	return PnL{result}, err
}

// Compare orders signed profit-and-loss values.
func (value PnL) Compare(other PnL) int { return value.compare(other.decimalValue) }

// Subtract returns an exact signed PnL difference.
func (value PnL) Subtract(other PnL) (PnL, error) {
	result, err := subtractDecimal("pnl_subtract", value.decimalValue, other.decimalValue, true)
	return PnL{result}, err
}

// Divide returns an exact decimal rate or rejects an inexact result.
func (value Money) Divide(other Money) (Rate, error) {
	result, err := divideDecimal("money_divide", value.decimalValue, other.decimalValue)
	return Rate{result}, err
}

// MoneyDifference returns a signed exact result after subtracting cost and fee.
func MoneyDifference(proceeds, cost Money, fee Fee) (PnL, error) {
	result, err := subtractDecimal("money_difference", proceeds.decimalValue, cost.decimalValue, true)
	if err != nil {
		return PnL{}, err
	}
	result, err = subtractDecimal("money_difference_fee", result, fee.decimalValue, true)
	return PnL{result}, err
}
