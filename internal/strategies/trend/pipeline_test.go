package trend

import (
	"context"
	"encoding/json"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"axiom/internal/accounting"
	"axiom/internal/backtest"
	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/portfolio"
	"axiom/internal/replay"
	"axiom/internal/risk"
	runtimecore "axiom/internal/runtime"
	"axiom/internal/simulation"
)

func TestA10TrendUsesRealAllocatorRiskPlannerAndSimulationPipeline(t *testing.T) {
	evaluator, input := testEvaluatorAndInput(t)
	adapter, err := NewAdapter(evaluator)
	if err != nil {
		t.Fatal(err)
	}
	owned := trendPipelinePortfolio(t)
	registry := portfolio.NewAssetRegistry()
	liquidity := portfolio.NewLiquidityPool()
	available, _ := domain.ParseQuantity("1")
	if err = liquidity.Open(input.Sizing.LiquidityDomain, available); err != nil {
		t.Fatal(err)
	}
	allocator, _ := portfolio.NewAllocator(owned, registry, liquidity)
	pipelineAllocator, _ := portfolio.NewPipelineAllocator(allocator)
	vault := portfolio.NewApprovalVault()
	riskEngine, _ := risk.NewEngine(&trendRiskAudit{}, trendRiskAlerts{})
	if err = riskEngine.ManualTransition(risk.StateNormal, trendRecoveryEvidence(input.Now)); err != nil {
		t.Fatal(err)
	}
	pipelineRisk, _ := risk.NewPipelineEngine(riskEngine, vault, registry, trendRiskInputs{at: input.Now.Add(time.Nanosecond)})
	trendPlanner, _ := NewPlanner("shadow", input.Sizing.LiquidityDomain, adapter)
	planner, _ := portfolio.NewEligibilityPlanner(trendPlanner, vault, registry)
	guard, _ := portfolio.NewBrokerGuard(owned, registry)
	broker := trendSimulatedBroker(t, input, guard)
	processor, err := backtest.NewPipelineProcessor(backtest.PipelineDependencies{Strategy: adapter,
		Allocator: pipelineAllocator, Risk: pipelineRisk, Planner: planner, Broker: broker,
		Reduce:  pipelineAllocator.ReduceSimulation,
		Metrics: func() backtest.Metrics { return backtest.Metrics{TotalNetReturn: "not_evaluated"} }})
	if err != nil {
		t.Fatal(err)
	}
	canonical, _ := json.Marshal(input)
	result, err := processor.Process(context.Background(), replay.Event{LogicalTime: input.LogicalTime, Ordinal: input.Ordinal, Canonical: canonical})
	if err != nil || result.Ordinal != input.Ordinal || len(result.Orders) == 0 {
		t.Fatalf("A10 shared pipeline = %#v %v", result, err)
	}
	if strings.Contains(string(result.Orders), input.Candles[len(input.Candles)-1].Close.String()+`\"`) {
		t.Fatal("signal close appeared as a simulated fill")
	}
}

func TestA10TrendAllocatorRiskP99AtMost25Milliseconds(t *testing.T) {
	if raceInstrumentation {
		t.Skip("latency qualification is invalid under race instrumentation")
	}
	harness := newTrendPerformanceHarness(t)
	durations := make([]time.Duration, 0, 200)
	for index := 0; index < 210; index++ {
		input := harness.baseline
		input.Ordinal = harness.baseline.Ordinal + uint64(index+1)
		input.LogicalTime = harness.baseline.LogicalTime + uint64(index+1)
		started := time.Now()
		decision, err := harness.evaluator.Evaluate(input)
		if err != nil || decision.Candidate == nil {
			t.Fatal(err)
		}
		candidate, err := harness.adapter.portfolioCandidateValue(input, decision)
		if err != nil {
			t.Fatal(err)
		}
		allocations, err := harness.allocator.Allocate([]portfolio.Candidate{candidate})
		if err != nil || len(allocations) != 1 {
			t.Fatal(err)
		}
		policy := risk.DefaultGlobalPolicy()
		policy.State = risk.StateNormal
		result, err := harness.risk.Evaluate(risk.Request{Intent: risk.IntentEntry, Policies: []risk.Policy{policy},
			Observations: trendHealthyObservations(), EvaluatedAt: harness.baseline.Now.Add(time.Nanosecond)})
		if err != nil || result.Action != risk.ActionApprove {
			t.Fatal(err)
		}
		elapsed := time.Since(started)
		if err = harness.allocator.Close(allocations[0], accounting.ReservationReleased); err != nil {
			t.Fatal(err)
		}
		if index >= 10 {
			durations = append(durations, elapsed)
		}
	}
	assertTrendP99(t, durations)
}

type trendPerformanceHarness struct {
	evaluator *Evaluator
	adapter   *Adapter
	allocator *portfolio.Allocator
	risk      *risk.Engine
	baseline  Input
}

func newTrendPerformanceHarness(t *testing.T) trendPerformanceHarness {
	t.Helper()
	evaluator, baseline := testEvaluatorAndInput(t)
	adapter, _ := NewAdapter(evaluator)
	liquidity := portfolio.NewLiquidityPool()
	available, _ := domain.ParseQuantity("100")
	if err := liquidity.Open(baseline.Sizing.LiquidityDomain, available); err != nil {
		t.Fatal(err)
	}
	allocator, _ := portfolio.NewAllocator(trendPipelinePortfolio(t), portfolio.NewAssetRegistry(), liquidity)
	riskEngine, _ := risk.NewEngine(&trendRiskAudit{}, trendRiskAlerts{})
	if err := riskEngine.ManualTransition(risk.StateNormal, trendRecoveryEvidence(baseline.Now)); err != nil {
		t.Fatal(err)
	}
	return trendPerformanceHarness{evaluator: evaluator, adapter: adapter, allocator: allocator, risk: riskEngine, baseline: baseline}
}

func assertTrendP99(t *testing.T, durations []time.Duration) {
	t.Helper()
	sort.Slice(durations, func(left, right int) bool { return durations[left] < durations[right] })
	p99 := durations[(len(durations)*99+99)/100-1]
	t.Logf("declared profile go=%s os=%s arch=%s cpus=%d samples=%d p99=%s", runtime.Version(), runtime.GOOS,
		runtime.GOARCH, runtime.NumCPU(), len(durations), p99)
	if p99 > 25*time.Millisecond {
		t.Fatalf("Trend + allocator + risk p99 %s exceeds 25ms", p99)
	}
}

func trendPipelinePortfolio(t *testing.T) *portfolio.Portfolio {
	t.Helper()
	runID, _ := domain.NewRunID("trend-pipeline-run")
	portfolioID, _ := domain.NewPortfolioID("trend-pipeline-portfolio")
	accountID, _ := domain.NewVirtualAccountID("trend-pipeline-account")
	result, err := portfolio.InitializeV1ATrend(runID, portfolioID, accountID, strings.Repeat("a", 64),
		accounting.NewMemoryJournal(), domain.EventTime{UTC: time.Date(2026, 7, 16, 8, 0, 0, 0, time.UTC), Sequence: 1})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func trendSimulatedBroker(t *testing.T, input Input, guard simulation.BoundaryGuard) *simulation.SimulatedBroker {
	t.Helper()
	randomness, _ := runtimecore.NewRandomness(make([]byte, 32))
	zeroRate, _ := domain.ParseRate("0")
	feeRate, _ := domain.ParseRate("0.001")
	zeroPercent, _ := domain.ParsePercent("0")
	partialRatio, _ := domain.ParsePercent("0.5")
	quantity, _ := domain.ParseQuantity("1")
	arrival := input.LogicalTime + uint64(time.Millisecond)
	state := simulation.BookState{Exchange: "binance", Instrument: input.Instrument, Version: 51, LogicalTime: arrival,
		Bids: []exchangecontracts.PriceLevel{{Price: price(t, "299.99"), Quantity: quantity}},
		Asks: []exchangecontracts.PriceLevel{{Price: input.Sizing.EntryReference, Quantity: quantity}}}
	models := simulation.BrokerModels{
		Fee: simulation.FeeModel{Version: "fee-v1", TakerRate: feeRate, MakerRate: zeroRate, RebateRate: zeroRate, DecimalScale: 18},
		Price: simulation.PriceModel{Version: "price-v1", Spread: zeroPercent, Slippage: zeroPercent,
			Impact: zeroPercent, AdverseSelection: zeroPercent, DecimalScale: 18},
		Latency: simulation.LatencyModel{Version: "latency-v1", Samples: []time.Duration{time.Millisecond}},
		Fill:    simulation.FillModel{Version: "fill-v1", PartialRatio: partialRatio, QuantityScale: 18},
	}
	broker, err := simulation.NewBroker(randomness, trendTimeline{state: state}, trendMetadata{value: input.Sizing.InstrumentMetadata},
		guard, simulation.NewLiquidityLedger(), models)
	if err != nil {
		t.Fatal(err)
	}
	return broker
}

type trendTimeline struct{ state simulation.BookState }

func (timeline trendTimeline) AtOrAfter(domain.Instrument, uint64) (simulation.BookState, bool, error) {
	return timeline.state, true, nil
}

type trendMetadata struct{ value domain.InstrumentMetadata }

func (metadata trendMetadata) Metadata(simulation.BookState) (domain.InstrumentMetadata, error) {
	return metadata.value, nil
}

type trendRiskInputs struct{ at time.Time }

func (inputs trendRiskInputs) Current() (risk.Observations, []risk.Policy, time.Time, error) {
	policy := risk.DefaultGlobalPolicy()
	policy.State = risk.StateNormal
	return trendHealthyObservations(), []risk.Policy{policy}, inputs.at, nil
}

func trendHealthyObservations() risk.Observations {
	percentage := func(value string) *domain.Percent { parsed, _ := domain.ParsePercent(value); return &parsed }
	openOrders, quality := uint32(0), uint8(100)
	age, lag, drift := time.Millisecond, time.Millisecond, time.Millisecond
	problem := false
	return risk.Observations{AccountDrawdown: percentage("0"), UTCDayLoss: percentage("0"),
		Rolling24HourLoss: percentage("0"), StrategyLoss: percentage("0"), AssetExposure: percentage("0"),
		CombinedExposure: percentage("0"), ExchangeExposure: percentage("0"), Reserve: percentage("1"),
		ReservedCapital: percentage("0"), Spread: percentage("0"), Slippage: percentage("0"), OpenOrders: &openOrders,
		BookAge: &age, QueueLag: &lag, ClockDrift: &drift, QualityScore: &quality,
		Health: risk.HealthInputs{Gap: &problem, StaleData: &problem, ReconciliationFault: &problem,
			AccountingFault: &problem, UnknownOrder: &problem, PersistenceFault: &problem,
			DiskFault: &problem, APIError: &problem, LeaseLost: &problem}}
}

func trendRecoveryEvidence(at time.Time) risk.RecoveryEvidence {
	return risk.RecoveryEvidence{Reconciled: true, PersistenceHealthy: true, BooksFresh: true,
		UnknownOrdersResolved: true, Reauthenticated: true, AuditDurable: true,
		Actor: "owner", Reason: "A10 pipeline qualification", At: at}
}

type trendRiskAudit struct{}

func (*trendRiskAudit) Append(risk.AuditEvent) error { return nil }

type trendRiskAlerts struct{}

func (trendRiskAlerts) Emit(string, risk.Action, risk.State) error { return nil }
