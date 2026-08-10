package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"axiom/internal/config"
	"axiom/internal/recorder"
	"axiom/internal/storage/postgres/generated"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestResearchRegistryPostgresTrendResearchQualification(t *testing.T) {
	dsn := os.Getenv("AXIOM_RESEARCH_REGISTRY_TEST_DSN")
	if dsn == "" {
		t.Skip("AXIOM_RESEARCH_REGISTRY_TEST_DSN is not set")
	}
	configuration, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(configuration.ConnConfig.Database, "_research_registry_test") {
		t.Fatal("research registry integration requires a dedicated database ending _research_registry_test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, configuration)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	assertEmptyTestDatabase(t, ctx, pool)
	migrations, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	if applied, applyErr := ApplyMigrations(ctx, pool); applyErr != nil || applied != len(migrations) {
		t.Fatalf("research registry migrations = %d %v", applied, applyErr)
	}
	seedResearchRegistryReferences(t, ctx, pool)
	repository, err := NewResearchRegistryRepository(pool)
	if err != nil {
		t.Fatal(err)
	}
	write := researchRegistryRegistrationFixture()
	if err = repository.Register(ctx, write); err != nil {
		t.Fatal(err)
	}
	if err = repository.Register(ctx, write); err == nil {
		t.Fatal("duplicate immutable registration committed")
	}
	assertResearchRegistryRegistration(t, ctx, pool)
	assertResearchRegistryFinalConsumption(t, ctx, pool, repository)
	assertResearchRegistryDecision(t, ctx, pool, repository)
	assertResearchRegistryReport(t, ctx, pool, repository)
}

func seedResearchRegistryReferences(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	seedResearchRegistryReferenceRows(t, ctx, pool, "public_market")
	qualifyResearchRegistryDataset(t, ctx, pool)
}

func seedResearchRegistryReferenceRows(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	datasetKind string,
) {
	t.Helper()
	now := time.Date(2026, 7, 16, 8, 0, 0, 0, time.UTC)
	hash := strings.Repeat("a", 64)
	canonicalConfiguration, err := json.Marshal(config.DefaultConfiguration())
	if err != nil {
		t.Fatal(err)
	}
	configurationHash := researchRegistryPayloadHash(canonicalConfiguration)
	datasetHash, recorderDatasetID, manifestPath, sourceCommit := hash, "", "", ""
	manifestRevision := any(nil)
	if selected := os.Getenv("AXIOM_OWNER_CONSOLE_E2E_DATASET_MANIFEST"); selected != "" {
		manifest, manifestErr := recorder.ReadManifest(selected)
		sourceCommit = os.Getenv("AXIOM_OWNER_CONSOLE_E2E_SOURCE_COMMIT")
		if manifestErr != nil || !manifest.Complete || len(manifest.Segments) < 2 || !ownerConsoleBuildIdentityValid(sourceCommit) {
			t.Fatalf("invalid owner console E2E dataset manifest: %#v %v", manifest, manifestErr)
		}
		datasetHash, recorderDatasetID, manifestPath = manifest.Hash, manifest.DatasetID, filepath.Base(selected)
		manifestRevision = int64(manifest.Revision)
	}
	statements := []struct {
		sql  string
		args []any
	}{
		{"INSERT INTO configuration_versions VALUES ('configuration-research_registry',1,$1,$2,'test',$3)", []any{configurationHash, canonicalConfiguration, now}},
		{"INSERT INTO configuration_activations(configuration_id,actor,reason,activated_at) VALUES('configuration-research_registry','test','research registry/owner console qualification baseline',$1)", []any{now}},
		{`INSERT INTO dataset_manifests
		  (id,dataset_hash,schema_compatibility,coverage_start,coverage_end,state,created_at,
		   recorder_dataset_id,manifest_revision,manifest_path,source_commit,dataset_kind)
		  VALUES ('dataset-public-data-formal-pending',$1,'public-data-normalized-v1',$2,$2,'building',$2,
		          nullif($3,''),$4,nullif($5,''),nullif($6,''),$7)`,
			[]any{datasetHash, now, recorderDatasetID, manifestRevision, manifestPath, sourceCommit, datasetKind}},
		{"INSERT INTO assets(symbol) VALUES ('USDT'),('BTC'),('ETH')", nil},
		{"INSERT INTO exchanges VALUES ('exchange-research_registry','binance','production_public')", nil},
		{"INSERT INTO instruments VALUES ('instrument-research_registry','BTC','USDT','spot'),('instrument-eth-research_registry','ETH','USDT','spot')", nil},
		{"INSERT INTO instrument_metadata_versions VALUES ('metadata-research_registry','exchange-research_registry','instrument-research_registry',1,0.01,0.00001,0.00001,10,$1,$1),('metadata-eth-research_registry','exchange-research_registry','instrument-eth-research_registry',1,0.01,0.00001,0.00001,10,$1,$1)", []any{now}},
		{"INSERT INTO model_versions VALUES ('fixed-bps-v1','fee',1,$1,'{}',NULL,$2),('fixed-zero-v1','latency',1,$1,'{}',NULL,$2),('fill-v1','fill',1,$1,'{}',NULL,$2),('slippage-v1','slippage',1,$1,'{}',NULL,$2),('gap-v1','gap',1,$1,'{}',NULL,$2)", []any{hash, now}},
	}
	for index, statement := range statements {
		if _, err := pool.Exec(ctx, statement.sql, statement.args...); err != nil {
			t.Fatalf("research registry seed %d failed: %v", index+1, err)
		}
	}
}

func qualifyResearchRegistryDataset(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	for _, state := range []string{"ready", "qualified"} {
		if _, err := pool.Exec(
			ctx,
			"UPDATE dataset_manifests SET state=$1 WHERE id='dataset-public-data-formal-pending'",
			state,
		); err != nil {
			t.Fatalf("research registry dataset %s transition failed: %v", state, err)
		}
	}
}

func researchRegistryRegistrationFixture() ResearchRegistryRegistrationWrite {
	now := pgTimestamp(time.Date(2026, 7, 16, 8, 1, 0, 0, time.UTC))
	hash := strings.Repeat("b", 64)
	manifest := []byte(`{"version":"trend-following@1.0.0"}`)
	manifestHash := researchRegistryPayloadHash(manifest)
	author, notes, metric := "owner", "Tier B local engineering evidence", "risk_adjusted_net_return"
	generation, minimumSamples := int32(1), int64(100)
	stop, reject, promote := "registered sample boundary", "confidence interval crosses rejection floor", "formal gates only"
	commit := strings.Repeat("c", 40)
	write := ResearchRegistryRegistrationWrite{
		Definition: generated.InsertResearchRegistryStrategyDefinitionParams{ID: "trend", Name: "Trend initial trend", Family: "trend"},
		Version: generated.InsertResearchRegistryStrategyVersionParams{ID: "trend-following-1-0-0", StrategyID: "trend", Version: 1,
			ImplementationHash: hash, PromotionStatus: "research", CreatedAt: now, ManifestHash: manifestHash,
			CanonicalManifest: manifest, CodeCommit: &commit,
			SupportedModes: []string{"backtest", "replay", "paper", "shadow"}, Author: &author, Notes: &notes},
		Experiment: generated.InsertResearchRegistryExperimentRegistrationParams{ID: "experiment-research_registry", StrategyVersionID: "trend-following-1-0-0",
			ConfigurationID: "configuration-research_registry", DatasetID: "dataset-public-data-formal-pending", Hypothesis: "registered before final test",
			Status: "registered", RegisteredAt: now, Generation: &generation, PrimaryMetric: &metric,
			TrainStart: pgTimestamp(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)), TrainEnd: pgTimestamp(time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)),
			ValidationStart: pgTimestamp(time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)), ValidationEnd: pgTimestamp(time.Date(2025, 9, 1, 0, 0, 0, 0, time.UTC)),
			FinalTestStart: pgTimestamp(time.Date(2025, 9, 1, 0, 0, 0, 0, time.UTC)), FinalTestEnd: pgTimestamp(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)),
			SearchSpace: []byte(`{"locked":"baseline"}`), ParameterNeighborhood: []byte(`{"atr":["2.25","2.5","2.75"]}`),
			ModelAssumptions:     []byte(`{"fee":"fixed-bps-v1","spread":"recorded","slippage":"slippage-v1","latency":"fixed-zero-v1","fill":"fill-v1","gap":"gap-v1"}`),
			BenchmarkAssumptions: []byte(`{"cash":true,"buy_hold":true,"static_inventory":true}`), MinimumSamples: &minimumSamples,
			StoppingRule: &stop, RejectionRule: &reject, PromotionRule: &promote, RegisteredSeedHash: hash},
		Generation: generated.InsertResearchGenerationParams{ID: "generation-research_registry-1", ExperimentID: "experiment-research_registry", Generation: 1,
			FinalWindowHash: hash, RegistrationHash: hash, RegisteredAt: now},
	}
	for index := 0; index < 16; index++ {
		name := "parameter-" + string(rune('a'+index))
		description, algorithm := "locked baseline parameter", "trend-following@1.0.0"
		minimum, maximum, rounding := "0", "1000", "half_even"
		cadence, warmup, mutability := "4h", "200_completed_candles", "immutable_per_run"
		scale := int32(18)
		inclusive := true
		write.Parameters = append(write.Parameters, generated.InsertResearchRegistryStrategyParameterParams{StrategyVersionID: "trend-following-1-0-0",
			ParameterName: name, DecimalValue: "1", Unit: "count", Description: &description, AlgorithmVersion: &algorithm,
			MinimumValue: &minimum, MaximumValue: &maximum, MinimumInclusive: &inclusive, MaximumInclusive: &inclusive,
			DecimalScale: &scale, Rounding: &rounding, Cadence: &cadence, WarmUp: &warmup, Mutability: &mutability,
			ModelDependencies: []byte(`[]`)})
	}
	return write
}

func assertResearchRegistryRegistration(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	for table, expected := range map[string]int{"strategy_versions": 1, "strategy_parameters": 16,
		"experiment_registrations": 1, "research_generations": 1} {
		var count int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != expected {
			t.Fatalf("research registry registration table %s = %d %v", table, count, err)
		}
	}
	if _, err := pool.Exec(ctx, "UPDATE research_generations SET generation=2 WHERE id='generation-research_registry-1'"); err == nil {
		t.Fatal("immutable research generation mutated")
	}
}

func assertResearchRegistryFinalConsumption(t *testing.T, ctx context.Context, pool *pgxpool.Pool, repository *ResearchRegistryRepository) {
	t.Helper()
	now := time.Date(2026, 7, 16, 8, 2, 0, 0, time.UTC)
	hash := strings.Repeat("d", 64)
	if _, err := pool.Exec(ctx, "INSERT INTO runs VALUES ('run-research_registry','backtest','configuration-research_registry','trend-following-1-0-0','dataset-public-data-formal-pending',$1,$1,'created',$2,NULL,NULL)", hash, now); err != nil {
		t.Fatal(err)
	}
	write := generated.ConsumeFinalTestGenerationParams{ResearchGenerationID: "generation-research_registry-1", ConsumedByRunID: "run-research_registry", ConsumptionHash: hash, ConsumedAt: pgTimestamp(now)}
	if err := repository.ConsumeFinalTest(ctx, write); err != nil {
		t.Fatal(err)
	}
	if err := repository.ConsumeFinalTest(ctx, write); err == nil {
		t.Fatal("final-test generation consumed twice")
	}
}

func assertResearchRegistryDecision(t *testing.T, ctx context.Context, pool *pgxpool.Pool, repository *ResearchRegistryRepository) {
	t.Helper()
	now := time.Date(2026, 7, 16, 8, 3, 0, 0, time.UTC)
	canonical := []byte(`{"reason":"entry_accepted"}`)
	hash := researchRegistryPayloadHash(canonical)
	if _, err := pool.Exec(ctx, "INSERT INTO decisions VALUES ('decision-research_registry',NULL,'run-research_registry','configuration-research_registry','trend-following-1-0-0','approved','entry_accepted','cause-research_registry',$1,1)", now); err != nil {
		t.Fatal(err)
	}
	write := generated.InsertTrendDecisionParams{DecisionID: "decision-research_registry", ExplanationHash: hash, CanonicalExplanation: canonical,
		CandleViewID: "candles-research_registry", CandleViewRevision: 1, MarketViewID: "market-research_registry", MarketViewRevision: 1,
		InstrumentMetadataID: "metadata-research_registry", AssetEligibilityVersion: 1, PortfolioRevision: 1, PositionRevision: 1,
		FeeModelID: "fixed-bps-v1", LatencyModelID: "fixed-zero-v1", FillModelID: "fill-v1", SlippageModelID: "slippage-v1",
		GapModelID: "gap-v1", CorrelationID: "correlation-research_registry", CausationID: "cause-research_registry", RecordedAt: pgTimestamp(now)}
	if err := repository.RecordDecision(ctx, write); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "UPDATE trend_decisions SET portfolio_revision=2 WHERE decision_id='decision-research_registry'"); err == nil {
		t.Fatal("immutable Trend decision mutated")
	}
}

func assertResearchRegistryReport(t *testing.T, ctx context.Context, pool *pgxpool.Pool, repository *ResearchRegistryRepository) {
	t.Helper()
	now := time.Date(2026, 7, 16, 8, 4, 0, 0, time.UTC)
	manifest := []byte(`{"tier":"local_tier_b"}`)
	hash := researchRegistryPayloadHash(manifest)
	write := generated.InsertResearchReportParams{ID: "report-research_registry", ResearchGenerationID: "generation-research_registry-1", ManifestHash: hash,
		ArtifactHash: strings.Repeat("f", 64), CanonicalManifest: manifest, RunReferences: []byte(`["run-research_registry"]`),
		ConfidenceLabel: "local_tier_b", PlatformCorrectness: "locally reproducible", StrategyEvidence: "formal evidence pending",
		ViabilityDisposition: "undetermined", DisclaimerPolicy: "no_production_profitability_claim", CreatedAt: pgTimestamp(now)}
	if err := repository.RecordReport(ctx, write); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "DELETE FROM research_reports WHERE id='report-research_registry'"); err == nil {
		t.Fatal("immutable research report deleted")
	}
}

func researchRegistryPayloadHash(payload []byte) string {
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}
