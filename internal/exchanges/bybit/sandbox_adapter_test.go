package bybit

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"axiom/internal/domain"
	"axiom/internal/exchanges/sandboxemulator"
	"axiom/internal/execution"
	"axiom/internal/sandbox"
)

func TestBybitSandboxMarketDataReusesClosedPublicTransport(t *testing.T) {
	now := time.UnixMilli(1_700_000_001_000).UTC()
	fixture := newBybitEmulatorFixture(t, now, sandboxemulator.Config{
		Exchange: sandbox.ExchangeBybit, APIKey: "test-key", APISecret: "test-secret",
	}, "cfg-market-transport")
	market, ok := fixture.adapter.marketData.(*PublicClient)
	if !ok {
		t.Fatalf("market data type = %T", fixture.adapter.marketData)
	}
	if market.httpClient != fixture.emulator {
		t.Fatalf("market transport = %T", market.httpClient)
	}
}

func TestBybitAutomaticMeanReversionDispatchUsesOnlyFencedDemoSpotIOC(t *testing.T) {
	now := time.UnixMilli(1_700_000_001_000).UTC()
	fixture := newBybitEmulatorFixture(t, now, sandboxemulator.Config{
		Exchange: sandbox.ExchangeBybit, APIKey: "test-key", APISecret: "test-secret",
	}, "cfg-automatic-mean-reversion")
	strategyID, err := domain.NewStrategyID(sandbox.StrategyMeanReversion)
	if err != nil {
		t.Fatal(err)
	}
	submission := fixture.submission
	submission.StrategyID = strategyID
	submission.Style = sandbox.OrderStyleLimitIOC
	lookup := fixture.adapter.lookup.(*demoLookup)
	lookup.submissions[submission.ClientOrderID] = submission
	repository := &bybitAutomaticDispatchRepository{submission: submission,
		worker: "automatic-mean-reversion-worker", fence: 9}
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
	assertBybitNoDuplicateDispatch(t, dispatcher, fixture.emulator, now)
	assertBybitAutomaticCreateFrame(t, fixture.emulator.PrivateFrames())
	expiredAt := submission.ApprovedAt.Add(sandbox.ArmLifetime + time.Second)
	if err = dispatcher.Cancel(context.Background(), submission.ClientOrderID, expiredAt); err != nil ||
		!repository.cancelling || repository.cancelEvent.OrderEvent == nil ||
		repository.cancelEvent.OrderEvent.State != execution.OrderCancelPending {
		t.Fatalf("post-expiry cancellation event=%#v cancelling=%t error=%v",
			repository.cancelEvent, repository.cancelling, err)
	}
	queried, queryErr := fixture.adapter.Query(context.Background(), submission.AccountID,
		submission.AccountEpoch, submission.ClientOrderID)
	if queryErr != nil || len(queried) != 1 || queried[0].OrderEvent == nil ||
		queried[0].OrderEvent.State != execution.OrderCanceled {
		t.Fatalf("post-expiry authoritative cancellation=%#v error=%v", queried, queryErr)
	}
	frames := fixture.emulator.PrivateFrames()
	if len(frames) != 2 || !strings.Contains(string(frames[1]), `"timeInForce":"IOC"`) ||
		!strings.Contains(string(frames[1]), `"category":"spot"`) {
		t.Fatalf("post-expiry cancellation lost Spot IOC order identity: %s", frames)
	}
	assertBybitAutomaticCreateCapture(t, fixture.emulator.Captures())
}

func assertBybitNoDuplicateDispatch(t *testing.T, dispatcher *sandbox.SandboxDispatcher,
	emulator *sandboxemulator.Emulator, now time.Time,
) {
	t.Helper()
	count, err := dispatcher.DispatchOnce(context.Background(), now.Add(time.Millisecond), 1)
	if err != nil || count != 0 || emulator.NativeOrderCount() != 1 {
		t.Fatalf("duplicate dispatch=%d native=%d error=%v", count, emulator.NativeOrderCount(), err)
	}
}

func assertBybitAutomaticCreateFrame(t *testing.T, frames [][]byte) {
	t.Helper()
	if len(frames) != 1 || !strings.Contains(string(frames[0]), `"timeInForce":"IOC"`) ||
		!strings.Contains(string(frames[0]), `"category":"spot"`) ||
		!strings.Contains(string(frames[0]), `"isLeverage":"0"`) {
		t.Fatalf("automatic order was not unleveraged Spot IOC: %s", frames)
	}
}

func assertBybitAutomaticCreateCapture(t *testing.T, captures []sandboxemulator.Capture) {
	t.Helper()
	var create sandboxemulator.Capture
	for _, capture := range captures {
		if capture.Method == "POST" && capture.Path == "/v5/order/create" {
			create = capture
		}
	}
	if create.Host != "api-demo.bybit.com" || create.Exchange != sandbox.ExchangeBybit ||
		create.RequestHash == "" || strings.Contains(strings.ToLower(strings.Join(create.FieldNames, ",")), "signature") {
		t.Fatalf("redacted Demo create capture=%#v", create)
	}
}

type bybitAutomaticDispatchRepository struct {
	submission  sandbox.Submission
	worker      string
	fence       uint64
	claimed     bool
	submitting  bool
	cancelling  bool
	event       sandbox.PrivateEvent
	cancelEvent sandbox.PrivateEvent
}

func (*bybitAutomaticDispatchRepository) ApprovePlan(context.Context, sandbox.ApprovedSandboxPlan,
	sandbox.SubmissionLimits, sandbox.KillPoint) error {
	return errors.New("automatic_test_plan_already_approved")
}

func (repository *bybitAutomaticDispatchRepository) ClaimOutbox(
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
	return []sandbox.SubmissionOutbox{{ID: "automatic-mean-reversion-outbox", Submission: repository.submission,
		State: sandbox.OutboxClaimed, ClaimOwner: worker, FencingToken: fence, UpdatedAt: now}}, nil
}

func (repository *bybitAutomaticDispatchRepository) MarkSubmitting(
	_ context.Context, outboxID string, fence uint64, _ time.Time, _ sandbox.KillPoint,
) error {
	if outboxID != "automatic-mean-reversion-outbox" || fence != repository.fence {
		return errors.New("automatic_test_submit_fence_rejected")
	}
	repository.submitting = true
	return nil
}

func (*bybitAutomaticDispatchRepository) MarkUnknown(context.Context, string, uint64, time.Time,
	sandbox.KillPoint) error {
	return errors.New("automatic_test_unexpected_unknown")
}

func (repository *bybitAutomaticDispatchRepository) MarkCancelPending(
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
	return "automatic-mean-reversion-cancel", nil
}

func (*bybitAutomaticDispatchRepository) MarkCancelUnknown(context.Context, string, uint64, time.Time,
	sandbox.KillPoint) error {
	return errors.New("automatic_test_unexpected_cancel_unknown")
}

func (repository *bybitAutomaticDispatchRepository) AppendPrivateEvent(
	_ context.Context, outboxID string, fence uint64, event sandbox.PrivateEvent, _ sandbox.KillPoint,
) error {
	if outboxID == "automatic-mean-reversion-cancel" {
		if fence != repository.fence || !repository.cancelling {
			return errors.New("automatic_test_cancel_ack_fence_rejected")
		}
		repository.cancelEvent = event
		return nil
	}
	if outboxID != "automatic-mean-reversion-outbox" || fence != repository.fence || !repository.submitting {
		return errors.New("automatic_test_ack_fence_rejected")
	}
	repository.event = event
	return nil
}

type demoLookup struct {
	submissions map[string]sandbox.Submission
	active      []sandbox.Submission
}

func (lookup *demoLookup) SubmissionByClientOrderID(
	_ context.Context,
	_ sandbox.AccountID,
	_ uint64,
	clientOrderID string,
) (sandbox.Submission, bool, error) {
	submission, found := lookup.submissions[clientOrderID]
	return submission, found, nil
}

func (lookup *demoLookup) ActiveSubmissions(
	context.Context,
	sandbox.AccountID,
	uint64,
) ([]sandbox.Submission, error) {
	return append([]sandbox.Submission(nil), lookup.active...), nil
}

type demoExpectations struct {
	expectation sandbox.SnapshotExpectation
	found       bool
}

func (expectations *demoExpectations) SnapshotExpectation(
	context.Context,
	sandbox.AccountID,
	uint64,
) (sandbox.SnapshotExpectation, bool, error) {
	return expectations.expectation, expectations.found, nil
}

func TestBybitDemoAdapterStatefulCreateQueryCancel(t *testing.T) {
	now := time.UnixMilli(1_700_000_001_000).UTC()
	fixture := newBybitEmulatorFixture(t, now, sandboxemulator.Config{
		Exchange:  sandbox.ExchangeBybit,
		APIKey:    "test-key",
		APISecret: "test-secret",
	}, "cfg-emulator")
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
		queried[0].OrderEvent == nil ||
		queried[0].OrderEvent.State != execution.OrderAcknowledged {
		t.Fatalf("query=%#v err=%v", queried, err)
	}
	cancelled, err := fixture.adapter.Cancel(
		context.Background(), fixture.identity.AccountID, 1,
		fixture.submission.ClientOrderID,
	)
	if err != nil || cancelled.OrderEvent == nil ||
		cancelled.OrderEvent.State != execution.OrderCancelPending {
		t.Fatalf("cancel ack=%#v err=%v", cancelled, err)
	}
	queried, err = fixture.adapter.Query(
		context.Background(), fixture.identity.AccountID, 1,
		fixture.submission.ClientOrderID,
	)
	if err != nil || len(queried) != 1 ||
		queried[0].OrderEvent.State != execution.OrderCanceled {
		t.Fatalf("authoritative cancel=%#v err=%v", queried, err)
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

func TestBybitDemoAmbiguousCreateRecoversWithoutRetry(t *testing.T) {
	now := time.UnixMilli(1_700_000_001_000).UTC()
	fixture := newBybitEmulatorFixture(t, now, sandboxemulator.Config{
		Exchange:  sandbox.ExchangeBybit,
		APIKey:    "test-key",
		APISecret: "test-secret",
		Faults: []sandboxemulator.Fault{
			sandboxemulator.FaultNone,
			sandboxemulator.FaultNone,
			sandboxemulator.FaultNone,
			sandboxemulator.FaultNone,
			sandboxemulator.FaultAmbiguousAfterCommit,
		},
	}, "cfg-ambiguous")
	if _, err := fixture.adapter.Submit(
		context.Background(), fixture.submission,
	); !errors.Is(err, ErrDemoAmbiguous) {
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

func TestBybitDemoQueryAcceptsDocumentedSpotShapeAndOpaqueCursor(
	t *testing.T,
) {
	now := time.UnixMilli(1_700_000_001_000).UTC()
	fixture := newBybitEmulatorFixture(t, now, sandboxemulator.Config{
		Exchange:  sandbox.ExchangeBybit,
		APIKey:    "test-key",
		APISecret: "test-secret",
	}, "cfg-documented-query")
	order := documentedDemoOrder(fixture.submission)
	order.Category = ""
	body, err := json.Marshal(responseEnvelope[orderListResult]{
		RetCode: 0,
		RetMsg:  "OK",
		Result: orderListResult{
			Category:       "spot",
			NextPageCursor: "opaque-cursor-for-exact-order-query",
			List:           []demoOrderPayload{order},
		},
		RetExtInfo: json.RawMessage(`{}`),
		Time:       now.UnixMilli(),
	})
	if err != nil {
		t.Fatal(err)
	}
	events, err := fixture.adapter.normalizeDemoQuery(
		context.Background(),
		fixture.submission,
		body,
	)
	if err != nil || len(events) != 1 ||
		events[0].OrderEvent == nil ||
		events[0].OrderEvent.State != execution.OrderAcknowledged {
		t.Fatalf("events=%#v error=%v", events, err)
	}
}

func TestBybitRESTCategoryBindingRejectsConflicts(t *testing.T) {
	orders := orderListResult{
		Category: "spot",
		List:     []demoOrderPayload{{Category: ""}},
	}
	if !bindDemoOrderCategories(&orders) ||
		orders.List[0].Category != "spot" {
		t.Fatal("REST order result category was not bound to its item")
	}
	orders.List[0].Category = "linear"
	if bindDemoOrderCategories(&orders) {
		t.Fatal("conflicting REST order item category accepted")
	}
	executions := executionListResult{
		Category: "spot",
		List:     []demoExecutionPayload{{Category: ""}},
	}
	if !bindDemoExecutionCategories(&executions) ||
		executions.List[0].Category != "spot" {
		t.Fatal("REST execution result category was not bound to its item")
	}
	executions.List[0].Category = "option"
	if bindDemoExecutionCategories(&executions) {
		t.Fatal("conflicting REST execution item category accepted")
	}
}

func TestBybitDemoQueryFallsBackToExactOrderHistory(t *testing.T) {
	now := time.UnixMilli(1_700_000_001_000).UTC()
	fixture := newBybitEmulatorFixture(t, now, sandboxemulator.Config{
		Exchange:  sandbox.ExchangeBybit,
		APIKey:    "test-key",
		APISecret: "test-secret",
	}, "cfg-history-fallback")
	if _, err := fixture.adapter.Submit(
		context.Background(), fixture.submission,
	); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(responseEnvelope[orderListResult]{
		RetCode: 0,
		RetMsg:  "OK",
		Result: orderListResult{
			Category: "spot",
			List:     []demoOrderPayload{},
		},
		RetExtInfo: json.RawMessage(`{}`),
		Time:       now.UnixMilli(),
	})
	if err != nil {
		t.Fatal(err)
	}
	events, err := fixture.adapter.normalizeDemoQuery(
		context.Background(),
		fixture.submission,
		body,
	)
	if err != nil || len(events) != 1 ||
		events[0].OrderEvent == nil ||
		events[0].OrderEvent.State != execution.OrderAcknowledged {
		t.Fatalf("events=%#v error=%v", events, err)
	}
}

func TestBybitDemoOrderDefaultsDoNotPermitConditionalOrders(t *testing.T) {
	now := time.UnixMilli(1_700_000_001_000).UTC()
	submission := demoSubmission(t, demoIdentity(now), now)
	order := documentedDemoOrder(submission)
	if !demoOrderMatches(order, submission) {
		t.Fatal("documented inactive Spot defaults rejected")
	}
	order.TakeProfit = "65000"
	if demoOrderMatches(order, submission) {
		t.Fatal("conditional take-profit order accepted")
	}
}

func TestBybitDemoOrderDefaultsDoNotPermitTrailingOrders(t *testing.T) {
	now := time.UnixMilli(1_700_000_001_000).UTC()
	submission := demoSubmission(t, demoIdentity(now), now)
	order := documentedDemoOrder(submission)
	order.ActivationPrice = "0"
	order.TrailingPercentage = "0"
	order.TrailingValue = "0"
	if !demoOrderMatches(order, submission) {
		t.Fatal("inactive Bybit Demo trailing fields rejected")
	}
	for field, mutate := range map[string]func(*demoOrderPayload){
		"activation price":    func(item *demoOrderPayload) { item.ActivationPrice = "1" },
		"trailing percentage": func(item *demoOrderPayload) { item.TrailingPercentage = "1" },
		"trailing value":      func(item *demoOrderPayload) { item.TrailingValue = "1" },
	} {
		t.Run(field, func(t *testing.T) {
			candidate := order
			mutate(&candidate)
			if demoOrderMatches(candidate, submission) {
				t.Fatal("active trailing-order field accepted")
			}
		})
	}
}

func TestBybitDemoExecutionFeeV2MustMirrorSpotFee(t *testing.T) {
	now := time.UnixMilli(1_700_000_001_000).UTC()
	submission := demoSubmission(t, demoIdentity(now), now)
	item := demoExecutionPayload{
		Category: "spot", Symbol: submission.Instrument.Symbol(),
		OrderID: "1000001", OrderLinkID: submission.ClientOrderID,
		Side: demoSide(submission.Side), OrderType: "Limit",
		ExecutionID: "execution-1", ExecutionType: "Trade",
		ExecutionPrice: submission.LimitPrice.String(),
		ExecutionQty:   submission.Quantity.String(),
		ExecutionFee:   "0.00000008", ExecutionFeeV2: "0.00000008",
		FeeCurrency: "BTC", MarketUnit: "baseCoin", IsLeverage: "",
		ExecutionTime: strconv.FormatInt(now.UnixMilli(), 10),
	}
	if _, _, _, _, err := normalizeDemoExecution(
		item, item.OrderID, submission,
	); err != nil {
		t.Fatalf("mirrored spot fee rejected: %v", err)
	}
	item.IsLeverage = "1"
	if _, _, _, _, err := normalizeDemoExecution(
		item, item.OrderID, submission,
	); err == nil {
		t.Fatal("leveraged execution accepted")
	}
	item.IsLeverage = ""
	item.ExecutionFeeV2 = "0.00000009"
	if _, _, _, _, err := normalizeDemoExecution(
		item, item.OrderID, submission,
	); err == nil {
		t.Fatal("mismatched spot execFeeV2 accepted")
	}
}

func documentedDemoOrder(submission sandbox.Submission) demoOrderPayload {
	return demoOrderPayload{
		Category: "spot", OrderID: "1000001",
		OrderLinkID: submission.ClientOrderID,
		Symbol:      submission.Instrument.Symbol(),
		Price:       submission.LimitPrice.String(),
		Quantity:    submission.Quantity.String(),
		Side:        demoSide(submission.Side),
		IsLeverage:  "0", PositionIndex: 0,
		OrderStatus: "New", AveragePrice: "0",
		LeavesQuantity:     submission.Quantity.String(),
		LeavesValue:        "0",
		CumulativeQuantity: "0",
		CumulativeValue:    "0",
		CumulativeFee:      "0",
		CumulativeFeeDetail: json.RawMessage(
			`{}`,
		),
		TimeInForce: "GTC", OrderType: "Limit",
		StopOrderType: "UNKNOWN", MarketUnit: "baseCoin",
		SlippageType: "UNKNOWN", SlippageTolerance: "0",
		TriggerPrice: "0", ActivationPrice: "0",
		TrailingPercentage: "0", TrailingValue: "0",
		TakeProfit: "0", StopLoss: "0",
		TPSLMode: "UNKNOWN", OCOTriggerBy: "OcoTriggerByUnknown",
		TPLimitPrice: "0", SLLimitPrice: "0",
		TPTriggerBy: "UNKNOWN", SLTriggerBy: "UNKNOWN",
		SMPType: "None", TriggerBy: "UNKNOWN",
		BasePrice: "64000", RPIMatchedQuantity: "0",
		CreatedTime: "1700000000001", UpdatedTime: "1700000000002",
		ExtraFees: json.RawMessage(`""`),
	}
}

func TestBybitDemoPreflightReadFailureIsRejectedBeforeCreate(t *testing.T) {
	now := time.UnixMilli(1_700_000_001_000).UTC()
	faults := make([]sandboxemulator.Fault, 64)
	for index := range faults {
		faults[index] = sandboxemulator.FaultTimeout
	}
	emulator, err := sandboxemulator.New(sandboxemulator.Config{
		Exchange: sandbox.ExchangeBybit, APIKey: "test-key",
		APISecret: "test-secret",
		Faults:    faults,
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
	identity := demoIdentity(now)
	submission := demoSubmission(t, identity, now)
	lookup := &demoLookup{submissions: map[string]sandbox.Submission{
		submission.ClientOrderID: submission,
	}}
	adapter, err := newSandboxAdapterForTest(
		client, identity, 1, lookup, &demoExpectations{}, demoRules(t, now),
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

type bybitEmulatorFixture struct {
	emulator   *sandboxemulator.Emulator
	adapter    *SandboxAdapter
	identity   sandbox.AccountIdentity
	submission sandbox.Submission
}

func newBybitEmulatorFixture(
	t *testing.T,
	now time.Time,
	emulatorConfig sandboxemulator.Config,
	configurationID string,
) bybitEmulatorFixture {
	t.Helper()
	emulator, err := sandboxemulator.New(emulatorConfig)
	if err != nil {
		t.Fatal(err)
	}
	client, err := newSandboxClientForTest(
		emulator,
		sandbox.CredentialPair{
			APIKey: "test-key", APISecret: "test-secret",
		},
		&captureEvidence{}, configurationID, func() time.Time { return now },
	)
	if err != nil {
		t.Fatal(err)
	}
	identity := demoIdentity(now)
	submission := demoSubmission(t, identity, now)
	lookup := &demoLookup{submissions: map[string]sandbox.Submission{
		submission.ClientOrderID: submission,
	}}
	adapter, err := NewSandboxAdapter(
		context.Background(), client, identity, 1,
		lookup, &demoExpectations{},
	)
	if err != nil {
		t.Fatalf("adapter startup: %v", err)
	}
	return bybitEmulatorFixture{
		emulator: emulator, adapter: adapter,
		identity: identity, submission: submission,
	}
}

func TestBybitDemoRulesRejectOffStepAndOversell(t *testing.T) {
	now := time.UnixMilli(1_700_000_000_000).UTC()
	rules := demoRules(t, now)[mustDemoInstrument(t).Symbol()]
	submission := demoSubmission(t, demoIdentity(now), now)
	owned, _ := domain.ParseBalance("1")
	if err := rules.validateSubmission(submission, owned); err != nil {
		t.Fatalf("valid submission: %v", err)
	}
	offStep := submission
	offStep.Quantity, _ = domain.ParseQuantity("0.100000001")
	if !errors.Is(rules.validateSubmission(offStep, owned), ErrDemoFilter) {
		t.Fatal("off-step quantity accepted")
	}
	oversell := submission
	oversell.Side = domain.SideSell
	oversell.Quantity, _ = domain.ParseQuantity("2")
	oversell.Notional, _ = domain.ParseNotional("200")
	if !errors.Is(rules.validateSubmission(oversell, owned), ErrDemoFilter) {
		t.Fatal("oversell accepted")
	}
}

func demoIdentity(now time.Time) sandbox.AccountIdentity {
	return sandbox.AccountIdentity{
		AccountID:            "bybit-demo-account",
		Exchange:             sandbox.ExchangeBybit,
		Environment:          sandbox.EnvironmentBybitDemo,
		AccountIdentityHash:  strings.Repeat("a", 64),
		KeyFingerprint:       strings.Repeat("b", 32),
		CredentialGeneration: 1,
		OwnerAttested:        true,
		ValidatedAt:          now,
	}
}

func demoSubmission(
	t *testing.T,
	identity sandbox.AccountIdentity,
	now time.Time,
) sandbox.Submission {
	t.Helper()
	instrument := mustDemoInstrument(t)
	planID, _ := domain.NewExecutionPlanID("bybit-plan")
	orderID, _ := domain.NewVirtualOrderID("bybit-order")
	strategyID, _ := domain.NewStrategyID(sandbox.StrategySandboxCanary)
	quantity, _ := domain.ParseQuantity("0.1")
	price, _ := domain.ParsePrice("100")
	notional, _ := domain.ParseNotional("10")
	return sandbox.Submission{
		PlanID:        planID,
		OrderID:       orderID,
		AccountID:     identity.AccountID,
		AccountEpoch:  1,
		ClientOrderID: "ax-00000001",
		StrategyID:    strategyID,
		Instrument:    instrument,
		Side:          domain.SideBuy,
		Quantity:      quantity,
		LimitPrice:    price,
		Notional:      notional,
		Style:         sandbox.OrderStyleLimitGTC,
		Action:        sandbox.IntentEntry,
		RequestHash:   strings.Repeat("1", 64),
		PolicyHash:    strings.Repeat("2", 64),
		ApprovedAt:    now,
	}
}

func mustDemoInstrument(t *testing.T) domain.Instrument {
	t.Helper()
	instrument, err := domain.NewSpotInstrument("BTC", "USDT")
	if err != nil {
		t.Fatal(err)
	}
	return instrument
}

func demoRules(
	t *testing.T,
	now time.Time,
) map[string]DemoInstrumentRules {
	t.Helper()
	result := make(map[string]DemoInstrumentRules, 3)
	for _, instrument := range approvedInstruments() {
		step, _ := domain.ParseQuantity("0.00000001")
		minimum, _ := domain.ParseQuantity("0.00000001")
		maximum, _ := domain.ParseQuantity("1000")
		tick, _ := domain.ParsePrice("0.00000001")
		minimumAmount, _ := domain.ParseNotional("0.00000001")
		maximumAmount, _ := domain.ParseNotional("1000000")
		result[instrument.Symbol()] = DemoInstrumentRules{
			Instrument:         instrument,
			QuantityStep:       step,
			MinimumQuantity:    minimum,
			MaximumQuantity:    maximum,
			MaximumLimitQty:    maximum,
			PostOnlyMaximumQty: maximum,
			PriceTick:          tick,
			MinimumOrderAmount: minimumAmount,
			MaximumOrderAmount: maximumAmount,
			ObservedAt:         now,
			SourceHash:         strings.Repeat("c", 64),
		}
	}
	return result
}
