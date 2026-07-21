package trend

import (
	"time"

	exchangecontracts "axiom/internal/exchanges/contracts"
)

func admitCandles(input Input, configuration Configuration) ([]exchangecontracts.Candle, string) {
	if len(input.Candles) == 0 {
		return nil, ReasonWarmUp
	}
	for index := 1; index < len(input.Candles); index++ {
		if input.Candles[index].OpenTime.Before(input.Candles[index-1].OpenTime) {
			return nil, ReasonCandleOrder
		}
	}
	unique := make([]exchangecontracts.Candle, 0, len(input.Candles))
	for _, candle := range input.Candles {
		if candle.Exchange != "binance" || candle.Interval != "4h" || !candle.Closed || candle.Instrument != input.Instrument ||
			!utcAligned(candle) || candle.RawPayloadHash == "" {
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
			case !candle.OpenTime.Equal(prior.OpenTime.Add(4 * time.Hour)):
				return nil, ReasonCandleGap
			}
		}
		unique = append(unique, candle)
	}
	if len(unique) < configuration.EMARegime {
		return nil, ReasonWarmUp
	}
	latest := unique[len(unique)-1]
	publication := latest.ReceivedAt.UTC
	eligibleAt := publication.Add(configuration.FinalizationDelay)
	if input.Now.Before(eligibleAt) {
		return nil, ReasonCandleFinality
	}
	if !input.Now.Before(eligibleAt.Add(configuration.EvaluationWindow)) {
		return nil, ReasonStaleSignal
	}
	return unique, ""
}

func identicalCandle(left, right exchangecontracts.Candle) bool {
	return left.Exchange == right.Exchange && left.Instrument == right.Instrument && left.Interval == right.Interval &&
		left.OpenTime.Equal(right.OpenTime) && left.CloseTime.Equal(right.CloseTime) &&
		left.Open.Compare(right.Open) == 0 && left.High.Compare(right.High) == 0 &&
		left.Low.Compare(right.Low) == 0 && left.Close.Compare(right.Close) == 0 &&
		left.Volume.Compare(right.Volume) == 0 && left.Closed == right.Closed &&
		left.ReceivedAt.UTC.Equal(right.ReceivedAt.UTC) && left.ReceivedAt.Sequence == right.ReceivedAt.Sequence &&
		left.RawPayloadHash == right.RawPayloadHash
}

func utcAligned(candle exchangecontracts.Candle) bool {
	open := candle.OpenTime.UTC()
	if open != candle.OpenTime || open.Minute() != 0 || open.Second() != 0 || open.Nanosecond() != 0 || open.Hour()%4 != 0 {
		return false
	}
	closeTime := candle.CloseTime.UTC()
	exact := closeTime.Equal(open.Add(4 * time.Hour))
	binanceStyle := closeTime.Equal(open.Add(4*time.Hour - time.Millisecond))
	return (exact || binanceStyle) && !candle.ReceivedAt.UTC.Before(closeTime)
}
