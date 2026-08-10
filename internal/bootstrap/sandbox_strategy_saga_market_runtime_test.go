package bootstrap

import (
	"strings"
	"testing"
	"time"

	"axiom/internal/domain"
	"axiom/internal/exchanges/binance"
	"axiom/internal/exchanges/bybit"
)

func TestSandboxSagaRuleProjectionUsesExactFiltersAndReviewedFeeModel(t *testing.T) {
	now := time.Date(2026, 8, 9, 20, 0, 0, 0, time.UTC)
	instrument, err := domain.NewSpotInstrument("BTC", "USDT")
	if err != nil {
		t.Fatal(err)
	}
	tick, _ := domain.ParsePrice("0.01")
	step, _ := domain.ParseQuantity("0.0001")
	minimum, _ := domain.ParseQuantity("0.0001")
	maximum, _ := domain.ParseQuantity("100")
	minimumNotional, _ := domain.ParseNotional("1")
	fee, _ := domain.ParseRate("0.001")
	hash := strings.Repeat("a", 64)
	binanceRules, err := binanceSandboxSagaRules([]binance.SandboxInstrumentRules{{
		Instrument: instrument, PriceTick: tick, QuantityStep: step,
		MinimumQuantity: minimum, MaximumQuantity: maximum,
		MinimumNotional: minimumNotional, ObservedAt: now, SourceHash: hash,
	}}, "fixed-bps-v1", fee)
	if err != nil || len(binanceRules) != 1 {
		t.Fatalf("rules=%#v error=%v", binanceRules, err)
	}
	bybitRules, err := bybitSandboxSagaRules([]bybit.DemoInstrumentRules{{
		Instrument: instrument, PriceTick: tick, QuantityStep: step,
		MinimumQuantity: minimum, MaximumQuantity: maximum,
		MinimumOrderAmount: minimumNotional, ObservedAt: now, SourceHash: hash,
	}}, "fixed-bps-v1", fee)
	if err != nil || len(bybitRules) != 1 {
		t.Fatalf("rules=%#v error=%v", bybitRules, err)
	}
	for _, rules := range append(binanceRules, bybitRules...) {
		if rules.Metadata.Instrument != instrument || rules.Metadata.PriceTick != tick ||
			rules.Metadata.QuantityStep != step || rules.Metadata.MinimumQuantity != minimum ||
			rules.Metadata.MinimumNotional != minimumNotional || rules.MaximumQuantity != maximum ||
			rules.Metadata.Version == 0 || !rules.Metadata.EffectiveAt.Equal(now) ||
			rules.Fee.Version != "fixed-bps-v1" || rules.Fee.Rate != fee ||
			rules.Fee.Asset != instrument.Quote || !rules.Active || !rules.ObservedAt.Equal(now) {
			t.Fatalf("projected rules=%#v", rules)
		}
	}
}

func TestSandboxSagaSourceVersionRejectsUnboundEvidence(t *testing.T) {
	if _, err := sandboxSagaSourceVersion("not-a-hash"); err == nil {
		t.Fatal("invalid source evidence produced a metadata version")
	}
}
