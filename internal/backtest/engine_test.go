package backtest

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"axiom/internal/domain"
	"axiom/internal/replay"
)

func TestEngineProducesTenByteIdenticalResults(t *testing.T) {
	root, selected := datasetFixture(t, 2)
	var canonical []byte
	for run := 0; run < 10; run++ {
		reader, err := OpenDataset(root, selected, compatibility(1, 0))
		if err != nil {
			t.Fatal(err)
		}
		controller, err := replay.NewController(ReplaySource{Reader: reader}, replay.RealPacer{}, replay.MaximumTiming, 1)
		if err != nil {
			t.Fatal(err)
		}
		engine, err := NewEngine(controller, &echoProcessor{}, runManifest(t, reader.Descriptor()))
		if err != nil {
			t.Fatal(err)
		}
		result, err := engine.Run(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		if run == 0 {
			canonical = encoded
		} else if string(encoded) != string(canonical) {
			t.Fatalf("run %d differs", run+1)
		}
	}
}

func TestResultComparisonRejectsIncompatibleNamespace(t *testing.T) {
	namespace := modelNamespace()
	left := CanonicalResult{Namespace: namespace, ResultHash: strings.Repeat("a", 64)}
	right := left
	right.Namespace.FillDomain = "fill-v2"
	if _, err := CompareResults(left, right); err == nil {
		t.Fatal("incompatible model worlds were compared")
	}
}

type echoProcessor struct{}

func (processor *echoProcessor) Process(_ context.Context, event replay.Event) (EventResult, error) {
	payload, _ := json.Marshal(struct {
		Ordinal uint64 `json:"ordinal"`
		Input   string `json:"input"`
	}{Ordinal: event.Ordinal, Input: string(event.Canonical)})
	empty := json.RawMessage(`[]`)
	return EventResult{Ordinal: event.Ordinal, Decision: payload, Orders: empty, Balances: empty}, nil
}

func (processor *echoProcessor) Metrics() Metrics {
	return Metrics{TotalNetReturn: "0", AnnualizedReturn: "unavailable", MaximumDrawdown: "0",
		CurrentDrawdown: "0", SharpeRatio: "unavailable", SortinoRatio: "unavailable",
		CalmarRatio: "unavailable", ProfitFactor: "unavailable", Expectancy: "0", WinRate: "0",
		AverageWin: "0", AverageLoss: "0", LargestWin: "0", LargestLoss: "0", Turnover: "0",
		Exposure: "0", FeePercentGrossProfit: "unavailable", SlippagePercentGrossProfit: "unavailable",
		RecoveryLoss: "0", TimeInMarket: "0", ByAsset: map[string]string{}, ByExchange: map[string]string{},
		ByStrategy: map[string]string{}, ByRegime: map[string]string{}}
}

func runManifest(t *testing.T, descriptor DatasetDescriptor) RunManifest {
	t.Helper()
	runID, _ := domain.NewRunID("engine-fixture")
	hash := strings.Repeat("b", 64)
	return RunManifest{RunID: runID, Mode: "backtest", CodeCommit: strings.Repeat("a", 64),
		Build: CurrentBuildIdentity([]string{"trimpath"}, hash, hash), Dataset: descriptor,
		ConfigurationHash: hash, Seed: "seed-1", SchedulerVersion: "scheduler-v1",
		SerializationVersion: "canonical-json-v1", Models: modelNamespace(), StartingBalanceHash: hash}
}

func modelNamespace() ModelNamespace {
	return ModelNamespace{ID: "models-v1", MarketContext: "production-public", LiquidityDomain: "combined-1",
		FeeDomain: "fee-v1", LatencyDomain: "latency-v1", FillDomain: "fill-v1"}
}
