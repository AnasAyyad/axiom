package rebalancing

import (
	"fmt"
	"sort"
	"testing"
	"time"
)

func TestAdvisoryOptimizerP99Within25Milliseconds(t *testing.T) {
	graph, request := performanceFixture(t)
	samples := make([]time.Duration, 400)
	for index := range samples {
		start := time.Now()
		if _, _, err := graph.Optimize(request); err != nil {
			t.Fatal(err)
		}
		samples[index] = time.Since(start)
	}
	sort.Slice(samples, func(left, right int) bool { return samples[left] < samples[right] })
	p99 := samples[(len(samples)*99+99)/100-1]
	t.Logf("B6 advisory optimizer p99=%s samples=%d", p99, len(samples))
	if p99 > 25*time.Millisecond {
		t.Fatalf("p99 %s exceeds 25ms", p99)
	}
}

func BenchmarkAdvisoryOptimizer(b *testing.B) {
	graph, request := performanceFixture(b)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, _, err := graph.Optimize(request); err != nil {
			b.Fatal(err)
		}
	}
}

func FuzzAdvisoryOptimizerPreservesExactNonNegativeCost(f *testing.F) {
	f.Add(uint16(1), uint16(2))
	f.Add(uint16(100), uint16(50))
	f.Fuzz(func(t *testing.T, feeUnits, riskUnits uint16) {
		source := testNode(t, "binance", "ETH")
		destination := testNode(t, "bybit", "ETH")
		fee := fmt.Sprintf("0.%06d", uint64(feeUnits)%999999+1)
		risk := fmt.Sprintf("0.%06d", uint64(riskUnits)%999999+1)
		edge := testTransfer(t, "fuzz-transfer", source, destination, "ETH", fee)
		edge.RiskScore = testPercent(t, risk)
		edge = SealEdge(edge)
		graph, err := NewGraph([]Edge{edge})
		if err != nil {
			t.Fatal(err)
		}
		recommendation, _, err := graph.Optimize(testRequest(t, source, destination))
		if err != nil {
			return
		}
		if recommendation.TotalCost.Compare(testMoney(t, fee)) != 0 ||
			recommendation.RiskScore.Compare(testPercent(t, risk)) != 0 ||
			!recommendation.AdvisoryOnly {
			t.Fatalf("recommendation = %#v", recommendation)
		}
	})
}

func performanceFixture(t testing.TB) (*Graph, Request) {
	t.Helper()
	source := testNode(t, "binance", "BTC")
	destination := testNode(t, "bybit", "BTC")
	edges := make([]Edge, 0, 8)
	for index := range 8 {
		edge := testTransfer(
			t, fmt.Sprintf("performance-route-%02d", index), source, destination,
			fmt.Sprintf("NET%02d", index), fmt.Sprintf("0.%04d", index+1),
		)
		edge.SourceChain = edge.Network
		edge.DestinationChain = edge.Network
		edge.MaximumDuration = time.Duration(index+2) * time.Second
		edges = append(edges, SealEdge(edge))
	}
	graph, err := NewGraph(edges)
	if err != nil {
		t.Fatal(err)
	}
	return graph, testRequest(t, source, destination)
}
