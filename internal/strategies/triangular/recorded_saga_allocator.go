package triangular

import (
	"context"
	"encoding/json"
	"sync"

	"axiom/internal/backtest"
	runtimecore "axiom/internal/runtime"
)

// RecordedSagaAllocator reconstructs an input-scoped atomic claim set for
// each immutable offline event. Offline backtest and replay have no external
// side effect between claim and reduction: on a worker restart the event is
// replayed from its recorded input and no uncommitted virtual claim survives.
// A completed event still releases or quarantines the exact delegate that
// created its claim, so a planner or reducer cannot cross ownership boundaries.
type RecordedSagaAllocator struct {
	owner  string
	fence  runtimecore.FencingToken
	mutex  sync.Mutex
	active map[string]*AtomicSagaAllocator
}

// NewRecordedSagaAllocator constructs the recorded-input allocator used by a
// credential-free offline runtime. It has no exchange adapter capability.
func NewRecordedSagaAllocator(owner string, fence runtimecore.FencingToken) (*RecordedSagaAllocator, error) {
	if owner == "" || fence == 0 {
		return nil, strategyError("recorded_saga_allocator_invalid")
	}
	return &RecordedSagaAllocator{owner: owner, fence: fence, active: make(map[string]*AtomicSagaAllocator)}, nil
}

// AllocateSaga derives capacity from the same immutable input that produced
// the candidate payload, then delegates ranking and all-or-nothing claiming to
// the normal AtomicSagaAllocator.
func (allocator *RecordedSagaAllocator) AllocateSaga(ctx context.Context, candidate backtest.SagaCandidate) (backtest.SagaAllocation, error) {
	var value sagaCandidateSet
	if allocator == nil || ctx == nil || candidate.Ordinal == 0 || json.Unmarshal(candidate.Payload, &value) != nil ||
		value.Input.Ordinal != candidate.Ordinal || value.Input.ValidateEventBinding(candidate.Ordinal, value.Input.LogicalTime) != nil {
		return backtest.SagaAllocation{}, strategyError("recorded_saga_allocator_invalid")
	}
	claims, err := NewRecordedSagaClaimSet(value.Input, allocator.owner)
	if err != nil {
		return backtest.SagaAllocation{}, err
	}
	delegate, err := NewAtomicSagaAllocator(claims, allocator.owner, allocator.fence)
	if err != nil {
		return backtest.SagaAllocation{}, err
	}
	allocated, err := delegate.AllocateSaga(ctx, candidate)
	if err != nil {
		return backtest.SagaAllocation{}, err
	}
	var allocation sagaAllocation
	if json.Unmarshal(allocated.Payload, &allocation) != nil || allocation.Claims.ID.Value() == "" {
		_ = delegate.CloseSagaAllocation(ctx, allocated, backtest.AllocationReleased)
		return backtest.SagaAllocation{}, strategyError("recorded_saga_allocator_invalid")
	}
	allocator.mutex.Lock()
	defer allocator.mutex.Unlock()
	key := allocation.Claims.ID.Value()
	if _, exists := allocator.active[key]; exists {
		_ = delegate.CloseSagaAllocation(ctx, allocated, backtest.AllocationReleased)
		return backtest.SagaAllocation{}, strategyError("recorded_saga_allocator_duplicate")
	}
	allocator.active[key] = delegate
	return allocated, nil
}

// CloseSagaAllocation delegates cleanup to the exact input-scoped allocator
// that created the claim and forgets it only after its fenced state transition
// succeeds.
func (allocator *RecordedSagaAllocator) CloseSagaAllocation(ctx context.Context, allocation backtest.SagaAllocation, disposition backtest.AllocationDisposition) error {
	var value sagaAllocation
	if allocator == nil || ctx == nil || allocation.Ordinal == 0 || json.Unmarshal(allocation.Payload, &value) != nil ||
		value.Claims.ID.Value() == "" {
		return strategyError("recorded_saga_allocator_invalid")
	}
	allocator.mutex.Lock()
	delegate := allocator.active[value.Claims.ID.Value()]
	allocator.mutex.Unlock()
	if delegate == nil || delegate.CloseSagaAllocation(ctx, allocation, disposition) != nil {
		return strategyError("recorded_saga_allocator_close_failed")
	}
	allocator.mutex.Lock()
	delete(allocator.active, value.Claims.ID.Value())
	allocator.mutex.Unlock()
	return nil
}

var _ backtest.SagaAllocator = (*RecordedSagaAllocator)(nil)
