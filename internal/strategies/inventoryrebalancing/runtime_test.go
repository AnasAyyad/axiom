package inventoryrebalancing

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"axiom/internal/backtest"
	"axiom/internal/config"
	"axiom/internal/domain"
	"axiom/internal/rebalancing"
	"axiom/internal/replay"
)

func TestRuntimeProducesRecommendationThroughCompleteNoActionPipeline(t *testing.T) {
	runtime, event := rebalancingRuntimeFixture(t, true)
	processor := newTestAdvisoryProcessor(t, runtime)
	result, err := processor.Process(context.Background(), event)
	if err != nil || string(result.Orders) != "[]" || string(result.ExecutionEvents) != "[]" {
		t.Fatalf("result=%+v error=%v", result, err)
	}
	var decision struct {
		Strategy struct {
			Outcome      string `json:"outcome"`
			AdvisoryOnly bool   `json:"advisory_only"`
		} `json:"strategy"`
		Plan struct {
			OrderCount            int  `json:"order_count"`
			TransferCount         int  `json:"transfer_count"`
			ExternalActionAllowed bool `json:"external_action_allowed"`
		} `json:"plan"`
		Reconciliation struct {
			NoExternalAction bool `json:"no_external_action_confirmed"`
		} `json:"reconciliation"`
	}
	if json.Unmarshal(result.Decision, &decision) != nil || decision.Strategy.Outcome != "recommended" ||
		!decision.Strategy.AdvisoryOnly || decision.Plan.OrderCount != 0 || decision.Plan.TransferCount != 0 ||
		decision.Plan.ExternalActionAllowed || !decision.Reconciliation.NoExternalAction {
		t.Fatalf("decision=%s", result.Decision)
	}
}

func TestRuntimePreservesNoRouteAsExplainedNoAction(t *testing.T) {
	runtime, event := rebalancingRuntimeFixture(t, false)
	processor := newTestAdvisoryProcessor(t, runtime)
	result, err := processor.Process(context.Background(), event)
	if err != nil {
		t.Fatal(err)
	}
	var decision struct {
		Strategy struct {
			Outcome string `json:"outcome"`
			Reason  string `json:"reason"`
		} `json:"strategy"`
		Risk struct {
			Status              string `json:"status"`
			ExecutionAuthorized bool   `json:"execution_authorized"`
		} `json:"risk"`
	}
	if json.Unmarshal(result.Decision, &decision) != nil || decision.Strategy.Outcome != "no_action" ||
		decision.Strategy.Reason != "rebalancing:route_unavailable" || decision.Risk.Status != "blocked_no_route" ||
		decision.Risk.ExecutionAuthorized {
		t.Fatalf("decision=%s", result.Decision)
	}
}

func TestRuntimeRejectsUnsealedInventoryEvidence(t *testing.T) {
	runtime, event := rebalancingRuntimeFixture(t, true)
	var input Input
	if json.Unmarshal(event.Canonical, &input) != nil {
		t.Fatal("fixture decode failed")
	}
	input.Inventory.SourceExcess = mustBalance(t, "3")
	event.Canonical, _ = json.Marshal(input)
	if _, err := runtime.EvaluateAdvisory(context.Background(), event); err == nil {
		t.Fatal("changed inventory accepted with stale evidence hash")
	}
}

func newTestAdvisoryProcessor(t *testing.T, runtime *Runtime) *backtest.AdvisoryPipelineProcessor {
	t.Helper()
	processor, err := backtest.NewAdvisoryPipelineProcessor(backtest.AdvisoryPipelineDependencies{
		Strategy: runtime, Allocator: runtime, Risk: runtime, Planner: runtime,
		Accounting: runtime, Reconciliation: runtime,
		Metrics: func() backtest.Metrics { return backtest.Metrics{TotalNetReturn: "not_applicable"} },
	})
	if err != nil {
		t.Fatal(err)
	}
	return processor
}

func rebalancingRuntimeFixture(t *testing.T, available bool) (*Runtime, replay.Event) {
	t.Helper()
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	configuration := config.DefaultMultiStrategyConfiguration()
	configurationHash := strings.Repeat("a", 64)
	runtime, err := NewRuntime(configuration.Rebalancing, configurationHash)
	if err != nil {
		t.Fatal(err)
	}
	btc, _ := domain.ParseAssetSymbol("BTC")
	source := rebalancing.Node{Exchange: "binance", Asset: btc}
	destination := rebalancing.Node{Exchange: "bybit", Asset: btc}
	optimizerConfig, err := rebalancing.ConfigurationFromReviewed(configuration.Rebalancing)
	if err != nil {
		t.Fatal(err)
	}
	request := rebalancing.Request{ID: "inventory-request-1", Source: source, Destination: destination,
		Quantity: mustBalance(t, "1"), DecisionTime: now, Configuration: optimizerConfig,
		ConfigurationHash: configurationHash, FactSetHash: strings.Repeat("b", 64)}
	edge := rebalancing.Edge{ID: "reviewed-transfer-1", Version: 1, Kind: rebalancing.TransferEdge,
		From: source, To: destination, Network: "BTC", SourceChain: "BTC", DestinationChain: "BTC",
		MinimumQuantity: mustBalance(t, "0.1"), Available: available, WithdrawalAvailable: true,
		DepositAvailable: true, Compatible: true, MinimumDuration: time.Minute, MaximumDuration: time.Hour,
		RiskScore: mustPercent(t, "0.1"), Warnings: []string{"manual_external_action_required"},
		ManualChecklist: []string{"review_route"}, Provenance: rebalancing.Provenance{
			Source: "recorded-facts", Observer: "owner-review", ObservedAt: now.Add(-time.Hour),
			ExpiresAt: now.Add(time.Hour), Confidence: mustPercent(t, "0.95"), Approval: rebalancing.Approval{
				Approved: true, Actor: "owner", Reference: "review-1", ApprovedAt: now.Add(-30 * time.Minute)},
		}}
	edge = rebalancing.SealEdge(edge)
	snapshot := SealInventorySnapshot(InventorySnapshot{ID: "inventory-snapshot-1", Source: source,
		Destination: destination, SourceExcess: mustBalance(t, "2"), DestinationDeficit: mustBalance(t, "2"),
		ObservedAt: now.Add(-time.Minute)})
	input := Input{Ordinal: 1, LogicalTime: 5, Inventory: snapshot, Request: request, Facts: []rebalancing.Edge{edge}}
	canonical, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	return runtime, replay.Event{Ordinal: 1, LogicalTime: 5, Canonical: canonical}
}

func mustBalance(t *testing.T, value string) domain.Balance {
	t.Helper()
	parsed, err := domain.ParseBalance(value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func mustPercent(t *testing.T, value string) domain.Percent {
	t.Helper()
	parsed, err := domain.ParsePercent(value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}
