package postgres

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"axiom/internal/authentication"
	"axiom/internal/domain"
	"axiom/internal/research"
	"axiom/internal/storage/postgres/generated"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestB7PostgresCleanInstallQualification(t *testing.T) {
	ctx, pool := openB7TestDatabase(t, "AXIOM_B7_TEST_DSN")
	defer pool.Close()
	assertPostgres18(t, ctx, pool)
	assertEmptyTestDatabase(t, ctx, pool)
	applyB4MigrationPrefix(t, ctx, pool, 19)
	assertB7SchemaAndPromotion(t, ctx, pool)
}

func TestB7PostgresB6ToB7UpgradeQualification(t *testing.T) {
	ctx, pool := openB7TestDatabase(t, "AXIOM_B7_UPGRADE_TEST_DSN")
	defer pool.Close()
	assertPostgres18(t, ctx, pool)
	assertEmptyTestDatabase(t, ctx, pool)
	applyB4MigrationPrefix(t, ctx, pool, 18)
	assertB7UpgradeSentinel(t, ctx, pool)
	connection, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Release()
	migrations, err := Migrations()
	if err != nil || len(migrations) < 19 {
		t.Fatalf("migration catalog=%d error=%v", len(migrations), err)
	}
	changed, err := applyMigration(ctx, connection, migrations[18])
	if err != nil || !changed {
		t.Fatalf("B6-to-B7 migration changed=%t error=%v", changed, err)
	}
	var sentinel int
	if err = pool.QueryRow(ctx,
		`SELECT count(*) FROM rebalancing_fact_sets WHERE id='b7-upgrade-sentinel'`,
	).Scan(&sentinel); err != nil || sentinel != 1 {
		t.Fatalf("B6 upgrade sentinel=%d error=%v", sentinel, err)
	}
	assertB7SchemaAndPromotion(t, ctx, pool)
}

func openB7TestDatabase(t *testing.T, environment string) (context.Context, *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv(environment)
	if dsn == "" {
		t.Skip(environment + " is not set")
	}
	configuration, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(configuration.ConnConfig.Database, "_b7_test") {
		t.Fatal("B7 integration requires a dedicated database ending _b7_test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
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

func assertB7UpgradeSentinel(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	now := time.Date(2026, 7, 24, 8, 0, 0, 0, time.UTC)
	hash := strings.Repeat("a", 64)
	if _, err := pool.Exec(ctx, `INSERT INTO configuration_versions(
id,version,configuration_hash,canonical_payload,actor,recorded_at
) VALUES ('configuration-b7-upgrade',700,$1,'{}','b7-upgrade',$2)`, hash, now); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO rebalancing_fact_sets(
id,configuration_id,configuration_hash,fact_schema_version,cost_model_version,
canonical_hash,recorded_at
) VALUES ('b7-upgrade-sentinel','configuration-b7-upgrade',$1,
'rebalancing-fact.v1','rebalancing-cost.v1',$1,$2)`, hash, now); err != nil {
		t.Fatal(err)
	}
}

func assertB7SchemaAndPromotion(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Millisecond)
	clock, _ := domain.NewReplayClock(now)
	_, _, login := a11QualificationAuthentication(t, ctx, pool, clock)
	seedA10References(t, ctx, pool)
	repository, err := NewB7Repository(pool)
	if err != nil {
		t.Fatal(err)
	}
	champion := seedB7ResearchFixture(t, ctx, pool, repository, now, "champion", 1)
	challenger := seedB7ResearchFixture(t, ctx, pool, repository, now, "challenger", 2)
	assertB7Comparison(t, ctx, pool, repository, champion, challenger, now)
	assertB7LowConfidenceDenied(t, ctx, repository, login.Principal, clock, champion)
	assertB7SequentialPromotion(t, ctx, repository, login.Principal, clock, champion)
	assertB7ConcurrentPromotion(t, ctx, pool, repository, login.Principal, clock, challenger)
	assertB7DatabaseAuthAndImmutability(
		t, ctx, pool, repository, login.Principal, challenger, now,
	)
	assertB7RoleMatrix(t, ctx, pool)
}

type b7Fixture struct {
	registration research.ExperimentRegistrationManifest
	suite        research.ValidationSuiteManifest
	suiteBytes   []byte
	evidenceHash string
}

func seedB7ResearchFixture(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	repository *B7Repository,
	now time.Time,
	suffix string,
	version int64,
) b7Fixture {
	t.Helper()
	registration, registrationBytes, err := research.RegisterExperiment(
		b7RegistrationInput(now, suffix, uint32(version)),
	)
	if err != nil {
		t.Fatal(err)
	}
	seedB7RegisteredGeneration(t, ctx, pool, registration, registrationBytes, version, now)
	if err = repository.RecordPreregistration(ctx, registrationBytes, registration.RegistrationHash); err != nil {
		t.Fatal(err)
	}
	suite, suiteBytes, err := research.BuildValidationSuite(
		registration, b7ValidationInput(registration, now, suffix),
	)
	if err != nil {
		t.Fatal(err)
	}
	evidenceHash := a10PayloadHash([]byte("evidence-" + suffix))
	if err = repository.RecordValidationSuite(
		ctx, registration.ID, suiteBytes, suite.ManifestHash, evidenceHash,
	); err != nil {
		t.Fatal(err)
	}
	return b7Fixture{registration: registration, suite: suite,
		suiteBytes: suiteBytes, evidenceHash: evidenceHash}
}

func seedB7RegisteredGeneration(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	registration research.ExperimentRegistrationManifest,
	canonical []byte,
	version int64,
	now time.Time,
) {
	t.Helper()
	hash := a10PayloadHash(canonical)
	if _, err := pool.Exec(ctx, `INSERT INTO strategy_definitions(id,name,family)
VALUES ('trend-b7','Trend B7','trend') ON CONFLICT (id) DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO strategy_versions(
id,strategy_id,version,implementation_hash,promotion_status,created_at,
manifest_hash,canonical_manifest
) VALUES ($1,'trend-b7',$2,$3,'research',$4,$3,$5)`,
		registration.StrategyVersionID, version, hash, registration.RegisteredAt, canonical); err != nil {
		t.Fatal(err)
	}
	insertB7ExperimentAndRuns(t, ctx, pool, registration, now)
}

func insertB7ExperimentAndRuns(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	registration research.ExperimentRegistrationManifest,
	now time.Time,
) {
	t.Helper()
	generation := int32(registration.Generation)
	minimumSamples := int64(registration.MinimumSamples)
	search, models, benchmarks := []byte(`{"locked":true}`), []byte(`{"registered":true}`), []byte(`{"registered":true}`)
	queries := generated.New(pool)
	_, err := queries.InsertA10ExperimentRegistration(ctx,
		generated.InsertA10ExperimentRegistrationParams{
			ID: registration.ID, StrategyVersionID: registration.StrategyVersionID,
			ConfigurationID: "configuration-a10", DatasetID: "dataset-a7-formal-pending",
			Hypothesis: registration.Hypothesis, Status: "registered",
			RegisteredAt: pgTimestamp(registration.RegisteredAt), Generation: &generation,
			PrimaryMetric:   &registration.PrimaryMetric,
			TrainStart:      pgTimestamp(registration.Split.Train.Start),
			TrainEnd:        pgTimestamp(registration.Split.Train.End),
			ValidationStart: pgTimestamp(registration.Split.Validation.Start),
			ValidationEnd:   pgTimestamp(registration.Split.Validation.End),
			FinalTestStart:  pgTimestamp(registration.Split.FinalTest.Start),
			FinalTestEnd:    pgTimestamp(registration.Split.FinalTest.End),
			SearchSpace:     search, ParameterNeighborhood: search,
			ModelAssumptions: models, BenchmarkAssumptions: benchmarks,
			MinimumSamples: &minimumSamples, StoppingRule: &registration.StoppingRule,
			RejectionRule: &registration.RejectionRule, PromotionRule: &registration.PromotionRule,
			RegisteredSeedHash: registration.RegisteredSeedHash,
		})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = queries.InsertResearchGeneration(ctx, generated.InsertResearchGenerationParams{
		ID: registration.ResearchGenerationID, ExperimentID: registration.ID,
		Generation:       int32(registration.Generation),
		FinalWindowHash:  a10PayloadHash([]byte(registration.ID + "-final")),
		RegistrationHash: registration.RegistrationHash,
		RegisteredAt:     pgTimestamp(registration.RegisteredAt),
	}); err != nil {
		t.Fatal(err)
	}
	insertB7RunsAndConsumption(t, ctx, pool, registration, now)
}

func insertB7RunsAndConsumption(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	registration research.ExperimentRegistrationManifest,
	now time.Time,
) {
	t.Helper()
	hash := strings.Repeat("d", 64)
	for _, mode := range []string{"backtest", "replay", "shadow"} {
		runID := mode + "-" + registration.ID
		if _, err := pool.Exec(ctx, `INSERT INTO runs(
id,mode,configuration_id,strategy_version_id,dataset_id,root_seed_hash,
reproducibility_hash,state,created_at
) VALUES ($1,$2,'configuration-a10',$3,'dataset-a7-formal-pending',$4,$4,
'created',$5)`, runID, mode, registration.StrategyVersionID, hash,
			now.Add(-time.Hour)); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE runs
SET state='running',started_at=$2 WHERE id=$1`, runID, now.Add(-time.Hour)); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE runs
SET state='completed',completed_at=$2 WHERE id=$1`, runID, now.Add(-time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	consumptionHash := a10PayloadHash([]byte("consumption-" + registration.ID))
	if _, err := generated.New(pool).ConsumeFinalTestGeneration(ctx,
		generated.ConsumeFinalTestGenerationParams{
			ResearchGenerationID: registration.ResearchGenerationID,
			ConsumedByRunID:      "backtest-" + registration.ID,
			ConsumptionHash:      consumptionHash, ConsumedAt: pgTimestamp(now.Add(-time.Hour)),
		}); err != nil {
		t.Fatal(err)
	}
}

func b7RegistrationInput(
	now time.Time,
	suffix string,
	generation uint32,
) research.ExperimentRegistrationInput {
	start := now.Add(-300 * time.Hour)
	models := make([]research.ModelAssumption, 0, 6)
	for index, name := range []string{"fee", "spread", "slippage", "latency", "gap", "missed_fill"} {
		models = append(models, research.ModelAssumption{Name: name,
			VersionHash: strings.Repeat(string(rune('a'+index)), 64)})
	}
	return research.ExperimentRegistrationInput{
		ID:                   "experiment-b7-" + suffix,
		ResearchGenerationID: "generation-b7-" + suffix,
		StrategyVersionID:    fmt.Sprintf("trend-b7-v%d", generation),
		Generation:           generation, Hypothesis: "Registered positive net expectancy.",
		PrimaryMetric: "risk_adjusted_net_return",
		ParameterSearch: []research.SearchParameter{
			{ID: "breakout", Values: []string{"19", "20", "21"}},
		},
		Split: research.ChronologicalSplit{
			Train: research.Window{Name: "train", Start: start, End: start.Add(100 * time.Hour)},
			Validation: research.Window{Name: "validation", Start: start.Add(100 * time.Hour),
				End: start.Add(150 * time.Hour)},
			FinalTest: research.Window{Name: "final_test", Start: start.Add(150 * time.Hour),
				End: start.Add(200 * time.Hour)},
		},
		Models: models, Benchmarks: []string{"cash", "buy_and_hold", "static_inventory"},
		MinimumSamples: 400, MinimumTrades: 100, MinimumShadowDuration: time.Hour,
		MinimumDeflatedSharpeProbability: "0.95",
		StoppingRule:                     "locked final window", RejectionRule: "any failed criterion",
		PromotionRule:      "tier A sequential evidence",
		RegisteredSeedHash: strings.Repeat("f", 64), RegisteredAt: start.Add(149 * time.Hour),
	}
}

func b7ValidationInput(
	registration research.ExperimentRegistrationManifest,
	now time.Time,
	suffix string,
) research.ValidationSuiteInput {
	result := func(name string) research.ResultSlice {
		return research.ResultSlice{Name: name, NetReturn: "0.02", MaxDrawdown: "0.03", Trades: 120}
	}
	stress := make([]research.ResultSlice, 0, 6)
	for _, name := range []string{"fee", "spread", "slippage", "latency", "gap", "missed_fill"} {
		stress = append(stress, result(name))
	}
	confidence, _ := research.BlockBootstrapMean(
		[]string{"0.01", "0.02", "0.03", "0.04"}, 2, 100, "b7-"+suffix,
	)
	sources := make([]research.EvidenceSource, 0, 3)
	for index, mode := range []string{"backtest", "replay", "shadow"} {
		sources = append(sources, research.EvidenceSource{
			RunID: mode + "-" + registration.ID, Mode: mode, DatasetTier: "tier_a",
			ConfidenceLabel: "formal_tier_a",
			ResultHash:      strings.Repeat(string(rune('1'+index)), 64), Primary: true,
		})
	}
	return research.ValidationSuiteInput{
		ID: "suite-b7-" + suffix, ResearchGenerationID: registration.ResearchGenerationID,
		StrategyVersionID: registration.StrategyVersionID, StrategyFamily: "trend",
		PreregistrationHash:      registration.RegistrationHash,
		FinalTestConsumptionHash: a10PayloadHash([]byte("consumption-" + registration.ID)),
		Sources:                  sources, WalkForward: []research.WalkForwardFold{{TrainStart: 0,
			TrainEnd: 200, ValidationStart: 200, ValidationEnd: 300,
			TestStart: 300, TestEnd: 400}}, Confidence: confidence,
		Neighborhood: []research.ResultSlice{result("base"), result("low"), result("high")},
		Capacity: []research.CapacityPoint{{Notional: "10", NetReturn: "0.02", FillRate: "1"},
			{Notional: "100", NetReturn: "0.01", FillRate: "0.9"}},
		Stress: stress, Benchmarks: []research.ResultSlice{result("cash"),
			result("buy_and_hold"), result("static_inventory")},
		Regimes: []research.ResultSlice{result("up"), result("down")},
		MultipleTesting: research.MultipleTestingInput{Method: "benjamini_hochberg_fdr.v1",
			Alpha: "0.05", RawPValues: []string{"0.001", "0.02", "0.4"}},
		Sharpe: research.SharpeInput{ObservedSharpe: "2", BenchmarkSharpe: "0",
			Skewness: "0", ExcessKurtosis: "0", Observations: 400, IndependentTrials: 10},
		ObservedSamples: 400, ObservedTrades: 120, ObservedShadowDuration: time.Hour,
		Criteria:        []research.PromotionCriterion{{ID: "registered_gate", Passed: true}},
		ConfidenceLabel: "formal_tier_a", PlatformCorrectness: "Deterministic checks passed.",
		StrategyEvidence:     "Statistical evidence remains uncertain.",
		ViabilityDisposition: "viable_for_more_research", CreatedAt: now,
	}
}

func assertB7Comparison(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	repository *B7Repository,
	champion b7Fixture,
	challenger b7Fixture,
	now time.Time,
) {
	t.Helper()
	result := func(name string) research.ResultSlice {
		return research.ResultSlice{Name: name, NetReturn: "0.02", MaxDrawdown: "0.03", Trades: 120}
	}
	report, canonical, err := research.BuildChampionChallengerReport(
		research.ChampionChallengerInput{ID: "comparison-b7",
			StrategyFamily:         "trend",
			ChampionVersionID:      champion.registration.StrategyVersionID,
			ChallengerVersionID:    challenger.registration.StrategyVersionID,
			ChampionEvidenceHash:   champion.suite.ManifestHash,
			ChallengerEvidenceHash: challenger.suite.ManifestHash,
			Overall:                []research.ResultSlice{result("champion"), result("challenger")},
			Regimes:                []research.ResultSlice{result("up"), result("down")},
			Disposition:            "recommend_challenger", Reason: "registered comparison",
			CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if err = repository.RecordChampionChallenger(ctx, champion.suite.ID,
		challenger.suite.ID, canonical, report.ManifestHash); err != nil {
		t.Fatal(err)
	}
	if err = repository.RecordChampionChallenger(ctx, challenger.suite.ID,
		champion.suite.ID, canonical, report.ManifestHash); err == nil {
		t.Fatal("champion/challenger suite identity mismatch accepted")
	}
	if _, err = pool.Exec(ctx,
		`DELETE FROM b7_champion_challenger_reports WHERE id='comparison-b7'`,
	); err == nil {
		t.Fatal("immutable champion/challenger report deleted")
	}
}

func assertB7LowConfidenceDenied(
	t *testing.T,
	ctx context.Context,
	repository *B7Repository,
	principal authentication.Principal,
	clock *domain.ReplayClock,
	fixture b7Fixture,
) {
	t.Helper()
	input := b7ValidationInput(fixture.registration, clock.Now().UTC, "low")
	input.ID = "suite-b7-low"
	input.ResearchGenerationID = fixture.registration.ResearchGenerationID
	input.StrategyVersionID = fixture.registration.StrategyVersionID
	input.PreregistrationHash = fixture.registration.RegistrationHash
	input.FinalTestConsumptionHash = fixture.suite.FinalTestConsumptionHash
	input.Sources[0].DatasetTier = "low_confidence"
	suite, canonical, err := research.BuildValidationSuite(fixture.registration, input)
	if err != nil || len(suite.EligibleMaturities) != 0 {
		t.Fatalf("low-confidence suite=%#v error=%v", suite.EligibleMaturities, err)
	}
	if err = repository.RecordValidationSuite(ctx, fixture.registration.ID, canonical,
		suite.ManifestHash, a10PayloadHash([]byte("low-evidence"))); err != nil {
		t.Fatal(err)
	}
	service, _ := research.NewPromotionService(repository, clock)
	_, err = service.Promote(ctx, principal, research.PromotionRequest{
		StrategyVersionID: fixture.registration.StrategyVersionID, EvidenceID: suite.ID,
		EvidenceHash: suite.ManifestHash, Target: research.MaturityBacktestValidated,
		ExpectedRevision: 1, IdempotencyKey: "b7-low-confidence",
		Reason: "negative qualification"})
	var failure research.Error
	if !errors.As(err, &failure) || failure.Code != "promotion_evidence_ineligible" {
		t.Fatalf("low-confidence promotion error=%v", err)
	}
}

func assertB7SequentialPromotion(
	t *testing.T,
	ctx context.Context,
	repository *B7Repository,
	principal authentication.Principal,
	clock *domain.ReplayClock,
	fixture b7Fixture,
) {
	t.Helper()
	service, _ := research.NewPromotionService(repository, clock)
	targets := []research.Maturity{research.MaturityBacktestValidated,
		research.MaturityReplayValidated, research.MaturityShadowValidated}
	var firstRequest research.PromotionRequest
	for index, target := range targets {
		request := research.PromotionRequest{
			StrategyVersionID: fixture.registration.StrategyVersionID,
			EvidenceID:        fixture.suite.ID, EvidenceHash: fixture.suite.ManifestHash,
			Target: target, ExpectedRevision: uint64(index + 1),
			IdempotencyKey: fmt.Sprintf("b7-sequential-%d", index+1),
			Reason:         "registered maturity gate passed"}
		if index == 0 {
			firstRequest = request
		}
		result, err := service.Promote(ctx, principal, request)
		if err != nil || result.Maturity != target || result.Revision != uint64(index+2) {
			t.Fatalf("sequential promotion %d = %#v %v", index, result, err)
		}
	}
	repeated, err := service.Promote(ctx, principal, firstRequest)
	if err != nil || repeated.Maturity != research.MaturityBacktestValidated ||
		repeated.Revision != 2 {
		t.Fatalf("idempotent promotion=%#v error=%v", repeated, err)
	}
}

func assertB7ConcurrentPromotion(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	repository *B7Repository,
	principal authentication.Principal,
	clock *domain.ReplayClock,
	fixture b7Fixture,
) {
	t.Helper()
	service, _ := research.NewPromotionService(repository, clock)
	results := make([]error, 2)
	var wait sync.WaitGroup
	for index := range results {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			_, results[index] = service.Promote(ctx, principal, research.PromotionRequest{
				StrategyVersionID: fixture.registration.StrategyVersionID,
				EvidenceID:        fixture.suite.ID, EvidenceHash: fixture.suite.ManifestHash,
				Target: research.MaturityBacktestValidated, ExpectedRevision: 1,
				IdempotencyKey: fmt.Sprintf("b7-concurrent-%d", index),
				Reason:         "concurrent registered command"})
		}(index)
	}
	wait.Wait()
	successes, conflicts := 0, 0
	for _, err := range results {
		if err == nil {
			successes++
			continue
		}
		var failure research.Error
		if errors.As(err, &failure) && failure.Code == "promotion_revision_conflict" {
			conflicts++
		}
	}
	var maturity string
	var revision int64
	if err := pool.QueryRow(ctx, `SELECT maturity,revision FROM strategy_maturity_states
WHERE strategy_version_id=$1`, fixture.registration.StrategyVersionID).Scan(&maturity, &revision); err != nil ||
		successes != 1 || conflicts != 1 || maturity != "BACKTEST_VALIDATED" || revision != 2 {
		t.Fatalf("concurrent promotion successes=%d conflicts=%d state=%s/%d error=%v",
			successes, conflicts, maturity, revision, err)
	}
}

func assertB7DatabaseAuthAndImmutability(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	repository *B7Repository,
	principal authentication.Principal,
	fixture b7Fixture,
	now time.Time,
) {
	t.Helper()
	result, err := repository.ApplyPromotion(ctx, research.PromotionCommand{
		PromotionRequest: research.PromotionRequest{
			StrategyVersionID: fixture.registration.StrategyVersionID,
			EvidenceID:        fixture.suite.ID, EvidenceHash: fixture.suite.ManifestHash,
			Target: research.MaturityReplayValidated, ExpectedRevision: 2,
			IdempotencyKey: "b7-invalid-session", Reason: "database auth check"},
		CommandID:   "promotion-b7-invalid-session",
		PayloadHash: strings.Repeat("8", 64), ActorUserID: principal.UserID,
		SessionID: "missing-session", CommandTime: now})
	if err != nil || result.FailureCode != "promotion_unauthorized" {
		t.Fatalf("database authentication result=%#v error=%v", result, err)
	}
	result, err = repository.ApplyPromotion(ctx, research.PromotionCommand{
		PromotionRequest: research.PromotionRequest{
			StrategyVersionID: fixture.registration.StrategyVersionID,
			EvidenceID:        fixture.suite.ID, EvidenceHash: fixture.suite.ManifestHash,
			Target: research.MaturityReplayValidated, ExpectedRevision: 2,
			IdempotencyKey: "b7-stale-command-time", Reason: "database time check"},
		CommandID:   "promotion-b7-stale-command-time",
		PayloadHash: strings.Repeat("7", 64), ActorUserID: principal.UserID,
		SessionID: principal.SessionID, CommandTime: now.Add(-2 * time.Minute)})
	if err != nil || result.FailureCode != "promotion_unauthorized" {
		t.Fatalf("stale database command time result=%#v error=%v", result, err)
	}
	for _, statement := range []string{
		`UPDATE b7_validation_suites SET confidence_label='rejected' WHERE id='suite-b7-champion'`,
		`DELETE FROM strategy_maturity_commands WHERE id='promotion-b7-invalid-session'`,
	} {
		if _, err = pool.Exec(ctx, statement); err == nil {
			t.Fatalf("immutable B7 evidence mutated: %s", statement)
		}
	}
}

func assertB7RoleMatrix(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	runtimeRole := testRole("AXIOM_B7_RUNTIME_ROLE", "axiom_app")
	recorderRole := testRole("AXIOM_B7_RECORDER_ROLE", "axiom_recorder")
	readOnlyRole := testRole("AXIOM_B7_READONLY_ROLE", "axiom_readonly")
	if err := ApplyRoleGrants(ctx, pool, runtimeRole, recorderRole, readOnlyRole); err != nil {
		t.Fatal(err)
	}
	var runtimeInsert, runtimeStateUpdate, runtimeExecute bool
	var recorderSelect, readOnlySelect, readOnlyInsert bool
	err := pool.QueryRow(ctx, `SELECT
has_table_privilege($1,'b7_validation_suites','INSERT'),
has_table_privilege($1,'strategy_maturity_states','UPDATE'),
has_function_privilege($1,'apply_b7_maturity_promotion(text,text,text,sha256_hex,text,bigint,text,text,text,sha256_hex,text,timestamptz)','EXECUTE'),
has_table_privilege($2,'b7_validation_suites','SELECT'),
has_table_privilege($3,'strategy_maturity_events','SELECT'),
has_table_privilege($3,'strategy_maturity_events','INSERT')`,
		runtimeRole, recorderRole, readOnlyRole).Scan(
		&runtimeInsert, &runtimeStateUpdate, &runtimeExecute,
		&recorderSelect, &readOnlySelect, &readOnlyInsert)
	if err != nil || !runtimeInsert || runtimeStateUpdate || !runtimeExecute ||
		recorderSelect || !readOnlySelect || readOnlyInsert {
		t.Fatalf("B7 role matrix insert=%t update=%t execute=%t recorder=%t read=%t write=%t error=%v",
			runtimeInsert, runtimeStateUpdate, runtimeExecute, recorderSelect,
			readOnlySelect, readOnlyInsert, err)
	}
}
