package meanreversion

import (
	"time"

	"github.com/cockroachdb/apd/v3"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
)

type entryCalculation struct {
	entry, atrStop, nominalStop, stressedExit, unitRisk decimal
	riskBudget, notionalCap, rawQuantity                decimal
}

func (evaluator *Evaluator) sizeEntry(input Input, latest exchangecontracts.Candle,
	indicators indicatorSnapshot, explanation Explanation) (Candidate, string) {
	if !input.Sizing.CentralRiskEligible || input.Sizing.FencingToken == 0 || input.Sizing.LiquidityDomain == "" ||
		input.Sizing.InstrumentMetadata.Instrument != input.Instrument || input.Sizing.InstrumentMetadata.Version == 0 {
		return Candidate{}, ReasonRiskClipped
	}
	if !validExecutableObservation(input, latest) {
		return Candidate{}, ReasonNoPostLatencyPrice
	}
	calculation, reason := evaluator.calculateEntry(input, indicators)
	if reason != "" {
		return Candidate{}, reason
	}
	return evaluator.buildEntryCandidate(input, latest, explanation, calculation)
}

func validExecutableObservation(input Input, latest exchangecontracts.Candle) bool {
	observed := input.Sizing.FirstExecutableAt
	return !observed.IsZero() && observed.Location() == time.UTC &&
		observed.After(latest.OpenTime.Add(time.Hour)) && !observed.After(input.Now)
}

func (evaluator *Evaluator) calculateEntry(input Input, indicators indicatorSnapshot) (entryCalculation, string) {
	entry, atrStop, nominal, stressed, unitRisk, err := evaluator.stressedUnitRisk(input, indicators)
	if err != nil {
		return entryCalculation{}, ReasonInvalidSizing
	}
	riskBudget, notionalCap, rawQuantity, reason := evaluator.riskQuantity(input, entry, unitRisk)
	return entryCalculation{entry: entry, atrStop: atrStop, nominalStop: nominal, stressedExit: stressed,
		unitRisk: unitRisk, riskBudget: riskBudget, notionalCap: notionalCap, rawQuantity: rawQuantity}, reason
}

func (evaluator *Evaluator) stressedUnitRisk(input Input, indicators indicatorSnapshot) (decimal, decimal, decimal, decimal, decimal, error) {
	entry, atrStop, nominal, err := evaluator.nominalStops(input, indicators)
	if err != nil {
		return decimal{}, decimal{}, decimal{}, decimal{}, decimal{}, err
	}
	stressed, err := evaluator.stressedExit(input, entry, nominal)
	if err != nil {
		return decimal{}, decimal{}, decimal{}, decimal{}, decimal{}, err
	}
	unitRisk, err := feeAdjustedUnitRisk(input, entry, stressed)
	if err != nil {
		return decimal{}, decimal{}, decimal{}, decimal{}, decimal{}, err
	}
	return entry, atrStop, nominal, stressed, unitRisk, nil
}

func (evaluator *Evaluator) nominalStops(input Input, indicators indicatorSnapshot) (decimal, decimal, decimal, error) {
	entry, entryErr := parseDecimal(input.Sizing.FirstExecutablePrice.String())
	atr, atrErr := parseDecimal(indicators.atr.String())
	protective, protectiveErr := parseDecimal(indicators.protectivePrice.String())
	if entryErr != nil || atrErr != nil || protectiveErr != nil || entry.value.Sign() <= 0 || atr.value.Sign() <= 0 {
		return decimal{}, decimal{}, decimal{}, strategyError(ReasonInvalidSizing)
	}
	distance, err := atr.multiply(evaluator.configuration.ProtectiveStopMultiplier, apd.RoundHalfEven)
	if err != nil {
		return decimal{}, decimal{}, decimal{}, err
	}
	atrStop, err := entry.subtract(distance)
	if err != nil || atrStop.value.Sign() <= 0 {
		return decimal{}, decimal{}, decimal{}, strategyError(ReasonInvalidSizing)
	}
	return entry, atrStop, minimum(atrStop, protective), nil
}

func (evaluator *Evaluator) stressedExit(input Input, entry, nominal decimal) (decimal, error) {
	gap, gapErr := parseDecimal(input.Sizing.GapAllowance.String())
	slippage, slippageErr := parseDecimal(input.Sizing.SlippageAllowance.String())
	if gapErr != nil || slippageErr != nil || gap.value.Sign() < 0 || slippage.value.Sign() < 0 {
		return decimal{}, strategyError(ReasonInvalidSizing)
	}
	gapRatio, gapRatioErr := gap.divide(entry, apd.RoundCeiling)
	slippageRatio, slippageRatioErr := slippage.divide(entry, apd.RoundCeiling)
	if gapRatioErr != nil || slippageRatioErr != nil || gapRatio.compare(evaluator.configuration.MaximumGapAllowance) > 0 ||
		slippageRatio.compare(evaluator.configuration.MaximumSlippage) > 0 {
		return decimal{}, strategyError(ReasonInvalidSizing)
	}
	stressed, err := nominal.subtract(gap)
	if err == nil {
		stressed, err = stressed.subtract(slippage)
	}
	if err != nil || stressed.value.Sign() <= 0 || stressed.compare(entry) >= 0 {
		return decimal{}, strategyError(ReasonInvalidSizing)
	}
	return stressed, nil
}

func feeAdjustedUnitRisk(input Input, entry, stressed decimal) (decimal, error) {
	entryFeeRate, entryFeeErr := parseDecimal(input.Sizing.EntryFeeRate.String())
	exitFeeRate, exitFeeErr := parseDecimal(input.Sizing.ExitFeeRate.String())
	if entryFeeErr != nil || exitFeeErr != nil {
		return decimal{}, strategyError(ReasonInvalidSizing)
	}
	entryFee, err := entry.multiply(entryFeeRate, apd.RoundCeiling)
	if err != nil {
		return decimal{}, err
	}
	exitFee, err := stressed.multiply(exitFeeRate, apd.RoundCeiling)
	unitRisk, unitErr := entry.subtract(stressed)
	if unitErr == nil {
		unitRisk, unitErr = unitRisk.add(entryFee)
	}
	if unitErr == nil {
		unitRisk, unitErr = unitRisk.add(exitFee)
	}
	if err != nil || unitErr != nil {
		return decimal{}, strategyError(ReasonInvalidSizing)
	}
	unitRisk, err = quantize(unitRisk, apd.RoundCeiling)
	if err != nil || unitRisk.value.Sign() <= 0 {
		return decimal{}, strategyError(ReasonInvalidSizing)
	}
	return unitRisk, nil
}

func (evaluator *Evaluator) riskQuantity(input Input, entry, unitRisk decimal) (decimal, decimal, decimal, string) {
	equity, equityErr := parseDecimal(input.Sizing.Equity.String())
	if equityErr != nil || equity.value.Sign() <= 0 {
		return decimal{}, decimal{}, decimal{}, ReasonInvalidSizing
	}
	riskBudget, err := equity.multiply(evaluator.configuration.RiskBudget, apd.RoundFloor)
	allocationCap, allocationErr := equity.multiply(evaluator.configuration.MaximumAllocation, apd.RoundFloor)
	available, availableErr := availableCash(input)
	if err != nil || allocationErr != nil || availableErr != nil || riskBudget.value.Sign() <= 0 || available.value.Sign() <= 0 {
		return decimal{}, decimal{}, decimal{}, ReasonRiskClipped
	}
	notionalCap := minimum(evaluator.configuration.MaximumNotional, minimum(allocationCap, available))
	for _, limit := range input.Sizing.NotionalLimits {
		parsed, parseErr := parseDecimal(limit.String())
		if parseErr != nil || parsed.value.Sign() <= 0 {
			return decimal{}, decimal{}, decimal{}, ReasonRiskClipped
		}
		notionalCap = minimum(notionalCap, parsed)
	}
	rawQuantity, err := riskBudget.divide(unitRisk, apd.RoundFloor)
	capQuantity, capErr := notionalCap.divide(entry, apd.RoundFloor)
	if err != nil || capErr != nil {
		return decimal{}, decimal{}, decimal{}, ReasonInvalidSizing
	}
	return riskBudget, notionalCap, minimum(rawQuantity, capQuantity), ""
}

func (evaluator *Evaluator) buildEntryCandidate(input Input, latest exchangecontracts.Candle,
	explanation Explanation, calculation entryCalculation) (Candidate, string) {
	quantity, err := domain.ParseQuantity(calculation.rawQuantity.stringValue())
	if err != nil {
		return Candidate{}, ReasonInvalidSizing
	}
	quantity, err = domain.RoundBuyQuantity(quantity, input.Sizing.InstrumentMetadata.QuantityStep)
	zero, _ := domain.ParseQuantity("0")
	if err != nil || quantity.Compare(zero) <= 0 {
		return Candidate{}, ReasonInvalidSizing
	}
	limit, err := domain.RoundMarketableLimitPrice(domain.SideBuy, input.Sizing.FirstExecutablePrice,
		input.Sizing.InstrumentMetadata.PriceTick)
	if err != nil {
		return Candidate{}, ReasonInvalidSizing
	}
	maximumSlippage, _ := domain.ParsePercent(evaluator.configuration.MaximumSlippage.stringValue())
	maximumPrice, err := domain.PriceAtSlippage(input.Sizing.FirstExecutablePrice, maximumSlippage, domain.SideBuy, 18)
	if err != nil || limit.Compare(maximumPrice) > 0 {
		return Candidate{}, ReasonMinimumFilter
	}
	notional, err := domain.CalculateNotional(limit, quantity, 18)
	if err != nil || quantity.Compare(input.Sizing.InstrumentMetadata.MinimumQuantity) < 0 ||
		notional.Compare(input.Sizing.InstrumentMetadata.MinimumNotional) < 0 {
		return Candidate{}, ReasonMinimumFilter
	}
	notionalValue, _ := parseDecimal(notional.String())
	if notionalValue.compare(calculation.notionalCap) > 0 {
		return Candidate{}, ReasonRiskClipped
	}
	explanation.RiskBudget, _ = domain.ParseMoney(calculation.riskBudget.stringValue())
	explanation.StressedUnitRisk, _ = domain.ParsePrice(calculation.unitRisk.stringValue())
	explanation.Attributes["atr_stop"] = calculation.atrStop.stringValue()
	explanation.Attributes["nominal_stop"] = calculation.nominalStop.stringValue()
	explanation.Attributes["stressed_exit"] = calculation.stressedExit.stringValue()
	explanation.Attributes["quantity_rounding"] = "down"
	explanation.Attributes["buy_price_rounding"] = "up_within_slippage_cap"
	explanation.Attributes["first_executable_at"] = input.Sizing.FirstExecutableAt.Format(time.RFC3339Nano)
	identifier := decisionID(input, latest, input.HigherCandles[len(input.HigherCandles)-1])
	return Candidate{DecisionID: identifier, DecisionLogicalTime: input.LogicalTime, Instrument: input.Instrument,
		Side: domain.SideBuy, Quantity: quantity, LimitPrice: limit, Notional: notional,
		ExpiresAt:  input.LogicalTime + uint64(evaluator.configuration.CandidateLifetime),
		ReasonCode: ReasonEntryAccepted, Explanation: explanation}, ""
}

func (evaluator *Evaluator) exitCandidate(input Input, latest exchangecontracts.Candle,
	reason string, explanation Explanation) (Candidate, error) {
	if !validExecutableObservation(input, latest) {
		return Candidate{}, strategyError(ReasonNoPostLatencyPrice)
	}
	if input.Sizing.InstrumentMetadata.Instrument != input.Instrument || input.Sizing.InstrumentMetadata.Version == 0 {
		return Candidate{}, strategyError(ReasonInvalidSizing)
	}
	zero, _ := domain.ParseQuantity("0")
	if input.Position.Quantity.Compare(zero) <= 0 {
		return Candidate{}, strategyError(ReasonInvalidSizing)
	}
	owned, err := domain.ParseBalance(input.Position.Quantity.String())
	if err != nil {
		return Candidate{}, err
	}
	quantity, err := domain.RoundSellQuantity(input.Position.Quantity, owned,
		input.Sizing.InstrumentMetadata.QuantityStep)
	if err != nil || quantity.Compare(input.Sizing.InstrumentMetadata.MinimumQuantity) < 0 {
		return Candidate{}, strategyError(ReasonMinimumFilter)
	}
	limit, err := domain.RoundMarketableLimitPrice(domain.SideSell, input.Sizing.FirstExecutablePrice,
		input.Sizing.InstrumentMetadata.PriceTick)
	if err != nil {
		return Candidate{}, err
	}
	notional, err := domain.CalculateNotional(limit, quantity, 18)
	if err != nil || notional.Compare(input.Sizing.InstrumentMetadata.MinimumNotional) < 0 {
		return Candidate{}, strategyError(ReasonMinimumFilter)
	}
	explanation.Attributes["exit_reference"] = input.Sizing.FirstExecutablePrice.String()
	explanation.Attributes["first_executable_at"] = input.Sizing.FirstExecutableAt.Format(time.RFC3339Nano)
	explanation.Attributes["sell_price_rounding"] = "down"
	return Candidate{DecisionID: decisionID(input, latest, input.HigherCandles[len(input.HigherCandles)-1]),
		DecisionLogicalTime: input.LogicalTime, Instrument: input.Instrument, Side: domain.SideSell,
		Quantity: quantity, LimitPrice: limit, Notional: notional,
		ExpiresAt:  input.LogicalTime + uint64(evaluator.configuration.OrderValidity),
		ReasonCode: reason, Explanation: explanation}, nil
}

func availableCash(input Input) (decimal, error) {
	cash, err := parseDecimal(input.Sizing.AvailableCash.String())
	if err != nil {
		return decimal{}, err
	}
	reserve, err := parseDecimal(input.Sizing.MinimumReserve.String())
	if err != nil || cash.compare(reserve) < 0 {
		return decimal{}, strategyError(ReasonRiskClipped)
	}
	return cash.subtract(reserve)
}
