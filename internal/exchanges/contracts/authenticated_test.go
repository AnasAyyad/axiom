package exchangecontracts

import (
	"crypto/sha256"
	"sort"
	"testing"
	"time"
)

func TestAuthenticatedEvidenceOnlyAcceptsRedactedShape(t *testing.T) {
	record := AuthenticatedRequestEvidence{
		Exchange:   "binance",
		Host:       "testnet.binance.vision",
		Method:     "POST",
		Path:       "/api/v3/order",
		FieldNames: []string{"newOrderRespType", "side", "symbol", "timestamp", "type"},
		Enumerated: map[string]string{
			"newOrderRespType": "ACK",
			"side":             "BUY",
			"type":             "LIMIT_MAKER",
		},
		RequestHash:     sha256.Sum256([]byte("private request")),
		ConfigurationID: "cfg",
		RecordedAt:      time.Unix(1, 0).UTC(),
	}
	if err := ValidateAuthenticatedRequestEvidence(record); err != nil {
		t.Fatalf("valid evidence rejected: %v", err)
	}
	record.FieldNames = append(record.FieldNames, "apiSecret")
	if err := ValidateAuthenticatedRequestEvidence(record); err == nil {
		t.Fatal("unsorted field list was accepted")
	}
}

func TestAuthenticatedEvidenceRejectsDestinationRouteFieldAndEnumerationDrift(t *testing.T) {
	base := AuthenticatedRequestEvidence{
		Exchange:        "bybit",
		Host:            "api-demo.bybit.com",
		Method:          "POST",
		Path:            "/v5/order/create",
		FieldNames:      []string{"category", "isLeverage", "orderFilter", "orderLinkId", "orderType", "price", "qty", "side", "symbol", "timeInForce"},
		Enumerated:      map[string]string{"category": "spot", "isLeverage": "0", "orderFilter": "Order", "orderType": "Limit", "side": "Buy", "timeInForce": "GTC"},
		RequestHash:     sha256.Sum256([]byte("shape")),
		ConfigurationID: "cfg",
		RecordedAt:      time.Unix(1, 0).UTC(),
	}
	sort.Strings(base.FieldNames)
	if err := ValidateAuthenticatedRequestEvidence(base); err != nil {
		t.Fatalf("valid evidence rejected: %v", err)
	}
	tests := []func(*AuthenticatedRequestEvidence){
		func(value *AuthenticatedRequestEvidence) { value.Host = "api.bybit.com" },
		func(value *AuthenticatedRequestEvidence) { value.Path = "/v5/asset/withdraw/create" },
		func(value *AuthenticatedRequestEvidence) {
			value.FieldNames = append(value.FieldNames, "takeProfit")
			sort.Strings(value.FieldNames)
		},
		func(value *AuthenticatedRequestEvidence) { value.Enumerated["category"] = "linear" },
		func(value *AuthenticatedRequestEvidence) { delete(value.Enumerated, "category") },
		func(value *AuthenticatedRequestEvidence) {
			value.FieldNames = append(value.FieldNames, "apiSecret")
			sort.Strings(value.FieldNames)
		},
		func(value *AuthenticatedRequestEvidence) {
			value.RecordedAt = value.RecordedAt.In(time.FixedZone("not-utc", 3600))
		},
	}
	for index, mutate := range tests {
		candidate := base
		candidate.FieldNames = append([]string(nil), base.FieldNames...)
		candidate.Enumerated = make(map[string]string, len(base.Enumerated))
		for name, value := range base.Enumerated {
			candidate.Enumerated[name] = value
		}
		mutate(&candidate)
		if err := ValidateAuthenticatedRequestEvidence(candidate); err == nil {
			t.Fatalf("unsafe evidence mutation %d accepted: %#v", index, candidate)
		}
	}
}

func TestAuthenticatedEvidenceBindsBinancePrivateStreamToTestnetHost(t *testing.T) {
	record := AuthenticatedRequestEvidence{
		Exchange:        "binance",
		Host:            "ws-api.testnet.binance.vision",
		Method:          "WS",
		Path:            "/ws-api/v3/userDataStream.subscribe.signature",
		FieldNames:      []string{"recvWindow", "timestamp"},
		Enumerated:      map[string]string{},
		RequestHash:     sha256.Sum256([]byte("stream subscription shape")),
		ConfigurationID: "cfg",
		RecordedAt:      time.Unix(1, 0).UTC(),
	}
	if err := ValidateAuthenticatedRequestEvidence(record); err != nil {
		t.Fatalf("valid stream evidence rejected: %v", err)
	}
	for _, host := range []string{
		"stream.binance.com",
		"ws-api.binance.com",
		"testnet.binance.vision",
	} {
		candidate := record
		candidate.Host = host
		if err := ValidateAuthenticatedRequestEvidence(candidate); err == nil {
			t.Fatalf("unsafe stream host accepted: %s", host)
		}
	}
}

func TestAuthenticatedEvidenceBindsBybitPrivateStreamToDemoHost(t *testing.T) {
	record := AuthenticatedRequestEvidence{
		Exchange:        "bybit",
		Host:            "stream-demo.bybit.com",
		Method:          "WS",
		Path:            "/v5/private/auth",
		FieldNames:      []string{"timestamp"},
		Enumerated:      map[string]string{},
		RequestHash:     sha256.Sum256([]byte("demo private auth shape")),
		ConfigurationID: "cfg",
		RecordedAt:      time.Unix(1, 0).UTC(),
	}
	if err := ValidateAuthenticatedRequestEvidence(record); err != nil {
		t.Fatalf("valid demo stream evidence rejected: %v", err)
	}
	for _, host := range []string{
		"stream.bybit.com",
		"stream-testnet.bybit.com",
		"api-demo.bybit.com",
	} {
		candidate := record
		candidate.Host = host
		if err := ValidateAuthenticatedRequestEvidence(candidate); err == nil {
			t.Fatalf("unsafe stream host accepted: %s", host)
		}
	}
}
