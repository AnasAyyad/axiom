package bootstrap

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"axiom/internal/config"
	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/sandbox"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	sandboxEngineLeaseTTL          = 60 * time.Second
	sandboxEngineLeaseRenewal      = 20 * time.Second
	sandboxEngineDispatchInterval  = time.Second
	sandboxEngineRecoveryInterval  = 5 * time.Second
	sandboxEngineReconcileInterval = 30 * time.Second
)

type sandboxEngineAdapter interface {
	sandbox.AccountReader
	sandbox.OrderBroker
	sandbox.Reconciler
	sandbox.SnapshotReconciler
	StrategyMarketData() (sandbox.StrategyMarketData, error)
	StrategyEligibility(
		context.Context,
	) ([]exchangecontracts.CollectorHealthSnapshot, error)
	StartupEligibility(
		context.Context,
	) (exchangecontracts.CollectorHealthSnapshot, error)
}

type sandboxEngineRoleWork struct {
	pool     *pgxpool.Pool
	runtime  config.Runtime
	product  config.Configuration
	exchange sandbox.Exchange
	ready    atomic.Bool
}

func newSandboxEngineRoleWork(
	_ context.Context,
	pool *pgxpool.Pool,
	runtimeConfig config.Runtime,
	product config.Configuration,
	exchange string,
) (roleWork, error) {
	if pool == nil || product.SchemaVersion != config.SchemaVersionSandboxRuntime {
		return nil, fmt.Errorf("sandbox_engine_configuration_invalid")
	}
	selected, err := sandboxEngineExchange(exchange, product.Mode)
	if err != nil {
		return nil, err
	}
	return &sandboxEngineRoleWork{
		pool: pool, runtime: runtimeConfig,
		product: product, exchange: selected,
	}, nil
}

func sandboxEngineExchange(
	exchange string,
	mode config.ExecutionMode,
) (sandbox.Exchange, error) {
	switch exchange {
	case "binance":
		if mode == config.ModeTestnet {
			return sandbox.ExchangeBinance, nil
		}
	case "bybit":
		if mode == config.ModeDemo {
			return sandbox.ExchangeBybit, nil
		}
	default:
		return "", fmt.Errorf("sandbox_engine_exchange_invalid")
	}
	return "", fmt.Errorf("sandbox_engine_mode_invalid")
}

// Ready reports whether the engine completed every locked startup stage.
func (work *sandboxEngineRoleWork) Ready() bool {
	return work.ready.Load()
}

// Run executes the locked startup gate and maintains one credential-owning
// sandbox engine until cancellation or a fail-closed runtime error.
func (work *sandboxEngineRoleWork) Run(
	ctx context.Context,
	logger *slog.Logger,
) error {
	work.ready.Store(false)
	startup, err := work.startSandboxEngine(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = startup.source.Close() }()
	work.ready.Store(true)
	startup.logReady(logger)
	runErr := work.runSandboxEngineLoop(
		ctx, startup.store, startup.account, startup.owner,
		startup.fence, startup.adapter, startup.source,
		startup.dispatcher, startup.recovery, startup.scheduler,
	)
	work.ready.Store(false)
	startup.lockAfterRun()
	return runErr
}
