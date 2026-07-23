package meanreversion

import (
	"github.com/cockroachdb/apd/v3"

	"axiom/internal/domain"
)

// OpenPosition creates immutable state from the actual simulated fill and the
// signal ATR. The z-score protection remains a separately evaluated completed-
// candle exit; it must not weaken or replace the 2.5 ATR intrabar trigger.
func OpenPosition(actualEntry, signalATR domain.Price, quantity domain.Quantity,
	configuration Configuration) (PositionState, error) {
	entry, entryErr := parseDecimal(actualEntry.String())
	atr, atrErr := parseDecimal(signalATR.String())
	if entryErr != nil || atrErr != nil || entry.value.Sign() <= 0 || atr.value.Sign() <= 0 {
		return PositionState{}, strategyError(ReasonInvalidSizing)
	}
	distance, err := atr.multiply(configuration.ProtectiveStopMultiplier, apd.RoundHalfEven)
	if err != nil {
		return PositionState{}, err
	}
	atrStop, err := entry.subtract(distance)
	if err != nil {
		return PositionState{}, strategyError(ReasonInvalidSizing)
	}
	if atrStop.value.Sign() <= 0 {
		return PositionState{}, strategyError(ReasonInvalidSizing)
	}
	stopPrice, err := domain.ParsePrice(atrStop.stringValue())
	if err != nil {
		return PositionState{}, err
	}
	return PositionState{Open: true, Quantity: quantity, ActualEntryPrice: actualEntry,
		InitialStop: stopPrice}, nil
}

func validPosition(position PositionState) bool {
	zeroQuantity, _ := domain.ParseQuantity("0")
	zeroPrice, _ := domain.ParsePrice("0")
	if !position.Open {
		return position.Quantity.Compare(zeroQuantity) == 0 && position.ActualEntryPrice.Compare(zeroPrice) == 0 &&
			position.InitialStop.Compare(zeroPrice) == 0
	}
	return position.Quantity.Compare(zeroQuantity) > 0 && position.ActualEntryPrice.Compare(zeroPrice) > 0 &&
		position.InitialStop.Compare(zeroPrice) > 0 && position.InitialStop.Compare(position.ActualEntryPrice) < 0
}

// AdvanceHolding consumes one completed primary candle while the position is open.
func AdvanceHolding(position PositionState) PositionState {
	if position.Open {
		position.HeldCandles++
	}
	return position
}

// CooldownAfterProtectiveExit starts the exact configured completed-candle block.
func CooldownAfterProtectiveExit(configuration Configuration) uint64 {
	return configuration.CooldownCandles
}

// AdvanceCooldown consumes one later completed candle, making the fourth
// candle eligible after the default exact three-candle block.
func AdvanceCooldown(remaining uint64) uint64 {
	if remaining == 0 {
		return 0
	}
	return remaining - 1
}
