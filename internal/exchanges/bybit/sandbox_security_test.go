package bybit

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"axiom/internal/sandbox"
)

func TestBybitCreateSerializationAndSigningGolden(t *testing.T) {
	now := time.UnixMilli(1_700_000_000_000).UTC()
	client, err := newSandboxClientForTest(
		authenticatedRoundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("network not expected")
		}),
		sandbox.CredentialPair{
			APIKey:    "demo-key",
			APISecret: "demo-secret",
		},
		&captureEvidence{},
		"cfg",
		func() time.Time { return now },
	)
	if err != nil {
		t.Fatal(err)
	}
	fields := url.Values{
		"category":    {"spot"},
		"isLeverage":  {"0"},
		"orderFilter": {"Order"},
		"orderLinkId": {"ax-00000001"},
		"orderType":   {"Limit"},
		"price":       {"100"},
		"qty":         {"0.1"},
		"side":        {"Buy"},
		"symbol":      {"BTCUSDT"},
		"timeInForce": {"GTC"},
	}
	signed, err := client.buildSignedRequest(authenticatedCreate, fields)
	if err != nil {
		t.Fatal(err)
	}
	wantBody := `{"category":"spot","isLeverage":"0","orderFilter":"Order","orderLinkId":"ax-00000001","orderType":"Limit","price":"100","qty":"0.1","side":"Buy","symbol":"BTCUSDT","timeInForce":"GTC"}`
	if string(signed.body) != wantBody {
		t.Fatalf("body=%s", signed.body)
	}
	const wantSignature = "e1249f43816696fad8a982ed750cf4bac834132e1121d7cc20e9027619f13177"
	if signed.headers.signature != wantSignature {
		t.Fatalf("signature=%s want=%s", signed.headers.signature, wantSignature)
	}
}

func TestBybitHTTP200RateLimitBlocksLocally(t *testing.T) {
	now := time.UnixMilli(1_700_000_000_000).UTC()
	calls := 0
	client, err := newSandboxClientForTest(
		authenticatedRoundTripFunc(func(*http.Request) (*http.Response, error) {
			calls++
			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"X-Bapi-Limit-Reset-Timestamp": {
						"1700000005000",
					},
				},
				Body: io.NopCloser(strings.NewReader(
					`{"retCode":10006,"retMsg":"Too many visits","result":{},"retExtInfo":{},"time":1700000000000}`,
				)),
			}, nil
		}),
		sandbox.CredentialPair{
			APIKey:    "key",
			APISecret: "secret",
		},
		&captureEvidence{},
		"cfg",
		func() time.Time { return now },
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.walletBalance(
		context.Background(),
	); !errors.Is(err, ErrDemoRateLimited) {
		t.Fatalf("first error=%v", err)
	}
	if _, err = client.walletBalance(
		context.Background(),
	); !errors.Is(err, ErrDemoRateLimited) {
		t.Fatalf("second error=%v", err)
	}
	if calls != 1 {
		t.Fatalf("network calls=%d want=1", calls)
	}
}

func TestBybitProductionPublicMetadataPathIsCredentialFree(t *testing.T) {
	now := time.UnixMilli(1_700_000_000_000).UTC()
	client, err := newSandboxClientForTest(
		authenticatedRoundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("demo network not expected")
		}),
		sandbox.CredentialPair{
			APIKey:    "demo-key",
			APISecret: "demo-secret",
		},
		&captureEvidence{},
		"cfg",
		func() time.Time { return now },
	)
	if err != nil {
		t.Fatal(err)
	}
	var captured *http.Request
	client.publicDoer = authenticatedRoundTripFunc(
		func(request *http.Request) (*http.Response, error) {
			captured = request
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body: io.NopCloser(strings.NewReader(
					`{"retCode":0,"retMsg":"OK","result":{},"retExtInfo":{},"time":1700000000000}`,
				)),
			}, nil
		},
	)
	_, err = client.executePublicUnsigned(
		context.Background(),
		"/v5/market/instruments-info",
		url.Values{"category": {"spot"}, "symbol": {"BTCUSDT"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if captured == nil || captured.URL.Host != "api.bybit.com" ||
		captured.Method != http.MethodGet ||
		captured.Header.Get("Authorization") != "" ||
		captured.Header.Get("Cookie") != "" ||
		captured.Header.Get("X-Bapi-Api-Key") != "" ||
		captured.Header.Get("X-Bapi-Sign") != "" ||
		captured.Header.Get("X-Bapi-Timestamp") != "" ||
		captured.Header.Get("X-Bapi-Recv-Window") != "" {
		t.Fatalf("unsafe public request: %#v", captured)
	}
}

func TestBybitDemoRulesAcceptUTAMetadataWithoutEnablingLeverage(t *testing.T) {
	now := time.UnixMilli(1_700_000_000_000).UTC()
	body := []byte(`{
	  "retCode":0,
	  "retMsg":"OK",
	  "result":{
	    "category":"spot",
	    "nextPageCursor":null,
	    "list":[{
	      "symbolId":1,
	      "symbol":"BTCUSDT",
	      "baseCoin":"BTC",
	      "quoteCoin":"USDT",
	      "innovation":"0",
	      "status":"Trading",
	      "marginTrading":"utaOnly",
	      "stTag":"0",
	      "lotSizeFilter":{
	        "basePrecision":"0.000001",
	        "quotePrecision":"0.0000001",
	        "maxOrderQty":"230",
	        "minOrderQty":"0.000001",
	        "minOrderAmt":"5",
	        "maxOrderAmt":"8000000",
	        "maxLimitOrderQty":"230",
	        "maxMarketOrderQty":"120",
	        "postOnlyMaxLimitOrderSize":"1150"
	      },
	      "priceFilter":{"tickSize":"0.1"},
	      "riskParameters":{},
	      "symbolType":"",
	      "xstockMultiplier":""
	    }]
	  },
	  "retExtInfo":{},
	  "time":1700000000000
	}`)
	rules, err := normalizeDemoRules(
		body,
		approvedInstruments()[0],
		now,
	)
	if err != nil || rules.validate() != nil {
		t.Fatalf("UTA spot metadata rules=%#v error=%v", rules, err)
	}
	if fields := validBybitRouteFields(authenticatedCreate); fields.Get("isLeverage") != "0" {
		t.Fatalf("UTA metadata changed compiled create policy: %#v", fields)
	}
}

func TestBybitStartupBookRejectsStalePublicFacts(t *testing.T) {
	now := time.UnixMilli(1_700_000_000_000).UTC()
	client, err := newSandboxClientForTest(
		authenticatedRoundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("demo network not expected")
		}),
		sandbox.CredentialPair{APIKey: "key", APISecret: "secret"},
		&captureEvidence{},
		"cfg",
		func() time.Time { return now },
	)
	if err != nil {
		t.Fatal(err)
	}
	client.publicDoer = authenticatedRoundTripFunc(
		func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body: io.NopCloser(strings.NewReader(
					`{"retCode":0,"retMsg":"OK","result":{"s":"BTCUSDT","b":[["99","1"]],"a":[["101","1"]],"ts":1699999997000,"u":42,"seq":42,"cts":1699999997000},"retExtInfo":{},"time":1700000000000}`,
				)),
			}, nil
		},
	)
	if err = client.validateStartupBook(
		context.Background(),
		approvedInstruments()[0],
	); !errors.Is(err, ErrDemoRequest) {
		t.Fatalf("stale public book error=%v", err)
	}
}

func TestBybitStartupEligibilityValidatesItsDeclaredInstrumentOnly(
	t *testing.T,
) {
	now := time.UnixMilli(1_700_000_000_000).UTC()
	client, err := newSandboxClientForTest(
		authenticatedRoundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("demo network not expected")
		}),
		sandbox.CredentialPair{APIKey: "key", APISecret: "secret"},
		&captureEvidence{},
		"cfg",
		func() time.Time { return now },
	)
	if err != nil {
		t.Fatal(err)
	}
	client.clockValidated = true
	client.clockObservedAt = now
	calls := 0
	client.publicDoer = authenticatedRoundTripFunc(
		func(request *http.Request) (*http.Response, error) {
			calls++
			if symbol := request.URL.Query().Get("symbol"); symbol != "BTCUSDT" {
				t.Fatalf("eligibility requested unrelated book %s", symbol)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body: io.NopCloser(strings.NewReader(
					`{"retCode":0,"retMsg":"OK","result":{"s":"BTCUSDT","b":[["99","1"]],"a":[["101","1"]],"ts":1700000000000,"u":42,"seq":42,"cts":1700000000000},"retExtInfo":{},"time":1700000000000}`,
				)),
			}, nil
		},
	)
	eligibility, err := (&SandboxAdapter{client: client}).
		StartupEligibility(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || eligibility.Instrument != "BTCUSDT" ||
		!eligibility.Eligible {
		t.Fatalf(
			"calls=%d instrument=%s eligible=%t",
			calls,
			eligibility.Instrument,
			eligibility.Eligible,
		)
	}
}
