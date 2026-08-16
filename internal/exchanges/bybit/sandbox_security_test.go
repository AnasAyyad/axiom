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

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
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

func TestSandboxPeerMarketClientUsesFixedPublicOnlyProxy(t *testing.T) {
	client, err := NewSandboxPeerMarketPublicClient(&domain.SystemClock{})
	if err != nil {
		t.Fatal(err)
	}
	httpClient, ok := client.httpClient.(*http.Client)
	if !ok {
		t.Fatalf("HTTP client type = %T", client.httpClient)
	}
	transport, ok := httpClient.Transport.(*http.Transport)
	if !ok || transport.Proxy == nil {
		t.Fatalf("HTTP transport = %T", httpClient.Transport)
	}
	proxy, err := transport.Proxy(&http.Request{})
	if err != nil || proxy == nil || proxy.String() != sandboxPeerPublicProxyOrigin {
		t.Fatalf("proxy = %v, %v", proxy, err)
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

func TestBybitStartupEligibilityUsesCredentialFreePublicMarketData(
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
	privateCalls := 0
	client.publicDoer = authenticatedRoundTripFunc(func(*http.Request) (*http.Response, error) {
		privateCalls++
		return nil, errors.New("demo_private_public_route_not_expected")
	})
	market := &bybitSandboxMarketData{snapshots: bybitStartupSnapshots(t)}
	eligibilities, err := (&SandboxAdapter{client: client, marketData: market}).
		StrategyEligibility(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if market.calls != len(approvedInstruments()) || privateCalls != 0 ||
		len(eligibilities) != len(approvedInstruments()) {
		t.Fatalf(
			"market_calls=%d private_calls=%d eligibilities=%#v",
			market.calls, privateCalls,
			eligibilities,
		)
	}
	for index, instrument := range approvedInstruments() {
		if !eligibilities[index].Eligible || eligibilities[index].Instrument != instrument.Symbol() {
			t.Fatalf("eligibility[%d]=%#v", index, eligibilities[index])
		}
	}
}

func TestBybitStartupEligibilityRejectsMalformedCredentialFreeBook(t *testing.T) {
	now := time.UnixMilli(1_700_000_000_000).UTC()
	client, err := newSandboxClientForTest(
		authenticatedRoundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("demo_network_not_expected")
		}),
		sandbox.CredentialPair{APIKey: "key", APISecret: "secret"},
		&captureEvidence{}, "cfg", func() time.Time { return now },
	)
	if err != nil {
		t.Fatal(err)
	}
	client.clockValidated = true
	client.clockObservedAt = now
	market := &bybitSandboxMarketData{snapshots: bybitStartupSnapshots(t)}
	broken := market.snapshots["BTCUSDT"]
	broken.Asks[0].Price = broken.Bids[0].Price
	market.snapshots["BTCUSDT"] = broken
	if _, err = (&SandboxAdapter{client: client, marketData: market}).
		StartupEligibility(context.Background()); !errors.Is(err, ErrDemoRequest) {
		t.Fatalf("malformed public book error=%v", err)
	}
}

type bybitSandboxMarketData struct {
	snapshots map[string]exchangecontracts.BookSnapshot
	calls     int
}

func (source *bybitSandboxMarketData) Snapshot(
	_ context.Context,
	request exchangecontracts.SnapshotRequest,
) (exchangecontracts.BookSnapshot, error) {
	source.calls++
	snapshot, found := source.snapshots[request.Instrument.Symbol()]
	if !found {
		return exchangecontracts.BookSnapshot{}, errors.New("bybit_sandbox_market_snapshot_missing")
	}
	return snapshot, nil
}

func (*bybitSandboxMarketData) Subscribe(
	context.Context,
	exchangecontracts.StreamRequest,
) (exchangecontracts.Stream, error) {
	return nil, errors.New("bybit_sandbox_market_stream_not_configured")
}

func bybitStartupSnapshots(t *testing.T) map[string]exchangecontracts.BookSnapshot {
	t.Helper()
	bid, err := domain.ParsePrice("100")
	if err != nil {
		t.Fatal(err)
	}
	ask, err := domain.ParsePrice("101")
	if err != nil {
		t.Fatal(err)
	}
	quantity, err := domain.ParseQuantity("1")
	if err != nil {
		t.Fatal(err)
	}
	result := make(map[string]exchangecontracts.BookSnapshot, len(approvedInstruments()))
	for index, instrument := range approvedInstruments() {
		result[instrument.Symbol()] = exchangecontracts.BookSnapshot{
			Exchange: "bybit", Instrument: instrument, LastSequence: uint64(index + 1),
			ReceivedAt: domain.EventTime{UTC: time.UnixMilli(1_700_000_000_000).UTC(), Sequence: uint64(index + 1)},
			Bids:       []exchangecontracts.PriceLevel{{Price: bid, Quantity: quantity}},
			Asks:       []exchangecontracts.PriceLevel{{Price: ask, Quantity: quantity}},
		}
	}
	return result
}
