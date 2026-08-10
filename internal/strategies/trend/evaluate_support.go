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

// feeInclusiveEntryAmount keeps candidate sizing aligned with the allocator's
// quote-asset reservation, which includes the entry fee under the same hard
// notional ceiling.
func feeInclusiveEntryAmount(amount decimal, rateText string) (decimal, error) {
	rate, err := parseDecimal(rateText)
	if err != nil || rate.value.Sign() < 0 {
		return decimal{}, trendError(ReasonInvalidSizing)
	}
	fee, err := amount.multiply(rate, apd.RoundCeiling)
	if err != nil {
		return decimal{}, err
	}
	return amount.add(fee)
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
