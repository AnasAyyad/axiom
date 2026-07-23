package crossarb

import (
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	"axiom/internal/domain"
)

func TestCrossExchangeEvaluatorP99Within25Milliseconds(t *testing.T) {
	input := evaluationFixture(t, "BTC", false)
	samples := make([]time.Duration, 400)
	for index := range samples {
		start := time.Now()
		if _, err := Evaluate(input); err != nil {
			t.Fatal(err)
		}
		samples[index] = time.Since(start)
	}
	sort.Slice(samples, func(left, right int) bool { return samples[left] < samples[right] })
	p99 := samples[(len(samples)*99+99)/100-1]
	t.Logf("B5 evaluator p99=%s samples=%d", p99, len(samples))
	if p99 > 25*time.Millisecond {
		t.Fatalf("p99 %s exceeds 25ms", p99)
	}
}

func TestCrossExchangeEvaluatorConcurrentDeterminism(t *testing.T) {
	input := evaluationFixture(t, "BTC", false)
	want, err := Evaluate(input)
	if err != nil {
		t.Fatal(err)
	}
	const workers = 32
	var wait sync.WaitGroup
	errors := make(chan error, workers)
	for range workers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			got, evaluateErr := Evaluate(input)
			if evaluateErr != nil {
				errors <- evaluateErr
				return
			}
			if len(got) != len(want) || got[0].ID != want[0].ID {
				errors <- fmt.Errorf("non-deterministic candidate")
			}
		}()
	}
	wait.Wait()
	close(errors)
	for concurrentErr := range errors {
		t.Fatal(concurrentErr)
	}
}

func BenchmarkCrossExchangeEvaluator(b *testing.B) {
	input := evaluationFixture(b, "BTC", false)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := Evaluate(input); err != nil {
			b.Fatal(err)
		}
	}
}

func FuzzCrossExchangeClosedCycle(f *testing.F) {
	f.Add(uint16(5), false)
	f.Add(uint16(100), true)
	f.Fuzz(func(t *testing.T, micros uint16, reverse bool) {
		input := evaluationFixture(t, "BTC", reverse)
		value := uint64(micros%1000) + 1
		input.Restoration.LatencyDeterioration = money(fmt.Sprintf("0.%06d", value))
		candidates, err := Evaluate(input)
		if err != nil {
			return
		}
		zero, _ := domain.ParsePnL("0")
		for _, candidate := range candidates {
			if candidate.Economics.ExpectedClosedCycleProfit.Compare(zero) <= 0 ||
				candidate.Economics.WorstClosedCycleProfit.Compare(zero) <= 0 {
				t.Fatalf("non-positive candidate = %#v", candidate)
			}
		}
	})
}
