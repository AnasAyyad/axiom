package trend

import (
	"context"
	"encoding/json"
	"sync"

	"axiom/internal/backtest"
	"axiom/internal/execution"
	"axiom/internal/portfolio"
	"axiom/internal/replay"
)

// OperationalProcessor records every deterministic Trend decision while
// sending accepted candidates through the shared operational pipeline.
type OperationalProcessor struct {
	evaluator *Evaluator
	pipeline  *backtest.PipelineProcessor
	portfolio *portfolio.Portfolio
	mutex     sync.Mutex
	trades    uint64
}

// NewOperationalProcessor composes decision evidence with the real A8-A10 path.
func NewOperationalProcessor(
	evaluator *Evaluator,
	pipeline *backtest.PipelineProcessor,
	owned *portfolio.Portfolio,
) (*OperationalProcessor, error) {
	if evaluator == nil || pipeline == nil || owned == nil {
		return nil, trendError(ReasonInvalidConfiguration)
	}
	return &OperationalProcessor{evaluator: evaluator, pipeline: pipeline, portfolio: owned}, nil
}

// Process preserves rejections as canonical evidence instead of treating an
// ordinary no-signal candle as a failed run.
func (processor *OperationalProcessor) Process(ctx context.Context, event replay.Event) (backtest.EventResult, error) {
	var input Input
	if json.Unmarshal(event.Canonical, &input) != nil || input.Ordinal != event.Ordinal ||
		input.LogicalTime != event.LogicalTime {
		return backtest.EventResult{}, trendError(ReasonCandleOrder)
	}
	decision, err := processor.evaluator.Evaluate(input)
	if err != nil {
		return backtest.EventResult{}, err
	}
	decisionPayload, err := json.Marshal(decision)
	if err != nil {
		return backtest.EventResult{}, trendError(ReasonInvalidConfiguration)
	}
	if decision.Candidate == nil {
		balances, _ := json.Marshal(processor.portfolio.Snapshot())
		return backtest.EventResult{Ordinal: event.Ordinal, Decision: decisionPayload,
			Orders: json.RawMessage("[]"), ExecutionEvents: json.RawMessage("[]"), Balances: balances}, nil
	}
	result, err := processor.pipeline.Process(ctx, event)
	if err != nil {
		return backtest.EventResult{}, err
	}
	result.Decision = decisionPayload
	processor.observeTrades(result.Orders)
	return result, nil
}

func (processor *OperationalProcessor) observeTrades(payload json.RawMessage) {
	var orders []execution.Order
	if json.Unmarshal(payload, &orders) != nil {
		return
	}
	processor.mutex.Lock()
	defer processor.mutex.Unlock()
	for _, order := range orders {
		if len(order.Fills) > 0 {
			processor.trades++
		}
	}
}

// Metrics reports only facts this minimal processor can establish. Financial
// performance remains unavailable until the registered A10 report reducer runs.
func (processor *OperationalProcessor) Metrics() backtest.Metrics {
	processor.mutex.Lock()
	defer processor.mutex.Unlock()
	unavailable := "unavailable"
	return backtest.Metrics{TotalNetReturn: unavailable, AnnualizedReturn: unavailable,
		MaximumDrawdown: unavailable, CurrentDrawdown: unavailable, SharpeRatio: unavailable,
		SortinoRatio: unavailable, CalmarRatio: unavailable, ProfitFactor: unavailable,
		Expectancy: unavailable, WinRate: unavailable, AverageWin: unavailable, AverageLoss: unavailable,
		LargestWin: unavailable, LargestLoss: unavailable, Turnover: unavailable, Exposure: unavailable,
		Trades: processor.trades, FeePercentGrossProfit: unavailable, SlippagePercentGrossProfit: unavailable,
		RecoveryLoss: unavailable, TimeInMarket: unavailable, ByAsset: map[string]string{},
		ByExchange: map[string]string{}, ByStrategy: map[string]string{}, ByRegime: map[string]string{}}
}

var _ backtest.Processor = (*OperationalProcessor)(nil)
