package bybit

import (
	"errors"
	"net/http"
	"net/url"
	"sort"
)

const (
	demoRESTHost      = "api-demo.bybit.com"
	demoRESTOrigin    = "https://" + demoRESTHost
	demoProxyOrigin   = "http://bybit-demo-egress:8080"
	demoReceiveWindow = "5000"
)

type authenticatedRoute uint8

const (
	authenticatedKeyInspection authenticatedRoute = iota + 1
	authenticatedWalletBalance
	authenticatedCreate
	authenticatedCancel
	authenticatedQuery
	authenticatedOrderHistory
	authenticatedExecutionHistory
)

type authenticatedRoutePolicy struct {
	method       string
	path         string
	required     []string
	optional     []string
	enumerations map[string]map[string]struct{}
}

var authenticatedRoutePolicies = map[authenticatedRoute]authenticatedRoutePolicy{
	authenticatedKeyInspection: {
		method: http.MethodGet, path: "/v5/user/query-api",
	},
	authenticatedWalletBalance: {
		method: http.MethodGet, path: "/v5/account/wallet-balance",
		required:     []string{"accountType"},
		enumerations: map[string]map[string]struct{}{"accountType": setOf("UNIFIED")},
	},
	authenticatedCreate: {
		method: http.MethodPost, path: "/v5/order/create",
		required: []string{
			"category", "isLeverage", "orderFilter", "orderLinkId", "orderType",
			"qty", "side", "symbol", "timeInForce",
		},
		optional: []string{"price"},
		enumerations: map[string]map[string]struct{}{
			"category":    setOf("spot"),
			"isLeverage":  setOf("0"),
			"orderFilter": setOf("Order"),
			"orderType":   setOf("Limit"),
			"side":        setOf("Buy", "Sell"),
			"timeInForce": setOf("GTC", "IOC", "PostOnly"),
		},
	},
	authenticatedCancel: {
		method: http.MethodPost, path: "/v5/order/cancel",
		required: []string{"category", "orderFilter", "orderLinkId", "symbol"},
		enumerations: map[string]map[string]struct{}{
			"category": setOf("spot"), "orderFilter": setOf("Order"),
		},
	},
	authenticatedQuery: {
		method: http.MethodGet, path: "/v5/order/realtime",
		required: []string{"category", "orderFilter", "orderLinkId"},
		optional: []string{"symbol"},
		enumerations: map[string]map[string]struct{}{
			"category": setOf("spot"), "orderFilter": setOf("Order"),
		},
	},
	authenticatedOrderHistory: {
		method: http.MethodGet, path: "/v5/order/history",
		required: []string{"category", "orderFilter"},
		optional: []string{
			"cursor", "endTime", "limit", "orderLinkId", "startTime", "symbol",
		},
		enumerations: map[string]map[string]struct{}{
			"category": setOf("spot"), "orderFilter": setOf("Order"),
		},
	},
	authenticatedExecutionHistory: {
		method: http.MethodGet, path: "/v5/execution/list",
		required:     []string{"category"},
		optional:     []string{"cursor", "endTime", "limit", "orderId", "orderLinkId", "startTime", "symbol"},
		enumerations: map[string]map[string]struct{}{"category": setOf("spot")},
	},
}

func setOf(values ...string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

var errAuthenticatedPolicy = errors.New("bybit_authenticated_policy_rejected")

func validateAuthenticatedFields(route authenticatedRoute, fields url.Values) (authenticatedRoutePolicy, error) {
	policy, ok := authenticatedRoutePolicies[route]
	if !ok {
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
		if accepted, enumerated := policy.enumerations[name]; enumerated {
			if _, ok := accepted[values[0]]; !ok {
				return authenticatedRoutePolicy{}, errAuthenticatedPolicy
			}
		}
	}
	if route == authenticatedCreate {
		if !validSymbol(fields.Get("symbol")) || !validDecimal(fields.Get("qty")) ||
			!validClientOrderID(fields.Get("orderLinkId")) {
			return authenticatedRoutePolicy{}, errAuthenticatedPolicy
		}
		if fields.Get("price") == "" || !validDecimal(fields.Get("price")) {
			return authenticatedRoutePolicy{}, errAuthenticatedPolicy
		}
	}
	if symbol := fields.Get("symbol"); symbol != "" && !validSymbol(symbol) {
		return authenticatedRoutePolicy{}, errAuthenticatedPolicy
	}
	if clientOrderID := fields.Get("orderLinkId"); clientOrderID != "" &&
		!validClientOrderID(clientOrderID) {
		return authenticatedRoutePolicy{}, errAuthenticatedPolicy
	}
	return policy, nil
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

func sortedAuthenticatedFieldNames(fields url.Values) []string {
	names := make([]string, 0, len(fields)+2)
	for name := range fields {
		names = append(names, name)
	}
	names = append(names, "recvWindow", "timestamp")
	sort.Strings(names)
	return names
}
