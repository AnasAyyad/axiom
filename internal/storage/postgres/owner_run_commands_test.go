package postgres

import (
	"strings"
	"testing"

	"axiom/internal/api/generated"
)

func TestOwnerRunSelectionAcceptsOnlyTheSemanticPreset(t *testing.T) {
	request := generated.RunCreateRequest{
		StrategyId: "trend-following", StrategyVersion: "trend-following@1.0.0",
		Mode: generated.RunCreateRequestModeBacktest, Exchanges: []generated.RunCreateRequestExchanges{generated.Binance},
		Instrument: "BTC/USDT", Preset: generated.LatestQualifiedInputs,
	}
	selection, err := ownerRunSelection(request)
	if err != nil || selection.StrategyID != "trend-following" || selection.Instrument != "BTC/USDT" {
		t.Fatalf("selection=%+v err=%v", selection, err)
	}
	request.Preset = "client-supplied-identifier"
	if _, err = ownerRunSelection(request); err == nil {
		t.Fatal("unapproved preset accepted")
	}
}

func TestOwnerRunInputHelpersRejectUnsafeInstrumentAndKeepSeedOpaque(t *testing.T) {
	if _, _, ok := ownerRunInstrument("BTC/USDT/other"); ok {
		t.Fatal("ambiguous instrument accepted")
	}
	seed, err := ownerRunSeed()
	if err != nil || len(seed) != 64 || strings.ToLower(seed) != seed {
		t.Fatalf("seed=%q err=%v", seed, err)
	}
}
