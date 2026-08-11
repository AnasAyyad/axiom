package bootstrap

import (
	"strings"
	"testing"

	"axiom/internal/backtest"
	"axiom/internal/config"
	"axiom/internal/domain"
)

func TestOfflineStrategyRegistryComposesTheTrendProductionPipeline(t *testing.T) {
	runID, err := domain.NewRunID("backtest-owner_console-worker")
	if err != nil {
		t.Fatal(err)
	}
	configuration := config.DefaultConfiguration()
	processor, err := newOfflineOperationalProcessor(backtest.JobClaim{ID: "backtest-owner_console-worker",
		Configuration: configuration, Manifest: backtest.RunManifest{RunID: runID, Mode: "backtest",
			ConfigurationHash: strings.Repeat("a", 64), StrategyVersion: "trend-following@1.0.0", Seed: strings.Repeat("1", 64),
			Models: backtest.ModelNamespace{ID: "namespace-owner_console", MarketContext: "production-public",
				LiquidityDomain: "combined-owner_console", FeeDomain: configuration.Models.Fee,
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
	runID, err := domain.NewRunID("backtest-owner_console-mean-reversion-worker")
	if err != nil {
		t.Fatal(err)
	}
	configuration := config.DefaultMultiStrategyConfiguration()
	processor, err := newOfflineOperationalProcessor(backtest.JobClaim{ID: "backtest-owner_console-mean-reversion-worker",
		Configuration: configuration, Manifest: backtest.RunManifest{RunID: runID, Mode: "backtest",
			ConfigurationHash: strings.Repeat("a", 64), StrategyVersion: "mean-reversion@1.0.0", Seed: strings.Repeat("1", 64),
			Models: backtest.ModelNamespace{ID: "namespace-owner_console-mean-reversion", MarketContext: "production-public",
				LiquidityDomain: "combined-owner_console", FeeDomain: configuration.Models.Fee,
				LatencyDomain: configuration.Models.Latency, FillDomain: "fill-v1"}}})
	if err != nil || processor == nil {
		t.Fatalf("mean-reversion operational worker pipeline = %#v %v", processor, err)
	}
}

func TestOfflineStrategyRegistryComposesRecordedMultilegPipelines(t *testing.T) {
	configuration := config.DefaultMultiStrategyConfiguration()
	for _, version := range []string{"triangular-arbitrage@1.0.0", "cross-exchange-arbitrage@1.0.0"} {
		identityVersion := strings.NewReplacer("@", "-", ".", "-").Replace(version)
		identity := "backtest-owner_console-" + identityVersion + "-worker"
		runID, err := domain.NewRunID(identity)
		if err != nil {
			t.Fatal(err)
		}
		processor, err := newOfflineOperationalProcessor(backtest.JobClaim{ID: identity,
			Configuration: configuration, Manifest: backtest.RunManifest{RunID: runID, Mode: "backtest",
				ConfigurationHash: strings.Repeat("a", 64), StrategyVersion: version, Seed: strings.Repeat("1", 64),
				Models: backtest.ModelNamespace{ID: "namespace-owner_console-" + identityVersion, MarketContext: "production-public",
					LiquidityDomain: "combined-owner_console", FeeDomain: configuration.Models.Fee,
					LatencyDomain: configuration.Models.Latency, FillDomain: "fill-v1"}}})
		if err != nil || processor == nil {
			t.Fatalf("multileg worker version=%s processor=%#v error=%v", version, processor, err)
		}
	}
}

func TestOfflineStrategyRegistryComposesInventoryAdvisoryPipeline(t *testing.T) {
	runID, err := domain.NewRunID("backtest-inventory-rebalancing-worker")
	if err != nil {
		t.Fatal(err)
	}
	configuration := config.DefaultMultiStrategyConfiguration()
	processor, err := newOfflineOperationalProcessor(backtest.JobClaim{ID: "backtest-inventory-rebalancing-worker",
		Configuration: configuration, Manifest: backtest.RunManifest{RunID: runID, Mode: "backtest",
			ConfigurationHash: strings.Repeat("a", 64), StrategyVersion: "inventory-rebalancing@1.0.0"}})
	if err != nil || processor == nil {
		t.Fatalf("inventory advisory worker pipeline = %#v %v", processor, err)
	}
}

func TestInstalledOfflineStrategyRuntimesHaveUniqueSemanticAndManifestVersions(t *testing.T) {
	runtimes := installedOfflineStrategyRuntimes()
	if len(runtimes) != 5 {
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

func TestStableEvaluationProcessorClaimIgnoresRepeatMemberIdentityWithoutMutatingClaim(t *testing.T) {
	originalRun, err := domain.NewRunID("evaluation-job-repeat-0")
	if err != nil {
		t.Fatal(err)
	}
	original := backtest.JobClaim{ID: "evaluation-job-repeat-0", Manifest: backtest.RunManifest{
		RunID: originalRun, Mode: "replay", StrategyVersion: "trend-following@1.0.0",
		Evaluation: &backtest.EvaluationRunIdentity{CampaignID: "evaluation-campaign-1",
			MemberID: "evaluation-member-repeat-0", ConfigurationKey: "trend-balanced-a",
			CapitalMicros: 2_000_000_000, CostStressBPS: 10_000}}}
	repeat := original
	repeat.ID = "evaluation-job-repeat-1"
	repeatIdentity := *original.Manifest.Evaluation
	repeatIdentity.MemberID = "evaluation-member-repeat-1"
	repeat.Manifest.Evaluation = &repeatIdentity

	stable, err := stableEvaluationProcessorClaim(original)
	if err != nil {
		t.Fatal(err)
	}
	stableRepeat, err := stableEvaluationProcessorClaim(repeat)
	if err != nil {
		t.Fatal(err)
	}
	if stable.ID != stableRepeat.ID || stable.Manifest.RunID != stableRepeat.Manifest.RunID ||
		stable.Manifest.Evaluation.MemberID != stableRepeat.Manifest.Evaluation.MemberID {
		t.Fatalf("deterministic repeats received distinct runtime identities: %#v %#v", stable, stableRepeat)
	}
	if original.ID != "evaluation-job-repeat-0" || original.Manifest.RunID != originalRun ||
		original.Manifest.Evaluation.MemberID != "evaluation-member-repeat-0" {
		t.Fatalf("input claim was mutated: %#v", original)
	}
	stress := repeat
	stressIdentity := *repeat.Manifest.Evaluation
	stressIdentity.CostStressBPS = 15_000
	stress.Manifest.Evaluation = &stressIdentity
	stableStress, err := stableEvaluationProcessorClaim(stress)
	if err != nil {
		t.Fatal(err)
	}
	if stableStress.ID == stable.ID {
		t.Fatal("cost-stress run reused baseline runtime identity")
	}
}
