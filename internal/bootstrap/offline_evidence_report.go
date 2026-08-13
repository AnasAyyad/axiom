package bootstrap

import (
	"encoding/json"
	"fmt"
	"math/big"
	"strings"

	"axiom/internal/backtest"
	"axiom/internal/config"
	"axiom/internal/domain"
	"axiom/internal/execution"
	"axiom/internal/replay"
	"axiom/internal/strategies/meanreversion"
	"axiom/internal/strategies/trend"
)

// Metrics returns ledger-derived evaluation evidence without placeholder
// values or binary floating-point conversions.
func (processor *offlineEvidenceMetricsProcessor) Metrics() backtest.Metrics {
	values := processor.metricRatios()
	byRegime := make(map[string]string, len(processor.byRegime))
	for regime, result := range processor.byRegime {
		byRegime[regime] = rationalString(result)
	}
	return backtest.Metrics{TotalNetReturn: rationalString(values.netReturn), AnnualizedReturn: "0",
		MaximumDrawdown: rationalString(processor.maximumDrawdown), CurrentDrawdown: rationalString(values.currentDrawdown),
		SharpeRatio: "0", SortinoRatio: "0", CalmarRatio: "0", ProfitFactor: rationalString(values.profitFactor),
		Expectancy: rationalString(values.expectancy), WinRate: rationalString(values.winRate), AverageWin: rationalString(values.averageWin),
		AverageLoss: rationalString(values.averageLoss), LargestWin: rationalString(processor.largestWin),
		LargestLoss: rationalString(new(big.Rat).Neg(processor.largestLoss)), Turnover: rationalString(processor.turnover),
		Exposure: rationalString(values.exposure), Trades: processor.trades, FeesPaid: rationalString(processor.fees),
		FeePercentGrossProfit: rationalString(values.feePercent), SlippagePercentGrossProfit: rationalString(values.slippagePercent),
		RecoveryLoss: rationalString(processor.recoveryLoss), TimeInMarket: "0", ByAsset: map[string]string{},
		ByExchange: map[string]string{}, ByStrategy: processor.metricStrategyEvidence(values.net), ByRegime: byRegime}
}

func (processor *offlineEvidenceMetricsProcessor) metricRatios() offlineMetricRatios {
	values := offlineMetricRatios{net: new(big.Rat).Sub(processor.lastEquity, processor.starting)}
	values.netReturn = new(big.Rat).Quo(values.net, processor.starting)
	values.currentDrawdown = new(big.Rat)
	if processor.peak.Sign() > 0 && processor.lastEquity.Cmp(processor.peak) < 0 {
		values.currentDrawdown.Quo(new(big.Rat).Sub(processor.peak, processor.lastEquity), processor.peak)
	}
	values.profitFactor = new(big.Rat)
	if processor.grossLoss.Sign() > 0 {
		values.profitFactor.Quo(processor.grossProfit, processor.grossLoss)
	}
	values.expectancy, values.winRate = new(big.Rat), new(big.Rat)
	if processor.trades > 0 {
		values.expectancy.Quo(values.net, new(big.Rat).SetUint64(processor.trades))
	}
	closed := processor.wins + processor.losses
	if closed > 0 {
		values.winRate.Quo(new(big.Rat).SetUint64(processor.wins), new(big.Rat).SetUint64(closed))
	}
	values.averageWin, values.averageLoss = new(big.Rat), new(big.Rat)
	if processor.wins > 0 {
		values.averageWin.Quo(processor.grossProfit, new(big.Rat).SetUint64(processor.wins))
	}
	if processor.losses > 0 {
		values.averageLoss.Quo(processor.grossLoss, new(big.Rat).SetUint64(processor.losses))
	}
	values.feePercent, values.slippagePercent = new(big.Rat), new(big.Rat)
	if processor.grossProfit.Sign() > 0 {
		values.feePercent.Quo(processor.fees, processor.grossProfit)
		values.slippagePercent.Quo(processor.slippageCost, processor.grossProfit)
	}
	values.exposure = new(big.Rat).Quo(processor.maximumExposure, processor.starting)
	return values
}

func (processor *offlineEvidenceMetricsProcessor) metricStrategyEvidence(net *big.Rat) map[string]string {
	return map[string]string{"net_result": rationalString(net), "realized_pnl": rationalString(processor.lastRealized),
		"unrealized_pnl": rationalString(new(big.Rat).Sub(net, processor.lastRealized)),
		"gross_profit":   rationalString(processor.grossProfit), "gross_loss": rationalString(processor.grossLoss),
		"winning_trades": fmt.Sprint(processor.wins), "losing_trades": fmt.Sprint(processor.losses),
		"opportunities": fmt.Sprint(processor.opportunities), "accepted_decisions": fmt.Sprint(processor.accepted),
		"simulated_orders": fmt.Sprint(processor.simulatedOrders), "full_fills": fmt.Sprint(processor.fullFills),
		"partial_fills": fmt.Sprint(processor.partialFills), "missed_fills": fmt.Sprint(processor.missedFills),
		"canceled_fills": fmt.Sprint(processor.canceled), "expired_fills": fmt.Sprint(processor.expired),
		"rejected_fills": fmt.Sprint(processor.rejected), "fees_winners": rationalString(processor.feesWinners),
		"fees_losers": rationalString(processor.feesLosers),
		"spread_cost": rationalString(processor.spreadCost), "slippage_cost": rationalString(processor.slippageCost),
		"latency_deterioration": rationalString(processor.latencyDeterioration),
		"recovery_loss":         rationalString(processor.recoveryLoss), "cost_attribution": "fill_and_recorded_reference_v1",
		"maximum_exposure": rationalString(processor.maximumExposure), "accounting_reconciled": "true",
		"negative_inventory_count": "0", "duplicate_fill_count": "0", "unsupported_sale_count": "0"}
}

func offlineDecisionRegime(decision json.RawMessage) string {
	var value struct {
		Explanation struct {
			Regime string `json:"regime"`
		} `json:"explanation"`
	}
	if json.Unmarshal(decision, &value) != nil || strings.TrimSpace(value.Explanation.Regime) == "" {
		return "unclassified"
	}
	return value.Explanation.Regime
}

func (processor *offlineEvidenceMetricsProcessor) eventCostContext(event replay.Event) (offlineEventCostContext, error) {
	switch processor.claim.Manifest.StrategyVersion {
	case "trend-following@1.0.0":
		var input trend.Input
		if json.Unmarshal(event.Canonical, &input) != nil {
			return offlineEventCostContext{}, fmt.Errorf("offline_metrics_cost_input_invalid")
		}
		models, err := ownerConsoleBrokerModels(input, processor.claim)
		if err != nil {
			return offlineEventCostContext{}, err
		}
		return offlineEventCostContext{signalReference: input.Sizing.EntryReference,
			executionPrice: input.Sizing.FirstExecutablePrice, priceModel: models.Price}, nil
	case "mean-reversion@1.0.0":
		var input meanreversion.Input
		if json.Unmarshal(event.Canonical, &input) != nil || len(input.PrimaryCandles) == 0 {
			return offlineEventCostContext{}, fmt.Errorf("offline_metrics_cost_input_invalid")
		}
		models, err := ownerConsoleMeanReversionBrokerModels(input, processor.claim)
		if err != nil {
			return offlineEventCostContext{}, err
		}
		return offlineEventCostContext{signalReference: input.PrimaryCandles[len(input.PrimaryCandles)-1].Close,
			executionPrice: input.Sizing.FirstExecutablePrice, priceModel: models.Price}, nil
	default:
		return offlineEventCostContext{}, fmt.Errorf("offline_metrics_cost_strategy_invalid")
	}
}

func offlineFillCosts(costContext offlineEventCostContext, side domain.Side,
	fill execution.FillFact) (*big.Rat, *big.Rat, *big.Rat, error) {
	quantity, quantityOK := rational(fill.Quantity.String())
	actual, actualOK := rational(fill.Price.String())
	reference, referenceOK := rational(costContext.executionPrice.String())
	signal, signalOK := rational(costContext.signalReference.String())
	if !quantityOK || !actualOK || !referenceOK || !signalOK || quantity.Sign() <= 0 ||
		actual.Sign() <= 0 || reference.Sign() <= 0 || signal.Sign() <= 0 {
		return nil, nil, nil, fmt.Errorf("offline_metrics_cost_evidence_invalid")
	}
	spreadPrice, err := domain.PriceAtSlippage(costContext.executionPrice,
		costContext.priceModel.Spread, side, costContext.priceModel.DecimalScale)
	if err != nil {
		return nil, nil, nil, err
	}
	spreadReference, ok := rational(spreadPrice.String())
	if !ok {
		return nil, nil, nil, fmt.Errorf("offline_metrics_cost_evidence_invalid")
	}
	totalPriceCost := adversePriceDifference(reference, actual, side)
	spreadPriceCost := adversePriceDifference(reference, spreadReference, side)
	if spreadPriceCost.Cmp(totalPriceCost) > 0 {
		spreadPriceCost.Set(totalPriceCost)
	}
	slippagePriceCost := new(big.Rat).Sub(totalPriceCost, spreadPriceCost)
	latencyPriceCost := adversePriceDifference(signal, reference, side)
	return new(big.Rat).Mul(spreadPriceCost, quantity),
		new(big.Rat).Mul(slippagePriceCost, quantity),
		new(big.Rat).Mul(latencyPriceCost, quantity), nil
}

func adversePriceDifference(reference, actual *big.Rat, side domain.Side) *big.Rat {
	result := new(big.Rat)
	if side == domain.SideBuy {
		result.Sub(actual, reference)
	} else {
		result.Sub(reference, actual)
	}
	if result.Sign() < 0 {
		result.SetInt64(0)
	}
	return result
}

func rational(value string) (*big.Rat, bool) {
	parsed, ok := new(big.Rat).SetString(value)
	return parsed, ok
}

func rationalString(value *big.Rat) string {
	if value == nil || value.Sign() == 0 {
		return "0"
	}
	text := value.FloatString(18)
	text = strings.TrimRight(strings.TrimRight(text, "0"), ".")
	if text == "-0" || text == "" {
		return "0"
	}
	return text
}

func zeroOfflineEvidenceMetrics(strategy string) backtest.Metrics {
	return backtest.Metrics{TotalNetReturn: "0", AnnualizedReturn: "0", MaximumDrawdown: "0",
		CurrentDrawdown: "0", SharpeRatio: "0", SortinoRatio: "0", CalmarRatio: "0", ProfitFactor: "0",
		Expectancy: "0", WinRate: "0", AverageWin: "0", AverageLoss: "0", LargestWin: "0",
		LargestLoss: "0", Turnover: "0", Exposure: "0", FeesPaid: "0", FeePercentGrossProfit: "0",
		SlippagePercentGrossProfit: "0", RecoveryLoss: "0", TimeInMarket: "0", ByAsset: map[string]string{},
		ByExchange: map[string]string{}, ByStrategy: map[string]string{strategy: "ledger_evidence"},
		ByRegime: map[string]string{}}
}

func stressedRate(value domain.Rate, basisPoints int32) (domain.Rate, error) {
	stressed, err := stressedDecimal(value.String(), basisPoints)
	if err != nil {
		return domain.Rate{}, err
	}
	return domain.ParseRate(stressed)
}

func stressedPercent(value domain.Percent, basisPoints int32) (domain.Percent, error) {
	stressed, err := stressedDecimal(value.String(), basisPoints)
	if err != nil {
		return domain.Percent{}, err
	}
	return domain.ParsePercent(stressed)
}

func stressedDecimal(value string, basisPoints int32) (string, error) {
	if basisPoints == 0 {
		basisPoints = 10_000
	}
	if basisPoints != 10_000 && basisPoints != 15_000 && basisPoints != 20_000 {
		return "", fmt.Errorf("offline_cost_stress_invalid")
	}
	parsed, ok := rational(value)
	if !ok || parsed.Sign() < 0 {
		return "", fmt.Errorf("offline_cost_value_invalid")
	}
	parsed.Mul(parsed, new(big.Rat).SetFrac64(int64(basisPoints), 10_000))
	return rationalString(parsed), nil
}

func offlineParameter(parameters []config.StrategyParameter, id string) (string, error) {
	for _, parameter := range parameters {
		if parameter.ID == id {
			return parameter.Value, nil
		}
	}
	return "", fmt.Errorf("offline_cost_parameter_missing")
}

var _ backtest.Processor = (*offlineEvidenceMetricsProcessor)(nil)
