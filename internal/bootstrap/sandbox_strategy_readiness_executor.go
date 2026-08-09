package bootstrap

import (
	"context"
	"fmt"
	"time"

	"axiom/internal/config"
	"axiom/internal/domain"
	"axiom/internal/sandbox"
	"axiom/internal/strategies/meanreversion"
	"axiom/internal/strategies/trend"
)

// SandboxStrategyReadinessExecutor verifies the real, immutable Trend and
// Mean Reversion rule graphs against fresh public input. It deliberately does
// not create a candidate or access an account, broker, dispatcher, or order
// adapter. Until a durable session-owned position and sizing projection is
// available, it reports the precise safe waiting state instead of inventing
// those facts from an account balance.
type SandboxStrategyReadinessExecutor struct {
	market *sandbox.StrategyMarketInputReader
}

// NewSandboxStrategyReadinessExecutor constructs a credential-free scheduler
// executor. StrategyMarketData has no private or order-capable methods.
func NewSandboxStrategyReadinessExecutor(
	market sandbox.StrategyMarketData,
	clock domain.Clock,
) (*SandboxStrategyReadinessExecutor, error) {
	reader, err := sandbox.NewStrategyMarketInputReader(market, clock)
	if err != nil {
		return nil, fmt.Errorf("sandbox_strategy_readiness_executor_invalid")
	}
	return &SandboxStrategyReadinessExecutor{market: reader}, nil
}

// EvaluateStrategySession produces an immutable scheduler outcome only. The
// eventual decision executor will add durable position, model, sizing, and
// risk facts before it invokes the shared strategy pipeline.
func (executor *SandboxStrategyReadinessExecutor) EvaluateStrategySession(
	ctx context.Context,
	work sandbox.StrategySessionWork,
	record sandbox.StrategySessionConfiguration,
	lease sandbox.StrategySessionExecutionLease,
	now time.Time,
) (sandbox.StrategySessionEvaluation, error) {
	if executor == nil || executor.market == nil || ctx == nil ||
		work.ValidAt(now) != nil || lease.ValidFor(work) != nil || now.IsZero() || now.Location() != time.UTC {
		return sandbox.StrategySessionEvaluation{}, fmt.Errorf("sandbox_strategy_readiness_input_invalid")
	}
	product, err := decodeSandboxStrategyConfiguration(work, record, now)
	if err != nil {
		return sandbox.StrategySessionEvaluation{}, err
	}
	if work.Strategy == sandbox.StrategyTriangular ||
		work.Strategy == sandbox.StrategyCrossExchangeArbitrage {
		return sandbox.NewStrategySessionEvaluation(
			work, sandbox.StrategySessionEvaluationWaiting, "waiting_for_multileg_facts", now,
		)
	}
	requirements, err := sandboxStrategyReadinessRequirements(work.Strategy, product)
	if err != nil {
		return sandbox.StrategySessionEvaluation{}, err
	}
	instrument, err := sandboxStrategyReadinessInstrument(work.Instrument)
	if err != nil {
		return sandbox.StrategySessionEvaluation{}, err
	}
	if _, err = executor.market.Read(ctx, instrument, requirements); err != nil {
		return sandbox.NewStrategySessionEvaluation(
			work, sandbox.StrategySessionEvaluationWaiting, "waiting_for_public_market_data", now,
		)
	}
	return sandbox.NewStrategySessionEvaluation(
		work, sandbox.StrategySessionEvaluationWaiting, "waiting_for_position_projection", now,
	)
}

func sandboxStrategyReadinessRequirements(
	strategy string,
	product config.Configuration,
) (sandbox.StrategyMarketRequirements, error) {
	switch strategy {
	case sandbox.StrategyTrend:
		parsed, err := trend.NewConfiguration(product.Trend)
		if err != nil {
			return sandbox.StrategyMarketRequirements{}, fmt.Errorf("sandbox_strategy_readiness_invalid")
		}
		if _, err = trend.NewEvaluator(parsed); err != nil {
			return sandbox.StrategyMarketRequirements{}, fmt.Errorf("sandbox_strategy_readiness_invalid")
		}
		return sandbox.StrategyMarketRequirements{CandleIntervals: []string{"4h"},
			CandleLimit: 1000, BookDepth: 50, MaximumBookAge: 250 * time.Millisecond}, nil
	case sandbox.StrategyMeanReversion:
		parsed, err := meanreversion.NewConfiguration(product.MeanReversion)
		if err != nil {
			return sandbox.StrategyMarketRequirements{}, fmt.Errorf("sandbox_strategy_readiness_invalid")
		}
		if _, err = meanreversion.NewEvaluator(parsed); err != nil {
			return sandbox.StrategyMarketRequirements{}, fmt.Errorf("sandbox_strategy_readiness_invalid")
		}
		return sandbox.StrategyMarketRequirements{CandleIntervals: []string{"1h", "4h"},
			CandleLimit: 1000, BookDepth: 50, MaximumBookAge: 250 * time.Millisecond}, nil
	default:
		return sandbox.StrategyMarketRequirements{}, fmt.Errorf("sandbox_strategy_readiness_invalid")
	}
}

func sandboxStrategyReadinessInstrument(symbol string) (domain.Instrument, error) {
	switch symbol {
	case "BTCUSDT":
		return domain.NewSpotInstrument("BTC", "USDT")
	case "ETHUSDT":
		return domain.NewSpotInstrument("ETH", "USDT")
	default:
		return domain.Instrument{}, fmt.Errorf("sandbox_strategy_readiness_invalid")
	}
}
