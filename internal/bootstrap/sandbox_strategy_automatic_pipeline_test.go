package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"axiom/internal/config"
	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/risk"
	"axiom/internal/sandbox"
)

// This test exercises the complete successful one-leg automatic path with the
// real strategy, allocator, central-risk adapter, planner, and sandbox plan
// builder. Exchange transport remains deliberately downstream of this path;
// authenticated adapter emulator tests consume the same closed Submission
// contract after the durable dispatcher claims it.
func TestSandboxStrategyDecisionExecutorApprovesArmedAutomaticOneLegPlans(t *testing.T) {
	tests := []automaticStrategyTestCase{
		{name: "trend on Binance Spot Testnet", strategy: sandbox.StrategyTrend,
			exchange: sandbox.ExchangeBinance, now: time.Date(2026, 8, 5, 12, 0, 3, 0, time.UTC),
			market: automaticTrendMarketData},
		{name: "mean reversion on Bybit Demo Spot", strategy: sandbox.StrategyMeanReversion,
			exchange: sandbox.ExchangeBybit, now: time.Date(2026, 7, 22, 12, 0, 3, 100_000_000, time.UTC),
			market: automaticMeanReversionMarketData},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) { runAutomaticStrategyPlanTest(t, test) })
	}
}

type automaticStrategyTestCase struct {
	name     string
	strategy string
	exchange sandbox.Exchange
	now      time.Time
	market   func(*testing.T, time.Time, sandbox.Exchange) *readinessMarketData
}

func runAutomaticStrategyPlanTest(t *testing.T, test automaticStrategyTestCase) {
	t.Helper()
	work, record := automaticStrategyWorkAndConfiguration(t, test.strategy, test.exchange, test.now)
	executor, store, riskHarness, inventory, decisions := automaticStrategyTestExecutor(t, test, work)
	first, err := executor.EvaluateStrategySession(
		context.Background(), work, record, readinessExecutionLease(work), test.now)
	if err != nil || first.State != sandbox.StrategySessionEvaluationWaiting ||
		first.Reason != "waiting_for_risk_projection" || len(store.plans) != 0 {
		t.Fatalf("baseline evaluation=%#v plans=%d error=%v", first, len(store.plans), err)
	}
	second, err := executor.EvaluateStrategySession(
		context.Background(), work, record, readinessExecutionLease(work), test.now)
	if err != nil || second.State != sandbox.StrategySessionEvaluationEvaluated ||
		second.Reason != "strategy_plan_approved" || len(store.plans) != 1 {
		t.Fatalf("evaluated=%#v plans=%d store_calls=%d rejected=%#v decision=%s error=%v",
			second, len(store.plans), store.calls, store.rejected, decisions.canonicalDecision, err)
	}
	assertAutomaticStrategyPlan(t, store.plans[0], store.limits, work, test.now)
	if riskHarness.projectCalls != 2 || riskHarness.engineCalls != 1 || inventory.calls != 1 || decisions.calls != 0 {
		t.Fatalf("risk projections=%d engines=%d inventory=%d no-plan journals=%d",
			riskHarness.projectCalls, riskHarness.engineCalls, inventory.calls, decisions.calls)
	}
}

func automaticStrategyTestExecutor(t *testing.T, test automaticStrategyTestCase,
	work sandbox.StrategySessionWork,
) (*SandboxStrategyDecisionExecutor, *automaticStrategyPlanStore, *automaticStrategyRiskHarness,
	*automaticStrategyInventory, *automaticStrategyDecisionRecorder) {
	t.Helper()
	store, riskHarness := &automaticStrategyPlanStore{}, &automaticStrategyRiskHarness{}
	factory, err := NewSandboxStrategyPipelineFactory(riskHarness, riskHarness, store)
	if err != nil {
		t.Fatal(err)
	}
	projector, err := NewSandboxStrategyPositionProjector(projectorJournal{}, projectorExecutions{})
	if err != nil {
		t.Fatal(err)
	}
	inventory, decisions := &automaticStrategyInventory{}, &automaticStrategyDecisionRecorder{}
	clock, err := domain.NewReplayClock(test.now)
	if err != nil {
		t.Fatal(err)
	}
	executor, err := NewSandboxStrategyDecisionExecutor(test.market(t, test.now, test.exchange), clock,
		projector, &decisionExecutorFacts{facts: automaticStrategyFacts(t, work, test.now)}, riskHarness,
		&decisionExecutorAdmission{admission: sizingFactsAdmission(work, test.now)}, inventory, factory, decisions)
	if err != nil {
		t.Fatal(err)
	}
	return executor, store, riskHarness, inventory, decisions
}

func automaticStrategyWorkAndConfiguration(
	t *testing.T,
	strategy string,
	exchange sandbox.Exchange,
	now time.Time,
) (sandbox.StrategySessionWork, sandbox.StrategySessionConfiguration) {
	t.Helper()
	mode := config.ModeTestnet
	if exchange == sandbox.ExchangeBybit {
		mode = config.ModeDemo
	}
	product, err := config.DefaultSandboxConfiguration(mode)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := config.NewSnapshot(product, config.SourceAdmin, "automatic-strategy-test", &domain.SystemClock{})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(product)
	if err != nil {
		t.Fatal(err)
	}
	work := sandbox.StrategySessionWork{SessionID: sandbox.SessionID("automatic-" + strategy),
		Strategy: strategy, Instrument: "BTCUSDT",
		Account: sandbox.StrategySessionAccount{ID: sandbox.AccountID(string(exchange) + "-automatic-account"),
			Epoch: 1, Exchange: exchange},
		ConfigurationID: "configuration-" + string(exchange), ConfigurationHash: snapshot.Hash(),
		StrategySetHash: strings.Repeat("a", 64), SessionRevision: 1, StrategyRevision: 1,
		ArmID: "arm-" + string(exchange), ArmRevision: 1,
		StartedAt: now.Add(-time.Minute), ArmExpiresAt: now.Add(time.Minute)}
	return work, sandbox.StrategySessionConfiguration{ID: work.ConfigurationID, Hash: work.ConfigurationHash, Payload: payload}
}

func automaticStrategyFacts(
	t *testing.T,
	work sandbox.StrategySessionWork,
	now time.Time,
) SandboxStrategySizingFacts {
	t.Helper()
	available, err := domain.ParseBalance("500")
	if err != nil {
		t.Fatal(err)
	}
	minimum := mustSizingFactsMoney(t, "75")
	maximumReserved := mustSizingFactsMoney(t, "425")
	maximumOrder := mustSizingFactsMoney(t, "10")
	zeroPrice, _ := domain.ParsePrice("0")
	fee, _ := domain.ParseRate("0.001")
	policy := sizingFactsRiskPolicy("automatic-risk-policy", 1)
	return SandboxStrategySizingFacts{
		AccountSnapshot: sandbox.AccountSnapshot{AccountID: work.Account.ID, Epoch: work.Account.Epoch,
			Balances:   []sandbox.Balance{{Asset: "USDT", Available: available}},
			OrdersHash: strings.Repeat("1", 64), FillsHash: strings.Repeat("2", 64),
			SnapshotHash: strings.Repeat("3", 64), ObservedAt: now},
		CentralRiskFacts: sandbox.StrategyRiskFacts{AccountID: work.Account.ID, AccountEpoch: work.Account.Epoch,
			SnapshotHash: strings.Repeat("3", 64), PolicyID: policy.ID, PolicyVersion: policy.Version,
			PolicyHash: strings.Repeat("4", 64), Policy: policy, MinimumReserve: minimum,
			MaximumReserved: maximumReserved, ObservedAt: now},
		PortfolioRevision: 1, PositionRevision: work.StrategyRevision, AssetEligibility: 1,
		RiskPolicyID: policy.ID, RiskPolicyVersion: policy.Version, RiskPolicyHash: strings.Repeat("4", 64),
		MinimumReserve: minimum, MaximumReserved: maximumReserved, MaximumOrderNotional: maximumOrder,
		EntryFeeRate: fee, ExitFeeRate: fee, GapAllowance: zeroPrice,
		LatencyDeterioration: zeroPrice, SlippageAllowance: zeroPrice,
		LiquidityDomain: "automatic-" + string(work.SessionID), FencingToken: 1,
		EvaluationOrdinal: 1, EvaluationLogicalTime: 1, ConfigurationHash: work.ConfigurationHash,
		ConfigurationVersion: config.SchemaVersionSandboxRuntime, FeeModelID: "fixed-bps-v1",
		LatencyModelID: "fixed-zero-v1", FillModelID: "fill-v1", SlippageModelID: "slippage-v1",
		GapModelID: "gap-v1", CorrelationModelID: "strategy-set-v1",
	}
}

func automaticTrendMarketData(
	t *testing.T,
	now time.Time,
	exchange sandbox.Exchange,
) *readinessMarketData {
	t.Helper()
	_, market, _ := validSandboxTrendInputBuilderFacts(t, now)
	market.Metadata.Metadata.MinimumNotional = ownerConsoleE2ENotional(t, "0.00000001")
	market.Candles["4h"] = append(
		automaticFlatCandles(t, market.Instrument, "4h", string(exchange),
			market.Candles["4h"][0].OpenTime, 800, "100"),
		market.Candles["4h"]...,
	)
	for index := range market.Candles["4h"] {
		market.Candles["4h"][index].Exchange = exchangecontracts.ExchangeID(exchange)
		market.Candles["4h"][index].RawPayloadHash = projectorHash(fmt.Sprintf("trend-%d", index))
	}
	return &readinessMarketData{metadata: market.Metadata, book: market.Book, candles: market.Candles}
}

func automaticMeanReversionMarketData(
	t *testing.T,
	now time.Time,
	exchange sandbox.Exchange,
) *readinessMarketData {
	t.Helper()
	instrument, err := domain.NewSpotInstrument("BTC", "USDT")
	if err != nil {
		t.Fatal(err)
	}
	signalEnd := now.Add(-3100 * time.Millisecond)
	primary := automaticFlatCandles(t, instrument, "1h", string(exchange), signalEnd.Add(-28*time.Hour), 972, "100")
	for index := 0; index < 28; index++ {
		closeValue := "100"
		if index%2 == 1 {
			closeValue = "101"
		}
		if index == 27 {
			closeValue = "95"
		}
		open := signalEnd.Add(time.Duration(index-28) * time.Hour)
		primary = append(primary, automaticCandle(t, instrument, "1h", string(exchange), open, time.Hour,
			closeValue, len(primary)+1))
	}
	higher := automaticFlatCandles(t, instrument, "4h", string(exchange), signalEnd, 1000, "100")
	priceTick, _ := domain.ParsePrice("0.01")
	quantityStep, _ := domain.ParseQuantity("0.00001")
	minimum, _ := domain.ParseNotional("0.00000001")
	bid, _ := domain.ParsePrice("95.05")
	ask, _ := domain.ParsePrice("95.1")
	depth, _ := domain.ParseQuantity("10")
	metadata := exchangecontracts.InstrumentRecord{Metadata: domain.InstrumentMetadata{Instrument: instrument,
		Version: 1, EffectiveAt: signalEnd.Add(-1000 * 4 * time.Hour), PriceTick: priceTick,
		QuantityStep: quantityStep, MinimumQuantity: quantityStep, MinimumNotional: minimum},
		RawPayloadHash: projectorHash("mean-reversion-metadata")}
	book := exchangecontracts.BookSnapshot{Instrument: instrument, LastSequence: 1,
		ReceivedAt:     domain.EventTime{UTC: now, Sequence: 1},
		Bids:           []exchangecontracts.PriceLevel{{Price: bid, Quantity: depth}},
		Asks:           []exchangecontracts.PriceLevel{{Price: ask, Quantity: depth}},
		RawPayloadHash: projectorHash("mean-reversion-book")}
	return &readinessMarketData{metadata: metadata, book: book,
		candles: map[string][]exchangecontracts.Candle{"1h": primary, "4h": higher}}
}

// automaticFlatCandles ends immediately at before and walks backwards so a
// caller can prepend history without changing the strategy's signal window.
func automaticFlatCandles(
	t *testing.T,
	instrument domain.Instrument,
	interval string,
	exchange string,
	before time.Time,
	count int,
	closeValue string,
) []exchangecontracts.Candle {
	t.Helper()
	width := time.Hour
	if interval == "4h" {
		width = 4 * time.Hour
	}
	items := make([]exchangecontracts.Candle, 0, count)
	start := before.Add(-time.Duration(count) * width)
	for index := 0; index < count; index++ {
		items = append(items, automaticCandle(t, instrument, interval, exchange,
			start.Add(time.Duration(index)*width), width, closeValue, index+1))
	}
	return items
}

func automaticCandle(
	t *testing.T,
	instrument domain.Instrument,
	interval string,
	exchange string,
	openTime time.Time,
	width time.Duration,
	closeValue string,
	sequence int,
) exchangecontracts.Candle {
	t.Helper()
	closePrice, err := domain.ParsePrice(closeValue)
	if err != nil {
		t.Fatal(err)
	}
	highValue := "101"
	lowValue := "99"
	if closeValue == "101" {
		highValue, lowValue = "102", "100"
	} else if closeValue == "95" {
		highValue, lowValue = "96", "94"
	}
	high, _ := domain.ParsePrice(highValue)
	low, _ := domain.ParsePrice(lowValue)
	volume, _ := domain.ParseQuantity("1")
	closeTime := openTime.Add(width)
	return exchangecontracts.Candle{Exchange: exchangecontracts.ExchangeID(exchange), Instrument: instrument, Interval: interval,
		OpenTime: openTime, CloseTime: closeTime, Open: closePrice, High: high, Low: low, Close: closePrice,
		Volume: volume, Closed: true, ReceivedAt: domain.EventTime{UTC: closeTime.Add(100 * time.Millisecond),
			Sequence: uint64(sequence)}, RawPayloadHash: projectorHash(fmt.Sprintf("%s-%s-%d-%d", exchange, interval, openTime.Unix(), sequence))}
}

type automaticStrategyRiskHarness struct {
	projectCalls int
	engineCalls  int
	observation  sandbox.StrategyRiskObservation
}

func (harness *automaticStrategyRiskHarness) ProjectStrategyRiskObservation(
	_ context.Context,
	_ sandbox.StrategySessionExecutionLease,
	admission sandbox.StrategySessionAdmission,
	snapshot sandbox.AccountSnapshot,
	market sandbox.StrategyMarketInput,
	facts sandbox.StrategyRiskFacts,
	now time.Time,
) (sandbox.StrategyRiskObservation, error) {
	work := admission.Work
	harness.projectCalls++
	if harness.projectCalls == 1 {
		return sandbox.StrategyRiskObservation{}, fmt.Errorf("automatic_strategy_risk_baseline_created")
	}
	zero, err := domain.ParsePercent("0")
	if err != nil {
		return sandbox.StrategyRiskObservation{}, err
	}
	one, err := domain.ParsePercent("1")
	if err != nil {
		return sandbox.StrategyRiskObservation{}, err
	}
	observation := sandbox.StrategyRiskObservation{StrategySessionID: work.SessionID,
		StrategyRevision: work.StrategyRevision, AccountID: work.Account.ID,
		AccountEpoch: work.Account.Epoch, SnapshotHash: snapshot.SnapshotHash,
		MarketHash: sandbox.StrategyMarketEvidenceHash(market), Instrument: work.Instrument,
		PolicyID: facts.PolicyID, PolicyVersion: facts.PolicyVersion, PolicyHash: facts.PolicyHash,
		AccountDrawdown: zero, UTCDayLoss: zero, Rolling24HourLoss: zero, StrategyLoss: zero,
		AssetExposure: zero, CombinedExposure: zero, ExchangeExposure: zero, Reserve: one,
		ReservedCapital: zero, Slippage: zero, QualityScore: 100, ObservedAt: now}
	spread, err := strategyBookSpread(market.Book.Bids[0].Price, market.Book.Asks[0].Price)
	if err != nil {
		return sandbox.StrategyRiskObservation{}, err
	}
	observation.Spread = spread
	observation.BookAge = now.Sub(market.Book.ReceivedAt.UTC)
	harness.observation = observation
	return observation, nil
}

func (harness *automaticStrategyRiskHarness) StrategyRiskObservation(
	_ context.Context,
	work sandbox.StrategySessionWork,
	snapshot sandbox.AccountSnapshot,
	market sandbox.StrategyMarketInput,
	facts sandbox.StrategyRiskFacts,
	now time.Time,
) (sandbox.StrategyRiskObservation, error) {
	if harness.observation.ValidFor(work, snapshot, market, facts, now) != nil {
		return sandbox.StrategyRiskObservation{}, fmt.Errorf("automatic_strategy_risk_observation_unavailable")
	}
	return harness.observation, nil
}

func (harness *automaticStrategyRiskHarness) SandboxStrategyRiskEngine(
	context.Context,
	time.Time,
) (*risk.Engine, error) {
	harness.engineCalls++
	return risk.NewRestoredEngine(risk.StateNormal, &pipelineFactoryRiskAudit{}, pipelineFactoryRiskAlerts{})
}

type automaticStrategyInventory struct{ calls int }

func (source *automaticStrategyInventory) StrategyOwnedInventory(
	_ context.Context,
	work sandbox.StrategySessionWork,
	asset domain.AssetSymbol,
	now time.Time,
) (sandbox.StrategyOwnedInventory, error) {
	source.calls++
	zero, err := domain.ParseBalance("0")
	if err != nil {
		return sandbox.StrategyOwnedInventory{}, err
	}
	return sandbox.StrategyOwnedInventory{SessionID: work.SessionID, AccountID: work.Account.ID,
		AccountEpoch: work.Account.Epoch, Asset: asset, Available: zero,
		EvidenceHash: projectorHash("automatic-empty-owned-inventory"), ObservedAt: now}, nil
}

type automaticStrategyDecisionRecorder struct {
	calls             int
	canonicalDecision []byte
}

func (recorder *automaticStrategyDecisionRecorder) RecordSandboxStrategyDecision(
	_ context.Context,
	_ string,
	_ uint64,
	_ sandbox.StrategySessionWork,
	evidence sandbox.StrategyDecisionEvidence,
	_ time.Time,
) error {
	recorder.calls++
	recorder.canonicalDecision = append([]byte(nil), evidence.CanonicalDecision...)
	return nil
}

type automaticStrategyPlanStore struct {
	pipelineFactoryRepository
	calls    int
	plans    []sandbox.ApprovedSandboxPlan
	rejected sandbox.ApprovedSandboxPlan
	limits   sandbox.SubmissionLimits
}

func (store *automaticStrategyPlanStore) ApprovePlan(
	_ context.Context,
	plan sandbox.ApprovedSandboxPlan,
	limits sandbox.SubmissionLimits,
	_ sandbox.KillPoint,
) error {
	store.calls++
	maximum, err := domain.ParseNotional(limits.MaximumOrderNotional)
	if err != nil || limits.MaximumOrderNotional != "10" || limits.MaximumDailyNotional != "50" ||
		limits.MaximumOpenPerAccount != 1 || limits.MaximumOpenGlobal != 2 ||
		len(plan.Submissions) != 1 || len(plan.Reservations) != 1 ||
		plan.Pipeline.ValidateFor(plan) != nil || plan.Submissions[0].Validate(maximum) != nil ||
		plan.Reservations[0].ValidateFor(plan.Submissions[0]) != nil {
		store.rejected = plan
		return fmt.Errorf("automatic_strategy_plan_invalid")
	}
	store.plans = append(store.plans, plan)
	store.limits = limits
	return nil
}

func assertAutomaticStrategyPlan(
	t *testing.T,
	plan sandbox.ApprovedSandboxPlan,
	limits sandbox.SubmissionLimits,
	work sandbox.StrategySessionWork,
	now time.Time,
) {
	t.Helper()
	submission := plan.Submissions[0]
	wantsArm, err := sandbox.RequiresEntryArm(plan)
	_, sagaErr := sandbox.ValidatePlanSaga(plan)
	topologyErr := sandbox.ValidateSubmissionTopology(plan.Submissions,
		map[sandbox.AccountID]sandbox.Exchange{work.Account.ID: work.Account.Exchange})
	if err != nil || sagaErr != nil || topologyErr != nil || !wantsArm ||
		plan.SessionID != work.SessionID || plan.ConfigurationID != work.ConfigurationID ||
		plan.StrategyDecision == nil || plan.StrategyDecision.ValidForPlan(plan) != nil ||
		plan.Pipeline.IntentKind != sandbox.ApprovalStrategyIntent || !plan.Pipeline.RiskApproved ||
		!plan.Pipeline.AssetApproved || submission.AccountID != work.Account.ID ||
		submission.AccountEpoch != work.Account.Epoch || submission.StrategyID.Value() != work.Strategy ||
		submission.Instrument.Symbol() != work.Instrument || submission.Side != domain.SideBuy ||
		submission.Style != sandbox.OrderStyleLimitIOC || submission.Action != sandbox.IntentEntry ||
		!submission.ApprovedAt.Equal(now) || !plan.Arm.Active(now) ||
		limits.MaximumOrderNotional != "10" || limits.MaximumDailyNotional != "50" ||
		limits.MaximumOpenPerAccount != 1 || limits.MaximumOpenGlobal != 2 {
		t.Fatalf("plan=%#v limits=%#v arm=%t errors=%v/%v/%v", plan, limits, wantsArm, err, sagaErr, topologyErr)
	}
}
