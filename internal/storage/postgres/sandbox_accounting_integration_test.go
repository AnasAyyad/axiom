package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/execution"
	"axiom/internal/replay"
	"axiom/internal/risk"
	"axiom/internal/sandbox"

	"github.com/jackc/pgx/v5/pgxpool"
)

// assertSandboxStrategyFillAccounting runs inside the clean-install database
// qualification after a real automatic strategy session has been admitted.
// It proves that a private fill, its immutable journal, and reservation close
// either commit together or all roll back after an injected crash.
func assertSandboxStrategyFillAccounting(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	store *SandboxRuntimeDispatcherStore,
	admission sandbox.StrategySessionAdmission,
) time.Time {
	t.Helper()
	approvedAt := admission.ApprovedAt
	plan := sandboxAccountingIntegrationPlan(t, ctx, store, admission, approvedAt)
	limits := sandbox.SubmissionLimits{
		MaximumOrderNotional: "10", MaximumDailyNotional: "50",
		MaximumOpenPerAccount: 1, MaximumOpenGlobal: 2,
	}
	if err := store.ApprovePlan(ctx, plan, limits, sandbox.NoKillPoint{}); err != nil {
		t.Fatalf("automatic accounting plan approval failed: %v", err)
	}
	claimed, err := store.ClaimOutbox(
		ctx, admission.Work.Account.ID, admission.Work.Account.Epoch,
		"sandbox_runtime-engine-runtime-worker", 1, approvedAt, time.Minute, 1, sandbox.NoKillPoint{},
	)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("automatic accounting outbox claim=%d error=%v", len(claimed), err)
	}
	if err = store.MarkSubmitting(ctx, claimed[0].ID, 1, approvedAt, sandbox.NoKillPoint{}); err != nil {
		t.Fatalf("automatic accounting submission transition failed: %v", err)
	}
	event := sandboxAccountingIntegrationFill(t, claimed[0].Submission, approvedAt.Add(time.Millisecond))
	err = store.AppendPrivateEvent(ctx, claimed[0].ID, 1, event,
		&sandboxRuntimePostgresCrashOnce{boundary: sandbox.KillBeforeReservationRelease})
	if !errors.Is(err, sandbox.ErrInjectedCrash) {
		t.Fatalf("automatic accounting crash result=%v", err)
	}
	assertSandboxAccountingRowCounts(t, ctx, pool, plan, event.NativeFillHash, 0, 0, 0)

	if err = store.AppendPrivateEvent(ctx, claimed[0].ID, 1, event, sandbox.NoKillPoint{}); err != nil {
		t.Fatalf("automatic accounting fill recovery failed: %v", err)
	}
	if err = store.AppendPrivateEvent(ctx, claimed[0].ID, 1, event, sandbox.NoKillPoint{}); err != nil {
		t.Fatalf("automatic accounting exact replay failed: %v", err)
	}
	assertSandboxAccountingRowCounts(t, ctx, pool, plan, event.NativeFillHash, 1, 1, 1)
	assertSandboxAccountingCommittedHeader(t, ctx, pool, plan, claimed[0].Submission)
	assertSandboxAccountingCommittedBalance(t, ctx, pool, plan.ID)
	assertSandboxAccountingCommittedProjection(t, ctx, pool, plan, claimed[0].Submission)
	return assertSandboxStrategyRiskProjection(t, ctx, pool, store, admission)
}

func assertSandboxStrategyRiskProjection(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	store *SandboxRuntimeDispatcherStore,
	prior sandbox.StrategySessionAdmission,
) time.Time {
	t.Helper()
	now := prior.ApprovedAt.Add(20 * time.Millisecond).Truncate(time.Microsecond)
	seedSandboxRiskProjectionState(t, ctx, pool, now)
	seedSandboxRiskProjectionPolicy(t, ctx, pool, now)
	snapshot := seedSandboxRiskProjectionFacts(t, ctx, store, prior, now)
	work, admission, facts := sandboxRiskProjectionAdmission(t, ctx, store, prior, snapshot, now)
	market := sandboxRiskProjectionMarket(now)
	assertSandboxRiskProjectionEvidence(t, ctx, pool, store, work, admission, snapshot, market, facts, now)
	return assertSandboxRiskRuntime(t, ctx, pool, now)
}

func seedSandboxRiskProjectionState(t *testing.T, ctx context.Context, pool *pgxpool.Pool, now time.Time) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
UPDATE owner_console_storage_pressure_state
SET level='NORMAL',available_bytes=21474836480,total_bytes=42949672960,
    revision=revision+1,observed_at=$1,source_instance='risk-projection-test'
WHERE scope_id='market-data'`, now); err != nil {
		t.Fatalf("risk projection storage fixture failed: %v", err)
	}
	currentRiskState, riskRevision := "PAUSED", int64(0)
	_ = pool.QueryRow(ctx, `
SELECT next_state,entity_revision FROM risk_state_events
ORDER BY entity_revision DESC LIMIT 1`).Scan(&currentRiskState, &riskRevision)
	if currentRiskState != "NORMAL" {
		if _, err := pool.Exec(ctx, `
INSERT INTO risk_state_events(
 id,prior_state,next_state,reason_code,actor,evidence_hash,occurred_at,entity_revision
) VALUES(
 'sandbox-strategy-risk-normal',$1,'NORMAL','integration_ready','system',$2,$3,$4
)`, currentRiskState, strings.Repeat("c", 64), now.Add(-time.Second), riskRevision+1); err != nil {
			t.Fatalf("risk projection state fixture failed: %v", err)
		}
		if _, err := pool.Exec(ctx, `
UPDATE api_entity_revisions
SET revision=$1,updated_at=$2
WHERE entity_type='risk' AND entity_id='global'`, riskRevision+1, now.Add(-time.Second)); err != nil {
			t.Fatalf("risk projection revision fixture failed: %v", err)
		}
	}
}

func seedSandboxRiskProjectionPolicy(t *testing.T, ctx context.Context, pool *pgxpool.Pool, now time.Time) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO risk_policies(
 id,version,scope_kind,scope_id,state,policy_hash,canonical_payload,effective_at,recorded_at
) VALUES(
 'sandbox-strategy-risk-policy',1,'global','platform','NORMAL',$1,$2,$3,$3
)
ON CONFLICT DO NOTHING`, strings.Repeat("d", 64), []byte(`{"policy":"sandbox-integration"}`),
		now.Add(-time.Second)); err != nil {
		t.Fatalf("risk projection policy fixture failed: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO risk_policy_limits(
 policy_id,policy_version,account_drawdown,utc_day_loss,rolling_24_hour_loss,
 strategy_loss,asset_exposure,combined_exposure,exchange_exposure,minimum_reserve,
 maximum_reserved_capital,maximum_spread,maximum_slippage,maximum_open_orders,
 maximum_book_age_microseconds,maximum_queue_lag_microseconds,
 maximum_clock_drift_microseconds,minimum_quality_score
) VALUES(
 'sandbox-strategy-risk-policy',1,0.05,0.02,0.03,0.03,0.30,0.50,0.60,0.15,
 0.85,0.01,0.005,1,250000,250000,250000,90
)
ON CONFLICT DO NOTHING`); err != nil {
		t.Fatalf("risk projection limits fixture failed: %v", err)
	}
}

func seedSandboxRiskProjectionFacts(
	t *testing.T,
	ctx context.Context,
	store *SandboxRuntimeDispatcherStore,
	prior sandbox.StrategySessionAdmission,
	now time.Time,
) sandbox.AccountSnapshot {
	t.Helper()
	btc, _ := domain.ParseBalance("0.001")
	usdt, _ := domain.ParseBalance("39.99")
	zero, _ := domain.ParseBalance("0")
	snapshot := sandbox.AccountSnapshot{AccountID: prior.Work.Account.ID,
		Epoch: prior.Work.Account.Epoch,
		Balances: []sandbox.Balance{{Asset: "BTC", Available: btc, Reserved: zero},
			{Asset: "USDT", Available: usdt, Reserved: zero}},
		OrdersHash: strings.Repeat("e", 64), FillsHash: strings.Repeat("f", 64),
		SnapshotHash: strings.Repeat("a", 64), ObservedAt: now}
	if err := store.RecordAccountSnapshot(ctx, "sandbox-strategy-risk-snapshot", snapshot); err != nil {
		t.Fatalf("risk projection account snapshot failed: %v", err)
	}
	reconciliation := sandbox.ReconciliationResult{ID: "sandbox-strategy-risk-reconciliation",
		AccountID: prior.Work.Account.ID, AccountEpoch: prior.Work.Account.Epoch,
		State: "clean", EvidenceHash: strings.Repeat("b", 64), ReconciledAt: now}
	if err := store.RecordReconciliation(ctx, reconciliation); err != nil {
		t.Fatalf("risk projection reconciliation failed: %v", err)
	}
	eligibility := exchangecontracts.CollectorHealthSnapshot{ObservedAt: now,
		Exchange: string(prior.Work.Account.Exchange), Instrument: prior.Work.Instrument,
		BookHealth: "healthy", BookHealthy: true, BookFresh: true, BookEligible: true,
		ClockEligible: true, ClockObservedAt: now, ClockUncertainty: time.Millisecond, Eligible: true}
	if err := store.RecordEngineObservations(ctx, prior.Work.Account.ID, prior.Work.Account.Epoch,
		prior.Work.Account.Exchange, 1, []exchangecontracts.CollectorHealthSnapshot{eligibility}); err != nil {
		t.Fatalf("risk projection engine observation failed: %v", err)
	}
	return snapshot
}

func sandboxRiskProjectionAdmission(
	t *testing.T,
	ctx context.Context,
	store *SandboxRuntimeDispatcherStore,
	prior sandbox.StrategySessionAdmission,
	snapshot sandbox.AccountSnapshot,
	now time.Time,
) (sandbox.StrategySessionWork, sandbox.StrategySessionAdmission, sandbox.StrategyRiskFacts) {
	t.Helper()
	work, err := store.ActiveStrategySessionWork(ctx, prior.Work.Account.ID, prior.Work.Account.Epoch,
		"sandbox_runtime-engine-runtime-worker", 1, now, 1)
	if err != nil || len(work) != 1 {
		t.Fatalf("risk projection work=%d error=%v", len(work), err)
	}
	admission, err := store.StrategySessionAdmission(ctx, work[0], now, [4]bool{true, true, true, true})
	if err != nil || admission.Valid() != nil {
		t.Fatalf("risk projection admission=%#v error=%v", admission, err)
	}
	facts, err := store.StrategyRiskFacts(ctx, work[0], snapshot, now)
	if err != nil || facts.ValidFor(work[0], snapshot, now) != nil {
		t.Fatalf("risk projection facts=%#v error=%v", facts, err)
	}
	return work[0], admission, facts
}

func sandboxRiskProjectionMarket(now time.Time) sandbox.StrategyMarketInput {
	instrument, _ := domain.NewSpotInstrument("BTC", "USDT")
	bid, _ := domain.ParsePrice("10000")
	ask, _ := domain.ParsePrice("10001")
	depth, _ := domain.ParseQuantity("1")
	return sandbox.StrategyMarketInput{Instrument: instrument,
		Metadata: exchangecontracts.InstrumentRecord{RawPayloadHash: strings.Repeat("1", 64)},
		Book: exchangecontracts.BookSnapshot{Instrument: instrument, LastSequence: 2,
			ReceivedAt:     domain.EventTime{UTC: now.Add(-10 * time.Millisecond), Sequence: 1},
			Bids:           []exchangecontracts.PriceLevel{{Price: bid, Quantity: depth}},
			Asks:           []exchangecontracts.PriceLevel{{Price: ask, Quantity: depth}},
			RawPayloadHash: strings.Repeat("2", 64)},
		Candles:    map[string][]exchangecontracts.Candle{"4h": {{RawPayloadHash: strings.Repeat("3", 64)}}},
		ObservedAt: domain.EventTime{UTC: now, Sequence: 2}}
}

func assertSandboxRiskProjectionEvidence(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	store *SandboxRuntimeDispatcherStore,
	work sandbox.StrategySessionWork,
	admission sandbox.StrategySessionAdmission,
	snapshot sandbox.AccountSnapshot,
	market sandbox.StrategyMarketInput,
	facts sandbox.StrategyRiskFacts,
	now time.Time,
) {
	t.Helper()
	lease := sandbox.StrategySessionExecutionLease{Account: work.Account.ID,
		Epoch: work.Account.Epoch, Owner: "sandbox_runtime-engine-runtime-worker", Fence: 1}
	var err error
	if _, err = store.ProjectStrategyRiskObservation(ctx, lease, admission, snapshot, market, facts, now); err == nil {
		t.Fatal("first risk valuation did not require a durable baseline")
	}
	observation, err := store.ProjectStrategyRiskObservation(ctx, lease, admission, snapshot, market, facts, now)
	if err != nil || observation.ValidFor(work, snapshot, market, facts, now) != nil ||
		observation.StrategyLoss.String() == "0" || observation.AssetExposure.String() == "0" ||
		observation.QualityScore != 100 || observation.AccountingFault || observation.ReconciliationFault ||
		observation.DiskFault || observation.APIError || observation.LeaseLost {
		t.Fatalf("risk projection observation=%#v error=%v", observation, err)
	}
	loaded, err := store.StrategyRiskObservation(ctx, work, snapshot, market, facts, now)
	if err != nil || loaded.EvidenceHash() != observation.EvidenceHash() {
		t.Fatalf("risk projection read=%#v error=%v", loaded, err)
	}
	assertSandboxRiskProjectionRows(t, ctx, pool, work)
}

func assertSandboxRiskProjectionRows(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	work sandbox.StrategySessionWork,
) {
	t.Helper()
	var baselines, evaluated, observations int
	if err := pool.QueryRow(ctx, `
SELECT count(*) FILTER (WHERE purpose='baseline'),
       count(*) FILTER (WHERE purpose='evaluated')
FROM sandbox_strategy_risk_valuations
WHERE strategy_session_id=$1 AND account_id=$2`, work.SessionID, work.Account.ID).Scan(
		&baselines, &evaluated); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM sandbox_strategy_risk_observations
WHERE strategy_session_id=$1 AND account_id=$2`, work.SessionID, work.Account.ID).Scan(&observations); err != nil {
		t.Fatal(err)
	}
	if baselines != 1 || evaluated != 1 || observations != 1 {
		t.Fatalf("risk projection evidence baseline=%d evaluated=%d observations=%d",
			baselines, evaluated, observations)
	}
}

func assertSandboxRiskRuntime(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	now time.Time,
) time.Time {
	t.Helper()
	tripAt := now.Add(time.Millisecond)
	clock, err := domain.NewReplayClock(tripAt)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewSandboxRiskRuntime(pool, clock)
	if err != nil {
		t.Fatal(err)
	}
	first, err := runtime.SandboxStrategyRiskEngine(ctx, now)
	if err != nil || first.State() != risk.StateNormal {
		t.Fatalf("first restored risk state=%v error=%v", first, err)
	}
	stale, err := runtime.SandboxStrategyRiskEngine(ctx, now)
	if err != nil || stale.State() != risk.StateNormal {
		t.Fatalf("second restored risk state=%v error=%v", stale, err)
	}
	if err = first.TripBreaker(risk.BreakerGap, tripAt); err != nil {
		t.Fatalf("durable sandbox risk breaker failed: %v", err)
	}
	if err = stale.TripBreaker(risk.BreakerSlippage, tripAt.Add(time.Millisecond)); err == nil {
		t.Fatal("stale sandbox risk engine overwrote a newer durable posture")
	}
	restored, err := runtime.SandboxStrategyRiskEngine(ctx, tripAt.Add(2*time.Millisecond))
	if err != nil || restored.State() != risk.StatePaused {
		t.Fatalf("reloaded sandbox risk state=%v error=%v", restored, err)
	}
	assertSandboxRiskRuntimeState(t, ctx, pool)
	return tripAt.Add(2 * time.Millisecond)
}

func assertSandboxRiskRuntimeState(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var state, reason string
	var eventRevision, entityRevision, alerts int64
	if err := pool.QueryRow(ctx, `
SELECT next_state,reason_code,entity_revision
FROM risk_state_events
ORDER BY entity_revision DESC
LIMIT 1`).Scan(&state, &reason, &eventRevision); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
SELECT revision FROM api_entity_revisions
WHERE entity_type='risk' AND entity_id='global'`).Scan(&entityRevision); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM alerts
WHERE alert_type='central-risk' AND reason_code='gap_or_stale_data'
  AND state='open'`).Scan(&alerts); err != nil {
		t.Fatal(err)
	}
	if state != string(risk.StatePaused) || reason != string(risk.BreakerGap) ||
		eventRevision != entityRevision || alerts != 1 {
		t.Fatalf("durable risk state=%s reason=%s event_revision=%d entity_revision=%d alerts=%d",
			state, reason, eventRevision, entityRevision, alerts)
	}
}

func sandboxAccountingIntegrationPlan(
	t *testing.T,
	ctx context.Context,
	store *SandboxRuntimeDispatcherStore,
	admission sandbox.StrategySessionAdmission,
	approvedAt time.Time,
) sandbox.ApprovedSandboxPlan {
	t.Helper()
	submission := sandboxAccountingIntegrationSubmission(admission, approvedAt)
	snapshot := sandboxAccountingIntegrationSnapshot(t, ctx, store, admission, approvedAt)
	evidence := sandboxAccountingIntegrationDecision(t, admission)
	pipeline := sandboxQualificationPipeline(approvedAt)
	plan := sandbox.ApprovedSandboxPlan{
		ID: submission.PlanID.String(), SessionID: admission.Work.SessionID,
		Submissions: []sandbox.Submission{submission},
		Reservations: []sandbox.DurableReservation{{
			ID:        "sandbox-strategy-accounting-reservation",
			AccountID: submission.AccountID, AccountEpoch: submission.AccountEpoch,
			OrderID: submission.OrderID.String(), Asset: "USDT", Quantity: "10",
		}},
		Arm: admission.Arm,
		Eligibility: map[sandbox.Exchange]sandbox.EligibilitySnapshot{
			admission.Work.Account.Exchange: admission.Eligibility,
		},
		EntrySafety: map[sandbox.AccountID]sandbox.EntrySafetySnapshot{submission.AccountID: admission.Safety},
		AccountSnapshots: map[sandbox.AccountID]sandbox.AccountSnapshotReference{submission.AccountID: {
			AccountID: submission.AccountID, AccountEpoch: submission.AccountEpoch,
			SnapshotHash: snapshot.SnapshotHash, ObservedAt: snapshot.ObservedAt,
		}},
		StrategyDecision: &evidence, Pipeline: pipeline, ApprovedAt: approvedAt,
		ConfigurationID: admission.Work.ConfigurationID,
	}
	plan.ApprovalHash = pipeline.HashFor(plan)
	return plan
}

func sandboxAccountingIntegrationSubmission(
	admission sandbox.StrategySessionAdmission,
	approvedAt time.Time,
) sandbox.Submission {
	planID, _ := domain.NewExecutionPlanID("sandbox-strategy-accounting-plan")
	orderID, _ := domain.NewVirtualOrderID("sandbox-strategy-accounting-order")
	strategyID, _ := domain.NewStrategyID(admission.Work.Strategy)
	instrument, _ := domain.NewSpotInstrument("BTC", "USDT")
	quantity, _ := domain.ParseQuantity("0.001")
	price, _ := domain.ParsePrice("10000")
	notional, _ := domain.ParseNotional("10")
	return sandbox.Submission{
		PlanID: planID, OrderID: orderID,
		AccountID: admission.Work.Account.ID, AccountEpoch: admission.Work.Account.Epoch,
		ClientOrderID: "ax-sandbox-strategy-accounting", StrategyID: strategyID,
		Instrument: instrument, Side: domain.SideBuy, Quantity: quantity, LimitPrice: price,
		Notional: notional, Style: sandbox.OrderStyleLimitGTC, Action: sandbox.IntentEntry,
		RequestHash: strings.Repeat("8", 64), PolicyHash: strings.Repeat("9", 64),
		ApprovedAt: approvedAt,
	}
}

func sandboxAccountingIntegrationSnapshot(
	t *testing.T,
	ctx context.Context,
	store *SandboxRuntimeDispatcherStore,
	admission sandbox.StrategySessionAdmission,
	approvedAt time.Time,
) sandbox.AccountSnapshot {
	t.Helper()
	if _, err := store.pool.Exec(ctx, `
INSERT INTO assets(symbol) VALUES ('BTC'),('USDT') ON CONFLICT DO NOTHING`); err != nil {
		t.Fatalf("automatic accounting assets failed: %v", err)
	}
	available, _ := domain.ParseBalance("50")
	zero, _ := domain.ParseBalance("0")
	snapshot := sandbox.AccountSnapshot{
		AccountID: admission.Work.Account.ID, Epoch: admission.Work.Account.Epoch,
		Balances:   []sandbox.Balance{{Asset: "USDT", Available: available, Reserved: zero}},
		OrdersHash: strings.Repeat("1", 64), FillsHash: strings.Repeat("2", 64),
		SnapshotHash: strings.Repeat("3", 64), ObservedAt: approvedAt,
	}
	if err := store.RecordAccountSnapshot(ctx, "sandbox-strategy-accounting-snapshot", snapshot); err != nil {
		t.Fatalf("automatic accounting snapshot failed: %v", err)
	}
	return snapshot
}

func sandboxAccountingIntegrationDecision(
	t *testing.T,
	admission sandbox.StrategySessionAdmission,
) sandbox.StrategyDecisionEvidence {
	t.Helper()
	evidence, err := sandbox.NewStrategyDecisionEvidence(
		admission,
		replay.Event{Ordinal: 1, LogicalTime: 1, Canonical: []byte(`{"market":"complete"}`)},
		[]byte(`{"id":"decision:sandbox-strategy-accounting","ordinal":1,"action":"entry","candidate":{}}`),
	)
	if err != nil {
		t.Fatalf("automatic accounting decision evidence failed: %v", err)
	}
	return evidence
}

func sandboxAccountingIntegrationFill(
	t *testing.T,
	submission sandbox.Submission,
	occurredAt time.Time,
) sandbox.PrivateEvent {
	t.Helper()
	fillID, _ := domain.NewVirtualFillID("sandbox-strategy-accounting-fill")
	feeAsset, _ := domain.ParseAssetSymbol("USDT")
	fee, _ := domain.ParseFee("0.01")
	fill := execution.FillFact{
		ID: fillID, Quantity: submission.Quantity, Price: submission.LimitPrice,
		Fee: fee, FeeAsset: feeAsset, Ordinal: 7,
	}
	orderEvent := execution.OrderEvent{
		ID: "sandbox-strategy-accounting-order-event", OrderID: submission.OrderID,
		ClientOrderID: submission.ClientOrderID, State: execution.OrderFilled,
		ExchangeStatus: "FILLED", CumulativeQuantity: submission.Quantity,
		Fees:  []execution.FeeFact{{Asset: feeAsset, Total: fee}},
		Fills: []execution.FillFact{fill}, OccurredAt: occurredAt, Ordinal: 7,
	}
	return sandbox.PrivateEvent{
		Identity:  "sandbox-strategy-accounting-private-fill",
		AccountID: submission.AccountID, AccountEpoch: submission.AccountEpoch,
		Kind: sandbox.PrivateFillEvent, OrderID: submission.OrderID,
		ClientOrderID:   submission.ClientOrderID,
		NativeOrderHash: strings.Repeat("4", 64), NativeFillHash: strings.Repeat("5", 64),
		OrderEvent: &orderEvent, OccurredAt: occurredAt, ReceivedAt: occurredAt.Add(time.Millisecond),
	}
}

func assertSandboxAccountingRowCounts(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	plan sandbox.ApprovedSandboxPlan,
	nativeFillHash string,
	wantFills, wantTransactions, wantPositions int,
) {
	t.Helper()
	var fills, transactions, positions int
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM sandbox_runtime_exchange_fills WHERE native_fill_id_hash=$1`,
		nativeFillHash).Scan(&fills); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM sandbox_accounting_transactions WHERE plan_id=$1`,
		plan.ID).Scan(&transactions); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM sandbox_accounting_positions
WHERE strategy_session_id=$1 AND account_id=$2 AND account_epoch=$3 AND instrument=$4`,
		plan.SessionID, plan.Submissions[0].AccountID, plan.Submissions[0].AccountEpoch,
		plan.Submissions[0].Instrument.Symbol()).Scan(&positions); err != nil {
		t.Fatal(err)
	}
	if fills != wantFills || transactions != wantTransactions || positions != wantPositions {
		t.Fatalf("atomic accounting rows fills=%d/%d transactions=%d/%d positions=%d/%d",
			fills, wantFills, transactions, wantTransactions, positions, wantPositions)
	}
}

func assertSandboxAccountingCommittedHeader(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	plan sandbox.ApprovedSandboxPlan,
	submission sandbox.Submission,
) {
	t.Helper()
	var transactionType, sourceMode, exchange, environment, configurationID string
	var policyHash, clientOrderID, action string
	var sealed bool
	if err := pool.QueryRow(ctx, `
SELECT transaction_type,source_mode,exchange,environment,configuration_id,
       policy_hash::text,client_order_id,intent_action,sealed
FROM sandbox_accounting_transactions WHERE plan_id=$1`, plan.ID).Scan(
		&transactionType, &sourceMode, &exchange, &environment, &configurationID,
		&policyHash, &clientOrderID, &action, &sealed,
	); err != nil {
		t.Fatal(err)
	}
	if transactionType != "fill" || sourceMode != "exchange_sandbox" ||
		exchange != string(sandbox.ExchangeBinance) ||
		environment != string(sandbox.EnvironmentBinanceSpotTestnet) ||
		configurationID != plan.ConfigurationID || policyHash != submission.PolicyHash ||
		clientOrderID != submission.ClientOrderID || action != string(submission.Action) || !sealed {
		t.Fatalf("automatic accounting header mismatch type=%s mode=%s exchange=%s environment=%s configuration=%s policy=%s client=%s action=%s sealed=%t",
			transactionType, sourceMode, exchange, environment, configurationID,
			policyHash, clientOrderID, action, sealed)
	}
}

func assertSandboxAccountingCommittedBalance(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	planID string,
) {
	t.Helper()
	rows, err := pool.Query(ctx, `
SELECT entry.asset_symbol,
       sum(CASE entry.direction WHEN 'debit' THEN entry.quantity ELSE -entry.quantity END)::text
FROM sandbox_accounting_entries entry
JOIN sandbox_accounting_transactions journal ON journal.id=entry.transaction_id
WHERE journal.plan_id=$1
GROUP BY entry.asset_symbol
ORDER BY entry.asset_symbol`, planID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	assets := 0
	zero, _ := domain.ParseBalance("0")
	for rows.Next() {
		var asset, balance string
		if err = rows.Scan(&asset, &balance); err != nil {
			t.Fatal(err)
		}
		assets++
		amount, parseErr := domain.ParseBalance(balance)
		if parseErr != nil || amount.Compare(zero) != 0 {
			t.Fatalf("automatic accounting asset %s balance=%s", asset, balance)
		}
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	var entries int
	if err = pool.QueryRow(ctx, `
SELECT count(*)
FROM sandbox_accounting_entries entry
JOIN sandbox_accounting_transactions journal ON journal.id=entry.transaction_id
WHERE journal.plan_id=$1`, planID).Scan(&entries); err != nil {
		t.Fatal(err)
	}
	if assets != 2 || entries != 6 {
		t.Fatalf("automatic accounting assets=%d entries=%d", assets, entries)
	}
}

func assertSandboxAccountingCommittedProjection(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	plan sandbox.ApprovedSandboxPlan,
	submission sandbox.Submission,
) {
	t.Helper()
	var quantity, totalCost, averageCost, realizedPnL, valuationState string
	var transactions, revision int64
	var projectionHash, lastTransactionID string
	if err := pool.QueryRow(ctx, `
SELECT quantity::text,total_cost::text,weighted_average_cost::text,
       realized_pnl::text,valuation_state,source_transaction_count,
       revision,projection_hash::text,last_transaction_id
FROM sandbox_accounting_positions
WHERE strategy_session_id=$1 AND account_id=$2 AND account_epoch=$3 AND instrument=$4`,
		plan.SessionID, submission.AccountID, submission.AccountEpoch,
		submission.Instrument.Symbol()).Scan(
		&quantity, &totalCost, &averageCost, &realizedPnL, &valuationState,
		&transactions, &revision, &projectionHash, &lastTransactionID,
	); err != nil {
		t.Fatal(err)
	}
	if quantity != "0.001000000000000000" || totalCost != "10.010000000000000000" ||
		averageCost != "10010.000000000000000000" || realizedPnL != "0.000000000000000000" ||
		valuationState != sandboxAccountingValuationComplete || transactions != 1 || revision != 1 ||
		len(projectionHash) != 64 || lastTransactionID == "" {
		t.Fatalf("automatic accounting projection quantity=%s cost=%s average=%s pnl=%s state=%s transactions=%d revision=%d hash=%s last=%s",
			quantity, totalCost, averageCost, realizedPnL, valuationState,
			transactions, revision, projectionHash, lastTransactionID)
	}
	var fee, rebate string
	if err := pool.QueryRow(ctx, `
SELECT fee_quantity::text,rebate_quantity::text
FROM sandbox_accounting_position_fees
WHERE strategy_session_id=$1 AND account_id=$2 AND account_epoch=$3
  AND instrument=$4 AND asset_symbol='USDT'`,
		plan.SessionID, submission.AccountID, submission.AccountEpoch,
		submission.Instrument.Symbol()).Scan(&fee, &rebate); err != nil {
		t.Fatal(err)
	}
	if fee != "0.010000000000000000" || rebate != "0.000000000000000000" {
		t.Fatalf("automatic accounting projection fee=%s rebate=%s", fee, rebate)
	}
}
