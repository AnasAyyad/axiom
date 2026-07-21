package trend

import (
	"github.com/cockroachdb/apd/v3"
)

const calculationScale = int32(18)

var calculationContext = apd.Context{
	Precision: 38, MaxExponent: 96, MinExponent: -96,
	Traps: apd.DefaultTraps, Rounding: apd.RoundHalfEven,
}

type decimal struct{ value apd.Decimal }

func parseDecimal(value string) (decimal, error) {
	parsed, _, err := apd.NewFromString(value)
	if err != nil || parsed.Form != apd.Finite {
		return decimal{}, trendError(ReasonInvalidSizing)
	}
	return quantize(decimal{value: *parsed}, apd.RoundHalfEven)
}

func (value decimal) stringValue() string {
	var reduced apd.Decimal
	reduced.Reduce(&value.value)
	return reduced.Text('f')
}

func (value decimal) compare(other decimal) int { return value.value.Cmp(&other.value) }

func (value decimal) add(other decimal) (decimal, error) {
	var result apd.Decimal
	if _, err := calculationContext.Add(&result, &value.value, &other.value); err != nil {
		return decimal{}, trendError(ReasonInvalidSizing)
	}
	return quantize(decimal{value: result}, apd.RoundHalfEven)
}

func (value decimal) subtract(other decimal) (decimal, error) {
	var result apd.Decimal
	if _, err := calculationContext.Sub(&result, &value.value, &other.value); err != nil {
		return decimal{}, trendError(ReasonInvalidSizing)
	}
	return quantize(decimal{value: result}, apd.RoundHalfEven)
}

func (value decimal) multiply(other decimal, rounding apd.Rounder) (decimal, error) {
	var result apd.Decimal
	context := calculationContext
	context.Rounding = rounding
	if _, err := context.Mul(&result, &value.value, &other.value); err != nil {
		return decimal{}, trendError(ReasonInvalidSizing)
	}
	return quantize(decimal{value: result}, rounding)
}

func (value decimal) divide(other decimal, rounding apd.Rounder) (decimal, error) {
	if other.value.Sign() <= 0 {
		return decimal{}, trendError(ReasonInvalidSizing)
	}
	var result apd.Decimal
	context := calculationContext
	context.Rounding = rounding
	if _, err := context.Quo(&result, &value.value, &other.value); err != nil {
		return decimal{}, trendError(ReasonInvalidSizing)
	}
	return quantize(decimal{value: result}, rounding)
}

func quantize(value decimal, rounding apd.Rounder) (decimal, error) {
	context := calculationContext
	context.Rounding = rounding
	var result apd.Decimal
	if _, err := context.Quantize(&result, &value.value, -calculationScale); err != nil {
		return decimal{}, trendError(ReasonInvalidSizing)
	}
	return decimal{value: result}, nil
}

func maximum(left, right decimal) decimal {
	if left.compare(right) >= 0 {
		return left
	}
	return right
}

func minimum(left, right decimal) decimal {
	if left.compare(right) <= 0 {
		return left
	}
	return right
}
