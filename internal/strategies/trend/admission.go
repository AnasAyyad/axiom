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
		if candle.Interval != "4h" || !candle.Closed || candle.Instrument != input.Instrument ||
			!utcAligned(candle) || candle.RawPayloadHash == "" {
			return nil, ReasonCandleFinality
		}
		if len(unique) > 0 {
			prior := unique[len(unique)-1]
			switch {
			case candle.OpenTime.Before(prior.OpenTime):
				return nil, ReasonCandleOrder
			case candle.OpenTime.Equal(prior.OpenTime):
				if candle.RawPayloadHash != prior.RawPayloadHash || candle.Close.Compare(prior.Close) != 0 ||
					candle.High.Compare(prior.High) != 0 || candle.Low.Compare(prior.Low) != 0 {
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
