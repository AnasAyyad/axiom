package meanreversion

import (
	"context"
	"encoding/json"

	"axiom/internal/backtest"
	"axiom/internal/replay"
)

// BalanceSnapshot supplies canonical read-only portfolio evidence for a
// rejected/no-change decision. Accepted decisions use reducer-owned balances.
type BalanceSnapshot func() (json.RawMessage, error)

// OperationalProcessor records every deterministic decision while accepted
// candidates alone enter the shared allocator/risk/planner/simulation path.
type OperationalProcessor struct {
	evaluator *Evaluator
	pipeline  *backtest.PipelineProcessor
	balances  BalanceSnapshot
}

// NewOperationalProcessor composes pure decision evidence with the shared path.
func NewOperationalProcessor(evaluator *Evaluator, pipeline *backtest.PipelineProcessor,
	balances BalanceSnapshot) (*OperationalProcessor, error) {
	if evaluator == nil || pipeline == nil || balances == nil {
		return nil, strategyError(ReasonInvalidConfiguration)
	}
	return &OperationalProcessor{evaluator: evaluator, pipeline: pipeline, balances: balances}, nil
}

// Process preserves ordinary rejections as canonical results.
func (processor *OperationalProcessor) Process(ctx context.Context, event replay.Event) (backtest.EventResult, error) {
	var input Input
	if json.Unmarshal(event.Canonical, &input) != nil || input.Ordinal != event.Ordinal ||
		input.LogicalTime != event.LogicalTime {
		return backtest.EventResult{}, strategyError(ReasonCandleOrder)
	}
	decision, err := processor.evaluator.Evaluate(input)
	if err != nil {
		return backtest.EventResult{}, err
	}
	decisionPayload, err := json.Marshal(decision)
	if err != nil {
		return backtest.EventResult{}, strategyError(ReasonInvalidConfiguration)
	}
	if decision.Candidate == nil {
		balances, balanceErr := processor.balances()
		if balanceErr != nil {
			return backtest.EventResult{}, balanceErr
		}
		return backtest.EventResult{Ordinal: event.Ordinal, Decision: decisionPayload,
			Orders: json.RawMessage("[]"), ExecutionEvents: json.RawMessage("[]"), Balances: balances}, nil
	}
	result, err := processor.pipeline.Process(ctx, event)
	if err != nil {
		return backtest.EventResult{}, err
	}
	result.Decision = decisionPayload
	return result, nil
}

// Metrics returns reducer-owned shared-pipeline counters.
func (processor *OperationalProcessor) Metrics() backtest.Metrics {
	return processor.pipeline.Metrics()
}

var _ backtest.Processor = (*OperationalProcessor)(nil)
