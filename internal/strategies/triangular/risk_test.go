package triangular

import (
	"testing"
	"time"

	"axiom/internal/domain"
	"axiom/internal/risk"
)

func TestCandidateRequiresCentralRiskApproval(t *testing.T) {
	now := time.Date(2026, 7, 23, 8, 0, 0, 0, time.UTC)
	engine, err := risk.NewEngine(&triangularRiskAudit{}, &triangularRiskAlerts{})
	if err != nil {
		t.Fatal(err)
	}
	if err = engine.ManualTransition(risk.StateNormal, triangularRecoveryEvidence(now)); err != nil {
		t.Fatal(err)
	}
	candidate := candidateFor(t, profitableInput(t, false), CycleUSDTBTCETHUSDT, "10")
	input := RiskInput{
		Policies:     []risk.Policy{triangularRiskPolicy(risk.StateNormal)},
		Observations: triangularHealthyRiskObservations(), EvaluatedAt: now.Add(time.Second),
	}
	decision, err := ApproveCandidate(engine, candidate, input, 1_010)
	if err != nil || decision.Action != risk.ActionApprove || decision.ReasonCode != "approved" {
		t.Fatalf("candidate was not centrally approved: %#v %v", decision, err)
	}

	input.Observations.BookAge = durationPointer(250 * time.Millisecond)
	decision, err = ApproveCandidate(engine, candidate, input, 1_010)
	if err == nil || decision.ReasonCode != "book_age_limit" ||
		decision.Action != risk.ActionPauseInstrument {
		t.Fatalf("central-risk rejection was not preserved: %#v %v", decision, err)
	}
}

func TestExpiredCandidateNeverReachesCentralRisk(t *testing.T) {
	candidate := candidateFor(t, profitableInput(t, false), CycleUSDTBTCETHUSDT, "10")
	spy := &triangularRiskSpy{}
	if _, err := ApproveCandidate(spy, candidate, RiskInput{}, candidate.ExpiresOffsetNanos+1); err == nil {
		t.Fatal("expired candidate was approved")
	}
	if spy.called {
		t.Fatal("expired candidate reached central risk")
	}
}

type triangularRiskSpy struct{ called bool }

func (spy *triangularRiskSpy) Evaluate(risk.Request) (risk.Decision, error) {
	spy.called = true
	return risk.Decision{Action: risk.ActionApprove}, nil
}

type triangularRiskAudit struct{}

func (*triangularRiskAudit) Append(risk.AuditEvent) error { return nil }

type triangularRiskAlerts struct{}

func (*triangularRiskAlerts) Emit(string, risk.Action, risk.State) error { return nil }

func triangularRiskPolicy(state risk.State) risk.Policy {
	return risk.Policy{
		ID: "triangular-policy", Version: 1,
		Scope: risk.Scope{Kind: risk.ScopeStrategy, ID: "triangular"},
		State: state, Limits: risk.DefaultLimits(),
	}
}

func triangularHealthyRiskObservations() risk.Observations {
	openOrders, quality := uint32(0), uint8(100)
	healthy := false
	return risk.Observations{
		AccountDrawdown: percentPointer("0"), UTCDayLoss: percentPointer("0"),
		Rolling24HourLoss: percentPointer("0"), StrategyLoss: percentPointer("0"),
		AssetExposure: percentPointer("0"), CombinedExposure: percentPointer("0"),
		ExchangeExposure: percentPointer("0"), Reserve: percentPointer("1"),
		ReservedCapital: percentPointer("0"), Spread: percentPointer("0"),
		Slippage: percentPointer("0"), OpenOrders: &openOrders,
		BookAge: durationPointer(time.Millisecond), QueueLag: durationPointer(time.Millisecond),
		ClockDrift: durationPointer(time.Millisecond), QualityScore: &quality,
		Health: risk.HealthInputs{
			Gap: &healthy, StaleData: &healthy, ReconciliationFault: &healthy,
			AccountingFault: &healthy, UnknownOrder: &healthy, PersistenceFault: &healthy,
			DiskFault: &healthy, APIError: &healthy, LeaseLost: &healthy,
		},
	}
}

func triangularRecoveryEvidence(at time.Time) risk.RecoveryEvidence {
	return risk.RecoveryEvidence{
		Reconciled: true, PersistenceHealthy: true, BooksFresh: true,
		UnknownOrdersResolved: true, Reauthenticated: true, AuditDurable: true,
		Actor: "owner", Reason: "b4-test", At: at,
	}
}

func percentPointer(value string) *domain.Percent {
	parsed, _ := domain.ParsePercent(value)
	return &parsed
}

func durationPointer(value time.Duration) *time.Duration { return &value }
