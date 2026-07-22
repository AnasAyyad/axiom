package meanreversion

import (
	"time"

	"github.com/cockroachdb/apd/v3"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
)

// Evaluator owns immutable B3 rules and performs no external side effect.
type Evaluator struct{ configuration Configuration }

// NewEvaluator constructs one pure deterministic evaluator.
func NewEvaluator(configuration Configuration) (*Evaluator, error) {
	if configuration.Version != "mean-reversion.v1b.1" || configuration.Hash == "" ||
		configuration.PrimaryTimeframe != "1h" || configuration.HigherTimeframe != "4h" ||
		configuration.ZScorePeriod < 2 || configuration.ADXPeriod < 2 || configuration.EMARegimePeriod < 2 ||
		configuration.EMADeclineLookback < 1 || configuration.ATRPeriod < 1 || configuration.MaximumPositions != 1 {
		return nil, strategyError(ReasonInvalidConfiguration)
	}
	return &Evaluator{configuration: configuration}, nil
}

type indicatorSnapshot struct {
	mean, deviation, atr, ema, protectivePrice domain.Price
	zscore, adx, decline                       decimal
	strongDecline                              bool
	regime                                     string
}

// Evaluate returns one deterministic accepted change or rejection record.
func (evaluator *Evaluator) Evaluate(input Input) (Decision, error) {
	if !validEvidence(input.Evidence, evaluator.configuration) || input.Ordinal == 0 || input.LogicalTime == 0 ||
		input.Now.Location() != time.UTC || !validPosition(input.Position) {
		return Decision{}, strategyError(ReasonInvalidConfiguration)
	}
	primary, higher, rejection := admitCandles(input, evaluator.configuration)
	if rejection != "" {
		return evaluator.rejection(input, nil, nil, rejection), nil
	}
	indicators, indicatorErr := evaluator.indicators(primary, higher)
	if indicatorErr != nil {
		var failure Error
		if !asStrategyError(indicatorErr, &failure) {
			return Decision{}, indicatorErr
		}
		return evaluator.rejection(input, primary, higher, failure.Code), nil
	}
	explanation := evaluator.explanation(input, primary[len(primary)-1], higher[len(higher)-1], indicators)
	if input.Position.Open {
		return evaluator.evaluateExit(input, primary[len(primary)-1], indicators, explanation), nil
	}
	if input.Position.CooldownRemaining > 0 {
		return evaluator.decision(input, primary[len(primary)-1], higher[len(higher)-1], ActionNone,
			ReasonCooldown, nil, explanation, 0), nil
	}
	if reason := evaluator.entryAdmission(input, indicators); reason != "" {
		return evaluator.decision(input, primary[len(primary)-1], higher[len(higher)-1], ActionNone,
			reason, nil, explanation, 0), nil
	}
	candidate, reason := evaluator.sizeEntry(input, primary[len(primary)-1], indicators, explanation)
	if reason != "" {
		return evaluator.decision(input, primary[len(primary)-1], higher[len(higher)-1], ActionNone,
			reason, nil, explanation, 0), nil
	}
	return evaluator.decision(input, primary[len(primary)-1], higher[len(higher)-1], ActionEntry,
		ReasonEntryAccepted, &candidate, candidate.Explanation, 0), nil
}

func asStrategyError(err error, target *Error) bool {
	typed, ok := err.(Error)
	if ok {
		*target = typed
	}
	return ok
}

func (evaluator *Evaluator) indicators(primary, higher []exchangecontracts.Candle) (indicatorSnapshot, error) {
	primaryCloses := make([]domain.Price, len(primary))
	for index := range primary {
		primaryCloses[index] = primary[index].Close
	}
	mean, deviation, zscoreText, err := RollingZScore(primaryCloses, evaluator.configuration.ZScorePeriod)
	if err != nil {
		return indicatorSnapshot{}, err
	}
	zscore, _ := parseDecimal(zscoreText)
	adxText, err := ADX(primary, evaluator.configuration.ADXPeriod)
	if err != nil {
		return indicatorSnapshot{}, err
	}
	adx, _ := parseDecimal(adxText)
	atr, err := ATR(primary, evaluator.configuration.ATRPeriod)
	if err != nil {
		return indicatorSnapshot{}, err
	}
	higherCloses := make([]domain.Price, len(higher))
	for index := range higher {
		higherCloses[index] = higher[index].Close
	}
	ema, declineText, strong, err := EMADecline(higherCloses, evaluator.configuration.EMARegimePeriod,
		evaluator.configuration.EMADeclineLookback, evaluator.configuration.EMADeclineThreshold.stringValue())
	if err != nil {
		return indicatorSnapshot{}, err
	}
	decline, _ := parseDecimal(declineText)
	meanValue, _ := parseDecimal(mean.String())
	deviationValue, _ := parseDecimal(deviation.String())
	offset, err := deviationValue.multiply(evaluator.configuration.ProtectiveExitZScore, apd.RoundHalfEven)
	if err != nil {
		return indicatorSnapshot{}, err
	}
	protective, err := meanValue.add(offset)
	if err != nil || protective.value.Sign() <= 0 {
		return indicatorSnapshot{}, strategyError(ReasonInvalidSizing)
	}
	protectivePrice, err := domain.ParsePrice(protective.stringValue())
	if err != nil {
		return indicatorSnapshot{}, err
	}
	regime := "range_or_constructive"
	if higher[len(higher)-1].Close.Compare(ema) < 0 && strong {
		regime = "dangerous_decline"
	}
	return indicatorSnapshot{mean: mean, deviation: deviation, zscore: zscore, adx: adx, atr: atr,
		ema: ema, decline: decline, strongDecline: strong, regime: regime, protectivePrice: protectivePrice}, nil
}

func (evaluator *Evaluator) entryAdmission(input Input, indicators indicatorSnapshot) string {
	if !input.MarketHealthy || input.BookAge < 0 || input.BookAge >= evaluator.configuration.MaximumBookAge {
		return ReasonUnhealthyMarket
	}
	if !input.MarketDataQualityPass {
		return ReasonMarketQuality
	}
	if input.ExchangeRiskPaused {
		return ReasonRiskPause
	}
	spread, err := parseDecimal(input.Spread.String())
	if err != nil || spread.compare(evaluator.configuration.MaximumSpread) > 0 {
		return ReasonSpread
	}
	if indicators.adx.compare(evaluator.configuration.ADXThreshold) >= 0 {
		return ReasonADX
	}
	if indicators.regime == "dangerous_decline" {
		return ReasonDangerousRegime
	}
	if indicators.zscore.compare(evaluator.configuration.EntryZScore) > 0 {
		return ReasonEntryThreshold
	}
	return ""
}

func (evaluator *Evaluator) evaluateExit(input Input, latest exchangecontracts.Candle,
	indicators indicatorSnapshot, explanation Explanation) Decision {
	reason := ""
	// Protective outcomes win first. This is the adverse/conservative ordering
	// when an intrabar low hits the stop but the completed close later normalizes.
	if latest.Low.Compare(input.Position.InitialStop) <= 0 {
		reason = ReasonExitATRStop
	} else if indicators.zscore.compare(evaluator.configuration.ProtectiveExitZScore) <= 0 {
		reason = ReasonExitProtectiveZScore
	} else if indicators.zscore.compare(evaluator.configuration.NormalExitZScore) >= 0 {
		reason = ReasonExitNormalZScore
	} else if input.Position.HeldCandles >= evaluator.configuration.MaximumHoldingCandles {
		reason = ReasonExitMaximumHolding
	}
	if reason == "" {
		return evaluator.decision(input, latest, input.HigherCandles[len(input.HigherCandles)-1], ActionNone,
			ReasonHoldPosition, nil, explanation, 0)
	}
	candidate, err := evaluator.exitCandidate(input, latest, reason, explanation)
	if err != nil {
		failureReason := ReasonInvalidSizing
		var failure Error
		if asStrategyError(err, &failure) {
			failureReason = failure.Code
		}
		return evaluator.decision(input, latest, input.HigherCandles[len(input.HigherCandles)-1], ActionNone,
			failureReason, nil, explanation, 0)
	}
	cooldown := uint64(0)
	if reason == ReasonExitATRStop || reason == ReasonExitProtectiveZScore {
		cooldown = evaluator.configuration.CooldownCandles
	}
	return evaluator.decision(input, latest, input.HigherCandles[len(input.HigherCandles)-1], ActionExit,
		reason, &candidate, candidate.Explanation, cooldown)
}
