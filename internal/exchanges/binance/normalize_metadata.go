package binance

import (
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
)

// NormalizeInstruments strictly converts Binance public exchange information.
// Native status is retained even when an unknown value returns a typed error.
func NormalizeInstruments(
	payload []byte,
	observedAt time.Time,
	version uint64,
) ([]exchangecontracts.InstrumentRecord, error) {
	var native exchangeInfoPayload
	if err := strictDecode(payload, &native); err != nil || native.Timezone != "UTC" || len(native.ExchangeFilter) != 0 ||
		native.ServerTime <= 0 || observedAt.IsZero() || observedAt.Location() != time.UTC || version == 0 {
		return nil, exchangecontracts.NewError(exchangecontracts.ErrorValidation, exchangecontracts.OperationMetadata, 0)
	}
	records := make([]exchangecontracts.InstrumentRecord, 0, len(native.Symbols))
	unknownStatus := false
	for _, item := range native.Symbols {
		record, err := normalizeInstrument(item, observedAt, version, payloadHash(payload))
		if err != nil {
			return nil, err
		}
		records = append(records, record)
		unknownStatus = unknownStatus || item.Status != "TRADING"
	}
	if unknownStatus {
		return records, exchangecontracts.NewError(exchangecontracts.ErrorValidation, exchangecontracts.OperationMetadata, 0)
	}
	return records, nil
}

func normalizeInstrument(
	native exchangeInstrumentPayload,
	observedAt time.Time,
	version uint64,
	rawHash string,
) (exchangecontracts.InstrumentRecord, error) {
	base, baseErr := domain.ParseAssetSymbol(native.BaseAsset)
	quote, quoteErr := domain.ParseAssetSymbol(native.QuoteAsset)
	instrument, instrumentErr := domain.NewSpotInstrument(base, quote)
	if baseErr != nil || quoteErr != nil || instrumentErr != nil || instrument.Symbol() != native.Symbol {
		return exchangecontracts.InstrumentRecord{}, metadataError()
	}
	priceTick, quantityStep, minimumQuantity, minimumNotional, err := normalizeFilters(native.Filters)
	if err != nil {
		return exchangecontracts.InstrumentRecord{}, err
	}
	metadata := domain.InstrumentMetadata{
		Instrument:      instrument,
		Version:         version,
		EffectiveAt:     observedAt,
		PriceTick:       priceTick,
		QuantityStep:    quantityStep,
		MinimumQuantity: minimumQuantity,
		MinimumNotional: minimumNotional,
	}
	if err := metadata.Validate(); err != nil {
		return exchangecontracts.InstrumentRecord{}, metadataError()
	}
	return exchangecontracts.InstrumentRecord{
		Exchange:       "binance",
		NativeSymbol:   native.Symbol,
		NativeStatus:   native.Status,
		Metadata:       metadata,
		RawPayloadHash: rawHash,
	}, nil
}

func normalizeFilters(filters []filterPayload) (
	domain.Price,
	domain.Quantity,
	domain.Quantity,
	domain.Notional,
	error,
) {
	var tick domain.Price
	var step, minimum domain.Quantity
	var notional domain.Notional
	seen := make(map[string]bool, 3)
	for _, filter := range filters {
		if seen[filter.Type] {
			return tick, step, minimum, notional, metadataError()
		}
		seen[filter.Type] = true
		var err error
		switch filter.Type {
		case "PRICE_FILTER":
			tick, err = domain.ParsePrice(filter.TickSize)
		case "LOT_SIZE":
			minimum, err = domain.ParseQuantity(filter.MinimumQty)
			if err == nil {
				step, err = domain.ParseQuantity(filter.StepSize)
			}
		case "NOTIONAL":
			notional, err = domain.ParseNotional(filter.MinimumNotional)
		case "MIN_NOTIONAL":
			notional, err = domain.ParseNotional(filter.MinimumNotional)
		case "ICEBERG_PARTS", "MARKET_LOT_SIZE", "TRAILING_DELTA", "PERCENT_PRICE",
			"PERCENT_PRICE_BY_SIDE", "MAX_NUM_ORDERS", "MAX_NUM_ORDER_LISTS", "MAX_NUM_ALGO_ORDERS",
			"MAX_NUM_ORDER_AMENDS":
			// Known public constraints that do not define the canonical price/size minima.
		default:
			return tick, step, minimum, notional, metadataError()
		}
		if err != nil {
			return tick, step, minimum, notional, metadataError()
		}
	}
	if !seen["PRICE_FILTER"] || !seen["LOT_SIZE"] || (seen["NOTIONAL"] == seen["MIN_NOTIONAL"]) {
		return tick, step, minimum, notional, metadataError()
	}
	return tick, step, minimum, notional, nil
}

func metadataError() error {
	return exchangecontracts.NewError(exchangecontracts.ErrorValidation, exchangecontracts.OperationMetadata, 0)
}
