package postgres

import (
	"testing"

	"axiom/internal/api/generated"
	"axiom/internal/sandbox"
)

func TestResolveSandboxQualificationSandboxStrategySelectionUsesSemanticTopology(t *testing.T) {
	base := generated.SandboxStrategySessionCreateRequest{
		Instrument: generated.SandboxStrategySessionCreateRequestInstrumentBTCUSDT,
		Preset:     generated.SandboxStrategySessionCreateRequestPresetLatestQualifiedInputs,
		Reason:     "prepare bounded strategy session",
	}
	trend := base
	trend.StrategyId = generated.SandboxStrategySessionCreateRequestStrategyIdTrendFollowing
	trend.Exchanges = []generated.SandboxExchange{generated.SandboxExchangeBinance}
	selection, err := resolveSandboxQualificationSandboxStrategySelection(trend)
	if err != nil || selection.strategy != sandbox.StrategyTrend ||
		selection.version != "trend-following@1.0.0" ||
		len(selection.exchanges) != 1 || selection.exchanges[0] != sandbox.ExchangeBinance {
		t.Fatalf("trend selection=%#v error=%v", selection, err)
	}

	cross := base
	cross.StrategyId = generated.SandboxStrategySessionCreateRequestStrategyIdCrossExchangeArbitrage
	cross.Exchanges = []generated.SandboxExchange{generated.SandboxExchangeBybit, generated.SandboxExchangeBinance}
	selection, err = resolveSandboxQualificationSandboxStrategySelection(cross)
	if err != nil || selection.strategy != sandbox.StrategyCrossExchangeArbitrage ||
		len(selection.exchanges) != 2 || selection.exchanges[0] != sandbox.ExchangeBinance ||
		selection.exchanges[1] != sandbox.ExchangeBybit {
		t.Fatalf("cross selection=%#v error=%v", selection, err)
	}
	cross.Exchanges = []generated.SandboxExchange{generated.SandboxExchangeBinance}
	if _, err = resolveSandboxQualificationSandboxStrategySelection(cross); err == nil {
		t.Fatal("partial cross-exchange session accepted")
	}
}
