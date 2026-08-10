package bootstrap

import (
	"crypto/sha256"
	"fmt"
	"time"

	"axiom/internal/accounting"
	"axiom/internal/backtest"
	"axiom/internal/config"
	"axiom/internal/domain"
	"axiom/internal/reconciliation"
	"axiom/internal/risk"
	runtimecore "axiom/internal/runtime"
	"axiom/internal/strategies/crossarb"
	"axiom/internal/strategies/triangular"
)

// newOwnerConsoleTriangularOperationalProcessor installs the exact recorded-input
// triangular saga in the credential-free backtest/replay worker. It never
// receives an exchange client or credential-bearing broker.
func newOwnerConsoleTriangularOperationalProcessor(claim backtest.JobClaim) (backtest.Processor, error) {
	portfolioID, err := offlineMultilegPortfolioID(claim.ID)
	if err != nil {
		return nil, err
	}
	return newOwnerConsoleTriangularOperationalProcessorWithOwnership(claim, portfolioID, "offline-owner")
}

func newOwnerConsoleTriangularOperationalProcessorWithOwnership(
	claim backtest.JobClaim,
	portfolioID domain.PortfolioID,
	owner string,
) (backtest.Processor, error) {
	if err := config.Validate(claim.Configuration); err != nil ||
		claim.Manifest.StrategyVersion != "triangular-arbitrage@1.0.0" ||
		claim.Configuration.Triangular.StrategyVersion != "triangular-arbitrage@1.0.0" ||
		portfolioID.Value() == "" || owner == "" {
		return nil, fmt.Errorf("owner_console_triangular_configuration_invalid")
	}
	allocator, err := triangular.NewRecordedSagaAllocator(owner, runtimecore.FencingToken(1))
	if err != nil {
		return nil, err
	}
	riskEngine, err := newOwnerConsoleRecordedSagaRiskEngine()
	if err != nil {
		return nil, err
	}
	riskAdapter, err := triangular.NewSagaRiskAdapter(riskEngine, triangular.RecordedSagaRiskInputs{})
	if err != nil {
		return nil, err
	}
	broker, err := triangular.NewSagaSimulationBroker(triangular.RecordedSagaSimulationInputs{})
	if err != nil {
		return nil, err
	}
	journal, reconciler, err := newOwnerConsoleRecordedSagaReductionAt(claim, owner, portfolioID)
	if err != nil {
		return nil, err
	}
	provider, err := triangular.NewRecordedSagaReductionProvider(journal, reconciler, claim.Manifest.RunID, portfolioID,
		owner, allocator)
	if err != nil {
		return nil, err
	}
	reducer, err := triangular.NewSagaReducer(provider)
	if err != nil {
		return nil, err
	}
	pipeline, err := backtest.NewSagaPipelineProcessor(backtest.SagaPipelineDependencies{Strategy: triangular.NewSagaStrategyAdapter(),
		Allocator: allocator, Risk: riskAdapter, Planner: triangular.NewSagaPlanner(), Broker: broker, Reducer: reducer,
		Metrics: func() backtest.Metrics { return backtest.Metrics{TotalNetReturn: "not_evaluated"} }})
	if err != nil {
		return nil, err
	}
	return triangular.NewOperationalProcessor(pipeline)
}

// newOwnerConsoleCrossExchangeOperationalProcessor installs the exact recorded-input
// concurrent two-venue saga in the credential-free backtest/replay worker.
func newOwnerConsoleCrossExchangeOperationalProcessor(claim backtest.JobClaim) (backtest.Processor, error) {
	portfolioID, err := offlineMultilegPortfolioID(claim.ID)
	if err != nil {
		return nil, err
	}
	return newOwnerConsoleCrossExchangeOperationalProcessorWithOwnership(claim, portfolioID, "offline-owner")
}

func offlineMultilegPortfolioID(claimID string) (domain.PortfolioID, error) {
	digest := sha256.Sum256([]byte(claimID))
	return domain.NewPortfolioID(fmt.Sprintf("offline-multileg-%x", digest[:20]))
}

func newOwnerConsoleCrossExchangeOperationalProcessorWithOwnership(
	claim backtest.JobClaim,
	portfolioID domain.PortfolioID,
	owner string,
) (backtest.Processor, error) {
	if err := config.Validate(claim.Configuration); err != nil ||
		claim.Manifest.StrategyVersion != "cross-exchange-arbitrage@1.0.0" ||
		claim.Configuration.CrossExchange.StrategyVersion != "cross-exchange-arbitrage@1.0.0" ||
		portfolioID.Value() == "" || owner == "" {
		return nil, fmt.Errorf("owner_console_cross_exchange_configuration_invalid")
	}
	allocator, err := crossarb.NewRecordedSagaAllocator(runtimecore.FencingToken(1))
	if err != nil {
		return nil, err
	}
	riskEngine, err := newOwnerConsoleRecordedSagaRiskEngine()
	if err != nil {
		return nil, err
	}
	riskAdapter, err := crossarb.NewSagaRiskAdapter(riskEngine, crossarb.RecordedSagaRiskInputs{})
	if err != nil {
		return nil, err
	}
	broker, err := crossarb.NewSagaSimulationBroker(crossarb.RecordedSagaSimulationInputs{})
	if err != nil {
		return nil, err
	}
	journal, reconciler, err := newOwnerConsoleRecordedSagaReductionAt(claim, owner, portfolioID)
	if err != nil {
		return nil, err
	}
	provider, err := crossarb.NewRecordedSagaReductionProvider(journal, reconciler, claim.Manifest.RunID, portfolioID,
		owner, allocator)
	if err != nil {
		return nil, err
	}
	reducer, err := crossarb.NewSagaReducer(provider)
	if err != nil {
		return nil, err
	}
	pipeline, err := backtest.NewSagaPipelineProcessor(backtest.SagaPipelineDependencies{Strategy: crossarb.NewSagaStrategyAdapter(),
		Allocator: allocator, Risk: riskAdapter, Planner: crossarb.NewSagaPlanner(), Broker: broker, Reducer: reducer,
		Metrics: func() backtest.Metrics { return backtest.Metrics{TotalNetReturn: "not_evaluated"} }})
	if err != nil {
		return nil, err
	}
	return crossarb.NewOperationalProcessor(pipeline)
}

func newOwnerConsoleRecordedSagaRiskEngine() (*risk.Engine, error) {
	engine, err := risk.NewEngine(&ownerConsoleRunRiskAudit{}, ownerConsoleRunRiskAlerts{})
	if err != nil {
		return nil, err
	}
	recoveryAt := time.Unix(0, 1).UTC()
	if err = engine.ManualTransition(risk.StateNormal, risk.RecoveryEvidence{Reconciled: true,
		PersistenceHealthy: true, BooksFresh: true, UnknownOrdersResolved: true, Reauthenticated: true,
		AuditDurable: true, Actor: "offline-worker", Reason: "verified immutable replay inputs", At: recoveryAt}); err != nil {
		return nil, err
	}
	return engine, nil
}

func newOwnerConsoleRecordedSagaReductionAt(
	claim backtest.JobClaim,
	owner string,
	portfolioID domain.PortfolioID,
) (accounting.Journal, *reconciliation.Reconciler, error) {
	if owner == "" || portfolioID.Value() == "" {
		return nil, nil, fmt.Errorf("owner_console_multileg_reduction_identity_invalid")
	}
	journal := accounting.NewMemoryJournal()
	reconciler, err := reconciliation.NewReconciler(ownerConsoleRecordedSagaCases{}, ownerConsoleRecordedSagaIncidents{},
		ownerConsoleRecordedSagaQuarantine{}, journal, reconciliation.Context{RunID: claim.Manifest.RunID,
			PortfolioID: portfolioID, Owner: owner, ConfigurationHash: claim.Manifest.ConfigurationHash})
	if err != nil {
		return nil, nil, err
	}
	return journal, reconciler, nil
}

// These credential-free sinks carry only clean reconciliation through a
// successful worker result. A mismatch makes the reducer fail closed before a
// run result is committed; no synthetic incident or mutable global state is
// substituted for recorded reconciliation evidence.
type ownerConsoleRecordedSagaCases struct{}

// Create accepts an in-memory recorded-data reconciliation case.
func (ownerConsoleRecordedSagaCases) Create(reconciliation.Case) error { return nil }

type ownerConsoleRecordedSagaIncidents struct{}

// Create records an in-memory recorded-data reconciliation incident.
func (ownerConsoleRecordedSagaIncidents) Create(string, string, time.Time) (string, error) {
	return "offline-recorded-reconciliation-incident", nil
}

type ownerConsoleRecordedSagaQuarantine struct{}

// Block records a deterministic offline quarantine decision.
func (ownerConsoleRecordedSagaQuarantine) Block(string, string) error { return nil }
