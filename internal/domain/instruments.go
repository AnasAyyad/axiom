package domain

import "time"

// ProductKind identifies the market product family.
type ProductKind string

// ProductSpot is the only V1 product kind.
const ProductSpot ProductKind = "spot"

// Instrument is a canonical base/quote spot pair.
type Instrument struct {
	Base    AssetSymbol `json:"base"`
	Quote   AssetSymbol `json:"quote"`
	Product ProductKind `json:"product"`
}

// NewSpotInstrument constructs a canonical spot instrument.
func NewSpotInstrument(base, quote AssetSymbol) (Instrument, error) {
	if _, err := ParseAssetSymbol(string(base)); err != nil {
		return Instrument{}, domainError(CodeInvalidInstrument, "instrument_base")
	}
	if _, err := ParseAssetSymbol(string(quote)); err != nil || base == quote {
		return Instrument{}, domainError(CodeInvalidInstrument, "instrument_quote")
	}
	return Instrument{Base: base, Quote: quote, Product: ProductSpot}, nil
}

// Symbol returns the concatenated exchange-neutral pair symbol.
func (instrument Instrument) Symbol() string {
	return string(instrument.Base) + string(instrument.Quote)
}

// InstrumentMetadata is one immutable version of exchange filters.
type InstrumentMetadata struct {
	Instrument      Instrument `json:"instrument"`
	Version         uint64     `json:"version"`
	EffectiveAt     time.Time  `json:"effective_at"`
	PriceTick       Price      `json:"price_tick"`
	QuantityStep    Quantity   `json:"quantity_step"`
	MinimumQuantity Quantity   `json:"minimum_quantity"`
	MinimumNotional Notional   `json:"minimum_notional"`
}

// Validate rejects incomplete, non-spot, or non-UTC metadata.
func (metadata InstrumentMetadata) Validate() error {
	if metadata.Instrument.Product != ProductSpot || metadata.Version == 0 {
		return domainError(CodeInvalidInstrument, "metadata_identity")
	}
	if metadata.EffectiveAt.IsZero() || metadata.EffectiveAt.Location() != time.UTC {
		return domainError(CodeInvalidTimestamp, "metadata_effective_at")
	}
	if metadata.PriceTick.decimal.Sign() <= 0 || metadata.QuantityStep.decimal.Sign() <= 0 {
		return domainError(CodeInvalidScale, "metadata_filters")
	}
	return nil
}
