package triangular

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"sort"
	"time"

	"axiom/internal/accounting"
	"axiom/internal/backtest"
	"axiom/internal/domain"
	"axiom/internal/execution"
	"axiom/internal/portfolio"
	"axiom/internal/reconciliation"
)

// SagaReconciler is the durable reconciliation boundary for one completed
// multi-leg event. The runtime supplies simulator or authoritative state for
// its selected mode; the reducer never substitutes global current state.
type SagaReconciler interface {
	Reconcile(string, reconciliation.State, reconciliation.State, time.Time) (reconciliation.Case, error)
}

// SagaReconciliation fixes the expected and observed state comparison for one
// exact canonical input. The clean case is intentionally represented by an
// empty reconciliation Case rather than a fabricated incident.
type SagaReconciliation struct {
	Reconciler SagaReconciler
	Scope      string
	Expected   reconciliation.State
	Actual     reconciliation.State
	At         time.Time
}

// SagaReductionProvider supplies all stateful boundaries that cannot be
// reconstructed from a candidate: the durable journal, mode-specific
// reconciliation evidence, and atomic-claim completion.
type SagaReductionProvider interface {
	Journal(Input) (*CycleJournal, error)
	Reconciliation(Input, Candidate, SimulationResult) (SagaReconciliation, error)
	Close(context.Context, backtest.SagaAllocation, backtest.AllocationDisposition) error
}

// SagaAllocationCloser releases or quarantines the exact claim that the
// allocator created. It keeps the reducer independent from whether a runtime
// holds one long-lived claim set or reconstructs an input-scoped recorded one.
type SagaAllocationCloser interface {
	CloseSagaAllocation(context.Context, backtest.SagaAllocation, backtest.AllocationDisposition) error
}

// RecordedSagaReductionProvider joins immutable recorded reduction evidence to
// the run-scoped durable journal, reconciler, and atomic allocator. Its only
// mutable dependencies are explicit runtime boundaries; it never looks up a
// newer market, portfolio, or reconciliation projection.
type RecordedSagaReductionProvider struct {
	journal     accounting.Journal
	reconciler  SagaReconciler
	runID       domain.RunID
	portfolioID domain.PortfolioID
	owner       string
	allocator   SagaAllocationCloser
}

// NewRecordedSagaReductionProvider constructs the real reduction wiring for
// a credential-free recorded run. The caller retains ownership of every
// durable dependency and must provide the allocator that created the claim.
func NewRecordedSagaReductionProvider(
	journal accounting.Journal,
	reconciler SagaReconciler,
	runID domain.RunID,
	portfolioID domain.PortfolioID,
	owner string,
	allocator SagaAllocationCloser,
) (*RecordedSagaReductionProvider, error) {
	if journal == nil || reconciler == nil || runID.Value() == "" || portfolioID.Value() == "" ||
		owner == "" || allocator == nil {
		return nil, strategyError("saga_reduction_provider_invalid")
	}
	return &RecordedSagaReductionProvider{journal: journal, reconciler: reconciler, runID: runID,
		portfolioID: portfolioID, owner: owner, allocator: allocator}, nil
}

// Journal creates a causally unique journal view using only the input's
// immutable timestamp, ordinal, and configuration identity.
func (provider *RecordedSagaReductionProvider) Journal(input Input) (*CycleJournal, error) {
	first, err := sagaJournalFirstOrdinal(input.Ordinal)
	if provider == nil || err != nil {
		return nil, strategyError("saga_reduction_journal_invalid")
	}
	return NewCycleJournal(provider.journal, JournalContext{RunID: provider.runID, PortfolioID: provider.portfolioID,
		Owner: provider.owner, ConfigurationHash: input.ConfigurationHash,
		RecordedAt: domain.EventTime{UTC: input.Now, Sequence: input.Ordinal}, FirstOrdinal: first})
}

// Reconciliation returns the recorded expected-versus-observed comparison
// with the runtime's durable reconciler; no current state is substituted.
func (provider *RecordedSagaReductionProvider) Reconciliation(
	input Input, _ Candidate, _ SimulationResult,
) (SagaReconciliation, error) {
	reduction, err := input.RecordedReduction()
	if provider == nil || err != nil {
		return SagaReconciliation{}, strategyError("saga_reduction_reconciliation_invalid")
	}
	return SagaReconciliation{Reconciler: provider.reconciler, Scope: reduction.Reconciliation.Scope,
		Expected: reduction.Reconciliation.Expected, Actual: reduction.Reconciliation.Actual,
		At: reduction.Reconciliation.At}, nil
}

// Close applies the reducer's exact terminal disposition only after journal
// and reconciliation evidence is durable. Unresolved exposure is retained as
// quarantined capacity rather than being made available to another cycle.
func (provider *RecordedSagaReductionProvider) Close(
	ctx context.Context,
	allocation backtest.SagaAllocation,
	disposition backtest.AllocationDisposition,
) error {
	if provider == nil || (disposition != backtest.AllocationReleased &&
		disposition != backtest.AllocationQuarantined) {
		return strategyError("saga_reduction_close_invalid")
	}
	return provider.allocator.CloseSagaAllocation(ctx, allocation, disposition)
}

func sagaJournalFirstOrdinal(ordinal uint64) (uint64, error) {
	const stride uint64 = 32
	if ordinal == 0 || ordinal > (^uint64(0)-1)/stride+1 {
		return 0, strategyError("saga_reduction_journal_invalid")
	}
	return (ordinal-1)*stride + 1, nil
}

// SagaReducer writes journal and reconciliation evidence before releasing an
// allocation that has completed a deterministic simulation.
type SagaReducer struct{ provider SagaReductionProvider }

// NewSagaReducer requires every stateful downstream boundary up front.
func NewSagaReducer(provider SagaReductionProvider) (*SagaReducer, error) {
	if provider == nil {
		return nil, strategyError("saga_reduction_invalid")
	}
	return &SagaReducer{provider: provider}, nil
}

// ReduceSaga accepts only mutually consistent allocated, planned, and
// simulated evidence. It appends every validated journal transaction, records
// reconciliation, and only then permits allocation release.
func (reducer *SagaReducer) ReduceSaga(
	ctx context.Context,
	allocated backtest.SagaAllocation,
	planned backtest.SagaPlan,
	executed backtest.SagaExecution,
) (backtest.EventResult, error) {
	allocation, plan, executionValue, err := decodeSagaReduction(reducer, ctx, allocated, planned, executed)
	if err != nil {
		return backtest.EventResult{}, err
	}
	journal, err := reducer.provider.Journal(allocation.Input)
	if err != nil || journal == nil {
		return backtest.EventResult{}, strategyError("saga_reduction_journal_invalid")
	}
	transactions, err := journal.Transactions(allocation.Candidate, executionValue.Simulation)
	if err != nil || len(transactions) == 0 || journal.Post(allocation.Candidate, executionValue.Simulation) != nil {
		return backtest.EventResult{}, strategyError("saga_reduction_journal_failed")
	}
	comparison, err := reducer.provider.Reconciliation(allocation.Input, allocation.Candidate, executionValue.Simulation)
	if err != nil || comparison.Reconciler == nil || comparison.Scope == "" || comparison.At.IsZero() ||
		comparison.At.Location() != time.UTC {
		return backtest.EventResult{}, strategyError("saga_reduction_reconciliation_invalid")
	}
	caseResult, err := comparison.Reconciler.Reconcile(comparison.Scope, comparison.Expected, comparison.Actual, comparison.At)
	if err != nil {
		return backtest.EventResult{}, strategyError("saga_reduction_reconciliation_failed")
	}
	if caseResult.State == "quarantined" {
		return backtest.EventResult{}, strategyError("saga_reduction_reconciliation_quarantined")
	}
	decision, decisionErr := json.Marshal(plan.Approval.Decision)
	orders, ordersErr := json.Marshal(plan.Execution)
	executionEvidence, executionErr := json.Marshal(executionValue.Simulation)
	balances, balancesErr := json.Marshal(sagaReductionEvidence{Transactions: transactions, Reconciliation: caseResult})
	if decisionErr != nil || ordersErr != nil || executionErr != nil || balancesErr != nil {
		return backtest.EventResult{}, strategyError("saga_reduction_encode_failed")
	}
	disposition := triangularReductionDisposition(executionValue.Simulation)
	if err = reducer.provider.Close(ctx, allocated, disposition); err != nil {
		return backtest.EventResult{}, strategyError("saga_reduction_close_failed")
	}
	return backtest.EventResult{Ordinal: allocated.Ordinal, Decision: decision, Orders: orders,
		ExecutionEvents: executionEvidence, Balances: balances}, nil
}

func decodeSagaReduction(reducer *SagaReducer, ctx context.Context, allocated backtest.SagaAllocation,
	planned backtest.SagaPlan, executed backtest.SagaExecution,
) (sagaAllocation, sagaPlan, sagaExecution, error) {
	var allocation sagaAllocation
	var plan sagaPlan
	var executionValue sagaExecution
	if reducer == nil || ctx == nil || allocated.Ordinal == 0 || planned.Ordinal != allocated.Ordinal ||
		executed.Ordinal != allocated.Ordinal || json.Unmarshal(allocated.Payload, &allocation) != nil ||
		json.Unmarshal(planned.Payload, &plan) != nil || json.Unmarshal(executed.Payload, &executionValue) != nil ||
		allocation.Input.ValidateEventBinding(allocated.Ordinal, allocation.Input.LogicalTime) != nil ||
		allocation.Claims.State != portfolio.ClaimActive || !reflect.DeepEqual(plan.Approval.Allocation, allocation) ||
		!reflect.DeepEqual(executionValue.Plan, plan) || executionValue.Simulation.CandidateID != allocation.Candidate.ID ||
		executionValue.Simulation.Saga.ID != plan.Execution.ID {
		return sagaAllocation{}, sagaPlan{}, sagaExecution{}, strategyError("saga_reduction_input_invalid")
	}
	return allocation, plan, executionValue, nil
}

func triangularReductionDisposition(result SimulationResult) backtest.AllocationDisposition {
	if result.Recovery.Quarantined || result.Saga.State == execution.PlanQuarantined {
		return backtest.AllocationQuarantined
	}
	return backtest.AllocationReleased
}

type sagaReductionEvidence struct {
	Transactions   []accounting.Transaction `json:"transactions"`
	Reconciliation reconciliation.Case      `json:"reconciliation"`
}

func rankedCandidates(candidates []Candidate) []Candidate {
	ordered := append([]Candidate(nil), candidates...)
	sort.SliceStable(ordered, func(left, right int) bool {
		if comparison := ordered[left].WorstCaseNet.Compare(ordered[right].WorstCaseNet); comparison != 0 {
			return comparison > 0
		}
		if comparison := ordered[left].ExpectedNet.Compare(ordered[right].ExpectedNet); comparison != 0 {
			return comparison > 0
		}
		return ordered[left].ID < ordered[right].ID
	})
	return ordered
}

func sagaReservationID(candidateID string) (domain.ReservationID, error) {
	digest := sha256.Sum256([]byte(candidateID))
	return domain.NewReservationID("triangular-saga-" + hex.EncodeToString(digest[:10]))
}

var _ backtest.SagaStrategy = (*SagaStrategyAdapter)(nil)
var _ backtest.SagaAllocator = (*AtomicSagaAllocator)(nil)
var _ backtest.SagaRiskEngine = (*SagaRiskAdapter)(nil)
var _ backtest.SagaPlanner = (*SagaPlanner)(nil)
var _ backtest.SagaBroker = (*SagaSimulationBroker)(nil)
var _ backtest.SagaReducer = (*SagaReducer)(nil)
var _ SagaReductionProvider = (*RecordedSagaReductionProvider)(nil)
