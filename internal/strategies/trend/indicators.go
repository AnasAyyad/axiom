package trend

import (
	"strconv"

	"github.com/cockroachdb/apd/v3"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
)

// EMA calculates the simple-mean-seeded exponential moving average at 18 decimals.
func EMA(values []domain.Price, period int) (domain.Price, error) {
	if period <= 0 || len(values) < period {
		return domain.Price{}, trendError(ReasonWarmUp)
	}
	zero, _ := parseDecimal("0")
	sum := zero
	for index := 0; index < period; index++ {
		value, err := parseDecimal(values[index].String())
		if err != nil {
			return domain.Price{}, err
		}
		sum, err = sum.add(value)
		if err != nil {
			return domain.Price{}, err
		}
	}
	denominator, _ := parseDecimal(stringInt(period))
	current, err := sum.divide(denominator, apd.RoundHalfEven)
	if err != nil {
		return domain.Price{}, err
	}
	two, _ := parseDecimal("2")
	periodPlusOne, _ := parseDecimal(stringInt(period + 1))
	alpha, err := two.divide(periodPlusOne, apd.RoundHalfEven)
	if err != nil {
		return domain.Price{}, err
	}
	one, _ := parseDecimal("1")
	oneMinusAlpha, _ := one.subtract(alpha)
	for index := period; index < len(values); index++ {
		value, parseErr := parseDecimal(values[index].String())
		if parseErr != nil {
			return domain.Price{}, parseErr
		}
		weightedValue, multiplyErr := value.multiply(alpha, apd.RoundHalfEven)
		if multiplyErr != nil {
			return domain.Price{}, multiplyErr
		}
		weightedPrior, multiplyErr := current.multiply(oneMinusAlpha, apd.RoundHalfEven)
		if multiplyErr != nil {
			return domain.Price{}, multiplyErr
		}
		current, err = weightedValue.add(weightedPrior)
		if err != nil {
			return domain.Price{}, err
		}
	}
	return domain.ParsePrice(current.stringValue())
}

// ATR calculates exact true range and simple-mean-seeded Wilder smoothing.
func ATR(candles []exchangecontracts.Candle, period int) (domain.Price, error) {
	if period <= 0 || len(candles) < period {
		return domain.Price{}, trendError(ReasonWarmUp)
	}
	ranges, err := trueRanges(candles)
	if err != nil {
		return domain.Price{}, err
	}
	zero, _ := parseDecimal("0")
	sum := zero
	for index := 0; index < period; index++ {
		sum, err = sum.add(ranges[index])
		if err != nil {
			return domain.Price{}, err
		}
	}
	periodValue, _ := parseDecimal(stringInt(period))
	current, err := sum.divide(periodValue, apd.RoundHalfEven)
	if err != nil {
		return domain.Price{}, err
	}
	periodMinusOne, _ := parseDecimal(stringInt(period - 1))
	for index := period; index < len(ranges); index++ {
		weighted, multiplyErr := current.multiply(periodMinusOne, apd.RoundHalfEven)
		if multiplyErr != nil {
			return domain.Price{}, multiplyErr
		}
		weighted, err = weighted.add(ranges[index])
		if err == nil {
			current, err = weighted.divide(periodValue, apd.RoundHalfEven)
		}
		if err != nil {
			return domain.Price{}, err
		}
	}
	return domain.ParsePrice(current.stringValue())
}

func trueRanges(candles []exchangecontracts.Candle) ([]decimal, error) {
	ranges := make([]decimal, len(candles))
	for index, candle := range candles {
		high, err := parseDecimal(candle.High.String())
		if err != nil {
			return nil, err
		}
		low, _ := parseDecimal(candle.Low.String())
		current, err := high.subtract(low)
		if err != nil {
			return nil, err
		}
		if index > 0 {
			previous, _ := parseDecimal(candles[index-1].Close.String())
			highPrevious := absoluteDifference(high, previous)
			lowPrevious := absoluteDifference(low, previous)
			current = maximum(current, maximum(highPrevious, lowPrevious))
		}
		ranges[index] = current
	}
	return ranges, nil
}

func absoluteDifference(left, right decimal) decimal {
	if left.compare(right) >= 0 {
		result, _ := left.subtract(right)
		return result
	}
	result, _ := right.subtract(left)
	return result
}

func stringInt(value int) string { return strconv.Itoa(value) }
