package bybit

import (
	"context"
	"errors"
	"net/url"
	"time"

	"axiom/internal/domain"
	"axiom/internal/sandbox"
)

// ErrDemoFilter is the generic fail-closed Demo instrument-filter error.
var ErrDemoFilter = errors.New("bybit_demo_instrument_filter_rejected")

// DemoInstrumentRules is one immutable Bybit Demo Spot instrument rule set.
type DemoInstrumentRules struct {
	Instrument         domain.Instrument
	QuantityStep       domain.Quantity
	MinimumQuantity    domain.Quantity
	MaximumQuantity    domain.Quantity
	MaximumLimitQty    domain.Quantity
	PostOnlyMaximumQty domain.Quantity
	PriceTick          domain.Price
	MinimumOrderAmount domain.Notional
	MaximumOrderAmount domain.Notional
	ObservedAt         time.Time
	SourceHash         string
}

func (client *SandboxClient) loadDemoRules(
	ctx context.Context,
	instrument domain.Instrument,
) (DemoInstrumentRules, error) {
	body, err := client.executePublicUnsigned(
		ctx,
		"/v5/market/instruments-info",
		url.Values{
			"category": {"spot"},
			"symbol":   {instrument.Symbol()},
		},
	)
	if err != nil {
		return DemoInstrumentRules{}, err
	}
	return normalizeDemoRules(body, instrument, client.now().UTC())
}

func normalizeDemoRules(
	body []byte,
	instrument domain.Instrument,
	observedAt time.Time,
) (DemoInstrumentRules, error) {
	result, err := decodeDemoResult[instrumentsResult](body)
	if err != nil || result.Category != "spot" ||
		result.NextPageCursor != "" || len(result.List) != 1 ||
		observedAt.IsZero() || observedAt.Location() != time.UTC {
		return DemoInstrumentRules{}, ErrDemoFilter
	}
	item := result.List[0]
	if item.Symbol != instrument.Symbol() ||
		item.BaseCoin != string(instrument.Base) ||
		item.QuoteCoin != string(instrument.Quote) ||
		item.Status != "Trading" ||
		(item.MarginTrading != "none" && item.MarginTrading != "utaOnly") {
		return DemoInstrumentRules{}, ErrDemoFilter
	}
	rules, err := parseDemoRules(item, instrument, observedAt)
	if err != nil {
		return DemoInstrumentRules{}, err
	}
	rules.SourceHash = payloadHash(body)
	if rules.validate() != nil {
		return DemoInstrumentRules{}, ErrDemoFilter
	}
	return rules, nil
}

func parseDemoRules(
	item instrumentPayload,
	instrument domain.Instrument,
	observedAt time.Time,
) (DemoInstrumentRules, error) {
	quantityStep, quantityErr := domain.ParseQuantity(
		item.LotSizeFilter.BasePrecision,
	)
	minimumQuantity, minimumQtyErr := domain.ParseQuantity(
		item.LotSizeFilter.MinimumOrderQty,
	)
	maximumQuantity, maximumQtyErr := domain.ParseQuantity(
		item.LotSizeFilter.MaximumOrderQty,
	)
	maximumLimit, maximumLimitErr := domain.ParseQuantity(
		item.LotSizeFilter.MaximumLimitQty,
	)
	postOnlyMaximum, postOnlyErr := domain.ParseQuantity(
		item.LotSizeFilter.PostOnlyMaximumQty,
	)
	priceTick, priceErr := domain.ParsePrice(item.PriceFilter.TickSize)
	minimumAmount, minimumAmountErr := domain.ParseNotional(
		item.LotSizeFilter.MinimumOrderAmount,
	)
	maximumAmount, maximumAmountErr := domain.ParseNotional(
		item.LotSizeFilter.MaximumOrderAmount,
	)
	if quantityErr != nil || minimumQtyErr != nil || maximumQtyErr != nil ||
		maximumLimitErr != nil || postOnlyErr != nil || priceErr != nil ||
		minimumAmountErr != nil || maximumAmountErr != nil ||
		!positiveDecimal(item.LotSizeFilter.BasePrecision) ||
		!positiveDecimal(item.PriceFilter.TickSize) {
		return DemoInstrumentRules{}, ErrDemoFilter
	}
	rules := DemoInstrumentRules{
		Instrument: instrument, QuantityStep: quantityStep,
		MinimumQuantity: minimumQuantity, MaximumQuantity: maximumQuantity,
		MaximumLimitQty: maximumLimit, PostOnlyMaximumQty: postOnlyMaximum,
		PriceTick: priceTick, MinimumOrderAmount: minimumAmount,
		MaximumOrderAmount: maximumAmount, ObservedAt: observedAt,
	}
	return rules, nil
}

func (rules DemoInstrumentRules) validate() error {
	if !approvedInstrument(rules.Instrument) ||
		rules.ObservedAt.IsZero() || rules.ObservedAt.Location() != time.UTC ||
		rules.SourceHash == "" ||
		!positiveQuantity(rules.QuantityStep) ||
		!positiveQuantity(rules.MinimumQuantity) ||
		!positiveQuantity(rules.MaximumQuantity) ||
		!positiveQuantity(rules.MaximumLimitQty) ||
		!positiveQuantity(rules.PostOnlyMaximumQty) ||
		!positivePrice(rules.PriceTick) ||
		!positiveNotional(rules.MinimumOrderAmount) ||
		!positiveNotional(rules.MaximumOrderAmount) {
		return ErrDemoFilter
	}
	return nil
}

func (rules DemoInstrumentRules) validateSubmission(
	submission sandbox.Submission,
	owned domain.Balance,
) error {
	if rules.validate() != nil || submission.Instrument != rules.Instrument {
		return ErrDemoFilter
	}
	roundedQuantity, err := domain.RoundBuyQuantity(
		submission.Quantity,
		rules.QuantityStep,
	)
	if err != nil || roundedQuantity.Compare(submission.Quantity) != 0 ||
		submission.Quantity.Compare(rules.MinimumQuantity) < 0 ||
		submission.Quantity.Compare(rules.MaximumQuantity) > 0 ||
		submission.Quantity.Compare(rules.MaximumLimitQty) > 0 ||
		(submission.Style == sandbox.OrderStylePostOnly &&
			submission.Quantity.Compare(rules.PostOnlyMaximumQty) > 0) {
		return ErrDemoFilter
	}
	roundedPrice, err := domain.RoundLimitPrice(
		submission.Side,
		submission.LimitPrice,
		rules.PriceTick,
	)
	if err != nil || roundedPrice.Compare(submission.LimitPrice) != 0 {
		return ErrDemoFilter
	}
	nativeNotional, err := domain.CalculateNotional(
		submission.LimitPrice,
		submission.Quantity,
		18,
	)
	if err != nil ||
		nativeNotional.Compare(rules.MinimumOrderAmount) < 0 ||
		nativeNotional.Compare(rules.MaximumOrderAmount) > 0 ||
		(submission.Instrument.Quote == "USDT" &&
			nativeNotional.Compare(submission.Notional) != 0) {
		return ErrDemoFilter
	}
	if submission.Side == domain.SideSell {
		ownedQuantity, parseErr := domain.ParseQuantity(owned.String())
		if parseErr != nil || submission.Quantity.Compare(ownedQuantity) > 0 {
			return ErrDemoFilter
		}
	}
	return nil
}
