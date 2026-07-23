package meanreversion

import (
	"time"

	exchangecontracts "axiom/internal/exchanges/contracts"
)

func admitCandles(input Input, configuration Configuration) ([]exchangecontracts.Candle, []exchangecontracts.Candle, string) {
	primary, reason := admitSeries(input.PrimaryCandles, input.Instrument, configuration.PrimaryTimeframe, time.Hour, input.Now)
	if reason != "" {
		return nil, nil, reason
	}
	higher, reason := admitSeries(input.HigherCandles, input.Instrument, configuration.HigherTimeframe, 4*time.Hour, input.Now)
	if reason != "" {
		return nil, nil, reason
	}
	minimumPrimary := configuration.ZScorePeriod
	if configuration.ADXPeriod*2 > minimumPrimary {
		minimumPrimary = configuration.ADXPeriod * 2
	}
	if configuration.ATRPeriod > minimumPrimary {
		minimumPrimary = configuration.ATRPeriod
	}
	if len(primary) < minimumPrimary || len(higher) < configuration.EMARegimePeriod+configuration.EMADeclineLookback {
		return nil, nil, ReasonWarmUp
	}
	latestPrimary, latestHigher := primary[len(primary)-1], higher[len(higher)-1]
	primaryEnd := latestPrimary.OpenTime.Add(time.Hour)
	higherEnd := latestHigher.OpenTime.Add(4 * time.Hour)
	if higherEnd.After(primaryEnd) || !primaryEnd.Before(higherEnd.Add(4*time.Hour)) ||
		latestPrimary.Exchange != latestHigher.Exchange {
		return nil, nil, ReasonTimeframeAlignment
	}
	primaryEligible := latestPrimary.ReceivedAt.UTC.Add(configuration.FinalizationDelay)
	higherEligible := latestHigher.ReceivedAt.UTC.Add(configuration.FinalizationDelay)
	if input.Now.Before(primaryEligible) || input.Now.Before(higherEligible) {
		return nil, nil, ReasonCandleFinality
	}
	if !input.Now.Before(primaryEligible.Add(configuration.EvaluationWindow)) {
		return nil, nil, ReasonStaleSignal
	}
	return primary, higher, ""
}

func admitSeries(candles []exchangecontracts.Candle, instrument interface{ Symbol() string }, interval string,
	duration time.Duration, now time.Time,
) ([]exchangecontracts.Candle, string) {
	if len(candles) == 0 {
		return nil, ReasonWarmUp
	}
	unique := make([]exchangecontracts.Candle, 0, len(candles))
	for _, candle := range candles {
		if (candle.Exchange != "binance" && candle.Exchange != "bybit") || candle.Interval != interval ||
			!candle.Closed || candle.Instrument.Symbol() != instrument.Symbol() || candle.RawPayloadHash == "" ||
			!utcAligned(candle, duration) {
			return nil, ReasonCandleFinality
		}
		if len(unique) > 0 {
			prior := unique[len(unique)-1]
			switch {
			case candle.OpenTime.Before(prior.OpenTime):
				return nil, ReasonCandleOrder
			case candle.OpenTime.Equal(prior.OpenTime):
				if !identicalCandle(candle, prior) {
					return nil, ReasonCandleConflict
				}
				continue
			case !candle.OpenTime.Equal(prior.OpenTime.Add(duration)):
				return nil, ReasonCandleGap
			}
		}
		if candle.ReceivedAt.UTC.After(now) {
			return nil, ReasonCandleFinality
		}
		unique = append(unique, candle)
	}
	return unique, ""
}

func utcAligned(candle exchangecontracts.Candle, duration time.Duration) bool {
	open := candle.OpenTime.UTC()
	if open != candle.OpenTime || open.Minute() != 0 || open.Second() != 0 || open.Nanosecond() != 0 {
		return false
	}
	if duration == 4*time.Hour && open.Hour()%4 != 0 {
		return false
	}
	exact := candle.CloseTime.Equal(open.Add(duration))
	binanceStyle := candle.CloseTime.Equal(open.Add(duration - time.Millisecond))
	return (exact || binanceStyle) && !candle.ReceivedAt.UTC.Before(candle.CloseTime)
}

func identicalCandle(left, right exchangecontracts.Candle) bool {
	return left.Exchange == right.Exchange && left.Instrument == right.Instrument && left.Interval == right.Interval &&
		left.OpenTime.Equal(right.OpenTime) && left.CloseTime.Equal(right.CloseTime) &&
		left.Open.Compare(right.Open) == 0 && left.High.Compare(right.High) == 0 && left.Low.Compare(right.Low) == 0 &&
		left.Close.Compare(right.Close) == 0 && left.Volume.Compare(right.Volume) == 0 && left.Closed == right.Closed &&
		left.ReceivedAt.UTC.Equal(right.ReceivedAt.UTC) && left.ReceivedAt.Sequence == right.ReceivedAt.Sequence &&
		left.RawPayloadHash == right.RawPayloadHash
}
