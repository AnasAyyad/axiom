package meanreversion

import (
	"strconv"

	"github.com/cockroachdb/apd/v3"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
)

// RollingZScore calculates the population-standard-deviation z-score over the
// last period values. Zero population deviation rejects the candidate.
func RollingZScore(values []domain.Price, period int) (domain.Price, domain.Price, string, error) {
	if period <= 1 || len(values) < period {
		return domain.Price{}, domain.Price{}, "", strategyError(ReasonWarmUp)
	}
	parsed, mean, deviation, err := populationStatistics(values[len(values)-period:])
	if err != nil {
		return domain.Price{}, domain.Price{}, "", err
	}
	latestDeviation, _ := parsed[len(parsed)-1].subtract(mean)
	zscore, err := latestDeviation.divide(deviation, apd.RoundHalfEven)
	if err != nil {
		return domain.Price{}, domain.Price{}, "", err
	}
	meanPrice, meanErr := domain.ParsePrice(mean.stringValue())
	deviationPrice, deviationErr := domain.ParsePrice(deviation.stringValue())
	if meanErr != nil || deviationErr != nil {
		return domain.Price{}, domain.Price{}, "", strategyError(ReasonInvalidSizing)
	}
	return meanPrice, deviationPrice, zscore.stringValue(), nil
}

func populationStatistics(values []domain.Price) ([]decimal, decimal, decimal, error) {
	zero, _ := parseDecimal("0")
	sum := zero
	parsed := make([]decimal, len(values))
	for index, value := range values {
		var err error
		parsed[index], err = parseDecimal(value.String())
		if err != nil {
			return nil, decimal{}, decimal{}, err
		}
		sum, err = sum.add(parsed[index])
		if err != nil {
			return nil, decimal{}, decimal{}, err
		}
	}
	count, _ := parseDecimal(strconv.Itoa(len(parsed)))
	mean, err := sum.divide(count, apd.RoundHalfEven)
	if err != nil {
		return nil, decimal{}, decimal{}, err
	}
	squared, err := squaredDeviationSum(parsed, mean)
	if err != nil {
		return nil, decimal{}, decimal{}, err
	}
	variance, err := squared.divide(count, apd.RoundHalfEven)
	if err != nil {
		return nil, decimal{}, decimal{}, err
	}
	deviation, err := variance.squareRoot()
	if err != nil || deviation.value.Sign() == 0 {
		return nil, decimal{}, decimal{}, strategyError(ReasonZeroDeviation)
	}
	return parsed, mean, deviation, nil
}

func squaredDeviationSum(values []decimal, mean decimal) (decimal, error) {
	squared, _ := parseDecimal("0")
	for _, value := range values {
		deviation, err := value.subtract(mean)
		if err != nil {
			return decimal{}, err
		}
		term, err := deviation.multiply(deviation, apd.RoundHalfEven)
		if err != nil {
			return decimal{}, err
		}
		squared, err = squared.add(term)
		if err != nil {
			return decimal{}, err
		}
	}
	return squared, nil
}

// EMAValues returns every simple-mean-seeded EMA value, beginning at the seed.
func EMAValues(values []domain.Price, period int) ([]domain.Price, error) {
	if period <= 0 || len(values) < period {
		return nil, strategyError(ReasonWarmUp)
	}
	zero, _ := parseDecimal("0")
	sum := zero
	for index := 0; index < period; index++ {
		value, err := parseDecimal(values[index].String())
		if err != nil {
			return nil, err
		}
		sum, err = sum.add(value)
		if err != nil {
			return nil, err
		}
	}
	periodValue, _ := parseDecimal(strconv.Itoa(period))
	current, err := sum.divide(periodValue, apd.RoundHalfEven)
	if err != nil {
		return nil, err
	}
	two, _ := parseDecimal("2")
	periodPlusOne, _ := parseDecimal(strconv.Itoa(period + 1))
	alpha, _ := two.divide(periodPlusOne, apd.RoundHalfEven)
	one, _ := parseDecimal("1")
	inverse, _ := one.subtract(alpha)
	result := make([]domain.Price, 0, len(values)-period+1)
	seed, _ := domain.ParsePrice(current.stringValue())
	result = append(result, seed)
	for index := period; index < len(values); index++ {
		value, parseErr := parseDecimal(values[index].String())
		weighted, multiplyErr := value.multiply(alpha, apd.RoundHalfEven)
		prior, priorErr := current.multiply(inverse, apd.RoundHalfEven)
		if parseErr != nil || multiplyErr != nil || priorErr != nil {
			return nil, strategyError(ReasonInvalidSizing)
		}
		current, err = weighted.add(prior)
		if err != nil {
			return nil, err
		}
		price, priceErr := domain.ParsePrice(current.stringValue())
		if priceErr != nil {
			return nil, priceErr
		}
		result = append(result, price)
	}
	return result, nil
}

// EMADecline returns latest EMA, decline fraction across lookback completed
// higher-timeframe candles, and whether the inclusive threshold is met.
func EMADecline(values []domain.Price, period, lookback int, threshold string) (domain.Price, string, bool, error) {
	series, err := EMAValues(values, period)
	if err != nil || lookback <= 0 || len(series) <= lookback {
		return domain.Price{}, "", false, strategyError(ReasonWarmUp)
	}
	decline, strong, err := ClassifyEMADecline(series[len(series)-1-lookback], series[len(series)-1], threshold)
	if err != nil {
		return domain.Price{}, "", false, err
	}
	return series[len(series)-1], decline, strong, nil
}

// ClassifyEMADecline applies the exact inclusive fractional decline boundary.
func ClassifyEMADecline(priorPrice, latestPrice domain.Price, threshold string) (string, bool, error) {
	prior, err := parseDecimal(priorPrice.String())
	if err != nil || prior.value.Sign() <= 0 {
		return "", false, strategyError(ReasonInvalidSizing)
	}
	latest, err := parseDecimal(latestPrice.String())
	if err != nil {
		return "", false, err
	}
	difference, err := prior.subtract(latest)
	if err != nil {
		return "", false, err
	}
	decline, err := difference.divide(prior, apd.RoundHalfEven)
	if err != nil {
		return "", false, err
	}
	boundary, err := parseDecimal(threshold)
	if err != nil {
		return "", false, err
	}
	return decline.stringValue(), decline.compare(boundary) >= 0, nil
}

// ATR calculates true range and simple-mean-seeded Wilder smoothing.
func ATR(candles []exchangecontracts.Candle, period int) (domain.Price, error) {
	if period <= 0 || len(candles) < period {
		return domain.Price{}, strategyError(ReasonWarmUp)
	}
	ranges, err := directionalFacts(candles)
	if err != nil {
		return domain.Price{}, err
	}
	zero, _ := parseDecimal("0")
	sum := zero
	for index := 0; index < period; index++ {
		sum, err = sum.add(ranges[index].trueRange)
		if err != nil {
			return domain.Price{}, err
		}
	}
	periodValue, _ := parseDecimal(strconv.Itoa(period))
	current, err := sum.divide(periodValue, apd.RoundHalfEven)
	periodMinusOne, _ := parseDecimal(strconv.Itoa(period - 1))
	for index := period; err == nil && index < len(ranges); index++ {
		current, err = wilderAverage(current, ranges[index].trueRange, periodValue, periodMinusOne)
	}
	if err != nil {
		return domain.Price{}, err
	}
	return domain.ParsePrice(current.stringValue())
}

// ADX calculates exact Wilder ADX. It requires two full periods of candles.
func ADX(candles []exchangecontracts.Candle, period int) (string, error) {
	if period <= 1 || len(candles) < period*2 {
		return "", strategyError(ReasonWarmUp)
	}
	facts, err := directionalFacts(candles)
	if err != nil {
		return "", err
	}
	periodValue, _ := parseDecimal(strconv.Itoa(period))
	smoothedTR, smoothedPlus, smoothedMinus, err := initialDirectionalSums(facts, period)
	if err != nil {
		return "", err
	}
	dxValues, err := directionalIndexSeries(facts, period, periodValue, smoothedTR, smoothedPlus, smoothedMinus)
	if err != nil {
		return "", err
	}
	return smoothDirectionalIndexes(dxValues, period, periodValue)
}

func initialDirectionalSums(facts []directionalFact, period int) (decimal, decimal, decimal, error) {
	zero, _ := parseDecimal("0")
	smoothedTR, smoothedPlus, smoothedMinus := zero, zero, zero
	var err error
	for index := 1; index <= period; index++ {
		smoothedTR, err = smoothedTR.add(facts[index].trueRange)
		if err == nil {
			smoothedPlus, err = smoothedPlus.add(facts[index].plusDM)
		}
		if err == nil {
			smoothedMinus, err = smoothedMinus.add(facts[index].minusDM)
		}
		if err != nil {
			return decimal{}, decimal{}, decimal{}, err
		}
	}
	return smoothedTR, smoothedPlus, smoothedMinus, nil
}

func directionalIndexSeries(facts []directionalFact, period int, periodValue,
	smoothedTR, smoothedPlus, smoothedMinus decimal,
) ([]decimal, error) {
	values := make([]decimal, 0, len(facts)-period)
	for index := period; index < len(facts); index++ {
		var err error
		if index > period {
			smoothedTR, err = wilderSum(smoothedTR, facts[index].trueRange, periodValue)
			if err == nil {
				smoothedPlus, err = wilderSum(smoothedPlus, facts[index].plusDM, periodValue)
			}
			if err == nil {
				smoothedMinus, err = wilderSum(smoothedMinus, facts[index].minusDM, periodValue)
			}
		}
		if err != nil {
			return nil, err
		}
		dx, err := directionalIndex(smoothedTR, smoothedPlus, smoothedMinus)
		if err != nil {
			return nil, err
		}
		values = append(values, dx)
	}
	return values, nil
}

func smoothDirectionalIndexes(values []decimal, period int, periodValue decimal) (string, error) {
	if len(values) < period {
		return "", strategyError(ReasonWarmUp)
	}
	sum, _ := parseDecimal("0")
	var err error
	for index := 0; index < period; index++ {
		sum, err = sum.add(values[index])
		if err != nil {
			return "", err
		}
	}
	adx, err := sum.divide(periodValue, apd.RoundHalfEven)
	periodMinusOne, _ := parseDecimal(strconv.Itoa(period - 1))
	for index := period; err == nil && index < len(values); index++ {
		adx, err = wilderAverage(adx, values[index], periodValue, periodMinusOne)
	}
	if err != nil {
		return "", err
	}
	return adx.stringValue(), nil
}

type directionalFact struct{ trueRange, plusDM, minusDM decimal }

func directionalFacts(candles []exchangecontracts.Candle) ([]directionalFact, error) {
	facts := make([]directionalFact, len(candles))
	zero, _ := parseDecimal("0")
	for index, candle := range candles {
		high, highErr := parseDecimal(candle.High.String())
		low, lowErr := parseDecimal(candle.Low.String())
		if highErr != nil || lowErr != nil {
			return nil, strategyError(ReasonInvalidSizing)
		}
		current, err := high.subtract(low)
		if err != nil {
			return nil, err
		}
		facts[index] = directionalFact{trueRange: current, plusDM: zero, minusDM: zero}
		if index == 0 {
			continue
		}
		previousHigh, _ := parseDecimal(candles[index-1].High.String())
		previousLow, _ := parseDecimal(candles[index-1].Low.String())
		previousClose, _ := parseDecimal(candles[index-1].Close.String())
		facts[index].trueRange = maximum(current, maximum(absoluteDifference(high, previousClose), absoluteDifference(low, previousClose)))
		up, _ := high.subtract(previousHigh)
		down, _ := previousLow.subtract(low)
		if up.value.Sign() > 0 && up.compare(down) > 0 {
			facts[index].plusDM = up
		}
		if down.value.Sign() > 0 && down.compare(up) > 0 {
			facts[index].minusDM = down
		}
	}
	return facts, nil
}

func directionalIndex(smoothedTR, plus, minus decimal) (decimal, error) {
	if smoothedTR.value.Sign() <= 0 {
		return decimal{}, strategyError(ReasonZeroDeviation)
	}
	hundred, _ := parseDecimal("100")
	plusDI, err := plus.divide(smoothedTR, apd.RoundHalfEven)
	if err == nil {
		plusDI, err = plusDI.multiply(hundred, apd.RoundHalfEven)
	}
	minusDI, minusErr := minus.divide(smoothedTR, apd.RoundHalfEven)
	if minusErr == nil {
		minusDI, minusErr = minusDI.multiply(hundred, apd.RoundHalfEven)
	}
	if err != nil || minusErr != nil {
		return decimal{}, strategyError(ReasonInvalidSizing)
	}
	denominator, err := plusDI.add(minusDI)
	if err != nil || denominator.value.Sign() == 0 {
		zero, _ := parseDecimal("0")
		return zero, nil
	}
	numerator, _ := plusDI.subtract(minusDI)
	numerator = absolute(numerator)
	dx, err := numerator.divide(denominator, apd.RoundHalfEven)
	if err == nil {
		dx, err = dx.multiply(hundred, apd.RoundHalfEven)
	}
	return dx, err
}

func wilderAverage(current, next, period, periodMinusOne decimal) (decimal, error) {
	weighted, err := current.multiply(periodMinusOne, apd.RoundHalfEven)
	if err == nil {
		weighted, err = weighted.add(next)
	}
	if err != nil {
		return decimal{}, err
	}
	return weighted.divide(period, apd.RoundHalfEven)
}

func wilderSum(current, next, period decimal) (decimal, error) {
	portion, err := current.divide(period, apd.RoundHalfEven)
	if err != nil {
		return decimal{}, err
	}
	reduced, err := current.subtract(portion)
	if err != nil {
		return decimal{}, err
	}
	return reduced.add(next)
}

func absoluteDifference(left, right decimal) decimal {
	value, _ := left.subtract(right)
	return absolute(value)
}
