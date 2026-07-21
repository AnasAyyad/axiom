package binance

import (
	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
)

func instrumentForSymbol(symbol string) (domain.Instrument, error) {
	var base, quote domain.AssetSymbol
	switch symbol {
	case "BTCUSDT":
		base, quote = "BTC", "USDT"
	case "ETHUSDT":
		base, quote = "ETH", "USDT"
	case "ETHBTC":
		base, quote = "ETH", "BTC"
	default:
		return domain.Instrument{}, exchangecontracts.NewError(
			exchangecontracts.ErrorValidation, exchangecontracts.OperationMetadata, 0,
		)
	}
	instrument, err := domain.NewSpotInstrument(base, quote)
	if err != nil {
		return domain.Instrument{}, exchangecontracts.NewError(
			exchangecontracts.ErrorValidation, exchangecontracts.OperationMetadata, 0,
		)
	}
	return instrument, nil
}

func supportedCandleInterval(interval string) bool {
	return interval == "15m" || interval == "1h" || interval == "4h"
}

func validSpotInstrument(instrument domain.Instrument) bool {
	validated, err := domain.NewSpotInstrument(instrument.Base, instrument.Quote)
	return err == nil && validated == instrument
}

func candleValues(native candleBodyPayload) (
	domain.Price,
	domain.Price,
	domain.Price,
	domain.Price,
	domain.Quantity,
	error,
) {
	open, openErr := domain.ParsePrice(native.Open)
	high, highErr := domain.ParsePrice(native.High)
	low, lowErr := domain.ParsePrice(native.Low)
	closeValue, closeErr := domain.ParsePrice(native.Close)
	volume, volumeErr := domain.ParseQuantity(native.Volume)
	if openErr != nil || highErr != nil || lowErr != nil || closeErr != nil || volumeErr != nil ||
		high.Compare(open) < 0 || high.Compare(closeValue) < 0 || low.Compare(open) > 0 || low.Compare(closeValue) > 0 {
		return domain.Price{}, domain.Price{}, domain.Price{}, domain.Price{}, domain.Quantity{},
			exchangecontracts.NewError(exchangecontracts.ErrorValidation, exchangecontracts.OperationCandles, 0)
	}
	return open, high, low, closeValue, volume, nil
}
