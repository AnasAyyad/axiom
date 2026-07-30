package bybit

import (
	"encoding/json"
	"fmt"
	"net/http"
)

func decodeDemoResult[T any](body []byte) (T, error) {
	var envelope responseEnvelope[T]
	if strictDecode(body, &envelope) != nil ||
		envelope.RetCode != 0 ||
		(envelope.RetMsg != "" && envelope.RetMsg != "OK") ||
		envelope.Time <= 0 {
		var zero T
		return zero, ErrDemoRequest
	}
	return envelope.Result, nil
}

func classifyDemoEnvelope(route authenticatedRoute, body []byte) error {
	var response demoResponseCode
	if json.Unmarshal(body, &response) != nil {
		return ErrDemoRequest
	}
	switch response.RetCode {
	case 0:
		return nil
	case 10006:
		return ErrDemoRateLimited
	case 10002:
		return ErrDemoClockRejected
	case 110001:
		if route == authenticatedQuery {
			return ErrDemoOrderNotFound
		}
		return fmt.Errorf(
			"bybit_demo_response_code_%d: %w",
			response.RetCode,
			ErrDemoRejected,
		)
	default:
		return fmt.Errorf(
			"bybit_demo_response_code_%d: %w",
			response.RetCode,
			ErrDemoRejected,
		)
	}
}

func classifyDemoResponse(
	route authenticatedRoute,
	status int,
	body []byte,
) error {
	if status == http.StatusTooManyRequests {
		return ErrDemoRateLimited
	}
	if status >= http.StatusInternalServerError && demoRouteCanChangeOrder(route) {
		return ErrDemoAmbiguous
	}
	if status >= http.StatusBadRequest &&
		status < http.StatusInternalServerError {
		return ErrDemoRejected
	}
	if err := classifyDemoEnvelope(route, body); err != nil {
		return err
	}
	return ErrDemoRequest
}

func demoRouteCanChangeOrder(route authenticatedRoute) bool {
	return route == authenticatedCreate || route == authenticatedCancel
}

func demoRouteIsReadOnly(route authenticatedRoute) bool {
	policy, ok := authenticatedRoutePolicies[route]
	return ok && policy.method == http.MethodGet
}

func validDemoKeyPermissions(permissions map[string][]string) bool {
	if !containsExactly(permissions["Spot"], "SpotTrade") {
		return false
	}
	for category, values := range permissions {
		switch category {
		case "Spot", "ContractTrade", "Options", "Derivatives":
		default:
			if len(values) != 0 {
				return false
			}
		}
	}
	spotOnly := len(permissions["ContractTrade"]) == 0 &&
		len(permissions["Options"]) == 0 &&
		len(permissions["Derivatives"]) == 0
	uiBundled := containsExactly(
		permissions["ContractTrade"],
		"Order",
		"Position",
	) &&
		containsExactly(permissions["Options"], "OptionsTrade") &&
		containsExactly(permissions["Derivatives"], "DerivativesTrade")
	return spotOnly || uiBundled
}

func containsExactly(values []string, wanted ...string) bool {
	if len(values) != len(wanted) {
		return false
	}
	actual := make(map[string]struct{}, len(values))
	for _, value := range values {
		if _, duplicate := actual[value]; duplicate {
			return false
		}
		actual[value] = struct{}{}
	}
	for _, value := range wanted {
		if _, present := actual[value]; !present {
			return false
		}
	}
	return true
}
