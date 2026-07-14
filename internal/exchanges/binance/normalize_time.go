package binance

import (
	"time"

	exchangecontracts "axiom/internal/exchanges/contracts"
)

func normalizeServerTime(payload []byte) (time.Time, error) {
	var native serverTimePayload
	if err := strictDecode(payload, &native); err != nil || native.ServerTime <= 0 {
		return time.Time{}, exchangecontracts.NewError(
			exchangecontracts.ErrorValidation, exchangecontracts.OperationCapability, 0,
		)
	}
	return time.UnixMilli(native.ServerTime).UTC(), nil
}
