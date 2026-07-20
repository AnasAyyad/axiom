package trend

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"axiom/internal/backtest"
	"axiom/internal/domain"
	"axiom/internal/execution"
	"axiom/internal/portfolio"
	"axiom/internal/replay"

	"github.com/cockroachdb/apd/v3"
)

// OperationalProcessor records every deterministic Trend decision while
// sending accepted candidates through the shared operational pipeline.
type OperationalProcessor struct {
	evaluator *Evaluator
	pipeline  *backtest.PipelineProcessor
	portfolio *portfolio.Portfolio
	mutex     sync.Mutex
	trades    uint64
	marks     map[domain.AssetSymbol]domain.Price
	starting  decimal
	latest    decimal
	highWater decimal
	maxDraw   decimal
	turnover  decimal
	fees      decimal
	observed  uint64
	exposed   uint64
	metricErr bool
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
	snapshot := owned.Snapshot()
	quote, ok := snapshot.Balances[domain.AssetSymbol(portfolio.V1ANumeraire)]
	if !ok {
		return nil, trendError(ReasonInvalidConfiguration)
	}
	available, err := parseDecimal(quote.Available.String())
	if err != nil {
		return nil, err
	}
	reserved, err := parseDecimal(quote.Reserved.String())
	if err != nil {
		return nil, err
	}
	starting, err := available.add(reserved)
	if err != nil || starting.value.Sign() <= 0 {
		return nil, trendError(ReasonInvalidConfiguration)
	}
	zero, _ := parseDecimal("0")
	return &OperationalProcessor{evaluator: evaluator, pipeline: pipeline, portfolio: owned,
		marks: make(map[domain.AssetSymbol]domain.Price), starting: starting, latest: starting,
		highWater: starting, maxDraw: zero, turnover: zero, fees: zero}, nil
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
		result := backtest.EventResult{Ordinal: event.Ordinal, Decision: decisionPayload,
			Orders: json.RawMessage("[]"), ExecutionEvents: json.RawMessage("[]"), Balances: balances}
		processor.observe(input, result.Orders)
		return result, nil
	}
	result, err := processor.pipeline.Process(ctx, event)
	if err != nil {
		return backtest.EventResult{}, err
	}
	result.Decision = decisionPayload
	processor.observe(input, result.Orders)
	return result, nil
}

func (processor *OperationalProcessor) observe(input Input, payload json.RawMessage) {
	processor.mutex.Lock()
	defer processor.mutex.Unlock()
	if !processor.observePortfolio(input) {
		return
	}
	processor.observeOrders(payload)
}

// observePortfolio updates exact portfolio-derived metrics while observe owns
// the processor mutex.
func (processor *OperationalProcessor) observePortfolio(input Input) bool {
	processor.marks[input.Instrument.Base] = input.Candles[len(input.Candles)-1].Close
	exposure, err := processor.portfolio.Exposure(processor.marks)
	if err != nil {
		processor.metricErr = true
		return false
	}
	equity, err := parseDecimal(exposure.Equity.String())
	if err != nil {
		processor.metricErr = true
		return false
	}
	processor.latest = equity
	if equity.compare(processor.highWater) > 0 {
		processor.highWater = equity
	}
	drawAmount, err := processor.highWater.subtract(equity)
	if err != nil {
		processor.metricErr = true
		return false
	}
	draw, err := drawAmount.divide(processor.highWater, apd.RoundHalfEven)
	if err != nil {
		processor.metricErr = true
		return false
	}
	if draw.compare(processor.maxDraw) > 0 {
		processor.maxDraw = draw
	}
	processor.observed++
	zeroBalance, _ := domain.ParseBalance("0")
	for _, quantity := range exposure.Inventory {
		if quantity.Compare(zeroBalance) > 0 {
			processor.exposed++
			break
		}
	}
	return true
}

// observeOrders updates exact fill-derived metrics while observe owns the
// processor mutex.
func (processor *OperationalProcessor) observeOrders(payload json.RawMessage) {
	var orders []execution.Order
	if json.Unmarshal(payload, &orders) != nil {
		processor.metricErr = true
		return
	}
	for _, order := range orders {
		if len(order.Fills) > 0 {
			processor.trades++
		}
		for _, fill := range order.Fills {
			price, priceErr := parseDecimal(fill.Price.String())
			quantity, quantityErr := parseDecimal(fill.Quantity.String())
			fee, feeErr := parseDecimal(fill.Fee.String())
			notional, multiplyErr := price.multiply(quantity, apd.RoundHalfEven)
			if priceErr != nil || quantityErr != nil || feeErr != nil || multiplyErr != nil {
				processor.metricErr = true
				return
			}
			var err error
			processor.turnover, err = processor.turnover.add(notional)
			if err == nil {
				processor.fees, err = processor.fees.add(fee)
			}
			if err != nil {
				processor.metricErr = true
				return
			}
		}
	}
}

// Metrics reports exact facts derived from the virtual portfolio and recorded
// marks. Statistics that require a registered multi-run research suite remain
// explicitly unavailable.
func (processor *OperationalProcessor) Metrics() backtest.Metrics {
	processor.mutex.Lock()
	defer processor.mutex.Unlock()
	unavailable := "unavailable"
	metrics := backtest.Metrics{TotalNetReturn: unavailable, AnnualizedReturn: unavailable,
		MaximumDrawdown: unavailable, CurrentDrawdown: unavailable, SharpeRatio: unavailable,
		SortinoRatio: unavailable, CalmarRatio: unavailable, ProfitFactor: unavailable,
		Expectancy: unavailable, WinRate: unavailable, AverageWin: unavailable, AverageLoss: unavailable,
		LargestWin: unavailable, LargestLoss: unavailable, Turnover: unavailable, Exposure: unavailable,
		Trades: processor.trades, FeesPaid: unavailable, FeePercentGrossProfit: unavailable, SlippagePercentGrossProfit: unavailable,
		RecoveryLoss: unavailable, TimeInMarket: unavailable, ByAsset: map[string]string{},
		ByExchange: map[string]string{}, ByStrategy: map[string]string{}, ByRegime: map[string]string{}}
	if processor.metricErr || processor.observed == 0 {
		return metrics
	}
	returnAmount, err := processor.latest.subtract(processor.starting)
	if processor.latest.compare(processor.starting) < 0 {
		returnAmount, err = processor.starting.subtract(processor.latest)
		returnAmount.value.Neg(&returnAmount.value)
	}
	totalReturn, returnErr := returnAmount.divide(processor.starting, apd.RoundHalfEven)
	currentAmount, drawErr := processor.highWater.subtract(processor.latest)
	currentDraw, currentErr := currentAmount.divide(processor.highWater, apd.RoundHalfEven)
	turnover, turnoverErr := processor.turnover.divide(processor.starting, apd.RoundHalfEven)
	observed, observedErr := parseDecimal(fmt.Sprint(processor.observed))
	exposed, exposedErr := parseDecimal(fmt.Sprint(processor.exposed))
	exposure, exposureErr := exposed.divide(observed, apd.RoundHalfEven)
	if err != nil || returnErr != nil || drawErr != nil || currentErr != nil || turnoverErr != nil ||
		observedErr != nil || exposedErr != nil || exposureErr != nil {
		return metrics
	}
	metrics.TotalNetReturn = totalReturn.stringValue()
	metrics.MaximumDrawdown = processor.maxDraw.stringValue()
	metrics.CurrentDrawdown = currentDraw.stringValue()
	metrics.Turnover = turnover.stringValue()
	metrics.Exposure = exposure.stringValue()
	metrics.TimeInMarket = exposure.stringValue()
	metrics.FeesPaid = processor.fees.stringValue()
	metrics.ByExchange[portfolio.V1AExchange] = metrics.TotalNetReturn
	metrics.ByStrategy[portfolio.V1AStrategy] = metrics.TotalNetReturn
	return metrics
}

var _ backtest.Processor = (*OperationalProcessor)(nil)
