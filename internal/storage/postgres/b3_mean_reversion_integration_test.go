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

func TestB3PostgresCleanInstallQualification(t *testing.T) {
	ctx, pool := openB3TestDatabase(t, "AXIOM_B3_TEST_DSN")
	defer pool.Close()
	assertPostgres18(t, ctx, pool)
	assertEmptyTestDatabase(t, ctx, pool)
	migrations, err := Migrations()
	if err != nil || len(migrations) != 15 {
		t.Fatalf("migration catalog=%d error=%v", len(migrations), err)
	}
	if applied, applyErr := ApplyMigrations(ctx, pool); applyErr != nil || applied != len(migrations) {
		t.Fatalf("clean migrations=%d/%d error=%v", applied, len(migrations), applyErr)
	}
	assertB3SchemaAndPersistence(t, ctx, pool)
}

func TestB3PostgresB2ToB3UpgradeQualification(t *testing.T) {
	ctx, pool := openB3TestDatabase(t, "AXIOM_B3_UPGRADE_TEST_DSN")
	defer pool.Close()
	assertPostgres18(t, ctx, pool)
	assertEmptyTestDatabase(t, ctx, pool)
	migrations, err := Migrations()
	if err != nil || len(migrations) != 15 {
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
			t.Fatalf("B2 migration %s changed=%t error=%v", migration.Name, changed, applyErr)
		}
	}
	connection.Release()
	if applied, applyErr := ApplyMigrations(ctx, pool); applyErr != nil || applied != 1 {
		t.Fatalf("B2-to-B3 migration=%d error=%v", applied, applyErr)
	}
	assertB3SchemaAndPersistence(t, ctx, pool)
}

func openB3TestDatabase(t *testing.T, environment string) (context.Context, *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv(environment)
	if dsn == "" {
		t.Skip(environment + " is not set")
	}
	configuration, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(configuration.ConnConfig.Database, "_b3_test") {
		t.Fatal("B3 integration requires a dedicated database ending _b3_test")
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

func assertB3SchemaAndPersistence(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	seedB3References(t, ctx, pool, now)
	repository, err := NewB3Repository(pool)
	if err != nil {
		t.Fatal(err)
	}
	registration := b3RegistrationFixture(t, now)
	if err = repository.Register(ctx, registration); err != nil {
		t.Fatal(err)
	}
	if err = repository.Register(ctx, registration); err == nil {
		t.Fatal("duplicate B3 registration committed")
	}
	assertB3ParameterGraph(t, ctx, pool)
	view := b2CoherentFixture(t, now.Add(time.Minute))
	coherentRepository, err := NewCoherentViewRepository(pool)
	if err != nil || coherentRepository.Commit(ctx, view, now.Add(time.Minute)) != nil {
		t.Fatalf("B3 coherent view unavailable: %v", err)
	}
	seedB3RuntimeReferences(t, ctx, pool, now.Add(2*time.Minute))
	assertB3FinalConsumption(t, ctx, pool, repository, now.Add(3*time.Minute))
	assertB3DecisionPersistence(t, ctx, pool, repository, view.Identity(), now.Add(4*time.Minute))
	assertB3ReportPersistence(t, ctx, pool, repository, now.Add(5*time.Minute))
}

func seedB3References(t *testing.T, ctx context.Context, pool *pgxpool.Pool, now time.Time) {
	t.Helper()
	configuration, err := json.Marshal(config.DefaultV1BConfiguration())
	if err != nil {
		t.Fatal(err)
	}
	hash := strings.Repeat("a", 64)
	statements := []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO configuration_versions(id,version,configuration_hash,canonical_payload,actor,recorded_at)
VALUES ('configuration-b3',1,$1,$2,'b3-qualification',$3)`, []any{a10PayloadHash(configuration), configuration, now}},
		{`INSERT INTO configuration_activations(configuration_id,actor,reason,activated_at)
VALUES ('configuration-b3','b3-qualification','immutable B3 baseline',$1)`, []any{now}},
		{`INSERT INTO dataset_manifests(id,dataset_hash,schema_compatibility,coverage_start,coverage_end,state,created_at)
VALUES ('dataset-b3',$1,'b3-normalized-v1',$2,$3,'building',$2)`, []any{hash, now.Add(-24 * time.Hour), now}},
		{"UPDATE dataset_manifests SET state='ready' WHERE id='dataset-b3'", nil},
		{"UPDATE dataset_manifests SET state='qualified' WHERE id='dataset-b3'", nil},
		{"INSERT INTO assets(symbol) VALUES ('USDT'),('BTC'),('ETH')", nil},
		{"INSERT INTO instruments(id,base_asset,quote_asset,product) VALUES ('BTCUSDT','BTC','USDT','spot')", nil},
		{`INSERT INTO instrument_metadata_versions
(id,exchange_id,instrument_id,version,price_tick,quantity_step,minimum_quantity,minimum_notional,effective_at,recorded_at)
VALUES ('metadata-b3','binance','BTCUSDT',1,0.01,0.00001,0.00001,10,$1,$1)`, []any{now}},
		{`INSERT INTO model_versions(id,model_type,version,model_hash,canonical_payload,used_at,created_at) VALUES
('fixed-bps-v1','fee',1,$1,'{}',NULL,$2),
('fixed-zero-v1','latency',1,$1,'{}',NULL,$2),
('fill-v1','fill',1,$1,'{}',NULL,$2),
('slippage-v1','slippage',1,$1,'{}',NULL,$2),
('gap-v1','gap',1,$1,'{}',NULL,$2),
('correlation-v1','correlation',1,$1,'{}',NULL,$2)`, []any{hash, now}},
		{`INSERT INTO risk_policies(id,version,scope_kind,scope_id,state,policy_hash,canonical_payload,effective_at,recorded_at)
VALUES ('risk-b3',1,'global','global','NORMAL',$1,'{}',$2,$2)`, []any{strings.Repeat("f", 64), now}},
	}
	for index, statement := range statements {
		if _, err = pool.Exec(ctx, statement.sql, statement.args...); err != nil {
			t.Fatalf("B3 seed %d failed: %v", index+1, err)
		}
	}
}

func b3RegistrationFixture(t *testing.T, now time.Time) B3RegistrationWrite {
	t.Helper()
	hash := strings.Repeat("b", 64)
	manifest := []byte(`{"version":"mean-reversion.v1b.1","scope":"spot-only"}`)
	manifestHash := a10PayloadHash(manifest)
	author, notes, metric := "authoritative_specification", "Tier B local engineering qualification", "risk_adjusted_net_return"
	generation, minimumSamples := int32(1), int64(100)
	stop, reject, promote := "registered sample boundary", "confidence interval crosses rejection floor", "formal gates only"
	commit := strings.Repeat("c", 40)
	write := A10RegistrationWrite{
		Definition: generated.InsertA10StrategyDefinitionParams{ID: "mean-reversion", Name: "Mean Reversion V1B", Family: "mean_reversion"},
		Version: generated.InsertA10StrategyVersionParams{ID: "mean-reversion-v1b-1", StrategyID: "mean-reversion", Version: 1,
			ImplementationHash: hash, PromotionStatus: "research", CreatedAt: pgTimestamp(now), ManifestHash: manifestHash,
			CanonicalManifest: manifest, CodeCommit: &commit, SupportedModes: []string{"backtest", "replay", "paper", "shadow"},
			Author: &author, Notes: &notes},
		Experiment: generated.InsertA10ExperimentRegistrationParams{ID: "experiment-b3", StrategyVersionID: "mean-reversion-v1b-1",
			ConfigurationID: "configuration-b3", DatasetID: "dataset-b3", Hypothesis: "registered mean-reversion hypothesis",
			Status: "registered", RegisteredAt: pgTimestamp(now), Generation: &generation, PrimaryMetric: &metric,
			TrainStart: pgTimestamp(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)), TrainEnd: pgTimestamp(time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)),
			ValidationStart: pgTimestamp(time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)), ValidationEnd: pgTimestamp(time.Date(2025, 9, 1, 0, 0, 0, 0, time.UTC)),
			FinalTestStart: pgTimestamp(time.Date(2025, 9, 1, 0, 0, 0, 0, time.UTC)), FinalTestEnd: pgTimestamp(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)),
			SearchSpace: []byte(`{"locked":"baseline"}`), ParameterNeighborhood: []byte(`{"entry_zscore":["-1.9","-2","-2.1"]}`),
			ModelAssumptions:     []byte(`{"fee":"fixed-bps-v1","latency":"fixed-zero-v1","fill":"fill-v1","slippage":"slippage-v1","gap":"gap-v1","correlation":"correlation-v1"}`),
			BenchmarkAssumptions: []byte(`{"cash":true,"buy_hold":true,"static_inventory":true}`), MinimumSamples: &minimumSamples,
			StoppingRule: &stop, RejectionRule: &reject, PromotionRule: &promote, RegisteredSeedHash: hash},
		Generation: generated.InsertResearchGenerationParams{ID: "generation-b3-1", ExperimentID: "experiment-b3", Generation: 1,
			FinalWindowHash: hash, RegistrationHash: hash, RegisteredAt: pgTimestamp(now)},
	}
	write.Parameters = b3ParameterWrites(t)
	return B3RegistrationWrite(write)
}

func b3ParameterWrites(t *testing.T) []generated.InsertA10StrategyParameterParams {
	t.Helper()
	approvedAt := pgTimestamp(time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC))
	parameters := make([]generated.InsertA10StrategyParameterParams, 0, config.MeanReversionParameterCount)
	for _, parameter := range config.DefaultV1BConfiguration().MeanReversion.Parameters {
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
		parameters = append(parameters, generated.InsertA10StrategyParameterParams{
			StrategyVersionID: "mean-reversion-v1b-1", ParameterName: parameter.ID, DecimalValue: parameter.Value,
			Unit: parameter.Unit, Description: &description, AlgorithmVersion: &algorithm, MinimumValue: &minimum,
			MaximumValue: &maximum, MinimumInclusive: &minimumInclusive, MaximumInclusive: &maximumInclusive,
			DecimalScale: &scale, Rounding: &rounding, Cadence: &cadence, WarmUp: &warmup, Mutability: &mutability,
			ModelDependencies: dependencies, EvaluationTimezone: &timezone, ChangeBehavior: &change, ApprovalActor: &actor,
			ApprovalReference: &reference, ApprovedAt: approvedAt, ChangeReason: &reason,
		})
	}
	return parameters
}

func assertB3ParameterGraph(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var count, complete int
	err := pool.QueryRow(ctx, `SELECT count(*),count(*) FILTER (WHERE description IS NOT NULL AND algorithm_version IS NOT NULL
AND minimum_value IS NOT NULL AND maximum_value IS NOT NULL AND decimal_scale IS NOT NULL AND rounding IS NOT NULL
AND cadence IS NOT NULL AND warm_up IS NOT NULL AND mutability IS NOT NULL AND model_dependencies IS NOT NULL
AND evaluation_timezone='UTC' AND change_behavior IS NOT NULL AND approval_actor IS NOT NULL
AND approval_reference IS NOT NULL AND approved_at IS NOT NULL AND change_reason IS NOT NULL)
FROM strategy_parameters WHERE strategy_version_id='mean-reversion-v1b-1'`).Scan(&count, &complete)
	if err != nil || count != config.MeanReversionParameterCount || complete != count {
		t.Fatalf("B3 parameter graph=%d complete=%d error=%v", count, complete, err)
	}
	if _, err = pool.Exec(ctx, `UPDATE strategy_parameters SET decimal_value='99'
WHERE strategy_version_id='mean-reversion-v1b-1' AND parameter_name='mean_reversion.entry_zscore'`); err == nil {
		t.Fatal("immutable B3 parameter mutated")
	}
}

func seedB3RuntimeReferences(t *testing.T, ctx context.Context, pool *pgxpool.Pool, now time.Time) {
	t.Helper()
	hash := strings.Repeat("d", 64)
	statements := []struct {
		sql  string
		args []any
	}{
		{"INSERT INTO runs(id,mode,configuration_id,strategy_version_id,dataset_id,root_seed_hash,reproducibility_hash,state,created_at) VALUES ('run-b3','backtest','configuration-b3','mean-reversion-v1b-1','dataset-b3',$1,$1,'created',$2)", []any{hash, now}},
		{"INSERT INTO portfolios VALUES ('portfolio-b3','Mean Reversion V1B','USDT',$1)", []any{now}},
		{"INSERT INTO virtual_accounts VALUES ('account-b3','portfolio-b3','run-b3','mean-reversion-binance',$1)", []any{now}},
		{"INSERT INTO virtual_balances VALUES ('account-b3','USDT',500,0,1,$1),('account-b3','BTC',0,0,1,$1),('account-b3','ETH',0,0,1,$1)", []any{now}},
	}
	for index, statement := range statements {
		if _, err := pool.Exec(ctx, statement.sql, statement.args...); err != nil {
			t.Fatalf("B3 runtime seed %d failed: %v", index+1, err)
		}
	}
	initializeB3Ownership(t, ctx, pool, "b3", "run-b3", "portfolio-b3", "account-b3", "mean-reversion-v1b-1", "mean_reversion", now)
	if _, err := pool.Exec(ctx, "INSERT INTO strategy_definitions VALUES ('trend-b3','Trend B3 cross-owner fixture','trend')"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO strategy_versions(id,strategy_id,version,implementation_hash,promotion_status,created_at) VALUES ('trend-b3-v1','trend-b3',1,$1,'research',$2)", hash, now); err != nil {
		t.Fatal(err)
	}
	statements = []struct {
		sql  string
		args []any
	}{
		{"INSERT INTO portfolios VALUES ('portfolio-trend-b3','Trend B3 Ownership Fixture','USDT',$1)", []any{now}},
		{"INSERT INTO virtual_accounts VALUES ('account-trend-b3','portfolio-trend-b3','run-b3','trend-binance',$1)", []any{now}},
		{"INSERT INTO virtual_balances VALUES ('account-trend-b3','USDT',500,0,1,$1),('account-trend-b3','BTC',0,0,1,$1),('account-trend-b3','ETH',0,0,1,$1)", []any{now}},
	}
	for index, statement := range statements {
		if _, err := pool.Exec(ctx, statement.sql, statement.args...); err != nil {
			t.Fatalf("cross-strategy seed %d failed: %v", index+1, err)
		}
	}
	initializeB3Ownership(t, ctx, pool, "trend-b3", "run-b3", "portfolio-trend-b3", "account-trend-b3", "trend-b3-v1", "trend", now)
}

func initializeB3Ownership(t *testing.T, ctx context.Context, pool *pgxpool.Pool, suffix, runID, portfolioID,
	accountID, strategyVersionID, strategyKey string, now time.Time,
) {
	t.Helper()
	hash := a10PayloadHash([]byte("ownership-" + suffix))
	repository, err := NewA9Repository(pool)
	if err != nil {
		t.Fatal(err)
	}
	journalID := "journal-initialization-" + suffix
	write := A9InitializationWrite{
		Journal: generated.InsertJournalTransactionParams{ID: journalID, TransactionType: "portfolio_initialization", RunID: runID,
			PortfolioID: portfolioID, ConfigurationID: "configuration-b3", CausationID: "initialization-" + suffix,
			CorrelationID: "initialization-" + suffix, RecordedAt: pgTimestamp(now), IngestOrdinal: 1},
		Entries: []generated.InsertLedgerEntryParams{
			{TransactionID: journalID, LineNumber: 1, AccountClass: "available_asset", AccountOwner: strategyKey, AssetSymbol: "USDT", Direction: "debit", Quantity: "500"},
			{TransactionID: journalID, LineNumber: 2, AccountClass: "external_equity", AccountOwner: strategyKey, AssetSymbol: "USDT", Direction: "credit", Quantity: "500"},
		},
		Ownership: generated.InsertPortfolioOwnershipParams{AccountID: accountID, PortfolioID: portfolioID, ExchangeID: "binance",
			StrategyVersionID: strategyVersionID, StrategyKey: strategyKey, InitializationTransactionID: journalID,
			NumeraireAsset: "USDT", OwnershipHash: hash, CreatedAt: pgTimestamp(now)},
		Snapshot: generated.InsertA9AccountSnapshotParams{ID: "snapshot-initialization-" + suffix, AccountID: accountID, Revision: 1,
			SnapshotHash: hash, CanonicalPayload: []byte("{}"), RecordedAt: pgTimestamp(now), OwnershipHash: hash,
			BalancesHash: hash, PositionsHash: hash, ReservationsHash: hash, RiskStateHash: hash},
	}
	if err = repository.InitializeV1ATrend(ctx, write); err != nil {
		t.Fatal(err)
	}
}

func assertB3FinalConsumption(t *testing.T, ctx context.Context, pool *pgxpool.Pool, repository *B3Repository, now time.Time) {
	t.Helper()
	hash := strings.Repeat("e", 64)
	write := generated.ConsumeFinalTestGenerationParams{ResearchGenerationID: "generation-b3-1", ConsumedByRunID: "run-b3",
		ConsumptionHash: hash, ConsumedAt: pgTimestamp(now)}
	if err := repository.ConsumeFinalTest(ctx, write); err != nil {
		t.Fatal(err)
	}
	if err := repository.ConsumeFinalTest(ctx, write); err == nil {
		t.Fatal("B3 final-test generation consumed twice")
	}
}

func assertB3DecisionPersistence(t *testing.T, ctx context.Context, pool *pgxpool.Pool, repository *B3Repository,
	coherentViewID string, now time.Time,
) {
	t.Helper()
	seedB3DecisionRows(t, ctx, pool, coherentViewID, now)
	canonical := []byte(`{"reason":"entry_accepted","strategy":"mean_reversion","version":"mean-reversion.v1b.1"}`)
	hash := a10PayloadHash(canonical)
	write := b3DecisionWrite(coherentViewID, now, canonical, hash)
	if err := repository.RecordDecision(ctx, write); err != nil {
		t.Fatal(err)
	}
	assertB3DecisionRestored(t, ctx, repository, write, canonical, hash, coherentViewID)
	if _, err := pool.Exec(ctx, "UPDATE mean_reversion_decisions SET portfolio_revision=2 WHERE decision_id='decision-b3'"); err == nil {
		t.Fatal("immutable B3 decision accepted update")
	}
	if _, err := pool.Exec(ctx, "DELETE FROM mean_reversion_decisions WHERE decision_id='decision-b3'"); err == nil {
		t.Fatal("immutable B3 decision accepted delete")
	}
	assertB3DecisionReferenceFailures(t, ctx, repository, write)
}

func seedB3DecisionRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, coherentViewID string, now time.Time) {
	t.Helper()
	for ordinal, id := range []string{"decision-b3", "decision-b3-bad-reference", "decision-b3-model-type", "decision-b3-cross-owner"} {
		_, err := pool.Exec(ctx, `INSERT INTO decisions
(id,run_id,configuration_id,strategy_version_id,outcome,reason_code,causation_id,decided_at,ingest_ordinal,decision_market_scope,cross_market_view_id)
VALUES ($1,'run-b3','configuration-b3','mean-reversion-v1b-1','approved','entry_accepted',$2,$3,$4,'cross_market',$5)`,
			id, "cause-"+id, now, ordinal+1, coherentViewID)
		if err != nil {
			t.Fatal(err)
		}
	}
}

func b3DecisionWrite(coherentViewID string, now time.Time, canonical []byte,
	hash string,
) generated.InsertMeanReversionDecisionParams {
	return generated.InsertMeanReversionDecisionParams{DecisionID: "decision-b3", StrategyVersionID: "mean-reversion-v1b-1",
		ConfigurationID: "configuration-b3", ExplanationHash: hash, CanonicalExplanation: canonical,
		PrimaryCandleViewID: "primary-candles-b3", PrimaryCandleViewRevision: 28,
		HigherCandleViewID: "higher-candles-b3", HigherCandleViewRevision: 210,
		MarketViewID: "market-view-b3", MarketViewRevision: 4, CoherentViewID: coherentViewID,
		CoherentVersionVectorHash: coherentViewID, PortfolioOwnershipAccountID: "account-b3",
		InstrumentMetadataID: "metadata-b3", AssetEligibilityVersion: 1, PortfolioRevision: 1, PositionRevision: 1,
		RiskPolicyID: "risk-b3", RiskPolicyVersion: 1, RiskPolicyHash: strings.Repeat("f", 64), FeeModelID: "fixed-bps-v1",
		LatencyModelID: "fixed-zero-v1", FillModelID: "fill-v1", SlippageModelID: "slippage-v1",
		GapModelID: "gap-v1", CorrelationModelID: "correlation-v1", CorrelationID: "correlation-b3",
		CausationID: "cause-decision-b3", RecordedAt: pgTimestamp(now)}
}

func assertB3DecisionRestored(t *testing.T, ctx context.Context, repository *B3Repository,
	write generated.InsertMeanReversionDecisionParams, canonical []byte, hash, coherentViewID string,
) {
	t.Helper()
	restored, err := repository.LoadDecision(ctx, write.DecisionID)
	if err != nil || restored.DecisionID != write.DecisionID || hashText(restored.ExplanationHash) != hash ||
		!bytes.Equal(restored.CanonicalExplanation, canonical) || hashText(restored.CoherentVersionVectorHash) != coherentViewID {
		t.Fatalf("B3 decision restart/load mismatch: %#v %v", restored, err)
	}
}

func assertB3DecisionReferenceFailures(t *testing.T, ctx context.Context, repository *B3Repository,
	write generated.InsertMeanReversionDecisionParams,
) {
	t.Helper()
	badReference := write
	badReference.DecisionID = "decision-b3-bad-reference"
	badReference.RiskPolicyHash = strings.Repeat("0", 64)
	if err := repository.RecordDecision(ctx, badReference); err == nil {
		t.Fatal("B3 decision with mismatched immutable risk reference committed")
	}
	wrongModelType := write
	wrongModelType.DecisionID = "decision-b3-model-type"
	wrongModelType.FeeModelID = "fixed-zero-v1"
	if err := repository.RecordDecision(ctx, wrongModelType); err == nil {
		t.Fatal("B3 decision with a cross-typed model reference committed")
	}
	crossOwner := write
	crossOwner.DecisionID = "decision-b3-cross-owner"
	crossOwner.PortfolioOwnershipAccountID = "account-trend-b3"
	if err := repository.RecordDecision(ctx, crossOwner); err == nil {
		t.Fatal("B3 decision crossed strategy ownership")
	}
}

func assertB3ReportPersistence(t *testing.T, ctx context.Context, pool *pgxpool.Pool, repository *B3Repository, now time.Time) {
	t.Helper()
	canonical, manifestHash := b3ResearchManifest(t, now)
	if _, err := researchcontract.ValidateMeanReversionReportCanonical(canonical, manifestHash, "generation-b3-1", "run-b3"); err != nil {
		t.Fatal(err)
	}
	write := generated.InsertResearchReportParams{ID: "report-b3", ResearchGenerationID: "generation-b3-1",
		ManifestHash: manifestHash, ArtifactHash: strings.Repeat("9", 64), CanonicalManifest: canonical,
		RunReferences: []byte(`["run-b3"]`), ConfidenceLabel: "local_tier_b",
		PlatformCorrectness: "locally deterministic platform qualification passed",
		StrategyEvidence:    "research evidence remains provisional and uncertain", ViabilityDisposition: "undetermined",
		DisclaimerPolicy: "no_production_profitability_claim", CreatedAt: pgTimestamp(now)}
	if err := repository.RecordReport(ctx, write); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "DELETE FROM research_reports WHERE id='report-b3'"); err == nil {
		t.Fatal("immutable B3 research report deleted")
	}
}

func b3ResearchManifest(t *testing.T, now time.Time) ([]byte, string) {
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
	input := researchcontract.ReportInput{ResearchGenerationID: "generation-b3-1", Hypothesis: "registered B3 hypothesis",
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
		RunReferences: []string{"run-b3"}, ConfidenceLabel: "local_tier_b",
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
