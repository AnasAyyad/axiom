package triangular

import (
	"context"
	"encoding/json"
	"reflect"

	"axiom/internal/backtest"
	"axiom/internal/execution"
	"axiom/internal/portfolio"
	"axiom/internal/replay"
	"axiom/internal/risk"
	runtimecore "axiom/internal/runtime"
)

// SagaStrategyAdapter evaluates every exact triangular candidate from one
// canonical Input. It performs no allocation or risk decision itself.
type SagaStrategyAdapter struct{}

// NewSagaStrategyAdapter constructs the mode-independent evaluator adapter.
func NewSagaStrategyAdapter() *SagaStrategyAdapter { return &SagaStrategyAdapter{} }

// EvaluateSaga decodes only immutable replay input and returns the full,
// exhaustively evaluated candidate set to the central allocation stage.
func (*SagaStrategyAdapter) EvaluateSaga(ctx context.Context, event replay.Event) (backtest.SagaCandidate, error) {
	if ctx == nil || event.Ordinal == 0 || event.LogicalTime == 0 || len(event.Canonical) == 0 {
		return backtest.SagaCandidate{}, strategyError("saga_input_invalid")
	}
	var input Input
	if json.Unmarshal(event.Canonical, &input) != nil ||
		input.ValidateEventBinding(event.Ordinal, event.LogicalTime) != nil {
		return backtest.SagaCandidate{}, strategyError("saga_input_invalid")
	}
	evaluation, err := input.EvaluationInput()
	if err != nil {
		return backtest.SagaCandidate{}, err
	}
	candidates, err := Evaluate(evaluation)
	if err != nil {
		return backtest.SagaCandidate{}, err
	}
	payload, err := json.Marshal(sagaCandidateSet{Input: input, Candidates: candidates})
	if err != nil {
		return backtest.SagaCandidate{}, strategyError("saga_candidate_encode_failed")
	}
	return backtest.SagaCandidate{Ordinal: event.Ordinal, Payload: payload}, nil
}

// AtomicSagaAllocator selects one deterministic highest-ranked candidate and
// reserves every balance, fee, liquidity, and recovery resource atomically.
// The claim set is supplied by the runtime owner so resource capacity remains
// shared rather than being synthesized from a candidate.
type AtomicSagaAllocator struct {
	claims *portfolio.AtomicClaimSet
	owner  string
	fence  runtimecore.FencingToken
}

// NewAtomicSagaAllocator requires the actual shared claim set and current
// fencing token. It cannot create an isolated substitute allocator.
func NewAtomicSagaAllocator(
	claims *portfolio.AtomicClaimSet,
	owner string,
	fence runtimecore.FencingToken,
) (*AtomicSagaAllocator, error) {
	if claims == nil || owner == "" || fence == 0 {
		return nil, strategyError("saga_allocator_invalid")
	}
	return &AtomicSagaAllocator{claims: claims, owner: owner, fence: fence}, nil
}

// AllocateSaga ranks all accepted candidates by worst-case return, then
// expected return and immutable ID. It reserves the first complete candidate;
// a resource conflict may fall through to the next independently valid one.
func (allocator *AtomicSagaAllocator) AllocateSaga(
	_ context.Context,
	input backtest.SagaCandidate,
) (backtest.SagaAllocation, error) {
	var candidates sagaCandidateSet
	if allocator == nil || input.Ordinal == 0 || json.Unmarshal(input.Payload, &candidates) != nil ||
		candidates.Input.Ordinal != input.Ordinal || candidates.Input.LogicalTime == 0 ||
		len(candidates.Candidates) == 0 || !sagaCandidatesMatchInput(candidates.Input, candidates.Candidates) {
		return backtest.SagaAllocation{}, strategyError("saga_candidate_invalid")
	}
	for _, candidate := range rankedCandidates(candidates.Candidates) {
		reservation, err := sagaReservationID(candidate.ID)
		if err != nil {
			return backtest.SagaAllocation{}, strategyError("saga_reservation_invalid")
		}
		group, claimErr := ClaimCandidate(allocator.claims, candidate, allocator.owner,
			reservation, allocator.fence, candidates.Input.LogicalTime)
		if claimErr != nil {
			continue
		}
		payload, marshalErr := json.Marshal(sagaAllocation{Input: candidates.Input,
			Candidate: candidate, Claims: group})
		if marshalErr != nil {
			_ = allocator.claims.Close(group.ID, group.Revision, allocator.fence, portfolio.ClaimReleased)
			return backtest.SagaAllocation{}, strategyError("saga_allocation_encode_failed")
		}
		return backtest.SagaAllocation{Ordinal: input.Ordinal, Payload: payload}, nil
	}
	return backtest.SagaAllocation{}, strategyError("saga_allocation_unavailable")
}

// sagaCandidatesMatchInput prevents an intermediate payload from changing
// economics, resource claims, or ordering after the immutable decision input
// was evaluated. Allocation always consumes the complete exact evaluator set.
func sagaCandidatesMatchInput(input Input, candidates []Candidate) bool {
	evaluation, err := input.EvaluationInput()
	if err != nil {
		return false
	}
	expected, err := Evaluate(evaluation)
	return err == nil && reflect.DeepEqual(candidates, expected)
}

// CloseSagaAllocation releases a known-safe failure or quarantines uncertain
// state with the original atomic-claim fence and revision.
func (allocator *AtomicSagaAllocator) CloseSagaAllocation(
	_ context.Context,
	allocation backtest.SagaAllocation,
	disposition backtest.AllocationDisposition,
) error {
	var value sagaAllocation
	if allocator == nil || allocation.Ordinal == 0 || json.Unmarshal(allocation.Payload, &value) != nil ||
		value.Claims.ID.Value() == "" || value.Claims.Fence != allocator.fence {
		return strategyError("saga_allocation_close_invalid")
	}
	next := portfolio.ClaimReleased
	if disposition == backtest.AllocationQuarantined {
		next = portfolio.ClaimQuarantined
	} else if disposition != backtest.AllocationReleased {
		return strategyError("saga_allocation_close_invalid")
	}
	if err := allocator.claims.Close(value.Claims.ID, value.Claims.Revision, allocator.fence, next); err != nil {
		return strategyError("saga_allocation_close_invalid")
	}
	return nil
}

type sagaCandidateSet struct {
	Input      Input       `json:"input"`
	Candidates []Candidate `json:"candidates"`
}

type sagaAllocation struct {
	Input     Input                `json:"input"`
	Candidate Candidate            `json:"candidate"`
	Claims    portfolio.ClaimGroup `json:"claims"`
}

// SagaRiskInputProvider supplies the immutable central-risk observations that
// belong to the exact canonical decision input. Implementations must not read
// a newer live book or account snapshot while processing historical replay.
type SagaRiskInputProvider interface {
	RiskInput(Input) (RiskInput, error)
}

// SagaRiskAdapter invokes the existing central risk engine for the exact
// candidate that owns the atomic claim group.
type SagaRiskAdapter struct {
	engine RiskEvaluator
	inputs SagaRiskInputProvider
}

// NewSagaRiskAdapter requires both the central engine and an immutable input
// provider; neither a nil provider nor strategy-local risk rules are allowed.
func NewSagaRiskAdapter(engine RiskEvaluator, inputs SagaRiskInputProvider) (*SagaRiskAdapter, error) {
	if engine == nil || inputs == nil {
		return nil, strategyError("saga_risk_invalid")
	}
	return &SagaRiskAdapter{engine: engine, inputs: inputs}, nil
}

// ApproveSaga passes only the allocated candidate into central risk. An
// expired or malformed replay input fails before the risk engine can approve
// it, preserving the strategy's existing expiry rule.
func (adapter *SagaRiskAdapter) ApproveSaga(
	_ context.Context,
	allocated backtest.SagaAllocation,
) (backtest.SagaApproval, error) {
	var value sagaAllocation
	if adapter == nil || allocated.Ordinal == 0 || json.Unmarshal(allocated.Payload, &value) != nil ||
		value.Input.Ordinal != allocated.Ordinal ||
		value.Input.ValidateEventBinding(value.Input.Ordinal, value.Input.LogicalTime) != nil ||
		value.Candidate.ID == "" || value.Claims.State != portfolio.ClaimActive {
		return backtest.SagaApproval{}, strategyError("saga_risk_input_invalid")
	}
	riskInput, err := adapter.inputs.RiskInput(value.Input)
	if err != nil {
		return backtest.SagaApproval{}, strategyError("saga_risk_input_invalid")
	}
	decision, err := ApproveCandidate(adapter.engine, value.Candidate, riskInput, value.Input.LogicalTime)
	if err != nil {
		return backtest.SagaApproval{}, err
	}
	payload, err := json.Marshal(sagaApproval{Allocation: value, Decision: decision})
	if err != nil {
		return backtest.SagaApproval{}, strategyError("saga_risk_encode_failed")
	}
	return backtest.SagaApproval{Ordinal: allocated.Ordinal, Payload: payload}, nil
}

type sagaApproval struct {
	Allocation sagaAllocation `json:"allocation"`
	Decision   risk.Decision  `json:"decision"`
}

// SagaPlanner materializes the exact sequential virtual execution saga only
// after central risk has approved its atomic allocation.
type SagaPlanner struct{}

// NewSagaPlanner constructs the strategy-owned plan boundary.
func NewSagaPlanner() *SagaPlanner { return &SagaPlanner{} }

// PlanSaga preserves the approved allocation and a pre-dispatch execution
// snapshot. The plan contains no exchange adapter or credential capability.
func (*SagaPlanner) PlanSaga(_ context.Context, approved backtest.SagaApproval) (backtest.SagaPlan, error) {
	var value sagaApproval
	if approved.Ordinal == 0 || json.Unmarshal(approved.Payload, &value) != nil ||
		value.Allocation.Input.Ordinal != approved.Ordinal ||
		value.Allocation.Input.ValidateEventBinding(approved.Ordinal, value.Allocation.Input.LogicalTime) != nil ||
		value.Allocation.Candidate.ID == "" || value.Allocation.Claims.State != portfolio.ClaimActive ||
		value.Decision.Action != risk.ActionApprove {
		return backtest.SagaPlan{}, strategyError("saga_plan_input_invalid")
	}
	saga, _, err := newSequentialSaga(value.Allocation.Candidate)
	if err != nil {
		return backtest.SagaPlan{}, strategyError("saga_plan_invalid")
	}
	payload, err := json.Marshal(sagaPlan{Approval: value, Execution: saga.Snapshot()})
	if err != nil {
		return backtest.SagaPlan{}, strategyError("saga_plan_encode_failed")
	}
	return backtest.SagaPlan{Ordinal: approved.Ordinal, Payload: payload}, nil
}

type sagaPlan struct {
	Approval  sagaApproval   `json:"approval"`
	Execution execution.Saga `json:"execution"`
}

// SagaSimulationInputProvider supplies recorded future books and the reviewed
// latency model associated with one canonical input. It must not substitute a
// newer live market view while processing a replay or demonstration.
type SagaSimulationInputProvider interface {
	SimulationInput(Input) (Timeline, LatencyModel, error)
}

// SagaSimulationBroker submits an approved virtual plan only to the existing
// deterministic simulator. It has no exchange adapter or credential field.
type SagaSimulationBroker struct{ inputs SagaSimulationInputProvider }

// NewSagaSimulationBroker requires a source of immutable simulation inputs.
func NewSagaSimulationBroker(inputs SagaSimulationInputProvider) (*SagaSimulationBroker, error) {
	if inputs == nil {
		return nil, strategyError("saga_simulation_invalid")
	}
	return &SagaSimulationBroker{inputs: inputs}, nil
}

// SubmitSaga verifies the exact approved planned saga before simulating it.
func (broker *SagaSimulationBroker) SubmitSaga(
	_ context.Context,
	planned backtest.SagaPlan,
) (backtest.SagaExecution, error) {
	var value sagaPlan
	if broker == nil || planned.Ordinal == 0 || json.Unmarshal(planned.Payload, &value) != nil ||
		value.Approval.Allocation.Input.Ordinal != planned.Ordinal ||
		value.Approval.Allocation.Input.ValidateEventBinding(planned.Ordinal,
			value.Approval.Allocation.Input.LogicalTime) != nil ||
		value.Approval.Decision.Action != risk.ActionApprove ||
		value.Approval.Allocation.Claims.State != portfolio.ClaimActive {
		return backtest.SagaExecution{}, strategyError("saga_simulation_input_invalid")
	}
	expected, _, err := newSequentialSaga(value.Approval.Allocation.Candidate)
	if err != nil || !reflect.DeepEqual(value.Execution, expected.Snapshot()) {
		return backtest.SagaExecution{}, strategyError("saga_simulation_plan_invalid")
	}
	timeline, latency, err := broker.inputs.SimulationInput(value.Approval.Allocation.Input)
	if err != nil {
		return backtest.SagaExecution{}, strategyError("saga_simulation_input_invalid")
	}
	result, err := Simulate(value.Approval.Allocation.Candidate, timeline, latency)
	if err != nil || result.CandidateID != value.Approval.Allocation.Candidate.ID ||
		result.Saga.ID != value.Execution.ID {
		return backtest.SagaExecution{}, strategyError("saga_simulation_failed")
	}
	payload, err := json.Marshal(sagaExecution{Plan: value, Simulation: result})
	if err != nil {
		return backtest.SagaExecution{}, strategyError("saga_simulation_encode_failed")
	}
	return backtest.SagaExecution{Ordinal: planned.Ordinal, Payload: payload}, nil
}

type sagaExecution struct {
	Plan       sagaPlan         `json:"plan"`
	Simulation SimulationResult `json:"simulation"`
}
