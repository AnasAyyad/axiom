package domain

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/cockroachdb/apd/v3"
)

const (
	decimalPrecision = uint32(38)
	maximumScale     = uint8(18)
)

var canonicalDecimal = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?$`)

var exactContext = apd.Context{
	Precision:   decimalPrecision,
	MaxExponent: 96,
	MinExponent: -96,
	Traps:       apd.DefaultTraps | apd.Inexact | apd.Rounded,
	Rounding:    apd.RoundHalfEven,
}

type decimalValue struct {
	decimal apd.Decimal
}

func parseDecimal(text, operation string, signed bool) (decimalValue, error) {
	if !canonicalDecimal.MatchString(text) {
		return decimalValue{}, domainError(CodeInvalidDecimal, operation)
	}
	parsed, _, err := apd.NewFromString(text)
	if err != nil || parsed.Form != apd.Finite || parsed.NumDigits() > int64(decimalPrecision) {
		return decimalValue{}, domainError(CodeDecimalRange, operation)
	}
	if strings.HasPrefix(text, "-") && parsed.Sign() == 0 {
		return decimalValue{}, domainError(CodeInvalidDecimal, operation)
	}
	if parsed.Exponent < -int32(maximumScale) || (!signed && parsed.Sign() < 0) {
		return decimalValue{}, domainError(CodeNegativeValue, operation)
	}
	return reducedValue(parsed), nil
}

func reducedValue(value *apd.Decimal) decimalValue {
	var reduced apd.Decimal
	reduced.Reduce(value)
	return decimalValue{decimal: reduced}
}

// String returns canonical fixed-point decimal text.
func (value decimalValue) String() string {
	return value.decimal.Text('f')
}

// MarshalText emits canonical fixed-point decimal text.
func (value decimalValue) MarshalText() ([]byte, error) {
	return []byte(value.String()), nil
}

// MarshalJSON emits a quoted canonical fixed-point decimal string.
func (value decimalValue) MarshalJSON() ([]byte, error) {
	return json.Marshal(value.String())
}

func (value decimalValue) compare(other decimalValue) int {
	return value.decimal.Cmp(&other.decimal)
}

func exactBinary(operation string, left, right decimalValue, apply binaryOperation) (decimalValue, error) {
	var result apd.Decimal
	_, err := apply(&exactContext, &result, &left.decimal, &right.decimal)
	if err != nil {
		return decimalValue{}, domainError(CodeArithmetic, operation)
	}
	return reducedValue(&result), nil
}

type binaryOperation func(*apd.Context, *apd.Decimal, *apd.Decimal, *apd.Decimal) (apd.Condition, error)

func addDecimal(operation string, left, right decimalValue) (decimalValue, error) {
	return exactBinary(operation, left, right, (*apd.Context).Add)
}

func subtractDecimal(operation string, left, right decimalValue, signed bool) (decimalValue, error) {
	result, err := exactBinary(operation, left, right, (*apd.Context).Sub)
	if err == nil && !signed && result.decimal.Sign() < 0 {
		return decimalValue{}, domainError(CodeNegativeValue, operation)
	}
	return result, err
}

func multiplyDecimal(operation string, left, right decimalValue) (decimalValue, error) {
	return exactBinary(operation, left, right, (*apd.Context).Mul)
}

func divideDecimal(operation string, left, right decimalValue) (decimalValue, error) {
	return exactBinary(operation, left, right, (*apd.Context).Quo)
}

func quantizeDecimal(operation string, value decimalValue, scale uint8, rounder apd.Rounder) (decimalValue, error) {
	if scale > maximumScale {
		return decimalValue{}, domainError(CodeInvalidScale, operation)
	}
	context := exactContext
	context.Traps = apd.DefaultTraps
	context.Rounding = rounder
	var result apd.Decimal
	if _, err := context.Quantize(&result, &value.decimal, -int32(scale)); err != nil {
		return decimalValue{}, domainError(CodeArithmetic, operation)
	}
	return reducedValue(&result), nil
}
