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

func TestA10PostgresTrendResearchQualification(t *testing.T) {
	dsn := os.Getenv("AXIOM_A10_TEST_DSN")
	if dsn == "" {
		t.Skip("AXIOM_A10_TEST_DSN is not set")
	}
	configuration, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(configuration.ConnConfig.Database, "_a10_test") {
		t.Fatal("A10 integration requires a dedicated database ending _a10_test")
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
		t.Fatalf("A10 migrations = %d %v", applied, applyErr)
	}
	seedA10References(t, ctx, pool)
	repository, err := NewA10Repository(pool)
	if err != nil {
		t.Fatal(err)
	}
	write := a10RegistrationFixture()
	if err = repository.Register(ctx, write); err != nil {
		t.Fatal(err)
	}
	if err = repository.Register(ctx, write); err == nil {
		t.Fatal("duplicate immutable registration committed")
	}
	assertA10Registration(t, ctx, pool)
	assertA10FinalConsumption(t, ctx, pool, repository)
	assertA10Decision(t, ctx, pool, repository)
	assertA10Report(t, ctx, pool, repository)
}

func seedA10References(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	now := time.Date(2026, 7, 16, 8, 0, 0, 0, time.UTC)
	hash := strings.Repeat("a", 64)
	canonicalConfiguration, err := json.Marshal(config.DefaultConfiguration())
	if err != nil {
		t.Fatal(err)
	}
	configurationHash := a10PayloadHash(canonicalConfiguration)
	datasetHash, recorderDatasetID, manifestPath, sourceCommit, datasetKind := hash, "", "", "", "public_market"
	manifestRevision := any(nil)
	if selected := os.Getenv("AXIOM_A11_E2E_DATASET_MANIFEST"); selected != "" {
		manifest, manifestErr := recorder.ReadManifest(selected)
		sourceCommit = os.Getenv("AXIOM_A11_E2E_SOURCE_COMMIT")
		if manifestErr != nil || !manifest.Complete || len(manifest.Segments) < 2 || !a11BuildIdentityValid(sourceCommit) {
			t.Fatalf("invalid A11 E2E dataset manifest: %#v %v", manifest, manifestErr)
		}
		datasetHash, recorderDatasetID, manifestPath, datasetKind = manifest.Hash, manifest.DatasetID, filepath.Base(selected), "decision_inputs"
		manifestRevision = int64(manifest.Revision)
	}
	statements := []struct {
		sql  string
		args []any
	}{
		{"INSERT INTO configuration_versions VALUES ('configuration-a10',1,$1,$2,'test',$3)", []any{configurationHash, canonicalConfiguration, now}},
		{"INSERT INTO configuration_activations(configuration_id,actor,reason,activated_at) VALUES('configuration-a10','test','A10/A11 qualification baseline',$1)", []any{now}},
		{`INSERT INTO dataset_manifests
		  (id,dataset_hash,schema_compatibility,coverage_start,coverage_end,state,created_at,
		   recorder_dataset_id,manifest_revision,manifest_path,source_commit,dataset_kind)
		  VALUES ('dataset-a7-formal-pending',$1,'a7-normalized-v1',$2,$2,'building',$2,
		          nullif($3,''),$4,nullif($5,''),nullif($6,''),$7)`,
			[]any{datasetHash, now, recorderDatasetID, manifestRevision, manifestPath, sourceCommit, datasetKind}},
		{"UPDATE dataset_manifests SET state='ready' WHERE id='dataset-a7-formal-pending'", nil},
		{"UPDATE dataset_manifests SET state='qualified' WHERE id='dataset-a7-formal-pending'", nil},
		{"INSERT INTO assets(symbol) VALUES ('USDT'),('BTC'),('ETH')", nil},
		{"INSERT INTO exchanges VALUES ('exchange-a10','binance','production_public')", nil},
		{"INSERT INTO instruments VALUES ('instrument-a10','BTC','USDT','spot'),('instrument-eth-a10','ETH','USDT','spot')", nil},
		{"INSERT INTO instrument_metadata_versions VALUES ('metadata-a10','exchange-a10','instrument-a10',1,0.01,0.00001,0.00001,10,$1,$1),('metadata-eth-a10','exchange-a10','instrument-eth-a10',1,0.01,0.00001,0.00001,10,$1,$1)", []any{now}},
		{"INSERT INTO model_versions VALUES ('fixed-bps-v1','fee',1,$1,'{}',NULL,$2),('fixed-zero-v1','latency',1,$1,'{}',NULL,$2),('fill-v1','fill',1,$1,'{}',NULL,$2),('slippage-v1','slippage',1,$1,'{}',NULL,$2),('gap-v1','gap',1,$1,'{}',NULL,$2)", []any{hash, now}},
	}
	for index, statement := range statements {
		if _, err := pool.Exec(ctx, statement.sql, statement.args...); err != nil {
			t.Fatalf("A10 seed %d failed: %v", index+1, err)
		}
	}
}

func a10RegistrationFixture() A10RegistrationWrite {
	now := pgTimestamp(time.Date(2026, 7, 16, 8, 1, 0, 0, time.UTC))
	hash := strings.Repeat("b", 64)
	manifest := []byte(`{"version":"trend.v1a.1"}`)
	manifestHash := a10PayloadHash(manifest)
	author, notes, metric := "owner", "Tier B local engineering evidence", "risk_adjusted_net_return"
	generation, minimumSamples := int32(1), int64(100)
	stop, reject, promote := "registered sample boundary", "confidence interval crosses rejection floor", "formal gates only"
	commit := strings.Repeat("c", 40)
	write := A10RegistrationWrite{
		Definition: generated.InsertA10StrategyDefinitionParams{ID: "trend", Name: "Trend V1A", Family: "trend"},
		Version: generated.InsertA10StrategyVersionParams{ID: "trend-v1a-1", StrategyID: "trend", Version: 1,
			ImplementationHash: hash, PromotionStatus: "research", CreatedAt: now, ManifestHash: manifestHash,
			CanonicalManifest: manifest, CodeCommit: &commit,
			SupportedModes: []string{"backtest", "replay", "paper", "shadow"}, Author: &author, Notes: &notes},
		Experiment: generated.InsertA10ExperimentRegistrationParams{ID: "experiment-a10", StrategyVersionID: "trend-v1a-1",
			ConfigurationID: "configuration-a10", DatasetID: "dataset-a7-formal-pending", Hypothesis: "registered before final test",
			Status: "registered", RegisteredAt: now, Generation: &generation, PrimaryMetric: &metric,
			TrainStart: pgTimestamp(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)), TrainEnd: pgTimestamp(time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)),
			ValidationStart: pgTimestamp(time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)), ValidationEnd: pgTimestamp(time.Date(2025, 9, 1, 0, 0, 0, 0, time.UTC)),
			FinalTestStart: pgTimestamp(time.Date(2025, 9, 1, 0, 0, 0, 0, time.UTC)), FinalTestEnd: pgTimestamp(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)),
			SearchSpace: []byte(`{"locked":"baseline"}`), ParameterNeighborhood: []byte(`{"atr":["2.25","2.5","2.75"]}`),
			ModelAssumptions:     []byte(`{"fee":"fixed-bps-v1","spread":"recorded","slippage":"slippage-v1","latency":"fixed-zero-v1","fill":"fill-v1","gap":"gap-v1"}`),
			BenchmarkAssumptions: []byte(`{"cash":true,"buy_hold":true,"static_inventory":true}`), MinimumSamples: &minimumSamples,
			StoppingRule: &stop, RejectionRule: &reject, PromotionRule: &promote, RegisteredSeedHash: hash},
		Generation: generated.InsertResearchGenerationParams{ID: "generation-a10-1", ExperimentID: "experiment-a10", Generation: 1,
			FinalWindowHash: hash, RegistrationHash: hash, RegisteredAt: now},
	}
	for index := 0; index < 16; index++ {
		name := "parameter-" + string(rune('a'+index))
		description, algorithm := "locked baseline parameter", "trend.v1a.1"
		minimum, maximum, rounding := "0", "1000", "half_even"
		cadence, warmup, mutability := "4h", "200_completed_candles", "immutable_per_run"
		scale := int32(18)
		inclusive := true
		write.Parameters = append(write.Parameters, generated.InsertA10StrategyParameterParams{StrategyVersionID: "trend-v1a-1",
			ParameterName: name, DecimalValue: "1", Unit: "count", Description: &description, AlgorithmVersion: &algorithm,
			MinimumValue: &minimum, MaximumValue: &maximum, MinimumInclusive: &inclusive, MaximumInclusive: &inclusive,
			DecimalScale: &scale, Rounding: &rounding, Cadence: &cadence, WarmUp: &warmup, Mutability: &mutability,
			ModelDependencies: []byte(`[]`)})
	}
	return write
}

func assertA10Registration(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	for table, expected := range map[string]int{"strategy_versions": 1, "strategy_parameters": 16,
		"experiment_registrations": 1, "research_generations": 1} {
		var count int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != expected {
			t.Fatalf("A10 registration table %s = %d %v", table, count, err)
		}
	}
	if _, err := pool.Exec(ctx, "UPDATE research_generations SET generation=2 WHERE id='generation-a10-1'"); err == nil {
		t.Fatal("immutable research generation mutated")
	}
}

func assertA10FinalConsumption(t *testing.T, ctx context.Context, pool *pgxpool.Pool, repository *A10Repository) {
	t.Helper()
	now := time.Date(2026, 7, 16, 8, 2, 0, 0, time.UTC)
	hash := strings.Repeat("d", 64)
	if _, err := pool.Exec(ctx, "INSERT INTO runs VALUES ('run-a10','backtest','configuration-a10','trend-v1a-1','dataset-a7-formal-pending',$1,$1,'created',$2,NULL,NULL)", hash, now); err != nil {
		t.Fatal(err)
	}
	write := generated.ConsumeFinalTestGenerationParams{ResearchGenerationID: "generation-a10-1", ConsumedByRunID: "run-a10", ConsumptionHash: hash, ConsumedAt: pgTimestamp(now)}
	if err := repository.ConsumeFinalTest(ctx, write); err != nil {
		t.Fatal(err)
	}
	if err := repository.ConsumeFinalTest(ctx, write); err == nil {
		t.Fatal("final-test generation consumed twice")
	}
}

func assertA10Decision(t *testing.T, ctx context.Context, pool *pgxpool.Pool, repository *A10Repository) {
	t.Helper()
	now := time.Date(2026, 7, 16, 8, 3, 0, 0, time.UTC)
	canonical := []byte(`{"reason":"entry_accepted"}`)
	hash := a10PayloadHash(canonical)
	if _, err := pool.Exec(ctx, "INSERT INTO decisions VALUES ('decision-a10',NULL,'run-a10','configuration-a10','trend-v1a-1','approved','entry_accepted','cause-a10',$1,1)", now); err != nil {
		t.Fatal(err)
	}
	write := generated.InsertTrendDecisionParams{DecisionID: "decision-a10", ExplanationHash: hash, CanonicalExplanation: canonical,
		CandleViewID: "candles-a10", CandleViewRevision: 1, MarketViewID: "market-a10", MarketViewRevision: 1,
		InstrumentMetadataID: "metadata-a10", AssetEligibilityVersion: 1, PortfolioRevision: 1, PositionRevision: 1,
		FeeModelID: "fixed-bps-v1", LatencyModelID: "fixed-zero-v1", FillModelID: "fill-v1", SlippageModelID: "slippage-v1",
		GapModelID: "gap-v1", CorrelationID: "correlation-a10", CausationID: "cause-a10", RecordedAt: pgTimestamp(now)}
	if err := repository.RecordDecision(ctx, write); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "UPDATE trend_decisions SET portfolio_revision=2 WHERE decision_id='decision-a10'"); err == nil {
		t.Fatal("immutable Trend decision mutated")
	}
}

func assertA10Report(t *testing.T, ctx context.Context, pool *pgxpool.Pool, repository *A10Repository) {
	t.Helper()
	now := time.Date(2026, 7, 16, 8, 4, 0, 0, time.UTC)
	manifest := []byte(`{"tier":"local_tier_b"}`)
	hash := a10PayloadHash(manifest)
	write := generated.InsertResearchReportParams{ID: "report-a10", ResearchGenerationID: "generation-a10-1", ManifestHash: hash,
		ArtifactHash: strings.Repeat("f", 64), CanonicalManifest: manifest, RunReferences: []byte(`["run-a10"]`),
		ConfidenceLabel: "local_tier_b", PlatformCorrectness: "locally reproducible", StrategyEvidence: "formal evidence pending",
		ViabilityDisposition: "undetermined", DisclaimerPolicy: "no_production_profitability_claim", CreatedAt: pgTimestamp(now)}
	if err := repository.RecordReport(ctx, write); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "DELETE FROM research_reports WHERE id='report-a10'"); err == nil {
		t.Fatal("immutable research report deleted")
	}
}

func a10PayloadHash(payload []byte) string {
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}
