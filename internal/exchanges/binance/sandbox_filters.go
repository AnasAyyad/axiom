package binance

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"time"

	"axiom/internal/domain"

	"github.com/cockroachdb/apd/v3"
)

// ErrSandboxFilter is the generic fail-closed instrument-filter error.
var ErrSandboxFilter = errors.New("binance_testnet_filter_rejected")

// SandboxInstrumentRules is one exact Testnet exchangeInfo rule set. Native
// DTOs remain private to the adapter.
type SandboxInstrumentRules struct {
	Instrument          domain.Instrument
	BasePrecision       uint8
	QuotePrecision      uint8
	MinimumPrice        domain.Price
	MaximumPrice        domain.Price
	PriceTick           domain.Price
	MinimumQuantity     domain.Quantity
	MaximumQuantity     domain.Quantity
	QuantityStep        domain.Quantity
	MinimumNotional     domain.Notional
	MaximumNotional     domain.Notional
	BidMultiplierUp     string
	BidMultiplierDown   string
	AskMultiplierUp     string
	AskMultiplierDown   string
	AveragePriceMinutes uint64
	ObservedAt          time.Time
	SourceHash          string
}

type sandboxAveragePrice struct {
	Minutes          uint64
	Price            domain.Price
	ObservedAt       time.Time
	ValidatedThrough time.Time
}

// loadSandboxRules fetches exact metadata through the fixed Testnet proxy.
func (client *SandboxClient) loadSandboxRules(
	ctx context.Context,
	instrument domain.Instrument,
) (SandboxInstrumentRules, error) {
	if !validSpotInstrument(instrument) || !validSymbol(instrument.Symbol()) {
		return SandboxInstrumentRules{}, ErrSandboxFilter
	}
	body, err := client.executeUnsigned(
		ctx,
		"/api/v3/exchangeInfo",
		url.Values{"showPermissionSets": {"false"}, "symbol": {instrument.Symbol()}},
	)
	if err != nil {
		return SandboxInstrumentRules{}, err
	}
	return normalizeSandboxRules(body, instrument, client.now().UTC())
}

func (client *SandboxClient) averagePrice(
	ctx context.Context,
	instrument domain.Instrument,
) (sandboxAveragePrice, error) {
	if !validSpotInstrument(instrument) || !validSymbol(instrument.Symbol()) {
		return sandboxAveragePrice{}, ErrSandboxFilter
	}
	body, err := client.executeUnsigned(
		ctx,
		"/api/v3/avgPrice",
		url.Values{"symbol": {instrument.Symbol()}},
	)
	if err != nil {
		return sandboxAveragePrice{}, err
	}
	var native struct {
		Minutes   uint64 `json:"mins"`
		Price     string `json:"price"`
		CloseTime int64  `json:"closeTime"`
	}
	if strictDecode(body, &native) != nil || native.CloseTime <= 0 {
		return sandboxAveragePrice{}, ErrSandboxFilter
	}
	price, err := domain.ParsePrice(native.Price)
	observedAt := time.UnixMilli(native.CloseTime).UTC()
	if err != nil {
		return sandboxAveragePrice{}, ErrSandboxFilter
	}
	observedThrough, err := client.conservativeServerNowFor(ctx, observedAt)
	if err != nil {
		return sandboxAveragePrice{}, err
	}
	return sandboxAveragePrice{
		Minutes: native.Minutes, Price: price, ObservedAt: observedAt,
		ValidatedThrough: observedThrough,
	}, nil
}

func (client *SandboxClient) executeUnsigned(
	ctx context.Context,
	path string,
	query url.Values,
) ([]byte, error) {
	if !validUnsignedRoute(path, query) {
		return nil, ErrSandboxRequest
	}
	if err := client.allowSandboxRequest(); err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		sandboxRESTOrigin+path+"?"+query.Encode(),
		nil,
	)
	if err != nil {
		return nil, ErrSandboxRequest
	}
	response, err := client.doer.Do(request)
	if err != nil {
		return nil, ErrSandboxRequest
	}
	defer response.Body.Close()
	client.observeSandboxRateLimit(response)
	body, err := io.ReadAll(io.LimitReader(response.Body, authenticatedResponseLimit+1))
	if response.StatusCode == http.StatusTooManyRequests ||
		response.StatusCode == http.StatusTeapot {
		return nil, ErrSandboxRateLimited
	}
	if err != nil || len(body) > authenticatedResponseLimit ||
		response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, ErrSandboxRequest
	}
	return body, nil
}

func validUnsignedRoute(path string, query url.Values) bool {
	switch path {
	case "/api/v3/time":
		return len(query) == 0
	case "/api/v3/exchangeInfo":
		return len(query) == 2 &&
			query.Get("showPermissionSets") == "false" &&
			validSymbol(query.Get("symbol"))
	case "/api/v3/avgPrice":
		return len(query) == 1 &&
			validSymbol(query.Get("symbol"))
	case "/api/v3/depth":
		return len(query) == 2 &&
			query.Get("limit") == "5" &&
			validSymbol(query.Get("symbol"))
	default:
		return false
	}
}

func normalizeSandboxRules(
	body []byte,
	instrument domain.Instrument,
	observedAt time.Time,
) (SandboxInstrumentRules, error) {
	var native exchangeInfoPayload
	if strictDecode(body, &native) != nil || native.Timezone != "UTC" ||
		len(native.ExchangeFilter) != 0 || native.ServerTime <= 0 ||
		len(native.Symbols) != 1 || observedAt.IsZero() ||
		observedAt.Location() != time.UTC {
		return SandboxInstrumentRules{}, ErrSandboxFilter
	}
	item := native.Symbols[0]
	if item.Symbol != instrument.Symbol() || item.Status != "TRADING" ||
		!item.SpotTradingAllowed || item.MarginTradingAllowed ||
		item.BaseAsset != string(instrument.Base) ||
		item.QuoteAsset != string(instrument.Quote) ||
		item.BaseAssetPrecision > 18 || item.QuoteAssetPrecision > 18 ||
		!containsExact(item.OrderTypes, "LIMIT") ||
		!containsExact(item.OrderTypes, "LIMIT_MAKER") {
		return SandboxInstrumentRules{}, ErrSandboxFilter
	}
	rules := SandboxInstrumentRules{
		Instrument: instrument, BasePrecision: item.BaseAssetPrecision,
		QuotePrecision: item.QuoteAssetPrecision, ObservedAt: observedAt,
		SourceHash: payloadHash(body),
	}
	if err := applySandboxFilters(&rules, item.Filters); err != nil {
		return SandboxInstrumentRules{}, err
	}
	return rules, nil
}

func applySandboxFilters(
	rules *SandboxInstrumentRules,
	filters []filterPayload,
) error {
	seen := make(map[string]bool, len(filters))
	for _, filter := range filters {
		if seen[filter.Type] {
			return ErrSandboxFilter
		}
		seen[filter.Type] = true
		switch filter.Type {
		case "PRICE_FILTER":
			if parseSandboxPriceFilter(rules, filter) != nil {
				return ErrSandboxFilter
			}
		case "LOT_SIZE":
			if parseSandboxQuantityFilter(rules, filter) != nil {
				return ErrSandboxFilter
			}
		case "NOTIONAL", "MIN_NOTIONAL":
			if parseSandboxNotionalFilter(rules, filter) != nil {
				return ErrSandboxFilter
			}
		case "PERCENT_PRICE", "PERCENT_PRICE_BY_SIDE":
			if parseSandboxPercentFilter(rules, filter) != nil {
				return ErrSandboxFilter
			}
		case "ICEBERG_PARTS", "MARKET_LOT_SIZE", "TRAILING_DELTA",
			"MAX_NUM_ORDERS", "MAX_NUM_ORDER_LISTS", "MAX_NUM_ALGO_ORDERS",
			"MAX_NUM_ORDER_AMENDS":
		default:
			return ErrSandboxFilter
		}
	}
	if !seen["PRICE_FILTER"] || !seen["LOT_SIZE"] ||
		(seen["NOTIONAL"] == seen["MIN_NOTIONAL"]) ||
		(seen["PERCENT_PRICE"] == seen["PERCENT_PRICE_BY_SIDE"]) {
		return ErrSandboxFilter
	}
	return nil
}

func parseSandboxPriceFilter(
	rules *SandboxInstrumentRules,
	filter filterPayload,
) error {
	var err error
	rules.MinimumPrice, err = domain.ParsePrice(filter.MinimumPrice)
	if err != nil {
		return err
	}
	rules.MaximumPrice, err = domain.ParsePrice(filter.MaximumPrice)
	if err != nil {
		return err
	}
	rules.PriceTick, err = domain.ParsePrice(filter.TickSize)
	if err != nil || !decimalPositive(filter.TickSize) {
		return ErrSandboxFilter
	}
	return nil
}

func parseSandboxQuantityFilter(
	rules *SandboxInstrumentRules,
	filter filterPayload,
) error {
	var err error
	rules.MinimumQuantity, err = domain.ParseQuantity(filter.MinimumQty)
	if err != nil {
		return err
	}
	rules.MaximumQuantity, err = domain.ParseQuantity(filter.MaximumQty)
	if err != nil {
		return err
	}
	rules.QuantityStep, err = domain.ParseQuantity(filter.StepSize)
	if err != nil || !decimalPositive(filter.StepSize) {
		return ErrSandboxFilter
	}
	return nil
}

func parseSandboxNotionalFilter(
	rules *SandboxInstrumentRules,
	filter filterPayload,
) error {
	var err error
	rules.MinimumNotional, err = domain.ParseNotional(filter.MinimumNotional)
	if err != nil {
		return err
	}
	maximum := filter.MaximumNotional
	if maximum == "" {
		maximum = "0"
	}
	rules.MaximumNotional, err = domain.ParseNotional(maximum)
	return err
}

func parseSandboxPercentFilter(
	rules *SandboxInstrumentRules,
	filter filterPayload,
) error {
	rules.AveragePriceMinutes = filter.AveragePriceMinutes
	if filter.Type == "PERCENT_PRICE" {
		rules.BidMultiplierUp = filter.MultiplierUp
		rules.BidMultiplierDown = filter.MultiplierDown
		rules.AskMultiplierUp = filter.MultiplierUp
		rules.AskMultiplierDown = filter.MultiplierDown
	} else {
		rules.BidMultiplierUp = filter.BidMultiplierUp
		rules.BidMultiplierDown = filter.BidMultiplierDown
		rules.AskMultiplierUp = filter.AskMultiplierUp
		rules.AskMultiplierDown = filter.AskMultiplierDown
	}
	for _, value := range []string{
		rules.BidMultiplierUp, rules.BidMultiplierDown,
		rules.AskMultiplierUp, rules.AskMultiplierDown,
	} {
		if !decimalPositive(value) {
			return ErrSandboxFilter
		}
	}
	return nil
}

func decimalPositive(value string) bool {
	decimal, _, err := apd.NewFromString(value)
	return err == nil && decimal.Form == apd.Finite && decimal.Sign() > 0
}
