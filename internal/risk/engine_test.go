package risk

import (
	"testing"
	"time"

	"axiom/internal/domain"
)

func TestEngineStartsPausedAndRequiresAuditedManualRecovery(t *testing.T) {
	engine, audit, _ := testEngine(t)
	now := time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC)
	decision, err := engine.Evaluate(healthyRequest(now, StateNormal, IntentEntry))
	if err != nil || decision.Action != ActionReject || decision.EffectiveState != StatePaused {
		t.Fatalf("startup decision = %#v %v", decision, err)
	}
	if err = engine.ManualTransition(StateNormal, recoveryEvidence(now)); err != nil {
		t.Fatal(err)
	}
	decision, err = engine.Evaluate(healthyRequest(now.Add(time.Second), StateNormal, IntentEntry))
	if err != nil || decision.Action != ActionApprove || len(audit.events) != 1 {
		t.Fatalf("active decision = %#v %v", decision, err)
	}
}

func TestEveryRiskThresholdAndMissingInputFailsAtDocumentedBoundary(t *testing.T) {
	now := time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC)
	cases := thresholdCases()
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			engine, _, _ := testEngine(t)
			if err := engine.ManualTransition(StateNormal, recoveryEvidence(now)); err != nil {
				t.Fatal(err)
			}
			request := healthyRequest(now.Add(time.Second), StateNormal, IntentEntry)
			test.mutate(&request.Observations)
			decision, err := engine.Evaluate(request)
			if err != nil || decision.ReasonCode != test.reason || decision.Action == ActionApprove {
				t.Fatalf("decision = %#v %v", decision, err)
			}
		})
	}
	engine, _, _ := testEngine(t)
	_ = engine.ManualTransition(StateNormal, recoveryEvidence(now))
	request := healthyRequest(now.Add(time.Second), StateNormal, IntentEntry)
	request.Observations.Reserve = nil
	decision, err := engine.Evaluate(request)
	if err != nil || decision.ReasonCode != "risk_input_missing" || decision.EffectiveState != StateLocked {
		t.Fatalf("missing input = %#v %v", decision, err)
	}
}

func TestSixScopePrecedenceAndIntentMatrix(t *testing.T) {
	now := time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC)
	engine, _, _ := testEngine(t)
	_ = engine.ManualTransition(StateNormal, recoveryEvidence(now))
	request := healthyRequest(now.Add(time.Second), StateNormal, IntentEntry)
	request.Policies = sixScopePolicies(StateNormal)
	request.Policies[4].State = StatePaused
	decision, err := engine.Evaluate(request)
	if err != nil || decision.EffectiveState != StatePaused || decision.Action != ActionReject ||
		len(decision.ContributingIDs) != 6 {
		t.Fatalf("scope precedence = %#v %v", decision, err)
	}
	assertIntentMatrix(t, now)
}

func TestExchangeAccountScopeAndPolicyOrderCannotHideStricterStateOrBreaker(t *testing.T) {
	now := time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC)
	engine, _, _ := testEngine(t)
	_ = engine.ManualTransition(StateNormal, recoveryEvidence(now))
	policies := sixScopePolicies(StateNormal)
	account := DefaultGlobalPolicy()
	account.ID, account.Scope, account.State = "account", Scope{Kind: ScopeAccount, ID: "virtual-account"}, StatePaused
	policies = append(policies, account)
	policies[0].Limits.AccountDrawdown = percent("0.01")
	request := healthyRequest(now.Add(time.Second), StateNormal, IntentEntry)
	request.Policies = policies
	request.Observations.AccountDrawdown = percentPointer("0.02")
	decision, err := engine.Evaluate(request)
	if err != nil || decision.EffectiveState != StateLocked || decision.Action != ActionLockEngine ||
		decision.ReasonCode != "account_drawdown_limit" || len(decision.ContributingIDs) != 7 ||
		decision.ContributingIDs[1] != "account" {
		t.Fatalf("account/order-independent policy = %#v %v", decision, err)
	}
}

func TestCautiousEntryRequiresReducedSizeStricterEdgeAndEligibleInstrument(t *testing.T) {
	now := time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC)
	request := healthyRequest(now, StateCautious, IntentEntry)
	decision := evaluatePolicies(request, StateCautious)
	if decision.Action != ActionReject || decision.ReasonCode != "cautious_controls_missing" {
		t.Fatalf("unreduced cautious entry = %#v", decision)
	}
	request.Cautious = CautiousControls{ReducedSize: true, StricterEdge: true, InstrumentEligible: true}
	if decision = evaluatePolicies(request, StateCautious); decision.Action != ActionApprove {
		t.Fatalf("qualified cautious entry = %#v", decision)
	}
}

func TestCautiousHysteresisAndNoAutomaticPausedLockedRecovery(t *testing.T) {
	now := time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC)
	engine, _, _ := testEngine(t)
	if err := engine.ManualTransition(StateCautious, recoveryEvidence(now)); err != nil {
		t.Fatal(err)
	}
	_ = engine.ObserveHealthy(now.Add(time.Second))
	_ = engine.ObserveHealthy(now.Add(5*time.Minute - time.Nanosecond))
	if engine.State() != StateCautious {
		t.Fatal("cautious recovered early")
	}
	if err := engine.ObserveHealthy(now.Add(5*time.Minute + time.Second)); err != nil || engine.State() != StateNormal {
		t.Fatal("cautious did not recover after five healthy minutes")
	}
	paused, _, _ := testEngine(t)
	_ = paused.ObserveHealthy(now.Add(10 * time.Minute))
	if paused.State() != StatePaused {
		t.Fatal("paused auto-unpaused")
	}
	locked, _, _ := testEngine(t)
	_ = locked.BeginStartupRecovery(now)
	_ = locked.ObserveHealthy(now.Add(10 * time.Minute))
	if locked.State() != StateLocked {
		t.Fatal("locked auto-unlocked")
	}
}

func TestEveryCircuitBreakerAuditsAndAlerts(t *testing.T) {
	now := time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC)
	kinds := []BreakerKind{BreakerGap, BreakerReconciliation, BreakerUnknownOrder, BreakerLoss,
		BreakerSlippage, BreakerPersistence, BreakerDisk, BreakerClockDrift, BreakerAPI, BreakerQueueLag, BreakerLeaseLoss}
	for _, kind := range kinds {
		engine, audit, alerts := testEngine(t)
		if err := engine.ManualTransition(StateNormal, recoveryEvidence(now)); err != nil {
			t.Fatal(err)
		}
		if err := engine.TripBreaker(kind, now.Add(time.Second)); err != nil || len(audit.events) != 2 || len(alerts.reasons) != 1 ||
			stateRank(engine.State()) < stateRank(StatePaused) {
			t.Fatalf("breaker %s failed", kind)
		}
	}
}

type thresholdCase struct {
	name, reason string
	mutate       func(*Observations)
}

func thresholdCases() []thresholdCase {
	return []thresholdCase{
		{"drawdown", "account_drawdown_limit", func(o *Observations) { o.AccountDrawdown = percentPointer("0.05") }},
		{"day_loss", "utc_day_loss_limit", func(o *Observations) { o.UTCDayLoss = percentPointer("0.01") }},
		{"rolling_loss", "rolling_loss_limit", func(o *Observations) { o.Rolling24HourLoss = percentPointer("0.01") }},
		{"strategy_loss", "strategy_loss_limit", func(o *Observations) { o.StrategyLoss = percentPointer("0.03") }},
		{"asset", "asset_exposure_limit", func(o *Observations) { o.AssetExposure = percentPointer("0.300000000000000001") }},
		{"combined", "combined_exposure_limit", func(o *Observations) { o.CombinedExposure = percentPointer("0.500000000000000001") }},
		{"exchange", "exchange_exposure_limit", func(o *Observations) { o.ExchangeExposure = percentPointer("0.600000000000000001") }},
		{"reserve", "minimum_reserve_limit", func(o *Observations) { o.Reserve = percentPointer("0.149999999999999999") }},
		{"reserved", "reserved_capital_limit", func(o *Observations) { o.ReservedCapital = percentPointer("0.850000000000000001") }},
		{"orders", "open_order_limit", func(o *Observations) { value := uint32(9); o.OpenOrders = &value }},
		{"spread", "spread_limit", func(o *Observations) { o.Spread = percentPointer("0.010000000000000001") }},
		{"slippage", "slippage_limit", func(o *Observations) { o.Slippage = percentPointer("0.005000000000000001") }},
		{"book", "book_age_limit", func(o *Observations) { value := 250 * time.Millisecond; o.BookAge = &value }},
		{"queue", "queue_lag_limit", func(o *Observations) { value := 250*time.Millisecond + 1; o.QueueLag = &value }},
		{"clock", "clock_drift_limit", func(o *Observations) { value := -100*time.Millisecond - 1; o.ClockDrift = &value }},
		{"quality", "quality_score_limit", func(o *Observations) { value := uint8(89); o.QualityScore = &value }},
	}
}

func healthyRequest(at time.Time, state State, intent Intent) Request {
	policy := DefaultGlobalPolicy()
	policy.State = state
	return Request{Intent: intent, Policies: []Policy{policy}, Observations: healthyObservations(), EvaluatedAt: at}
}

func healthyObservations() Observations {
	openOrders, quality := uint32(0), uint8(100)
	book, queue, drift := time.Millisecond, time.Millisecond, time.Millisecond
	healthy := false
	return Observations{AccountDrawdown: percentPointer("0"), UTCDayLoss: percentPointer("0"),
		Rolling24HourLoss: percentPointer("0"), StrategyLoss: percentPointer("0"), AssetExposure: percentPointer("0"),
		CombinedExposure: percentPointer("0"), ExchangeExposure: percentPointer("0"), Reserve: percentPointer("1"),
		ReservedCapital: percentPointer("0"), Spread: percentPointer("0"), Slippage: percentPointer("0"),
		OpenOrders: &openOrders, BookAge: &book, QueueLag: &queue, ClockDrift: &drift, QualityScore: &quality,
		Health: HealthInputs{Gap: &healthy, StaleData: &healthy, ReconciliationFault: &healthy,
			AccountingFault: &healthy, UnknownOrder: &healthy, PersistenceFault: &healthy,
			DiskFault: &healthy, APIError: &healthy, LeaseLost: &healthy}}
}

func sixScopePolicies(state State) []Policy {
	kinds := []ScopeKind{ScopeGlobal, ScopeExchange, ScopeStrategy, ScopePortfolio, ScopeAsset, ScopeInstrument}
	policies := make([]Policy, len(kinds))
	for index, kind := range kinds {
		policies[index] = Policy{ID: string(kind), Version: 1, Scope: Scope{Kind: kind, ID: string(kind)},
			State: state, Limits: DefaultLimits()}
	}
	return policies
}

func assertIntentMatrix(t *testing.T, now time.Time) {
	t.Helper()
	tests := []struct {
		state                       State
		intent                      Intent
		reducing, approved, allowed bool
	}{{StateNormal, IntentEntry, false, false, true}, {StateCautious, IntentEntry, false, false, true},
		{StatePaused, IntentEntry, false, false, false}, {StatePaused, IntentExit, true, false, true},
		{StatePaused, IntentRecovery, false, false, false}, {StateLocked, IntentCancel, false, false, true},
		{StateLocked, IntentExit, true, true, true}, {StateLocked, IntentRecovery, true, false, false}}
	for index, test := range tests {
		request := Request{Intent: test.intent, RiskReducing: test.reducing,
			LockedPolicyApproved: test.approved, Policies: sixScopePolicies(test.state),
			Observations: healthyObservations(), EvaluatedAt: now.Add(time.Duration(index) * time.Second)}
		if test.state == StateCautious && test.intent == IntentEntry {
			request.Cautious = CautiousControls{ReducedSize: true, StricterEdge: true, InstrumentEligible: true}
		}
		decision := evaluatePolicies(request, test.state)
		if (decision.Action == ActionApprove) != test.allowed {
			t.Fatalf("matrix %#v = %#v", test, decision)
		}
	}
}

func recoveryEvidence(at time.Time) RecoveryEvidence {
	return RecoveryEvidence{Reconciled: true, PersistenceHealthy: true, BooksFresh: true,
		UnknownOrdersResolved: true, Reauthenticated: true, AuditDurable: true,
		Actor: "owner", Reason: "validated", At: at}
}

func percentPointer(value string) *domain.Percent {
	parsed, _ := domain.ParsePercent(value)
	return &parsed
}

type memoryAudit struct{ events []AuditEvent }

func (audit *memoryAudit) Append(event AuditEvent) error {
	audit.events = append(audit.events, event)
	return nil
}

type memoryAlerts struct{ reasons []string }

func (alerts *memoryAlerts) Emit(reason string, _ Action, _ State) error {
	alerts.reasons = append(alerts.reasons, reason)
	return nil
}

func testEngine(t *testing.T) (*Engine, *memoryAudit, *memoryAlerts) {
	t.Helper()
	audit, alerts := &memoryAudit{}, &memoryAlerts{}
	engine, err := NewEngine(audit, alerts)
	if err != nil {
		t.Fatal(err)
	}
	return engine, audit, alerts
}
