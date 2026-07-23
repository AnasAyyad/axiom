package arbitrage

import "github.com/cockroachdb/apd/v3"

const exactScale = int32(18)

var exactContext = apd.Context{
	Precision: 38, MaxExponent: 96, MinExponent: -96,
	Traps: apd.DefaultTraps, Rounding: apd.RoundHalfEven,
}

type number struct{ value apd.Decimal }

func parseNumber(text string) (number, error) {
	parsed, _, err := apd.NewFromString(text)
	if err != nil || parsed.Form != apd.Finite {
		return number{}, conversionError("decimal_invalid")
	}
	return quantize(number{value: *parsed}, apd.RoundHalfEven)
}

func (value number) text() string {
	var reduced apd.Decimal
	reduced.Reduce(&value.value)
	return reduced.Text('f')
}

func (value number) sign() int                { return value.value.Sign() }
func (value number) compare(other number) int { return value.value.Cmp(&other.value) }

func (value number) add(other number, rounding apd.Rounder) (number, error) {
	context := exactContext
	context.Rounding = rounding
	var result apd.Decimal
	if _, err := context.Add(&result, &value.value, &other.value); err != nil {
		return number{}, conversionError("decimal_add")
	}
	return quantize(number{value: result}, rounding)
}

func (value number) subtract(other number, allowNegative bool, rounding apd.Rounder) (number, error) {
	context := exactContext
	context.Rounding = rounding
	var result apd.Decimal
	if _, err := context.Sub(&result, &value.value, &other.value); err != nil ||
		(!allowNegative && result.Sign() < 0) {
		return number{}, conversionError("decimal_subtract")
	}
	return quantize(number{value: result}, rounding)
}

func (value number) multiply(other number, rounding apd.Rounder) (number, error) {
	context := exactContext
	context.Rounding = rounding
	var result apd.Decimal
	if _, err := context.Mul(&result, &value.value, &other.value); err != nil {
		return number{}, conversionError("decimal_multiply")
	}
	return quantize(number{value: result}, rounding)
}

func (value number) divide(other number, rounding apd.Rounder) (number, error) {
	if other.sign() <= 0 {
		return number{}, conversionError("decimal_divide")
	}
	context := exactContext
	context.Rounding = rounding
	var result apd.Decimal
	if _, err := context.Quo(&result, &value.value, &other.value); err != nil {
		return number{}, conversionError("decimal_divide")
	}
	return quantize(number{value: result}, rounding)
}

func (value number) floorMultiple(increment number) (number, error) {
	if increment.sign() <= 0 {
		return number{}, conversionError("increment_invalid")
	}
	var quotient apd.Decimal
	if _, err := exactContext.QuoInteger(&quotient, &value.value, &increment.value); err != nil {
		return number{}, conversionError("increment_floor")
	}
	result, err := number{value: quotient}.multiply(increment, apd.RoundFloor)
	if err != nil {
		return number{}, conversionError("increment_floor")
	}
	return result, nil
}

func (value number) multipleOf(increment number) bool {
	if increment.sign() <= 0 {
		return false
	}
	var quotient apd.Decimal
	if _, err := exactContext.QuoInteger(&quotient, &value.value, &increment.value); err != nil {
		return false
	}
	var rebuilt apd.Decimal
	if _, err := exactContext.Mul(&rebuilt, &quotient, &increment.value); err != nil {
		return false
	}
	return rebuilt.Cmp(&value.value) == 0
}

func quantize(value number, rounding apd.Rounder) (number, error) {
	context := exactContext
	context.Rounding = rounding
	var result apd.Decimal
	if _, err := context.Quantize(&result, &value.value, -exactScale); err != nil {
		return number{}, conversionError("decimal_quantize")
	}
	return number{value: result}, nil
}
