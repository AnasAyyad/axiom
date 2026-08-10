package crossarb

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
	runtimecore "axiom/internal/runtime"
)

func TestSagaAdapterEvaluatesAllViableDirectionsThenClaimsOneDeterministicWinner(t *testing.T) {
	input := inputFromEvaluation(t, evaluationFixture(t, "BTC", false))
	candidates, value := crossSagaCandidatesForTest(t, input)
	if len(value.Candidates) == 0 {
		t.Fatal("no viable candidate reached shared allocation")
	}
	winner := rankedCandidates(value.Candidates)[0]
	claims := claimSetForCandidate(t, winner)
	allocator, err := NewAtomicSagaAllocator(claims, runtimecore.FencingToken(7))
	if err != nil {
		t.Fatal(err)
	}
	allocation, err := allocator.AllocateSaga(context.Background(), candidates)
	if err != nil {
		t.Fatal(err)
	}
	var allocated sagaAllocation
	if err = json.Unmarshal(allocation.Payload, &allocated); err != nil {
		t.Fatal(err)
	}
	if allocated.Candidate.ID != winner.ID || allocated.Claims.State != portfolio.ClaimActive {
		t.Fatalf("allocation=%#v winner=%#v", allocated, winner)
	}
	if err = allocator.CloseSagaAllocation(context.Background(), allocation, backtest.AllocationReleased); err != nil {
		t.Fatal(err)
	}
	if claims.State().Groups[0].State != portfolio.ClaimReleased {
		t.Fatal("released allocation was not persisted")
	}
	value.Candidates[0].ID = "forged-candidate"
	candidates.Payload, err = json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = allocator.AllocateSaga(context.Background(), candidates); err == nil {
		t.Fatal("allocator accepted a candidate payload that differed from the recorded evaluation")
	}
}

func crossSagaCandidatesForTest(t *testing.T, input Input) (backtest.SagaCandidate, sagaCandidateSet) {
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
	return candidates, value
}

func TestSagaAllocatorFailsClosedWithoutCompleteTwoVenueClaimSet(t *testing.T) {
	input := inputFromEvaluation(t, evaluationFixture(t, "BTC", false))
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
	claims := portfolio.NewAtomicClaimSet()
	allocator, err := NewAtomicSagaAllocator(claims, 7)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = allocator.AllocateSaga(context.Background(), candidates); err == nil {
		t.Fatal("allocator accepted missing shared capacity")
	}
	if len(claims.State().Groups) != 0 {
		t.Fatal("failed allocation created a partial claim")
	}
}

func TestSagaRiskAdapterUsesCentralEngineForExactTwoVenueAllocation(t *testing.T) {
	input := inputFromEvaluation(t, evaluationFixture(t, "BTC", false))
	allocation := sagaAllocationForTest(t, input)
	engine := &sagaRiskEvaluator{decision: risk.Decision{Action: risk.ActionApprove, ReasonCode: "approved"}}
	adapter, err := NewSagaRiskAdapter(engine, sagaRiskInputFunc(func(Input) (RiskInput, error) {
		return RiskInput{}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	approved, err := adapter.ApproveSaga(context.Background(), allocation)
	if err != nil || approved.Ordinal != input.Ordinal || len(engine.requests) != 1 {
		t.Fatalf("approved=%#v requests=%d error=%v", approved, len(engine.requests), err)
	}
	var value sagaApproval
	if err = json.Unmarshal(approved.Payload, &value); err != nil ||
		value.Decision.Action != risk.ActionApprove || value.Allocation.Candidate.ID == "" {
		t.Fatalf("approval=%#v error=%v", value, err)
	}
}

func TestSagaRiskAdapterRejectsCentralRiskBlock(t *testing.T) {
	input := inputFromEvaluation(t, evaluationFixture(t, "BTC", false))
	allocation := sagaAllocationForTest(t, input)
	engine := &sagaRiskEvaluator{decision: risk.Decision{Action: risk.ActionReject, ReasonCode: "policy_blocked"}}
	adapter, err := NewSagaRiskAdapter(engine, sagaRiskInputFunc(func(Input) (RiskInput, error) {
		return RiskInput{}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = adapter.ApproveSaga(context.Background(), allocation); err == nil || len(engine.requests) != 1 {
		t.Fatalf("central risk block accepted; requests=%d error=%v", len(engine.requests), err)
	}
}

func TestSagaPlannerMaterializesOnlyApprovedConcurrentExecution(t *testing.T) {
	input := inputFromEvaluation(t, evaluationFixture(t, "BTC", false))
	allocation := sagaAllocationForTest(t, input)
	approved := approvedSagaForTest(t, allocation)
	plan, err := NewSagaPlanner().PlanSaga(context.Background(), approved)
	if err != nil || plan.Ordinal != input.Ordinal {
		t.Fatalf("plan=%#v error=%v", plan, err)
	}
	var value sagaPlan
	if err = json.Unmarshal(plan.Payload, &value); err != nil ||
		value.Execution.Policy != execution.DispatchConcurrent || value.Execution.State != execution.PlanPlanned ||
		len(value.Execution.Legs) != 2 || value.Approval.Allocation.Candidate.ID == "" {
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
	input := inputFromEvaluation(t, evaluationFixture(t, "BTC", false))
	allocation := sagaAllocationForTest(t, input)
	plan, err := NewSagaPlanner().PlanSaga(context.Background(), approvedSagaForTest(t, allocation))
	if err != nil {
		t.Fatal(err)
	}
	projected, err := DecodeApprovedSandboxSaga(plan)
	if err != nil || projected.Candidate.ID == "" ||
		projected.Execution.Policy != execution.DispatchConcurrent ||
		len(projected.Execution.Legs) != 2 {
		t.Fatalf("projected=%#v error=%v", projected, err)
	}
	legs, err := projected.SandboxLegs()
	if err != nil || len(legs) != 2 || legs[0].Exchange != projected.Candidate.BuyExchange ||
		legs[1].Exchange != projected.Candidate.SellExchange ||
		legs[0].OrderID != projected.Execution.Legs[0].OrderID ||
		legs[1].OrderID != projected.Execution.Legs[1].OrderID ||
		legs[0].LimitPrice.String() == "0" || legs[1].LimitPrice.String() == "0" {
		t.Fatalf("sandbox legs=%#v error=%v", legs, err)
	}
	changed := projected
	changed.Candidate.ID += "-changed"
	if _, err = changed.SandboxLegs(); err == nil {
		t.Fatal("mutated cross-exchange candidate projected sandbox orders")
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
		t.Fatal("tampered cross-exchange order identity reached sandbox projection")
	}
}

func TestSagaSimulationBrokerExecutesOnlyExactApprovedConcurrentPlan(t *testing.T) {
	input := inputFromEvaluation(t, evaluationFixture(t, "BTC", false))
	allocation := sagaAllocationForTest(t, input)
	plan, err := NewSagaPlanner().PlanSaga(context.Background(), approvedSagaForTest(t, allocation))
	if err != nil {
		t.Fatal(err)
	}
	evaluation, err := input.EvaluationInput()
	if err != nil {
		t.Fatal(err)
	}
	broker, err := NewSagaSimulationBroker(sagaSimulationInputFunc(func(Input) (Timeline, LatencyDistribution, RecoveryPolicy, error) {
		return &fixtureTimeline{markets: map[string]Market{
			"binance": evaluation.Markets[0], "bybit": evaluation.Markets[1],
		}, directives: arrivalStates(execution.OrderFilled, execution.OrderFilled)}, testLatency(), RecoveryPolicy{}, nil
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
		value.Simulation.Outcome != OutcomeBothFilled || value.Simulation.Saga.State != execution.PlanCompleted {
		t.Fatalf("simulation=%#v error=%v", value, err)
	}
	var tampered sagaPlan
	if err = json.Unmarshal(plan.Payload, &tampered); err != nil {
		t.Fatal(err)
	}
	tampered.Execution.Policy = execution.DispatchSequential
	plan.Payload, err = json.Marshal(tampered)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = broker.SubmitSaga(context.Background(), plan); err == nil {
		t.Fatal("broker accepted a plan that differed from the approved candidate")
	}
}

func TestSagaReducerPostsJournalReconcilesThenReleasesCompletedAllocation(t *testing.T) {
	input := inputFromEvaluation(t, evaluationFixture(t, "BTC", false))
	allocation := sagaAllocationForTest(t, input)
	plan, executed := crossSagaExecutedForTest(t, input, allocation)
	memory := accounting.NewMemoryJournal()
	runID, _ := domain.NewRunID("crossarb-saga-run")
	portfolioID, _ := domain.NewPortfolioID("crossarb-saga-portfolio")
	journal, err := NewCrossExchangeJournal(memory, JournalContext{RunID: runID, PortfolioID: portfolioID,
		Owner: "portfolio-cross_exchange_arbitrage", ConfigurationHash: input.ConfigurationHash,
		RecordedAt: domain.EventTime{UTC: input.Now, Sequence: 1}, FirstOrdinal: 100})
	if err != nil {
		t.Fatal(err)
	}
	cases, incidents, quarantine := &sagaCases{}, &sagaIncidents{}, &sagaQuarantine{}
	reconciler, err := reconciliation.NewReconciler(cases, incidents, quarantine, memory,
		reconciliation.Context{RunID: runID, PortfolioID: portfolioID, Owner: "portfolio-cross_exchange_arbitrage",
			ConfigurationHash: input.ConfigurationHash})
	if err != nil {
		t.Fatal(err)
	}
	provider := &sagaReductionProvider{journal: journal, attribution: completeAttribution(), reconciliation: SagaReconciliation{
		Reconciler: reconciler, Scope: "crossarb/" + input.ConfigurationHash,
		Expected: matchingSagaReconciliationState(), Actual: matchingSagaReconciliationState(), At: input.Now,
	}}
	reducer, err := NewSagaReducer(provider)
	if err != nil {
		t.Fatal(err)
	}
	result, err := reducer.ReduceSaga(context.Background(), allocation, plan, executed)
	if err != nil || result.Ordinal != input.Ordinal || provider.releases != 1 || len(memory.Transactions()) != 11 {
		t.Fatalf("result=%#v releases=%d transactions=%d error=%v", result, provider.releases, len(memory.Transactions()), err)
	}
	var evidence sagaReductionEvidence
	if err = json.Unmarshal(result.Balances, &evidence); err != nil || len(evidence.Transactions) != 11 ||
		evidence.Reconciliation.ID != "" || len(cases.items) != 0 || len(incidents.items) != 0 || len(quarantine.items) != 0 {
		t.Fatalf("evidence=%#v error=%v", evidence, err)
	}
}

func crossSagaExecutedForTest(t *testing.T, input Input, allocation backtest.SagaAllocation) (
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
	broker, err := NewSagaSimulationBroker(sagaSimulationInputFunc(func(Input) (Timeline, LatencyDistribution, RecoveryPolicy, error) {
		return &fixtureTimeline{markets: map[string]Market{
			"binance": evaluation.Markets[0], "bybit": evaluation.Markets[1],
		}, directives: arrivalStates(execution.OrderFilled, execution.OrderFilled)}, testLatency(), RecoveryPolicy{}, nil
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

func TestSagaPipelineProcessorRunsEveryCrossExchangeStageWithoutBypass(t *testing.T) {
	input, event := crossSagaPipelineInput(t)
	operational, memory, allocator := crossSagaPipelineProcessor(t, input)
	result, err := operational.Process(context.Background(), event)
	if err != nil || result.Ordinal != input.Ordinal || len(memory.Transactions()) != 11 || len(allocator.active) != 0 {
		t.Fatalf("result=%#v transactions=%d active_claims=%d error=%v", result, len(memory.Transactions()), len(allocator.active), err)
	}
	if operational.Metrics().Trades != 2 {
		t.Fatalf("completed cross-exchange trade legs=%d", operational.Metrics().Trades)
	}
}

func crossSagaPipelineInput(t *testing.T) (Input, replay.Event) {
	t.Helper()
	input := inputFromEvaluation(t, evaluationFixture(t, "BTC", false))
	input.CentralRisk = &RiskInput{Policies: []risk.Policy{{ID: "crossarb-pipeline-risk", Version: 1,
		Scope: risk.Scope{Kind: risk.ScopeStrategy, ID: "crossarb"}, State: risk.StateNormal}}, EvaluatedAt: input.Now}
	state := matchingSagaReconciliationState()
	input.Reduction = &ReductionInput{Attribution: completeAttribution(),
		Reconciliation: ReconciliationInput{Scope: "crossarb/" + input.ConfigurationHash,
			Expected: state, Actual: state, At: input.Now}}
	input.Simulation = &SimulationInput{Latency: testLatency(), Recovery: RecoveryPolicy{}}
	for _, offset := range []uint64{input.LogicalTime + 10, input.LogicalTime + 20} {
		for _, market := range input.Markets {
			input.Simulation.Markets = append(input.Simulation.Markets, TimedMarketInput{Offset: offset, Market: market})
		}
	}
	input.Simulation.Directives = []TimedDirective{
		{Exchange: "binance", Phase: PhaseArrival, Offset: input.LogicalTime + 10,
			Directive: LegDirective{State: execution.OrderFilled}},
		{Exchange: "bybit", Phase: PhaseArrival, Offset: input.LogicalTime + 20,
			Directive: LegDirective{State: execution.OrderFilled}},
	}
	payload, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	return input, replay.Event{Ordinal: input.Ordinal, LogicalTime: input.LogicalTime, Canonical: payload}
}

func crossSagaPipelineProcessor(t *testing.T, input Input) (
	*OperationalProcessor, *accounting.MemoryJournal, *RecordedSagaAllocator,
) {
	t.Helper()
	allocator, err := NewRecordedSagaAllocator(7)
	if err != nil {
		t.Fatal(err)
	}
	riskAdapter, err := NewSagaRiskAdapter(&sagaRiskEvaluator{decision: risk.Decision{Action: risk.ActionApprove, ReasonCode: "approved"}}, RecordedSagaRiskInputs{})
	if err != nil {
		t.Fatal(err)
	}
	broker, err := NewSagaSimulationBroker(RecordedSagaSimulationInputs{})
	if err != nil {
		t.Fatal(err)
	}
	memory := accounting.NewMemoryJournal()
	reducer := crossSagaPipelineReducer(t, input, memory, allocator)
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

func crossSagaPipelineReducer(t *testing.T, input Input, memory *accounting.MemoryJournal,
	allocator *RecordedSagaAllocator,
) *SagaReducer {
	t.Helper()
	runID, _ := domain.NewRunID("crossarb-pipeline-run")
	portfolioID, _ := domain.NewPortfolioID("crossarb-pipeline-portfolio")
	cases, incidents, quarantine := &sagaCases{}, &sagaIncidents{}, &sagaQuarantine{}
	reconciler, err := reconciliation.NewReconciler(cases, incidents, quarantine, memory,
		reconciliation.Context{RunID: runID, PortfolioID: portfolioID, Owner: "portfolio-cross_exchange_arbitrage",
			ConfigurationHash: input.ConfigurationHash})
	if err != nil {
		t.Fatal(err)
	}
	reductionProvider, err := NewRecordedSagaReductionProvider(memory, reconciler, runID, portfolioID, "portfolio-cross_exchange_arbitrage", allocator)
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
	if err = json.Unmarshal(candidates.Payload, &value); err != nil || len(value.Candidates) == 0 {
		t.Fatalf("candidates=%#v error=%v", value, err)
	}
	claims := claimSetForCandidate(t, rankedCandidates(value.Candidates)[0])
	allocator, err := NewAtomicSagaAllocator(claims, 1)
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

type sagaSimulationInputFunc func(Input) (Timeline, LatencyDistribution, RecoveryPolicy, error)

func (function sagaSimulationInputFunc) SimulationInput(input Input) (Timeline, LatencyDistribution, RecoveryPolicy, error) {
	return function(input)
}

type sagaReductionProvider struct {
	journal        *CrossExchangeJournal
	attribution    PortfolioAttribution
	reconciliation SagaReconciliation
	release        func(context.Context, backtest.SagaAllocation) error
	releases       int
}

func (provider *sagaReductionProvider) Journal(Input) (*CrossExchangeJournal, error) {
	return provider.journal, nil
}

func (provider *sagaReductionProvider) Attribution(Input, Candidate, SimulationResult) (PortfolioAttribution, error) {
	return provider.attribution, nil
}

func (provider *sagaReductionProvider) Reconciliation(Input, Candidate, SimulationResult) (SagaReconciliation, error) {
	return provider.reconciliation, nil
}

func (provider *sagaReductionProvider) Release(ctx context.Context, value backtest.SagaAllocation) error {
	provider.releases++
	if provider.release != nil {
		return provider.release(ctx, value)
	}
	return nil
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

type sagaRiskEvaluator struct {
	requests []risk.Request
	decision risk.Decision
}

func (engine *sagaRiskEvaluator) Evaluate(request risk.Request) (risk.Decision, error) {
	engine.requests = append(engine.requests, request)
	return engine.decision, nil
}

func claimSetForCandidate(t *testing.T, candidate Candidate) *portfolio.AtomicClaimSet {
	t.Helper()
	claims := portfolio.NewAtomicClaimSet()
	for _, requirement := range candidate.Claims {
		key := portfolio.ClaimKey{Kind: mustClaimKind(t, requirement.Kind), Owner: stringsLower(requirement.Owner),
			Exchange: stringsLower(requirement.Exchange), Resource: stringsLower(requirement.Resource)}
		if err := claims.OpenResource(key, requirement.Quantity); err != nil {
			t.Fatal(err)
		}
	}
	return claims
}
