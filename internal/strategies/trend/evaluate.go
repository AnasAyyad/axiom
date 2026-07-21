package trend

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/cockroachdb/apd/v3"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
)

// Evaluator owns immutable Trend rules and performs no external side effect.
type Evaluator struct{ configuration Configuration }

// NewEvaluator constructs one evaluator from a validated immutable graph.
func NewEvaluator(configuration Configuration) (*Evaluator, error) {
	if configuration.Version == "" || configuration.Hash == "" || configuration.EMARegime < 2 ||
		configuration.EMAConfirmation < 1 || configuration.ATRPeriod < 1 || configuration.BreakoutLookback < 1 {
		return nil, trendError(ReasonInvalidConfiguration)
	}
	return &Evaluator{configuration: configuration}, nil
}

// Evaluate returns one deterministic accepted change or rejection record.
func (evaluator *Evaluator) Evaluate(input Input) (Decision, error) {
	if !validEvidence(input.Evidence, evaluator.configuration) || input.Ordinal == 0 || input.LogicalTime == 0 || input.Now.Location() != input.Now.UTC().Location() {
		return Decision{}, trendError(ReasonInvalidConfiguration)
	}
	candles, rejection := admitCandles(input, evaluator.configuration)
	if rejection != "" {
		return evaluator.rejection(input, nil, rejection), nil
	}
	latest := candles[len(candles)-1]
	if !input.MarketHealthy || input.BookAge < 0 || input.BookAge >= evaluator.configuration.MaximumBookAge {
		return evaluator.rejection(input, candles, ReasonUnhealthyMarket), nil
	}
	ema50, ema200, atr14, indicatorErr := indicators(candles, evaluator.configuration)
	if indicatorErr != nil {
		return evaluator.rejection(input, candles, ReasonWarmUp), nil
	}
	breakoutHigh := priorHigh(candles, evaluator.configuration.BreakoutLookback)
	explanation := evaluator.explanation(input, latest, ema50, ema200, atr14, breakoutHigh)
	if input.Position.Open {
		return evaluator.evaluateExit(input, latest, ema50, atr14, explanation)
	}
	if input.Position.CooldownRemaining > 0 {
		return evaluator.decision(input, latest, ActionNone, ReasonCooldown, nil, explanation, 0), nil
	}
	if latest.Close.Compare(ema200) <= 0 {
		return evaluator.decision(input, latest, ActionNone, ReasonRegimeFailed, nil, explanation, 0), nil
	}
	if ema50.Compare(ema200) <= 0 {
		return evaluator.decision(input, latest, ActionNone, ReasonConfirmationFailed, nil, explanation, 0), nil
	}
	if latest.Close.Compare(breakoutHigh) <= 0 {
		return evaluator.decision(input, latest, ActionNone, ReasonBreakoutFailed, nil, explanation, 0), nil
	}
	candidate, reason := evaluator.sizeEntry(input, latest, atr14, explanation)
	if reason != "" {
		return evaluator.decision(input, latest, ActionNone, reason, nil, explanation, 0), nil
	}
	return evaluator.decision(input, latest, ActionEntry, ReasonEntryAccepted, &candidate, candidate.Explanation, 0), nil
}

func (evaluator *Evaluator) evaluateExit(
	input Input,
	latest exchangecontracts.Candle,
	ema50 domain.Price,
	atr domain.Price,
	explanation Explanation,
) (Decision, error) {
	position, err := AdvancePosition(input.Position, latest.Close, atr, evaluator.configuration)
	if err != nil {
		return evaluator.decision(input, latest, ActionNone, ReasonInvalidSizing, nil, explanation, 0), nil
	}
	protective := position.InitialStop
	reason := ReasonExitInitialStop
	if position.TrailingStop.Compare(protective) > 0 {
		protective = position.TrailingStop
		reason = ReasonExitTrailingStop
	}
	triggered := latest.Low.Compare(protective) <= 0
	if !triggered && latest.Close.Compare(ema50) >= 0 {
		explanation.Attributes["next_trailing_stop"] = position.TrailingStop.String()
		explanation.Attributes["highest_favorable_close"] = position.HighestFavorableClose.String()
		return evaluator.decision(input, latest, ActionNone, ReasonExistingPosition, nil, explanation, 0), nil
	}
	if !triggered {
		reason = ReasonExitEMA50
	}
	candidate, candidateErr := evaluator.exitCandidate(input, latest, reason, explanation)
	if candidateErr != nil {
		return evaluator.decision(input, latest, ActionNone, ReasonInvalidSizing, nil, explanation, 0), nil
	}
	cooldown := uint64(0)
	if reason == ReasonExitInitialStop || reason == ReasonExitTrailingStop {
		cooldown = evaluator.configuration.CooldownCandles
	}
	return evaluator.decision(input, latest, ActionExit, reason, &candidate, candidate.Explanation, cooldown), nil
}

func (evaluator *Evaluator) sizeEntry(
	input Input,
	latest exchangecontracts.Candle,
	atr domain.Price,
	explanation Explanation,
) (Candidate, string) {
	if !input.Sizing.CentralRiskEligible || input.Sizing.FencingToken == 0 || input.Sizing.LiquidityDomain == "" ||
		input.Sizing.InstrumentMetadata.Instrument != input.Instrument || input.Sizing.InstrumentMetadata.Version == 0 {
		return Candidate{}, ReasonRiskClipped
	}
	calculation, reason := evaluator.calculateEntry(input, atr)
	if reason != "" {
		return Candidate{}, reason
	}
	return evaluator.buildEntryCandidate(input, latest, explanation, calculation)
}

type entryCalculation struct {
	entry, nominalStop, stressedExit, unitRisk decimal
	riskBudget, notionalCap, rawQuantity       decimal
}

func (evaluator *Evaluator) calculateEntry(input Input, atr domain.Price) (entryCalculation, string) {
	entry, nominalStop, stressedExit, unitRisk, err := evaluator.stressedUnitRisk(input, atr)
	if err != nil {
		return entryCalculation{}, ReasonInvalidSizing
	}
	riskBudget, notionalCap, rawQuantity, reason := evaluator.riskQuantity(input, entry, unitRisk)
	return entryCalculation{entry: entry, nominalStop: nominalStop, stressedExit: stressedExit, unitRisk: unitRisk,
		riskBudget: riskBudget, notionalCap: notionalCap, rawQuantity: rawQuantity}, reason
}

func (evaluator *Evaluator) stressedUnitRisk(input Input, atr domain.Price) (decimal, decimal, decimal, decimal, error) {
	entry, entryErr := parseDecimal(input.Sizing.EntryReference.String())
	atrValue, atrErr := parseDecimal(atr.String())
	if entryErr != nil || atrErr != nil || entry.value.Sign() <= 0 || atrValue.value.Sign() <= 0 {
		return decimal{}, decimal{}, decimal{}, decimal{}, trendError(ReasonInvalidSizing)
	}
	stopDistance, err := atrValue.multiply(evaluator.configuration.InitialStopMultiplier, apd.RoundHalfEven)
	if err != nil {
		return decimal{}, decimal{}, decimal{}, decimal{}, err
	}
	nominalStop, err := entry.subtract(stopDistance)
	if err != nil || nominalStop.value.Sign() <= 0 {
		return decimal{}, decimal{}, decimal{}, decimal{}, trendError(ReasonInvalidSizing)
	}
	gap, _ := parseDecimal(input.Sizing.GapAllowance.String())
	latency, _ := parseDecimal(input.Sizing.LatencyDeterioration.String())
	stressedExit, err := nominalStop.subtract(gap)
	if err == nil {
		stressedExit, err = stressedExit.subtract(latency)
	}
	if err != nil || stressedExit.value.Sign() <= 0 || stressedExit.compare(entry) >= 0 {
		return decimal{}, decimal{}, decimal{}, decimal{}, trendError(ReasonInvalidSizing)
	}
	entryFeeRate, _ := parseDecimal(input.Sizing.EntryFeeRate.String())
	exitFeeRate, _ := parseDecimal(input.Sizing.ExitFeeRate.String())
	entryFee, err := entry.multiply(entryFeeRate, apd.RoundCeiling)
	if err != nil {
		return decimal{}, decimal{}, decimal{}, decimal{}, err
	}
	exitFee, err := stressedExit.multiply(exitFeeRate, apd.RoundCeiling)
	if err != nil {
		return decimal{}, decimal{}, decimal{}, decimal{}, err
	}
	unitRisk, err := entry.subtract(stressedExit)
	if err == nil {
		unitRisk, err = unitRisk.add(entryFee)
	}
	if err == nil {
		unitRisk, err = unitRisk.add(exitFee)
	}
	if err != nil {
		return decimal{}, decimal{}, decimal{}, decimal{}, err
	}
	unitRisk, err = quantize(unitRisk, apd.RoundCeiling)
	if err != nil || unitRisk.value.Sign() <= 0 {
		return decimal{}, decimal{}, decimal{}, decimal{}, trendError(ReasonInvalidSizing)
	}
	return entry, nominalStop, stressedExit, unitRisk, nil
}

func (evaluator *Evaluator) riskQuantity(input Input, entry, unitRisk decimal) (decimal, decimal, decimal, string) {
	equity, _ := parseDecimal(input.Sizing.Equity.String())
	riskBudget, err := equity.multiply(evaluator.configuration.RiskBudget, apd.RoundFloor)
	if err != nil || riskBudget.value.Sign() <= 0 {
		return decimal{}, decimal{}, decimal{}, ReasonInvalidSizing
	}
	available, availableErr := availableCash(input)
	if availableErr != nil || available.value.Sign() <= 0 {
		return decimal{}, decimal{}, decimal{}, ReasonRiskClipped
	}
	notionalCap := minimum(evaluator.configuration.MaximumNotional, available)
	for _, limit := range input.Sizing.NotionalLimits {
		parsed, parseErr := parseDecimal(limit.String())
		if parseErr != nil || parsed.value.Sign() <= 0 {
			return decimal{}, decimal{}, decimal{}, ReasonRiskClipped
		}
		notionalCap = minimum(notionalCap, parsed)
	}
	rawQuantity, err := riskBudget.divide(unitRisk, apd.RoundFloor)
	if err != nil {
		return decimal{}, decimal{}, decimal{}, ReasonInvalidSizing
	}
	capQuantity, err := notionalCap.divide(entry, apd.RoundFloor)
	if err != nil {
		return decimal{}, decimal{}, decimal{}, ReasonInvalidSizing
	}
	return riskBudget, notionalCap, minimum(rawQuantity, capQuantity), ""
}

func (evaluator *Evaluator) buildEntryCandidate(input Input, latest exchangecontracts.Candle, explanation Explanation, calculation entryCalculation) (Candidate, string) {
	quantity, err := domain.ParseQuantity(calculation.rawQuantity.stringValue())
	if err != nil {
		return Candidate{}, ReasonInvalidSizing
	}
	quantity, err = domain.RoundBuyQuantity(quantity, input.Sizing.InstrumentMetadata.QuantityStep)
	zeroQuantity, _ := domain.ParseQuantity("0")
	if err != nil || quantity.Compare(zeroQuantity) <= 0 {
		return Candidate{}, ReasonInvalidSizing
	}
	limitPrice, err := domain.RoundMarketableLimitPrice(domain.SideBuy, input.Sizing.EntryReference, input.Sizing.InstrumentMetadata.PriceTick)
	if err != nil {
		return Candidate{}, ReasonInvalidSizing
	}
	slippage, _ := domain.ParsePercent(evaluator.configuration.MaximumSlippage.stringValue())
	maximumPrice, err := domain.PriceAtSlippage(input.Sizing.EntryReference, slippage, domain.SideBuy, 18)
	if err != nil || limitPrice.Compare(maximumPrice) > 0 {
		return Candidate{}, ReasonMinimumFilter
	}
	notional, err := domain.CalculateNotional(limitPrice, quantity, 18)
	if err != nil || quantity.Compare(input.Sizing.InstrumentMetadata.MinimumQuantity) < 0 ||
		notional.Compare(input.Sizing.InstrumentMetadata.MinimumNotional) < 0 {
		return Candidate{}, ReasonMinimumFilter
	}
	notionalValue, _ := parseDecimal(notional.String())
	if notionalValue.compare(calculation.notionalCap) > 0 {
		return Candidate{}, ReasonRiskClipped
	}
	riskMoney, _ := domain.ParseMoney(calculation.riskBudget.stringValue())
	unitRiskPrice, _ := domain.ParsePrice(calculation.unitRisk.stringValue())
	explanation.RiskBudget = riskMoney
	explanation.StressedUnitRisk = unitRiskPrice
	explanation.Attributes["nominal_stop"] = calculation.nominalStop.stringValue()
	explanation.Attributes["stressed_exit"] = calculation.stressedExit.stringValue()
	explanation.Attributes["quantity_rounding"] = "down"
	explanation.Attributes["buy_price_rounding"] = "up_within_slippage_cap"
	decisionID := decisionID(input, latest)
	return Candidate{DecisionID: decisionID, DecisionLogicalTime: input.LogicalTime, Instrument: input.Instrument, Side: domain.SideBuy,
		Quantity: quantity, LimitPrice: limitPrice, Notional: notional,
		ExpiresAt:  input.LogicalTime + uint64(evaluator.configuration.CandidateLifetime),
		ReasonCode: ReasonEntryAccepted, Explanation: explanation}, ""
}

func (evaluator *Evaluator) exitCandidate(input Input, latest exchangecontracts.Candle, reason string, explanation Explanation) (Candidate, error) {
	zero, _ := domain.ParseQuantity("0")
	if input.Position.Quantity.Compare(zero) <= 0 {
		return Candidate{}, trendError(ReasonInvalidSizing)
	}
	owned, err := domain.ParseBalance(input.Position.Quantity.String())
	if err != nil {
		return Candidate{}, err
	}
	quantity, err := domain.RoundSellQuantity(input.Position.Quantity, owned, input.Sizing.InstrumentMetadata.QuantityStep)
	if err != nil || quantity.Compare(input.Sizing.InstrumentMetadata.MinimumQuantity) < 0 {
		return Candidate{}, trendError(ReasonMinimumFilter)
	}
	reference := input.Sizing.FirstExecutablePrice
	limit, err := domain.RoundMarketableLimitPrice(domain.SideSell, reference, input.Sizing.InstrumentMetadata.PriceTick)
	if err != nil {
		return Candidate{}, err
	}
	notional, err := domain.CalculateNotional(limit, quantity, 18)
	if err != nil || notional.Compare(input.Sizing.InstrumentMetadata.MinimumNotional) < 0 {
		return Candidate{}, trendError(ReasonMinimumFilter)
	}
	explanation.Attributes["exit_reference"] = reference.String()
	explanation.Attributes["sell_price_rounding"] = "down"
	return Candidate{DecisionID: decisionID(input, latest), DecisionLogicalTime: input.LogicalTime, Instrument: input.Instrument, Side: domain.SideSell,
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
		return decimal{}, trendError(ReasonRiskClipped)
	}
	return cash.subtract(reserve)
}

func indicators(candles []exchangecontracts.Candle, configuration Configuration) (domain.Price, domain.Price, domain.Price, error) {
	closes := make([]domain.Price, len(candles))
	for index := range candles {
		closes[index] = candles[index].Close
	}
	ema50, err := EMA(closes, configuration.EMAConfirmation)
	if err != nil {
		return domain.Price{}, domain.Price{}, domain.Price{}, err
	}
	ema200, err := EMA(closes, configuration.EMARegime)
	if err != nil {
		return domain.Price{}, domain.Price{}, domain.Price{}, err
	}
	atr, err := ATR(candles, configuration.ATRPeriod)
	return ema50, ema200, atr, err
}

func priorHigh(candles []exchangecontracts.Candle, lookback int) domain.Price {
	start := len(candles) - 1 - lookback
	highest := candles[start].High
	for index := start + 1; index < len(candles)-1; index++ {
		if candles[index].High.Compare(highest) > 0 {
			highest = candles[index].High
		}
	}
	return highest
}

func (evaluator *Evaluator) explanation(input Input, latest exchangecontracts.Candle, ema50, ema200, atr, breakout domain.Price) Explanation {
	return Explanation{Evidence: input.Evidence, SignalCandleHash: latest.RawPayloadHash,
		SignalCandleClose: latest.CloseTime, EMA50: ema50, EMA200: ema200, ATR14: atr,
		BreakoutHigh: breakout, Attributes: map[string]string{
			"strategy_version":   evaluator.configuration.Version,
			"configuration_hash": evaluator.configuration.Hash,
			"indicator_scale":    "18", "indicator_rounding": "half_even",
		}}
}

func (evaluator *Evaluator) rejection(input Input, candles []exchangecontracts.Candle, reason string) Decision {
	var latest exchangecontracts.Candle
	if len(candles) > 0 {
		latest = candles[len(candles)-1]
	} else if len(input.Candles) > 0 {
		latest = input.Candles[len(input.Candles)-1]
	}
	explanation := Explanation{ReasonCode: reason, Evidence: input.Evidence,
		SignalCandleHash: latest.RawPayloadHash, SignalCandleClose: latest.CloseTime,
		Attributes: map[string]string{"strategy_version": evaluator.configuration.Version,
			"configuration_hash": evaluator.configuration.Hash}}
	return evaluator.decision(input, latest, ActionNone, reason, nil, explanation, 0)
}

func (evaluator *Evaluator) decision(input Input, latest exchangecontracts.Candle, action Action, reason string, candidate *Candidate, explanation Explanation, cooldown uint64) Decision {
	explanation.ReasonCode = reason
	identifier := decisionID(input, latest)
	if candidate != nil {
		candidate.DecisionID = identifier
		candidate.Explanation.ReasonCode = reason
	}
	return Decision{ID: identifier, Ordinal: input.Ordinal, Action: action, ReasonCode: reason,
		Candidate: candidate, Explanation: explanation, CooldownStart: cooldown}
}

func decisionID(input Input, latest exchangecontracts.Candle) domain.DecisionID {
	identity := struct {
		Ordinal       uint64
		Instrument    string
		OpenTime      string
		CandleHash    string
		Strategy      string
		Configuration string
	}{input.Ordinal, input.Instrument.Symbol(), latest.OpenTime.UTC().Format(time.RFC3339Nano), latest.RawPayloadHash,
		input.Evidence.StrategyVersion, input.Evidence.ConfigurationHash}
	canonical, _ := json.Marshal(identity)
	digest := sha256.Sum256(canonical)
	identifier, _ := domain.NewDecisionID("trend-" + hex.EncodeToString(digest[:12]))
	return identifier
}

func validEvidence(evidence InputEvidence, configuration Configuration) bool {
	return evidence.CandleViewID != "" && evidence.CandleViewRevision > 0 && evidence.MarketViewID != "" &&
		evidence.MarketViewRevision > 0 && evidence.InstrumentMetadataID != "" && evidence.AssetEligibilityVersion > 0 &&
		evidence.ConfigurationVersion != "" && evidence.ConfigurationHash == configuration.Hash &&
		evidence.StrategyVersion == configuration.Version && evidence.PortfolioRevision > 0 && evidence.PositionRevision > 0 &&
		evidence.FeeModelID != "" && evidence.LatencyModelID != "" && evidence.FillModelID != "" &&
		evidence.SlippageModelID != "" && evidence.GapModelID != "" && evidence.CorrelationID != "" && evidence.CausationID != ""
}
