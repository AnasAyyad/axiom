package binance

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"axiom/internal/domain"
	"axiom/internal/exchanges/sandboxemulator"
	"axiom/internal/execution"
	"axiom/internal/sandbox"
)

func TestBinanceAutomaticTrendDispatchUsesOnlyFencedSpotTestnetIOC(t *testing.T) {
	now := time.UnixMilli(1_700_000_001_000).UTC()
	fixture := newBinanceEmulatorFixture(t, now, sandboxemulator.Config{
		Exchange: sandbox.ExchangeBinance, APIKey: "test-key", APISecret: "test-secret",
	}, "cfg-automatic-trend", true)
	strategyID, err := domain.NewStrategyID(sandbox.StrategyTrend)
	if err != nil {
		t.Fatal(err)
	}
	submission := fixture.submission
	submission.StrategyID = strategyID
	submission.Style = sandbox.OrderStyleLimitIOC
	lookup := fixture.adapter.lookup.(*sandboxLookup)
	lookup.submissions[submission.ClientOrderID] = submission
	repository := &binanceAutomaticDispatchRepository{submission: submission, worker: "automatic-trend-worker", fence: 7}
	dispatcher, err := sandbox.NewSandboxDispatcher(submission.AccountID, submission.AccountEpoch,
		repository.worker, repository.fence, repository, fixture.adapter, sandbox.NoKillPoint{})
	if err != nil {
		t.Fatal(err)
	}
	count, err := dispatcher.DispatchOnce(context.Background(), now, 1)
	if err != nil || count != 1 || !repository.submitting || repository.event.OrderEvent == nil ||
		repository.event.OrderEvent.State != execution.OrderAcknowledged || fixture.emulator.NativeOrderCount() != 1 {
		t.Fatalf("dispatch=%d submitting=%t event=%#v native=%d error=%v", count,
			repository.submitting, repository.event, fixture.emulator.NativeOrderCount(), err)
	}
	if count, err = dispatcher.DispatchOnce(context.Background(), now.Add(time.Millisecond), 1); err != nil ||
		count != 0 || fixture.emulator.NativeOrderCount() != 1 {
		t.Fatalf("duplicate dispatch=%d native=%d error=%v", count, fixture.emulator.NativeOrderCount(), err)
	}
	frames := fixture.emulator.PrivateFrames()
	if len(frames) != 1 || !strings.Contains(string(frames[0]), `"f":"IOC"`) {
		t.Fatalf("automatic order was not IOC: %s", frames)
	}
	expiredAt := submission.ApprovedAt.Add(sandbox.ArmLifetime + time.Second)
	if err = dispatcher.Cancel(context.Background(), submission.ClientOrderID, expiredAt); err != nil ||
		!repository.cancelling || repository.cancelEvent.OrderEvent == nil ||
		repository.cancelEvent.OrderEvent.State != execution.OrderCanceled {
		t.Fatalf("post-expiry cancellation event=%#v cancelling=%t error=%v",
			repository.cancelEvent, repository.cancelling, err)
	}
	frames = fixture.emulator.PrivateFrames()
	if len(frames) != 2 || !strings.Contains(string(frames[1]), `"f":"IOC"`) {
		t.Fatalf("post-expiry cancellation lost IOC order identity: %s", frames)
	}
	assertBinanceAutomaticCreateCapture(t, fixture.emulator.Captures())
}

func assertBinanceAutomaticCreateCapture(t *testing.T, captures []sandboxemulator.Capture) {
	t.Helper()
	var create sandboxemulator.Capture
	for _, capture := range captures {
		if capture.Method == http.MethodPost && capture.Path == "/api/v3/order" {
			create = capture
		}
	}
	if create.Host != "testnet.binance.vision" || create.Exchange != sandbox.ExchangeBinance ||
		create.RequestHash == "" || strings.Contains(strings.ToLower(strings.Join(create.FieldNames, ",")), "signature") {
		t.Fatalf("redacted Testnet create capture=%#v", create)
	}
}

type binanceAutomaticDispatchRepository struct {
	submission  sandbox.Submission
	worker      string
	fence       uint64
	claimed     bool
	submitting  bool
	cancelling  bool
	event       sandbox.PrivateEvent
	cancelEvent sandbox.PrivateEvent
}

func (*binanceAutomaticDispatchRepository) ApprovePlan(context.Context, sandbox.ApprovedSandboxPlan,
	sandbox.SubmissionLimits, sandbox.KillPoint) error {
	return errors.New("automatic_test_plan_already_approved")
}

func (repository *binanceAutomaticDispatchRepository) ClaimOutbox(
	_ context.Context,
	account sandbox.AccountID,
	epoch uint64,
	worker string,
	fence uint64,
	now time.Time,
	_ time.Duration,
	limit int,
	_ sandbox.KillPoint,
) ([]sandbox.SubmissionOutbox, error) {
	if account != repository.submission.AccountID || epoch != repository.submission.AccountEpoch ||
		worker != repository.worker || fence != repository.fence || now.Location() != time.UTC || limit != 1 {
		return nil, errors.New("automatic_test_claim_rejected")
	}
	if repository.claimed {
		return nil, nil
	}
	repository.claimed = true
	return []sandbox.SubmissionOutbox{{ID: "automatic-trend-outbox", Submission: repository.submission,
		State: sandbox.OutboxClaimed, ClaimOwner: worker, FencingToken: fence, UpdatedAt: now}}, nil
}

func (repository *binanceAutomaticDispatchRepository) MarkSubmitting(
	_ context.Context, outboxID string, fence uint64, _ time.Time, _ sandbox.KillPoint,
) error {
	if outboxID != "automatic-trend-outbox" || fence != repository.fence {
		return errors.New("automatic_test_submit_fence_rejected")
	}
	repository.submitting = true
	return nil
}

func (*binanceAutomaticDispatchRepository) MarkUnknown(context.Context, string, uint64, time.Time, sandbox.KillPoint) error {
	return errors.New("automatic_test_unexpected_unknown")
}

func (repository *binanceAutomaticDispatchRepository) MarkCancelPending(
	_ context.Context,
	account sandbox.AccountID,
	epoch uint64,
	clientOrderID string,
	worker string,
	fence uint64,
	now time.Time,
	_ sandbox.KillPoint,
) (string, error) {
	if account != repository.submission.AccountID || epoch != repository.submission.AccountEpoch ||
		clientOrderID != repository.submission.ClientOrderID || worker != repository.worker ||
		fence != repository.fence || now.Before(repository.submission.ApprovedAt.Add(sandbox.ArmLifetime)) {
		return "", errors.New("automatic_test_cancel_fence_rejected")
	}
	repository.cancelling = true
	return "automatic-trend-cancel", nil
}

func (*binanceAutomaticDispatchRepository) MarkCancelUnknown(context.Context, string, uint64, time.Time,
	sandbox.KillPoint) error {
	return errors.New("automatic_test_unexpected_cancel_unknown")
}

func (repository *binanceAutomaticDispatchRepository) AppendPrivateEvent(
	_ context.Context, outboxID string, fence uint64, event sandbox.PrivateEvent, _ sandbox.KillPoint,
) error {
	if outboxID == "automatic-trend-cancel" {
		if fence != repository.fence || !repository.cancelling {
			return errors.New("automatic_test_cancel_ack_fence_rejected")
		}
		repository.cancelEvent = event
		return nil
	}
	if outboxID != "automatic-trend-outbox" || fence != repository.fence || !repository.submitting {
		return errors.New("automatic_test_ack_fence_rejected")
	}
	repository.event = event
	return nil
}

type sandboxLookup struct {
	submissions map[string]sandbox.Submission
	active      []sandbox.Submission
}

func (lookup *sandboxLookup) SubmissionByClientOrderID(
	_ context.Context,
	_ sandbox.AccountID,
	_ uint64,
	clientOrderID string,
) (sandbox.Submission, bool, error) {
	submission, found := lookup.submissions[clientOrderID]
	return submission, found, nil
}

func (lookup *sandboxLookup) ActiveSubmissions(
	context.Context,
	sandbox.AccountID,
	uint64,
) ([]sandbox.Submission, error) {
	return append([]sandbox.Submission(nil), lookup.active...), nil
}

type sandboxExpectations struct {
	expectation sandbox.SnapshotExpectation
	found       bool
}

func (expectations *sandboxExpectations) SnapshotExpectation(
	context.Context,
	sandbox.AccountID,
	uint64,
) (sandbox.SnapshotExpectation, bool, error) {
	return expectations.expectation, expectations.found, nil
}

func TestBinanceSandboxAdapterSubmitsOnlyAfterFiltersAndTestCreate(t *testing.T) {
	now := time.UnixMilli(1_700_000_000_000).UTC()
	submission := sandboxSubmission(t, "100", "0.1", "10")
	submission.AccountID = sandboxIdentity(now).AccountID
	submission = completeSandboxSubmission(submission, now)
	var paths []string
	doer := binanceSubmitSequence(t, &paths)
	client := sandboxTestClient(t, doer, now)
	lookup := &sandboxLookup{submissions: map[string]sandbox.Submission{
		submission.ClientOrderID: submission,
	}}
	adapter, err := newSandboxAdapterForTest(
		client, sandboxIdentity(now), 1, lookup,
		&sandboxExpectations{}, sandboxRules(t, now),
	)
	if err != nil {
		t.Fatal(err)
	}
	event, err := adapter.Submit(context.Background(), submission)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if event.Kind != sandbox.PrivateOrderEvent ||
		event.OrderEvent == nil ||
		event.OrderEvent.ExchangeStatus != "ACK" ||
		event.OrderEvent.ClientOrderID != submission.ClientOrderID {
		t.Fatalf("unexpected acknowledgement: %#v", event)
	}
	want := []string{
		"/api/v3/account", "/api/v3/avgPrice",
		"/api/v3/order/test", "/api/v3/order",
	}
	if strings.Join(paths, ",") != strings.Join(want, ",") {
		t.Fatalf("paths=%v want=%v", paths, want)
	}
}

func binanceSubmitSequence(
	t *testing.T,
	paths *[]string,
) authenticatedRoundTripFunc {
	t.Helper()
	return authenticatedRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		*paths = append(*paths, request.URL.Path)
		body := ""
		switch request.URL.Path {
		case "/api/v3/account":
			body = sandboxAccountJSON
		case "/api/v3/avgPrice":
			if request.Header.Get("X-MBX-APIKEY") != "" {
				t.Fatal("credential sent on public average-price route")
			}
			body = `{"mins":5,"price":"100","closeTime":1700000000000}`
		case "/api/v3/order/test":
			body = `{}`
		case "/api/v3/order":
			body = `{"symbol":"BTCUSDT","orderId":42,"orderListId":-1,"clientOrderId":"ax-00000001","transactTime":1700000000000}`
		default:
			t.Fatalf("unexpected path %s", request.URL.Path)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	})
}

func TestBinanceSandboxAdapterMalformedCreateAcknowledgementIsAmbiguous(t *testing.T) {
	now := time.UnixMilli(1_700_000_000_000).UTC()
	submission := completeSandboxSubmission(
		sandboxSubmission(t, "100", "0.1", "10"),
		now,
	)
	submission.AccountID = sandboxIdentity(now).AccountID
	doer := authenticatedRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		body := `{}`
		switch request.URL.Path {
		case "/api/v3/account":
			body = sandboxAccountJSON
		case "/api/v3/avgPrice":
			body = `{"mins":5,"price":"100","closeTime":1700000000000}`
		case "/api/v3/order":
			body = `{"symbol":"BTCUSDT","orderId":42}`
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	})
	client := sandboxTestClient(t, doer, now)
	lookup := &sandboxLookup{submissions: map[string]sandbox.Submission{
		submission.ClientOrderID: submission,
	}}
	adapter, err := newSandboxAdapterForTest(
		client,
		sandboxIdentity(now),
		1,
		lookup,
		&sandboxExpectations{},
		sandboxRules(t, now),
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = adapter.Submit(context.Background(), submission)
	if !errors.Is(err, ErrSandboxAmbiguous) {
		t.Fatalf("error=%v want ambiguous", err)
	}
}

func TestBinanceSandboxOrderNormalizationIncludesCumulativeFills(t *testing.T) {
	now := time.UnixMilli(1_700_000_001_000).UTC()
	submission := completeSandboxSubmission(
		sandboxSubmission(t, "100", "0.1", "10"),
		now.Add(-time.Second),
	)
	order := []byte(`{
	  "symbol":"BTCUSDT","orderId":42,"orderListId":-1,
	  "clientOrderId":"ax-00000001","price":"100","origQty":"0.1",
	  "executedQty":"0.1","cummulativeQuoteQty":"10","status":"FILLED",
	  "timeInForce":"GTC","type":"LIMIT","side":"BUY","stopPrice":"0",
	  "icebergQty":"0","time":1700000000000,"updateTime":1700000001000,
	  "isWorking":false,"workingTime":1700000000000,"origQuoteOrderQty":"0",
	  "selfTradePreventionMode":"NONE"
	}`)
	fills := []byte(`[{
	  "symbol":"BTCUSDT","id":7,"orderId":42,"orderListId":-1,
	  "price":"100","qty":"0.1","quoteQty":"10","commission":"0.01",
	  "commissionAsset":"USDT","time":1700000001000,
	  "isBuyer":true,"isMaker":false,"isBestMatch":true
	}]`)
	event, err := normalizeSandboxOrder(order, fills, submission, now)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if event.Kind != sandbox.PrivateFillEvent ||
		event.OrderEvent == nil ||
		len(event.OrderEvent.Fills) != 1 ||
		len(event.OrderEvent.Fees) != 1 ||
		event.OrderEvent.CumulativeQuantity.String() != "0.1" {
		t.Fatalf("unexpected fill event: %#v", event)
	}
	reduced, err := sandbox.ReducePrivateOrderEvents(
		submission,
		[]execution.OrderEvent{*event.OrderEvent},
	)
	if err != nil || reduced.State != execution.OrderFilled ||
		len(reduced.Fills) != 1 {
		t.Fatalf("canonical reduction failed: order=%#v err=%v", reduced, err)
	}
}

func TestBinanceSandboxPayloadFailureCodesAreClosed(t *testing.T) {
	submission := completeSandboxSubmission(
		sandboxSubmission(t, "100", "0.1", "10"),
		time.UnixMilli(1_700_000_001_000).UTC(),
	)
	_, err := normalizeSandboxOrder(
		[]byte(`{"unexpected":"private-value"}`),
		nil,
		submission,
		time.UnixMilli(1_700_000_001_000).UTC(),
	)
	if !errors.Is(err, ErrSandboxPayload) ||
		SandboxPayloadFailureCode(err) != "order_decode" ||
		strings.Contains(SandboxPayloadFailureCode(err), "private-value") {
		t.Fatalf(
			"payload failure code=%q error=%v",
			SandboxPayloadFailureCode(err),
			err,
		)
	}
	if code := SandboxPayloadFailureCode(errors.New("external private value")); code != "" {
		t.Fatalf("unreviewed error produced failure code %q", code)
	}
	if code := SandboxFailureCode(ErrSandboxRequest); code != "request" {
		t.Fatalf("closed request failure code=%q", code)
	}
}

func TestBinanceSandboxAdapterStatefulEmulatorCreateQueryCancel(t *testing.T) {
	now := time.UnixMilli(1_700_000_001_000).UTC()
	fixture := newBinanceEmulatorFixture(t, now, sandboxemulator.Config{
		Exchange:  sandbox.ExchangeBinance,
		APIKey:    "test-key",
		APISecret: "test-secret",
	}, "cfg-emulator", true)
	created, err := fixture.adapter.Submit(
		context.Background(), fixture.submission,
	)
	if err != nil || created.OrderEvent == nil ||
		created.OrderEvent.State != execution.OrderAcknowledged {
		t.Fatalf("create=%#v err=%v", created, err)
	}
	queried, err := fixture.adapter.Query(
		context.Background(), fixture.identity.AccountID, 1,
		fixture.submission.ClientOrderID,
	)
	if err != nil || len(queried) != 1 ||
		queried[0].OrderEvent.State != execution.OrderAcknowledged {
		t.Fatalf("query=%#v err=%v", queried, err)
	}
	canceled, err := fixture.adapter.Cancel(
		context.Background(), fixture.identity.AccountID, 1,
		fixture.submission.ClientOrderID,
	)
	if err != nil || canceled.OrderEvent == nil ||
		canceled.OrderEvent.State != execution.OrderCanceled {
		t.Fatalf("cancel=%#v err=%v", canceled, err)
	}
	if fixture.emulator.NativeOrderCount() != 1 ||
		len(fixture.emulator.PrivateFrames()) != 2 {
		t.Fatalf(
			"orders=%d frames=%d",
			fixture.emulator.NativeOrderCount(),
			len(fixture.emulator.PrivateFrames()),
		)
	}
}

func TestBinanceAmbiguousCreateRecoversByClientIDWithoutRetry(t *testing.T) {
	now := time.UnixMilli(1_700_000_001_000).UTC()
	fixture := newBinanceEmulatorFixture(t, now, sandboxemulator.Config{
		Exchange:  sandbox.ExchangeBinance,
		APIKey:    "test-key",
		APISecret: "test-secret",
		Faults: []sandboxemulator.Fault{
			sandboxemulator.FaultNone,
			sandboxemulator.FaultNone,
			sandboxemulator.FaultNone,
			sandboxemulator.FaultAmbiguousAfterCommit,
		},
	}, "cfg-ambiguous", false)
	if _, err := fixture.adapter.Submit(
		context.Background(), fixture.submission,
	); !errors.Is(err, ErrSandboxAmbiguous) {
		t.Fatalf("create error=%v", err)
	}
	recovered, err := fixture.adapter.Query(
		context.Background(), fixture.identity.AccountID, 1,
		fixture.submission.ClientOrderID,
	)
	if err != nil || len(recovered) != 1 ||
		recovered[0].OrderEvent == nil ||
		recovered[0].OrderEvent.State != execution.OrderAcknowledged ||
		fixture.emulator.NativeOrderCount() != 1 {
		t.Fatalf(
			"recovered=%#v native=%d err=%v",
			recovered,
			fixture.emulator.NativeOrderCount(),
			err,
		)
	}
}

func TestBinancePreflightReadFailureIsRejectedBeforeCreate(t *testing.T) {
	now := time.UnixMilli(1_700_000_001_000).UTC()
	emulator, err := sandboxemulator.New(sandboxemulator.Config{
		Exchange: sandbox.ExchangeBinance, APIKey: "test-key",
		APISecret: "test-secret",
		Faults:    []sandboxemulator.Fault{sandboxemulator.FaultTimeout},
	})
	if err != nil {
		t.Fatal(err)
	}
	client, err := newSandboxClientForTest(
		emulator,
		sandbox.CredentialPair{APIKey: "test-key", APISecret: "test-secret"},
		&captureEvidence{}, "cfg-preflight", func() time.Time { return now },
	)
	if err != nil {
		t.Fatal(err)
	}
	identity := sandboxIdentity(now)
	submission := completeSandboxSubmission(
		sandboxSubmission(t, "100", "0.1", "10"), now,
	)
	submission.AccountID = identity.AccountID
	lookup := &sandboxLookup{submissions: map[string]sandbox.Submission{
		submission.ClientOrderID: submission,
	}}
	adapter, err := newSandboxAdapterForTest(
		client, identity, 1, lookup, &sandboxExpectations{},
		sandboxRules(t, now),
	)
	if err != nil {
		t.Fatal(err)
	}
	event, err := adapter.Submit(context.Background(), submission)
	if err != nil || event.OrderEvent == nil ||
		event.OrderEvent.State != execution.OrderRejected ||
		emulator.NativeOrderCount() != 0 {
		t.Fatalf(
			"preflight event=%#v native=%d error=%v",
			event,
			emulator.NativeOrderCount(),
			err,
		)
	}
}

type binanceEmulatorFixture struct {
	emulator   *sandboxemulator.Emulator
	adapter    *SandboxAdapter
	identity   sandbox.AccountIdentity
	submission sandbox.Submission
}

func newBinanceEmulatorFixture(
	t *testing.T,
	now time.Time,
	emulatorConfig sandboxemulator.Config,
	configurationID string,
	fullStartup bool,
) binanceEmulatorFixture {
	t.Helper()
	emulator, err := sandboxemulator.New(emulatorConfig)
	if err != nil {
		t.Fatal(err)
	}
	client, err := newSandboxClientForTest(
		emulator,
		sandbox.CredentialPair{APIKey: "test-key", APISecret: "test-secret"},
		&captureEvidence{}, configurationID, func() time.Time { return now },
	)
	if err != nil {
		t.Fatal(err)
	}
	identity := sandboxIdentity(now)
	submission := completeSandboxSubmission(
		sandboxSubmission(t, "100", "0.1", "10"), now,
	)
	submission.AccountID = identity.AccountID
	lookup := &sandboxLookup{submissions: map[string]sandbox.Submission{
		submission.ClientOrderID: submission,
	}}
	adapter := buildBinanceEmulatorAdapter(
		t, client, identity, lookup, now, fullStartup,
	)
	return binanceEmulatorFixture{
		emulator: emulator, adapter: adapter,
		identity: identity, submission: submission,
	}
}

func buildBinanceEmulatorAdapter(
	t *testing.T,
	client *SandboxClient,
	identity sandbox.AccountIdentity,
	lookup *sandboxLookup,
	now time.Time,
	fullStartup bool,
) *SandboxAdapter {
	t.Helper()
	var adapter *SandboxAdapter
	var err error
	if fullStartup {
		adapter, err = NewSandboxAdapter(
			context.Background(), client, identity, 1,
			lookup, &sandboxExpectations{},
		)
	} else {
		adapter, err = newSandboxAdapterForTest(
			client, identity, 1, lookup,
			&sandboxExpectations{}, sandboxRules(t, now),
		)
	}
	if err != nil {
		t.Fatalf("adapter startup: %v", err)
	}
	return adapter
}

func completeSandboxSubmission(
	submission sandbox.Submission,
	at time.Time,
) sandbox.Submission {
	submission.RequestHash = strings.Repeat("1", 64)
	submission.PolicyHash = strings.Repeat("2", 64)
	submission.ApprovedAt = at
	return submission
}

func sandboxTestClient(
	t *testing.T,
	doer sandboxDoer,
	now time.Time,
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

func sandboxIdentity(now time.Time) sandbox.AccountIdentity {
	return sandbox.AccountIdentity{
		AccountID:            "binance-testnet-account",
		Exchange:             sandbox.ExchangeBinance,
		Environment:          sandbox.EnvironmentBinanceSpotTestnet,
		AccountIdentityHash:  strings.Repeat("a", 64),
		KeyFingerprint:       strings.Repeat("b", 32),
		CredentialGeneration: 1,
		OwnerAttested:        true,
		ValidatedAt:          now,
	}
}

func sandboxRules(
	t *testing.T,
	now time.Time,
) map[string]SandboxInstrumentRules {
	t.Helper()
	rules := make(map[string]SandboxInstrumentRules, 3)
	for _, instrument := range approvedSandboxInstruments() {
		payload := fullSandboxExchangeInfo
		payload = strings.ReplaceAll(payload, "BTCUSDT", instrument.Symbol())
		payload = strings.ReplaceAll(payload, `"baseAsset":"BTC"`, `"baseAsset":"`+string(instrument.Base)+`"`)
		payload = strings.ReplaceAll(payload, `"quoteAsset":"USDT"`, `"quoteAsset":"`+string(instrument.Quote)+`"`)
		normalized, err := normalizeSandboxRules([]byte(payload), instrument, now)
		if err != nil {
			t.Fatalf("rules for %s: %v", instrument.Symbol(), err)
		}
		rules[instrument.Symbol()] = normalized
	}
	return rules
}

const sandboxAccountJSON = `{
  "makerCommission":0,"takerCommission":0,"buyerCommission":0,"sellerCommission":0,
  "commissionRates":{"maker":"0","taker":"0","buyer":"0","seller":"0"},
  "canTrade":true,"canWithdraw":false,"canDeposit":true,"brokered":false,
  "requireSelfTradePrevention":false,"preventSor":false,
  "updateTime":1700000000000,"accountType":"SPOT",
  "balances":[
    {"asset":"BTC","free":"1","locked":"0"},
    {"asset":"ETH","free":"1","locked":"0"},
    {"asset":"USDT","free":"100","locked":"0"},
    {"asset":"IRRELEVANTASSET","free":"100","locked":"0"}
  ],
  "permissions":["SPOT"],"uid":12345
}`
