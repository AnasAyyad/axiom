package demonstrations

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"axiom/internal/backtest"
	"axiom/internal/domain"
	"axiom/internal/rebalancing"
)

// RebalancingID is the stable semantic ID of the bundled advisory scenario.
const RebalancingID = "inventory-rebalancing-basics"

// RunInventoryRebalancing runs the deterministic advisory-only scenario.
func RunInventoryRebalancing(ctx context.Context) (Result, error) {
	if ctx == nil {
		return Result{}, fmt.Errorf("demonstration_context_invalid")
	}
	now := time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)
	evidence, configurationHash, err := guidedRebalancingEvidence(now)
	if err != nil {
		return Result{}, err
	}
	result := Result{ID: RebalancingID, StrategyID: "inventory-rebalancing", StrategyVersion: "inventory-rebalancing@1.0.0", Synthetic: true, AdvisoryOnly: true, AdvisoryEvidence: evidence, ConfigurationHash: configurationHash, Accepted: advisoryEvent("recommended"), Rejected: advisoryEvent("stale_fact_rejected"), Metrics: backtest.Metrics{TotalNetReturn: "not_applicable"}}
	hash, err := resultHash(result)
	if err != nil {
		return Result{}, err
	}
	result.ResultHash = hash
	return result, nil
}

func guidedRebalancingEvidence(now time.Time) (json.RawMessage, string, error) {
	source, err := rebalancingNode("binance", "BTC")
	if err != nil {
		return nil, "", err
	}
	destination, err := rebalancingNode("bybit", "BTC")
	if err != nil {
		return nil, "", err
	}
	sell, err := rebalancingTrade("guided-sell", source, rebalancingNodeMust("binance", "USDT"), "BTCUSDT", "2", now)
	if err != nil {
		return nil, "", err
	}
	buy, err := rebalancingTrade("guided-buy", rebalancingNodeMust("bybit", "USDT"), destination, "BTCUSDT", "3", now)
	if err != nil {
		return nil, "", err
	}
	transfer, err := rebalancingTransfer("guided-transfer", source, destination, now)
	if err != nil {
		return nil, "", err
	}
	graph, err := rebalancing.NewGraph([]rebalancing.Edge{transfer, buy, sell})
	if err != nil {
		return nil, "", err
	}
	request, err := rebalancingRequest(source, destination, now)
	if err != nil {
		return nil, "", err
	}
	request.NaturalReversals = []rebalancing.NaturalReversalPlan{{ID: "guided-natural", CrossExchangeArbitrageDecisionID: "guided-imbalance", Source: source, Destination: destination, SellFactID: sell.ID, BuyFactID: buy.ID}}
	accepted, acceptedDiagnostics, err := graph.Optimize(request)
	if err != nil {
		return nil, "", err
	}
	rejectedDiagnostics, rejectedErr, err := guidedRebalancingRejection(transfer, request, now)
	if err != nil {
		return nil, "", err
	}
	evidence, err := json.Marshal(map[string]any{"recommendation": accepted, "diagnostics": acceptedDiagnostics, "rejected_reason": rejectedErr.Error(), "rejected_diagnostics": rejectedDiagnostics})
	if err != nil {
		return nil, "", err
	}
	return evidence, request.ConfigurationHash, nil
}

func guidedRebalancingRejection(transfer rebalancing.Edge, request rebalancing.Request,
	now time.Time,
) (rebalancing.Diagnostics, error, error) {
	transfer.Provenance.ExpiresAt = now
	rejectedGraph, err := rebalancing.NewGraph([]rebalancing.Edge{rebalancing.SealEdge(transfer)})
	if err != nil {
		return rebalancing.Diagnostics{}, nil, err
	}
	_, diagnostics, rejectedErr := rejectedGraph.Optimize(request)
	if rejectedErr == nil {
		return rebalancing.Diagnostics{}, nil, fmt.Errorf("demonstration_rejection_incomplete")
	}
	return diagnostics, rejectedErr, nil
}

func advisoryEvent(outcome string) backtest.EventResult {
	return backtest.EventResult{Ordinal: 1, Decision: json.RawMessage(`{"outcome":"` + outcome + `"}`), Orders: json.RawMessage("[]"), ExecutionEvents: json.RawMessage("[]"), Balances: json.RawMessage("{}")}
}
func rebalancingNode(exchange, asset string) (rebalancing.Node, error) {
	value, err := domain.ParseAssetSymbol(asset)
	return rebalancing.Node{Exchange: exchange, Asset: value}, err
}
func rebalancingNodeMust(exchange, asset string) rebalancing.Node {
	value, _ := rebalancingNode(exchange, asset)
	return value
}
func rebalancingRequest(source, destination rebalancing.Node, now time.Time) (rebalancing.Request, error) {
	quantity, err := domain.ParseBalance("1")
	if err != nil {
		return rebalancing.Request{}, err
	}
	return rebalancing.Request{ID: "guided-rebalancing", Source: source, Destination: destination, Quantity: quantity, DecisionTime: now, Configuration: rebalancing.DefaultConfiguration(), ConfigurationHash: strings.Repeat("a", 64), FactSetHash: strings.Repeat("b", 64)}, nil
}
func rebalancingTrade(id string, from, to rebalancing.Node, instrument, fee string, now time.Time) (rebalancing.Edge, error) {
	return rebalancingEdge(rebalancing.Edge{ID: id, Version: 1, Kind: rebalancing.TradeEdge, From: from, To: to, Instrument: instrument, Available: true, Compatible: true}, fee, now)
}
func rebalancingTransfer(id string, from, to rebalancing.Node, now time.Time) (rebalancing.Edge, error) {
	return rebalancingEdge(rebalancing.Edge{ID: id, Version: 1, Kind: rebalancing.TransferEdge, From: from, To: to, Network: "BTC", SourceChain: "BTC", DestinationChain: "BTC", Available: true, WithdrawalAvailable: true, DepositAvailable: true, Compatible: true}, "10", now)
}
func rebalancingEdge(edge rebalancing.Edge, fee string, now time.Time) (rebalancing.Edge, error) {
	money, moneyErr := domain.ParseMoney(fee)
	minimum, minimumErr := domain.ParseBalance("0.0001")
	riskScore, riskErr := domain.ParsePercent("0.1")
	confidence, confidenceErr := domain.ParsePercent("0.95")
	if moneyErr != nil || minimumErr != nil || riskErr != nil || confidenceErr != nil {
		return rebalancing.Edge{}, fmt.Errorf("demonstration_input_invalid")
	}
	edge.MinimumQuantity = minimum
	edge.MinimumDuration = time.Second
	edge.MaximumDuration = 2 * time.Second
	edge.RiskScore = riskScore
	edge.Costs.Fee = money
	edge.Warnings = []string{"review_external_conditions"}
	edge.ManualChecklist = []string{"confirm_fact_source"}
	edge.Provenance = rebalancing.Provenance{Source: "guided", Observer: "guided-demo", ObservedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), Confidence: confidence, Approval: rebalancing.Approval{Approved: true, Actor: "guided-owner", Reference: "guided", ApprovedAt: now.Add(-time.Minute)}}
	return rebalancing.SealEdge(edge), nil
}
