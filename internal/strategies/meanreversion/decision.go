package meanreversion

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
)

func (evaluator *Evaluator) explanation(input Input, latest, higher exchangecontracts.Candle,
	indicators indicatorSnapshot) Explanation {
	return Explanation{Evidence: input.Evidence, PrimarySignalHash: latest.RawPayloadHash,
		HigherSignalHash: higher.RawPayloadHash, PrimarySignalClose: latest.CloseTime,
		HigherSignalClose: higher.CloseTime, RollingMean: indicators.mean, PopulationStdDev: indicators.deviation,
		ZScore: indicators.zscore.stringValue(), ADX14: indicators.adx.stringValue(), HigherEMA200: indicators.ema,
		EMADeclineFraction: indicators.decline.stringValue(), Regime: indicators.regime, ATR14: indicators.atr,
		PriceAtProtectiveZ: indicators.protectivePrice, Attributes: map[string]string{
			"strategy_version": evaluator.configuration.Version, "strategy_hash": input.Evidence.StrategyHash,
			"configuration_hash": evaluator.configuration.Hash, "indicator_scale": "18",
			"indicator_rounding": "half_even", "ambiguity_policy": "adverse_conservative",
		}}
}

func (evaluator *Evaluator) rejection(input Input, primary, higher []exchangecontracts.Candle, reason string) Decision {
	var latest, higherLatest exchangecontracts.Candle
	if len(primary) > 0 {
		latest = primary[len(primary)-1]
	} else if len(input.PrimaryCandles) > 0 {
		latest = input.PrimaryCandles[len(input.PrimaryCandles)-1]
	}
	if len(higher) > 0 {
		higherLatest = higher[len(higher)-1]
	} else if len(input.HigherCandles) > 0 {
		higherLatest = input.HigherCandles[len(input.HigherCandles)-1]
	}
	explanation := Explanation{ReasonCode: reason, Evidence: input.Evidence,
		PrimarySignalHash: latest.RawPayloadHash, HigherSignalHash: higherLatest.RawPayloadHash,
		PrimarySignalClose: latest.CloseTime, HigherSignalClose: higherLatest.CloseTime,
		Attributes: map[string]string{"strategy_version": evaluator.configuration.Version,
			"configuration_hash": evaluator.configuration.Hash}}
	return evaluator.decision(input, latest, higherLatest, ActionNone, reason, nil, explanation, 0)
}

func (evaluator *Evaluator) decision(input Input, latest, higher exchangecontracts.Candle, action Action,
	reason string, candidate *Candidate, explanation Explanation, cooldown uint64) Decision {
	explanation.ReasonCode = reason
	identifier := decisionID(input, latest, higher)
	if candidate != nil {
		candidate.DecisionID = identifier
		candidate.Explanation.ReasonCode = reason
	}
	return Decision{ID: identifier, Ordinal: input.Ordinal, Action: action, ReasonCode: reason,
		Candidate: candidate, Explanation: explanation, CooldownStart: cooldown}
}

func decisionID(input Input, latest, higher exchangecontracts.Candle) domain.DecisionID {
	identity := struct {
		Ordinal, LogicalTime     uint64
		Instrument, PrimaryOpen  string
		PrimaryHash, HigherOpen  string
		HigherHash, CoherentView string
		Strategy, Configuration  string
	}{input.Ordinal, input.LogicalTime, input.Instrument.Symbol(), latest.OpenTime.UTC().Format(time.RFC3339Nano),
		latest.RawPayloadHash, higher.OpenTime.UTC().Format(time.RFC3339Nano), higher.RawPayloadHash,
		input.Evidence.CoherentViewID, input.Evidence.StrategyVersion, input.Evidence.ConfigurationHash}
	canonical, _ := json.Marshal(identity)
	digest := sha256.Sum256(canonical)
	identifier, _ := domain.NewDecisionID("mean-reversion-" + hex.EncodeToString(digest[:12]))
	return identifier
}

func validEvidence(evidence InputEvidence, configuration Configuration) bool {
	return evidence.PrimaryCandleViewID != "" && evidence.PrimaryCandleViewRevision > 0 &&
		evidence.HigherCandleViewID != "" && evidence.HigherCandleViewRevision > 0 &&
		evidence.MarketViewID != "" && evidence.MarketViewRevision > 0 && validHash(evidence.CoherentViewID) &&
		evidence.CoherentVersionVectorHash == evidence.CoherentViewID && evidence.InstrumentMetadataID != "" &&
		evidence.AssetEligibilityVersion > 0 && evidence.ConfigurationSnapshotID != "" &&
		evidence.ConfigurationVersion == "axiom.config.v1b.2" && evidence.ConfigurationHash == configuration.Hash &&
		evidence.StrategyVersion == configuration.Version && validHash(evidence.StrategyHash) &&
		evidence.PortfolioRevision > 0 && evidence.PositionRevision > 0 && evidence.RiskPolicyID != "" && evidence.RiskPolicyVersion > 0 &&
		validHash(evidence.RiskPolicyHash) && evidence.FeeModelID != "" && evidence.LatencyModelID != "" &&
		evidence.FillModelID != "" && evidence.SlippageModelID != "" && evidence.GapModelID != "" &&
		evidence.CorrelationModelID != "" && evidence.CorrelationID != "" && evidence.CausationID != ""
}

func validHash(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && value == hex.EncodeToString(decoded)
}
