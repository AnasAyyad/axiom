package bootstrap

import (
	"strings"
	"testing"

	"axiom/internal/backtest"
	"axiom/internal/config"
	"axiom/internal/domain"
)

func TestA11OperationalWorkerFactoryComposesProductionPipeline(t *testing.T) {
	runID, err := domain.NewRunID("backtest-a11-worker")
	if err != nil {
		t.Fatal(err)
	}
	configuration := config.DefaultConfiguration()
	processor, err := newA11OperationalProcessor(backtest.JobClaim{ID: "backtest-a11-worker",
		Configuration: configuration, Manifest: backtest.RunManifest{RunID: runID, Mode: "backtest",
			ConfigurationHash: strings.Repeat("a", 64), Seed: strings.Repeat("1", 64),
			Models: backtest.ModelNamespace{ID: "namespace-a11", MarketContext: "production-public",
				LiquidityDomain: "combined-a11", FeeDomain: configuration.Models.Fee,
				LatencyDomain: configuration.Models.Latency, FillDomain: "fill-v1"}}})
	if err != nil || processor == nil {
		t.Fatalf("operational worker pipeline = %#v %v", processor, err)
	}
}

func TestA11OperationalWorkerFactoryRejectsIncompleteConfiguration(t *testing.T) {
	configuration := config.DefaultConfiguration()
	configuration.Trend.Parameters = nil
	if _, err := newA11OperationalProcessor(backtest.JobClaim{Configuration: configuration}); err == nil {
		t.Fatal("incomplete durable configuration was accepted")
	}
}
