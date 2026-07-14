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
