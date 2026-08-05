package triangular

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	"axiom/internal/backtest"
	"axiom/internal/domain"
	"axiom/internal/portfolio"
	"axiom/internal/replay"
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
// fencing token. It cannot create a private substitute allocator.
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
		len(candidates.Candidates) == 0 {
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
