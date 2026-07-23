package arbitrage

import (
	"time"

	"axiom/internal/domain"
	"axiom/internal/marketdata"
)

// Error is one bounded exact-conversion rejection.
type Error struct{ Code string }

// Error returns a stable reason without market payload content.
func (failure *Error) Error() string { return "arbitrage_conversion:" + failure.Code }

func conversionError(code string) error { return &Error{Code: code} }

// FeeSchedule identifies the exact fee rate, asset, and optional third-asset
// conversion mark used for one immutable decision.
type FeeSchedule struct {
	Version                string
	Rate                   domain.Rate
	Asset                  domain.AssetSymbol
	ThirdAssetPriceInQuote domain.Price
}

// InstrumentRules extends shared instrument metadata with B4/B5 exact maximum
// quantity and immutable fee facts.
type InstrumentRules struct {
	Exchange        string
	Metadata        domain.InstrumentMetadata
	MaximumQuantity domain.Quantity
	Fee             FeeSchedule
	Active          bool
	ObservedAt      time.Time
}

// Request asks for one exact source-to-target spot conversion.
type Request struct {
	Source domain.AssetSymbol
	Target domain.AssetSymbol
	Input  domain.Quantity
	Book   marketdata.BookView
	Rules  InstrumentRules
}

// Result is the complete exact conversion and cost attribution for one leg.
type Result struct {
	Exchange           string
	Instrument         domain.Instrument
	Side               domain.Side
	Source             domain.AssetSymbol
	Target             domain.AssetSymbol
	Input              domain.Quantity
	TradeQuantity      domain.Quantity
	GrossOutput        domain.Quantity
	NetOutput          domain.Quantity
	SourceDust         domain.Quantity
	FeeAsset           domain.AssetSymbol
	FeeQuantity        domain.Quantity
	FeeQuoteEquivalent domain.Money
	Notional           domain.Notional
	VWAP               domain.Price
	SpreadCost         domain.Money
	MetadataVersion    uint64
	FeeVersion         string
	BookVersion        uint64
	Generation         uint64
}
