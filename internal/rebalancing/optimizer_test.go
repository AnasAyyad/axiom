package rebalancing

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"axiom/internal/domain"
)

var testDecisionTime = time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)

func TestOptimizePrefersEligibleNaturalReversalAndPreservesExactEvidence(t *testing.T) {
	source := testNode(t, "binance", "BTC")
	destination := testNode(t, "bybit", "BTC")
	sell := testTrade(t, "sell-btc-binance", source, testNode(t, "binance", "USDT"), "BTCUSDT", "2")
	buy := testTrade(t, "buy-btc-bybit", testNode(t, "bybit", "USDT"), destination, "BTCUSDT", "3")
	transfer := testTransfer(t, "transfer-btc", source, destination, "BTC", "0.1")
	graph, err := NewGraph([]Edge{transfer, buy, sell})
	if err != nil {
		t.Fatal(err)
	}
	request := testRequest(t, source, destination)
	request.NaturalReversals = []NaturalReversalPlan{{
		ID: "natural-1", CrossExchangeArbitrageDecisionID: "cross_exchange_arbitrage-decision-1", Source: source, Destination: destination,
		SellFactID: sell.ID, BuyFactID: buy.ID,
	}}

	recommendation, diagnostics, err := graph.Optimize(request)
	if err != nil {
		t.Fatal(err)
	}
	if recommendation.Method != NaturalReverseMethod || !recommendation.AdvisoryOnly ||
		len(recommendation.Steps) != 2 || recommendation.Steps[0].Fact.ID != sell.ID ||
		recommendation.Steps[1].Fact.ID != buy.ID {
		t.Fatalf("natural recommendation = %#v", recommendation)
	}
	if recommendation.TotalCost.String() != "5" ||
		recommendation.Costs.Fee.String() != "5" ||
		recommendation.MinimumDuration != 2*time.Second ||
		recommendation.MaximumDuration != 4*time.Second ||
		recommendation.RiskScore.String() != "0.2" {
		t.Fatalf("exact evidence = %#v", recommendation)
	}
	if diagnostics.ReviewedFacts != 3 || diagnostics.EligibleFacts != 3 ||
		diagnostics.RejectedFacts != 0 || diagnostics.CandidatePaths != 1 {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	if !slices.Contains(recommendation.Warnings, "natural_reverse_arbitrage_preferred") ||
		len(recommendation.ManualChecklist) < 4 || len(recommendation.CanonicalHash) != 64 ||
		!strings.HasPrefix(recommendation.ID, "inventory_rebalancing-") {
		t.Fatalf("incomplete advisory evidence = %#v", recommendation)
	}
}

func TestOptimizeIsDeterministicAcrossFactOrderAndUsesCompleteCostTieBreaks(t *testing.T) {
	source := testNode(t, "binance", "ETH")
	destination := testNode(t, "bybit", "ETH")
	slow := testTransfer(t, "route-slow", source, destination, "ETH", "1")
	slow.MaximumDuration = 5 * time.Second
	slow = SealEdge(slow)
	fast := testTransfer(t, "route-fast", source, destination, "ETH-FAST", "1")
	direct := []Edge{slow, fast}
	reversed := []Edge{fast, slow}
	request := testRequest(t, source, destination)

	firstGraph, err := NewGraph(direct)
	if err != nil {
		t.Fatal(err)
	}
	secondGraph, err := NewGraph(reversed)
	if err != nil {
		t.Fatal(err)
	}
	first, _, err := firstGraph.Optimize(request)
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := secondGraph.Optimize(request)
	if err != nil {
		t.Fatal(err)
	}
	if first.CanonicalHash != second.CanonicalHash || first.Steps[0].Fact.ID != "route-fast" {
		t.Fatalf("non-deterministic recommendations: %#v / %#v", first, second)
	}
	if first.TotalCost.String() != "1" || first.Costs.Fee.String() != "1" ||
		first.Costs.Spread.String() != "0" || first.Costs.Depth.String() != "0" ||
		first.Costs.Delay.String() != "0" || first.Costs.NetworkFee.String() != "0" ||
		first.Costs.Compatibility.String() != "0" || first.Costs.VolatilityRisk.String() != "0" ||
		first.Costs.OperationalRisk.String() != "0" {
		t.Fatalf("cost components not preserved: %#v", first.Costs)
	}
}

func TestFactsFailClosedWhenStaleUnapprovedAmbiguousOrIncompatible(t *testing.T) {
	source := testNode(t, "binance", "BTC")
	destination := testNode(t, "bybit", "BTC")
	tests := []struct {
		name  string
		alter func(*Edge)
	}{
		{name: "stale", alter: func(edge *Edge) {
			edge.Provenance.ExpiresAt = testDecisionTime
		}},
		{name: "unapproved", alter: func(edge *Edge) {
			edge.Provenance.Approval = Approval{}
		}},
		{name: "low confidence", alter: func(edge *Edge) {
			edge.Provenance.Confidence = testPercent(t, "0.79")
		}},
		{name: "ambiguous", alter: func(edge *Edge) {
			edge.Ambiguous = true
		}},
		{name: "incompatible", alter: func(edge *Edge) {
			edge.Compatible = false
		}},
		{name: "withdrawal unavailable", alter: func(edge *Edge) {
			edge.WithdrawalAvailable = false
		}},
		{name: "deposit unavailable", alter: func(edge *Edge) {
			edge.DepositAvailable = false
		}},
		{name: "chain mismatch", alter: func(edge *Edge) {
			edge.DestinationChain = "BEP20"
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			edge := testTransfer(t, "transfer-btc", source, destination, "BTC", "1")
			test.alter(&edge)
			edge = SealEdge(edge)
			graph, err := NewGraph([]Edge{edge})
			if err != nil {
				t.Fatal(err)
			}
			_, diagnostics, err := graph.Optimize(testRequest(t, source, destination))
			if errorCode(err) != "route_unavailable" || diagnostics.EligibleFacts != 0 ||
				diagnostics.RejectedFacts != 1 {
				t.Fatalf("result = %#v, %v", diagnostics, err)
			}
		})
	}
}

func TestLatestFactVersionAndProvenanceIntegrityFailClosed(t *testing.T) {
	source := testNode(t, "binance", "BTC")
	destination := testNode(t, "bybit", "BTC")
	old := testTransfer(t, "transfer-v1", source, destination, "BTC", "1")
	newest := old
	newest.ID = "transfer-v2"
	newest.Version = 2
	newest.Available = false
	newest = SealEdge(newest)
	graph, err := NewGraph([]Edge{old, newest})
	if err != nil {
		t.Fatal(err)
	}
	_, diagnostics, err := graph.Optimize(testRequest(t, source, destination))
	if errorCode(err) != "route_unavailable" || diagnostics.ReviewedFacts != 2 ||
		diagnostics.RejectedFacts != 1 {
		t.Fatalf("latest version did not fail closed: %#v, %v", diagnostics, err)
	}

	tampered := old
	tampered.Costs.Fee = testMoney(t, "0.01")
	if _, err := NewGraph([]Edge{tampered}); errorCode(err) != "fact_invalid" {
		t.Fatalf("tampered provenance accepted: %v", err)
	}
	duplicate := old
	duplicate.ID = "duplicate-id"
	duplicate = SealEdge(duplicate)
	if _, err := NewGraph([]Edge{old, duplicate}); errorCode(err) != "fact_ambiguous" {
		t.Fatalf("ambiguous fact version accepted: %v", err)
	}
}

func TestOptimizeRejectsInvalidRequestAndUnavailableBoundedRoute(t *testing.T) {
	source := testNode(t, "binance", "BTC")
	destination := testNode(t, "bybit", "BTC")
	graph, err := NewGraph([]Edge{testTransfer(t, "transfer-btc", source, destination, "BTC", "30")})
	if err != nil {
		t.Fatal(err)
	}
	request := testRequest(t, source, destination)
	if _, _, err := graph.Optimize(request); errorCode(err) != "route_unavailable" {
		t.Fatalf("over-cost route accepted: %v", err)
	}
	request.ConfigurationHash = "not-a-hash"
	if _, _, err := graph.Optimize(request); errorCode(err) != "request_invalid" {
		t.Fatalf("invalid request accepted: %v", err)
	}
}

func testRequest(t testing.TB, source, destination Node) Request {
	t.Helper()
	return Request{
		ID: "request-1", Source: source, Destination: destination,
		Quantity: testBalance(t, "1"), DecisionTime: testDecisionTime,
		Configuration:     DefaultConfiguration(),
		ConfigurationHash: strings.Repeat("a", 64),
		FactSetHash:       strings.Repeat("b", 64),
	}
}

func testTrade(t testing.TB, id string, from, to Node, instrument, fee string) Edge {
	t.Helper()
	return testEdge(t, Edge{
		ID: id, Version: 1, Kind: TradeEdge, From: from, To: to, Instrument: instrument,
		Available: true, Compatible: true, Costs: CostBreakdown{Fee: testMoney(t, fee)},
	})
}

func testTransfer(t testing.TB, id string, from, to Node, network, fee string) Edge {
	t.Helper()
	return testEdge(t, Edge{
		ID: id, Version: 1, Kind: TransferEdge, From: from, To: to,
		Network: network, SourceChain: network, DestinationChain: network,
		Available: true, WithdrawalAvailable: true, DepositAvailable: true, Compatible: true,
		Costs: CostBreakdown{Fee: testMoney(t, fee)},
	})
}

func testEdge(t testing.TB, edge Edge) Edge {
	t.Helper()
	edge.MinimumQuantity = testBalance(t, "0.0001")
	edge.MinimumDuration = time.Second
	edge.MaximumDuration = 2 * time.Second
	edge.RiskScore = testPercent(t, "0.1")
	edge.Warnings = []string{"review_external_conditions"}
	edge.ManualChecklist = []string{"confirm_fact_source"}
	edge.Provenance = Provenance{
		Source: "reviewed-fixture", Observer: "inventory_rebalancing-test",
		ObservedAt: testDecisionTime.Add(-time.Hour), ExpiresAt: testDecisionTime.Add(time.Hour),
		Confidence: testPercent(t, "0.95"),
		Approval: Approval{
			Approved: true, Actor: "risk-reviewer", Reference: "AX-V1B-B06-TEST",
			ApprovedAt: testDecisionTime.Add(-30 * time.Minute),
		},
	}
	return SealEdge(edge)
}

func testNode(t testing.TB, exchange, asset string) Node {
	t.Helper()
	symbol, err := domain.ParseAssetSymbol(asset)
	if err != nil {
		t.Fatal(err)
	}
	return Node{Exchange: exchange, Asset: symbol}
}

func testMoney(t testing.TB, value string) domain.Money {
	t.Helper()
	result, err := domain.ParseMoney(value)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func testBalance(t testing.TB, value string) domain.Balance {
	t.Helper()
	result, err := domain.ParseBalance(value)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func testPercent(t testing.TB, value string) domain.Percent {
	t.Helper()
	result, err := domain.ParsePercent(value)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func errorCode(err error) string {
	var failure *Error
	if errors.As(err, &failure) {
		return failure.Code
	}
	return ""
}
