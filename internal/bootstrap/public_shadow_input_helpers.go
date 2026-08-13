package bootstrap

import (
	"fmt"
	"sort"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/portfolio"
)

func mergeOwnerConsoleCandles(history, live []exchangecontracts.Candle, now time.Time) []exchangecontracts.Candle {
	items := make(map[int64]exchangecontracts.Candle, len(history)+len(live))
	for _, candle := range append(append([]exchangecontracts.Candle(nil), history...), live...) {
		if candle.Closed && !candle.CloseTime.After(now) {
			items[candle.OpenTime.UnixNano()] = candle
		}
	}
	result := make([]exchangecontracts.Candle, 0, len(items))
	for _, candle := range items {
		result = append(result, candle)
	}
	sort.Slice(result, func(left, right int) bool { return result[left].OpenTime.Before(result[right].OpenTime) })
	if len(result) > 1000 {
		result = result[len(result)-1000:]
	}
	return result
}

func publicShadowFeeRate(model string) (domain.Rate, error) {
	if model != "fixed-bps-v1" {
		return domain.Rate{}, fmt.Errorf("shadow_fee_model_unsupported")
	}
	return domain.ParseRate("0.001")
}

func ownerConsoleBookAge(current, published uint64) time.Duration {
	if published == 0 || current < published || current-published > uint64(time.Duration(1<<63-1)) {
		return time.Duration(1<<63 - 1)
	}
	return time.Duration(current - published)
}

func positionRevision(snapshot portfolio.Snapshot, instrument domain.Instrument) uint64 {
	for _, position := range snapshot.Positions {
		if position.Instrument == instrument {
			return position.Revision
		}
	}
	return 1
}
