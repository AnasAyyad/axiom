package trend

import (
	"github.com/cockroachdb/apd/v3"

	"axiom/internal/domain"
)

// OpenPosition creates immutable position state from the actual simulated fill.
func OpenPosition(actualEntry, signalATR domain.Price, quantity domain.Quantity, configuration Configuration) (PositionState, error) {
	entry, err := parseDecimal(actualEntry.String())
	if err != nil || entry.value.Sign() <= 0 {
		return PositionState{}, trendError(ReasonInvalidSizing)
	}
	atr, err := parseDecimal(signalATR.String())
	if err != nil || atr.value.Sign() <= 0 {
		return PositionState{}, trendError(ReasonInvalidSizing)
	}
	distance, err := atr.multiply(configuration.InitialStopMultiplier, apd.RoundHalfEven)
	if err != nil {
		return PositionState{}, err
	}
	stop, err := entry.subtract(distance)
	if err != nil || stop.value.Sign() <= 0 {
		return PositionState{}, trendError(ReasonInvalidSizing)
	}
	stopPrice, err := domain.ParsePrice(stop.stringValue())
	if err != nil {
		return PositionState{}, err
	}
	zero, _ := domain.ParsePrice("0")
	return PositionState{Open: true, Quantity: quantity, ActualEntryPrice: actualEntry, SignalATR: signalATR,
		InitialStop: stopPrice, TrailingStop: zero, HighestFavorableClose: actualEntry}, nil
}

// AdvancePosition returns a tightened trailing stop using completed closes only.
func AdvancePosition(position PositionState, completedClose, atr domain.Price, configuration Configuration) (PositionState, error) {
	if !position.Open {
		return PositionState{}, trendError(ReasonExistingPosition)
	}
	advanced := position
	if completedClose.Compare(advanced.HighestFavorableClose) > 0 {
		advanced.HighestFavorableClose = completedClose
	}
	highest, err := parseDecimal(advanced.HighestFavorableClose.String())
	if err != nil {
		return PositionState{}, err
	}
	atrValue, err := parseDecimal(atr.String())
	if err != nil || atrValue.value.Sign() <= 0 {
		return PositionState{}, trendError(ReasonInvalidSizing)
	}
	distance, err := atrValue.multiply(configuration.TrailingStopMultiplier, apd.RoundHalfEven)
	if err != nil {
		return PositionState{}, err
	}
	proposed, err := highest.subtract(distance)
	if err != nil || proposed.value.Sign() <= 0 {
		return advanced, nil
	}
	proposedPrice, err := domain.ParsePrice(proposed.stringValue())
	if err != nil {
		return PositionState{}, err
	}
	if proposedPrice.Compare(advanced.TrailingStop) > 0 {
		advanced.TrailingStop = proposedPrice
	}
	return advanced, nil
}

// CooldownAfterProtectiveExit starts the configured completed-candle block.
func CooldownAfterProtectiveExit(configuration Configuration) uint64 {
	return configuration.CooldownCandles
}

// AdvanceCooldown consumes one later completed candle; the fourth candle after
// the default three-candle block is therefore eligible.
func AdvanceCooldown(remaining uint64) uint64 {
	if remaining == 0 {
		return 0
	}
	return remaining - 1
}
