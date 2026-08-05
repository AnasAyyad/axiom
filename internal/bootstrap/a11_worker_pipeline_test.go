package bootstrap

import (
	"strings"
	"testing"

	"axiom/internal/backtest"
	"axiom/internal/config"
	"axiom/internal/domain"
)

func TestOfflineStrategyRegistryComposesTheTrendProductionPipeline(t *testing.T) {
	runID, err := domain.NewRunID("backtest-a11-worker")
	if err != nil {
		t.Fatal(err)
	}
	configuration := config.DefaultConfiguration()
	processor, err := newOfflineOperationalProcessor(backtest.JobClaim{ID: "backtest-a11-worker",
		Configuration: configuration, Manifest: backtest.RunManifest{RunID: runID, Mode: "backtest",
			ConfigurationHash: strings.Repeat("a", 64), StrategyVersion: "trend.v1a.1", Seed: strings.Repeat("1", 64),
			Models: backtest.ModelNamespace{ID: "namespace-a11", MarketContext: "production-public",
				LiquidityDomain: "combined-a11", FeeDomain: configuration.Models.Fee,
				LatencyDomain: configuration.Models.Latency, FillDomain: "fill-v1"}}})
	if err != nil || processor == nil {
		t.Fatalf("operational worker pipeline = %#v %v", processor, err)
	}
}

func TestOfflineStrategyRegistryRejectsIncompleteConfiguration(t *testing.T) {
	configuration := config.DefaultConfiguration()
	configuration.Trend.Parameters = nil
	if _, err := newOfflineOperationalProcessor(backtest.JobClaim{Configuration: configuration}); err == nil {
		t.Fatal("incomplete durable configuration was accepted")
	}
}

func TestOfflineStrategyRegistrySelectsMeanReversionFromManifest(t *testing.T) {
	runID, err := domain.NewRunID("backtest-a11-mean-reversion-worker")
	if err != nil {
		t.Fatal(err)
	}
	configuration := config.DefaultV1BConfiguration()
	processor, err := newOfflineOperationalProcessor(backtest.JobClaim{ID: "backtest-a11-mean-reversion-worker",
		Configuration: configuration, Manifest: backtest.RunManifest{RunID: runID, Mode: "backtest",
			ConfigurationHash: strings.Repeat("a", 64), StrategyVersion: "mean-reversion.v1b.1", Seed: strings.Repeat("1", 64),
			Models: backtest.ModelNamespace{ID: "namespace-a11-mean-reversion", MarketContext: "production-public",
				LiquidityDomain: "combined-a11", FeeDomain: configuration.Models.Fee,
				LatencyDomain: configuration.Models.Latency, FillDomain: "fill-v1"}}})
	if err != nil || processor == nil {
		t.Fatalf("mean-reversion operational worker pipeline = %#v %v", processor, err)
	}
}

func TestInstalledOfflineStrategyRuntimesHaveUniqueSemanticAndManifestVersions(t *testing.T) {
	runtimes := installedOfflineStrategyRuntimes()
	if len(runtimes) != 2 {
		t.Fatalf("installed runtimes=%d", len(runtimes))
	}
	ids, semantic, manifests := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, runtime := range runtimes {
		if runtime.ID == "" || runtime.SemanticVersion == "" || runtime.ManifestVersion == "" ||
			runtime.NewProcessor == nil || ids[runtime.ID] || semantic[runtime.SemanticVersion] ||
			manifests[runtime.ManifestVersion] {
			t.Fatalf("invalid runtime=%+v", runtime)
		}
		ids[runtime.ID], semantic[runtime.SemanticVersion], manifests[runtime.ManifestVersion] = true, true, true
	}
}
