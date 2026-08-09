package bybit

import (
	"testing"
	"time"
)

func TestSandboxAdapterStrategyInstrumentRulesAreSortedDefensiveValues(t *testing.T) {
	now := time.Date(2026, 8, 9, 20, 30, 0, 0, time.UTC)
	adapter := &SandboxAdapter{rules: demoRules(t, now)}
	first, err := adapter.StrategyInstrumentRules()
	if err != nil || len(first) != 3 || first[0].Instrument.Symbol() != "BTCUSDT" ||
		first[1].Instrument.Symbol() != "ETHBTC" || first[2].Instrument.Symbol() != "ETHUSDT" {
		t.Fatalf("rules=%#v error=%v", first, err)
	}
	first[0].SourceHash = "changed"
	second, err := adapter.StrategyInstrumentRules()
	if err != nil || second[0].SourceHash == "changed" {
		t.Fatal("caller mutation escaped the sanitized rules copy")
	}
}
