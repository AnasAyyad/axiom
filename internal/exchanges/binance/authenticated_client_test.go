package binance

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/sandbox"
)

type captureEvidence struct {
	records []exchangecontracts.AuthenticatedRequestEvidence
	err     error
}

func (sink *captureEvidence) RecordAuthenticatedRequest(
	_ context.Context,
	record exchangecontracts.AuthenticatedRequestEvidence,
) error {
	sink.records = append(sink.records, record)
	return sink.err
}

type authenticatedRoundTripFunc func(*http.Request) (*http.Response, error)

func (function authenticatedRoundTripFunc) Do(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestBinanceSigningGoldenAndRedactedEvidence(t *testing.T) {
	now := time.UnixMilli(1_700_000_000_000).UTC()
	evidence := &captureEvidence{}
	var captured *http.Request
	doer := authenticatedRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		captured = request
		return &http.Response{
			StatusCode: http.StatusOK,
			Body: io.NopCloser(strings.NewReader(
				`{"canTrade":true,"accountType":"SPOT","permissions":["SPOT"],"uid":12345}`,
			)),
		}, nil
	})
	credentials := sandbox.CredentialPair{APIKey: "test-key", APISecret: "test-secret"}
	client, err := newSandboxClientForTest(doer, credentials, evidence, "cfg-1", func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	identity, err := client.ValidateStartup(context.Background(), BinanceTestnetAttestation{
		AccountIdentityHash: hashString("12345|SPOT"),
		KeyFingerprint:      fingerprintString("test-key"),
		TestnetOnly:         true,
	})
	if err != nil {
		t.Fatalf("startup validation failed: %v", err)
	}
	if identity.Environment != sandbox.EnvironmentBinanceSpotTestnet {
		t.Fatalf("wrong environment: %s", identity.Environment)
	}
	query := captured.URL.Query()
	signature := query.Get("signature")
	query.Del("signature")
	mac := hmac.New(sha256.New, []byte("test-secret"))
	_, _ = mac.Write([]byte(query.Encode()))
	if signature != hex.EncodeToString(mac.Sum(nil)) {
		t.Fatalf("signature mismatch: %s", signature)
	}
	if captured.URL.Host != sandboxRESTHost || captured.URL.Path != "/api/v3/account" {
		t.Fatalf("unsafe target: %s", captured.URL)
	}
	if len(evidence.records) != 1 {
		t.Fatalf("evidence count = %d", len(evidence.records))
	}
	record := evidence.records[0]
	if strings.Contains(strings.Join(record.FieldNames, ","), "signature") ||
		strings.Contains(strings.Join(record.FieldNames, ","), "price") ||
		strings.Contains(strings.Join(record.FieldNames, ","), "quantity") {
		t.Fatalf("private evidence fields: %#v", record.FieldNames)
	}
}

func TestBinanceEvidenceFailurePreventsNetwork(t *testing.T) {
	evidence := &captureEvidence{err: errors.New("disk full")}
	called := false
	doer := authenticatedRoundTripFunc(func(*http.Request) (*http.Response, error) {
		called = true
		return nil, errors.New("unexpected")
	})
	client, err := newSandboxClientForTest(
		doer, sandbox.CredentialPair{APIKey: "key", APISecret: "secret"},
		evidence, "cfg", func() time.Time { return time.Unix(1, 0).UTC() },
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.ValidateStartup(context.Background(), BinanceTestnetAttestation{TestnetOnly: true})
	if err == nil || called {
		t.Fatalf("fail-closed evidence: err=%v network=%t", err, called)
	}
}

func TestBinanceStartupRejectsMissingIdentityAndOversizedResponse(t *testing.T) {
	testCases := []struct {
		name string
		body string
		want error
	}{
		{
			name: "missing uid",
			body: `{"canTrade":true,"accountType":"SPOT","permissions":["SPOT"]}`,
			want: ErrSandboxStartupIdentity,
		},
		{
			name: "oversized valid prefix",
			body: `{"canTrade":true,"accountType":"SPOT","permissions":["SPOT"],"uid":12345}` +
				strings.Repeat(" ", authenticatedResponseLimit+1),
			want: ErrSandboxRequest,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			client, err := newSandboxClientForTest(
				authenticatedRoundTripFunc(func(*http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader(testCase.body)),
					}, nil
				}),
				sandbox.CredentialPair{APIKey: "key", APISecret: "secret"},
				&captureEvidence{},
				"cfg",
				func() time.Time { return time.UnixMilli(1_700_000_000_000).UTC() },
			)
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.ValidateStartup(context.Background(), BinanceTestnetAttestation{
				AccountIdentityHash: hashString("12345|SPOT"),
				KeyFingerprint:      fingerprintString("key"),
				TestnetOnly:         true,
			})
			if !errors.Is(err, testCase.want) {
				t.Fatalf("error=%v want=%v", err, testCase.want)
			}
		})
	}
}

func TestBinanceAuthenticatedPolicyRejectsForbiddenCapabilities(t *testing.T) {
	base := url.Values{
		"newClientOrderId": {"axiom-0001"}, "newOrderRespType": {"ACK"},
		"price": {"1.00"}, "quantity": {"1.00"}, "recvWindow": {sandboxReceiveWindow},
		"side": {"BUY"}, "symbol": {"BTCUSDT"}, "timeInForce": {"GTC"},
		"timestamp": {"1700000000000"}, "type": {"LIMIT"},
	}
	if _, err := validateAuthenticatedFields(authenticatedCreate, base); err != nil {
		t.Fatalf("valid limit rejected: %v", err)
	}
	mutations := []func(url.Values){
		func(values url.Values) { values.Set("type", "MARKET") },
		func(values url.Values) { values.Set("timeInForce", "FOK") },
		func(values url.Values) { values.Set("withdrawAddress", "x") },
		func(values url.Values) { values.Set("quoteOrderQty", "1") },
		func(values url.Values) { values.Set("symbol", "BTCUSD_PERP") },
	}
	for index, mutate := range mutations {
		values := cloneValues(base)
		mutate(values)
		if _, err := validateAuthenticatedFields(authenticatedCreate, values); err == nil {
			t.Fatalf("forbidden mutation %d accepted: %v", index, values)
		}
	}
	if _, ok := authenticatedRoutePolicies[authenticatedRoute(255)]; ok {
		t.Fatal("unexpected generic route")
	}
}

func TestBinanceAuthenticatedRouteMatrixIsClosedAndExhaustive(t *testing.T) {
	client, err := newSandboxClientForTest(
		authenticatedRoundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("network must not be used")
		}),
		sandbox.CredentialPair{APIKey: "key", APISecret: "secret"},
		&captureEvidence{}, "cfg", func() time.Time { return time.UnixMilli(1_700_000_000_000).UTC() },
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(authenticatedRoutePolicies) != 8 {
		t.Fatalf("authenticated route count = %d", len(authenticatedRoutePolicies))
	}
	for route, policy := range authenticatedRoutePolicies {
		fields := validBinanceRouteFields(route)
		signed, buildErr := client.buildSignedRequest(route, fields)
		if buildErr != nil {
			t.Fatalf("%s %s rejected: %v", policy.method, policy.path, buildErr)
		}
		if signed.method != policy.method || signed.path != policy.path ||
			!strings.HasPrefix(signed.path, "/api/v3/") ||
			strings.Contains(signed.path, "/sapi/") ||
			strings.Contains(sandboxRESTHost, "binance.com") {
			t.Fatalf("unsafe signed route: %#v", signed)
		}
		for _, forbidden := range []string{
			"address", "batchOrders", "callbackRate", "closePosition", "leverage",
			"marginType", "positionSide", "quantityType", "reduceOnly",
			"stopPrice", "takeProfit", "withdrawAddress",
		} {
			mutated := cloneValues(fields)
			mutated.Set(forbidden, "1")
			if _, mutationErr := client.buildSignedRequest(route, mutated); mutationErr == nil {
				t.Fatalf("%s %s accepted forbidden field %s", policy.method, policy.path, forbidden)
			}
		}
	}
	for _, route := range []authenticatedRoute{0, 255} {
		if _, err = client.buildSignedRequest(route, url.Values{"timestamp": {"1"}}); err == nil {
			t.Fatalf("unknown route %d accepted", route)
		}
	}
}

func validBinanceRouteFields(route authenticatedRoute) url.Values {
	common := url.Values{
		"recvWindow": {sandboxReceiveWindow},
		"timestamp":  {"1700000000000"},
	}
	switch route {
	case authenticatedOpenOrders:
		common.Set("symbol", "BTCUSDT")
	case authenticatedOrderHistory, authenticatedFills:
		common.Set("symbol", "BTCUSDT")
	case authenticatedTestCreate, authenticatedCreate:
		common.Set("newClientOrderId", "axiom-0001")
		common.Set("newOrderRespType", "ACK")
		common.Set("price", "1.00")
		common.Set("quantity", "1.00")
		common.Set("side", "BUY")
		common.Set("symbol", "BTCUSDT")
		common.Set("timeInForce", "GTC")
		common.Set("type", "LIMIT")
	case authenticatedQuery, authenticatedCancel:
		common.Set("origClientOrderId", "axiom-0001")
		common.Set("symbol", "BTCUSDT")
	}
	return common
}

func FuzzBinanceAuthenticatedCreatePolicy(f *testing.F) {
	f.Add("newClientOrderId=ax-00000001&newOrderRespType=ACK&price=1.00&quantity=1.00&recvWindow=5000&side=BUY&symbol=BTCUSDT&timeInForce=GTC&timestamp=1700000000000&type=LIMIT")
	f.Add("recvWindow=5000&timestamp=1700000000000&type=MARKET")
	f.Fuzz(func(t *testing.T, raw string) {
		values, err := url.ParseQuery(raw)
		if err != nil {
			return
		}
		policy, err := validateAuthenticatedFields(authenticatedCreate, values)
		if err != nil {
			return
		}
		if policy.method != http.MethodPost || policy.path != "/api/v3/order" ||
			values.Get("recvWindow") != sandboxReceiveWindow ||
			(values.Get("type") != "LIMIT" && values.Get("type") != "LIMIT_MAKER") {
			t.Fatalf("unsafe accepted policy: %#v %#v", policy, values)
		}
	})
}

func cloneValues(input url.Values) url.Values {
	output := make(url.Values, len(input))
	for key, values := range input {
		output[key] = append([]string(nil), values...)
	}
	return output
}
