package exchangecontracts

import (
	"context"
	"crypto/sha256"
	"errors"
	"sort"
	"strings"
	"time"
)

// AuthenticatedRequestEvidence is the deliberately redacted record retained
// before an authenticated request may leave an engine. It never contains
// headers, credentials, signatures, or private field values.
type AuthenticatedRequestEvidence struct {
	Exchange        string
	Host            string
	Method          string
	Path            string
	FieldNames      []string
	Enumerated      map[string]string
	RequestHash     [sha256.Size]byte
	ConfigurationID string
	RecordedAt      time.Time
}

// AuthenticatedEvidenceSink must durably accept evidence before network I/O.
// A sink failure is a hard request failure.
type AuthenticatedEvidenceSink interface {
	RecordAuthenticatedRequest(context.Context, AuthenticatedRequestEvidence) error
}

// ErrAuthenticatedEvidenceRejected reports a fail-closed evidence violation.
var ErrAuthenticatedEvidenceRejected = errors.New("authenticated_request_evidence_rejected")

type authenticatedEvidencePolicy struct {
	host       string
	routes     map[string]struct{}
	fields     map[string]struct{}
	enumerated map[string]map[string]struct{}
}

var authenticatedEvidencePolicies = map[string]authenticatedEvidencePolicy{
	"binance": {
		host: "testnet.binance.vision",
		routes: stringSet(
			"GET /api/v3/account",
			"GET /api/v3/openOrders",
			"GET /api/v3/allOrders",
			"GET /api/v3/myTrades",
			"POST /api/v3/order/test",
			"POST /api/v3/order",
			"GET /api/v3/order",
			"DELETE /api/v3/order",
		),
		fields: stringSet(
			"endTime", "fromId", "limit", "newClientOrderId", "newOrderRespType",
			"orderId", "origClientOrderId", "price", "quantity", "recvWindow",
			"side", "startTime", "symbol", "timeInForce", "timestamp", "type",
		),
		enumerated: map[string]map[string]struct{}{
			"newOrderRespType": stringSet("ACK"),
			"side":             stringSet("BUY", "SELL"),
			"timeInForce":      stringSet("GTC", "IOC"),
			"type":             stringSet("LIMIT", "LIMIT_MAKER"),
		},
	},
	"bybit": {
		host: "api-demo.bybit.com",
		routes: stringSet(
			"GET /v5/user/query-api",
			"GET /v5/account/wallet-balance",
			"POST /v5/order/create",
			"POST /v5/order/cancel",
			"GET /v5/order/realtime",
			"GET /v5/order/history",
			"GET /v5/execution/list",
		),
		fields: stringSet(
			"accountType", "category", "cursor", "endTime", "isLeverage", "limit",
			"orderFilter", "orderId", "orderLinkId", "orderType", "price", "qty",
			"recvWindow", "side", "startTime", "symbol", "timeInForce", "timestamp",
		),
		enumerated: map[string]map[string]struct{}{
			"accountType": stringSet("UNIFIED"),
			"category":    stringSet("spot"),
			"isLeverage":  stringSet("0"),
			"orderFilter": stringSet("Order"),
			"orderType":   stringSet("Limit"),
			"side":        stringSet("Buy", "Sell"),
			"timeInForce": stringSet("GTC", "IOC", "PostOnly"),
		},
	},
}

// ValidateAuthenticatedRequestEvidence rejects records which could act as a
// secondary private-payload store.
func ValidateAuthenticatedRequestEvidence(record AuthenticatedRequestEvidence) error {
	policy, err := authenticatedEvidencePolicyFor(record)
	if err != nil {
		return err
	}
	if err = validateAuthenticatedEvidenceFields(record, policy); err != nil {
		return err
	}
	return validateAuthenticatedEvidenceEnumerations(record, policy)
}

func authenticatedEvidencePolicyFor(
	record AuthenticatedRequestEvidence,
) (authenticatedEvidencePolicy, error) {
	if record.Exchange == "" || record.Host == "" || record.Method == "" || record.Path == "" ||
		record.ConfigurationID == "" || len(record.ConfigurationID) > 128 ||
		record.RecordedAt.IsZero() || record.RecordedAt.Location() != time.UTC ||
		record.RequestHash == ([sha256.Size]byte{}) {
		return authenticatedEvidencePolicy{}, ErrAuthenticatedEvidenceRejected
	}
	policy, ok := authenticatedEvidencePolicies[record.Exchange]
	if !ok || record.Host != policy.host {
		return authenticatedEvidencePolicy{}, ErrAuthenticatedEvidenceRejected
	}
	if _, ok = policy.routes[record.Method+" "+record.Path]; !ok {
		return authenticatedEvidencePolicy{}, ErrAuthenticatedEvidenceRejected
	}
	return policy, nil
}

func validateAuthenticatedEvidenceFields(
	record AuthenticatedRequestEvidence,
	policy authenticatedEvidencePolicy,
) error {
	if len(record.FieldNames) == 0 || len(record.FieldNames) > 32 || len(record.Enumerated) > 8 {
		return ErrAuthenticatedEvidenceRejected
	}
	if !sort.StringsAreSorted(record.FieldNames) {
		return ErrAuthenticatedEvidenceRejected
	}
	for index, name := range record.FieldNames {
		if name == "" || (index > 0 && record.FieldNames[index-1] == name) {
			return ErrAuthenticatedEvidenceRejected
		}
		if _, ok := policy.fields[name]; !ok || sensitiveEvidenceName(name) {
			return ErrAuthenticatedEvidenceRejected
		}
	}
	return nil
}

func validateAuthenticatedEvidenceEnumerations(
	record AuthenticatedRequestEvidence,
	policy authenticatedEvidencePolicy,
) error {
	for name, value := range record.Enumerated {
		if name == "" || value == "" {
			return ErrAuthenticatedEvidenceRejected
		}
		if !containsString(record.FieldNames, name) {
			return ErrAuthenticatedEvidenceRejected
		}
		allowed, enumerated := policy.enumerated[name]
		if !enumerated {
			return ErrAuthenticatedEvidenceRejected
		}
		if _, ok := allowed[value]; !ok {
			return ErrAuthenticatedEvidenceRejected
		}
	}
	for name := range policy.enumerated {
		if containsString(record.FieldNames, name) {
			if _, ok := record.Enumerated[name]; !ok {
				return ErrAuthenticatedEvidenceRejected
			}
		}
	}
	return nil
}

func stringSet(values ...string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

func sensitiveEvidenceName(value string) bool {
	lower := strings.ToLower(value)
	for _, token := range []string{
		"apikey", "apisecret", "authorization", "cookie", "header", "secret",
		"signature", "token",
	} {
		if strings.Contains(lower, token) {
			return true
		}
	}
	return false
}

func containsString(values []string, wanted string) bool {
	index := sort.SearchStrings(values, wanted)
	return index < len(values) && values[index] == wanted
}
