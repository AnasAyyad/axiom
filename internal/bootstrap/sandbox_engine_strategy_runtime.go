package bootstrap

import (
	"context"
	"fmt"
	"time"

	"axiom/internal/config"
	"axiom/internal/domain"
	"axiom/internal/sandbox"
	postgresstore "axiom/internal/storage/postgres"

	"github.com/jackc/pgx/v5/pgxpool"
)

// newSandboxEngineStrategyScheduler installs all automatic sandbox strategy
// families in the credential-owning engine. The private adapter is used only
// during construction to copy sanitized filter facts; no executor receives it
// or can submit an order directly.
func newSandboxEngineStrategyScheduler(
	ctx context.Context,
	pool *pgxpool.Pool,
	store *postgresstore.SandboxRuntimeDispatcherStore,
	adapter sandboxEngineAdapter,
	market sandbox.StrategyMarketData,
	product config.Configuration,
	exchange sandbox.Exchange,
	account sandbox.AccountID,
	epoch uint64,
	owner string,
	fence uint64,
) (sandboxStrategyScheduler, error) {
	if ctx == nil || pool == nil || store == nil || adapter == nil || market == nil ||
		account == "" || epoch == 0 || owner == "" || fence == 0 {
		return nil, fmt.Errorf("sandbox_engine_strategy_runtime_invalid")
	}
	switches, enabled := canarySwitches(product, exchange)
	if !enabled {
		return nil, fmt.Errorf("sandbox_engine_strategy_runtime_disabled")
	}
	clock := &domain.SystemClock{}
	singleExecutor, riskRuntime, err := newSandboxSingleStrategyExecutor(pool, store, market, clock, switches)
	if err != nil {
		return nil, err
	}
	sagaExecutor, crossCache, err := newSandboxSagaStrategyExecutor(
		ctx, adapter, market, clock, product, exchange, store, riskRuntime)
	if err != nil {
		return nil, err
	}
	executor, err := NewSandboxStrategyExecutorRouter(singleExecutor, sagaExecutor)
	if err != nil {
		return nil, fmt.Errorf("sandbox_engine_strategy_executor_unavailable")
	}
	scheduler, err := sandbox.NewStrategySessionScheduler(store, store, executor, store, clock,
		account, epoch, owner, fence)
	if err != nil {
		return nil, fmt.Errorf("sandbox_engine_strategy_scheduler_unavailable")
	}
	return &sandboxRefreshingStrategyScheduler{market: crossCache, scheduler: scheduler}, nil
}

func newSandboxSingleStrategyExecutor(pool *pgxpool.Pool, store *postgresstore.SandboxRuntimeDispatcherStore,
	market sandbox.StrategyMarketData, clock domain.Clock, switches [4]bool,
) (*SandboxStrategyDecisionExecutor, *postgresstore.SandboxRiskRuntime, error) {
	positions, err := NewSandboxStrategyPositionProjector(store, store)
	if err != nil {
		return nil, nil, fmt.Errorf("sandbox_engine_strategy_position_projector_unavailable")
	}
	facts, err := NewSandboxStrategySizingFactsReader(store)
	if err != nil {
		return nil, nil, fmt.Errorf("sandbox_engine_strategy_facts_unavailable")
	}
	admission, err := NewSandboxStrategyAdmissionAdapter(store, switches)
	if err != nil {
		return nil, nil, fmt.Errorf("sandbox_engine_strategy_admission_unavailable")
	}
	riskRuntime, err := postgresstore.NewSandboxRiskRuntime(pool, clock)
	if err != nil {
		return nil, nil, fmt.Errorf("sandbox_engine_strategy_risk_runtime_unavailable")
	}
	pipelines, err := NewSandboxStrategyPipelineFactory(store, riskRuntime, store)
	if err != nil {
		return nil, nil, fmt.Errorf("sandbox_engine_strategy_pipeline_unavailable")
	}
	executor, err := NewSandboxStrategyDecisionExecutor(market, clock, positions, facts, store,
		admission, store, pipelines, store)
	if err != nil {
		return nil, nil, fmt.Errorf("sandbox_engine_strategy_executor_unavailable")
	}
	return executor, riskRuntime, nil
}

func newSandboxSagaStrategyExecutor(ctx context.Context, adapter sandboxEngineAdapter,
	market sandbox.StrategyMarketData, clock domain.Clock, product config.Configuration,
	exchange sandbox.Exchange, store *postgresstore.SandboxRuntimeDispatcherStore,
	riskRuntime *postgresstore.SandboxRiskRuntime,
) (*SandboxStrategySagaDecisionExecutor, *SandboxSagaMarketCacheSet, error) {
	triangularCache, crossCache, err := newSandboxEngineSagaMarketRuntime(
		ctx, adapter, market, clock, product, exchange,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("sandbox_engine_strategy_saga_market_unavailable")
	}
	triangularMarket, err := NewSandboxSagaMarketInputReader(triangularCache)
	if err != nil {
		return nil, nil, fmt.Errorf("sandbox_engine_strategy_saga_market_unavailable")
	}
	crossMarket, err := NewSandboxSagaMarketInputReader(crossCache)
	if err != nil {
		return nil, nil, fmt.Errorf("sandbox_engine_strategy_saga_market_unavailable")
	}
	sagaMarket := &sandboxStrategySagaMarketRouter{triangular: triangularMarket, cross: crossMarket}
	sagaFacts, err := NewSandboxStrategySagaFactsReader(store, product)
	if err != nil {
		return nil, nil, fmt.Errorf("sandbox_engine_strategy_saga_facts_unavailable")
	}
	sagaPipelines, err := NewSandboxStrategySagaPipelineFactory(riskRuntime, store)
	if err != nil {
		return nil, nil, fmt.Errorf("sandbox_engine_strategy_saga_pipeline_unavailable")
	}
	sagaExecutor, err := NewSandboxStrategySagaDecisionExecutor(
		sagaFacts, sagaMarket, store, sagaPipelines, store,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("sandbox_engine_strategy_saga_executor_unavailable")
	}
	return sagaExecutor, crossCache, nil
}

type sandboxStrategySagaMarketRouter struct {
	triangular *SandboxSagaMarketInputReader
	cross      *SandboxSagaMarketInputReader
}

// ReadTriangular returns coherent exchange-local books for a triangular cycle.
func (router *sandboxStrategySagaMarketRouter) ReadTriangular(
	ctx context.Context,
	work sandbox.StrategySessionWork,
	now time.Time,
) (SandboxTriangularMarketInput, error) {
	if router == nil || router.triangular == nil {
		return SandboxTriangularMarketInput{}, fmt.Errorf("sandbox_engine_strategy_saga_market_unavailable")
	}
	return router.triangular.ReadTriangular(ctx, work, now)
}

// ReadCrossExchange returns a coherent paired Binance and Bybit generation.
func (router *sandboxStrategySagaMarketRouter) ReadCrossExchange(
	ctx context.Context,
	work sandbox.StrategySessionWork,
	now time.Time,
) (SandboxCrossExchangeMarketInput, error) {
	if router == nil || router.cross == nil {
		return SandboxCrossExchangeMarketInput{}, fmt.Errorf("sandbox_engine_strategy_saga_market_unavailable")
	}
	return router.cross.ReadCrossExchange(ctx, work, now)
}

type sandboxSagaMarketRefresher interface {
	Refresh(context.Context) error
}

// sandboxRefreshingStrategyScheduler captures a complete public generation
// immediately before the scheduler fixes its decision instant. A public
// refresh failure is surfaced per session as a semantic synchronized-books
// wait; it does not prevent independent one-leg sessions from evaluating.
type sandboxRefreshingStrategyScheduler struct {
	market    sandboxSagaMarketRefresher
	scheduler sandboxStrategyScheduler
}

// Tick refreshes public market caches before one scheduled evaluation pass.
func (scheduler *sandboxRefreshingStrategyScheduler) Tick(
	ctx context.Context,
) (sandbox.StrategySessionScheduleResult, error) {
	if scheduler == nil || scheduler.market == nil || scheduler.scheduler == nil || ctx == nil {
		return sandbox.StrategySessionScheduleResult{}, fmt.Errorf("sandbox_engine_strategy_scheduler_unavailable")
	}
	_ = scheduler.market.Refresh(ctx)
	return scheduler.scheduler.Tick(ctx)
}
