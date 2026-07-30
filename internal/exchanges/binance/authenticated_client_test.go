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
	"strconv"
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
			Body: io.NopCloser(strings.NewReader(strings.Replace(
				sandboxAccountJSON,
				`"canWithdraw":false`,
				`"canWithdraw":true`,
				1,
			))),
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
	assertBinanceSignedAccountRequest(t, captured)
	assertBinanceRedactedEvidence(t, evidence)
}

func assertBinanceSignedAccountRequest(
	t *testing.T,
	captured *http.Request,
) {
	t.Helper()
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
}

func assertBinanceRedactedEvidence(
	t *testing.T,
	evidence *captureEvidence,
) {
	t.Helper()
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

func TestBinanceOfficialHMACSigningGolden(t *testing.T) {
	const payload = "symbol=LTCBTC&side=BUY&type=LIMIT&timeInForce=GTC&" +
		"quantity=1&price=0.1&recvWindow=5000&timestamp=1499827319559"
	const documentedSigningKey = "NhqPtmdSJYdKjVHjA7PZj4Mge3R5YNiP1e3UZjInClVN65XAbvqqM6A7H5fATj0j"
	const expected = "c8db56825ae71d6d79447849e617115f4a920fa2acdcab2b053c4b2838bd6b71"
	actual, err := hmacSHA256Hex(documentedSigningKey, payload)
	if err != nil {
		t.Fatal(err)
	}
	if actual != expected {
		t.Fatalf("official HMAC golden mismatch: %s", actual)
	}
}

func TestBinanceCreateSerializationGolden(t *testing.T) {
	client, err := newSandboxClientForTest(
		authenticatedRoundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("network not expected")
		}),
		sandbox.CredentialPair{APIKey: "test-key", APISecret: "test-secret"},
		&captureEvidence{},
		"cfg",
		func() time.Time { return time.UnixMilli(1_700_000_000_000).UTC() },
	)
	if err != nil {
		t.Fatal(err)
	}
	fields := url.Values{
		"newClientOrderId": {"ax-00000001"},
		"newOrderRespType": {"ACK"},
		"price":            {"100"},
		"quantity":         {"0.1"},
		"recvWindow":       {"5000"},
		"side":             {"BUY"},
		"symbol":           {"BTCUSDT"},
		"timeInForce":      {"GTC"},
		"timestamp":        {"1700000000000"},
		"type":             {"LIMIT"},
	}
	signed, err := client.buildSignedRequest(authenticatedCreate, fields)
	if err != nil {
		t.Fatal(err)
	}
	const expected = "newClientOrderId=ax-00000001&newOrderRespType=ACK&" +
		"price=100&quantity=0.1&recvWindow=5000&side=BUY&symbol=BTCUSDT&" +
		"timeInForce=GTC&timestamp=1700000000000&type=LIMIT&" +
		"signature=3aecb53fa8e8310ce07db7a6af459c90755f75486b74720c0f4a8ad3d0012baa"
	if signed.method != http.MethodPost ||
		signed.path != "/api/v3/order" ||
		signed.query != expected {
		t.Fatalf("serialized create drifted: %#v", signed)
	}
}

func TestBinanceRetryAfterBlocksFurtherRequests(t *testing.T) {
	now := time.UnixMilli(1_700_000_000_000).UTC()
	requests := 0
	client, err := newSandboxClientForTest(
		authenticatedRoundTripFunc(func(*http.Request) (*http.Response, error) {
			requests++
			if requests == 1 {
				return &http.Response{
					StatusCode: http.StatusTooManyRequests,
					Header:     http.Header{"Retry-After": []string{"2"}},
					Body:       io.NopCloser(strings.NewReader(`{"code":-1003}`)),
				}, nil
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{}`)),
			}, nil
		}),
		sandbox.CredentialPair{APIKey: "key", APISecret: "secret"},
		&captureEvidence{},
		"cfg",
		func() time.Time { return now },
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.account(context.Background()); !errors.Is(err, ErrSandboxRateLimited) {
		t.Fatalf("first request error=%v", err)
	}
	if _, err = client.account(context.Background()); !errors.Is(err, ErrSandboxRateLimited) ||
		requests != 1 {
		t.Fatalf("retry was not locally blocked: requests=%d err=%v", requests, err)
	}
	now = now.Add(2 * time.Second)
	if _, err = client.account(context.Background()); err != nil || requests != 2 {
		t.Fatalf("request did not resume: requests=%d err=%v", requests, err)
	}
}

func TestBinanceReadOnlyTimestampRejectionResynchronizesOnce(t *testing.T) {
	now := time.UnixMilli(1_700_000_000_000).UTC()
	var paths []string
	accountRequests := 0
	evidence := &captureEvidence{}
	client, err := newSandboxClientForTest(
		authenticatedRoundTripFunc(func(request *http.Request) (*http.Response, error) {
			paths = append(paths, request.URL.Path)
			if request.URL.Path == "/api/v3/time" {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body: io.NopCloser(strings.NewReader(
						`{"serverTime":1700000000000}`,
					)),
				}, nil
			}
			accountRequests++
			if accountRequests == 1 {
				return &http.Response{
					StatusCode: http.StatusBadRequest,
					Body:       io.NopCloser(strings.NewReader(`{"code":-1021}`)),
				}, nil
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(sandboxAccountJSON)),
			}, nil
		}),
		sandbox.CredentialPair{APIKey: "key", APISecret: "secret"},
		evidence,
		"cfg",
		func() time.Time { return now },
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.account(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertBinanceTimestampResynchronization(t, paths, accountRequests, evidence)
}

func assertBinanceTimestampResynchronization(
	t *testing.T,
	paths []string,
	accountRequests int,
	evidence *captureEvidence,
) {
	t.Helper()
	if strings.Join(paths, ",") !=
		"/api/v3/account,/api/v3/time,/api/v3/account" {
		t.Fatalf("timestamp retry sequence=%v", paths)
	}
	if accountRequests != 2 || len(evidence.records) != 2 {
		t.Fatalf(
			"account requests=%d evidence=%d",
			accountRequests,
			len(evidence.records),
		)
	}
}

func TestBinanceTimestampRetryIsBoundedAndNeverRetriesOrderMutation(t *testing.T) {
	t.Run("read only stops after one retry", func(t *testing.T) {
		assertBinanceTimestampRetryBound(t, authenticatedAccount, 3)
	})
	t.Run("order mutation is not retried", func(t *testing.T) {
		assertBinanceTimestampRetryBound(t, authenticatedCreate, 1)
	})
}

func assertBinanceTimestampRetryBound(
	t *testing.T,
	route authenticatedRoute,
	wantedRequests int,
) {
	t.Helper()
	now := time.UnixMilli(1_700_000_000_000).UTC()
	requests := 0
	client, err := newSandboxClientForTest(
		authenticatedRoundTripFunc(func(request *http.Request) (*http.Response, error) {
			requests++
			if request.URL.Path == "/api/v3/time" {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body: io.NopCloser(strings.NewReader(
						`{"serverTime":1700000000000}`,
					)),
				}, nil
			}
			return &http.Response{
				StatusCode: http.StatusBadRequest,
				Body:       io.NopCloser(strings.NewReader(`{"code":-1021}`)),
			}, nil
		}),
		sandbox.CredentialPair{APIKey: "key", APISecret: "secret"},
		&captureEvidence{},
		"cfg",
		func() time.Time { return now },
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.execute(
		context.Background(),
		route,
		validBinanceRouteFields(route),
	); !errors.Is(err, ErrSandboxTimestamp) {
		t.Fatalf("error=%v want timestamp rejection", err)
	}
	if requests != wantedRequests {
		t.Fatalf("requests=%d want=%d", requests, wantedRequests)
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
			body: strings.Replace(sandboxAccountJSON, `,"uid":12345`, "", 1),
			want: ErrSandboxStartupIdentity,
		},
		{
			name: "oversized valid prefix",
			body: sandboxAccountJSON + strings.Repeat(" ", authenticatedResponseLimit+1),
			want: ErrSandboxRequest,
		},
		{
			name: "extra account permission",
			body: strings.Replace(
				sandboxAccountJSON,
				`"permissions":["SPOT"]`,
				`"permissions":["SPOT","MARGIN"]`,
				1,
			),
			want: ErrSandboxStartupIdentity,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assertBinanceStartupRejection(t, testCase.body, testCase.want)
		})
	}
}

func assertBinanceStartupRejection(
	t *testing.T,
	body string,
	wanted error,
) {
	t.Helper()
	client, err := newSandboxClientForTest(
		authenticatedRoundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(body)),
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
	if !errors.Is(err, wanted) {
		t.Fatalf("error=%v want=%v", err, wanted)
	}
}

func TestBinanceAuthenticatedRequestsFailClosedUntilClockSynchronizes(t *testing.T) {
	t.Run("synchronized clock signs with server time", func(t *testing.T) {
		assertBinanceClockSynchronization(t)
	})
	t.Run("cold transport retries within unchanged uncertainty limit", func(t *testing.T) {
		assertBinanceClockWarmupRetries(t)
	})
	t.Run("transient clock transport errors retry", func(t *testing.T) {
		assertBinanceClockTransportRetries(t)
	})
	t.Run("invalid clock stops signed I/O", func(t *testing.T) {
		assertBinanceClockFailureStopsSignedIO(t)
	})
}

func assertBinanceClockSynchronization(t *testing.T) {
	t.Helper()
	now := time.UnixMilli(1_700_000_000_000).UTC()
	var requests []*http.Request
	doer := authenticatedRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests = append(requests, request)
		if request.URL.Path == "/api/v3/time" {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(strings.NewReader(
					`{"serverTime":1700000001000}`,
				)),
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(sandboxAccountJSON)),
		}, nil
	})
	client := newClockTestClient(t, now, doer)
	client.clockValidated = false
	if _, err := client.ValidateStartup(
		context.Background(),
		BinanceTestnetAttestation{
			AccountIdentityHash: hashString("12345|SPOT"),
			KeyFingerprint:      fingerprintString("key"),
			TestnetOnly:         true,
		},
	); err != nil {
		t.Fatal(err)
	}
	if len(requests) != 2 ||
		requests[0].URL.Path != "/api/v3/time" ||
		requests[0].Header.Get("X-MBX-APIKEY") != "" ||
		requests[1].URL.Query().Get("timestamp") != "1700000000000" {
		t.Fatalf("unsafe clock sequence: %#v", requests)
	}
}

func assertBinanceClockWarmupRetries(t *testing.T) {
	t.Helper()
	base := time.UnixMilli(1_700_000_000_000).UTC()
	current := base
	now := func() time.Time { return current }
	requests := 0
	client, err := newSandboxClientForTest(
		authenticatedRoundTripFunc(func(*http.Request) (*http.Response, error) {
			requests++
			serverAt := base.Add(400 * time.Millisecond)
			current = base.Add(800 * time.Millisecond)
			if requests == 2 {
				current = base.Add(1100 * time.Millisecond)
				serverAt = base.Add(950 * time.Millisecond)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(strings.NewReader(
					`{"serverTime":` +
						strconv.FormatInt(serverAt.UnixMilli(), 10) + `}`,
				)),
			}, nil
		}),
		sandbox.CredentialPair{APIKey: "key", APISecret: "secret"},
		&captureEvidence{},
		"cfg",
		now,
	)
	if err != nil {
		t.Fatal(err)
	}
	client.clockValidated = false
	if err = client.ensureClock(context.Background()); err != nil {
		t.Fatalf("clock warmup failed: %v", err)
	}
	if requests != 2 || !client.clockValidated ||
		client.clock.Health().Uncertainty > 250*time.Millisecond {
		t.Fatalf(
			"clock warmup requests=%d validated=%t health=%#v",
			requests,
			client.clockValidated,
			client.clock.Health(),
		)
	}
}

func assertBinanceClockTransportRetries(t *testing.T) {
	t.Helper()
	now := time.UnixMilli(1_700_000_000_000).UTC()
	requests := 0
	client := newClockTestClient(t, now, authenticatedRoundTripFunc(
		func(*http.Request) (*http.Response, error) {
			requests++
			if requests < 4 {
				return nil, errors.New("temporary clock transport failure")
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(strings.NewReader(
					`{"serverTime":1700000000000}`,
				)),
			}, nil
		},
	))
	client.clockValidated = false
	if err := client.ensureClock(context.Background()); err != nil ||
		requests != 4 || !client.clockValidated {
		t.Fatalf(
			"transport retries=%d validated=%t error=%v",
			requests,
			client.clockValidated,
			err,
		)
	}
}

func assertBinanceClockFailureStopsSignedIO(t *testing.T) {
	t.Helper()
	now := time.UnixMilli(1_700_000_000_000).UTC()
	var requests []*http.Request
	client := newClockTestClient(t, now, authenticatedRoundTripFunc(
		func(request *http.Request) (*http.Response, error) {
			requests = append(requests, request)
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"serverTime":0}`)),
			}, nil
		},
	))
	client.clockValidated = false
	if _, err := client.ValidateStartup(
		context.Background(),
		BinanceTestnetAttestation{TestnetOnly: true},
	); !errors.Is(err, ErrSandboxRequest) ||
		len(requests) != sandboxClockWarmupAttempts ||
		requests[0].Header.Get("X-MBX-APIKEY") != "" {
		t.Fatalf("clock failure did not stop signed I/O: err=%v requests=%d", err, len(requests))
	}
}

func newClockTestClient(
	t *testing.T,
	now time.Time,
	doer sandboxDoer,
) *SandboxClient {
	t.Helper()
	client, err := newSandboxClientForTest(
		doer,
		sandbox.CredentialPair{APIKey: "key", APISecret: "secret"},
		&captureEvidence{},
		"cfg",
		func() time.Time { return now },
	)
	if err != nil {
		t.Fatal(err)
	}
	return client
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
