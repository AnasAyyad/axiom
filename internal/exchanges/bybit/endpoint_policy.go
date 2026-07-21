package bybit

import (
	"net/http"
	"net/url"
	"strconv"

	exchangecontracts "axiom/internal/exchanges/contracts"
)

const (
	publicEndpointSet = "bybit-public-v1"
	publicRESTOrigin  = "https://api.bybit.com"
	publicWSOrigin    = "wss://stream.bybit.com/v5/public/spot"
)

type publicRoute string

const (
	routeTime       publicRoute = "public_time"
	routeMetadata   publicRoute = "public_metadata"
	routeDepth      publicRoute = "public_depth"
	routeTrades     publicRoute = "public_trades"
	routeCandles    publicRoute = "public_candles"
	routeTickers    publicRoute = "public_tickers"
	routeSpotStream publicRoute = "public_spot_stream"
)

var publicRESTPaths = map[string]publicRoute{
	"/v5/market/time":             routeTime,
	"/v5/market/instruments-info": routeMetadata,
	"/v5/market/orderbook":        routeDepth,
	"/v5/market/recent-trade":     routeTrades,
	"/v5/market/kline":            routeCandles,
	"/v5/market/tickers":          routeTickers,
}

var deniedHeaders = []string{
	"Authorization", "Cookie", "X-Bapi-Api-Key", "X-Bapi-Sign", "X-Bapi-Timestamp", "X-Bapi-Recv-Window",
}

func validateRESTTarget(method string, target *url.URL, headers http.Header) (publicRoute, error) {
	if method != http.MethodGet || target == nil || target.Scheme != "https" ||
		target.Hostname() != "api.bybit.com" || (target.Port() != "" && target.Port() != "443") ||
		target.User != nil || target.Fragment != "" || target.RawPath != "" || target.Path != target.EscapedPath() {
		return "", policyError(exchangecontracts.OperationCapability)
	}
	for name, values := range headers {
		canonical := http.CanonicalHeaderKey(name)
		for _, denied := range deniedHeaders {
			if canonical == http.CanonicalHeaderKey(denied) && len(values) != 0 {
				return "", policyError(exchangecontracts.OperationCapability)
			}
		}
	}
	route, ok := publicRESTPaths[target.Path]
	if !ok || !validRouteQuery(route, target.Query()) {
		return "", policyError(exchangecontracts.OperationCapability)
	}
	return route, nil
}

func validRouteQuery(route publicRoute, query url.Values) bool {
	for _, values := range query {
		if len(values) != 1 || values[0] == "" {
			return false
		}
	}
	switch route {
	case routeTime:
		return len(query) == 0
	case routeMetadata, routeTickers:
		return exactKeys(query, "category", "symbol") && spotSymbolQuery(query)
	case routeDepth:
		return exactKeys(query, "category", "limit", "symbol") && spotSymbolQuery(query) &&
			oneOf(query.Get("limit"), "1", "50", "200", "1000")
	case routeTrades:
		return exactKeys(query, "category", "limit", "symbol") && spotSymbolQuery(query) &&
			boundedUint(query.Get("limit"), 1, 1000)
	case routeCandles:
		return exactKeys(query, "category", "end", "interval", "limit", "start", "symbol") &&
			spotSymbolQuery(query) && oneOf(query.Get("interval"), "15", "60", "240") &&
			boundedUint(query.Get("limit"), 1, 1000) && boundedUint(query.Get("start"), 1, 1<<62) &&
			boundedUint(query.Get("end"), 1, 1<<62) && validRange(query.Get("start"), query.Get("end"))
	default:
		return false
	}
}

func validateWebSocketTarget(target *url.URL) (publicRoute, error) {
	if target == nil || target.Scheme != "wss" || target.Hostname() != "stream.bybit.com" ||
		(target.Port() != "" && target.Port() != "443") || target.User != nil || target.Fragment != "" ||
		target.RawPath != "" || target.Path != "/v5/public/spot" || target.RawQuery != "" {
		return "", policyError(exchangecontracts.OperationStream)
	}
	return routeSpotStream, nil
}

func spotSymbolQuery(query url.Values) bool {
	return query.Get("category") == "spot" && validSymbol(query.Get("symbol"))
}

func validSymbol(symbol string) bool {
	return symbol == "BTCUSDT" || symbol == "ETHUSDT" || symbol == "ETHBTC"
}

func exactKeys(query url.Values, keys ...string) bool {
	if len(query) != len(keys) {
		return false
	}
	for _, key := range keys {
		if _, ok := query[key]; !ok {
			return false
		}
	}
	return true
}

func oneOf(value string, values ...string) bool {
	for _, candidate := range values {
		if value == candidate {
			return true
		}
	}
	return false
}

func boundedUint(value string, minimum, maximum uint64) bool {
	parsed, err := strconv.ParseUint(value, 10, 64)
	return err == nil && parsed >= minimum && parsed <= maximum && strconv.FormatUint(parsed, 10) == value
}

func validRange(start, end string) bool {
	left, leftErr := strconv.ParseUint(start, 10, 64)
	right, rightErr := strconv.ParseUint(end, 10, 64)
	return leftErr == nil && rightErr == nil && left <= right
}

func policyError(operation exchangecontracts.Operation) error {
	return exchangecontracts.NewError(exchangecontracts.ErrorCapability, operation, 0)
}
