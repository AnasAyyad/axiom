package bootstrap

import (
	"context"
	"testing"
	"time"

	"axiom/internal/backtest"
	"axiom/internal/config"
	"axiom/internal/domain"
	"axiom/internal/sandbox"
)

func TestSandboxStrategyDecisionExecutorWaitsBeforeAnyPrivateProjectionForIncompletePublicWarmup(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	clock, err := domain.NewReplayClock(now)
	if err != nil {
		t.Fatal(err)
	}
	work, record := readinessWorkAndConfiguration(t, sandbox.StrategyTrend, now)
	instrument, err := domain.NewSpotInstrument("BTC", "USDT")
	if err != nil {
		t.Fatal(err)
	}
	projector, err := NewSandboxStrategyPositionProjector(projectorJournal{}, projectorExecutions{})
	if err != nil {
		t.Fatal(err)
	}
	facts := &decisionExecutorFacts{}
	admission := &decisionExecutorAdmission{}
	inventory := &decisionExecutorInventory{}
	pipelines := &decisionExecutorPipelines{}
	decisions := &decisionExecutorRecorder{}
	riskProjector := &decisionExecutorRiskProjector{}
	executor, err := NewSandboxStrategyDecisionExecutor(
		newReadinessMarketData(t, instrument, now, 999), clock, projector, facts, riskProjector,
		admission, inventory, pipelines, decisions,
	)
	if err != nil {
		t.Fatal(err)
	}
	evaluation, err := executor.EvaluateStrategySession(context.Background(), work, record, readinessExecutionLease(work), now)
	if err != nil || evaluation.State != sandbox.StrategySessionEvaluationWaiting ||
		evaluation.Reason != "waiting_for_public_market_data" || facts.calls != 0 || admission.calls != 0 ||
		inventory.calls != 0 || riskProjector.calls != 0 || pipelines.calls != 0 || decisions.calls != 0 {
		t.Fatalf("evaluation=%#v error=%v facts=%d admission=%d risk=%d inventory=%d pipelines=%d decisions=%d",
			evaluation, err, facts.calls, admission.calls, riskProjector.calls, inventory.calls, pipelines.calls, decisions.calls)
	}
}

func TestSandboxStrategyDecisionExecutorDoesNotRepeatAFinalizedCandleTrigger(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	clock, err := domain.NewReplayClock(now)
	if err != nil {
		t.Fatal(err)
	}
	work, record := readinessWorkAndConfiguration(t, sandbox.StrategyTrend, now)
	instrument, err := domain.NewSpotInstrument("BTC", "USDT")
	if err != nil {
		t.Fatal(err)
	}
	data, validFacts := repeatedCandleTriggerFacts(t, work, instrument, now)
	projector, err := NewSandboxStrategyPositionProjector(projectorJournal{}, projectorExecutions{})
	if err != nil {
		t.Fatal(err)
	}
	facts := &decisionExecutorFacts{facts: validFacts}
	admission := &decisionExecutorAdmission{admission: sizingFactsAdmission(work, now)}
	inventory := &decisionExecutorInventory{}
	pipelines := &decisionExecutorPipelines{}
	decisions := &decisionExecutorRecorder{}
	riskProjector := &decisionExecutorRiskProjector{}
	executor, err := NewSandboxStrategyDecisionExecutor(
		data, clock, projector, facts, riskProjector, admission, inventory, pipelines, decisions,
	)
	if err != nil {
		t.Fatal(err)
	}
	evaluation, err := executor.EvaluateStrategySession(
		context.Background(), work, record, readinessExecutionLease(work), now,
	)
	if err != nil || evaluation.State != sandbox.StrategySessionEvaluationWaiting ||
		evaluation.Reason != "waiting_for_next_strategy_trigger" || facts.calls != 1 || admission.calls != 1 ||
		riskProjector.calls != 0 || inventory.calls != 0 || pipelines.calls != 0 || decisions.calls != 0 {
		t.Fatalf("evaluation=%#v error=%v facts=%d admission=%d risk=%d inventory=%d pipelines=%d decisions=%d",
			evaluation, err, facts.calls, admission.calls, riskProjector.calls, inventory.calls, pipelines.calls, decisions.calls)
	}
}

func TestSandboxSinglePipelineOutcomePreservesTheFailedStage(t *testing.T) {
	tests := []struct {
		code   string
		state  sandbox.StrategySessionEvaluationState
		reason string
	}{
		{"strategy_stage_failed", sandbox.StrategySessionEvaluationEvaluated, "strategy_candidate_rejected"},
		{"allocation_stage_failed", sandbox.StrategySessionEvaluationEvaluated, "strategy_allocation_rejected"},
		{"risk_stage_failed", sandbox.StrategySessionEvaluationEvaluated, "central_risk_rejected"},
		{"planning_stage_failed", sandbox.StrategySessionEvaluationBlocked, "strategy_planning_failed"},
		{"simulation_stage_failed", sandbox.StrategySessionEvaluationBlocked, "strategy_plan_persistence_uncertain"},
		{"durable_stage_failed", sandbox.StrategySessionEvaluationBlocked, "strategy_plan_persistence_uncertain"},
	}
	for _, test := range tests {
		t.Run(test.code, func(t *testing.T) {
			state, reason := sandboxSinglePipelineOutcome(&backtest.Error{Code: test.code})
			if state != test.state || reason != test.reason {
				t.Fatalf("state=%s reason=%s", state, reason)
			}
		})
	}
}

func repeatedCandleTriggerFacts(t *testing.T, work sandbox.StrategySessionWork,
	instrument domain.Instrument, now time.Time,
) (*readinessMarketData, SandboxStrategySizingFacts) {
	t.Helper()
	data := newReadinessMarketData(t, instrument, now, 1000)
	priorTrigger, err := sandboxStrategyEvaluationTrigger(work, sandbox.StrategyMarketInput{
		Instrument: instrument, Candles: data.candles})
	if err != nil {
		t.Fatal(err)
	}
	_, _, facts := validSandboxTrendInputBuilderFacts(t, now)
	facts.AccountSnapshot.AccountID = work.Account.ID
	facts.AccountSnapshot.Epoch = work.Account.Epoch
	facts.CentralRiskFacts.AccountID = work.Account.ID
	facts.CentralRiskFacts.AccountEpoch = work.Account.Epoch
	facts.ConfigurationHash = work.ConfigurationHash
	facts.PriorEvaluationTriggerHash = priorTrigger
	return data, facts
}

type decisionExecutorFacts struct {
	calls int
	facts SandboxStrategySizingFacts
}

func (source *decisionExecutorFacts) SandboxStrategySizingFacts(
	context.Context,
	sandbox.StrategySessionWork,
	config.Configuration,
	sandbox.StrategySessionAdmission,
	sandbox.StrategySessionExecutionLease,
	time.Time,
) (SandboxStrategySizingFacts, error) {
	source.calls++
	return source.facts, nil
}

type decisionExecutorAdmission struct {
	calls     int
	admission sandbox.StrategySessionAdmission
}

func (source *decisionExecutorAdmission) SandboxStrategySessionAdmission(
	context.Context,
	sandbox.StrategySessionWork,
	time.Time,
) (sandbox.StrategySessionAdmission, error) {
	source.calls++
	return source.admission, nil
}

type decisionExecutorInventory struct{ calls int }

type decisionExecutorRiskProjector struct{ calls int }

func (source *decisionExecutorRiskProjector) ProjectStrategyRiskObservation(
	context.Context,
	sandbox.StrategySessionExecutionLease,
	sandbox.StrategySessionAdmission,
	sandbox.AccountSnapshot,
	sandbox.StrategyMarketInput,
	sandbox.StrategyRiskFacts,
	time.Time,
) (sandbox.StrategyRiskObservation, error) {
	source.calls++
	return sandbox.StrategyRiskObservation{}, nil
}

func (source *decisionExecutorInventory) StrategyOwnedInventory(
	context.Context,
	sandbox.StrategySessionWork,
	domain.AssetSymbol,
	time.Time,
) (sandbox.StrategyOwnedInventory, error) {
	source.calls++
	return sandbox.StrategyOwnedInventory{}, nil
}

type decisionExecutorPipelines struct{ calls int }

func (source *decisionExecutorPipelines) SandboxStrategyPipelineDependencies(
	context.Context,
	sandbox.StrategySessionAdmission,
	SandboxStrategySizingFacts,
	sandbox.StrategyMarketInput,
	sandbox.StrategyOwnedInventory,
	backtest.Strategy,
) (sandbox.StrategyPipelineDependencies, error) {
	source.calls++
	return sandbox.StrategyPipelineDependencies{}, nil
}

type decisionExecutorRecorder struct{ calls int }

func (recorder *decisionExecutorRecorder) RecordSandboxStrategyDecision(
	context.Context,
	string,
	uint64,
	sandbox.StrategySessionWork,
	sandbox.StrategyDecisionEvidence,
	time.Time,
) error {
	recorder.calls++
	return nil
}
