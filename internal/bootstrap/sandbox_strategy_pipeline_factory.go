package bootstrap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"axiom/internal/backtest"
	"axiom/internal/domain"
	"axiom/internal/execution"
	"axiom/internal/portfolio"
	"axiom/internal/risk"
	"axiom/internal/sandbox"
	"axiom/internal/strategies/meanreversion"
	"axiom/internal/strategies/trend"
)

// SandboxStrategyRiskEngineSource restores the current durable central posture
// for exactly one immutable evaluation. Implementations must not auto-unpause
// or reuse a process-local state after another engine may have escalated it.
type SandboxStrategyRiskEngineSource interface {
	SandboxStrategyRiskEngine(context.Context, time.Time) (*risk.Engine, error)
}

// SandboxStrategyPipelineFactory constructs one in-process shared pipeline
// from an exact immutable evaluation snapshot. The central risk engine is
// restored for each evaluation; this factory cannot normalize its state and
// refuses to compose automatic work unless the durable posture is NORMAL.
type SandboxStrategyPipelineFactory struct {
	observations sandbox.StrategyRiskObservationSource
	riskEngines  SandboxStrategyRiskEngineSource
	store        sandbox.DispatcherRepository
}

// NewSandboxStrategyPipelineFactory creates the fail-closed dependency
// source. The observation source must return complete persisted evidence; the
// dispatcher repository remains the only durable plan boundary.
func NewSandboxStrategyPipelineFactory(
	observations sandbox.StrategyRiskObservationSource,
	riskEngines SandboxStrategyRiskEngineSource,
	store sandbox.DispatcherRepository,
) (*SandboxStrategyPipelineFactory, error) {
	if observations == nil || riskEngines == nil || store == nil {
		return nil, fmt.Errorf("sandbox_strategy_pipeline_factory_invalid")
	}
	return &SandboxStrategyPipelineFactory{observations: observations, riskEngines: riskEngines, store: store}, nil
}

// SandboxStrategyPipelineDependencies snapshots every shared stage before
// Strategy.Evaluate. No database or network call is introduced between the
// strategy, allocator, central risk, planner, and durable plan builder.
func (factory *SandboxStrategyPipelineFactory) SandboxStrategyPipelineDependencies(
	ctx context.Context,
	admission sandbox.StrategySessionAdmission,
	facts SandboxStrategySizingFacts,
	market sandbox.StrategyMarketInput,
	inventory sandbox.StrategyOwnedInventory,
	strategy backtest.Strategy,
) (sandbox.StrategyPipelineDependencies, error) {
	if factory == nil || factory.observations == nil || factory.riskEngines == nil ||
		factory.store == nil || ctx == nil || strategy == nil || admission.Valid() != nil ||
		facts.ValidFor(admission.Work, admission.ApprovedAt) != nil ||
		inventory.ValidFor(admission, market.Instrument.Base) != nil ||
		market.Instrument.Symbol() != admission.Work.Instrument ||
		sandbox.StrategyMarketEvidenceHash(market) == "" {
		return sandbox.StrategyPipelineDependencies{}, fmt.Errorf("sandbox_strategy_pipeline_factory_unavailable")
	}
	riskEngine, inputs, err := factory.strategyPipelineRiskInputs(ctx, admission, facts, market)
	if err != nil {
		return sandbox.StrategyPipelineDependencies{}, err
	}
	owned, registry, liquidity, limits, err := strategyAllocationProjection(admission, facts, market, inventory)
	if err != nil {
		return sandbox.StrategyPipelineDependencies{}, err
	}
	allocator, err := portfolio.NewAllocatorWithLimits(owned, registry, liquidity, limits)
	if err != nil {
		return sandbox.StrategyPipelineDependencies{}, fmt.Errorf("sandbox_strategy_pipeline_factory_invalid")
	}
	pipelineAllocator, err := portfolio.NewPipelineAllocator(allocator)
	if err != nil {
		return sandbox.StrategyPipelineDependencies{}, fmt.Errorf("sandbox_strategy_pipeline_factory_invalid")
	}
	vault := portfolio.NewApprovalVault()
	pipelineRisk, err := risk.NewPipelineEngine(riskEngine, vault, registry, inputs)
	if err != nil {
		return sandbox.StrategyPipelineDependencies{}, fmt.Errorf("sandbox_strategy_pipeline_factory_invalid")
	}
	strategyPlanner, err := sandboxStrategyPlanner(admission.Work, facts.LiquidityDomain, strategy)
	if err != nil {
		return sandbox.StrategyPipelineDependencies{}, err
	}
	planner, err := portfolio.NewEligibilityPlanner(strategyPlanner, vault, registry)
	if err != nil {
		return sandbox.StrategyPipelineDependencies{}, fmt.Errorf("sandbox_strategy_pipeline_factory_invalid")
	}
	return sandbox.StrategyPipelineDependencies{Allocator: pipelineAllocator, Risk: pipelineRisk,
		Planner: planner, Store: factory.store, Kill: sandbox.NoKillPoint{}}, nil
}

func (factory *SandboxStrategyPipelineFactory) strategyPipelineRiskInputs(ctx context.Context,
	admission sandbox.StrategySessionAdmission, facts SandboxStrategySizingFacts,
	market sandbox.StrategyMarketInput,
) (*risk.Engine, *sandbox.StrategyRiskInputs, error) {
	engine, err := factory.riskEngines.SandboxStrategyRiskEngine(ctx, admission.ApprovedAt)
	if err != nil || engine == nil || engine.State() != risk.StateNormal {
		return nil, nil, fmt.Errorf("sandbox_strategy_pipeline_factory_unavailable")
	}
	observation, err := factory.observations.StrategyRiskObservation(ctx, admission.Work,
		facts.AccountSnapshot, market, facts.CentralRiskFacts, admission.ApprovedAt)
	if err != nil || observation.ValidFor(admission.Work, facts.AccountSnapshot, market,
		facts.CentralRiskFacts, admission.ApprovedAt) != nil {
		return nil, nil, fmt.Errorf("sandbox_strategy_pipeline_factory_unavailable")
	}
	inputs, err := sandbox.NewStrategyRiskInputs(admission.Work, facts.AccountSnapshot,
		market, facts.CentralRiskFacts, observation, admission.ApprovedAt)
	if err != nil {
		return nil, nil, fmt.Errorf("sandbox_strategy_pipeline_factory_invalid")
	}
	return engine, inputs, nil
}

func strategyAllocationProjection(
	admission sandbox.StrategySessionAdmission,
	facts SandboxStrategySizingFacts,
	market sandbox.StrategyMarketInput,
	inventory sandbox.StrategyOwnedInventory,
) (*portfolio.Portfolio, *portfolio.MemoryAssetRegistry, *portfolio.LiquidityPool, portfolio.AllocationLimits, error) {
	work := admission.Work
	strategyOwner, err := strategyProjectionOwner(work.Strategy)
	if err != nil {
		return nil, nil, nil, portfolio.AllocationLimits{}, fmt.Errorf("sandbox_strategy_pipeline_factory_unsupported")
	}
	identity := strategyPipelineProjectionIdentity(work)
	portfolioID, portfolioErr := domain.NewPortfolioID("sandbox-portfolio-" + identity)
	accountID, accountErr := domain.NewVirtualAccountID("sandbox-account-" + identity)
	if portfolioErr != nil || accountErr != nil {
		return nil, nil, nil, portfolio.AllocationLimits{}, fmt.Errorf("sandbox_strategy_pipeline_factory_invalid")
	}
	balances := strategyProjectionBalances(facts.AccountSnapshot, market.Instrument.Base, inventory.Available)
	owned, err := portfolio.NewAccountBalancePortfolio(portfolio.Ownership{PortfolioID: portfolioID,
		AccountID: accountID, Strategy: strategyOwner, Exchange: string(work.Account.Exchange)}, "USDT", balances)
	if err != nil {
		return nil, nil, nil, portfolio.AllocationLimits{}, fmt.Errorf("sandbox_strategy_pipeline_factory_invalid")
	}
	registry, err := portfolio.NewSnapshotAssetRegistry([]portfolio.AssetEligibility{
		{Asset: market.Instrument.Base, Status: domain.AssetApproved, Version: facts.AssetEligibility},
		{Asset: market.Instrument.Quote, Status: domain.AssetApproved, Version: facts.AssetEligibility},
	})
	if err != nil {
		return nil, nil, nil, portfolio.AllocationLimits{}, fmt.Errorf("sandbox_strategy_pipeline_factory_invalid")
	}
	depth, err := conservativeStrategyBookDepth(market)
	if err != nil {
		return nil, nil, nil, portfolio.AllocationLimits{}, err
	}
	liquidity := portfolio.NewLiquidityPool()
	if err = liquidity.Open(facts.LiquidityDomain, depth); err != nil {
		return nil, nil, nil, portfolio.AllocationLimits{}, fmt.Errorf("sandbox_strategy_pipeline_factory_invalid")
	}
	limits, err := strategyAllocationLimits(facts)
	if err != nil {
		return nil, nil, nil, portfolio.AllocationLimits{}, err
	}
	return owned, registry, liquidity, limits, nil
}

func strategyProjectionOwner(strategy string) (string, error) {
	switch strategy {
	case sandbox.StrategyTrend:
		return portfolio.TrendStrategy, nil
	case sandbox.StrategyMeanReversion:
		return portfolio.MultiStrategyResearchMeanReversionStrategy, nil
	default:
		return "", fmt.Errorf("sandbox_strategy_pipeline_factory_unsupported")
	}
}

func strategyProjectionBalances(snapshot sandbox.AccountSnapshot, base domain.AssetSymbol,
	ownedBase domain.Balance,
) []portfolio.AccountBalance {
	balances := make([]portfolio.AccountBalance, 0, len(snapshot.Balances)+1)
	baseFound := false
	for _, balance := range snapshot.Balances {
		available := balance.Available
		if balance.Asset == base {
			baseFound = true
			if available.Compare(ownedBase) > 0 {
				available = ownedBase
			}
		}
		balances = append(balances, portfolio.AccountBalance{Asset: balance.Asset, Available: available})
	}
	if !baseFound {
		balances = append(balances, portfolio.AccountBalance{Asset: base, Available: ownedBase})
	}
	return balances
}

func strategyPipelineProjectionIdentity(work sandbox.StrategySessionWork) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%d\x00%d", work.SessionID,
		work.Account.ID, work.Account.Epoch, work.StrategyRevision)))
	return hex.EncodeToString(digest[:12])
}

func conservativeStrategyBookDepth(market sandbox.StrategyMarketInput) (domain.Quantity, error) {
	zero, err := domain.ParseQuantity("0")
	if err != nil || len(market.Book.Bids) == 0 || len(market.Book.Asks) == 0 {
		return domain.Quantity{}, fmt.Errorf("sandbox_strategy_pipeline_liquidity_unavailable")
	}
	bids, asks := zero, zero
	for _, level := range market.Book.Bids {
		bids, err = bids.Add(level.Quantity)
		if err != nil {
			return domain.Quantity{}, fmt.Errorf("sandbox_strategy_pipeline_liquidity_invalid")
		}
	}
	for _, level := range market.Book.Asks {
		asks, err = asks.Add(level.Quantity)
		if err != nil {
			return domain.Quantity{}, fmt.Errorf("sandbox_strategy_pipeline_liquidity_invalid")
		}
	}
	if bids.Compare(zero) <= 0 || asks.Compare(zero) <= 0 {
		return domain.Quantity{}, fmt.Errorf("sandbox_strategy_pipeline_liquidity_unavailable")
	}
	if bids.Compare(asks) < 0 {
		return bids, nil
	}
	return asks, nil
}

func strategyAllocationLimits(facts SandboxStrategySizingFacts) (portfolio.AllocationLimits, error) {
	minimum, minimumErr := domain.ParseBalance(facts.MinimumReserve.String())
	maximum, maximumErr := domain.ParseBalance(facts.MaximumReserved.String())
	orderMaximum, orderErr := domain.ParseBalance(facts.MaximumOrderNotional.String())
	if minimumErr != nil || maximumErr != nil || orderErr != nil {
		return portfolio.AllocationLimits{}, fmt.Errorf("sandbox_strategy_pipeline_limits_invalid")
	}
	zero, _ := domain.ParseBalance("0")
	reserved := zero
	for _, balance := range facts.AccountSnapshot.Balances {
		if balance.Asset == "USDT" {
			reserved = balance.Reserved
			break
		}
	}
	headroom, err := maximum.Subtract(reserved)
	if err != nil {
		headroom = zero
	}
	if orderMaximum.Compare(headroom) > 0 {
		orderMaximum = headroom
	}
	return portfolio.AllocationLimits{MinimumReserve: minimum,
		MaximumReserved: headroom, MaximumOrderAmount: orderMaximum}, nil
}

func sandboxStrategyPlanner(
	work sandbox.StrategySessionWork,
	namespace string,
	strategy backtest.Strategy,
) (execution.ExecutionPlanner, error) {
	mode := "testnet"
	if work.Account.Exchange == sandbox.ExchangeBybit {
		mode = "demo"
	} else if work.Account.Exchange != sandbox.ExchangeBinance {
		return nil, fmt.Errorf("sandbox_strategy_pipeline_factory_unsupported")
	}
	switch work.Strategy {
	case sandbox.StrategyTrend:
		source, ok := strategy.(trend.CandidateSource)
		if !ok {
			return nil, fmt.Errorf("sandbox_strategy_pipeline_factory_invalid")
		}
		return trend.NewPlanner(mode, namespace, source)
	case sandbox.StrategyMeanReversion:
		source, ok := strategy.(meanreversion.CandidateSource)
		if !ok {
			return nil, fmt.Errorf("sandbox_strategy_pipeline_factory_invalid")
		}
		return meanreversion.NewPlanner(mode, namespace, source)
	default:
		return nil, fmt.Errorf("sandbox_strategy_pipeline_factory_unsupported")
	}
}

var _ SandboxStrategyPipelineDependenciesSource = (*SandboxStrategyPipelineFactory)(nil)
