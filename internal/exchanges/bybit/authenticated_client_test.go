package bybit

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

type idleClosingDemoDoer struct {
	requests int
	closed   int
}

func (doer *idleClosingDemoDoer) Do(*http.Request) (*http.Response, error) {
	doer.requests++
	if doer.requests == 1 {
		return nil, errors.New("stale idle connection")
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(
			demoEnvelope(`{"list":[]}`),
		)),
	}, nil
}

func (doer *idleClosingDemoDoer) CloseIdleConnections() {
	doer.closed++
}

func TestBybitSigningGoldenAndPermissionValidation(t *testing.T) {
	now := time.UnixMilli(1_700_000_000_000).UTC()
	evidence := &captureEvidence{}
	var captured *http.Request
	doer := authenticatedRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		captured = request
		return &http.Response{
			StatusCode: http.StatusOK,
			Body: io.NopCloser(strings.NewReader(
				demoEnvelopeWithMessage("", `{"id":"demo-account","note":"","apiKey":"demo-key","readOnly":0,"secret":"","permissions":{"ContractTrade":[],"Spot":["SpotTrade"],"Wallet":[],"Options":[],"Derivatives":[]},"ips":[],"type":1,"deadlineDay":0,"expiredAt":"","createdAt":"","uta":1,"userID":12345,"inviterID":0,"vipLevel":"","mktMakerLevel":"","affiliateID":0,"rsaPublicKey":"","isMaster":true,"parentUid":"","kycLevel":"","kycRegion":"","unified":1}`),
			)),
		}, nil
	})
	client, err := newSandboxClientForTest(
		doer, sandbox.CredentialPair{APIKey: "demo-key", APISecret: "demo-secret"},
		evidence, "cfg-1", func() time.Time { return now },
	)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := client.ValidateStartup(
		context.Background(),
		demoAttestation("demo-key", 12345),
	)
	if err != nil {
		t.Fatalf("startup validation failed: %v", err)
	}
	if identity.Environment != sandbox.EnvironmentBybitDemo {
		t.Fatalf("wrong environment: %s", identity.Environment)
	}
	signingInput := captured.Header.Get("X-BAPI-TIMESTAMP") + "demo-key" +
		captured.Header.Get("X-BAPI-RECV-WINDOW")
	mac := hmac.New(sha256.New, []byte("demo-secret"))
	_, _ = mac.Write([]byte(signingInput))
	if captured.Header.Get("X-BAPI-SIGN") != hex.EncodeToString(mac.Sum(nil)) {
		t.Fatal("signature mismatch")
	}
	if captured.URL.Host != demoRESTHost || captured.URL.Path != "/v5/user/query-api" {
		t.Fatalf("unsafe target: %s", captured.URL)
	}
	if len(evidence.records) != 1 ||
		strings.Contains(strings.Join(evidence.records[0].FieldNames, ","), "apiKey") {
		t.Fatalf("unsafe evidence: %#v", evidence.records)
	}
}

func TestBybitDemoClockWarmupRetriesHighUncertainty(t *testing.T) {
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
			result := `{"timeSecond":"` +
				strconv.FormatInt(serverAt.Unix(), 10) +
				`","timeNano":"` +
				strconv.FormatInt(serverAt.UnixNano(), 10) + `"}`
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(demoEnvelope(result))),
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
	if err = client.ensureDemoClock(context.Background()); err != nil {
		t.Fatalf("clock warmup failed: %v", err)
	}
	if requests != 2 || !client.clockValidated ||
		client.demoClockOffset() != 0 {
		t.Fatalf(
			"clock warmup requests=%d validated=%t offset=%s",
			requests,
			client.clockValidated,
			client.demoClockOffset(),
		)
	}
}

func TestBybitDemoClockWarmupUsesBoundedSampleBudget(t *testing.T) {
	base := time.UnixMilli(1_700_000_000_000).UTC()
	current := base
	requests := 0
	client, err := newSandboxClientForTest(
		authenticatedRoundTripFunc(func(*http.Request) (*http.Response, error) {
			requests++
			sentAt := current
			roundTrip := 800 * time.Millisecond
			if requests == demoClockWarmupAttempts {
				roundTrip = 200 * time.Millisecond
			}
			current = sentAt.Add(roundTrip)
			serverAt := sentAt.Add(roundTrip / 2)
			result := `{"timeSecond":"` +
				strconv.FormatInt(serverAt.Unix(), 10) +
				`","timeNano":"` +
				strconv.FormatInt(serverAt.UnixNano(), 10) + `"}`
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(demoEnvelope(result))),
			}, nil
		}),
		sandbox.CredentialPair{APIKey: "key", APISecret: "secret"},
		&captureEvidence{},
		"cfg",
		func() time.Time { return current },
	)
	if err != nil {
		t.Fatal(err)
	}
	client.clockValidated = false
	if err = client.ensureDemoClock(context.Background()); err != nil {
		t.Fatalf("bounded clock warmup failed: %v", err)
	}
	if requests != demoClockWarmupAttempts ||
		!client.clockValidated ||
		client.demoClockOffset() != 0 {
		t.Fatalf(
			"clock warmup requests=%d validated=%t offset=%s",
			requests,
			client.clockValidated,
			client.demoClockOffset(),
		)
	}
}

func TestBybitDemoClockWarmupRejectsUnsafeSampleBudget(t *testing.T) {
	base := time.UnixMilli(1_700_000_000_000).UTC()
	current := base
	requests := 0
	client, err := newSandboxClientForTest(
		authenticatedRoundTripFunc(func(*http.Request) (*http.Response, error) {
			requests++
			sentAt := current
			current = sentAt.Add(800 * time.Millisecond)
			serverAt := sentAt.Add(400 * time.Millisecond)
			result := `{"timeSecond":"` +
				strconv.FormatInt(serverAt.Unix(), 10) +
				`","timeNano":"` +
				strconv.FormatInt(serverAt.UnixNano(), 10) + `"}`
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(demoEnvelope(result))),
			}, nil
		}),
		sandbox.CredentialPair{APIKey: "key", APISecret: "secret"},
		&captureEvidence{},
		"cfg",
		func() time.Time { return current },
	)
	if err != nil {
		t.Fatal(err)
	}
	client.clockValidated = false
	if err = client.ensureDemoClock(context.Background()); !errors.Is(
		err,
		ErrDemoRequest,
	) {
		t.Fatalf("unsafe clock warmup error=%v", err)
	}
	if requests != demoClockWarmupAttempts || client.clockValidated {
		t.Fatalf(
			"clock warmup requests=%d validated=%t",
			requests,
			client.clockValidated,
		)
	}
}

func TestBybitClockRejectionRefreshesAndRetriesOnce(t *testing.T) {
	now := time.UnixMilli(1_700_000_000_000).UTC()
	walletRequests := 0
	timeRequests := 0
	evidence := &captureEvidence{}
	client, err := newSandboxClientForTest(
		authenticatedRoundTripFunc(func(request *http.Request) (*http.Response, error) {
			var body string
			switch request.URL.Path {
			case "/v5/account/wallet-balance":
				walletRequests++
				if walletRequests == 1 {
					body = `{"retCode":10002,"retMsg":"","result":{},"retExtInfo":{},"time":1700000000000}`
				} else {
					body = demoEnvelope(`{"list":[]}`)
				}
			case "/v5/market/time":
				timeRequests++
				body = demoEnvelope(
					`{"timeSecond":"1700000000","timeNano":"1700000000000000000"}`,
				)
			default:
				t.Fatalf("unexpected path %q", request.URL.Path)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(body)),
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
	if _, err = client.walletBalance(context.Background()); err != nil {
		t.Fatalf("clock rejection was not recovered: %v", err)
	}
	if walletRequests != 2 || timeRequests != 1 ||
		len(evidence.records) != 2 {
		t.Fatalf(
			"wallet=%d time=%d evidence=%d",
			walletRequests,
			timeRequests,
			len(evidence.records),
		)
	}
}

func TestBybitReadTransportFailureClosesIdlePoolAndRetriesOnce(t *testing.T) {
	now := time.UnixMilli(1_700_000_000_000).UTC()
	doer := &idleClosingDemoDoer{}
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
	if _, err = client.walletBalance(context.Background()); err != nil {
		t.Fatalf("stale read connection was not recovered: %v", err)
	}
	if doer.requests != 2 || doer.closed != 1 {
		t.Fatalf("requests=%d closed=%d", doer.requests, doer.closed)
	}
}

func TestBybitAcceptsExactDemoUIUnifiedTradingBundle(t *testing.T) {
	permissions := map[string][]string{
		"ContractTrade": {"Position", "Order"},
		"Spot":          {"SpotTrade"},
		"Wallet":        {},
		"Options":       {"OptionsTrade"},
		"Derivatives":   {"DerivativesTrade"},
		"Exchange":      {},
	}
	if !validDemoKeyPermissions(permissions) {
		t.Fatal("exact Bybit Demo UI Unified Trading bundle rejected")
	}
}

func TestBybitRejectsForbiddenPermissions(t *testing.T) {
	responses := []string{
		`{"id":"demo-account","readOnly":1,"secret":"","uta":1,"userID":12345,"permissions":{"Spot":["SpotTrade"]}}`,
		`{"id":"demo-account","readOnly":0,"secret":"","uta":1,"userID":12345,"permissions":{"Spot":[]}}`,
		`{"id":"demo-account","readOnly":0,"secret":"","uta":1,"userID":12345,"permissions":{"Spot":["SpotTrade","MarginTrade"]}}`,
		`{"id":"demo-account","readOnly":0,"secret":"","uta":1,"userID":12345,"permissions":{"Spot":["SpotTrade"],"Wallet":["AccountTransfer"]}}`,
		`{"id":"demo-account","readOnly":0,"secret":"","uta":1,"userID":12345,"permissions":{"Spot":["SpotTrade"],"ContractTrade":["Order"]}}`,
		`{"id":"demo-account","readOnly":0,"secret":"","uta":1,"userID":12345,"permissions":{"Spot":["SpotTrade"],"Options":["OptionsTrade"]}}`,
		`{"id":"demo-account","readOnly":0,"secret":"","uta":1,"userID":12345,"permissions":{"Spot":["SpotTrade"],"Derivatives":["DerivativesTrade"]}}`,
		`{"id":"demo-account","readOnly":0,"secret":"","uta":1,"userID":12345,"permissions":{"Spot":["SpotTrade"],"ContractTrade":["Order","Position"],"Options":["OptionsTrade"],"Derivatives":[]}}`,
		`{"id":"demo-account","readOnly":0,"secret":"","uta":1,"userID":12345,"permissions":{"Spot":["SpotTrade"],"ContractTrade":["Order","Position"],"Options":["OptionsTrade"],"Derivatives":["DerivativesTrade"],"Exchange":["ExchangeHistory"]}}`,
		`{"id":"demo-account","readOnly":0,"secret":"","uta":1,"userID":12345,"permissions":{"Spot":["SpotTrade"],"ContractTrade":["Order","Position"],"Options":["OptionsTrade"],"Derivatives":["DerivativesTrade"],"FutureProduct":["FutureTrade"]}}`,
		`{"id":"demo-account","readOnly":0,"secret":"","uta":1,"userID":12345,"permissions":{"Spot":["SpotTrade"],"Earn":["Earn"]}}`,
	}
	for index, response := range responses {
		doer := authenticatedRoundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(demoEnvelope(response))),
			}, nil
		})
		client, err := newSandboxClientForTest(
			doer, sandbox.CredentialPair{APIKey: "key", APISecret: "secret"},
			&captureEvidence{}, "cfg", func() time.Time { return time.Unix(1, 0).UTC() },
		)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := client.ValidateStartup(
			context.Background(),
			demoAttestation("key", 12345),
		); !errors.Is(err, ErrDemoStartupPermission) {
			t.Fatalf("forbidden permission response %d accepted: %v", index, err)
		}
	}
}

func TestBybitEvidenceFailurePreventsNetwork(t *testing.T) {
	called := false
	client, err := newSandboxClientForTest(
		authenticatedRoundTripFunc(func(*http.Request) (*http.Response, error) {
			called = true
			return nil, errors.New("unexpected")
		}),
		sandbox.CredentialPair{APIKey: "key", APISecret: "secret"},
		&captureEvidence{err: errors.New("disk full")}, "cfg",
		func() time.Time { return time.Unix(1, 0).UTC() },
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.ValidateStartup(
		context.Background(),
		demoAttestation("key", 12345),
	); err == nil || called {
		t.Fatalf("fail-closed evidence: err=%v network=%t", err, called)
	}
}

func TestBybitStartupRejectsOversizedResponse(t *testing.T) {
	body := demoEnvelope(`{"id":"demo-account","readOnly":0,"secret":"","uta":1,"userID":12345,"permissions":{"Spot":["SpotTrade"]}}`) +
		strings.Repeat(" ", authenticatedResponseLimit+1)
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
	if _, err = client.ValidateStartup(
		context.Background(),
		demoAttestation("key", 12345),
	); !errors.Is(err, ErrDemoRequest) {
		t.Fatalf("oversized response error=%v want=%v", err, ErrDemoRequest)
	}
}

func demoEnvelope(result string) string {
	return demoEnvelopeWithMessage("OK", result)
}

func demoEnvelopeWithMessage(message, result string) string {
	return `{"retCode":0,"retMsg":"` + message + `","result":` + result +
		`,"retExtInfo":{},"time":1700000000000}`
}

func demoAttestation(apiKey string, userID uint64) BybitDemoAttestation {
	return BybitDemoAttestation{
		AccountIdentityHash: hashString(strconv.FormatUint(userID, 10) + "|UNIFIED"),
		KeyFingerprint:      fingerprintString(apiKey),
		DemoOnly:            true,
	}
}

func TestBybitRequestHashCommitsSignedTimestamp(t *testing.T) {
	now := time.UnixMilli(1_700_000_000_000).UTC()
	client, err := newSandboxClientForTest(
		authenticatedRoundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("network must not be used")
		}),
		sandbox.CredentialPair{APIKey: "key", APISecret: "secret"},
		&captureEvidence{},
		"cfg",
		func() time.Time { return now },
	)
	if err != nil {
		t.Fatal(err)
	}
	fields := validBybitRouteFields(authenticatedWalletBalance)
	first, err := client.buildSignedRequest(authenticatedWalletBalance, fields)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Millisecond)
	second, err := client.buildSignedRequest(authenticatedWalletBalance, fields)
	if err != nil {
		t.Fatal(err)
	}
	if first.hash == second.hash ||
		first.headers.timestamp == second.headers.timestamp ||
		first.headers.signature == second.headers.signature {
		t.Fatal("signed timestamp was not committed to the request identity")
	}
}

func TestBybitAuthenticatedPolicyRejectsForbiddenCapabilities(t *testing.T) {
	base := url.Values{
		"category": {"spot"}, "isLeverage": {"0"}, "orderFilter": {"Order"},
		"orderLinkId": {"axiom-0001"}, "orderType": {"Limit"}, "price": {"1.00"},
		"qty": {"1.00"}, "side": {"Buy"}, "symbol": {"BTCUSDT"}, "timeInForce": {"GTC"},
	}
	if _, err := validateAuthenticatedFields(authenticatedCreate, base); err != nil {
		t.Fatalf("valid create rejected: %v", err)
	}
	mutations := []func(url.Values){
		func(values url.Values) { values.Set("category", "linear") },
		func(values url.Values) { values.Set("isLeverage", "1") },
		func(values url.Values) { values.Set("orderType", "Market") },
		func(values url.Values) { values.Set("timeInForce", "FOK") },
		func(values url.Values) { values.Set("takeProfit", "2") },
		func(values url.Values) { values.Set("orderFilter", "StopOrder") },
	}
	for index, mutate := range mutations {
		values := cloneValues(base)
		mutate(values)
		if _, err := validateAuthenticatedFields(authenticatedCreate, values); err == nil {
			t.Fatalf("forbidden mutation %d accepted: %v", index, values)
		}
	}
}

func TestBybitAuthenticatedRouteMatrixIsClosedAndExhaustive(t *testing.T) {
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
	if len(authenticatedRoutePolicies) != 7 {
		t.Fatalf("authenticated route count = %d", len(authenticatedRoutePolicies))
	}
	for route, policy := range authenticatedRoutePolicies {
		fields := validBybitRouteFields(route)
		signed, buildErr := client.buildSignedRequest(route, fields)
		if buildErr != nil {
			t.Fatalf("%s %s rejected: %v", policy.method, policy.path, buildErr)
		}
		if signed.method != policy.method || signed.path != policy.path ||
			!strings.HasPrefix(signed.path, "/v5/") ||
			strings.Contains(demoRESTHost, "api.bybit.com") {
			t.Fatalf("unsafe signed route: %#v", signed)
		}
		for _, forbidden := range []string{
			"autoAddMargin", "borrowAmount", "leverage", "positionIdx", "reduceOnly",
			"takeProfit", "tpslMode", "triggerDirection", "withdrawType",
		} {
			mutated := cloneValues(fields)
			mutated.Set(forbidden, "1")
			if _, mutationErr := client.buildSignedRequest(route, mutated); mutationErr == nil {
				t.Fatalf("%s %s accepted forbidden field %s", policy.method, policy.path, forbidden)
			}
		}
	}
	for _, route := range []authenticatedRoute{0, 255} {
		if _, err = client.buildSignedRequest(route, url.Values{}); err == nil {
			t.Fatalf("unknown route %d accepted", route)
		}
	}
}

func validBybitRouteFields(route authenticatedRoute) url.Values {
	fields := url.Values{}
	switch route {
	case authenticatedWalletBalance:
		fields.Set("accountType", "UNIFIED")
	case authenticatedCreate:
		fields.Set("category", "spot")
		fields.Set("isLeverage", "0")
		fields.Set("orderFilter", "Order")
		fields.Set("orderLinkId", "axiom-0001")
		fields.Set("orderType", "Limit")
		fields.Set("price", "1.00")
		fields.Set("qty", "1.00")
		fields.Set("side", "Buy")
		fields.Set("symbol", "BTCUSDT")
		fields.Set("timeInForce", "GTC")
	case authenticatedCancel:
		fields.Set("category", "spot")
		fields.Set("orderFilter", "Order")
		fields.Set("orderLinkId", "axiom-0001")
		fields.Set("symbol", "BTCUSDT")
	case authenticatedQuery:
		fields.Set("category", "spot")
		fields.Set("orderFilter", "Order")
		fields.Set("orderLinkId", "axiom-0001")
	case authenticatedOrderHistory:
		fields.Set("category", "spot")
		fields.Set("orderFilter", "Order")
	case authenticatedExecutionHistory:
		fields.Set("category", "spot")
	}
	return fields
}

func FuzzBybitAuthenticatedCreatePolicy(f *testing.F) {
	f.Add("category=spot&isLeverage=0&orderFilter=Order&orderLinkId=ax-00000001&orderType=Limit&price=1.00&qty=1.00&side=Buy&symbol=BTCUSDT&timeInForce=GTC")
	f.Add("category=linear&isLeverage=1&orderType=Market")
	f.Fuzz(func(t *testing.T, raw string) {
		values, err := url.ParseQuery(raw)
		if err != nil {
			return
		}
		policy, err := validateAuthenticatedFields(authenticatedCreate, values)
		if err != nil {
			return
		}
		if policy.method != http.MethodPost || policy.path != "/v5/order/create" ||
			values.Get("category") != "spot" || values.Get("isLeverage") != "0" ||
			values.Get("orderFilter") != "Order" || values.Get("orderType") != "Limit" {
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
