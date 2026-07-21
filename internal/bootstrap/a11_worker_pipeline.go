package bootstrap

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"axiom/internal/accounting"
	"axiom/internal/backtest"
	"axiom/internal/config"
	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/execution"
	"axiom/internal/portfolio"
	"axiom/internal/replay"
	"axiom/internal/risk"
	runtimecore "axiom/internal/runtime"
	"axiom/internal/simulation"
	postgresstore "axiom/internal/storage/postgres"
	"axiom/internal/strategies/trend"

	"github.com/jackc/pgx/v5/pgxpool"
)

func newA11WorkerRoleWork(
	pool *pgxpool.Pool,
	runtimeConfig config.Runtime,
) (*workerRoleWork, error) {
	materialize, err := postgresstore.NewA11JobMaterializer(pool, runtimeConfig.Recorder.Root)
	if err != nil {
		return nil, err
	}
	store, err := postgresstore.NewA11JobStore(pool, runtimeConfig.InstanceID, &domain.SystemClock{}, materialize)
	if err != nil {
		return nil, err
	}
	worker, err := backtest.NewWorker(store, newA11OperationalProcessor, replay.RealPacer{})
	if err != nil {
		return nil, err
	}
	return newWorkerRoleWork(worker, time.Second)
}

func newA11OperationalProcessor(claim backtest.JobClaim) (backtest.Processor, error) {
	return newA11OperationalProcessorWithPortfolio(claim, nil)
}

func newA11OperationalProcessorWithPortfolio(claim backtest.JobClaim,
	owned *portfolio.Portfolio) (backtest.Processor, error) {
	components, err := newA11WorkerComponents(claim, owned)
	if err != nil {
		return nil, err
	}
	pipeline, err := composeA11WorkerPipeline(claim, components)
	if err != nil {
		return nil, err
	}
	operational, err := trend.NewOperationalProcessor(components.evaluator, pipeline, components.owned)
	if err != nil {
		return nil, err
	}
	return &a11InputAwareProcessor{inputs: components.inputs, delegate: operational}, nil
}

type a11WorkerComponents struct {
	evaluator *trend.Evaluator
	adapter   *trend.Adapter
	owned     *portfolio.Portfolio
	registry  *portfolio.MemoryAssetRegistry
	allocator *portfolio.PipelineAllocator
	inputs    *a11DecisionInputContext
}

func newA11WorkerComponents(claim backtest.JobClaim, owned *portfolio.Portfolio) (a11WorkerComponents, error) {
	if err := config.Validate(claim.Configuration); err != nil || claim.Configuration.Trend.StrategyVersion != "trend.v1a.1" {
		return a11WorkerComponents{}, fmt.Errorf("a11_worker_configuration_invalid")
	}
	configuredTrend, err := trend.NewConfiguration(claim.Configuration.Trend)
	if err != nil {
		return a11WorkerComponents{}, err
	}
	evaluator, err := trend.NewEvaluator(configuredTrend)
	if err != nil {
		return a11WorkerComponents{}, err
	}
	adapter, err := trend.NewAdapter(evaluator)
	if err != nil {
		return a11WorkerComponents{}, err
	}
	if owned == nil {
		owned, err = newA11OfflinePortfolio(claim)
		if err != nil {
			return a11WorkerComponents{}, err
		}
	}
	registry := portfolio.NewAssetRegistry()
	liquidity := portfolio.NewLiquidityPool()
	availableDepth, _ := domain.ParseQuantity("1000000000")
	if err = liquidity.Open(claim.Manifest.Models.LiquidityDomain, availableDepth); err != nil {
		return a11WorkerComponents{}, err
	}
	allocator, err := portfolio.NewAllocator(owned, registry, liquidity)
	if err != nil {
		return a11WorkerComponents{}, err
	}
	pipelineAllocator, err := portfolio.NewPipelineAllocator(allocator)
	if err != nil {
		return a11WorkerComponents{}, err
	}
	return a11WorkerComponents{evaluator: evaluator, adapter: adapter, owned: owned, registry: registry,
		allocator: pipelineAllocator, inputs: &a11DecisionInputContext{}}, nil
}

func composeA11WorkerPipeline(claim backtest.JobClaim, components a11WorkerComponents) (*backtest.PipelineProcessor, error) {
	riskEngine, err := risk.NewEngine(&a11RunRiskAudit{}, a11RunRiskAlerts{})
	if err != nil {
		return nil, err
	}
	recoveryAt := time.Unix(0, 1).UTC()
	if err = riskEngine.ManualTransition(risk.StateNormal, risk.RecoveryEvidence{Reconciled: true,
		PersistenceHealthy: true, BooksFresh: true, UnknownOrdersResolved: true, Reauthenticated: true,
		AuditDurable: true, Actor: "offline-worker", Reason: "verified immutable replay inputs", At: recoveryAt}); err != nil {
		return nil, err
	}
	vault := portfolio.NewApprovalVault()
	pipelineRisk, err := risk.NewPipelineEngine(riskEngine, vault, components.registry, components.inputs)
	if err != nil {
		return nil, err
	}
	trendPlanner, err := trend.NewPlanner(claim.Manifest.Mode, claim.Manifest.Models.LiquidityDomain, components.adapter)
	if err != nil {
		return nil, err
	}
	planner, err := portfolio.NewEligibilityPlanner(trendPlanner, vault, components.registry)
	if err != nil {
		return nil, err
	}
	guard, err := portfolio.NewBrokerGuard(components.owned, components.registry)
	if err != nil {
		return nil, err
	}
	broker, err := newA11DynamicBroker(claim, components.inputs, guard)
	if err != nil {
		return nil, err
	}
	return backtest.NewPipelineProcessor(backtest.PipelineDependencies{Strategy: components.adapter,
		Allocator: components.allocator, Risk: pipelineRisk, Planner: planner, Broker: broker,
		Reduce: components.allocator.ReduceSimulation, Metrics: func() backtest.Metrics { return backtest.Metrics{} }})
}

func newA11OfflinePortfolio(claim backtest.JobClaim) (*portfolio.Portfolio, error) {
	portfolioID, err := domain.NewPortfolioID("offline-portfolio-" + claim.ID)
	if err != nil {
		return nil, err
	}
	accountID, err := domain.NewVirtualAccountID("offline-account-" + claim.ID)
	if err != nil {
		return nil, err
	}
	capital, err := domain.ParseBalance(claim.Configuration.Portfolio.StartingCapital.Value)
	if err != nil {
		return nil, err
	}
	return portfolio.InitializeTrend(claim.Manifest.RunID, portfolioID, accountID, claim.Manifest.ConfigurationHash,
		capital, accounting.NewMemoryJournal(), domain.EventTime{UTC: time.Unix(0, 1).UTC(), Sequence: 1})
}

type a11InputAwareProcessor struct {
	inputs   *a11DecisionInputContext
	delegate backtest.Processor
}

// Process updates the per-event evidence providers before running the delegate.
func (processor *a11InputAwareProcessor) Process(ctx context.Context, event replay.Event) (backtest.EventResult, error) {
	var input trend.Input
	if json.Unmarshal(event.Canonical, &input) != nil || processor.inputs.Set(input) != nil {
		return backtest.EventResult{}, fmt.Errorf("a11_decision_input_invalid")
	}
	return processor.delegate.Process(ctx, event)
}

// Metrics returns the delegate's canonical result metrics.
func (processor *a11InputAwareProcessor) Metrics() backtest.Metrics {
	return processor.delegate.Metrics()
}

type a11DecisionInputContext struct {
	mutex sync.RWMutex
	input trend.Input
	set   bool
}

// Set replaces the exact immutable input visible to downstream stage adapters.
func (inputs *a11DecisionInputContext) Set(input trend.Input) error {
	if input.Ordinal == 0 || input.LogicalTime == 0 || input.Instrument.Product != domain.ProductSpot ||
		input.Sizing.InstrumentMetadata.Instrument != input.Instrument || input.Evidence.MarketViewRevision == 0 {
		return fmt.Errorf("a11_decision_input_invalid")
	}
	inputs.mutex.Lock()
	inputs.input, inputs.set = input, true
	inputs.mutex.Unlock()
	return nil
}

func (inputs *a11DecisionInputContext) current() (trend.Input, error) {
	inputs.mutex.RLock()
	defer inputs.mutex.RUnlock()
	if !inputs.set {
		return trend.Input{}, fmt.Errorf("a11_decision_input_unavailable")
	}
	return inputs.input, nil
}

// Current returns the conservative risk projection for the current decision input.
func (inputs *a11DecisionInputContext) Current() (risk.Observations, []risk.Policy, time.Time, error) {
	input, err := inputs.current()
	if err != nil || input.Now.IsZero() || input.Now.Location() != time.UTC {
		return risk.Observations{}, nil, time.Time{}, fmt.Errorf("a11_risk_input_unavailable")
	}
	zero := a11Percent("0")
	one := a11Percent("1")
	openOrders, quality := uint32(0), uint8(100)
	queueLag, clockDrift := time.Duration(0), time.Duration(0)
	problem := !input.MarketHealthy
	policy := risk.DefaultGlobalPolicy()
	policy.State = risk.StateNormal
	observations := risk.Observations{AccountDrawdown: &zero, UTCDayLoss: &zero, Rolling24HourLoss: &zero,
		StrategyLoss: &zero, AssetExposure: &zero, CombinedExposure: &zero, ExchangeExposure: &zero,
		Reserve: &one, ReservedCapital: &zero, Spread: &zero, Slippage: &zero, OpenOrders: &openOrders,
		BookAge: &input.BookAge, QueueLag: &queueLag, ClockDrift: &clockDrift, QualityScore: &quality,
		Health: risk.HealthInputs{Gap: &problem, StaleData: &problem, ReconciliationFault: &problem,
			AccountingFault: &problem, UnknownOrder: &problem, PersistenceFault: &problem,
			DiskFault: &problem, APIError: &problem, LeaseLost: &problem}}
	return observations, []risk.Policy{policy}, input.Now, nil
}

func a11Percent(value string) domain.Percent {
	parsed, _ := domain.ParsePercent(value)
	return parsed
}

type a11DynamicBroker struct {
	claim      backtest.JobClaim
	inputs     *a11DecisionInputContext
	guard      simulation.BoundaryGuard
	randomness *runtimecore.Randomness
	liquidity  *simulation.LiquidityLedger
}

func newA11DynamicBroker(claim backtest.JobClaim, inputs *a11DecisionInputContext,
	guard simulation.BoundaryGuard) (*a11DynamicBroker, error) {
	seed, err := hex.DecodeString(claim.Manifest.Seed)
	if err != nil {
		return nil, fmt.Errorf("a11_worker_seed_invalid")
	}
	randomness, err := runtimecore.NewRandomness(seed)
	if err != nil {
		return nil, err
	}
	return &a11DynamicBroker{claim: claim, inputs: inputs, guard: guard, randomness: randomness,
		liquidity: simulation.NewLiquidityLedger()}, nil
}

// Submit builds the exact per-input simulation models and performs no network I/O.
func (broker *a11DynamicBroker) Submit(ctx context.Context, plan execution.SimulatedPlan) ([]execution.OrderEvent, error) {
	input, err := broker.inputs.current()
	if err != nil || input.Evidence.FeeModelID != broker.claim.Configuration.Models.Fee ||
		input.Evidence.LatencyModelID != broker.claim.Configuration.Models.Latency {
		return nil, fmt.Errorf("a11_simulation_model_mismatch")
	}
	models, err := a11BrokerModels(input, broker.claim.Manifest.Models)
	if err != nil {
		return nil, err
	}
	simulated, err := simulation.NewBroker(broker.randomness, a11InputTimeline{input: input},
		a11InputMetadata{input: input}, broker.guard, broker.liquidity, models)
	if err != nil {
		return nil, err
	}
	events, err := simulated.Submit(ctx, plan)
	if err != nil {
		return nil, err
	}
	for index := range events {
		events[index].OccurredAt = input.Now.Add(time.Duration(index) * time.Nanosecond)
	}
	return events, nil
}

// Cancel fails closed because this bounded broker leaves no nonterminal orders.
func (broker *a11DynamicBroker) Cancel(context.Context, domain.VirtualOrderID, string) ([]execution.OrderEvent, error) {
	return nil, fmt.Errorf("a11_simulation_order_not_active")
}

func a11BrokerModels(input trend.Input, namespace backtest.ModelNamespace) (simulation.BrokerModels, error) {
	if namespace.FillDomain == "" || input.Evidence.FillModelID != namespace.FillDomain {
		return simulation.BrokerModels{}, fmt.Errorf("a11_fill_model_mismatch")
	}
	zeroRate, _ := domain.ParseRate("0")
	zeroPercent, _ := domain.ParsePercent("0")
	onePercent, _ := domain.ParsePercent("1")
	latency := time.Duration(0)
	if input.Evidence.LatencyModelID != "fixed-zero-v1" {
		return simulation.BrokerModels{}, fmt.Errorf("a11_latency_model_unsupported")
	}
	return simulation.BrokerModels{
		Fee: simulation.FeeModel{Version: input.Evidence.FeeModelID, TakerRate: input.Sizing.EntryFeeRate,
			MakerRate: zeroRate, RebateRate: zeroRate, DecimalScale: 18},
		Price: simulation.PriceModel{Version: "recorded-first-executable-v1", Spread: zeroPercent,
			Slippage: zeroPercent, Impact: zeroPercent, AdverseSelection: zeroPercent, DecimalScale: 18},
		Latency: simulation.LatencyModel{Version: input.Evidence.LatencyModelID, Samples: []time.Duration{latency}},
		Fill:    simulation.FillModel{Version: input.Evidence.FillModelID, PartialRatio: onePercent, QuantityScale: 18},
	}, nil
}

type a11InputTimeline struct{ input trend.Input }

// AtOrAfter returns only the recorded first executable observation.
func (timeline a11InputTimeline) AtOrAfter(instrument domain.Instrument, logical uint64) (simulation.BookState, bool, error) {
	if instrument != timeline.input.Instrument || timeline.input.Sizing.FirstExecutablePrice.String() == "0" {
		return simulation.BookState{}, false, nil
	}
	quantity, _ := domain.ParseQuantity("1000000000")
	level := exchangecontracts.PriceLevel{Price: timeline.input.Sizing.FirstExecutablePrice, Quantity: quantity}
	if logical <= timeline.input.LogicalTime {
		logical = timeline.input.LogicalTime + 1
	}
	return simulation.BookState{Exchange: "binance", Instrument: instrument,
		Version: timeline.input.Evidence.MarketViewRevision, LogicalTime: logical,
		Bids: []exchangecontracts.PriceLevel{level}, Asks: []exchangecontracts.PriceLevel{level}}, true, nil
}

type a11InputMetadata struct{ input trend.Input }

// Metadata returns the exact versioned filter set embedded in the input.
func (metadata a11InputMetadata) Metadata(state simulation.BookState) (domain.InstrumentMetadata, error) {
	if state.Instrument != metadata.input.Instrument {
		return domain.InstrumentMetadata{}, fmt.Errorf("a11_metadata_identity_mismatch")
	}
	return metadata.input.Sizing.InstrumentMetadata, nil
}

type a11RunRiskAudit struct{}

// Append accepts the deterministic run-local transition captured by the run output.
func (*a11RunRiskAudit) Append(risk.AuditEvent) error { return nil }

type a11RunRiskAlerts struct{}

// Emit records no external side effect for a credential-free offline run.
func (a11RunRiskAlerts) Emit(string, risk.Action, risk.State) error { return nil }

var _ execution.Broker = (*a11DynamicBroker)(nil)
var _ risk.ObservationProvider = (*a11DecisionInputContext)(nil)
var _ backtest.Processor = (*a11InputAwareProcessor)(nil)
