package arbitrage

import (
	"github.com/cockroachdb/apd/v3"

	"axiom/internal/domain"
)

// HaircutQuantity applies one exact conservative non-negative decimal rate.
func HaircutQuantity(value domain.Quantity, rate domain.Rate) (domain.Quantity, error) {
	one, _ := parseNumber("1")
	parsedRate, err := parseNumber(rate.String())
	if err != nil || parsedRate.sign() < 0 || parsedRate.compare(one) > 0 {
		return domain.Quantity{}, conversionError("haircut_invalid")
	}
	multiplier, err := one.subtract(parsedRate, false, apd.RoundFloor)
	if err != nil {
		return domain.Quantity{}, err
	}
	parsedValue, _ := parseNumber(value.String())
	result, err := parsedValue.multiply(multiplier, apd.RoundFloor)
	if err != nil {
		return domain.Quantity{}, err
	}
	return domain.ParseQuantity(result.text())
}

// QuantityDifference returns the exact signed left-minus-right value.
func QuantityDifference(left, right domain.Quantity) (domain.PnL, error) {
	parsedLeft, _ := parseNumber(left.String())
	parsedRight, _ := parseNumber(right.String())
	result, err := parsedLeft.subtract(parsedRight, true, apd.RoundHalfEven)
	if err != nil {
		return domain.PnL{}, err
	}
	return domain.ParsePnL(result.text())
}

// PositiveEdge returns the non-negative exact edge for a profitable conversion.
func PositiveEdge(final, initial domain.Quantity) (domain.Percent, error) {
	if final.Compare(initial) <= 0 {
		return domain.Percent{}, conversionError("edge_not_positive")
	}
	profit, err := final.Subtract(initial)
	if err != nil {
		return domain.Percent{}, err
	}
	numerator, _ := domain.ParseMoney(profit.String())
	denominator, _ := domain.ParseMoney(initial.String())
	return domain.CalculatePercent(numerator, denominator, 18)
}

// AddRates returns an exact non-negative rate sum.
func AddRates(left, right domain.Rate) (domain.Rate, error) {
	parsedLeft, _ := parseNumber(left.String())
	parsedRight, _ := parseNumber(right.String())
	result, err := parsedLeft.add(parsedRight, apd.RoundCeiling)
	if err != nil {
		return domain.Rate{}, err
	}
	return domain.ParseRate(result.text())
}
