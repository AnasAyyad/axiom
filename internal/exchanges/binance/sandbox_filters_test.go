package binance

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"axiom/internal/domain"
	"axiom/internal/sandbox"
)

func TestBinanceSandboxRulesEnforceExactFiltersAndDynamicPrice(t *testing.T) {
	now := time.UnixMilli(1_700_000_000_000).UTC()
	instrument := mustSandboxInstrument(t)
	rules, err := normalizeSandboxRules(
		[]byte(fullSandboxExchangeInfo),
		instrument,
		now,
	)
	if err != nil {
		t.Fatalf("normalize rules: %v", err)
	}
	submission := sandboxSubmission(t, "100", "0.1", "10")
	owned, _ := domain.ParseBalance("1")
	reference := sandboxAveragePrice{Minutes: 5, Price: sandboxMustPrice(t, "100"), ObservedAt: now}
	if err = rules.validateSubmission(submission, owned, reference, now); err != nil {
		t.Fatalf("valid submission rejected: %v", err)
	}
	assertSandboxRuleRejections(t, rules, submission, owned, reference, now)
}

func assertSandboxRuleRejections(
	t *testing.T,
	rules SandboxInstrumentRules,
	submission sandbox.Submission,
	owned domain.Balance,
	reference sandboxAveragePrice,
	now time.Time,
) {
	t.Helper()
	for _, testCase := range sandboxRuleRejectionCases(t, now) {
		t.Run(testCase.name, func(t *testing.T) {
			candidate := submission
			candidateReference := reference
			testCase.mutate(&candidate, &candidateReference)
			if err := rules.validateSubmission(candidate, owned, candidateReference, now); !errors.Is(err, ErrSandboxFilter) {
				t.Fatalf("error=%v want filter rejection", err)
			}
		})
	}
}

type sandboxRuleRejectionCase struct {
	name   string
	mutate func(*sandbox.Submission, *sandboxAveragePrice)
}

func sandboxRuleRejectionCases(
	t *testing.T,
	now time.Time,
) []sandboxRuleRejectionCase {
	t.Helper()
	return []sandboxRuleRejectionCase{
		{
			name: "off tick",
			mutate: func(submission *sandbox.Submission, _ *sandboxAveragePrice) {
				submission.LimitPrice = sandboxMustPrice(t, "100.001")
			},
		},
		{
			name: "off step",
			mutate: func(submission *sandbox.Submission, _ *sandboxAveragePrice) {
				submission.Quantity = sandboxMustQuantity(t, "0.10001")
			},
		},
		{
			name: "dynamic price",
			mutate: func(submission *sandbox.Submission, _ *sandboxAveragePrice) {
				submission.LimitPrice = sandboxMustPrice(t, "501")
				submission.Notional, _ = domain.ParseNotional("50.1")
			},
		},
		{
			name: "stale average",
			mutate: func(_ *sandbox.Submission, reference *sandboxAveragePrice) {
				reference.ObservedAt = now.Add(-6 * time.Minute)
			},
		},
		{
			name: "oversell",
			mutate: func(submission *sandbox.Submission, _ *sandboxAveragePrice) {
				submission.Side = domain.SideSell
				submission.Quantity = sandboxMustQuantity(t, "2")
				submission.Notional, _ = domain.ParseNotional("200")
			},
		},
	}
}

func TestBinanceSandboxRulesRejectUnknownOrIncompleteFilters(t *testing.T) {
	now := time.UnixMilli(1_700_000_000_000).UTC()
	instrument := mustSandboxInstrument(t)
	testCases := []string{
		strings.Replace(fullSandboxExchangeInfo, `"filterType":"LOT_SIZE"`, `"filterType":"NEW_UNREVIEWED_FILTER"`, 1),
		strings.Replace(fullSandboxExchangeInfo, `"filterType":"PRICE_FILTER"`, `"filterType":"LOT_SIZE"`, 1),
		strings.Replace(fullSandboxExchangeInfo, `"filterType":"PERCENT_PRICE"`, `"filterType":"MAX_NUM_ORDERS"`, 1),
	}
	for index, payload := range testCases {
		if _, err := normalizeSandboxRules([]byte(payload), instrument, now); !errors.Is(err, ErrSandboxFilter) {
			t.Fatalf("case %d error=%v want filter rejection", index, err)
		}
	}
}

func TestBinanceUnsignedMetadataUsesOnlyFixedTestnetRoutesWithoutCredentials(t *testing.T) {
	now := time.UnixMilli(1_700_000_000_000).UTC()
	var requests []*http.Request
	doer := authenticatedRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests = append(requests, request)
		body := fullSandboxExchangeInfo
		if request.URL.Path == "/api/v3/avgPrice" {
			body = `{"mins":5,"price":"100","closeTime":1700000000000}`
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	})
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
	instrument := mustSandboxInstrument(t)
	if _, err = client.loadSandboxRules(context.Background(), instrument); err != nil {
		t.Fatal(err)
	}
	if _, err = client.averagePrice(context.Background(), instrument); err != nil {
		t.Fatal(err)
	}
	if len(requests) != 2 {
		t.Fatalf("request count=%d", len(requests))
	}
	for _, request := range requests {
		if request.URL.Host != sandboxRESTHost ||
			(request.URL.Path != "/api/v3/exchangeInfo" &&
				request.URL.Path != "/api/v3/avgPrice") ||
			request.Header.Get("X-MBX-APIKEY") != "" ||
			request.URL.Query().Get("signature") != "" {
			t.Fatalf("unsafe unsigned request: %s headers=%v", request.URL, request.Header)
		}
	}
}

type averagePriceClockFixture struct {
	now           time.Time
	closeAt       time.Time
	priceRequests int
	timeRequests  int
}

func (fixture *averagePriceClockFixture) roundTrip(
	request *http.Request,
) (*http.Response, error) {
	body := fmt.Sprintf(
		`{"mins":5,"price":"100","closeTime":%d}`,
		fixture.closeAt.UnixMilli(),
	)
	if request.URL.Path == "/api/v3/time" {
		fixture.timeRequests++
		fixture.now = fixture.now.Add(time.Millisecond)
		body = fmt.Sprintf(
			`{"serverTime":%d}`,
			fixture.now.Add(2*time.Second).UnixMilli(),
		)
	} else {
		fixture.priceRequests++
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}, nil
}

func TestBinanceAveragePriceUsesBoundedExchangeClock(t *testing.T) {
	fixture := &averagePriceClockFixture{
		now: time.UnixMilli(1_700_000_000_000).UTC(),
	}
	fixture.closeAt = fixture.now.Add(1500 * time.Millisecond)
	client, err := newSandboxClientForTest(
		authenticatedRoundTripFunc(fixture.roundTrip),
		sandbox.CredentialPair{APIKey: "key", APISecret: "secret"},
		&captureEvidence{},
		"cfg",
		func() time.Time { return fixture.now },
	)
	if err != nil {
		t.Fatal(err)
	}
	if err = client.clock.Observe(
		fixture.now,
		fixture.now,
		fixture.now.Add(2*time.Second),
		0,
		0,
	); err != nil {
		t.Fatal(err)
	}
	instrument := mustSandboxInstrument(t)
	reference, err := client.averagePrice(context.Background(), instrument)
	if err != nil ||
		!reference.ValidatedThrough.Equal(fixture.now.Add(2*time.Second)) {
		t.Fatalf("bounded exchange time was rejected: %v", err)
	}
	fixture.closeAt = reference.ValidatedThrough.Add(time.Millisecond)
	reference, err = client.averagePrice(context.Background(), instrument)
	if err != nil || fixture.timeRequests != 1 || fixture.priceRequests != 2 {
		t.Fatalf(
			"bounded clock catch-up reference=%#v time=%d price=%d error=%v",
			reference,
			fixture.timeRequests,
			fixture.priceRequests,
			err,
		)
	}
	fixture.closeAt = reference.ValidatedThrough.Add(time.Second)
	if _, err = client.averagePrice(context.Background(), instrument); err == nil ||
		fixture.timeRequests != 1+sandboxClockWarmupAttempts ||
		fixture.priceRequests != 3 {
		t.Fatalf("future exchange time error=%v", err)
	}
}

func TestBinanceCreateTimeoutAndServerFailureAreAmbiguous(t *testing.T) {
	submission := sandboxSubmission(t, "100", "0.1", "10")
	testCases := []struct {
		name string
		doer authenticatedRoundTripFunc
	}{
		{
			name: "transport",
			doer: func(*http.Request) (*http.Response, error) {
				return nil, errors.New("timeout")
			},
		},
		{
			name: "server",
			doer: func(*http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusInternalServerError,
					Body:       io.NopCloser(strings.NewReader(`{"code":-1000}`)),
				}, nil
			},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			client, err := newSandboxClientForTest(
				testCase.doer,
				sandbox.CredentialPair{APIKey: "key", APISecret: "secret"},
				&captureEvidence{},
				"cfg",
				func() time.Time { return time.UnixMilli(1_700_000_000_000).UTC() },
			)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = client.create(context.Background(), submission); !errors.Is(err, ErrSandboxAmbiguous) {
				t.Fatalf("error=%v want ambiguous", err)
			}
		})
	}
}

func mustSandboxInstrument(t *testing.T) domain.Instrument {
	t.Helper()
	instrument, err := domain.NewSpotInstrument("BTC", "USDT")
	if err != nil {
		t.Fatal(err)
	}
	return instrument
}

func sandboxSubmission(
	t *testing.T,
	priceText string,
	quantityText string,
	notionalText string,
) sandbox.Submission {
	t.Helper()
	planID, err := domain.NewExecutionPlanID("plan-binance-test")
	if err != nil {
		t.Fatal(err)
	}
	orderID, err := domain.NewVirtualOrderID("order-binance-test")
	if err != nil {
		t.Fatal(err)
	}
	strategyID, err := domain.NewStrategyID(sandbox.StrategySandboxCanary)
	if err != nil {
		t.Fatal(err)
	}
	return sandbox.Submission{
		PlanID: planID, OrderID: orderID, AccountID: "binance-account",
		AccountEpoch: 1, ClientOrderID: "ax-00000001", StrategyID: strategyID,
		Instrument: mustSandboxInstrument(t), Side: domain.SideBuy,
		Quantity: sandboxMustQuantity(t, quantityText), LimitPrice: sandboxMustPrice(t, priceText),
		Notional: sandboxMustNotional(t, notionalText), Style: sandbox.OrderStyleLimitGTC,
		Action: sandbox.IntentEntry,
	}
}

func sandboxMustPrice(t *testing.T, value string) domain.Price {
	t.Helper()
	parsed, err := domain.ParsePrice(value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func sandboxMustQuantity(t *testing.T, value string) domain.Quantity {
	t.Helper()
	parsed, err := domain.ParseQuantity(value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func sandboxMustNotional(t *testing.T, value string) domain.Notional {
	t.Helper()
	parsed, err := domain.ParseNotional(value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

const fullSandboxExchangeInfo = `{
  "timezone":"UTC",
  "serverTime":1700000000000,
  "rateLimits":[],
  "exchangeFilters":[],
  "symbols":[{
    "symbol":"BTCUSDT",
    "status":"TRADING",
    "baseAsset":"BTC",
    "baseAssetPrecision":4,
    "quoteAsset":"USDT",
    "quotePrecision":2,
    "quoteAssetPrecision":2,
    "baseCommissionPrecision":8,
    "quoteCommissionPrecision":8,
    "orderTypes":["LIMIT","LIMIT_MAKER"],
    "icebergAllowed":false,
    "ocoAllowed":false,
    "otoAllowed":false,
    "opoAllowed":false,
    "quoteOrderQtyMarketAllowed":false,
    "allowTrailingStop":false,
    "cancelReplaceAllowed":false,
    "amendAllowed":false,
    "pegInstructionsAllowed":false,
    "isSpotTradingAllowed":true,
    "isMarginTradingAllowed":false,
    "filters":[
      {"filterType":"PRICE_FILTER","minPrice":"0.01","maxPrice":"1000000","tickSize":"0.01"},
      {"filterType":"LOT_SIZE","minQty":"0.0001","maxQty":"10","stepSize":"0.0001"},
      {"filterType":"NOTIONAL","minNotional":"5","applyMinToMarket":false,"maxNotional":"100","applyMaxToMarket":false},
      {"filterType":"PERCENT_PRICE","multiplierUp":"5","multiplierDown":"0.2","avgPriceMins":5}
    ],
    "permissions":["SPOT"],
    "permissionSets":[],
    "defaultSelfTradePreventionMode":"NONE",
    "allowedSelfTradePreventionModes":["NONE"]
  }]
}`
