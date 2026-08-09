package postgres

import (
	"strings"
	"testing"

	"axiom/internal/api/generated"
)

func TestOwnerRunSelectionAcceptsOnlyTheSemanticPreset(t *testing.T) {
	request := generated.RunCreateRequest{
		StrategyId: "trend-following", StrategyVersion: "trend-following@1.0.0",
		Mode: generated.RunCreateRequestModeBacktest, Exchanges: []generated.RunCreateRequestExchanges{generated.RunCreateRequestExchangesBinance},
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

func TestOwnerSandboxRunInstrumentAcceptsOnlyReviewedSpotSymbols(t *testing.T) {
	for input, expected := range map[string]generated.SandboxStrategySessionCreateRequestInstrument{
		"BTC/USDT": generated.SandboxStrategySessionCreateRequestInstrumentBTCUSDT,
		"ETH/USDT": generated.SandboxStrategySessionCreateRequestInstrumentETHUSDT,
	} {
		value, ok := ownerSandboxRunInstrument(input)
		if !ok || value != expected {
			t.Fatalf("sandbox instrument %q=%q valid=%t", input, value, ok)
		}
	}
	if _, ok := ownerSandboxRunInstrument("BTC/USD"); ok {
		t.Fatal("unsupported sandbox instrument accepted")
	}
}

func TestOwnerOfflineStrategyMapsOnlyInstalledSemanticRuntimes(t *testing.T) {
	for _, item := range []struct {
		id, version, storage string
	}{
		{"trend-following", "trend-following@1.0.0", "trend-following-1-0-0"},
		{"mean-reversion", "mean-reversion@1.0.0", "mean-reversion-1-0-0"},
		{"triangular-arbitrage", "triangular-arbitrage@1.0.0", "triangular-arbitrage-1-0-0"},
		{"cross-exchange-arbitrage", "cross-exchange-arbitrage@1.0.0", "cross-exchange-arbitrage-1-0-0"},
		{"triangular-arbitrage", "triangular-arbitrage@1.0.0", "triangular-arbitrage-1-0-0"},
		{"triangular-arbitrage", "triangular-arbitrage@1.0.0", "triangular-arbitrage-1-0-0"},
		{"cross-exchange-arbitrage", "cross-exchange-arbitrage@1.0.0", "cross-exchange-arbitrage-1-0-0"},
		{"inventory-rebalancing", "inventory-rebalancing@1.0.0", "inventory-rebalancing-1-0-0"},
	} {
		runtime, ok := ownerOfflineStrategy(item.id, item.version)
		if !ok || runtime.storageID != item.storage || runtime.version == "" {
			t.Fatalf("runtime id=%s version=%s result=%+v installed=%t", item.id, item.version, runtime, ok)
		}
	}
}

func TestOwnerShadowStrategyMapsOnlyInstalledPublicDataRuntimes(t *testing.T) {
	for _, item := range []struct {
		id, version, storage string
	}{
		{"trend-following", "trend-following@1.0.0", "trend-following-1-0-0"},
		{"mean-reversion", "mean-reversion@1.0.0", "mean-reversion-1-0-0"},
	} {
		runtime, ok := ownerShadowStrategy(item.id, item.version)
		if !ok || runtime.storageID != item.storage || runtime.version == "" {
			t.Fatalf("shadow runtime id=%s version=%s result=%+v installed=%t", item.id, item.version, runtime, ok)
		}
	}
	if _, ok := ownerShadowStrategy("inventory-rebalancing", "inventory-rebalancing@1.0.0"); ok {
		t.Fatal("advisory strategy was mapped to an order-capable shadow runtime")
	}
}
