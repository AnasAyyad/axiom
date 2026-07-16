package domain

import "github.com/cockroachdb/apd/v3"

// Side identifies the direction of a virtual spot trade.
type Side string

// Supported spot trade sides.
const (
	SideBuy  Side = "buy"
	SideSell Side = "sell"
)

// RoundBuyQuantity rounds a requested buy down to the exchange step size.
func RoundBuyQuantity(requested, step Quantity) (Quantity, error) {
	result, err := floorMultiple("buy_quantity_round", requested.decimalValue, step.decimalValue)
	return Quantity{result}, err
}

// RoundSellQuantity caps at owned inventory and rounds down to the step size.
func RoundSellQuantity(requested Quantity, owned Balance, step Quantity) (Quantity, error) {
	capped := requested.decimalValue
	ownedQuantity := Quantity(owned)
	if requested.Compare(ownedQuantity) > 0 {
		capped = owned.decimalValue
	}
	result, err := floorMultiple("sell_quantity_round", capped, step.decimalValue)
	return Quantity{result}, err
}

// RoundLimitPrice rounds buys down and sells up so limits never become riskier.
func RoundLimitPrice(side Side, requested, tick Price) (Price, error) {
	floor, err := floorMultiple("limit_price_round", requested.decimalValue, tick.decimalValue)
	if err != nil || side == SideBuy {
		return Price{floor}, err
	}
	if side != SideSell {
		return Price{}, domainError(CodeInvalidInstrument, "side")
	}
	if floor.compare(requested.decimalValue) == 0 {
		return Price{floor}, nil
	}
	ceiling, err := addDecimal("limit_price_round", floor, tick.decimalValue)
	return Price{ceiling}, err
}

// RoundMarketableLimitPrice rounds a buy upward and a sell downward so the
// requested marketable protection remains executable without crossing beyond
// the configured pre-rounding slippage boundary.
func RoundMarketableLimitPrice(side Side, requested, tick Price) (Price, error) {
	floor, err := floorMultiple("marketable_limit_price_round", requested.decimalValue, tick.decimalValue)
	if err != nil || side == SideSell {
		return Price{floor}, err
	}
	if side != SideBuy {
		return Price{}, domainError(CodeInvalidInstrument, "side")
	}
	if floor.compare(requested.decimalValue) == 0 {
		return Price{floor}, nil
	}
	ceiling, err := addDecimal("marketable_limit_price_round", floor, tick.decimalValue)
	return Price{ceiling}, err
}

// CalculateNotional multiplies price and quantity and rounds half-even to scale.
func CalculateNotional(price Price, quantity Quantity, scale uint8) (Notional, error) {
	product, err := multiplyDecimal("notional_multiply", price.decimalValue, quantity.decimalValue)
	if err != nil {
		return Notional{}, err
	}
	result, err := quantizeDecimal("notional_quantize", product, scale, apd.RoundHalfEven)
	return Notional{result}, err
}

// CalculateFee multiplies notional and rate and rounds toward positive infinity.
func CalculateFee(notional Notional, rate Rate, scale uint8) (Fee, error) {
	product, err := multiplyDecimal("fee_multiply", notional.decimalValue, rate.decimalValue)
	if err != nil {
		return Fee{}, err
	}
	result, err := quantizeDecimal("fee_quantize", product, scale, apd.RoundCeiling)
	return Fee{result}, err
}

// CalculateMoney multiplies a price by an owned balance and rounds half-even.
func CalculateMoney(price Price, quantity Balance, scale uint8) (Money, error) {
	product, err := multiplyDecimal("money_multiply", price.decimalValue, quantity.decimalValue)
	if err != nil {
		return Money{}, err
	}
	result, err := quantizeDecimal("money_quantize", product, scale, apd.RoundHalfEven)
	return Money{result}, err
}

// CalculateAveragePrice divides total cost by owned quantity and rounds half-even.
func CalculateAveragePrice(cost Money, quantity Balance, scale uint8) (Price, error) {
	if quantity.decimal.Sign() <= 0 {
		return Price{}, domainError(CodeArithmetic, "average_price_zero_quantity")
	}
	context := exactContext
	context.Traps = apd.DefaultTraps
	context.Rounding = apd.RoundHalfEven
	var quotient apd.Decimal
	if _, err := context.Quo(&quotient, &cost.decimal, &quantity.decimal); err != nil {
		return Price{}, domainError(CodeArithmetic, "average_price_divide")
	}
	result, err := quantizeDecimal("average_price_quantize", reducedValue(&quotient), scale, apd.RoundHalfEven)
	return Price{result}, err
}

// CalculatePercent divides exact money values and rounds half-even at scale.
func CalculatePercent(numerator, denominator Money, scale uint8) (Percent, error) {
	if denominator.decimal.Sign() <= 0 {
		return Percent{}, domainError(CodeArithmetic, "percent_zero_denominator")
	}
	context := exactContext
	context.Traps = apd.DefaultTraps
	context.Rounding = apd.RoundHalfEven
	var quotient apd.Decimal
	if _, err := context.Quo(&quotient, &numerator.decimal, &denominator.decimal); err != nil {
		return Percent{}, domainError(CodeArithmetic, "percent_divide")
	}
	result, err := quantizeDecimal("percent_quantize", reducedValue(&quotient), scale, apd.RoundHalfEven)
	return Percent{result}, err
}

// CalculateVWAP divides exact total notional by filled base quantity and
// rounds half-even at the explicitly selected output scale.
func CalculateVWAP(notional Notional, quantity Quantity, scale uint8) (Price, error) {
	if quantity.decimal.Sign() <= 0 {
		return Price{}, domainError(CodeArithmetic, "vwap_zero_quantity")
	}
	context := exactContext
	context.Traps = apd.DefaultTraps
	context.Rounding = apd.RoundHalfEven
	var quotient apd.Decimal
	if _, err := context.Quo(&quotient, &notional.decimal, &quantity.decimal); err != nil {
		return Price{}, domainError(CodeArithmetic, "vwap_divide")
	}
	result, err := quantizeDecimal("vwap_quantize", reducedValue(&quotient), scale, apd.RoundHalfEven)
	return Price{result}, err
}

// PriceAtSlippage returns the inclusive buy ceiling or sell floor around one
// reference price. The percentage is a decimal fraction in [0,1].
func PriceAtSlippage(reference Price, slippage Percent, side Side, scale uint8) (Price, error) {
	one, _ := parseDecimal("1", "slippage_one", false)
	if slippage.decimal.Sign() < 0 || slippage.decimalValue.compare(one) > 0 {
		return Price{}, domainError(CodeArithmetic, "slippage_range")
	}
	var multiplier decimalValue
	var err error
	switch side {
	case SideBuy:
		multiplier, err = addDecimal("buy_slippage", one, slippage.decimalValue)
	case SideSell:
		multiplier, err = subtractDecimal("sell_slippage", one, slippage.decimalValue, false)
	default:
		return Price{}, domainError(CodeInvalidInstrument, "slippage_side")
	}
	if err != nil {
		return Price{}, err
	}
	adjusted, err := multiplyDecimal("slippage_price", reference.decimalValue, multiplier)
	if err != nil {
		return Price{}, err
	}
	result, err := quantizeDecimal("slippage_price", adjusted, scale, apd.RoundHalfEven)
	return Price{result}, err
}

// ScaleQuantity multiplies quantity by a decimal fraction and rounds down so a
// modeled partial fill can never exceed the requested amount.
func ScaleQuantity(quantity Quantity, fraction Percent, scale uint8) (Quantity, error) {
	one, _ := parseDecimal("1", "quantity_fraction_one", false)
	if fraction.decimal.Sign() < 0 || fraction.decimalValue.compare(one) > 0 {
		return Quantity{}, domainError(CodeArithmetic, "quantity_fraction_range")
	}
	product, err := multiplyDecimal("quantity_fraction", quantity.decimalValue, fraction.decimalValue)
	if err != nil {
		return Quantity{}, err
	}
	result, err := quantizeDecimal("quantity_fraction", product, scale, apd.RoundFloor)
	return Quantity{result}, err
}

func floorMultiple(operation string, value, increment decimalValue) (decimalValue, error) {
	if increment.decimal.Sign() <= 0 {
		return decimalValue{}, domainError(CodeInvalidScale, operation)
	}
	var quotient apd.Decimal
	if _, err := exactContext.QuoInteger(&quotient, &value.decimal, &increment.decimal); err != nil {
		return decimalValue{}, domainError(CodeArithmetic, operation)
	}
	return exactBinary(operation, reducedValue(&quotient), increment, (*apd.Context).Mul)
}
