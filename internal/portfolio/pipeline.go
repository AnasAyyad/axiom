package portfolio

import (
	"context"
	"encoding/json"

	"axiom/internal/accounting"
	"axiom/internal/backtest"
)

// PipelineAllocator adapts central exclusive allocation to the shared A8 pipeline.
type PipelineAllocator struct{ allocator *Allocator }

// NewPipelineAllocator constructs the real A9 allocation adapter.
func NewPipelineAllocator(allocator *Allocator) (*PipelineAllocator, error) {
	if allocator == nil {
		return nil, portfolioError("pipeline_allocator_invalid")
	}
	return &PipelineAllocator{allocator: allocator}, nil
}

// Allocate decodes one candidate and returns its exclusive allocation only.
func (adapter *PipelineAllocator) Allocate(
	_ context.Context,
	candidate backtest.Candidate,
) (backtest.AllocatedIntent, error) {
	var requested Candidate
	if candidate.Ordinal == 0 || json.Unmarshal(candidate.Payload, &requested) != nil {
		return backtest.AllocatedIntent{}, portfolioError("pipeline_candidate_invalid")
	}
	allocations, err := adapter.allocator.Allocate([]Candidate{requested})
	if err != nil || len(allocations) != 1 {
		return backtest.AllocatedIntent{}, portfolioError("pipeline_allocation_rejected")
	}
	payload, err := json.Marshal(allocations[0])
	if err != nil {
		return backtest.AllocatedIntent{}, portfolioError("pipeline_allocation_encode_failed")
	}
	return backtest.AllocatedIntent{Ordinal: candidate.Ordinal, Payload: payload}, nil
}

// CloseAllocation releases known-safe failures and quarantines uncertain downstream failures.
func (adapter *PipelineAllocator) CloseAllocation(
	_ context.Context,
	allocated backtest.AllocatedIntent,
	disposition backtest.AllocationDisposition,
) error {
	var allocation Allocation
	if allocated.Ordinal == 0 || json.Unmarshal(allocated.Payload, &allocation) != nil {
		return portfolioError("pipeline_allocation_close_invalid")
	}
	state := accounting.ReservationReleased
	if disposition == backtest.AllocationQuarantined {
		state = accounting.ReservationQuarantined
	} else if disposition != backtest.AllocationReleased {
		return portfolioError("pipeline_allocation_close_invalid")
	}
	return adapter.allocator.Close(allocation, state)
}

var _ backtest.Allocator = (*PipelineAllocator)(nil)
var _ backtest.AllocationCloser = (*PipelineAllocator)(nil)
