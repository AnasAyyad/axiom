package binance

import (
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strconv"
)

const (
	sandboxRESTHost       = "testnet.binance.vision"
	sandboxRESTOrigin     = "https://" + sandboxRESTHost
	sandboxProxyOrigin    = "http://binance-testnet-egress:8080"
	sandboxReceiveWindow  = "5000"
	sandboxSignatureField = "signature"
)

type authenticatedRoute uint8

const (
	authenticatedAccount authenticatedRoute = iota + 1
	authenticatedOpenOrders
	authenticatedOrderHistory
	authenticatedFills
	authenticatedTestCreate
	authenticatedCreate
	authenticatedQuery
	authenticatedCancel
)

type authenticatedRoutePolicy struct {
	method       string
	path         string
	required     []string
	optional     []string
	enumerations map[string]map[string]struct{}
}

var authenticatedRoutePolicies = map[authenticatedRoute]authenticatedRoutePolicy{
	authenticatedAccount: {
		method: http.MethodGet, path: "/api/v3/account",
		required: []string{"recvWindow", "timestamp"},
	},
	authenticatedOpenOrders: {
		method: http.MethodGet, path: "/api/v3/openOrders",
		required: []string{"recvWindow", "timestamp"}, optional: []string{"symbol"},
	},
	authenticatedOrderHistory: {
		method: http.MethodGet, path: "/api/v3/allOrders",
		required: []string{"recvWindow", "symbol", "timestamp"},
		optional: []string{"endTime", "limit", "orderId", "startTime"},
	},
	authenticatedFills: {
		method: http.MethodGet, path: "/api/v3/myTrades",
		required: []string{"recvWindow", "symbol", "timestamp"},
		optional: []string{"endTime", "fromId", "limit", "orderId", "startTime"},
	},
	authenticatedTestCreate: orderCreatePolicy("/api/v3/order/test"),
	authenticatedCreate:     orderCreatePolicy("/api/v3/order"),
	authenticatedQuery: {
		method: http.MethodGet, path: "/api/v3/order",
		required: []string{"origClientOrderId", "recvWindow", "symbol", "timestamp"},
	},
	authenticatedCancel: {
		method: http.MethodDelete, path: "/api/v3/order",
		required: []string{"origClientOrderId", "recvWindow", "symbol", "timestamp"},
		optional: []string{"newClientOrderId"},
	},
}

func orderCreatePolicy(path string) authenticatedRoutePolicy {
	return authenticatedRoutePolicy{
		method: http.MethodPost, path: path,
		required: []string{
			"newClientOrderId", "newOrderRespType", "quantity", "recvWindow",
			"side", "symbol", "timestamp", "type",
		},
		optional: []string{"price", "timeInForce"},
		enumerations: map[string]map[string]struct{}{
			"newOrderRespType": setOf("ACK"),
			"side":             setOf("BUY", "SELL"),
			"timeInForce":      setOf("GTC", "IOC"),
			"type":             setOf("LIMIT", "LIMIT_MAKER"),
		},
	}
}

func setOf(values ...string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

var errAuthenticatedPolicy = errors.New("binance_authenticated_policy_rejected")

func validateAuthenticatedFields(route authenticatedRoute, fields url.Values) (authenticatedRoutePolicy, error) {
	policy, ok := authenticatedRoutePolicies[route]
	if !ok || len(fields) == 0 {
		return authenticatedRoutePolicy{}, errAuthenticatedPolicy
	}
	allowed := make(map[string]struct{}, len(policy.required)+len(policy.optional))
	for _, name := range policy.required {
		allowed[name] = struct{}{}
		if len(fields[name]) != 1 || fields.Get(name) == "" {
			return authenticatedRoutePolicy{}, errAuthenticatedPolicy
		}
	}
	for _, name := range policy.optional {
		allowed[name] = struct{}{}
	}
	for name, values := range fields {
		if _, ok := allowed[name]; !ok || len(values) != 1 || values[0] == "" {
			return authenticatedRoutePolicy{}, errAuthenticatedPolicy
		}
		if allowedValues, enumerated := policy.enumerations[name]; enumerated {
			if _, ok := allowedValues[values[0]]; !ok {
				return authenticatedRoutePolicy{}, errAuthenticatedPolicy
			}
		}
	}
	if err := validateAuthenticatedSemantics(route, fields); err != nil {
		return authenticatedRoutePolicy{}, err
	}
	return policy, nil
}

func validateAuthenticatedSemantics(route authenticatedRoute, fields url.Values) error {
	if fields.Get("recvWindow") != sandboxReceiveWindow || !validPositiveInteger(fields.Get("timestamp")) {
		return errAuthenticatedPolicy
	}
	if symbol := fields.Get("symbol"); symbol != "" && !validSymbol(symbol) {
		return errAuthenticatedPolicy
	}
	if route == authenticatedTestCreate || route == authenticatedCreate {
		orderType := fields.Get("type")
		timeInForce := fields.Get("timeInForce")
		price := fields.Get("price")
		switch orderType {
		case "LIMIT":
			if (timeInForce != "GTC" && timeInForce != "IOC") || price == "" {
				return errAuthenticatedPolicy
			}
		case "LIMIT_MAKER":
			if timeInForce != "" || price == "" {
				return errAuthenticatedPolicy
			}
		default:
			return errAuthenticatedPolicy
		}
		if !validDecimal(fields.Get("quantity")) || !validDecimal(price) ||
			!validClientOrderID(fields.Get("newClientOrderId")) {
			return errAuthenticatedPolicy
		}
	}
	return nil
}

func validPositiveInteger(value string) bool {
	parsed, err := strconv.ParseUint(value, 10, 64)
	return err == nil && parsed != 0 && strconv.FormatUint(parsed, 10) == value
}

func validDecimal(value string) bool {
	if value == "" || value[0] == '-' || value[0] == '+' {
		return false
	}
	dot := false
	digits := 0
	for _, char := range value {
		switch {
		case char == '.' && !dot:
			dot = true
		case char >= '0' && char <= '9':
			digits++
		default:
			return false
		}
	}
	return digits > 0
}

func validClientOrderID(value string) bool {
	if len(value) < 8 || len(value) > 36 {
		return false
	}
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') || char == '-' || char == '_' {
			continue
		}
		return false
	}
	return true
}

func sortedFieldNames(fields url.Values) []string {
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
