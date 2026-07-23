package meanreversion

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
	"axiom/internal/execution"
	"axiom/internal/portfolio"
	"axiom/internal/replay"
	"axiom/internal/risk"
	runtimecore "axiom/internal/runtime"
	"axiom/internal/simulation"
)

func TestB3MeanReversionUsesRealAllocatorRiskPlannerSimulationReducerAndAccounting(t *testing.T) {
	operational, input, owned := newOperationalPipeline(t, true)
	canonical, _ := json.Marshal(input)
	result, err := operational.Process(context.Background(), replay.Event{LogicalTime: input.LogicalTime,
		Ordinal: input.Ordinal, Canonical: canonical})
	if err != nil || result.Ordinal != input.Ordinal || len(result.Orders) == 0 || len(result.Balances) == 0 {
		t.Fatalf("B3 shared pipeline = %#v, %v", result, err)
	}
	if strings.Contains(string(result.Orders), `"price":"`+input.PrimaryCandles[len(input.PrimaryCandles)-1].Close.String()+`"`) {
		t.Fatal("signal-close fill appeared in B3 simulation")
	}
	snapshot := owned.Snapshot()
	if snapshot.Ownership.Strategy != portfolio.V1BMeanReversionStrategy || len(snapshot.Positions) != 1 ||
		snapshot.Positions[0].Quantity.String() == "0" {
		t.Fatalf("mean-reversion accounting ownership = %#v", snapshot)
	}
}

func TestB3CentralRiskRejectionReleasesFundsAndLiquidityReservation(t *testing.T) {
	_, input, owned, processor, liquidity := pipelineComponents(t, false)
	canonical, _ := json.Marshal(input)
	_, err := processor.Process(context.Background(), replay.Event{LogicalTime: input.LogicalTime,
		Ordinal: input.Ordinal, Canonical: canonical})
	if err == nil || err.Error() != "backtest:risk_stage_failed" {
		t.Fatalf("risk rejection = %v", err)
	}
	snapshot := owned.Snapshot()
	available := snapshot.Balances["USDT"].Available
	reserved := snapshot.Balances["USDT"].Reserved
	remaining, _ := liquidity.Available(input.Sizing.LiquidityDomain)
	if available.String() != "500" || reserved.String() != "0" || remaining.String() != "1" {
		t.Fatalf("released ownership = %s/%s/%s", available, reserved, remaining)
	}
}

func TestB3DeclaredProfileStrategyAllocatorRiskP99AtMost25Milliseconds(t *testing.T) {
	if raceInstrumentation {
		t.Skip("latency qualification is invalid under race instrumentation")
	}
	fixture := newB3LatencyFixture(t)
	durations := make([]time.Duration, 0, 200)
	for index := 0; index < 210; index++ {
		elapsed := measureB3PipelineLatency(t, fixture, index)
		if index >= 10 {
			durations = append(durations, elapsed)
		}
	}
	sort.Slice(durations, func(left, right int) bool { return durations[left] < durations[right] })
	p99 := durations[(len(durations)*99+99)/100-1]
	t.Logf("declared profile go=%s os=%s arch=%s cpus=%d samples=%d p99=%s", runtime.Version(),
		runtime.GOOS, runtime.GOARCH, runtime.NumCPU(), len(durations), p99)
	if p99 > 25*time.Millisecond {
		t.Fatalf("mean reversion + allocator + risk p99 %s exceeds 25ms", p99)
	}
}

type b3LatencyFixture struct {
	evaluator  *Evaluator
	baseline   Input
	adapter    *Adapter
	allocator  *portfolio.Allocator
	riskEngine *risk.Engine
}

func newB3LatencyFixture(t *testing.T) b3LatencyFixture {
	t.Helper()
	evaluator, _ := evaluatorFixture(t)
	baseline := baselineInput(t)
	adapter, _ := NewAdapter(evaluator)
	owned := meanReversionPortfolio(t)
	liquidity := portfolio.NewLiquidityPool()
	available, _ := domain.ParseQuantity("100")
	if err := liquidity.Open(baseline.Sizing.LiquidityDomain, available); err != nil {
		t.Fatal(err)
	}
	allocator, _ := portfolio.NewAllocator(owned, portfolio.NewAssetRegistry(), liquidity)
	riskEngine, _ := risk.NewEngine(&meanReversionRiskAudit{}, meanReversionRiskAlerts{})
	if err := riskEngine.ManualTransition(risk.StateNormal, recoveryEvidence(baseline.Now)); err != nil {
		t.Fatal(err)
	}
	return b3LatencyFixture{evaluator: evaluator, baseline: baseline, adapter: adapter,
		allocator: allocator, riskEngine: riskEngine}
}

func measureB3PipelineLatency(t *testing.T, fixture b3LatencyFixture, index int) time.Duration {
	t.Helper()
	input := cloneInput(fixture.baseline)
	input.Ordinal += uint64(index + 1)
	input.LogicalTime += uint64(index + 1)
	started := time.Now()
	decision, err := fixture.evaluator.Evaluate(input)
	if err != nil || decision.Candidate == nil {
		t.Fatal(err)
	}
	payload, err := fixture.adapter.portfolioCandidate(input, decision)
	if err != nil {
		t.Fatal(err)
	}
	var candidate portfolio.Candidate
	if json.Unmarshal(payload, &candidate) != nil {
		t.Fatal("candidate decode")
	}
	allocations, err := fixture.allocator.Allocate([]portfolio.Candidate{candidate})
	if err != nil || len(allocations) != 1 {
		t.Fatal(err)
	}
	policy := risk.DefaultGlobalPolicy()
	policy.State = risk.StateNormal
	approved, err := fixture.riskEngine.Evaluate(risk.Request{Intent: risk.IntentEntry, Policies: []risk.Policy{policy},
		Observations: healthyObservations(), EvaluatedAt: fixture.baseline.Now.Add(time.Nanosecond)})
	elapsed := time.Since(started)
	if err != nil || approved.Action != risk.ActionApprove {
		t.Fatal(err)
	}
	if err = fixture.allocator.Close(allocations[0], accounting.ReservationReleased); err != nil {
		t.Fatal(err)
	}
	return elapsed
}

func newOperationalPipeline(t *testing.T, normalRisk bool) (*OperationalProcessor, Input, *portfolio.Portfolio) {
	t.Helper()
	evaluator, input, owned, processor, _ := pipelineComponents(t, normalRisk)
	balances := func() (json.RawMessage, error) { return json.Marshal(owned.Snapshot()) }
	operational, err := NewOperationalProcessor(evaluator, processor, balances)
	if err != nil {
		t.Fatal(err)
	}
	return operational, input, owned
}

func pipelineComponents(t *testing.T, normalRisk bool) (*Evaluator, Input, *portfolio.Portfolio,
	*backtest.PipelineProcessor, *portfolio.LiquidityPool) {
	t.Helper()
	evaluator, _ := evaluatorFixture(t)
	input := baselineInput(t)
	adapter, _ := NewAdapter(evaluator)
	owned := meanReversionPortfolio(t)
	registry := portfolio.NewAssetRegistry()
	liquidity := portfolio.NewLiquidityPool()
	available, _ := domain.ParseQuantity("1")
	if err := liquidity.Open(input.Sizing.LiquidityDomain, available); err != nil {
		t.Fatal(err)
	}
	allocator, _ := portfolio.NewAllocator(owned, registry, liquidity)
	pipelineAllocator, _ := portfolio.NewPipelineAllocator(allocator)
	vault := portfolio.NewApprovalVault()
	riskEngine, _ := risk.NewEngine(&meanReversionRiskAudit{}, meanReversionRiskAlerts{})
	if normalRisk {
		if err := riskEngine.ManualTransition(risk.StateNormal, recoveryEvidence(input.Now)); err != nil {
			t.Fatal(err)
		}
	}
	pipelineRisk, _ := risk.NewPipelineEngine(riskEngine, vault, registry,
		meanReversionRiskInputs{at: input.Now.Add(time.Nanosecond)})
	strategyPlanner, _ := NewPlanner("shadow", input.Sizing.LiquidityDomain, adapter)
	planner, _ := portfolio.NewEligibilityPlanner(strategyPlanner, vault, registry)
	guard, _ := portfolio.NewBrokerGuard(owned, registry)
	broker := simulatedBroker(t, input, guard)
	processor, err := backtest.NewPipelineProcessor(backtest.PipelineDependencies{Strategy: adapter,
		Allocator: pipelineAllocator, Risk: pipelineRisk, Planner: planner, Broker: broker,
		Reduce: pipelineAllocator.ReduceSimulation, Metrics: func() backtest.Metrics {
			return backtest.Metrics{TotalNetReturn: "not_evaluated"}
		}})
	if err != nil {
		t.Fatal(err)
	}
	return evaluator, input, owned, processor, liquidity
}

func meanReversionPortfolio(t *testing.T) *portfolio.Portfolio {
	t.Helper()
	runID, _ := domain.NewRunID("mean-reversion-pipeline-run")
	portfolioID, _ := domain.NewPortfolioID("mean-reversion-pipeline-portfolio")
	accountID, _ := domain.NewVirtualAccountID("mean-reversion-pipeline-account")
	capital, _ := domain.ParseBalance("500")
	result, err := portfolio.InitializeMeanReversion(runID, portfolioID, accountID, strings.Repeat("a", 64),
		capital, accounting.NewMemoryJournal(),
		domain.EventTime{UTC: time.Date(2026, 7, 22, 8, 0, 0, 0, time.UTC), Sequence: 1})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func simulatedBroker(t *testing.T, input Input, guard simulation.BoundaryGuard) execution.Broker {
	t.Helper()
	randomness, _ := runtimecore.NewRandomness(make([]byte, 32))
	zeroRate, _ := domain.ParseRate("0")
	feeRate, _ := domain.ParseRate("0.001")
	zeroPercent, _ := domain.ParsePercent("0")
	partialRatio, _ := domain.ParsePercent("0.5")
	quantity, _ := domain.ParseQuantity("1")
	arrival := input.LogicalTime + uint64(time.Millisecond)
	bid, _ := domain.ParsePrice("95")
	state := simulation.BookState{Exchange: "binance", Instrument: input.Instrument, Version: 51,
		LogicalTime: arrival, Bids: []exchangecontracts.PriceLevel{{Price: bid, Quantity: quantity}},
		Asks: []exchangecontracts.PriceLevel{{Price: input.Sizing.FirstExecutablePrice, Quantity: quantity}}}
	models := simulation.BrokerModels{
		Fee: simulation.FeeModel{Version: "fee-v1", TakerRate: feeRate, MakerRate: zeroRate,
			RebateRate: zeroRate, DecimalScale: 18},
		Price: simulation.PriceModel{Version: "price-v1", Spread: zeroPercent, Slippage: zeroPercent,
			Impact: zeroPercent, AdverseSelection: zeroPercent, DecimalScale: 18},
		Latency: simulation.LatencyModel{Version: "latency-v1", Samples: []time.Duration{time.Millisecond}},
		Fill:    simulation.FillModel{Version: "fill-v1", PartialRatio: partialRatio, QuantityScale: 18},
	}
	broker, err := simulation.NewBroker(randomness, meanReversionTimeline{state: state},
		meanReversionMetadata{value: input.Sizing.InstrumentMetadata}, loggingGuard{t: t, delegate: guard},
		simulation.NewLiquidityLedger(), models)
	if err != nil {
		t.Fatal(err)
	}
	return loggingBroker{t: t, delegate: broker}
}

type loggingBroker struct {
	t        *testing.T
	delegate execution.Broker
}

func (broker loggingBroker) Submit(ctx context.Context, plan execution.SimulatedPlan) ([]execution.OrderEvent, error) {
	events, err := broker.delegate.Submit(ctx, plan)
	if err != nil {
		broker.t.Logf("simulated broker plan=%#v error=%v", plan, err)
	}
	return events, err
}

func (broker loggingBroker) Cancel(ctx context.Context, id domain.VirtualOrderID, reason string) ([]execution.OrderEvent, error) {
	return broker.delegate.Cancel(ctx, id, reason)
}

type loggingGuard struct {
	t        *testing.T
	delegate simulation.BoundaryGuard
}

func (guard loggingGuard) Authorize(leg execution.PlannedLeg, state simulation.BookState) (domain.Balance, error) {
	owned, err := guard.delegate.Authorize(leg, state)
	if err != nil {
		guard.t.Logf("broker guard leg side=%s instrument=%s error=%v", leg.Side, leg.Instrument.Symbol(), err)
	}
	return owned, err
}

type meanReversionTimeline struct{ state simulation.BookState }

func (timeline meanReversionTimeline) AtOrAfter(domain.Instrument, uint64) (simulation.BookState, bool, error) {
	return timeline.state, true, nil
}

type meanReversionMetadata struct{ value domain.InstrumentMetadata }

func (metadata meanReversionMetadata) Metadata(simulation.BookState) (domain.InstrumentMetadata, error) {
	return metadata.value, nil
}

type meanReversionRiskInputs struct{ at time.Time }

func (inputs meanReversionRiskInputs) Current() (risk.Observations, []risk.Policy, time.Time, error) {
	policy := risk.DefaultGlobalPolicy()
	policy.State = risk.StateNormal
	return healthyObservations(), []risk.Policy{policy}, inputs.at, nil
}

func healthyObservations() risk.Observations {
	percentage := func(value string) *domain.Percent { parsed, _ := domain.ParsePercent(value); return &parsed }
	openOrders, quality := uint32(0), uint8(100)
	age, lag, drift := time.Millisecond, time.Millisecond, time.Millisecond
	problem := false
	return risk.Observations{AccountDrawdown: percentage("0"), UTCDayLoss: percentage("0"),
		Rolling24HourLoss: percentage("0"), StrategyLoss: percentage("0"), AssetExposure: percentage("0"),
		CombinedExposure: percentage("0"), ExchangeExposure: percentage("0"), Reserve: percentage("1"),
		ReservedCapital: percentage("0"), Spread: percentage("0"), Slippage: percentage("0"),
		OpenOrders: &openOrders, BookAge: &age, QueueLag: &lag, ClockDrift: &drift, QualityScore: &quality,
		Health: risk.HealthInputs{Gap: &problem, StaleData: &problem, ReconciliationFault: &problem,
			AccountingFault: &problem, UnknownOrder: &problem, PersistenceFault: &problem,
			DiskFault: &problem, APIError: &problem, LeaseLost: &problem}}
}

func recoveryEvidence(at time.Time) risk.RecoveryEvidence {
	return risk.RecoveryEvidence{Reconciled: true, PersistenceHealthy: true, BooksFresh: true,
		UnknownOrdersResolved: true, Reauthenticated: true, AuditDurable: true,
		Actor: "owner", Reason: "B3 pipeline qualification", At: at}
}

type meanReversionRiskAudit struct{}

func (*meanReversionRiskAudit) Append(risk.AuditEvent) error { return nil }

type meanReversionRiskAlerts struct{}

func (meanReversionRiskAlerts) Emit(string, risk.Action, risk.State) error { return nil }
