package triangular

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"axiom/internal/accounting"
	"axiom/internal/backtest"
	"axiom/internal/domain"
	"axiom/internal/execution"
	"axiom/internal/portfolio"
	"axiom/internal/reconciliation"
	"axiom/internal/replay"
	"axiom/internal/risk"
)

func TestSagaAdapterEvaluatesAllCandidatesThenClaimsOneDeterministicWinner(t *testing.T) {
	input := inputFromEvaluation(t, profitableInput(t, false))
	payload, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	strategy := NewSagaStrategyAdapter()
	candidateSet, err := strategy.EvaluateSaga(context.Background(), replay.Event{
		Ordinal: input.Ordinal, LogicalTime: input.LogicalTime, Canonical: payload,
	})
	if err != nil {
		t.Fatal(err)
	}
	var decoded sagaCandidateSet
	if err = json.Unmarshal(candidateSet.Payload, &decoded); err != nil || len(decoded.Candidates) < 2 {
		t.Fatalf("candidate set=%#v error=%v", decoded, err)
	}
	winner := rankedCandidates(decoded.Candidates)[0]
	claims := NewCandidateClaimFixture(t, winner, "portfolio-a")
	allocator, err := NewAtomicSagaAllocator(claims, "portfolio-a", 7)
	if err != nil {
		t.Fatal(err)
	}
	allocation, err := allocator.AllocateSaga(context.Background(), candidateSet)
	if err != nil {
		t.Fatal(err)
	}
	var allocated sagaAllocation
	if err = json.Unmarshal(allocation.Payload, &allocated); err != nil || allocated.Candidate.ID != winner.ID ||
		allocated.Claims.State != portfolio.ClaimActive {
		t.Fatalf("allocation=%#v error=%v", allocated, err)
	}
	if err = allocator.CloseSagaAllocation(context.Background(), allocation, backtest.AllocationReleased); err != nil {
		t.Fatal(err)
	}
	stored, ok := claims.Group(allocated.Claims.ID)
	if !ok || stored.State != portfolio.ClaimReleased {
		t.Fatalf("claim state=%#v exists=%t", stored, ok)
	}
	decoded.Candidates[0].ID = "forged-candidate"
	candidateSet.Payload, err = json.Marshal(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = allocator.AllocateSaga(context.Background(), candidateSet); err == nil {
		t.Fatal("allocator accepted a candidate payload that differed from the recorded evaluation")
	}
}

func TestSagaAllocatorFailsClosedWhenNoCandidateHasACompleteClaimSet(t *testing.T) {
	input := inputFromEvaluation(t, profitableInput(t, false))
	payload, _ := json.Marshal(input)
	candidateSet, err := NewSagaStrategyAdapter().EvaluateSaga(context.Background(), replay.Event{
		Ordinal: input.Ordinal, LogicalTime: input.LogicalTime, Canonical: payload,
	})
	if err != nil {
		t.Fatal(err)
	}
	claims := portfolio.NewAtomicClaimSet()
	allocator, err := NewAtomicSagaAllocator(claims, "portfolio-a", 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = allocator.AllocateSaga(context.Background(), candidateSet); err == nil {
		t.Fatal("allocator accepted a candidate without every shared resource")
	}
	if len(claims.State().Groups) != 0 {
		t.Fatal("failed allocation created a partial claim")
	}
}

func TestSagaRiskAdapterUsesCentralEngineForTheExactAtomicAllocation(t *testing.T) {
	input := inputFromEvaluation(t, profitableInput(t, false))
	allocation := sagaAllocationForTest(t, input)
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	engine, err := risk.NewEngine(&triangularRiskAudit{}, &triangularRiskAlerts{})
	if err != nil {
		t.Fatal(err)
	}
	if err = engine.ManualTransition(risk.StateNormal, triangularRecoveryEvidence(now)); err != nil {
		t.Fatal(err)
	}
	adapter, err := NewSagaRiskAdapter(engine, sagaRiskInputFunc(func(Input) (RiskInput, error) {
		return RiskInput{Policies: []risk.Policy{triangularRiskPolicy(risk.StateNormal)},
			Observations: triangularHealthyRiskObservations(), EvaluatedAt: now.Add(time.Second)}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	approved, err := adapter.ApproveSaga(context.Background(), allocation)
	if err != nil || approved.Ordinal != input.Ordinal {
		t.Fatalf("approved=%#v error=%v", approved, err)
	}
	var decision sagaApproval
	if err = json.Unmarshal(approved.Payload, &decision); err != nil ||
		decision.Decision.Action != risk.ActionApprove || decision.Allocation.Candidate.ID == "" {
		t.Fatalf("risk decision=%#v error=%v", decision, err)
	}
}

func TestSagaRiskAdapterRejectsCentralRiskBlock(t *testing.T) {
	input := inputFromEvaluation(t, profitableInput(t, false))
	allocation := sagaAllocationForTest(t, input)
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	engine, err := risk.NewEngine(&triangularRiskAudit{}, &triangularRiskAlerts{})
	if err != nil {
		t.Fatal(err)
	}
	if err = engine.ManualTransition(risk.StateNormal, triangularRecoveryEvidence(now)); err != nil {
		t.Fatal(err)
	}
	adapter, err := NewSagaRiskAdapter(engine, sagaRiskInputFunc(func(Input) (RiskInput, error) {
		observations := triangularHealthyRiskObservations()
		observations.BookAge = durationPointer(250 * time.Millisecond)
		return RiskInput{Policies: []risk.Policy{triangularRiskPolicy(risk.StateNormal)},
			Observations: observations, EvaluatedAt: now.Add(time.Second)}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = adapter.ApproveSaga(context.Background(), allocation); err == nil {
		t.Fatal("central risk block was accepted")
	}
}

func TestSagaPlannerMaterializesOnlyApprovedSequentialExecution(t *testing.T) {
	input := inputFromEvaluation(t, profitableInput(t, false))
	allocation := sagaAllocationForTest(t, input)
	approved := approvedSagaForTest(t, allocation)
	plan, err := NewSagaPlanner().PlanSaga(context.Background(), approved)
	if err != nil || plan.Ordinal != input.Ordinal {
		t.Fatalf("plan=%#v error=%v", plan, err)
	}
	var value sagaPlan
	if err = json.Unmarshal(plan.Payload, &value); err != nil ||
		value.Execution.Policy != execution.DispatchSequential || value.Execution.State != execution.PlanPlanned ||
		len(value.Execution.Legs) != 3 || value.Approval.Allocation.Candidate.ID == "" {
		t.Fatalf("planned=%#v error=%v", value, err)
	}
	blocked := approved
	var rejection sagaApproval
	if err = json.Unmarshal(approved.Payload, &rejection); err != nil {
		t.Fatal(err)
	}
	rejection.Decision.Action = risk.ActionReject
	blocked.Payload, err = json.Marshal(rejection)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = NewSagaPlanner().PlanSaga(context.Background(), blocked); err == nil {
		t.Fatal("central-risk rejection materialized a plan")
	}
}

func TestApprovedSandboxSagaProjectionRejectsPlannerPayloadTampering(t *testing.T) {
	input := inputFromEvaluation(t, profitableInput(t, false))
	allocation := sagaAllocationForTest(t, input)
	plan, err := NewSagaPlanner().PlanSaga(context.Background(), approvedSagaForTest(t, allocation))
	if err != nil {
		t.Fatal(err)
	}
	projected, err := DecodeApprovedSandboxSaga(plan)
	if err != nil || projected.Candidate.ID == "" ||
		projected.Execution.Policy != execution.DispatchSequential ||
		len(projected.Candidate.Legs) != 3 || len(projected.Execution.Legs) != 3 {
		t.Fatalf("projected=%#v error=%v", projected, err)
	}
	legs, err := projected.SandboxLegs()
	if err != nil || len(legs) != 3 {
		t.Fatalf("sandbox legs=%#v error=%v", legs, err)
	}
	for index, leg := range legs {
		if leg.Index != uint32(index) || leg.OrderID != projected.Execution.Legs[index].OrderID ||
			leg.Instrument != projected.Candidate.Legs[index].Instrument ||
			leg.Quantity.Compare(projected.Candidate.Legs[index].TradeQuantity) != 0 ||
			leg.LimitPrice.String() == "0" || leg.Notional.String() == "0" {
			t.Fatalf("sandbox leg %d=%#v", index, leg)
		}
	}
	changed := projected
	changed.Candidate.ID += "-changed"
	if _, err = changed.SandboxLegs(); err == nil {
		t.Fatal("mutated triangular candidate projected sandbox orders")
	}
	var tampered sagaPlan
	if err = json.Unmarshal(plan.Payload, &tampered); err != nil {
		t.Fatal(err)
	}
	tampered.Execution.Legs[1].OrderID = tampered.Execution.Legs[0].OrderID
	plan.Payload, err = json.Marshal(tampered)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = DecodeApprovedSandboxSaga(plan); err == nil {
		t.Fatal("tampered triangular order identity reached sandbox projection")
	}
}

func TestSagaSimulationBrokerExecutesOnlyExactApprovedSequentialPlan(t *testing.T) {
	input := inputFromEvaluation(t, profitableInput(t, false))
	allocation := sagaAllocationForTest(t, input)
	plan, err := NewSagaPlanner().PlanSaga(context.Background(), approvedSagaForTest(t, allocation))
	if err != nil {
		t.Fatal(err)
	}
	evaluation, err := input.EvaluationInput()
	if err != nil {
		t.Fatal(err)
	}
	broker, err := NewSagaSimulationBroker(sagaSimulationInputFunc(func(Input) (Timeline, LatencyModel, error) {
		return &scriptedTimeline{markets: evaluation.Markets}, testLatency(), nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	executed, err := broker.SubmitSaga(context.Background(), plan)
	if err != nil || executed.Ordinal != input.Ordinal {
		t.Fatalf("execution=%#v error=%v", executed, err)
	}
	var value sagaExecution
	if err = json.Unmarshal(executed.Payload, &value); err != nil ||
		value.Simulation.Outcome != OutcomeFullSuccess || value.Simulation.Saga.State != execution.PlanCompleted {
		t.Fatalf("simulation=%#v error=%v", value, err)
	}
	var tampered sagaPlan
	if err = json.Unmarshal(plan.Payload, &tampered); err != nil {
		t.Fatal(err)
	}
	tampered.Execution.Policy = execution.DispatchConcurrent
	plan.Payload, err = json.Marshal(tampered)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = broker.SubmitSaga(context.Background(), plan); err == nil {
		t.Fatal("broker accepted a plan that differed from the approved candidate")
	}
}

func TestSagaReducerPostsJournalReconcilesThenReleasesCompletedAllocation(t *testing.T) {
	input := inputFromEvaluation(t, profitableInput(t, false))
	allocation := sagaAllocationForTest(t, input)
	plan, executed := triangularSagaExecutedForTest(t, input, allocation)
	memory := accounting.NewMemoryJournal()
	runID, _ := domain.NewRunID("triangular-saga-run")
	portfolioID, _ := domain.NewPortfolioID("triangular-saga-portfolio")
	journal, err := NewCycleJournal(memory, JournalContext{RunID: runID, PortfolioID: portfolioID,
		Owner: "portfolio-a", ConfigurationHash: input.ConfigurationHash,
		RecordedAt: domain.EventTime{UTC: input.Now, Sequence: 1}, FirstOrdinal: 100})
	if err != nil {
		t.Fatal(err)
	}
	cases, incidents, quarantine := &sagaCases{}, &sagaIncidents{}, &sagaQuarantine{}
	reconciler, err := reconciliation.NewReconciler(cases, incidents, quarantine, memory,
		reconciliation.Context{RunID: runID, PortfolioID: portfolioID, Owner: "portfolio-a",
			ConfigurationHash: input.ConfigurationHash})
	if err != nil {
		t.Fatal(err)
	}
	provider := &sagaReductionProvider{journal: journal, reconciliation: SagaReconciliation{
		Reconciler: reconciler, Scope: "triangular/" + input.ConfigurationHash,
		Expected: matchingSagaReconciliationState(), Actual: matchingSagaReconciliationState(), At: input.Now,
	}}
	reducer, err := NewSagaReducer(provider)
	if err != nil {
		t.Fatal(err)
	}
	result, err := reducer.ReduceSaga(context.Background(), allocation, plan, executed)
	if err != nil || result.Ordinal != input.Ordinal || provider.closes != 1 ||
		provider.disposition != backtest.AllocationReleased || len(memory.Transactions()) == 0 {
		t.Fatalf("result=%#v closes=%d disposition=%s transactions=%d error=%v", result, provider.closes,
			provider.disposition, len(memory.Transactions()), err)
	}
	var evidence sagaReductionEvidence
	if err = json.Unmarshal(result.Balances, &evidence); err != nil || len(evidence.Transactions) == 0 ||
		evidence.Reconciliation.ID != "" || len(cases.items) != 0 || len(incidents.items) != 0 || len(quarantine.items) != 0 {
		t.Fatalf("evidence=%#v error=%v", evidence, err)
	}
}

func TestSagaReducerQuarantinesRatherThanReleasesOnCriticalReconciliationMismatch(t *testing.T) {
	input := inputFromEvaluation(t, profitableInput(t, false))
	allocation := sagaAllocationForTest(t, input)
	plan, executed := triangularSagaExecutedForTest(t, input, allocation)
	memory := accounting.NewMemoryJournal()
	runID, _ := domain.NewRunID("triangular-reconciliation-run")
	portfolioID, _ := domain.NewPortfolioID("triangular-reconciliation-portfolio")
	journal, err := NewCycleJournal(memory, JournalContext{RunID: runID, PortfolioID: portfolioID,
		Owner: "portfolio-a", ConfigurationHash: input.ConfigurationHash,
		RecordedAt: domain.EventTime{UTC: input.Now, Sequence: 1}, FirstOrdinal: 100})
	if err != nil {
		t.Fatal(err)
	}
	cases, incidents, quarantine := &sagaCases{}, &sagaIncidents{}, &sagaQuarantine{}
	reconciler, err := reconciliation.NewReconciler(cases, incidents, quarantine, memory,
		reconciliation.Context{RunID: runID, PortfolioID: portfolioID, Owner: "portfolio-a",
			ConfigurationHash: input.ConfigurationHash})
	if err != nil {
		t.Fatal(err)
	}
	actual := matchingSagaReconciliationState()
	actual.Orders = strings.Repeat("b", 64)
	provider := &sagaReductionProvider{journal: journal, reconciliation: SagaReconciliation{
		Reconciler: reconciler, Scope: "triangular/" + input.ConfigurationHash,
		Expected: matchingSagaReconciliationState(), Actual: actual, At: input.Now,
	}}
	reducer, err := NewSagaReducer(provider)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = reducer.ReduceSaga(context.Background(), allocation, plan, executed); err == nil ||
		provider.closes != 0 || len(cases.items) != 1 || len(incidents.items) != 1 || len(quarantine.items) != 1 {
		t.Fatalf("closes=%d cases=%#v incidents=%#v quarantine=%#v error=%v", provider.closes, cases.items, incidents.items, quarantine.items, err)
	}
}

func triangularSagaExecutedForTest(t *testing.T, input Input, allocation backtest.SagaAllocation) (
	backtest.SagaPlan, backtest.SagaExecution,
) {
	t.Helper()
	plan, err := NewSagaPlanner().PlanSaga(context.Background(), approvedSagaForTest(t, allocation))
	if err != nil {
		t.Fatal(err)
	}
	evaluation, err := input.EvaluationInput()
	if err != nil {
		t.Fatal(err)
	}
	broker, err := NewSagaSimulationBroker(sagaSimulationInputFunc(func(Input) (Timeline, LatencyModel, error) {
		return &scriptedTimeline{markets: evaluation.Markets}, testLatency(), nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	executed, err := broker.SubmitSaga(context.Background(), plan)
	if err != nil {
		t.Fatal(err)
	}
	return plan, executed
}

func TestSagaPipelineProcessorRunsEveryTriangularStageWithoutBypass(t *testing.T) {
	input, event := triangularSagaPipelineInput(t)
	operational, memory, allocator := triangularSagaPipelineProcessor(t, input)
	result, err := operational.Process(context.Background(), event)
	if err != nil || result.Ordinal != input.Ordinal || len(memory.Transactions()) == 0 || len(allocator.active) != 0 {
		t.Fatalf("result=%#v transactions=%d active_claims=%d error=%v", result, len(memory.Transactions()), len(allocator.active), err)
	}
	if operational.Metrics().Trades != 3 {
		t.Fatalf("completed triangular trade legs=%d", operational.Metrics().Trades)
	}
}

func triangularSagaPipelineInput(t *testing.T) (Input, replay.Event) {
	t.Helper()
	input := inputFromEvaluation(t, profitableInput(t, false))
	input.CentralRisk = &RiskInput{Policies: []risk.Policy{triangularRiskPolicy(risk.StateNormal)},
		Observations: triangularHealthyRiskObservations(), EvaluatedAt: input.Now}
	state := matchingSagaReconciliationState()
	input.Reduction = &ReductionInput{Reconciliation: ReconciliationInput{Scope: "triangular/" + input.ConfigurationHash,
		Expected: state, Actual: state, At: input.Now}}
	input.Simulation = &SimulationInput{Latency: testLatency()}
	for _, offset := range []uint64{input.LogicalTime + 10, input.LogicalTime + 30, input.LogicalTime + 60} {
		for _, market := range input.Markets {
			input.Simulation.Markets = append(input.Simulation.Markets, TimedMarketInput{Offset: offset, Market: market})
		}
	}
	payload, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	return input, replay.Event{Ordinal: input.Ordinal, LogicalTime: input.LogicalTime, Canonical: payload}
}

func triangularSagaPipelineProcessor(t *testing.T, input Input) (
	*OperationalProcessor, *accounting.MemoryJournal, *RecordedSagaAllocator,
) {
	t.Helper()
	allocator, err := NewRecordedSagaAllocator("portfolio-a", 7)
	if err != nil {
		t.Fatal(err)
	}
	riskAdapter, err := NewSagaRiskAdapter(sagaAllowRiskEvaluator{}, RecordedSagaRiskInputs{})
	if err != nil {
		t.Fatal(err)
	}
	broker, err := NewSagaSimulationBroker(RecordedSagaSimulationInputs{})
	if err != nil {
		t.Fatal(err)
	}
	memory := accounting.NewMemoryJournal()
	reducer := triangularSagaPipelineReducer(t, input, memory, allocator)
	pipeline, err := backtest.NewSagaPipelineProcessor(backtest.SagaPipelineDependencies{
		Strategy: NewSagaStrategyAdapter(), Allocator: allocator, Risk: riskAdapter, Planner: NewSagaPlanner(), Broker: broker,
		Reducer: reducer, Metrics: func() backtest.Metrics { return backtest.Metrics{} },
	})
	if err != nil {
		t.Fatal(err)
	}
	operational, err := NewOperationalProcessor(pipeline)
	if err != nil {
		t.Fatal(err)
	}
	return operational, memory, allocator
}

func triangularSagaPipelineReducer(t *testing.T, input Input, memory *accounting.MemoryJournal,
	allocator *RecordedSagaAllocator,
) *SagaReducer {
	t.Helper()
	runID, _ := domain.NewRunID("triangular-pipeline-run")
	portfolioID, _ := domain.NewPortfolioID("triangular-pipeline-portfolio")
	cases, incidents, quarantine := &sagaCases{}, &sagaIncidents{}, &sagaQuarantine{}
	reconciler, err := reconciliation.NewReconciler(cases, incidents, quarantine, memory,
		reconciliation.Context{RunID: runID, PortfolioID: portfolioID, Owner: "portfolio-a",
			ConfigurationHash: input.ConfigurationHash})
	if err != nil {
		t.Fatal(err)
	}
	reductionProvider, err := NewRecordedSagaReductionProvider(memory, reconciler, runID, portfolioID, "portfolio-a", allocator)
	if err != nil {
		t.Fatal(err)
	}
	reducer, err := NewSagaReducer(reductionProvider)
	if err != nil {
		t.Fatal(err)
	}
	return reducer
}

func sagaAllocationForTest(t *testing.T, input Input) backtest.SagaAllocation {
	t.Helper()
	payload, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	candidates, err := NewSagaStrategyAdapter().EvaluateSaga(context.Background(), replay.Event{
		Ordinal: input.Ordinal, LogicalTime: input.LogicalTime, Canonical: payload,
	})
	if err != nil {
		t.Fatal(err)
	}
	var value sagaCandidateSet
	if err = json.Unmarshal(candidates.Payload, &value); err != nil {
		t.Fatal(err)
	}
	winner := rankedCandidates(value.Candidates)[0]
	claims := NewCandidateClaimFixture(t, winner, "portfolio-a")
	allocator, err := NewAtomicSagaAllocator(claims, "portfolio-a", 1)
	if err != nil {
		t.Fatal(err)
	}
	allocation, err := allocator.AllocateSaga(context.Background(), candidates)
	if err != nil {
		t.Fatal(err)
	}
	return allocation
}

func approvedSagaForTest(t *testing.T, allocation backtest.SagaAllocation) backtest.SagaApproval {
	t.Helper()
	var value sagaAllocation
	if err := json.Unmarshal(allocation.Payload, &value); err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(sagaApproval{Allocation: value, Decision: risk.Decision{Action: risk.ActionApprove}})
	if err != nil {
		t.Fatal(err)
	}
	return backtest.SagaApproval{Ordinal: allocation.Ordinal, Payload: payload}
}

type sagaRiskInputFunc func(Input) (RiskInput, error)

func (function sagaRiskInputFunc) RiskInput(input Input) (RiskInput, error) { return function(input) }

type sagaSimulationInputFunc func(Input) (Timeline, LatencyModel, error)

func (function sagaSimulationInputFunc) SimulationInput(input Input) (Timeline, LatencyModel, error) {
	return function(input)
}

type sagaReductionProvider struct {
	journal        *CycleJournal
	reconciliation SagaReconciliation
	close          func(context.Context, backtest.SagaAllocation, backtest.AllocationDisposition) error
	closes         int
	disposition    backtest.AllocationDisposition
}

func (provider *sagaReductionProvider) Journal(Input) (*CycleJournal, error) {
	return provider.journal, nil
}

func (provider *sagaReductionProvider) Reconciliation(Input, Candidate, SimulationResult) (SagaReconciliation, error) {
	return provider.reconciliation, nil
}

func (provider *sagaReductionProvider) Close(ctx context.Context, value backtest.SagaAllocation,
	disposition backtest.AllocationDisposition) error {
	provider.closes++
	provider.disposition = disposition
	if provider.close != nil {
		return provider.close(ctx, value, disposition)
	}
	return nil
}

func TestTriangularReductionDispositionRetainsUnresolvedExposure(t *testing.T) {
	if triangularReductionDisposition(SimulationResult{}) != backtest.AllocationReleased {
		t.Fatal("clean simulation did not release its claim")
	}
	if triangularReductionDisposition(SimulationResult{Recovery: RecoveryResult{Quarantined: true}}) !=
		backtest.AllocationQuarantined {
		t.Fatal("unresolved recovery released its claim")
	}
	if triangularReductionDisposition(SimulationResult{Saga: execution.Saga{State: execution.PlanQuarantined}}) !=
		backtest.AllocationQuarantined {
		t.Fatal("quarantined saga released its claim")
	}
}

func matchingSagaReconciliationState() reconciliation.State {
	hash := strings.Repeat("a", 64)
	return reconciliation.State{Orders: hash, Fills: hash, Reservations: hash, Balances: hash,
		Positions: hash, Ownership: hash, Journal: hash, Projections: hash}
}

type sagaCases struct{ items []reconciliation.Case }

func (store *sagaCases) Create(value reconciliation.Case) error {
	store.items = append(store.items, value)
	return nil
}

type sagaIncidents struct{ items []string }

func (sink *sagaIncidents) Create(scope, reason string, _ time.Time) (string, error) {
	sink.items = append(sink.items, scope+":"+reason)
	return "incident", nil
}

type sagaQuarantine struct{ items []string }

func (sink *sagaQuarantine) Block(scope, reason string) error {
	sink.items = append(sink.items, scope+":"+reason)
	return nil
}

type sagaAllowRiskEvaluator struct{}

func (sagaAllowRiskEvaluator) Evaluate(risk.Request) (risk.Decision, error) {
	return risk.Decision{Action: risk.ActionApprove, ReasonCode: "approved"}, nil
}
