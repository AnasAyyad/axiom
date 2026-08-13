package bootstrap

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"axiom/internal/accounting"
	"axiom/internal/backtest"
	"axiom/internal/config"
	"axiom/internal/domain"
	"axiom/internal/portfolio"
	"axiom/internal/risk"
	"axiom/internal/strategies/meanreversion"
	"axiom/internal/strategies/trend"
)

type offlineStrategyRuntime struct {
	ID              string
	SemanticVersion string
	ManifestVersion string
	NewProcessor    func(backtest.JobClaim) (backtest.Processor, error)
}

func installedOfflineStrategyRuntimes() []offlineStrategyRuntime {
	return []offlineStrategyRuntime{
		{
			ID:              "trend-following",
			SemanticVersion: "trend-following@1.0.0",
			ManifestVersion: "trend-following@1.0.0",
			NewProcessor: func(claim backtest.JobClaim) (backtest.Processor, error) {
				return newOwnerConsoleOperationalProcessorWithPortfolio(claim, nil)
			},
		},
		{
			ID:              "mean-reversion",
			SemanticVersion: "mean-reversion@1.0.0",
			ManifestVersion: "mean-reversion@1.0.0",
			NewProcessor:    newOwnerConsoleMeanReversionOperationalProcessor,
		},
		{
			ID:              "triangular-arbitrage",
			SemanticVersion: "triangular-arbitrage@1.0.0",
			ManifestVersion: "triangular-arbitrage@1.0.0",
			NewProcessor:    newOwnerConsoleTriangularOperationalProcessor,
		},
		{
			ID:              "cross-exchange-arbitrage",
			SemanticVersion: "cross-exchange-arbitrage@1.0.0",
			ManifestVersion: "cross-exchange-arbitrage@1.0.0",
			NewProcessor:    newOwnerConsoleCrossExchangeOperationalProcessor,
		},
		{
			ID:              "inventory-rebalancing",
			SemanticVersion: "inventory-rebalancing@1.0.0",
			ManifestVersion: "inventory-rebalancing@1.0.0",
			NewProcessor:    newInventoryRebalancingOperationalProcessor,
		},
	}
}

func newOfflineOperationalProcessor(claim backtest.JobClaim) (backtest.Processor, error) {
	processorClaim, err := stableEvaluationProcessorClaim(claim)
	if err != nil {
		return nil, err
	}
	for _, runtime := range installedOfflineStrategyRuntimes() {
		if runtime.ManifestVersion == claim.Manifest.StrategyVersion {
			processor, err := runtime.NewProcessor(processorClaim)
			if err != nil {
				return nil, err
			}
			if claim.Manifest.StrategyVersion == "trend-following@1.0.0" ||
				claim.Manifest.StrategyVersion == "mean-reversion@1.0.0" {
				processor, err = newOfflineEvidenceMetricsProcessor(processor, processorClaim)
				if err != nil {
					return nil, err
				}
			}
			if claim.Manifest.Evaluation != nil {
				return newEvaluationMarketProcessor(processorClaim, processor)
			}
			return processor, nil
		}
	}
	return nil, fmt.Errorf("offline_strategy_runtime_unavailable")
}

func stableEvaluationProcessorClaim(claim backtest.JobClaim) (backtest.JobClaim, error) {
	if claim.Manifest.Evaluation == nil {
		return claim, nil
	}
	identity := claim.Manifest.Evaluation
	if identity.CampaignID == "" || identity.ConfigurationKey == "" || claim.Manifest.StrategyVersion == "" ||
		claim.Manifest.Mode == "" || identity.CapitalMicros <= 0 || identity.CostStressBPS <= 0 {
		return backtest.JobClaim{}, fmt.Errorf("evaluation_processor_identity_invalid")
	}
	digest := sha256.Sum256([]byte(fmt.Sprintf("evaluation-runtime-v1:%s:%s:%s:%s:%d:%d",
		identity.CampaignID, claim.Manifest.StrategyVersion, identity.ConfigurationKey, claim.Manifest.Mode,
		identity.CapitalMicros, identity.CostStressBPS)))
	stable := "evaluation-runtime-" + hex.EncodeToString(digest[:16])
	runID, err := domain.NewRunID(stable)
	if err != nil {
		return backtest.JobClaim{}, err
	}
	claim.ID = stable
	claim.Manifest.RunID = runID
	evaluationIdentity := *identity
	evaluationIdentity.MemberID = stable
	claim.Manifest.Evaluation = &evaluationIdentity
	return claim, nil
}

func newOwnerConsoleOperationalProcessorWithPortfolio(claim backtest.JobClaim,
	owned *portfolio.Portfolio) (backtest.Processor, error) {
	if claim.Manifest.StrategyVersion != "trend-following@1.0.0" {
		return nil, fmt.Errorf("owner_console_worker_strategy_runtime_unavailable")
	}
	components, err := newOwnerConsoleWorkerComponents(claim, owned)
	if err != nil {
		return nil, err
	}
	pipeline, err := composeOwnerConsoleWorkerPipeline(claim, components)
	if err != nil {
		return nil, err
	}
	operational, err := trend.NewOperationalProcessor(components.evaluator, pipeline, components.owned)
	if err != nil {
		return nil, err
	}
	return &ownerConsoleInputAwareProcessor{inputs: components.inputs, delegate: operational}, nil
}

type ownerConsoleWorkerComponents struct {
	evaluator *trend.Evaluator
	adapter   *trend.Adapter
	owned     *portfolio.Portfolio
	registry  *portfolio.MemoryAssetRegistry
	allocator *portfolio.PipelineAllocator
	inputs    *ownerConsoleDecisionInputContext
}

func newOwnerConsoleWorkerComponents(claim backtest.JobClaim, owned *portfolio.Portfolio) (ownerConsoleWorkerComponents, error) {
	if err := config.Validate(claim.Configuration); err != nil || claim.Manifest.StrategyVersion != "trend-following@1.0.0" ||
		claim.Configuration.Trend.StrategyVersion != "trend-following@1.0.0" {
		return ownerConsoleWorkerComponents{}, fmt.Errorf("owner_console_worker_configuration_invalid")
	}
	configuredTrend, err := trend.NewConfiguration(claim.Configuration.Trend)
	if err != nil {
		return ownerConsoleWorkerComponents{}, err
	}
	evaluator, err := trend.NewEvaluator(configuredTrend)
	if err != nil {
		return ownerConsoleWorkerComponents{}, err
	}
	adapter, err := trend.NewAdapter(evaluator)
	if err != nil {
		return ownerConsoleWorkerComponents{}, err
	}
	if owned == nil {
		owned, err = newOwnerConsoleOfflinePortfolio(claim)
		if err != nil {
			return ownerConsoleWorkerComponents{}, err
		}
	}
	registry := portfolio.NewAssetRegistry()
	liquidity := portfolio.NewLiquidityPool()
	availableDepth, _ := domain.ParseQuantity("1000000000")
	if err = liquidity.Open(claim.Manifest.Models.LiquidityDomain, availableDepth); err != nil {
		return ownerConsoleWorkerComponents{}, err
	}
	allocator, err := portfolio.NewAllocator(owned, registry, liquidity)
	if err != nil {
		return ownerConsoleWorkerComponents{}, err
	}
	pipelineAllocator, err := portfolio.NewPipelineAllocator(allocator)
	if err != nil {
		return ownerConsoleWorkerComponents{}, err
	}
	return ownerConsoleWorkerComponents{evaluator: evaluator, adapter: adapter, owned: owned, registry: registry,
		allocator: pipelineAllocator, inputs: &ownerConsoleDecisionInputContext{}}, nil
}

func composeOwnerConsoleWorkerPipeline(claim backtest.JobClaim, components ownerConsoleWorkerComponents) (*backtest.PipelineProcessor, error) {
	riskEngine, err := risk.NewEngine(&ownerConsoleRunRiskAudit{}, ownerConsoleRunRiskAlerts{})
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
	broker, err := newOwnerConsoleDynamicBroker(claim, components.inputs, guard)
	if err != nil {
		return nil, err
	}
	return backtest.NewPipelineProcessor(backtest.PipelineDependencies{Strategy: components.adapter,
		Allocator: components.allocator, Risk: pipelineRisk, Planner: planner, Broker: broker,
		Reduce: components.allocator.ReduceSimulation, Metrics: func() backtest.Metrics { return backtest.Metrics{} }})
}

func newOwnerConsoleOfflinePortfolio(claim backtest.JobClaim) (*portfolio.Portfolio, error) {
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

// newOwnerConsoleMeanReversionOperationalProcessor installs the existing mean reversion evaluator
// into the same durable allocator, risk, planner, simulation, and accounting
// path as Trend. The immutable manifest selects it; configuration presence on
// its own is not enough because a multi-strategy research graph can contain several strategies.
func newOwnerConsoleMeanReversionOperationalProcessor(claim backtest.JobClaim) (backtest.Processor, error) {
	owned, err := newOwnerConsoleMeanReversionOfflinePortfolio(claim)
	if err != nil {
		return nil, err
	}
	return newOwnerConsoleMeanReversionOperationalProcessorWithPortfolio(claim, owned)
}

func newOwnerConsoleMeanReversionOperationalProcessorWithPortfolio(claim backtest.JobClaim,
	owned *portfolio.Portfolio) (backtest.Processor, error) {
	components, err := newOwnerConsoleMeanReversionWorkerComponents(claim, owned)
	if err != nil {
		return nil, err
	}
	pipeline, err := composeOwnerConsoleMeanReversionWorkerPipeline(claim, components)
	if err != nil {
		return nil, err
	}
	operational, err := meanreversion.NewOperationalProcessor(components.evaluator, pipeline, func() (json.RawMessage, error) {
		return json.Marshal(components.owned.Snapshot())
	})
	if err != nil {
		return nil, err
	}
	return &ownerConsoleMeanReversionInputAwareProcessor{inputs: components.inputs, delegate: operational}, nil
}

type ownerConsoleMeanReversionWorkerComponents struct {
	evaluator *meanreversion.Evaluator
	adapter   *meanreversion.Adapter
	owned     *portfolio.Portfolio
	registry  *portfolio.MemoryAssetRegistry
	allocator *portfolio.PipelineAllocator
	inputs    *ownerConsoleMeanReversionInputContext
}

func newOwnerConsoleMeanReversionWorkerComponents(claim backtest.JobClaim,
	owned *portfolio.Portfolio,
) (ownerConsoleMeanReversionWorkerComponents, error) {
	if err := config.Validate(claim.Configuration); err != nil ||
		claim.Manifest.StrategyVersion != "mean-reversion@1.0.0" ||
		claim.Configuration.MeanReversion.StrategyVersion != "mean-reversion@1.0.0" || owned == nil {
		return ownerConsoleMeanReversionWorkerComponents{}, fmt.Errorf("owner_console_mean_reversion_configuration_invalid")
	}
	configured, err := meanreversion.NewConfiguration(claim.Configuration.MeanReversion)
	if err != nil {
		return ownerConsoleMeanReversionWorkerComponents{}, err
	}
	evaluator, err := meanreversion.NewEvaluator(configured)
	if err != nil {
		return ownerConsoleMeanReversionWorkerComponents{}, err
	}
	adapter, err := meanreversion.NewAdapter(evaluator)
	if err != nil {
		return ownerConsoleMeanReversionWorkerComponents{}, err
	}
	registry := portfolio.NewAssetRegistry()
	liquidity := portfolio.NewLiquidityPool()
	availableDepth, _ := domain.ParseQuantity("1000000000")
	if err = liquidity.Open(claim.Manifest.Models.LiquidityDomain, availableDepth); err != nil {
		return ownerConsoleMeanReversionWorkerComponents{}, err
	}
	allocator, err := portfolio.NewAllocator(owned, registry, liquidity)
	if err != nil {
		return ownerConsoleMeanReversionWorkerComponents{}, err
	}
	pipelineAllocator, err := portfolio.NewPipelineAllocator(allocator)
	if err != nil {
		return ownerConsoleMeanReversionWorkerComponents{}, err
	}
	return ownerConsoleMeanReversionWorkerComponents{evaluator: evaluator, adapter: adapter, owned: owned,
		registry: registry, allocator: pipelineAllocator, inputs: &ownerConsoleMeanReversionInputContext{}}, nil
}

func composeOwnerConsoleMeanReversionWorkerPipeline(claim backtest.JobClaim,
	components ownerConsoleMeanReversionWorkerComponents,
) (*backtest.PipelineProcessor, error) {
	riskEngine, err := risk.NewEngine(&ownerConsoleRunRiskAudit{}, ownerConsoleRunRiskAlerts{})
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
	strategyPlanner, err := meanreversion.NewPlanner(claim.Manifest.Mode,
		claim.Manifest.Models.LiquidityDomain, components.adapter)
	if err != nil {
		return nil, err
	}
	planner, err := portfolio.NewEligibilityPlanner(strategyPlanner, vault, components.registry)
	if err != nil {
		return nil, err
	}
	guard, err := portfolio.NewBrokerGuard(components.owned, components.registry)
	if err != nil {
		return nil, err
	}
	broker, err := newOwnerConsoleMeanReversionDynamicBroker(claim, components.inputs, guard)
	if err != nil {
		return nil, err
	}
	return backtest.NewPipelineProcessor(backtest.PipelineDependencies{Strategy: components.adapter,
		Allocator: components.allocator, Risk: pipelineRisk, Planner: planner, Broker: broker,
		Reduce:  components.allocator.ReduceSimulation,
		Metrics: func() backtest.Metrics { return backtest.Metrics{} }})
}

func newOwnerConsoleMeanReversionOfflinePortfolio(claim backtest.JobClaim) (*portfolio.Portfolio, error) {
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
	return portfolio.InitializeMeanReversion(claim.Manifest.RunID, portfolioID, accountID, claim.Manifest.ConfigurationHash,
		capital, accounting.NewMemoryJournal(), domain.EventTime{UTC: time.Unix(0, 1).UTC(), Sequence: 1})
}
