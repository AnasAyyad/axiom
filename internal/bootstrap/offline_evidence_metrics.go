package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"

	"axiom/internal/backtest"
	"axiom/internal/domain"
	"axiom/internal/execution"
	"axiom/internal/portfolio"
	"axiom/internal/replay"
	"axiom/internal/simulation"
)

// offlineEvidenceMetricsProcessor derives generic evidence exclusively from
// the actual virtual portfolio projection and deduplicated simulated fills.
// It uses rational arithmetic and never substitutes binary floating point.
type offlineEvidenceMetricsProcessor struct {
	delegate                                                          backtest.Processor
	starting                                                          *big.Rat
	peak                                                              *big.Rat
	lastEquity                                                        *big.Rat
	lastRealized                                                      *big.Rat
	maximumDrawdown                                                   *big.Rat
	grossProfit                                                       *big.Rat
	grossLoss                                                         *big.Rat
	largestWin                                                        *big.Rat
	largestLoss                                                       *big.Rat
	fees                                                              *big.Rat
	feesWinners                                                       *big.Rat
	feesLosers                                                        *big.Rat
	spreadCost                                                        *big.Rat
	slippageCost                                                      *big.Rat
	latencyDeterioration                                              *big.Rat
	recoveryLoss                                                      *big.Rat
	turnover                                                          *big.Rat
	maximumExposure                                                   *big.Rat
	returnSum                                                         *big.Rat
	positiveReturns                                                   *big.Rat
	negativeReturns                                                   *big.Rat
	seenFills                                                         map[string]struct{}
	seenOrderStates                                                   map[string]execution.OrderState
	byRegime                                                          map[string]*big.Rat
	trades, wins, losses, observations                                uint64
	opportunities, accepted, simulatedOrders                          uint64
	fullFills, partialFills, missedFills, canceled, expired, rejected uint64
	lastOutcome                                                       int
	claim                                                             backtest.JobClaim
}

type offlineEventCostContext struct {
	signalReference domain.Price
	executionPrice  domain.Price
	priceModel      simulation.PriceModel
}

type offlineMetricRatios struct {
	net, netReturn, currentDrawdown, profitFactor, expectancy, winRate *big.Rat
	averageWin, averageLoss, feePercent, slippagePercent, exposure     *big.Rat
}

func newOfflineEvidenceMetricsProcessor(delegate backtest.Processor,
	claim backtest.JobClaim) (backtest.Processor, error) {
	if delegate == nil {
		return nil, fmt.Errorf("offline_metrics_delegate_missing")
	}
	starting, ok := new(big.Rat).SetString(claim.Configuration.Portfolio.StartingCapital.Value)
	if !ok || starting.Sign() <= 0 {
		return nil, fmt.Errorf("offline_metrics_starting_capital_invalid")
	}
	zero := func() *big.Rat { return new(big.Rat) }
	return &offlineEvidenceMetricsProcessor{delegate: delegate, starting: starting, peak: new(big.Rat).Set(starting),
		lastEquity: new(big.Rat).Set(starting), lastRealized: zero(), maximumDrawdown: zero(),
		grossProfit: zero(), grossLoss: zero(), largestWin: zero(), largestLoss: zero(), fees: zero(),
		feesWinners: zero(), feesLosers: zero(), spreadCost: zero(), slippageCost: zero(),
		latencyDeterioration: zero(), recoveryLoss: zero(), claim: claim,
		turnover: zero(), maximumExposure: zero(), returnSum: zero(), positiveReturns: zero(),
		negativeReturns: zero(), seenFills: make(map[string]struct{}),
		seenOrderStates: make(map[string]execution.OrderState), byRegime: make(map[string]*big.Rat)}, nil
}

// Process delegates one event and derives financial and funnel evidence from
// the returned virtual-ledger projection.
func (processor *offlineEvidenceMetricsProcessor) Process(ctx context.Context,
	event replay.Event) (backtest.EventResult, error) {
	costContext, err := processor.eventCostContext(event)
	if err != nil {
		return backtest.EventResult{}, err
	}
	result, err := processor.delegate.Process(ctx, event)
	if err != nil {
		return result, err
	}
	var snapshot portfolio.Snapshot
	var orders []execution.Order
	if json.Unmarshal(result.Balances, &snapshot) != nil || json.Unmarshal(result.Orders, &orders) != nil {
		return backtest.EventResult{}, fmt.Errorf("offline_metrics_evidence_invalid")
	}
	processor.opportunities++
	if len(orders) > 0 {
		processor.accepted++
		processor.simulatedOrders += uint64(len(orders))
	}
	priorEquity := new(big.Rat).Set(processor.lastEquity)
	if err = processor.observeSnapshot(snapshot); err != nil {
		return backtest.EventResult{}, err
	}
	regime := offlineDecisionRegime(result.Decision)
	if processor.byRegime[regime] == nil {
		processor.byRegime[regime] = new(big.Rat)
	}
	processor.byRegime[regime].Add(processor.byRegime[regime],
		new(big.Rat).Sub(processor.lastEquity, priorEquity))
	if err = processor.observeOrders(orders, costContext); err != nil {
		return backtest.EventResult{}, err
	}
	processor.observations++
	return result, nil
}

func (processor *offlineEvidenceMetricsProcessor) observeSnapshot(snapshot portfolio.Snapshot) error {
	processor.lastOutcome = 0
	equity, realized, exposure, err := offlineSnapshotValues(snapshot)
	if err != nil {
		return err
	}
	processor.observeEquity(equity, exposure)
	processor.observeRealized(realized)
	processor.lastEquity.Set(equity)
	processor.lastRealized.Set(realized)
	return nil
}

func offlineSnapshotValues(snapshot portfolio.Snapshot) (*big.Rat, *big.Rat, *big.Rat, error) {
	settlement, exists := snapshot.Balances[snapshot.Numeraire]
	if !exists {
		return nil, nil, nil, fmt.Errorf("offline_metrics_settlement_missing")
	}
	available, ok := rational(settlement.Available.String())
	if !ok {
		return nil, nil, nil, fmt.Errorf("offline_metrics_balance_invalid")
	}
	reserved, ok := rational(settlement.Reserved.String())
	if !ok {
		return nil, nil, nil, fmt.Errorf("offline_metrics_balance_invalid")
	}
	equity := new(big.Rat).Add(available, reserved)
	realized, exposure := new(big.Rat), new(big.Rat)
	for _, position := range snapshot.Positions {
		cost, costOK := rational(position.Cost.String())
		unrealized, unrealizedOK := rational(position.UnrealizedPnL.String())
		positionRealized, realizedOK := rational(position.RealizedPnL.String())
		if !costOK || !unrealizedOK || !realizedOK {
			return nil, nil, nil, fmt.Errorf("offline_metrics_position_invalid")
		}
		equity.Add(equity, new(big.Rat).Add(cost, unrealized))
		exposure.Add(exposure, cost)
		realized.Add(realized, positionRealized)
	}
	return equity, realized, exposure, nil
}

func (processor *offlineEvidenceMetricsProcessor) observeEquity(equity, exposure *big.Rat) {
	if exposure.Cmp(processor.maximumExposure) > 0 {
		processor.maximumExposure.Set(exposure)
	}
	if equity.Cmp(processor.peak) > 0 {
		processor.peak.Set(equity)
	}
	if processor.peak.Sign() > 0 && equity.Cmp(processor.peak) < 0 {
		drawdown := new(big.Rat).Quo(new(big.Rat).Sub(processor.peak, equity), processor.peak)
		if drawdown.Cmp(processor.maximumDrawdown) > 0 {
			processor.maximumDrawdown.Set(drawdown)
		}
	}
	if processor.lastEquity.Sign() > 0 {
		periodReturn := new(big.Rat).Quo(new(big.Rat).Sub(equity, processor.lastEquity), processor.lastEquity)
		processor.returnSum.Add(processor.returnSum, periodReturn)
		if periodReturn.Sign() > 0 {
			processor.positiveReturns.Add(processor.positiveReturns, periodReturn)
		} else if periodReturn.Sign() < 0 {
			processor.negativeReturns.Add(processor.negativeReturns, new(big.Rat).Neg(periodReturn))
		}
	}
	processor.lastEquity.Set(equity)
}

func (processor *offlineEvidenceMetricsProcessor) observeRealized(realized *big.Rat) {
	realizedDelta := new(big.Rat).Sub(realized, processor.lastRealized)
	if realizedDelta.Sign() > 0 {
		processor.grossProfit.Add(processor.grossProfit, realizedDelta)
		if realizedDelta.Cmp(processor.largestWin) > 0 {
			processor.largestWin.Set(realizedDelta)
		}
		processor.wins++
		processor.lastOutcome = 1
	} else if realizedDelta.Sign() < 0 {
		loss := new(big.Rat).Neg(realizedDelta)
		processor.grossLoss.Add(processor.grossLoss, loss)
		if loss.Cmp(processor.largestLoss) > 0 {
			processor.largestLoss.Set(loss)
		}
		processor.losses++
		processor.lastOutcome = -1
	}
	processor.lastRealized.Set(realized)
}

func (processor *offlineEvidenceMetricsProcessor) observeOrders(orders []execution.Order,
	costContext offlineEventCostContext) error {
	for _, order := range orders {
		processor.observeOrderState(order)
		for _, fill := range order.Fills {
			if err := processor.observeFill(order.Identity.Side, fill, costContext); err != nil {
				return err
			}
		}
	}
	return nil
}

func (processor *offlineEvidenceMetricsProcessor) observeOrderState(order execution.Order) {
	orderID := order.Identity.ID.String()
	if prior, seen := processor.seenOrderStates[orderID]; seen && prior == order.State {
		return
	}
	switch order.State {
	case execution.OrderFilled:
		processor.fullFills++
	case execution.OrderPartiallyFilled:
		processor.partialFills++
	case execution.OrderCanceled:
		processor.canceled++
		if len(order.Fills) == 0 {
			processor.missedFills++
		}
	case execution.OrderExpired:
		processor.expired++
		if len(order.Fills) == 0 {
			processor.missedFills++
		}
	case execution.OrderRejected:
		processor.rejected++
	}
	processor.seenOrderStates[orderID] = order.State
}

func (processor *offlineEvidenceMetricsProcessor) observeFill(side domain.Side, fill execution.FillFact,
	costContext offlineEventCostContext) error {
	key := fill.ID.String()
	if _, duplicate := processor.seenFills[key]; duplicate {
		return nil
	}
	quantity, quantityOK := rational(fill.Quantity.String())
	price, priceOK := rational(fill.Price.String())
	fee, feeOK := rational(fill.Fee.String())
	rebate, rebateOK := rational(fill.Rebate.String())
	if !quantityOK || !priceOK || !feeOK || !rebateOK || quantity.Sign() < 0 || price.Sign() < 0 {
		return fmt.Errorf("offline_metrics_fill_invalid")
	}
	processor.turnover.Add(processor.turnover, new(big.Rat).Mul(quantity, price))
	spread, slippage, latency, err := offlineFillCosts(costContext, side, fill)
	if err != nil {
		return err
	}
	processor.spreadCost.Add(processor.spreadCost, spread)
	processor.slippageCost.Add(processor.slippageCost, slippage)
	processor.latencyDeterioration.Add(processor.latencyDeterioration, latency)
	processor.observeFillFee(new(big.Rat).Sub(fee, rebate))
	processor.seenFills[key], processor.trades = struct{}{}, processor.trades+1
	return nil
}

func (processor *offlineEvidenceMetricsProcessor) observeFillFee(netFee *big.Rat) {
	processor.fees.Add(processor.fees, netFee)
	if processor.lastOutcome > 0 {
		processor.feesWinners.Add(processor.feesWinners, netFee)
	} else if processor.lastOutcome < 0 {
		processor.feesLosers.Add(processor.feesLosers, netFee)
	}
}

// Metrics returns exact aggregate evidence derived from all observed virtual
// ledger snapshots and deduplicated fills.
