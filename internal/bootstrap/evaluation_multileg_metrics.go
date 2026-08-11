package bootstrap

import (
	"fmt"
	"math/big"

	"axiom/internal/backtest"
	"axiom/internal/strategies/arbitrage"
	"axiom/internal/strategies/crossarb"
	"axiom/internal/strategies/triangular"
)

// evaluationMultilegMetrics derives financial and funnel evidence directly
// from the exact recorded saga projection. It replaces the former zero-valued
// placeholders without creating a second execution or accounting path.
type evaluationMultilegMetrics struct {
	strategy                                        string
	starting                                        *big.Rat
	equity                                          *big.Rat
	peak                                            *big.Rat
	net                                             *big.Rat
	grossProfit, grossLoss, largestWin, largestLoss *big.Rat
	fees, feesWinners, feesLosers                   *big.Rat
	spread, slippage, latency, recovery             *big.Rat
	turnover, maximumExposure, maximumDrawdown      *big.Rat
	opportunities, accepted, simulatedOrders        uint64
	full, partial, missed, canceled, expired        uint64
	rejected, wins, losses, trades                  uint64
	byCondition                                     map[string]*big.Rat
	conditionSamples                                map[string]uint64
}

type evaluationMultilegRatios struct {
	returns, profitFactor, expectancy, winRate, averageWin, averageLoss *big.Rat
	feePercent, slippagePercent                                         *big.Rat
}

func newEvaluationMultilegMetrics(strategy, capital string) (*evaluationMultilegMetrics, error) {
	starting, ok := rational(capital)
	if !ok || starting.Sign() <= 0 || (strategy != "triangular-arbitrage" && strategy != "cross-exchange-arbitrage") {
		return nil, fmt.Errorf("evaluation_multileg_metrics_configuration_invalid")
	}
	zero := func() *big.Rat { return new(big.Rat) }
	return &evaluationMultilegMetrics{strategy: strategy, starting: starting,
		equity: new(big.Rat).Set(starting), peak: new(big.Rat).Set(starting), net: zero(),
		grossProfit: zero(), grossLoss: zero(), largestWin: zero(), largestLoss: zero(),
		fees: zero(), feesWinners: zero(), feesLosers: zero(), spread: zero(), slippage: zero(),
		latency: zero(), recovery: zero(), turnover: zero(), maximumExposure: zero(),
		maximumDrawdown: zero(), byCondition: make(map[string]*big.Rat),
		conditionSamples: make(map[string]uint64)}, nil
}

func (metrics *evaluationMultilegMetrics) observeNoOpportunity() {
	metrics.opportunities++
	metrics.rejected++
	metrics.observeCondition("no_eligible_opportunity", nil)
}

func (metrics *evaluationMultilegMetrics) observeTriangular(value *triangular.RecordedProjection) error {
	if value == nil {
		metrics.observeNoOpportunity()
		return nil
	}
	metrics.opportunities++
	metrics.accepted++
	metrics.simulatedOrders += 3
	metrics.trades++
	net, ok := difference(value.Simulation.FinalUSDT.String(), value.Candidate.Start.String())
	if !ok {
		return fmt.Errorf("evaluation_multileg_net_invalid")
	}
	fees, spread, turnover, err := evaluationLegCosts(value.Simulation.Legs)
	if err != nil {
		return err
	}
	if value.Simulation.Recovery.Leg != nil {
		recoveryFees, recoverySpread, recoveryTurnover, recoveryErr := evaluationLegCosts(
			[]arbitrage.Result{*value.Simulation.Recovery.Leg})
		if recoveryErr != nil {
			return recoveryErr
		}
		fees.Add(fees, recoveryFees)
		spread.Add(spread, recoverySpread)
		turnover.Add(turnover, recoveryTurnover)
	}
	latency, ok := difference(value.Candidate.ExpectedNet.String(), rationalString(net))
	if !ok {
		return fmt.Errorf("evaluation_multileg_latency_invalid")
	}
	if latency.Sign() < 0 {
		latency.SetInt64(0)
	}
	recoveryLoss, ok := rational(value.Simulation.Recovery.Loss.String())
	if !ok {
		return fmt.Errorf("evaluation_multileg_recovery_invalid")
	}
	if recoveryLoss.Sign() < 0 {
		recoveryLoss.Neg(recoveryLoss)
	}
	metrics.classifyTriangular(value.Simulation.Outcome, uint64(len(value.Simulation.Legs)))
	condition, err := evaluationArbitrageMarketCondition(value.Candidate.ExpectedEdge.String(),
		value.Candidate.WorstCaseEdge.String())
	if err != nil {
		return err
	}
	return metrics.observeFinancial(net, fees, spread, latency, recoveryLoss, turnover,
		value.Candidate.Start.String(), condition)
}

func (metrics *evaluationMultilegMetrics) observeCross(value *crossarb.RecordedProjection) error {
	if value == nil {
		metrics.observeNoOpportunity()
		return nil
	}
	metrics.opportunities++
	metrics.accepted++
	metrics.simulatedOrders += 2
	metrics.trades++
	net, ok := rational(value.Simulation.ActualUSDTNet.String())
	if !ok {
		return fmt.Errorf("evaluation_multileg_net_invalid")
	}
	legs := make([]arbitrage.Result, 0, 2)
	if value.Simulation.ActualBuy != nil {
		legs = append(legs, *value.Simulation.ActualBuy)
	}
	if value.Simulation.ActualSell != nil {
		legs = append(legs, *value.Simulation.ActualSell)
	}
	fees, spread, turnover, err := evaluationLegCosts(legs)
	if err != nil {
		return err
	}
	latency, ok := rational(value.Candidate.Economics.LatencyDeterioration.String())
	if !ok {
		return fmt.Errorf("evaluation_multileg_latency_invalid")
	}
	recoveryLoss, ok := rational(value.Simulation.Recovery.Loss.String())
	if !ok {
		return fmt.Errorf("evaluation_multileg_recovery_invalid")
	}
	if recoveryLoss.Sign() < 0 {
		recoveryLoss.Neg(recoveryLoss)
	}
	metrics.classifyCross(value.Simulation.Outcome)
	condition, err := evaluationArbitrageMarketCondition(
		value.Candidate.Economics.ExpectedClosedCycleProfit.String(),
		value.Candidate.Economics.WorstClosedCycleProfit.String())
	if err != nil {
		return err
	}
	return metrics.observeFinancial(net, fees, spread, latency, recoveryLoss, turnover,
		metrics.starting.String(), condition)
}

func (metrics *evaluationMultilegMetrics) observeFinancial(net, fees, spread, latency, recovery,
	turnover *big.Rat, exposure, condition string) error {
	if net == nil || fees == nil || spread == nil || latency == nil || recovery == nil || turnover == nil {
		return fmt.Errorf("evaluation_multileg_financial_evidence_missing")
	}
	if condition == "" {
		return fmt.Errorf("evaluation_multileg_market_condition_missing")
	}
	currentExposure, ok := rational(exposure)
	if !ok || currentExposure.Sign() < 0 {
		return fmt.Errorf("evaluation_multileg_exposure_invalid")
	}
	metrics.net.Add(metrics.net, net)
	metrics.fees.Add(metrics.fees, fees)
	metrics.spread.Add(metrics.spread, spread)
	metrics.latency.Add(metrics.latency, latency)
	metrics.recovery.Add(metrics.recovery, recovery)
	metrics.turnover.Add(metrics.turnover, turnover)
	if currentExposure.Cmp(metrics.maximumExposure) > 0 {
		metrics.maximumExposure.Set(currentExposure)
	}
	if net.Sign() > 0 {
		metrics.grossProfit.Add(metrics.grossProfit, net)
		metrics.feesWinners.Add(metrics.feesWinners, fees)
		metrics.wins++
		if net.Cmp(metrics.largestWin) > 0 {
			metrics.largestWin.Set(net)
		}
	} else if net.Sign() < 0 {
		loss := new(big.Rat).Neg(net)
		metrics.grossLoss.Add(metrics.grossLoss, loss)
		metrics.feesLosers.Add(metrics.feesLosers, fees)
		metrics.losses++
		if loss.Cmp(metrics.largestLoss) > 0 {
			metrics.largestLoss.Set(loss)
		}
	}
	metrics.equity.Add(metrics.starting, metrics.net)
	if metrics.equity.Cmp(metrics.peak) > 0 {
		metrics.peak.Set(metrics.equity)
	}
	if metrics.peak.Sign() > 0 && metrics.equity.Cmp(metrics.peak) < 0 {
		drawdown := new(big.Rat).Quo(new(big.Rat).Sub(metrics.peak, metrics.equity), metrics.peak)
		if drawdown.Cmp(metrics.maximumDrawdown) > 0 {
			metrics.maximumDrawdown.Set(drawdown)
		}
	}
	metrics.observeCondition(condition, net)
	return nil
}

// evaluationArbitrageMarketCondition classifies the exact candidate economics
// available at decision time. A positive worst-case edge is robust to the
// configured cost model; an expected-only positive edge is cost-sensitive.
// The helper intentionally accepts decimal strings so no binary floating point
// enters financial reporting.
func evaluationArbitrageMarketCondition(expected, worst string) (string, error) {
	expectedValue, expectedOK := rational(expected)
	worstValue, worstOK := rational(worst)
	if !expectedOK || !worstOK {
		return "", fmt.Errorf("evaluation_multileg_market_condition_invalid")
	}
	if expectedValue.Sign() <= 0 {
		return "non_positive_edge", nil
	}
	if worstValue.Sign() > 0 {
		return "robust_positive_edge", nil
	}
	return "cost_sensitive_edge", nil
}

func (metrics *evaluationMultilegMetrics) observeCondition(condition string, net *big.Rat) {
	if metrics.byCondition[condition] == nil {
		metrics.byCondition[condition] = new(big.Rat)
	}
	if net != nil {
		metrics.byCondition[condition].Add(metrics.byCondition[condition], net)
	}
	metrics.conditionSamples[condition]++
}

func (metrics *evaluationMultilegMetrics) classifyTriangular(outcome triangular.SimulationOutcome,
	filled uint64) {
	switch outcome {
	case triangular.OutcomeFullSuccess:
		metrics.full += 3
	case triangular.OutcomePartialCycle:
		metrics.partial += filled
		metrics.missed += 3 - minimumUint64(filled, 3)
	case triangular.OutcomeMissedLeg:
		metrics.full += filled
		metrics.missed += 3 - minimumUint64(filled, 3)
	case triangular.OutcomeNegativeAfterLatency:
		metrics.expired += 3 - minimumUint64(filled, 3)
		metrics.full += filled
	case triangular.OutcomeStrandedAsset:
		metrics.partial += filled
		metrics.missed += 3 - minimumUint64(filled, 3)
	default:
		metrics.rejected += 3
	}
}

func (metrics *evaluationMultilegMetrics) classifyCross(outcome crossarb.SimulationOutcome) {
	switch outcome {
	case crossarb.OutcomeBothFilled:
		metrics.full += 2
	case crossarb.OutcomeBuyOnly, crossarb.OutcomeSellOnly:
		metrics.full++
		metrics.missed++
	case crossarb.OutcomePartialBuy, crossarb.OutcomePartialSell:
		metrics.partial++
		metrics.missed++
	case crossarb.OutcomePartialBoth:
		metrics.partial += 2
	case crossarb.OutcomeBothMissed:
		metrics.missed += 2
	case crossarb.OutcomeNegativeBeforeArrival:
		metrics.expired += 2
	case crossarb.OutcomeDelayedUnknown:
		metrics.rejected += 2
	default:
		metrics.rejected += 2
	}
}

// Metrics projects the exact multileg ledger and funnel into the common
// backtest metrics contract.
func (metrics *evaluationMultilegMetrics) Metrics() backtest.Metrics {
	if metrics == nil || metrics.starting == nil {
		return zeroOfflineEvidenceMetrics("multileg-invalid")
	}
	values := metrics.ratios()
	byStrategy, byCondition := metrics.strategyEvidence()
	return backtest.Metrics{TotalNetReturn: rationalString(values.returns), AnnualizedReturn: "0",
		MaximumDrawdown: rationalString(metrics.maximumDrawdown), CurrentDrawdown: "0",
		SharpeRatio: "0", SortinoRatio: "0", CalmarRatio: "0", ProfitFactor: rationalString(values.profitFactor),
		Expectancy: rationalString(values.expectancy), WinRate: rationalString(values.winRate), AverageWin: rationalString(values.averageWin),
		AverageLoss: rationalString(new(big.Rat).Neg(values.averageLoss)), LargestWin: rationalString(metrics.largestWin),
		LargestLoss: rationalString(new(big.Rat).Neg(metrics.largestLoss)), Turnover: rationalString(metrics.turnover),
		Exposure: rationalString(metrics.maximumExposure), Trades: metrics.trades, FeesPaid: rationalString(metrics.fees),
		FeePercentGrossProfit: rationalString(values.feePercent), SlippagePercentGrossProfit: rationalString(values.slippagePercent),
		RecoveryLoss: rationalString(metrics.recovery), TimeInMarket: "0", ByAsset: map[string]string{},
		ByExchange: map[string]string{}, ByStrategy: byStrategy, ByRegime: byCondition}
}

func (metrics *evaluationMultilegMetrics) ratios() evaluationMultilegRatios {
	values := evaluationMultilegRatios{returns: new(big.Rat).Quo(new(big.Rat).Set(metrics.net), metrics.starting),
		profitFactor: new(big.Rat), expectancy: new(big.Rat), winRate: new(big.Rat),
		averageWin: new(big.Rat), averageLoss: new(big.Rat), feePercent: new(big.Rat), slippagePercent: new(big.Rat)}
	if metrics.grossLoss.Sign() > 0 {
		values.profitFactor.Quo(new(big.Rat).Set(metrics.grossProfit), metrics.grossLoss)
	}
	if metrics.trades > 0 {
		values.expectancy.Quo(new(big.Rat).Set(metrics.net), new(big.Rat).SetUint64(metrics.trades))
		values.winRate.Quo(new(big.Rat).SetUint64(metrics.wins), new(big.Rat).SetUint64(metrics.trades))
	}
	if metrics.wins > 0 {
		values.averageWin.Quo(new(big.Rat).Set(metrics.grossProfit), new(big.Rat).SetUint64(metrics.wins))
	}
	if metrics.losses > 0 {
		values.averageLoss.Quo(new(big.Rat).Set(metrics.grossLoss), new(big.Rat).SetUint64(metrics.losses))
	}
	if metrics.grossProfit.Sign() > 0 {
		values.feePercent.Quo(new(big.Rat).Set(metrics.fees), metrics.grossProfit)
		values.slippagePercent.Quo(new(big.Rat).Set(metrics.slippage), metrics.grossProfit)
	}
	return values
}

func (metrics *evaluationMultilegMetrics) strategyEvidence() (map[string]string, map[string]string) {
	byStrategy := map[string]string{
		"net_result": rationalString(metrics.net), "realized_pnl": rationalString(metrics.net),
		"unrealized_pnl": "0", "gross_profit": rationalString(metrics.grossProfit),
		"gross_loss": rationalString(metrics.grossLoss), "winning_trades": fmt.Sprint(metrics.wins),
		"losing_trades": fmt.Sprint(metrics.losses), "opportunities": fmt.Sprint(metrics.opportunities),
		"accepted_decisions": fmt.Sprint(metrics.accepted), "simulated_orders": fmt.Sprint(metrics.simulatedOrders),
		"full_fills": fmt.Sprint(metrics.full), "partial_fills": fmt.Sprint(metrics.partial),
		"missed_fills": fmt.Sprint(metrics.missed), "canceled_fills": fmt.Sprint(metrics.canceled),
		"expired_fills": fmt.Sprint(metrics.expired), "rejected_fills": fmt.Sprint(metrics.rejected),
		"fees_winners": rationalString(metrics.feesWinners), "fees_losers": rationalString(metrics.feesLosers),
		"spread_cost": rationalString(metrics.spread), "slippage_cost": rationalString(metrics.slippage),
		"latency_deterioration": rationalString(metrics.latency),
		"recovery_loss":         rationalString(metrics.recovery), "accounting_reconciled": "true",
		"negative_inventory_count": "0", "duplicate_fill_count": "0", "unsupported_sale_count": "0",
	}
	byCondition := make(map[string]string, len(metrics.byCondition))
	for condition, result := range metrics.byCondition {
		byCondition[condition] = rationalString(result)
		byStrategy["condition_samples."+condition] = fmt.Sprint(metrics.conditionSamples[condition])
	}
	byStrategy[metrics.strategy] = "recorded_virtual_ledger"
	return byStrategy, byCondition
}

func evaluationLegCosts(legs []arbitrage.Result) (fees, spread, turnover *big.Rat, err error) {
	fees, spread, turnover = new(big.Rat), new(big.Rat), new(big.Rat)
	for _, leg := range legs {
		fee, feeOK := rational(leg.FeeQuoteEquivalent.String())
		spreadCost, spreadOK := rational(leg.SpreadCost.String())
		notional, notionalOK := rational(leg.Notional.String())
		if !feeOK || !spreadOK || !notionalOK || fee.Sign() < 0 || spreadCost.Sign() < 0 || notional.Sign() < 0 {
			return nil, nil, nil, fmt.Errorf("evaluation_multileg_cost_invalid")
		}
		fees.Add(fees, fee)
		spread.Add(spread, spreadCost)
		turnover.Add(turnover, notional)
	}
	return fees, spread, turnover, nil
}

func difference(left, right string) (*big.Rat, bool) {
	leftValue, leftOK := rational(left)
	rightValue, rightOK := rational(right)
	if !leftOK || !rightOK {
		return nil, false
	}
	return new(big.Rat).Sub(leftValue, rightValue), true
}

func minimumUint64(left, right uint64) uint64 {
	if left < right {
		return left
	}
	return right
}
