package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"axiom/internal/config"
	researchcontract "axiom/internal/research"
	"axiom/internal/storage/postgres/generated"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMeanReversionPostgresCleanInstallQualification(t *testing.T) {
	ctx, pool := openMeanReversionTestDatabase(t, "AXIOM_MEAN_REVERSION_TEST_DSN")
	defer pool.Close()
	assertPostgres18(t, ctx, pool)
	assertEmptyTestDatabase(t, ctx, pool)
	migrations, err := Migrations()
	if err != nil || len(migrations) < 15 {
		t.Fatalf("migration catalog=%d error=%v", len(migrations), err)
	}
	if applied, applyErr := ApplyMigrations(ctx, pool); applyErr != nil || applied != len(migrations) {
		t.Fatalf("clean migrations=%d/%d error=%v", applied, len(migrations), applyErr)
	}
	assertMeanReversionSchemaAndPersistence(t, ctx, pool)
}

func TestMeanReversionPostgresCoherentMarketDataToMeanReversionUpgradeQualification(t *testing.T) {
	ctx, pool := openMeanReversionTestDatabase(t, "AXIOM_MEAN_REVERSION_UPGRADE_TEST_DSN")
	defer pool.Close()
	assertPostgres18(t, ctx, pool)
	assertEmptyTestDatabase(t, ctx, pool)
	migrations, err := Migrations()
	if err != nil || len(migrations) < 15 {
		t.Fatalf("migration catalog=%d error=%v", len(migrations), err)
	}
	connection, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = ensureMigrationTable(ctx, connection); err != nil {
		connection.Release()
		t.Fatal(err)
	}
	for _, migration := range migrations[:14] {
		changed, applyErr := applyMigration(ctx, connection, migration)
		if applyErr != nil || !changed {
			connection.Release()
			t.Fatalf("coherent market data migration %s changed=%t error=%v", migration.Name, changed, applyErr)
		}
	}
	connection.Release()
	expected := len(migrations) - 14
	if applied, applyErr := ApplyMigrations(ctx, pool); applyErr != nil || applied != expected {
		t.Fatalf("coherent-market-data-to-current migration=%d/%d error=%v", applied, expected, applyErr)
	}
	assertMeanReversionSchemaAndPersistence(t, ctx, pool)
}

func openMeanReversionTestDatabase(t *testing.T, environment string) (context.Context, *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv(environment)
	if dsn == "" {
		t.Skip(environment + " is not set")
	}
	configuration, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(configuration.ConnConfig.Database, "_mean_reversion_test") {
		t.Fatal("mean reversion integration requires a dedicated database ending _mean_reversion_test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	pool, err := pgxpool.NewWithConfig(ctx, configuration)
	if err != nil {
		t.Fatal(err)
	}
	if err = pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatal(err)
	}
	return ctx, pool
}

func assertMeanReversionSchemaAndPersistence(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	seedMeanReversionReferences(t, ctx, pool, now)
	repository, err := NewMeanReversionRepository(pool)
	if err != nil {
		t.Fatal(err)
	}
	registration := meanReversionRegistrationFixture(t, now)
	if err = repository.Register(ctx, registration); err != nil {
		t.Fatal(err)
	}
	if err = repository.Register(ctx, registration); err == nil {
		t.Fatal("duplicate mean reversion registration committed")
	}
	assertMeanReversionParameterGraph(t, ctx, pool)
	view := coherentMarketDataCoherentFixture(t, now.Add(time.Minute))
	coherentRepository, err := NewCoherentViewRepository(pool)
	if err != nil || coherentRepository.Commit(ctx, view, now.Add(time.Minute)) != nil {
		t.Fatalf("mean reversion coherent view unavailable: %v", err)
	}
	seedMeanReversionRuntimeReferences(t, ctx, pool, now.Add(2*time.Minute))
	assertMeanReversionFinalConsumption(t, ctx, pool, repository, now.Add(3*time.Minute))
	assertMeanReversionDecisionPersistence(t, ctx, pool, repository, view.Identity(), now.Add(4*time.Minute))
	assertMeanReversionReportPersistence(t, ctx, pool, repository, now.Add(5*time.Minute))
}

func seedMeanReversionReferences(t *testing.T, ctx context.Context, pool *pgxpool.Pool, now time.Time) {
	t.Helper()
	configuration, err := json.Marshal(config.DefaultMultiStrategyConfiguration())
	if err != nil {
		t.Fatal(err)
	}
	hash := strings.Repeat("a", 64)
	statements := []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO configuration_versions(id,version,configuration_hash,canonical_payload,actor,recorded_at)
VALUES ('configuration-mean_reversion',1,$1,$2,'mean_reversion-qualification',$3)`, []any{researchRegistryPayloadHash(configuration), configuration, now}},
		{`INSERT INTO configuration_activations(configuration_id,actor,reason,activated_at)
VALUES ('configuration-mean_reversion','mean_reversion-qualification','immutable mean reversion baseline',$1)`, []any{now}},
		{`INSERT INTO dataset_manifests(id,dataset_hash,schema_compatibility,coverage_start,coverage_end,state,created_at)
VALUES ('dataset-mean_reversion',$1,'mean_reversion-normalized-v1',$2,$3,'building',$2)`, []any{hash, now.Add(-24 * time.Hour), now}},
		{"UPDATE dataset_manifests SET state='ready' WHERE id='dataset-mean_reversion'", nil},
		{"UPDATE dataset_manifests SET state='qualified' WHERE id='dataset-mean_reversion'", nil},
		{"INSERT INTO assets(symbol) VALUES ('USDT'),('BTC'),('ETH')", nil},
		{"INSERT INTO instruments(id,base_asset,quote_asset,product) VALUES ('BTCUSDT','BTC','USDT','spot')", nil},
		{`INSERT INTO instrument_metadata_versions
(id,exchange_id,instrument_id,version,price_tick,quantity_step,minimum_quantity,minimum_notional,effective_at,recorded_at)
VALUES ('metadata-mean_reversion','binance','BTCUSDT',1,0.01,0.00001,0.00001,10,$1,$1)`, []any{now}},
		{`INSERT INTO model_versions(id,model_type,version,model_hash,canonical_payload,used_at,created_at) VALUES
('fixed-bps-v1','fee',1,$1,'{}',NULL,$2),
('fixed-zero-v1','latency',1,$1,'{}',NULL,$2),
('fill-v1','fill',1,$1,'{}',NULL,$2),
('slippage-v1','slippage',1,$1,'{}',NULL,$2),
('gap-v1','gap',1,$1,'{}',NULL,$2),
('correlation-v1','correlation',1,$1,'{}',NULL,$2)`, []any{hash, now}},
		{`INSERT INTO risk_policies(id,version,scope_kind,scope_id,state,policy_hash,canonical_payload,effective_at,recorded_at)
VALUES ('risk-mean_reversion',1,'global','global','NORMAL',$1,'{}',$2,$2)`, []any{strings.Repeat("f", 64), now}},
	}
	for index, statement := range statements {
		if _, err = pool.Exec(ctx, statement.sql, statement.args...); err != nil {
			t.Fatalf("mean reversion seed %d failed: %v", index+1, err)
		}
	}
}

func meanReversionRegistrationFixture(t *testing.T, now time.Time) MeanReversionRegistrationWrite {
	t.Helper()
	hash := strings.Repeat("b", 64)
	manifest := []byte(`{"version":"mean-reversion@1.0.0","scope":"spot-only"}`)
	manifestHash := researchRegistryPayloadHash(manifest)
	author, notes, metric := "authoritative_specification", "Tier B local engineering qualification", "risk_adjusted_net_return"
	generation, minimumSamples := int32(1), int64(100)
	stop, reject, promote := "registered sample boundary", "confidence interval crosses rejection floor", "formal gates only"
	commit := strings.Repeat("c", 40)
	write := ResearchRegistryRegistrationWrite{
		Definition: generated.InsertResearchRegistryStrategyDefinitionParams{ID: "mean-reversion", Name: "Mean Reversion multi-strategy research", Family: "mean_reversion"},
		Version: generated.InsertResearchRegistryStrategyVersionParams{ID: "mean-reversion-1-0-0", StrategyID: "mean-reversion", Version: 1,
			ImplementationHash: hash, PromotionStatus: "research", CreatedAt: pgTimestamp(now), ManifestHash: manifestHash,
			CanonicalManifest: manifest, CodeCommit: &commit, SupportedModes: []string{"backtest", "replay", "paper", "shadow"},
			Author: &author, Notes: &notes},
		Experiment: generated.InsertResearchRegistryExperimentRegistrationParams{ID: "experiment-mean_reversion", StrategyVersionID: "mean-reversion-1-0-0",
			ConfigurationID: "configuration-mean_reversion", DatasetID: "dataset-mean_reversion", Hypothesis: "registered mean-reversion hypothesis",
			Status: "registered", RegisteredAt: pgTimestamp(now), Generation: &generation, PrimaryMetric: &metric,
			TrainStart: pgTimestamp(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)), TrainEnd: pgTimestamp(time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)),
			ValidationStart: pgTimestamp(time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)), ValidationEnd: pgTimestamp(time.Date(2025, 9, 1, 0, 0, 0, 0, time.UTC)),
			FinalTestStart: pgTimestamp(time.Date(2025, 9, 1, 0, 0, 0, 0, time.UTC)), FinalTestEnd: pgTimestamp(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)),
			SearchSpace: []byte(`{"locked":"baseline"}`), ParameterNeighborhood: []byte(`{"entry_zscore":["-1.9","-2","-2.1"]}`),
			ModelAssumptions:     []byte(`{"fee":"fixed-bps-v1","latency":"fixed-zero-v1","fill":"fill-v1","slippage":"slippage-v1","gap":"gap-v1","correlation":"correlation-v1"}`),
			BenchmarkAssumptions: []byte(`{"cash":true,"buy_hold":true,"static_inventory":true}`), MinimumSamples: &minimumSamples,
			StoppingRule: &stop, RejectionRule: &reject, PromotionRule: &promote, RegisteredSeedHash: hash},
		Generation: generated.InsertResearchGenerationParams{ID: "generation-mean_reversion-1", ExperimentID: "experiment-mean_reversion", Generation: 1,
			FinalWindowHash: hash, RegistrationHash: hash, RegisteredAt: pgTimestamp(now)},
	}
	write.Parameters = meanReversionParameterWrites(t)
	return MeanReversionRegistrationWrite(write)
}

func meanReversionParameterWrites(t *testing.T) []generated.InsertResearchRegistryStrategyParameterParams {
	t.Helper()
	approvedAt := pgTimestamp(time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC))
	parameters := make([]generated.InsertResearchRegistryStrategyParameterParams, 0, config.MeanReversionParameterCount)
	for _, parameter := range config.DefaultMultiStrategyConfiguration().MeanReversion.Parameters {
		description, algorithm := parameter.Description, parameter.AlgorithmVersion
		minimum, maximum := parameter.Minimum, parameter.Maximum
		minimumInclusive, maximumInclusive := parameter.MinimumInclusive, parameter.MaximumInclusive
		scale := int32(parameter.Scale)
		rounding, cadence, warmup, mutability := parameter.Rounding, parameter.Cadence, parameter.WarmUp, parameter.Mutability
		timezone, change := parameter.EvaluationTimezone, parameter.ChangeBehavior
		actor, reference, reason := parameter.ApprovalActor, parameter.ApprovalReference, parameter.ChangeReason
		dependencies, err := json.Marshal(parameter.ModelDependencies)
		if err != nil {
			t.Fatal(err)
		}
		parameters = append(parameters, generated.InsertResearchRegistryStrategyParameterParams{
			StrategyVersionID: "mean-reversion-1-0-0", ParameterName: parameter.ID, DecimalValue: parameter.Value,
			Unit: parameter.Unit, Description: &description, AlgorithmVersion: &algorithm, MinimumValue: &minimum,
			MaximumValue: &maximum, MinimumInclusive: &minimumInclusive, MaximumInclusive: &maximumInclusive,
			DecimalScale: &scale, Rounding: &rounding, Cadence: &cadence, WarmUp: &warmup, Mutability: &mutability,
			ModelDependencies: dependencies, EvaluationTimezone: &timezone, ChangeBehavior: &change, ApprovalActor: &actor,
			ApprovalReference: &reference, ApprovedAt: approvedAt, ChangeReason: &reason,
		})
	}
	return parameters
}

func assertMeanReversionParameterGraph(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var count, complete int
	err := pool.QueryRow(ctx, `SELECT count(*),count(*) FILTER (WHERE description IS NOT NULL AND algorithm_version IS NOT NULL
AND minimum_value IS NOT NULL AND maximum_value IS NOT NULL AND decimal_scale IS NOT NULL AND rounding IS NOT NULL
AND cadence IS NOT NULL AND warm_up IS NOT NULL AND mutability IS NOT NULL AND model_dependencies IS NOT NULL
AND evaluation_timezone='UTC' AND change_behavior IS NOT NULL AND approval_actor IS NOT NULL
AND approval_reference IS NOT NULL AND approved_at IS NOT NULL AND change_reason IS NOT NULL)
FROM strategy_parameters WHERE strategy_version_id='mean-reversion-1-0-0'`).Scan(&count, &complete)
	if err != nil || count != config.MeanReversionParameterCount || complete != count {
		t.Fatalf("mean reversion parameter graph=%d complete=%d error=%v", count, complete, err)
	}
	if _, err = pool.Exec(ctx, `UPDATE strategy_parameters SET decimal_value='99'
WHERE strategy_version_id='mean-reversion-1-0-0' AND parameter_name='mean_reversion.entry_zscore'`); err == nil {
		t.Fatal("immutable mean reversion parameter mutated")
	}
}

func seedMeanReversionRuntimeReferences(t *testing.T, ctx context.Context, pool *pgxpool.Pool, now time.Time) {
	t.Helper()
	hash := strings.Repeat("d", 64)
	statements := []struct {
		sql  string
		args []any
	}{
		{"INSERT INTO runs(id,mode,configuration_id,strategy_version_id,dataset_id,root_seed_hash,reproducibility_hash,state,created_at) VALUES ('run-mean_reversion','backtest','configuration-mean_reversion','mean-reversion-1-0-0','dataset-mean_reversion',$1,$1,'created',$2)", []any{hash, now}},
		{"INSERT INTO portfolios VALUES ('portfolio-mean_reversion','Mean Reversion multi-strategy research','USDT',$1)", []any{now}},
		{"INSERT INTO virtual_accounts VALUES ('account-mean_reversion','portfolio-mean_reversion','run-mean_reversion','mean-reversion-binance',$1)", []any{now}},
		{"INSERT INTO virtual_balances VALUES ('account-mean_reversion','USDT',500,0,1,$1),('account-mean_reversion','BTC',0,0,1,$1),('account-mean_reversion','ETH',0,0,1,$1)", []any{now}},
	}
	for index, statement := range statements {
		if _, err := pool.Exec(ctx, statement.sql, statement.args...); err != nil {
			t.Fatalf("mean reversion runtime seed %d failed: %v", index+1, err)
		}
	}
	initializeMeanReversionOwnership(t, ctx, pool, "mean_reversion", "run-mean_reversion", "portfolio-mean_reversion", "account-mean_reversion", "mean-reversion-1-0-0", "mean_reversion", now)
	if _, err := pool.Exec(ctx, "INSERT INTO strategy_definitions VALUES ('trend-mean_reversion','Trend mean reversion cross-owner fixture','trend')"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO strategy_versions(id,strategy_id,version,implementation_hash,promotion_status,created_at) VALUES ('trend-mean_reversion-v1','trend-mean_reversion',1,$1,'research',$2)", hash, now); err != nil {
		t.Fatal(err)
	}
	statements = []struct {
		sql  string
		args []any
	}{
		{"INSERT INTO portfolios VALUES ('portfolio-trend-mean_reversion','Trend mean reversion Ownership Fixture','USDT',$1)", []any{now}},
		{"INSERT INTO virtual_accounts VALUES ('account-trend-mean_reversion','portfolio-trend-mean_reversion','run-mean_reversion','trend-binance',$1)", []any{now}},
		{"INSERT INTO virtual_balances VALUES ('account-trend-mean_reversion','USDT',500,0,1,$1),('account-trend-mean_reversion','BTC',0,0,1,$1),('account-trend-mean_reversion','ETH',0,0,1,$1)", []any{now}},
	}
	for index, statement := range statements {
		if _, err := pool.Exec(ctx, statement.sql, statement.args...); err != nil {
			t.Fatalf("cross-strategy seed %d failed: %v", index+1, err)
		}
	}
	initializeMeanReversionOwnership(t, ctx, pool, "trend-mean_reversion", "run-mean_reversion", "portfolio-trend-mean_reversion", "account-trend-mean_reversion", "trend-mean_reversion-v1", "trend", now)
}

func initializeMeanReversionOwnership(t *testing.T, ctx context.Context, pool *pgxpool.Pool, suffix, runID, portfolioID,
	accountID, strategyVersionID, strategyKey string, now time.Time,
) {
	t.Helper()
	hash := researchRegistryPayloadHash([]byte("ownership-" + suffix))
	repository, err := NewPortfolioRiskRepository(pool)
	if err != nil {
		t.Fatal(err)
	}
	journalID := "journal-initialization-" + suffix
	write := PortfolioRiskInitializationWrite{
		Journal: generated.InsertJournalTransactionParams{ID: journalID, TransactionType: "portfolio_initialization", RunID: runID,
			PortfolioID: portfolioID, ConfigurationID: "configuration-mean_reversion", CausationID: "initialization-" + suffix,
			CorrelationID: "initialization-" + suffix, RecordedAt: pgTimestamp(now), IngestOrdinal: 1},
		Entries: []generated.InsertLedgerEntryParams{
			{TransactionID: journalID, LineNumber: 1, AccountClass: "available_asset", AccountOwner: strategyKey, AssetSymbol: "USDT", Direction: "debit", Quantity: "500"},
			{TransactionID: journalID, LineNumber: 2, AccountClass: "external_equity", AccountOwner: strategyKey, AssetSymbol: "USDT", Direction: "credit", Quantity: "500"},
		},
		Ownership: generated.InsertPortfolioOwnershipParams{AccountID: accountID, PortfolioID: portfolioID, ExchangeID: "binance",
			StrategyVersionID: strategyVersionID, StrategyKey: strategyKey, InitializationTransactionID: journalID,
			NumeraireAsset: "USDT", OwnershipHash: hash, CreatedAt: pgTimestamp(now)},
		Snapshot: generated.InsertPortfolioRiskAccountSnapshotParams{ID: "snapshot-initialization-" + suffix, AccountID: accountID, Revision: 1,
			SnapshotHash: hash, CanonicalPayload: []byte("{}"), RecordedAt: pgTimestamp(now), OwnershipHash: hash,
			BalancesHash: hash, PositionsHash: hash, ReservationsHash: hash, RiskStateHash: hash},
	}
	if err = repository.InitializeTrend(ctx, write); err != nil {
		t.Fatal(err)
	}
}

func assertMeanReversionFinalConsumption(t *testing.T, ctx context.Context, pool *pgxpool.Pool, repository *MeanReversionRepository, now time.Time) {
	t.Helper()
	hash := strings.Repeat("e", 64)
	write := generated.ConsumeFinalTestGenerationParams{ResearchGenerationID: "generation-mean_reversion-1", ConsumedByRunID: "run-mean_reversion",
		ConsumptionHash: hash, ConsumedAt: pgTimestamp(now)}
	if err := repository.ConsumeFinalTest(ctx, write); err != nil {
		t.Fatal(err)
	}
	if err := repository.ConsumeFinalTest(ctx, write); err == nil {
		t.Fatal("mean reversion final-test generation consumed twice")
	}
}

func assertMeanReversionDecisionPersistence(t *testing.T, ctx context.Context, pool *pgxpool.Pool, repository *MeanReversionRepository,
	coherentViewID string, now time.Time,
) {
	t.Helper()
	seedMeanReversionDecisionRows(t, ctx, pool, coherentViewID, now)
	canonical := []byte(`{"reason":"entry_accepted","strategy":"mean_reversion","version":"mean-reversion@1.0.0"}`)
	hash := researchRegistryPayloadHash(canonical)
	write := meanReversionDecisionWrite(coherentViewID, now, canonical, hash)
	if err := repository.RecordDecision(ctx, write); err != nil {
		t.Fatal(err)
	}
	assertMeanReversionDecisionRestored(t, ctx, repository, write, canonical, hash, coherentViewID)
	if _, err := pool.Exec(ctx, "UPDATE mean_reversion_decisions SET portfolio_revision=2 WHERE decision_id='decision-mean_reversion'"); err == nil {
		t.Fatal("immutable mean reversion decision accepted update")
	}
	if _, err := pool.Exec(ctx, "DELETE FROM mean_reversion_decisions WHERE decision_id='decision-mean_reversion'"); err == nil {
		t.Fatal("immutable mean reversion decision accepted delete")
	}
	assertMeanReversionDecisionReferenceFailures(t, ctx, repository, write)
}

func seedMeanReversionDecisionRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, coherentViewID string, now time.Time) {
	t.Helper()
	for ordinal, id := range []string{"decision-mean_reversion", "decision-mean_reversion-bad-reference", "decision-mean_reversion-model-type", "decision-mean_reversion-cross-owner"} {
		_, err := pool.Exec(ctx, `INSERT INTO decisions
(id,run_id,configuration_id,strategy_version_id,outcome,reason_code,causation_id,decided_at,ingest_ordinal,decision_market_scope,cross_market_view_id)
VALUES ($1,'run-mean_reversion','configuration-mean_reversion','mean-reversion-1-0-0','approved','entry_accepted',$2,$3,$4,'cross_market',$5)`,
			id, "cause-"+id, now, ordinal+1, coherentViewID)
		if err != nil {
			t.Fatal(err)
		}
	}
}

func meanReversionDecisionWrite(coherentViewID string, now time.Time, canonical []byte,
	hash string,
) generated.InsertMeanReversionDecisionParams {
	return generated.InsertMeanReversionDecisionParams{DecisionID: "decision-mean_reversion", StrategyVersionID: "mean-reversion-1-0-0",
		ConfigurationID: "configuration-mean_reversion", ExplanationHash: hash, CanonicalExplanation: canonical,
		PrimaryCandleViewID: "primary-candles-mean_reversion", PrimaryCandleViewRevision: 28,
		HigherCandleViewID: "higher-candles-mean_reversion", HigherCandleViewRevision: 210,
		MarketViewID: "market-view-mean_reversion", MarketViewRevision: 4, CoherentViewID: coherentViewID,
		CoherentVersionVectorHash: coherentViewID, PortfolioOwnershipAccountID: "account-mean_reversion",
		InstrumentMetadataID: "metadata-mean_reversion", AssetEligibilityVersion: 1, PortfolioRevision: 1, PositionRevision: 1,
		RiskPolicyID: "risk-mean_reversion", RiskPolicyVersion: 1, RiskPolicyHash: strings.Repeat("f", 64), FeeModelID: "fixed-bps-v1",
		LatencyModelID: "fixed-zero-v1", FillModelID: "fill-v1", SlippageModelID: "slippage-v1",
		GapModelID: "gap-v1", CorrelationModelID: "correlation-v1", CorrelationID: "correlation-mean_reversion",
		CausationID: "cause-decision-mean_reversion", RecordedAt: pgTimestamp(now)}
}

func assertMeanReversionDecisionRestored(t *testing.T, ctx context.Context, repository *MeanReversionRepository,
	write generated.InsertMeanReversionDecisionParams, canonical []byte, hash, coherentViewID string,
) {
	t.Helper()
	restored, err := repository.LoadDecision(ctx, write.DecisionID)
	if err != nil || restored.DecisionID != write.DecisionID || hashText(restored.ExplanationHash) != hash ||
		!bytes.Equal(restored.CanonicalExplanation, canonical) || hashText(restored.CoherentVersionVectorHash) != coherentViewID {
		t.Fatalf("mean reversion decision restart/load mismatch: %#v %v", restored, err)
	}
}

func assertMeanReversionDecisionReferenceFailures(t *testing.T, ctx context.Context, repository *MeanReversionRepository,
	write generated.InsertMeanReversionDecisionParams,
) {
	t.Helper()
	badReference := write
	badReference.DecisionID = "decision-mean_reversion-bad-reference"
	badReference.RiskPolicyHash = strings.Repeat("0", 64)
	if err := repository.RecordDecision(ctx, badReference); err == nil {
		t.Fatal("mean reversion decision with mismatched immutable risk reference committed")
	}
	wrongModelType := write
	wrongModelType.DecisionID = "decision-mean_reversion-model-type"
	wrongModelType.FeeModelID = "fixed-zero-v1"
	if err := repository.RecordDecision(ctx, wrongModelType); err == nil {
		t.Fatal("mean reversion decision with a cross-typed model reference committed")
	}
	crossOwner := write
	crossOwner.DecisionID = "decision-mean_reversion-cross-owner"
	crossOwner.PortfolioOwnershipAccountID = "account-trend-mean_reversion"
	if err := repository.RecordDecision(ctx, crossOwner); err == nil {
		t.Fatal("mean reversion decision crossed strategy ownership")
	}
}

func assertMeanReversionReportPersistence(t *testing.T, ctx context.Context, pool *pgxpool.Pool, repository *MeanReversionRepository, now time.Time) {
	t.Helper()
	canonical, manifestHash := meanReversionResearchManifest(t, now)
	if _, err := researchcontract.ValidateMeanReversionReportCanonical(canonical, manifestHash, "generation-mean_reversion-1", "run-mean_reversion"); err != nil {
		t.Fatal(err)
	}
	write := generated.InsertResearchReportParams{ID: "report-mean_reversion", ResearchGenerationID: "generation-mean_reversion-1",
		ManifestHash: manifestHash, ArtifactHash: strings.Repeat("9", 64), CanonicalManifest: canonical,
		RunReferences: []byte(`["run-mean_reversion"]`), ConfidenceLabel: "local_tier_b",
		PlatformCorrectness: "locally deterministic platform qualification passed",
		StrategyEvidence:    "research evidence remains provisional and uncertain", ViabilityDisposition: "undetermined",
		DisclaimerPolicy: "no_production_profitability_claim", CreatedAt: pgTimestamp(now)}
	if err := repository.RecordReport(ctx, write); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "DELETE FROM research_reports WHERE id='report-mean_reversion'"); err == nil {
		t.Fatal("immutable mean reversion research report deleted")
	}
}

func meanReversionResearchManifest(t *testing.T, now time.Time) ([]byte, string) {
	t.Helper()
	result := func(name string) researchcontract.ResultSlice {
		return researchcontract.ResultSlice{Name: name, NetReturn: "0.01", MaxDrawdown: "0.02", Trades: 20}
	}
	results := func(names ...string) []researchcontract.ResultSlice {
		values := make([]researchcontract.ResultSlice, len(names))
		for index, name := range names {
			values[index] = result(name)
		}
		return values
	}
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	input := researchcontract.ReportInput{ResearchGenerationID: "generation-mean_reversion-1", Hypothesis: "registered mean reversion hypothesis",
		PrimaryMetric: "risk_adjusted_net_return", Split: researchcontract.ChronologicalSplit{
			Train:      researchcontract.Window{Name: "train", Start: start, End: start.Add(100 * time.Hour)},
			Validation: researchcontract.Window{Name: "validation", Start: start.Add(100 * time.Hour), End: start.Add(150 * time.Hour)},
			FinalTest:  researchcontract.Window{Name: "final_test", Start: start.Add(150 * time.Hour), End: start.Add(200 * time.Hour)}},
		WalkForward:  []researchcontract.WalkForwardFold{{TrainStart: 0, TrainEnd: 40, ValidationStart: 40, ValidationEnd: 50, TestStart: 50, TestEnd: 60}},
		Confidence:   researchcontract.ConfidenceInterval{Lower: "-0.01", Point: "0.01", Upper: "0.02", Iterations: 100, BlockSize: 2, SeedHash: strings.Repeat("8", 64)},
		Neighborhood: results("base", "entry_low", "entry_high"),
		Capacity:     []researchcontract.CapacityPoint{{Notional: "10", NetReturn: "0.01", FillRate: "1"}, {Notional: "75", NetReturn: "0.005", FillRate: "0.9"}},
		Stress:       results("fee", "spread", "slippage", "latency", "gap", "missed_fill"),
		Benchmarks:   results("cash", "buy_and_hold", "static_inventory"),
		Breakdowns: map[string][]researchcontract.ResultSlice{"asset": results("BTC"), "regime": results("range"),
			"holding_period": results("short"), "fast_decline_failure": results("fast_decline"),
			"maximum_adverse_excursion": results("mae"), "trend_filter_comparison": results("disabled"), "drawdown": results("peak")},
		Rejections: map[string]uint64{"mean_reversion.reject.dangerous_regime": 4, "mean_reversion.reject.adx": 3,
			"mean_reversion.reject.market_quality": 2, "mean_reversion.failure.fast_decline": 1},
		RunReferences: []string{"run-mean_reversion"}, ConfidenceLabel: "local_tier_b",
		PlatformCorrectness: "locally deterministic platform qualification passed",
		StrategyEvidence:    "research evidence remains provisional and uncertain", ViabilityDisposition: "undetermined", CreatedAt: now}
	manifest, err := researchcontract.BuildMeanReversionReport(input)
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	return canonical, manifest.ManifestHash
}
