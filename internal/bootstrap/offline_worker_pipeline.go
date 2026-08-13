package bootstrap

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"axiom/internal/backtest"
	"axiom/internal/buildinfo"
	"axiom/internal/config"
	"axiom/internal/domain"
	"axiom/internal/evaluation"
	"axiom/internal/exchanges/binance"
	"axiom/internal/exchanges/bybit"
	"axiom/internal/observability"
	"axiom/internal/replay"
	postgresstore "axiom/internal/storage/postgres"

	"github.com/jackc/pgx/v5/pgxpool"
)

func newOwnerConsoleWorkerRoleWork(
	pool *pgxpool.Pool,
	runtimeConfig config.Runtime,
	product config.Configuration,
	metrics *observability.Metrics,
) (*workerRoleWork, error) {
	materialize, err := postgresstore.NewOfflineJobMaterializer(pool, runtimeConfig.Recorder.Root)
	if err != nil {
		return nil, err
	}
	store, err := postgresstore.NewOfflineJobStore(pool, runtimeConfig.InstanceID, &domain.SystemClock{}, materialize)
	if err != nil {
		return nil, err
	}
	worker, err := backtest.NewWorker(store, newOfflineOperationalProcessor, replay.RealPacer{})
	if err != nil {
		return nil, err
	}
	reportWorker, err := postgresstore.NewOperationalEvidenceReportWorker(
		pool, runtimeConfig.InstanceID+":reports", &domain.SystemClock{},
	)
	if err != nil {
		return nil, err
	}
	lifecycleWorker, err := postgresstore.NewOperationalReadinessLifecycleWorker(pool, &domain.SystemClock{})
	if err != nil {
		return nil, err
	}
	campaignWorker, err := newEvaluationCampaignWorker(pool, runtimeConfig, product)
	if err != nil {
		return nil, err
	}
	metricWorker, err := newEvaluationMetricWorker(pool, metrics, &domain.SystemClock{})
	if err != nil {
		return nil, err
	}
	return newWorkerRoleWork(orderedOfflineWorkers{metricWorker, lifecycleWorker, reportWorker, worker, campaignWorker}, time.Second)
}

func newEvaluationCampaignWorker(pool *pgxpool.Pool, runtimeConfig config.Runtime,
	product config.Configuration) (offlineWorker, error) {
	clock := &domain.SystemClock{}
	historical, audit, shadow, err := newEvaluationCampaignStages(pool, runtimeConfig, product, clock)
	if err != nil {
		return nil, err
	}
	campaignWorker, err := newEvaluationCampaignOrchestratorWorker(pool, runtimeConfig, product, clock,
		historical, audit, shadow)
	if err != nil {
		return nil, err
	}
	return orderedOfflineWorkers{audit, campaignWorker}, nil
}

func newEvaluationCampaignStages(pool *pgxpool.Pool, runtimeConfig config.Runtime,
	product config.Configuration, clock domain.Clock) (*evaluation.HistoricalCoordinator,
	*postgresstore.EvaluationDataAuditCoordinator, *evaluationCombinedShadowEngine, error) {
	historical, err := newEvaluationHistoricalCoordinator(pool, runtimeConfig, product, clock)
	if err != nil {
		return nil, nil, nil, err
	}
	audit, err := postgresstore.NewEvaluationDataAuditCoordinator(pool,
		runtimeConfig.InstanceID+":evaluation-audit", runtimeConfig.Recorder.Root, clock)
	if err != nil {
		return nil, nil, nil, err
	}
	materialize, err := postgresstore.NewOfflineJobMaterializer(pool, runtimeConfig.Recorder.Root)
	if err != nil {
		return nil, nil, nil, err
	}
	shadow, err := newEvaluationCombinedShadowEngine(pool, runtimeConfig.Recorder.Root, clock, materialize)
	return historical, audit, shadow, err
}

func newEvaluationHistoricalCoordinator(pool *pgxpool.Pool, runtimeConfig config.Runtime,
	product config.Configuration, clock domain.Clock) (*evaluation.HistoricalCoordinator, error) {
	exchanges := product.PublicExchanges()
	if len(exchanges) != 2 {
		return nil, fmt.Errorf("evaluation_public_exchanges_incomplete")
	}
	byID := make(map[string]config.ExchangeConfiguration, 2)
	for _, exchange := range exchanges {
		byID[exchange.ID] = exchange
	}
	binanceClient, err := binance.NewPublicClient(byID["binance"].EndpointSet, clock)
	if err != nil {
		return nil, err
	}
	bybitClient, err := bybit.NewPublicClient(byID["bybit"].EndpointSet, clock)
	if err != nil {
		return nil, err
	}
	historyRoot := filepath.Join(runtimeConfig.Recorder.Root, "evaluation-history")
	if err = os.MkdirAll(historyRoot, 0o750); err != nil {
		return nil, fmt.Errorf("evaluation_history_root_unavailable")
	}
	segmentStore, err := postgresstore.NewEvaluationHistoricalSegmentStore(pool, clock)
	if err != nil {
		return nil, err
	}
	sink, err := evaluation.NewHistoricalFileSink(historyRoot, segmentStore)
	if err != nil {
		return nil, err
	}
	importer, err := evaluation.NewHistoricalImporter(binanceClient, bybitClient, sink)
	if err != nil {
		return nil, err
	}
	taskStore, err := postgresstore.NewEvaluationHistoricalTaskStore(pool,
		runtimeConfig.InstanceID+":evaluation-history", clock)
	if err != nil {
		return nil, err
	}
	catalog, err := postgresstore.NewRecordedDatasetCatalog(pool)
	if err != nil {
		return nil, err
	}
	historical, err := evaluation.NewHistoricalCoordinator(historyRoot, buildinfo.Current().Commit, clock,
		importer, taskStore, segmentStore, catalog)
	return historical, err
}

func newEvaluationCampaignOrchestratorWorker(pool *pgxpool.Pool, runtimeConfig config.Runtime,
	product config.Configuration, clock domain.Clock, historical *evaluation.HistoricalCoordinator,
	audit *postgresstore.EvaluationDataAuditCoordinator, shadow *evaluationCombinedShadowEngine) (offlineWorker, error) {
	driver, err := postgresstore.NewEvaluationCampaignDriver(pool, runtimeConfig.Recorder.Root,
		clock, product, historical, audit, shadow)
	if err != nil {
		return nil, err
	}
	store, err := postgresstore.NewEvaluationWorkerStore(pool,
		runtimeConfig.InstanceID+":evaluation-campaign", clock)
	if err != nil {
		return nil, err
	}
	orchestrator, err := evaluation.NewOrchestrator(driver)
	if err != nil {
		return nil, err
	}
	campaignWorker, err := evaluation.NewWorker(store, orchestrator)
	if err != nil {
		return nil, err
	}
	return campaignWorker, nil
}

// offlineStrategyRuntime binds one semantic strategy identity to the exact
// shared offline processor that interprets its immutable manifest. New
// strategy families are registered here only after their real allocator,
// risk, execution, accounting, and reconciliation pipeline exists.
