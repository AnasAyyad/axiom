package bootstrap

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
	runtimecore "axiom/internal/runtime"
	"axiom/internal/sandbox"
	"axiom/internal/strategies/arbitrage"
	"axiom/internal/strategies/triangular"
)

func validSandboxSagaInstrumentRules(
	rules arbitrage.InstrumentRules,
	key runtimecore.MarketKey,
	now time.Time,
) bool {
	zeroQuantity, quantityErr := domain.ParseQuantity("0")
	zeroRate, rateErr := domain.ParseRate("0")
	_, assetErr := domain.ParseAssetSymbol(string(rules.Fee.Asset))
	return quantityErr == nil && rateErr == nil && assetErr == nil && rules.Active &&
		rules.Exchange == key.Exchange && rules.Metadata.Instrument == key.Instrument &&
		rules.Metadata.Validate() == nil && !rules.Metadata.EffectiveAt.After(now) &&
		rules.MaximumQuantity.Compare(zeroQuantity) > 0 && rules.Fee.Version != "" &&
		rules.Fee.Rate.Compare(zeroRate) >= 0 && !rules.ObservedAt.IsZero() &&
		rules.ObservedAt.Location() == time.UTC && !rules.ObservedAt.After(now)
}

func sandboxSagaInstrument(symbol string) (domain.Instrument, error) {
	switch symbol {
	case "BTCUSDT":
		return domain.NewSpotInstrument("BTC", "USDT")
	case "ETHUSDT":
		return domain.NewSpotInstrument("ETH", "USDT")
	case "ETHBTC":
		return domain.NewSpotInstrument("ETH", "BTC")
	default:
		return domain.Instrument{}, fmt.Errorf("sandbox_saga_instrument_invalid")
	}
}

func sandboxSagaMarketEvidenceHash(identity string, rules []arbitrage.InstrumentRules) string {
	encoded, _ := json.Marshal(struct {
		Identity string                      `json:"coherent_view_id"`
		Rules    []arbitrage.InstrumentRules `json:"rules"`
	}{Identity: identity, Rules: rules})
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func sandboxSagaRiskMarket(
	markets []triangular.MarketInput,
	work sandbox.StrategySessionWork,
	trigger runtimecore.AsOfTrigger,
) (sandbox.StrategyMarketInput, error) {
	if work.ValidAt(trigger.UTC) != nil || trigger.MonotonicNanos == 0 || trigger.IngestOrdinal == 0 {
		return sandbox.StrategyMarketInput{}, fmt.Errorf("sandbox_saga_risk_market_invalid")
	}
	for _, market := range markets {
		if string(market.Snapshot.Exchange) != string(work.Account.Exchange) ||
			market.Snapshot.Instrument.Symbol() != work.Instrument ||
			market.Observation.Validate() != nil ||
			!validSandboxSagaInstrumentRules(market.Rules, runtimecore.MarketKey{
				Exchange: string(work.Account.Exchange), Instrument: market.Snapshot.Instrument,
			}, trigger.UTC) {
			continue
		}
		encoded, err := json.Marshal(market.Rules)
		if err != nil {
			return sandbox.StrategyMarketInput{}, fmt.Errorf("sandbox_saga_risk_market_invalid")
		}
		digest := sha256.Sum256(encoded)
		metadataHash := hex.EncodeToString(digest[:])
		result := sandbox.StrategyMarketInput{Instrument: market.Snapshot.Instrument,
			Metadata: exchangecontracts.InstrumentRecord{Exchange: market.Snapshot.Exchange,
				NativeSymbol: market.Snapshot.Instrument.Symbol(), NativeStatus: "Trading",
				Metadata: market.Rules.Metadata, MaximumQuantity: market.Rules.MaximumQuantity,
				RawPayloadHash: metadataHash},
			Book: market.Snapshot, ObservedAt: domain.EventTime{UTC: trigger.UTC,
				Sequence: trigger.IngestOrdinal}}
		if sandbox.StrategyMarketEvidenceHash(result) == "" {
			return sandbox.StrategyMarketInput{}, fmt.Errorf("sandbox_saga_risk_market_invalid")
		}
		return result, nil
	}
	return sandbox.StrategyMarketInput{}, fmt.Errorf("sandbox_saga_risk_market_unavailable")
}
