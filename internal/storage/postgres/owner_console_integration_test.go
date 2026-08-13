package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"axiom/internal/accounting"
	"axiom/internal/api/console"
	"axiom/internal/api/generated"
	"axiom/internal/authentication"
	"axiom/internal/backtest"
	"axiom/internal/config"
	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/execution"
	"axiom/internal/marketdata"
	"axiom/internal/replay"
	"axiom/internal/research"
	"axiom/internal/risk"
	runtimecore "axiom/internal/runtime"
	"axiom/internal/strategies/arbitrage"
	"axiom/internal/strategies/crossarb"
	"axiom/internal/strategies/meanreversion"
	"axiom/internal/strategies/triangular"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestOwnerConsolePostgresAuthenticationCommandsAndConsoleQualification(t *testing.T) {
	ctx, cancel, pool, now := ownerConsoleQualificationDatabase(t)
	defer cancel()
	defer pool.Close()
	clock, _ := domain.NewReplayClock(now)
	authService, password, login := ownerConsoleQualificationAuthentication(t, ctx, pool, clock)
	seedResearchRegistryReferenceRows(t, ctx, pool, "decision_inputs")
	seedRecordedDatasetEvidence(t, ctx, pool, now)
	qualifyResearchRegistryDataset(t, ctx, pool)
	repository, _ := NewResearchRegistryRepository(pool)
	if err := repository.Register(ctx, researchRegistryRegistrationFixture()); err != nil {
		t.Fatal(err)
	}
	seedOwnerConsoleRegisteredReport(t, ctx, pool, now)
	seedOwnerConsoleRuntimeEvidence(t, ctx, pool, now)
	consoleStore, err := NewOwnerConsoleStore(pool, []byte(strings.Repeat("s", 32)), clock)
	if err != nil {
		t.Fatal(err)
	}
	trendStatus, err := consoleStore.Trend(ctx)
	if err != nil || trendStatus.Version != generated.TrendFollowing100 || len(trendStatus.Parameters) != 16 {
		t.Fatalf("registered Trend projection = %#v %v", trendStatus, err)
	}
	assertOwnerConsoleStablePagination(t, ctx, pool, consoleStore, now)
	assertOwnerConsoleIncidentReplayWindow(t, ctx, pool, consoleStore)
	assertOwnerConsoleRiskRecovery(t, ctx, consoleStore, login.Principal)
	assertOwnerConsoleDurableJobs(t, ctx, pool, consoleStore, login.Principal)
	assertOwnerConsoleWorkerLeasesAndRecovery(t, ctx, pool, consoleStore, login.Principal, clock)
	assertPublicShadowAndAudit(t, ctx, pool, consoleStore, login.Principal, clock)
	assertOwnerConsoleResumableStream(t, ctx, pool, consoleStore, login.Principal)
	assertOwnerConsoleSessionLimitAndHistoricalAuthorizationGuard(t, ctx, pool, authService, clock, login, password, now)
}

func assertOwnerConsoleWorkerLeasesAndRecovery(t *testing.T, ctx context.Context, pool *pgxpool.Pool,
	consoleStore *OwnerConsoleStore, principal authentication.Principal, clock *domain.ReplayClock) {
	t.Helper()
	materialize := func(_ context.Context, id string, kind string, payload json.RawMessage) (backtest.JobClaim, error) {
		if kind != "backtest" || !json.Valid(payload) {
			return backtest.JobClaim{}, errors.New("invalid qualification job")
		}
		return ownerConsoleQualificationClaim(id, kind), nil
	}
	first, err := NewOfflineJobStore(pool, "qualification-worker-one", clock, materialize)
	if err != nil {
		t.Fatal(err)
	}
	claim, ok, err := first.Claim(ctx)
	if err != nil || !ok || claim.ID == "" {
		t.Fatalf("durable claim = %#v %t %v", claim, ok, err)
	}
	result := ownerConsoleQualificationJobResult()
	canonical, _ := json.Marshal(result)
	if err = first.Complete(ctx, claim.ID, result, canonical); err != nil {
		t.Fatalf("durable completion failed: %v", err)
	}
	assertOwnerConsoleCanonicalOutputs(t, ctx, pool, claim.Manifest.RunID.Value())
	assertOwnerConsoleCompletedJob(t, ctx, consoleStore, claim, result)
	failed, ok, err := first.Claim(ctx)
	if err != nil || !ok || first.Fail(ctx, failed.ID, "qualification_failure") != nil {
		t.Fatalf("durable failure = %#v %t %v", failed, ok, err)
	}
	expired, ok, err := first.Claim(ctx)
	if err != nil || !ok {
		t.Fatalf("crash lease claim = %#v %t %v", expired, ok, err)
	}
	if err = clock.Advance(offlineJobLease + time.Second); err != nil {
		t.Fatal(err)
	}
	second, _ := NewOfflineJobStore(pool, "qualification-worker-two", clock, materialize)
	recovered, ok, err := second.Claim(ctx)
	if err != nil || !ok || recovered.ID != expired.ID {
		t.Fatalf("expired lease recovery = %#v %t %v", recovered, ok, err)
	}
	if err = second.Fail(ctx, recovered.ID, "qualification_recovered_failure"); err != nil {
		t.Fatal(err)
	}
	remaining, ok, err := first.Claim(ctx)
	if err != nil || !ok || first.Fail(ctx, remaining.ID, "qualification_queue_drained") != nil {
		t.Fatalf("qualification queue drain = %#v %t %v", remaining, ok, err)
	}
	assertOwnerConsolePauseDuringClaimMaterialization(t, ctx, pool, consoleStore, principal, clock)
}

func assertOwnerConsolePauseDuringClaimMaterialization(t *testing.T, ctx context.Context, pool *pgxpool.Pool,
	consoleStore *OwnerConsoleStore, principal authentication.Principal, clock *domain.ReplayClock) {
	t.Helper()
	speed := generated.ReplayJobRequestSpeedMaximum
	request := generated.ReplayJobRequest{ConfigurationId: "configuration-research_registry", DatasetId: "dataset-public-data-formal-pending",
		ResearchGenerationId: "generation-research_registry-1", RootSeedHash: strings.Repeat("8", 64),
		StrategyVersion: generated.ReplayJobRequestStrategyVersionTrendFollowing100, Speed: &speed}
	job, err := consoleStore.CreateJob(ctx, principal, "replay", "replay-claim-race-owner_console", request)
	if err != nil {
		t.Fatal(err)
	}
	materialize := func(ctx context.Context, id string, kind string, payload json.RawMessage) (backtest.JobClaim, error) {
		if id != job.Id || kind != "replay" || !json.Valid(payload) {
			return backtest.JobClaim{}, errors.New("invalid claim-race job")
		}
		now := clock.Now().UTC
		_, updateErr := pool.Exec(ctx, `UPDATE jobs SET state='PAUSE_REQUESTED',updated_at=$2,
		  progress_revision=progress_revision+1 WHERE id=$1 AND state='RUNNING'`, id, now)
		return ownerConsoleQualificationClaim(id, kind), updateErr
	}
	store, err := NewOfflineJobStore(pool, "qualification-claim-race", clock, materialize)
	if err != nil {
		t.Fatal(err)
	}
	claim, ok, err := store.Claim(ctx)
	if err != nil || !ok || claim.ID != job.Id {
		t.Fatalf("pause during materialization = %#v %t %v", claim, ok, err)
	}
	var state, runID string
	if err = pool.QueryRow(ctx, `SELECT state,run_id FROM jobs WHERE id=$1`, job.Id).Scan(&state, &runID); err != nil ||
		state != "PAUSE_REQUESTED" || runID != job.Id {
		t.Fatalf("pause-requested run attachment = %s/%s %v", state, runID, err)
	}
	if err = store.Fail(ctx, job.Id, "qualification_claim_race_closed"); err != nil {
		t.Fatal(err)
	}
}

func assertOwnerConsoleCanonicalOutputs(t *testing.T, ctx context.Context, pool *pgxpool.Pool, runID string) {
	t.Helper()
	outputRows, err := pool.Query(ctx, `SELECT output_hash::text,canonical_payload
	  FROM run_canonical_outputs WHERE run_id=$1 ORDER BY output_kind`, runID)
	if err != nil {
		t.Fatal(err)
	}
	canonicalOutputs := 0
	for outputRows.Next() {
		var stored string
		var payload []byte
		if err = outputRows.Scan(&stored, &payload); err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(payload)
		if stored != hex.EncodeToString(digest[:]) {
			t.Fatalf("canonical output hash mismatch: %s", stored)
		}
		canonicalOutputs++
	}
	outputRows.Close()
	if err = outputRows.Err(); err != nil || canonicalOutputs != 5 {
		t.Fatalf("canonical outputs = %d %v", canonicalOutputs, err)
	}
}

func assertOwnerConsoleCompletedJob(t *testing.T, ctx context.Context, consoleStore *OwnerConsoleStore, claim backtest.JobClaim, result backtest.CanonicalResult) {
	t.Helper()
	projection, err := consoleStore.Job(ctx, claim.ID, "")
	if err != nil || projection.State != generated.JobResourceState("SUCCEEDED") || projection.Result == nil || projection.Result.ResultHash != result.ResultHash {
		t.Fatalf("completed projection = %#v %v", projection, err)
	}
	if projection.RegisteredReport == nil || projection.RegisteredReport.ResearchGenerationId != "generation-research_registry-1" ||
		len(projection.RegisteredReport.Benchmarks) != 3 || len(projection.RegisteredReport.Stress) != 6 ||
		len(projection.RegisteredReport.Capacity) != 2 || projection.RegisteredReport.ConfidenceLabel != "local_tier_b" {
		t.Fatalf("registered report projection = %#v", projection.RegisteredReport)
	}
	if projection.InputManifest == nil || projection.InputManifest.DatasetId != "dataset-public-data-formal-pending" ||
		projection.Lifecycle == nil || !projection.Lifecycle.Reproduce || projection.ReproductionBundle == nil ||
		projection.ReproductionBundle.RunId != claim.ID || projection.ReproductionBundle.ResultHash == nil ||
		*projection.ReproductionBundle.ResultHash != result.ResultHash || projection.ReproductionBundle.CanonicalManifest == "" {
		t.Fatalf("run lab reproduction projection = %#v", projection)
	}
	revision, _ := strconv.ParseInt(projection.Revision, 10, 64)
	tx, err := consoleStore.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	record := map[string]string{}
	if err = ownerControlExportJobRecord(ctx, tx, claim.ID, revision, record); err != nil ||
		record["manifest_hash"] == "" || record["configuration_id"] != "configuration-research_registry" ||
		record["dataset_id"] != "dataset-public-data-formal-pending" || record["result_hash"] != result.ResultHash {
		t.Fatalf("run lab safe export record = %#v %v", record, err)
	}
	for _, forbidden := range []string{"request_payload", "canonical_manifest", "authorization", "signature", "credential"} {
		if _, exposed := record[forbidden]; exposed {
			t.Fatalf("run lab export exposed %s", forbidden)
		}
	}
	inspection, err := consoleStore.replayInspection(ctx, claim.Manifest.RunID.Value(), "1")
	if err != nil || inspection == nil || inspection.Ordinal != "1" || inspection.EventCount != "1" ||
		inspection.EventHash == "" || inspection.CanonicalDecision != `{"outcome":"rejected"}` {
		t.Fatalf("replay inspection = %#v %v", inspection, err)
	}
}

func ownerConsoleQualificationClaim(id, kind string) backtest.JobClaim {
	hash := strings.Repeat("d", 64)
	runID, _ := domain.NewRunID(id)
	return backtest.JobClaim{TimingMode: replay.MaximumTiming, Acceleration: 1,
		Manifest: backtest.RunManifest{RunID: runID, Mode: kind,
			ResearchGenerationID: "generation-research_registry-1",
			CodeCommit:           strings.Repeat("c", 40), Build: backtest.CurrentBuildIdentity([]string{"trimpath"}, hash, hash),
			Dataset: backtest.DatasetDescriptor{DatasetID: "recorder-dataset-owner_console", ManifestHash: hash, Revision: 1,
				SourceCommit: strings.Repeat("c", 40), SchemaVersion: "dataset.v1", ParserVersion: "parser-v1",
				NormalizationVersion: "normalizer-v1", SegmentHashes: []string{hash}, RecordCount: 1,
				Complete: true, Confidence: backtest.ConfidenceB}, ConfigurationHash: hash, Seed: strings.Repeat("8", 64),
			SchedulerVersion: "scheduler-v1", SerializationVersion: "canonical-json-v1",
			Models: backtest.ModelNamespace{ID: "namespace-owner_console", MarketContext: "production-public",
				LiquidityDomain: "combined-owner_console", FeeDomain: "fee-research_registry", LatencyDomain: "latency-research_registry", FillDomain: "fill-research_registry"},
			StartingBalanceHash: hash}}
}

func ownerConsoleQualificationJobResult() backtest.CanonicalResult {
	metrics := backtest.Metrics{TotalNetReturn: "0", MaximumDrawdown: "0", CurrentDrawdown: "0",
		SharpeRatio: "0", SortinoRatio: "0", ProfitFactor: "0", Expectancy: "0", WinRate: "0",
		Turnover: "0", Exposure: "0"}
	return backtest.CanonicalResult{ManifestHash: strings.Repeat("b", 64), Confidence: backtest.ConfidenceB,
		Events: []backtest.EventResult{{Ordinal: 1, Decision: json.RawMessage(`{"outcome":"rejected"}`),
			Orders: json.RawMessage(`[]`), ExecutionEvents: json.RawMessage(`[]`), Balances: json.RawMessage(`{"USDT":"1000"}`)}},
		Metrics: metrics, ResultHash: strings.Repeat("a", 64)}
}

func seedOwnerConsoleRegisteredReport(t *testing.T, ctx context.Context, pool *pgxpool.Pool, now time.Time) {
	t.Helper()
	interval, err := research.BlockBootstrapMean([]string{"0.01", "0", "0.02", "-0.01"}, 2, 100, "owner_console-suite-seed")
	if err != nil {
		t.Fatal(err)
	}
	result := func(name string) research.ResultSlice {
		return research.ResultSlice{Name: name, NetReturn: "0.01", MaxDrawdown: "0.02", Trades: 20}
	}
	stress := make([]research.ResultSlice, 0, 6)
	for _, name := range []string{"fee", "spread", "slippage", "latency", "gap", "missed_fill"} {
		stress = append(stress, result(name))
	}
	start := now.Add(-300 * time.Hour)
	manifest, err := research.BuildReport(ownerConsoleRegisteredReportInput(start, now, interval, result, stress))
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	runReferences, _ := json.Marshal(manifest.RunReferences)
	if _, err = pool.Exec(ctx, `INSERT INTO research_reports(id,research_generation_id,manifest_hash,artifact_hash,
	  canonical_manifest,run_references,confidence_label,platform_correctness,strategy_evidence,
	  viability_disposition,disclaimer_policy,created_at) VALUES('registered-report-owner_console','generation-research_registry-1',$1,$2,$3,$4,$5,$6,$7,$8,
	  'no_production_profitability_claim',$9)`, manifest.ManifestHash, strings.Repeat("f", 64), canonical, runReferences,
		manifest.ConfidenceLabel, manifest.PlatformCorrectness, manifest.StrategyEvidence, manifest.ViabilityDisposition, manifest.CreatedAt); err != nil {
		t.Fatal(err)
	}
}

func ownerConsoleRegisteredReportInput(start, now time.Time, interval research.ConfidenceInterval,
	result func(string) research.ResultSlice, stress []research.ResultSlice) research.ReportInput {
	return research.ReportInput{
		ResearchGenerationID: "generation-research_registry-1", Hypothesis: "Strict breakouts may retain positive net expectancy after costs.",
		PrimaryMetric: "net_return", Split: research.ChronologicalSplit{
			Train:      research.Window{Name: "train", Start: start, End: start.Add(100 * time.Hour)},
			Validation: research.Window{Name: "validation", Start: start.Add(100 * time.Hour), End: start.Add(150 * time.Hour)},
			FinalTest:  research.Window{Name: "final_test", Start: start.Add(150 * time.Hour), End: start.Add(200 * time.Hour)},
		},
		WalkForward:  []research.WalkForwardFold{{TrainStart: 0, TrainEnd: 40, ValidationStart: 40, ValidationEnd: 50, TestStart: 50, TestEnd: 60}},
		Confidence:   interval,
		Neighborhood: []research.ResultSlice{result("base"), result("ema_low"), result("ema_high")},
		Capacity: []research.CapacityPoint{{Notional: "10", NetReturn: "0.01", FillRate: "1"},
			{Notional: "150", NetReturn: "0.005", FillRate: "0.9"}},
		Stress: stress, Benchmarks: []research.ResultSlice{result("cash"), result("buy_and_hold"), result("static_inventory")},
		Breakdowns: map[string][]research.ResultSlice{
			"asset": {result("BTC")}, "regime": {result("up")}, "holding_period": {result("short")},
			"false_breakout": {result("false")}, "drawdown": {result("peak")},
		},
		Rejections: map[string]uint64{"trend.reject.breakout": 5}, RunReferences: []string{"suite-run-2", "suite-run-1"},
		ConfidenceLabel: "local_tier_b", PlatformCorrectness: "Deterministic registered suite validated.",
		StrategyEvidence:     "Registered local suite remains provisional and uncertain.",
		ViabilityDisposition: "viable_for_more_research", CreatedAt: now.Add(-time.Minute),
	}
}

func ownerConsoleQualificationDatabase(t *testing.T) (context.Context, context.CancelFunc, *pgxpool.Pool, time.Time) {
	t.Helper()
	dsn := os.Getenv("AXIOM_OWNER_CONSOLE_TEST_DSN")
	if dsn == "" {
		t.Skip("AXIOM_OWNER_CONSOLE_TEST_DSN is not set")
	}
	configuration, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(configuration.ConnConfig.Database, "_owner_console_test") {
		t.Fatal("owner console integration requires a dedicated database ending _owner_console_test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	pool, err := pgxpool.NewWithConfig(ctx, configuration)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	assertEmptyTestDatabase(t, ctx, pool)
	migrations, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	if applied, applyErr := ApplyMigrations(ctx, pool); applyErr != nil || applied != len(migrations) {
		t.Fatalf("owner console migrations = %d %v", applied, applyErr)
	}
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	// The production pressure gate compares this row with the database wall
	// clock. Keep the console/replay clock deterministic, but establish a fresh
	// qualification-only NORMAL observation so the fixture does not age into a
	// fail-closed state as the calendar advances.
	seedOperationalReadinessNormalPressure(t, ctx, pool, time.Now().UTC())
	return ctx, cancel, pool, now
}

func ownerConsoleQualificationAuthentication(t *testing.T, ctx context.Context, pool *pgxpool.Pool, clock *domain.ReplayClock) (*authentication.Service, string, authentication.LoginResult) {
	t.Helper()
	now := clock.Now().UTC
	authStore, err := NewOwnerAuthenticationStore(pool)
	if err != nil {
		t.Fatal(err)
	}
	authService, err := authentication.NewService(authStore, clock, []byte(strings.Repeat("c", 32)))
	if err != nil {
		t.Fatal(err)
	}
	password := "qualification-only-password"
	passwordHash, err := (authentication.PasswordHasher{}).Hash(password)
	if err != nil {
		t.Fatal(err)
	}
	created, err := authService.Bootstrap(ctx, "owner@example.test", passwordHash)
	if err != nil || !created {
		t.Fatalf("bootstrap = %t %v", created, err)
	}
	if created, err = authService.Bootstrap(ctx, "replacement@example.test", passwordHash); err != nil || created {
		t.Fatalf("existing owner overwritten = %t %v", created, err)
	}

	login, err := authService.Login(ctx, "OWNER@example.test", password, "127.0.0.1", "login-owner_console")
	if err != nil {
		t.Fatal(err)
	}
	assertOwnerConsoleSecretsAreHashed(t, ctx, pool, password, passwordHash, login)
	if err = authService.ValidateRequestCSRF(ctx, login.SessionToken, login.CSRFToken, login.CSRFToken); err != nil {
		t.Fatalf("valid CSRF rejected: %v", err)
	}
	if err = authService.ValidateRequestCSRF(ctx, login.SessionToken, login.CSRFToken, "different"); !errors.Is(err, authentication.ErrCSRFInvalid) {
		t.Fatalf("mismatched CSRF accepted: %v", err)
	}
	later, err := authStore.TouchSession(ctx, login.Principal.SessionID, now.Add(2*time.Second), now.Add(authentication.IdleLifetime))
	if err != nil {
		t.Fatal(err)
	}
	earlier, err := authStore.TouchSession(ctx, login.Principal.SessionID, now.Add(time.Second), now.Add(authentication.IdleLifetime-time.Second))
	if err != nil || earlier.LastSeenAt.Before(later.LastSeenAt) || earlier.IdleExpiresAt.Before(later.IdleExpiresAt) {
		t.Fatalf("out-of-order session touch regressed: %#v %#v %v", later, earlier, err)
	}
	return authService, password, login
}

func assertOwnerConsoleResumableStream(t *testing.T, ctx context.Context, pool *pgxpool.Pool, store *OwnerConsoleStore, principal authentication.Principal) {
	t.Helper()
	var maximum int64
	if err := pool.QueryRow(ctx, `SELECT max(revision) FROM outbox_events`).Scan(&maximum); err != nil {
		t.Fatal(err)
	}
	requestContext, cancel := context.WithCancel(ctx)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/stream", nil).WithContext(requestContext)
	request.Header.Set("Last-Event-ID", strconv.FormatInt(maximum-1, 10))
	response := httptest.NewRecorder()
	timer := time.AfterFunc(100*time.Millisecond, cancel)
	err := store.Serve(response, request, principal)
	timer.Stop()
	if err != nil {
		t.Fatalf("resumable stream failed: %v", err)
	}
	body := response.Body.String()
	if response.Code != http.StatusOK || !strings.Contains(body, "id: "+strconv.FormatInt(maximum, 10)+"\n") ||
		!strings.Contains(body, `"schema_version":"axiom.stream.v1"`) || strings.Contains(body, "event: ") {
		t.Fatalf("resumable stream envelope = %d %q", response.Code, body)
	}
	var invalidStreams, openConnections int
	if err = pool.QueryRow(ctx, `SELECT
	      count(*) FILTER (WHERE stream NOT IN ('system','exchange','portfolio','risk','trend','strategy','job','shadow','incident','alert','order','fill')),
      (SELECT count(*) FROM stream_connections WHERE closed_at IS NULL)
      FROM outbox_events`).Scan(&invalidStreams, &openConnections); err != nil || invalidStreams != 0 || openConnections != 0 {
		t.Fatalf("stream safety invalid/open = %d/%d %v", invalidStreams, openConnections, err)
	}
	for index := 0; index < 3; index++ {
		if _, err = pool.Exec(ctx, `INSERT INTO stream_connections(id,user_id,session_id,opened_at,heartbeat_at,last_revision) VALUES($1,$2,$3,$4,$4,0)`, "quota-stream-"+strconv.Itoa(index), principal.UserID, principal.SessionID, time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
	}
	quotaRequest := httptest.NewRequest(http.MethodGet, "/api/v1/stream", nil)
	if err = store.Serve(httptest.NewRecorder(), quotaRequest, principal); !errors.Is(err, console.ErrQuota) {
		t.Fatalf("fourth user stream accepted: %v", err)
	}
	if _, err = pool.Exec(ctx, `UPDATE stream_connections SET closed_at=$1 WHERE id LIKE 'quota-stream-%'`, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
}

func assertOwnerConsoleStablePagination(t *testing.T, ctx context.Context, pool *pgxpool.Pool, store *OwnerConsoleStore, now time.Time) {
	t.Helper()
	for index, id := range []string{"incident-z", "incident-a", "incident-m"} {
		if _, err := pool.Exec(ctx, `INSERT INTO incidents(id,severity,state,reason_code,opened_at) VALUES($1,'warning','resolved','pagination_fixture',$2)`, id, now.Add(time.Duration(index)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	first, err := store.Incidents(ctx, "", 1, "")
	if err != nil || len(first.Items) != 1 || first.NextCursor == nil || first.Items[0].Id != "incident-m" {
		t.Fatalf("first incident page = %#v %v", first, err)
	}
	second, err := store.Incidents(ctx, *first.NextCursor, 1, "")
	if err != nil || len(second.Items) != 1 || second.Items[0].Id != "incident-a" || second.Items[0].Id == first.Items[0].Id {
		t.Fatalf("second incident page = %#v %v", second, err)
	}
	if _, err = store.TrendDecisions(ctx, *first.NextCursor, 1); !errors.Is(err, console.ErrInvalidRequest) {
		t.Fatalf("filter/sort-bound cursor crossed resource scope: %v", err)
	}
}

func assertOwnerConsoleIncidentReplayWindow(t *testing.T, ctx context.Context, pool *pgxpool.Pool, store *OwnerConsoleStore) {
	t.Helper()
	detail, err := store.Incident(ctx, "incident-z", false)
	if err != nil || detail.ReplayWindow.DatasetId != "dataset-public-data-formal-pending" ||
		detail.ReplayWindow.FirstOrdinal != "1" || detail.ReplayWindow.LastOrdinal != "1" {
		t.Fatalf("qualified incident replay window = %#v %v", detail.ReplayWindow, err)
	}
	incidentID, first, last := "incident-z", generated.Revision("1"), generated.Revision("1")
	request := generated.ReplayJobRequest{ConfigurationId: "configuration-research_registry", DatasetId: "dataset-public-data-formal-pending",
		ResearchGenerationId: "generation-research_registry-1",
		RootSeedHash:         strings.Repeat("8", 64), StrategyVersion: generated.ReplayJobRequestStrategyVersionTrendFollowing100,
		IncidentId: &incidentID, FirstOrdinal: &first, LastOrdinal: &last}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err = validateOwnerConsoleIncidentReplay(ctx, tx, request); err != nil {
		t.Fatalf("exact incident replay rejected: %v", err)
	}
	changed := generated.Revision("2")
	request.LastOrdinal = &changed
	if err = validateOwnerConsoleIncidentReplay(ctx, tx, request); !errors.Is(err, console.ErrPrecondition) {
		t.Fatalf("altered incident replay accepted: %v", err)
	}
}

func assertOwnerConsoleSecretsAreHashed(t *testing.T, ctx context.Context, pool *pgxpool.Pool, password, passwordHash string, login authentication.LoginResult) {
	t.Helper()
	var storedPassword, tokenHash, csrfHash string
	if err := pool.QueryRow(ctx, `SELECT u.password_hash,s.token_hash,s.csrf_token_hash FROM users u JOIN sessions s ON s.user_id=u.id WHERE s.id=$1`, login.Principal.SessionID).
		Scan(&storedPassword, &tokenHash, &csrfHash); err != nil {
		t.Fatal(err)
	}
	if storedPassword == password || storedPassword != passwordHash || !strings.HasPrefix(storedPassword, "$argon2id$") {
		t.Fatal("bootstrap credential was not retained exclusively as the supplied Argon2id hash")
	}
	wantToken := sha256.Sum256([]byte(login.SessionToken))
	wantCSRF := sha256.Sum256([]byte(login.CSRFToken))
	if tokenHash != hex.EncodeToString(wantToken[:]) || csrfHash != hex.EncodeToString(wantCSRF[:]) ||
		tokenHash == login.SessionToken || csrfHash == login.CSRFToken {
		t.Fatal("opaque session material was not stored exclusively as hashes")
	}
	var bootstrapAudit int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE event_type='owner_bootstrapped'`).Scan(&bootstrapAudit); err != nil || bootstrapAudit != 1 {
		t.Fatalf("bootstrap audit count = %d %v", bootstrapAudit, err)
	}
}

func seedOwnerConsoleRuntimeEvidence(t *testing.T, ctx context.Context, pool *pgxpool.Pool, now time.Time) {
	t.Helper()
	hash := strings.Repeat("7", 64)
	payload, err := json.Marshal(config.DefaultMultiStrategyConfiguration())
	if err != nil {
		t.Fatal(err)
	}
	for index, statement := range ownerConsoleRuntimeEvidenceStatements(hash, researchRegistryPayloadHash(payload), payload, now) {
		if _, err := pool.Exec(ctx, statement.sql, statement.args...); err != nil {
			t.Fatalf("runtime evidence seed %d failed: %v", index+1, err)
		}
	}
	seedOwnerConsoleRecoveryEvidence(t, ctx, pool, hash, now)
}

func ownerConsoleRuntimeEvidenceStatements(hash, configurationHash string, payload []byte, now time.Time) []struct {
	sql  string
	args []any
} {
	return []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO runs(id,mode,configuration_id,strategy_version_id,dataset_id,root_seed_hash,reproducibility_hash,state,created_at)
          VALUES('run-owner_console-shadow','shadow','configuration-research_registry','trend-following-1-0-0','dataset-public-data-formal-pending',$1,$1,'created',$2)`, []any{hash, now}},
		{`INSERT INTO portfolios(id,name,reporting_asset,created_at) VALUES('portfolio-owner_console','owner console virtual portfolio','USDT',$1)`, []any{now}},
		{`INSERT INTO configuration_versions(id,version,configuration_hash,canonical_payload,actor,recorded_at)
		  VALUES('configuration-shadow-research',2,$1,$2,'test',$3)`, []any{configurationHash, payload, now}},
		{`INSERT INTO strategy_definitions(id,name,family)
		  VALUES('mean-reversion','Mean Reversion','mean_reversion')`, nil},
		{`INSERT INTO strategy_versions(id,strategy_id,version,implementation_hash,promotion_status,created_at,
		  supported_modes)
		  VALUES('mean-reversion-1-0-0','mean-reversion',1,$1,'research',$2,
		  ARRAY['backtest','replay','paper','shadow'])`, []any{hash, now}},
		{`INSERT INTO experiment_registrations(id,strategy_version_id,configuration_id,dataset_id,hypothesis,status,registered_at)
		  VALUES('experiment-owner_console-shadow-bybit','trend-following-1-0-0','configuration-shadow-research','dataset-public-data-formal-pending',
		  'qualify semantic Bybit public-shadow input resolution','registered',$1)`, []any{now}},
		{`INSERT INTO experiment_registrations(id,strategy_version_id,configuration_id,dataset_id,hypothesis,status,registered_at)
		  VALUES('experiment-owner_console-shadow-mean','mean-reversion-1-0-0','configuration-shadow-research','dataset-public-data-formal-pending',
		  'qualify semantic Mean Reversion public-shadow input resolution','registered',$1)`, []any{now}},
		{`INSERT INTO model_namespaces(id,namespace_hash,market_context,liquidity_domain,fee_model_id,latency_model_id,
		  fill_model_id,price_model_hash,canonical_payload,created_at)
		  VALUES('namespace-owner_console',$1,'production-public','combined-owner_console','fixed-bps-v1','fixed-zero-v1','fill-v1',$1,'{}',$2)`, []any{hash, now}},
		{`INSERT INTO virtual_accounts(id,portfolio_id,run_id,name,created_at) VALUES('account-owner_console','portfolio-owner_console','run-owner_console-shadow','main',$1)`, []any{now}},
		{`INSERT INTO virtual_balances(account_id,asset_symbol,available,reserved,revision,updated_at)
          VALUES('account-owner_console','USDT',1000,0,1,$1),('account-owner_console','BTC',0,0,1,$1)`, []any{now}},
		{`INSERT INTO instrument_metadata_versions(id,exchange_id,instrument_id,version,price_tick,quantity_step,
		  minimum_quantity,minimum_notional,effective_at,recorded_at)
		  VALUES('metadata-owner_console-binance','binance','instrument-research_registry',1,0.01,0.00001,0.00001,10,$1,$1),
		        ('metadata-owner_console-binance-eth','binance','instrument-eth-research_registry',1,0.01,0.00001,0.00001,10,$1,$1),
		        ('metadata-owner_console-bybit','bybit','instrument-research_registry',1,0.01,0.00001,0.00001,10,$1,$1),
		        ('metadata-owner_console-bybit-eth','bybit','instrument-eth-research_registry',1,0.01,0.00001,0.00001,10,$1,$1)`, []any{now}},
		{`INSERT INTO startup_recovery_attempts(id,run_id,state,build_hash,configuration_hash,started_at,completed_at)
	          VALUES('recovery-owner_console','run-owner_console-shadow','ready_paused',$1,$1,$2,$2)`, []any{hash, now}},
	}
}

func seedOwnerConsoleRecoveryEvidence(t *testing.T, ctx context.Context, pool *pgxpool.Pool, hash string, now time.Time) {
	t.Helper()
	stages := []string{"database_prerequisites", "fenced_ownership", "build_safety_manifest", "configuration_graph",
		"schema_and_durability", "checkpoint_and_cursor", "protected_state", "committed_event_replay",
		"journal_and_projections", "simulator_reconciliation", "recorder_segments", "public_market_state",
		"operational_invariants", "administrative_readiness"}
	for ordinal, stage := range stages {
		if _, err := pool.Exec(ctx, `INSERT INTO startup_recovery_evidence(attempt_id,ordinal,stage,evidence_hash,recorded_at) VALUES('recovery-owner_console',$1,$2,$3,$4)`, ordinal, stage, hash, now); err != nil {
			t.Fatalf("recovery evidence %d failed: %v", ordinal, err)
		}
	}
}

func seedRecordedDatasetEvidence(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	now time.Time,
) {
	t.Helper()
	hash := strings.Repeat("7", 64)
	statements := []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO market_data_segments(id,recorder_session,exchange_id,instrument_id,event_type,schema_version,parser_version,normalization_version,compression,path,checksum,ordered_content_hash,record_count,first_ordinal,last_ordinal,started_at,ended_at,state,finalized_at)
		  VALUES('segment-owner_console','recorder-owner_console','binance','instrument-research_registry','book','market-wire.v1','parser-owner_console','normalizer-owner_console','zstd','owner_console/segment.zst',$1,$1,1,1,1,$2,$2,'ready',$2)`, []any{hash, now}},
		{`INSERT INTO market_data_segments(id,recorder_session,exchange_id,instrument_id,event_type,schema_version,parser_version,normalization_version,compression,path,checksum,ordered_content_hash,record_count,first_ordinal,last_ordinal,started_at,ended_at,state,finalized_at)
		  VALUES('segment-owner_console-candle','recorder-owner_console','binance','instrument-research_registry','candle','market-wire.v1','parser-owner_console','normalizer-owner_console','zstd','owner_console/candle.zst',$1,$1,1,2,2,$2,$2,'ready',$2)`, []any{hash, now}},
		{`INSERT INTO market_data_segments(id,recorder_session,exchange_id,instrument_id,event_type,schema_version,parser_version,normalization_version,compression,path,checksum,ordered_content_hash,record_count,first_ordinal,last_ordinal,started_at,ended_at,state,finalized_at)
		  VALUES('segment-owner_console-bybit-candle','recorder-owner_console-bybit','bybit','instrument-research_registry','candle','market-wire.v2','parser-exchange_expansion','normalizer-exchange_expansion','zstd','owner_console/bybit-candle.zst',$1,$1,1,3,3,$2,$2,'ready',$2)`, []any{hash, now}},
		{`INSERT INTO dataset_segments(dataset_id,segment_id,ordinal)
		  VALUES('dataset-public-data-formal-pending','segment-owner_console',0)`, nil},
	}
	if os.Getenv("AXIOM_OWNER_CONSOLE_E2E_DATASET_MANIFEST") != "" {
		seedOwnerConsoleE2EDatasetWindow(t, ctx, pool)
	}
	for index, statement := range statements {
		if _, err := pool.Exec(ctx, statement.sql, statement.args...); err != nil {
			t.Fatalf("owner console dataset evidence %d failed: %v", index+1, err)
		}
	}
}

func seedOwnerConsoleE2EDatasetWindow(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) {
	t.Helper()
	now := time.Now().UTC()
	_, err := pool.Exec(ctx, `INSERT INTO market_data_segments(
 id,recorder_session,exchange_id,instrument_id,event_type,schema_version,
 parser_version,normalization_version,compression,path,checksum,
 ordered_content_hash,record_count,first_ordinal,last_ordinal,started_at,
 ended_at,state,finalized_at
) VALUES(
 'segment-owner_console-e2e-window','owner_console-e2e-window','binance','instrument-research_registry',
 'candle','market-wire.v1','decision-input-v1','decision-input-v1','zstd',
 'owner_console/e2e-window.zst',$1,$1,1,1,1,$2,$3,'ready',$3
)`, strings.Repeat("9", 64), now.Add(-time.Second), now.Add(time.Second))
	if err != nil {
		t.Fatalf("owner console E2E window segment failed: %v", err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO dataset_segments(
 dataset_id,segment_id,ordinal
) VALUES('dataset-public-data-formal-pending','segment-owner_console-e2e-window',1)`); err != nil {
		t.Fatalf("owner console E2E window membership failed: %v", err)
	}
}

func assertOwnerConsoleRiskRecovery(t *testing.T, ctx context.Context, store *OwnerConsoleStore, principal authentication.Principal) {
	t.Helper()
	risk, err := store.Risk(ctx)
	if err != nil || risk.State != generated.RiskStatusState("PAUSED") || risk.Revision != "1" || !risk.RecoveryReady {
		t.Fatalf("initial fail-closed risk = %#v %v", risk, err)
	}
	stale := generated.RevisionCommandRequest{ExpectedRevision: "2", Reason: "qualification recovery"}
	if _, err = store.RiskCommand(ctx, principal, "resume", "risk-resume-stale-owner_console", stale); !errors.Is(err, console.ErrConflict) {
		t.Fatalf("stale recovery revision accepted: %v", err)
	}
	request := generated.RevisionCommandRequest{ExpectedRevision: "1", Reason: "qualification recovery"}
	accepted, err := store.RiskCommand(ctx, principal, "resume", "risk-resume-valid-owner_console", request)
	if err != nil || accepted.State != generated.CommandAcceptedStateApplied {
		t.Fatalf("safe recovery rejected: %#v %v", accepted, err)
	}
	replayed, err := store.RiskCommand(ctx, principal, "resume", "risk-resume-valid-owner_console", request)
	if err != nil || replayed.Id != accepted.Id {
		t.Fatalf("idempotent risk command changed: %#v %v", replayed, err)
	}
	conflict := request
	conflict.Reason = "different qualification recovery"
	if _, err = store.RiskCommand(ctx, principal, "resume", "risk-resume-valid-owner_console", conflict); !errors.Is(err, console.ErrIdempotencyConflict) {
		t.Fatalf("idempotency conflict accepted: %v", err)
	}
	risk, err = store.Risk(ctx)
	if err != nil || risk.State != generated.RiskStatusState("NORMAL") || risk.Revision != "2" {
		t.Fatalf("resumed risk projection = %#v %v", risk, err)
	}
}

func assertOwnerConsoleDurableJobs(t *testing.T, ctx context.Context, pool *pgxpool.Pool, store *OwnerConsoleStore, principal authentication.Principal) {
	t.Helper()
	request := generated.OfflineJobRequest{ConfigurationId: "configuration-research_registry", DatasetId: "dataset-public-data-formal-pending",
		ResearchGenerationId: "generation-research_registry-1",
		RootSeedHash:         strings.Repeat("8", 64), StrategyVersion: generated.OfflineJobRequestStrategyVersionTrendFollowing100}
	first, err := store.CreateJob(ctx, principal, "backtest", "backtest-idempotent-owner_console", request)
	if err != nil || first.State != generated.JobResourceState("QUEUED") {
		t.Fatalf("backtest create = %#v %v", first, err)
	}
	replayed, err := store.CreateJob(ctx, principal, "backtest", "backtest-idempotent-owner_console", request)
	if err != nil || replayed.Id != first.Id {
		t.Fatalf("backtest replay changed identity = %#v %v", replayed, err)
	}
	changed := request
	changed.RootSeedHash = strings.Repeat("9", 64)
	if _, err = store.CreateJob(ctx, principal, "backtest", "backtest-idempotent-owner_console", changed); !errors.Is(err, console.ErrIdempotencyConflict) {
		t.Fatalf("job idempotency conflict accepted: %v", err)
	}
	for index := 2; index <= 4; index++ {
		if _, err = store.CreateJob(ctx, principal, "backtest", "backtest-queued-owner_console-"+string(rune('0'+index)), request); err != nil {
			t.Fatalf("queued job %d rejected: %v", index, err)
		}
	}
	if _, err = store.CreateJob(ctx, principal, "backtest", "backtest-queued-owner_console-5", request); !errors.Is(err, console.ErrQuota) {
		t.Fatalf("fifth queued job accepted: %v", err)
	}
	retriedAtQuota, err := store.CreateJob(ctx, principal, "backtest", "backtest-idempotent-owner_console", request)
	if err != nil || retriedAtQuota.Id != first.Id {
		t.Fatalf("accepted idempotent retry was blocked by quota: %#v %v", retriedAtQuota, err)
	}
	var commands, jobs, outbox int
	if err = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM command_requests),(SELECT count(*) FROM jobs),(SELECT count(*) FROM outbox_events)`).Scan(&commands, &jobs, &outbox); err != nil {
		t.Fatal(err)
	}
	if commands != 5 || jobs != 4 || outbox != 5 {
		t.Fatalf("durable command/job/outbox counts = %d/%d/%d", commands, jobs, outbox)
	}
}

func assertPublicShadowAndAudit(t *testing.T, ctx context.Context, pool *pgxpool.Pool, store *OwnerConsoleStore, principal authentication.Principal, clock *domain.ReplayClock) {
	t.Helper()
	request := generated.ShadowSessionRequest{ConfigurationId: "configuration-research_registry", PortfolioId: "portfolio-owner_console",
		StrategyVersion: generated.ShadowSessionRequestStrategyVersionTrendFollowing100}
	shadow, err := store.CreateShadow(ctx, principal, "shadow-create-owner_console", request)
	if err != nil || !bool(shadow.PublicOnly) || !bool(shadow.SimulationOnly) || shadow.EntriesEnabled || shadow.State != generated.ShadowSessionResourceState("QUEUED") {
		t.Fatalf("shadow safety projection = %#v %v", shadow, err)
	}
	if _, err = store.CreateShadow(ctx, principal, "shadow-create-second-owner_console", request); !errors.Is(err, console.ErrConflict) {
		t.Fatalf("second active shadow accepted: %v", err)
	}
	stop := generated.RevisionCommandRequest{ExpectedRevision: shadow.Revision, Reason: "qualification stop"}
	if _, err = store.StopShadow(ctx, principal, shadow.Id, "shadow-stop-owner_console", stop); err != nil {
		t.Fatalf("shadow stop rejected: %v", err)
	}
	assertPublicShadowRuntimeEvidence(t, ctx, pool, store, principal, clock, request)
	assertPublicShadowImmutability(t, ctx, pool)
}

func assertPublicShadowRuntimeEvidence(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	store *OwnerConsoleStore,
	principal authentication.Principal,
	clock *domain.ReplayClock,
	request generated.ShadowSessionRequest,
) {
	t.Helper()
	runtimeShadow, err := store.CreateShadow(ctx, principal, "shadow-runtime-owner_console", request)
	if err != nil {
		t.Fatal(err)
	}
	runtimeStore, err := NewPublicShadowStore(pool, "qualification-shadow-runtime", clock)
	if err != nil {
		t.Fatal(err)
	}
	claim, found, err := runtimeStore.Claim(ctx)
	if err != nil || !found || claim.ID != runtimeShadow.Id || claim.Models.ID != "namespace-owner_console" ||
		claim.SlippageModelID != "slippage-v1" || claim.GapModelID != "gap-v1" {
		t.Fatalf("shadow runtime claim = %#v %t %v", claim, found, err)
	}
	if err = runtimeStore.Fail(ctx, claim.ID, "qualification_complete"); err != nil {
		t.Fatal(err)
	}
	projection, err := store.Shadow(ctx, claim.ID)
	if err != nil || projection.RunId == nil || *projection.RunId != claim.ID ||
		projection.PnlAttribution == nil || projection.Decisions == nil || projection.Balances == nil ||
		projection.Positions == nil || projection.ExchangeId == nil || *projection.ExchangeId != "binance" {
		t.Fatalf("run lab shadow evidence projection = %#v %v", projection, err)
	}
	history, err := store.Shadows(ctx, "", 10, "FAILED")
	if err != nil || len(history.Items) != 1 || history.Items[0].Id != claim.ID ||
		!bool(history.Items[0].PublicOnly) || !bool(history.Items[0].SimulationOnly) {
		t.Fatalf("run lab shadow history = %#v %v", history, err)
	}
	assertOwnerConsoleBybitShadowSelection(t, ctx, store, runtimeStore, principal)
	assertOwnerConsoleMeanReversionShadowSelection(t, ctx, pool, store, runtimeStore, principal)
	assertOwnerConsoleTriangularShadowPersistence(t, ctx, pool, store, runtimeStore, principal, clock)
	assertOwnerConsoleCrossExchangeShadowPersistence(t, ctx, pool, store, runtimeStore, principal, clock)
}

func assertOwnerConsoleTriangularShadowPersistence(t *testing.T, ctx context.Context, pool *pgxpool.Pool,
	store *OwnerConsoleStore, runtimeStore *PublicShadowStore, principal authentication.Principal,
	clock *domain.ReplayClock,
) {
	t.Helper()
	now := clock.Now().UTC
	seedOwnerConsoleTriangularShadow(t, ctx, pool, now)
	claim := ownerConsoleTriangularShadowClaim(t, ctx, pool, store, runtimeStore, principal)
	prepared, result, settlement, capital := ownerConsoleTriangularShadowResult(t, claim, now)
	updated, err := runtimeStore.RecordTriangularShadowDecision(ctx, claim, prepared, result)
	if err != nil || len(updated) != 3 || updated[settlement].Available.Compare(capital) <= 0 {
		t.Fatalf("Triangle shadow persistence=%#v error=%v", updated, err)
	}
	assertOwnerConsoleTriangularShadowEvidence(t, ctx, pool, claim)
	if err = runtimeStore.Fail(ctx, claim.ID, "qualification_complete"); err != nil {
		t.Fatal(err)
	}
}

func seedOwnerConsoleTriangularShadow(t *testing.T, ctx context.Context, pool *pgxpool.Pool, now time.Time) {
	t.Helper()
	hash := strings.Repeat("8", 64)
	statements := []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO strategy_definitions(id,name,family)
         VALUES('triangular-shadow-owner_console','Triangular Arbitrage','triangular')`, nil},
		{`INSERT INTO strategy_versions(id,strategy_id,version,implementation_hash,promotion_status,created_at,supported_modes)
         VALUES('triangular-arbitrage-1-0-0','triangular-shadow-owner_console',1,$1,'research',$2,ARRAY['backtest','replay','shadow'])`, []any{hash, now}},
		{`INSERT INTO experiment_registrations(id,strategy_version_id,configuration_id,dataset_id,hypothesis,status,registered_at)
         VALUES('experiment-owner_console-shadow-triangle','triangular-arbitrage-1-0-0','configuration-shadow-research',
         'dataset-public-data-formal-pending','qualify exact three-market public shadow','registered',$1)`, []any{now}},
		{`INSERT INTO instruments(id,base_asset,quote_asset,product)
         VALUES('instrument-ethbtc-owner_console','ETH','BTC','spot')`, nil},
		{`INSERT INTO instrument_metadata_versions(id,exchange_id,instrument_id,version,price_tick,quantity_step,
         minimum_quantity,minimum_notional,effective_at,recorded_at)
         VALUES('metadata-owner_console-binance-ethbtc','binance','instrument-ethbtc-owner_console',1,0.000001,0.0001,0.0001,0.00001,$1,$1)`, []any{now}},
		{`INSERT INTO market_data_segments(id,recorder_session,exchange_id,instrument_id,event_type,schema_version,
         parser_version,normalization_version,compression,path,checksum,ordered_content_hash,record_count,
         first_ordinal,last_ordinal,started_at,ended_at,state,finalized_at)
         VALUES('segment-owner_console-triangle-ethusdt','triangle-owner_console','binance','instrument-eth-research_registry','book',
         'market-wire.v1','parser-owner_console','normalizer-owner_console','zstd','owner_console/triangle-ethusdt.zst',$1,$1,1,4,4,$2,$2,'ready',$2)`, []any{hash, now}},
		{`INSERT INTO market_data_segments(id,recorder_session,exchange_id,instrument_id,event_type,schema_version,
         parser_version,normalization_version,compression,path,checksum,ordered_content_hash,record_count,
         first_ordinal,last_ordinal,started_at,ended_at,state,finalized_at)
         VALUES('segment-owner_console-triangle-ethbtc','triangle-owner_console','binance','instrument-ethbtc-owner_console','book',
         'market-wire.v1','parser-owner_console','normalizer-owner_console','zstd','owner_console/triangle-ethbtc.zst',$1,$1,1,5,5,$2,$2,'ready',$2)`, []any{hash, now}},
	}
	for index, statement := range statements {
		if _, err := pool.Exec(ctx, statement.sql, statement.args...); err != nil {
			t.Fatalf("Triangle shadow seed %d failed: %v", index+1, err)
		}
	}
}

func ownerConsoleTriangularShadowClaim(t *testing.T, ctx context.Context, pool *pgxpool.Pool,
	store *OwnerConsoleStore, runtimeStore *PublicShadowStore, principal authentication.Principal,
) PublicShadowClaim {
	t.Helper()
	created, err := store.CreateRun(ctx, principal, "shadow-triangle-owner_console", generated.RunCreateRequest{
		StrategyId: "triangular-arbitrage", StrategyVersion: "triangular-arbitrage@1.0.0",
		Mode:       generated.RunCreateRequestModeShadow,
		Exchanges:  []generated.RunCreateRequestExchanges{generated.RunCreateRequestExchangesBinance},
		Instrument: "BTC/USDT", Preset: generated.LatestQualifiedInputs,
	})
	if err != nil {
		t.Fatalf("Triangle selected shadow create failed: %v", err)
	}
	claim, found, err := runtimeStore.Claim(ctx)
	if err != nil || !found || claim.ID != created.Id || claim.StrategyID != "triangular-arbitrage-1-0-0" ||
		claim.StrategyVersion != "triangular-arbitrage@1.0.0" || len(claim.MarketScopes) != 3 {
		t.Fatalf("Triangle shadow claim=%#v found=%t error=%v", claim, found, err)
	}
	if _, err = pool.Exec(ctx, `UPDATE shadow_sessions SET claim_expires_at=CURRENT_TIMESTAMP+interval '1 minute'
	      WHERE id=$1`, claim.ID); err != nil {
		t.Fatal(err)
	}
	return claim
}

func ownerConsoleTriangularShadowResult(t *testing.T, claim PublicShadowClaim, now time.Time,
) (triangular.Input, backtest.EventResult, domain.AssetSymbol, domain.Balance) {
	t.Helper()
	input := ownerConsoleTriangularRecordedInput(t)
	configured, err := triangular.ConfigurationFromReviewed(claim.Configuration.Triangular)
	if err != nil {
		t.Fatal(err)
	}
	capital, _ := domain.ParseBalance(claim.Configuration.Portfolio.StartingCapital.Value)
	settlement, _ := domain.ParseAssetSymbol("USDT")
	input.Configuration, input.ConfigurationHash = configured, claim.ConfigurationHash
	input.AvailableSettlement, input.StrategyBudget = capital, capital
	input.FeeBalances = map[domain.AssetSymbol]domain.Balance{settlement: capital}
	input.Now = now
	prepared, projection, err := triangular.AttachCleanRecordedReduction(input,
		"shadow/triangular/"+claim.ID)
	if err != nil || projection == nil {
		t.Fatalf("Triangle shadow projection=%#v error=%v", projection, err)
	}
	transactions, err := ownerConsoleTriangularTransactions(claim, prepared, projection)
	if err != nil {
		t.Fatal(err)
	}
	decisionPayload, _ := json.Marshal(risk.Decision{Action: risk.ActionApprove,
		ReasonCode: "approved", EffectiveState: risk.StateNormal, EvaluatedAt: now})
	planPayload, _ := json.Marshal(projection.Plan)
	simulationPayload, _ := json.Marshal(projection.Simulation)
	reductionPayload, _ := json.Marshal(ownerConsoleTriangularReductionEvidence{Transactions: transactions})
	result := backtest.EventResult{Ordinal: prepared.Ordinal, Decision: decisionPayload,
		Orders: planPayload, ExecutionEvents: simulationPayload, Balances: reductionPayload}
	return prepared, result, settlement, capital
}

func assertOwnerConsoleTriangularShadowEvidence(t *testing.T, ctx context.Context, pool *pgxpool.Pool, claim PublicShadowClaim) {
	t.Helper()
	var decisions, executions, journals, lines int
	if err := pool.QueryRow(ctx, `SELECT
      (SELECT count(*) FROM shadow_strategy_decision_evidence WHERE strategy_version_id='triangular-arbitrage-1-0-0'),
      (SELECT count(*) FROM shadow_multileg_execution_evidence WHERE strategy_version_id='triangular-arbitrage-1-0-0'),
      (SELECT count(*) FROM journal_transactions WHERE run_id=$1),
      (SELECT count(*) FROM ledger_entries entry JOIN journal_transactions journal
       ON journal.id=entry.transaction_id WHERE journal.run_id=$1)`, claim.RunID).
		Scan(&decisions, &executions, &journals, &lines); err != nil ||
		decisions != 1 || executions != 1 || journals == 0 || lines != journals*2 {
		t.Fatalf("Triangle evidence decisions=%d executions=%d journals=%d lines=%d error=%v",
			decisions, executions, journals, lines, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE shadow_multileg_execution_evidence SET outcome='tampered'
	      WHERE strategy_version_id='triangular-arbitrage-1-0-0'`); err == nil {
		t.Fatal("immutable Triangle execution evidence was mutated")
	}
}

func assertOwnerConsoleCrossExchangeShadowPersistence(t *testing.T, ctx context.Context, pool *pgxpool.Pool,
	store *OwnerConsoleStore, runtimeStore *PublicShadowStore, principal authentication.Principal,
	clock *domain.ReplayClock,
) {
	t.Helper()
	now := clock.Now().UTC
	seedOwnerConsoleCrossExchangeShadow(t, ctx, pool, now)
	claim := ownerConsoleCrossExchangeShadowClaim(t, ctx, pool, store, runtimeStore, principal)
	markets, coherent := ownerConsoleCrossExchangeRecordedMarkets(t, now, claim.Configuration.Models.Fee)
	initialized, err := runtimeStore.InitializeCrossExchangeShadowInventory(ctx, claim, markets, now)
	if err != nil || len(initialized) != 2 {
		t.Fatalf("Cross-Exchange inventory initialization=%#v error=%v", initialized, err)
	}
	prepared, result := ownerConsoleCrossExchangeShadowResult(t, claim, markets, coherent, initialized, now)
	updated, err := runtimeStore.RecordCrossExchangeShadowDecision(ctx, claim, prepared, result)
	assertOwnerConsoleCrossExchangeUpdatedBalances(t, initialized, updated, err)
	assertOwnerConsoleCrossExchangeShadowEvidence(t, ctx, pool, claim)
	if err = runtimeStore.Fail(ctx, claim.ID, "qualification_complete"); err != nil {
		t.Fatal(err)
	}
}

func seedOwnerConsoleCrossExchangeShadow(t *testing.T, ctx context.Context, pool *pgxpool.Pool, now time.Time) {
	t.Helper()
	hash := strings.Repeat("6", 64)
	statements := []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO strategy_definitions(id,name,family)
         VALUES('cross-exchange-shadow-owner_console','Cross-Exchange Arbitrage','cross_exchange')`, nil},
		{`INSERT INTO strategy_versions(id,strategy_id,version,implementation_hash,promotion_status,created_at,supported_modes)
         VALUES('cross-exchange-arbitrage-1-0-0','cross-exchange-shadow-owner_console',1,$1,'research',$2,
         ARRAY['backtest','replay','shadow'])`, []any{hash, now}},
		{`INSERT INTO experiment_registrations(id,strategy_version_id,configuration_id,dataset_id,hypothesis,status,registered_at)
         VALUES('experiment-owner_console-shadow-cross','cross-exchange-arbitrage-1-0-0','configuration-shadow-research',
         'dataset-public-data-formal-pending','qualify exact paired public shadow','registered',$1)`, []any{now}},
		{`INSERT INTO market_data_segments(id,recorder_session,exchange_id,instrument_id,event_type,schema_version,
         parser_version,normalization_version,compression,path,checksum,ordered_content_hash,record_count,
         first_ordinal,last_ordinal,started_at,ended_at,state,finalized_at)
         VALUES('segment-owner_console-cross-bybit','cross-owner_console','bybit','instrument-research_registry','book',
         'market-wire.v1','parser-owner_console','normalizer-owner_console','zstd','owner_console/cross-bybit.zst',$1,$1,1,6,6,$2,$2,'ready',$2)`, []any{hash, now}},
	}
	for index, statement := range statements {
		if _, err := pool.Exec(ctx, statement.sql, statement.args...); err != nil {
			t.Fatalf("Cross-Exchange shadow seed %d failed: %v", index+1, err)
		}
	}
}

func ownerConsoleCrossExchangeShadowClaim(t *testing.T, ctx context.Context, pool *pgxpool.Pool,
	store *OwnerConsoleStore, runtimeStore *PublicShadowStore, principal authentication.Principal,
) PublicShadowClaim {
	t.Helper()
	created, err := store.CreateRun(ctx, principal, "shadow-cross-owner_console", generated.RunCreateRequest{
		StrategyId: "cross-exchange-arbitrage", StrategyVersion: "cross-exchange-arbitrage@1.0.0",
		Mode: generated.RunCreateRequestModeShadow,
		Exchanges: []generated.RunCreateRequestExchanges{
			generated.RunCreateRequestExchangesBinance, generated.RunCreateRequestExchangesBybit,
		},
		Instrument: "BTC/USDT", Preset: generated.LatestQualifiedInputs,
	})
	if err != nil || created.Exchanges == nil || len(*created.Exchanges) != 2 {
		t.Fatalf("Cross-Exchange selected shadow create=%#v error=%v", created, err)
	}
	claim, found, err := runtimeStore.Claim(ctx)
	if err != nil || !found || claim.ID != created.Id || claim.StrategyID != "cross-exchange-arbitrage-1-0-0" ||
		claim.StrategyVersion != "cross-exchange-arbitrage@1.0.0" || len(claim.MarketScopes) != 2 ||
		len(claim.VenueAccountIDs) != 2 || claim.VenueAccountIDs["binance"] == "" ||
		claim.VenueAccountIDs["bybit"] == "" || claim.VenueAccountIDs["binance"] == claim.VenueAccountIDs["bybit"] {
		t.Fatalf("Cross-Exchange shadow claim=%#v found=%t error=%v", claim, found, err)
	}
	if _, err = pool.Exec(ctx, `UPDATE shadow_sessions SET claim_expires_at=CURRENT_TIMESTAMP+interval '1 minute'
	      WHERE id=$1`, claim.ID); err != nil {
		t.Fatal(err)
	}
	return claim
}

func ownerConsoleCrossExchangeShadowResult(t *testing.T, claim PublicShadowClaim, markets []crossarb.MarketInput,
	coherent crossarb.CoherentViewInput,
	initialized map[string]map[domain.AssetSymbol]accounting.BalanceSnapshot,
	now time.Time,
) (crossarb.Input, backtest.EventResult) {
	t.Helper()
	input := ownerConsoleCrossExchangeRecordedInput(t, claim, markets, coherent, initialized, now)
	before := ownerConsoleCrossExchangeAvailableBalances(initialized)
	prepared, projection, err := crossarb.AttachCleanRecordedReduction(input,
		"shadow/cross-exchange/"+claim.ID, before)
	if err != nil || projection == nil || prepared.Reduction == nil ||
		projection.Simulation.Outcome != crossarb.OutcomeBothFilled {
		t.Fatalf("Cross-Exchange shadow projection=%#v error=%v", projection, err)
	}
	transactions, err := ownerConsoleCrossExchangeTransactions(claim, prepared, projection)
	if err != nil {
		t.Fatal(err)
	}
	decisionPayload, _ := json.Marshal(risk.Decision{Action: risk.ActionApprove,
		ReasonCode: "approved", EffectiveState: risk.StateNormal, EvaluatedAt: now})
	planPayload, _ := json.Marshal(projection.Plan)
	simulationPayload, _ := json.Marshal(projection.Simulation)
	reductionPayload, _ := json.Marshal(ownerConsoleCrossExchangeReductionEvidence{Transactions: transactions})
	result := backtest.EventResult{Ordinal: prepared.Ordinal, Decision: decisionPayload,
		Orders: planPayload, ExecutionEvents: simulationPayload, Balances: reductionPayload}
	return prepared, result
}

func assertOwnerConsoleCrossExchangeUpdatedBalances(t *testing.T,
	initialized, updated map[string]map[domain.AssetSymbol]accounting.BalanceSnapshot,
	err error,
) {
	t.Helper()
	btc, _ := domain.ParseAssetSymbol("BTC")
	if err != nil || len(updated) != 2 ||
		updated["binance"][btc].Available.Compare(initialized["binance"][btc].Available) <= 0 ||
		updated["bybit"][btc].Available.Compare(initialized["bybit"][btc].Available) >= 0 {
		t.Fatalf("Cross-Exchange shadow persistence=%#v error=%v", updated, err)
	}
}

func assertOwnerConsoleCrossExchangeShadowEvidence(t *testing.T, ctx context.Context, pool *pgxpool.Pool, claim PublicShadowClaim) {
	t.Helper()
	var initializations, ownership, decisions, executions, journals int
	if err := pool.QueryRow(ctx, `SELECT
      (SELECT count(*) FROM shadow_cross_exchange_inventory_initializations WHERE session_id=$1),
      (SELECT count(*) FROM portfolio_ownership WHERE portfolio_id=$2 AND strategy_version_id='cross-exchange-arbitrage-1-0-0'),
      (SELECT count(*) FROM shadow_strategy_decision_evidence WHERE strategy_version_id='cross-exchange-arbitrage-1-0-0'),
      (SELECT count(*) FROM shadow_multileg_execution_evidence WHERE strategy_version_id='cross-exchange-arbitrage-1-0-0'),
      (SELECT count(*) FROM journal_transactions WHERE run_id=$1)`, claim.ID, claim.PortfolioID).
		Scan(&initializations, &ownership, &decisions, &executions, &journals); err != nil ||
		initializations != 2 || ownership != 2 || decisions != 1 || executions != 1 || journals <= 2 {
		t.Fatalf("Cross-Exchange evidence init=%d ownership=%d decisions=%d executions=%d journals=%d error=%v",
			initializations, ownership, decisions, executions, journals, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE shadow_cross_exchange_inventory_initializations
	      SET model_version='tampered' WHERE session_id=$1`, claim.ID); err == nil {
		t.Fatal("immutable Cross-Exchange inventory evidence was mutated")
	}
}

func ownerConsoleCrossExchangeRecordedMarkets(t *testing.T, now time.Time,
	feeVersion string,
) ([]crossarb.MarketInput, crossarb.CoherentViewInput) {
	t.Helper()
	btc, _ := domain.ParseAssetSymbol("BTC")
	usdt, _ := domain.ParseAssetSymbol("USDT")
	instrument, err := domain.NewSpotInstrument(btc, usdt)
	if err != nil {
		t.Fatal(err)
	}
	views := runtimecore.NewMarketViews()
	keys := make([]runtimecore.MarketKey, 0, 2)
	markets := make([]crossarb.MarketInput, 0, 2)
	for index, value := range []struct{ exchange, bid, ask string }{
		{"binance", "99", "100"}, {"bybit", "104", "105"},
	} {
		market, book := ownerConsoleCrossExchangeMarket(t, now, feeVersion, instrument, usdt,
			value.exchange, value.bid, value.ask, uint64(index+1))
		key := publishOwnerConsoleCrossExchangeMarket(t, views, book, market, now)
		keys = append(keys, key)
		markets = append(markets, market)
	}
	trigger := runtimecore.AsOfTrigger{MonotonicNanos: 200, IngestOrdinal: 100, UTC: now}
	view, err := views.CoherentAsOf(keys, trigger, runtimecore.InitialCoherentMarketDataCoherentPolicy())
	if err != nil {
		t.Fatal(err)
	}
	return markets, crossarb.CoherentViewInput{Identity: view.Identity(), Policy: view.Policy(),
		Trigger: view.Trigger(), Members: view.Members()}
}

func ownerConsoleCrossExchangeMarket(t *testing.T, now time.Time, feeVersion string,
	instrument domain.Instrument, feeAsset domain.AssetSymbol, exchange, bid, ask string, ordinal uint64,
) (crossarb.MarketInput, *marketdata.Book) {
	t.Helper()
	book, err := marketdata.NewBook(exchange, instrument, 20, 20, nil)
	if err != nil || book.BeginGeneration("owner_console-cross-"+exchange, 1) != nil {
		t.Fatalf("Cross-Exchange qualification book failed: %v", err)
	}
	observation := marketdata.Observation{
		ReceivedAt:   domain.EventTime{UTC: now, Sequence: ordinal*3 - 2},
		ProcessedAt:  domain.EventTime{UTC: now, Sequence: ordinal*3 - 1},
		PublishedAt:  domain.EventTime{UTC: now, Sequence: ordinal * 3},
		ConnectionID: "owner_console-cross-" + exchange, ConnectionGeneration: 1,
		SourceSequence: ordinal, IngestOrdinal: ordinal,
		ReceivedOffsetNanos: 100 + ordinal*10, ProcessedOffsetNanos: 101 + ordinal*10,
		PublishedOffsetNanos: 102 + ordinal*10,
	}
	snapshot := exchangecontracts.BookSnapshot{Exchange: exchangecontracts.ExchangeID(exchange),
		Instrument: instrument, LastSequence: ordinal, ReceivedAt: observation.ReceivedAt,
		Bids:           []exchangecontracts.PriceLevel{{Price: ownerConsoleCrossPrice(t, bid), Quantity: ownerConsoleCrossQuantity(t, "100")}},
		Asks:           []exchangecontracts.PriceLevel{{Price: ownerConsoleCrossPrice(t, ask), Quantity: ownerConsoleCrossQuantity(t, "100")}},
		RawPayloadHash: "sha256:owner_console-cross-" + exchange}
	if err = book.ReplaceSnapshot(snapshot, observation); err != nil {
		t.Fatal(err)
	}
	rules := arbitrage.InstrumentRules{Exchange: exchange,
		Metadata: domain.InstrumentMetadata{Instrument: instrument, Version: 1,
			EffectiveAt: now.Add(-time.Minute), PriceTick: ownerConsoleCrossPrice(t, "0.01"),
			QuantityStep:    ownerConsoleCrossQuantity(t, "0.000001"),
			MinimumQuantity: ownerConsoleCrossQuantity(t, "0.000001"), MinimumNotional: ownerConsoleCrossNotional(t, "0.01")},
		MaximumQuantity: ownerConsoleCrossQuantity(t, "1000"), Fee: arbitrage.FeeSchedule{
			Version: feeVersion, Rate: ownerConsoleCrossRate(t, "0.001"), Asset: feeAsset},
		Active: true, ObservedAt: now.Add(-time.Minute)}
	return crossarb.MarketInput{Snapshot: snapshot, Observation: observation, Rules: rules}, book
}

func publishOwnerConsoleCrossExchangeMarket(t *testing.T, views *runtimecore.MarketViews,
	book *marketdata.Book, market crossarb.MarketInput, now time.Time,
) runtimecore.MarketKey {
	t.Helper()
	view := book.View()
	key := runtimecore.MarketKey{Exchange: string(market.Snapshot.Exchange), Instrument: market.Snapshot.Instrument}
	if err := views.ActivateGeneration(key, view.Generation()); err != nil {
		t.Fatal(err)
	}
	coherent, err := marketdata.CoherentInput(view, exchangecontracts.ClockHealth{
		ObservedAt: now, Uncertainty: time.Millisecond, Eligible: true},
		"owner_console-cross-collector-"+string(market.Snapshot.Exchange), "qualification")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = views.Publish(coherent); err != nil {
		t.Fatal(err)
	}
	return key
}

func ownerConsoleCrossExchangeRecordedInput(t *testing.T, claim PublicShadowClaim, markets []crossarb.MarketInput,
	coherent crossarb.CoherentViewInput,
	balances map[string]map[domain.AssetSymbol]accounting.BalanceSnapshot,
	now time.Time,
) crossarb.Input {
	t.Helper()
	configuration, err := crossarb.ConfigurationFromReviewed(claim.Configuration.CrossExchange)
	if err != nil {
		t.Fatal(err)
	}
	inventory, feeBalances := ownerConsoleCrossExchangeInventory(t, claim, markets[0].Snapshot.Instrument.Base, balances)
	input := crossarb.Input{Ordinal: 11, LogicalTime: coherent.Trigger.MonotonicNanos, Now: now,
		Markets: markets, Coherent: coherent, Inventory: inventory, QuoteBudget: ownerConsoleCrossBalance(t, "10"),
		FeeBalances: feeBalances, Configuration: configuration, ConfigurationHash: claim.ConfigurationHash,
		InstrumentMetadataSetHash: strings.Repeat("5", 64), Restoration: ownerConsoleCrossExchangeRestoration(t)}
	offset := input.LogicalTime + 1
	input.Simulation = &crossarb.SimulationInput{Latency: crossarb.LatencyDistribution{
		Version: "fixed-one-nanosecond-v1", BuySamplesNanos: []uint64{1}, SellSamplesNanos: []uint64{1},
		VerificationNanos: 1, RetryNanos: 1, RecoveryNanos: 1}}
	for _, market := range markets {
		input.Simulation.Markets = append(input.Simulation.Markets, crossarb.TimedMarketInput{Offset: offset, Market: market})
		input.Simulation.Directives = append(input.Simulation.Directives, crossarb.TimedDirective{
			Exchange: string(market.Snapshot.Exchange), Phase: crossarb.PhaseArrival, Offset: offset,
			Directive: crossarb.LegDirective{State: execution.OrderFilled}})
	}
	return input
}

func ownerConsoleCrossExchangeInventory(t *testing.T, claim PublicShadowClaim, base domain.AssetSymbol,
	balances map[string]map[domain.AssetSymbol]accounting.BalanceSnapshot,
) ([]crossarb.VenueInventory, map[string]domain.Balance) {
	t.Helper()
	usdt, _ := domain.ParseAssetSymbol("USDT")
	totalBase, _ := domain.ParseBalance("0")
	totalUSDT, _ := domain.ParseBalance("0")
	var err error
	for _, exchange := range []string{"binance", "bybit"} {
		totalBase, err = totalBase.Add(balances[exchange][base].Available)
		if err == nil {
			totalUSDT, err = totalUSDT.Add(balances[exchange][usdt].Available)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	inventory := make([]crossarb.VenueInventory, 0, 2)
	feeBalances := make(map[string]domain.Balance, 2)
	for _, exchange := range []string{"binance", "bybit"} {
		inventory = append(inventory, crossarb.VenueInventory{Owner: "cross-exchange:" + claim.ConfigurationHash,
			Exchange: exchange, BaseAsset: base, OwnedBase: balances[exchange][base].Available,
			TotalEligibleBase: totalBase, OwnedUSDT: balances[exchange][usdt].Available,
			TotalEligibleUSDT: totalUSDT, Revision: balances[exchange][base].Revision})
		feeBalances[exchange+":USDT"] = balances[exchange][usdt].Available
	}
	return inventory, feeBalances
}

func ownerConsoleCrossExchangeRestoration(t *testing.T) crossarb.RestorationEconomics {
	t.Helper()
	return crossarb.RestorationEconomics{
		ModelVersion: "closed-inventory-cycle.v1", LatencyModelVersion: "fixed-one-nanosecond-v1",
		RecoveryModelVersion: "recovery-v1", InventoryShadowPriceVersion: "public-book-v1",
		ConcentrationModelVersion: "concentration-v1", LatencyDeterioration: ownerConsoleCrossMoney(t, "0.005"),
		RecoveryAllowance: ownerConsoleCrossMoney(t, "0.005"), MarginalInventoryReplacement: ownerConsoleCrossMoney(t, "0.005"),
		NaturalReversalCost: ownerConsoleCrossMoney(t, "0.005"), AdvisoryRebalancingCost: ownerConsoleCrossMoney(t, "0.005"),
		ExchangeConcentrationPenalty:  ownerConsoleCrossMoney(t, "0.005"),
		USDTVenueConcentrationPenalty: ownerConsoleCrossMoney(t, "0.005"), MaximumOneLegLoss: ownerConsoleCrossMoney(t, "0.01"),
		EstimatedRestorationDelayNanos: 25_000_000, NaturalReverseAvailable: true,
		AdvisoryRebalancingRequired: true}
}

func ownerConsoleCrossPrice(t *testing.T, value string) domain.Price {
	t.Helper()
	result, err := domain.ParsePrice(value)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func ownerConsoleCrossQuantity(t *testing.T, value string) domain.Quantity {
	t.Helper()
	result, err := domain.ParseQuantity(value)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func ownerConsoleCrossNotional(t *testing.T, value string) domain.Notional {
	t.Helper()
	result, err := domain.ParseNotional(value)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func ownerConsoleCrossRate(t *testing.T, value string) domain.Rate {
	t.Helper()
	result, err := domain.ParseRate(value)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func ownerConsoleCrossBalance(t *testing.T, value string) domain.Balance {
	t.Helper()
	result, err := domain.ParseBalance(value)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func ownerConsoleCrossMoney(t *testing.T, value string) domain.Money {
	t.Helper()
	result, err := domain.ParseMoney(value)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func assertOwnerConsoleBybitShadowSelection(t *testing.T, ctx context.Context, store *OwnerConsoleStore,
	runtimeStore *PublicShadowStore, principal authentication.Principal,
) {
	t.Helper()
	created, err := store.CreateRun(ctx, principal, "shadow-bybit-owner_console", generated.RunCreateRequest{
		StrategyId: "trend-following", StrategyVersion: "trend-following@1.0.0",
		Mode:       generated.RunCreateRequestModeShadow,
		Exchanges:  []generated.RunCreateRequestExchanges{generated.RunCreateRequestExchangesBybit},
		Instrument: "BTC/USDT", Preset: generated.LatestQualifiedInputs,
	})
	if err != nil || created.Exchanges == nil || len(*created.Exchanges) != 1 ||
		(*created.Exchanges)[0] != generated.RunResourceExchangesBybit || created.Instrument == nil ||
		*created.Instrument != "BTC/USDT" || created.Environment != generated.ProductionPublic {
		t.Fatalf("Bybit shadow create=%#v error=%v", created, err)
	}
	claim, found, err := runtimeStore.Claim(ctx)
	if err != nil || !found || claim.ID != created.Id || claim.ExchangeID != "bybit" ||
		claim.InstrumentID != "BTCUSDT" {
		t.Fatalf("Bybit shadow claim=%#v found=%t error=%v", claim, found, err)
	}
	projected, err := store.Run(ctx, created.Id)
	if err != nil || projected.Exchanges == nil || len(*projected.Exchanges) != 1 ||
		(*projected.Exchanges)[0] != generated.RunResourceExchangesBybit || projected.Instrument == nil ||
		*projected.Instrument != "BTC/USDT" || projected.Environment != generated.ProductionPublic {
		t.Fatalf("Bybit unified run projection=%#v error=%v", projected, err)
	}
	if err = runtimeStore.Fail(ctx, claim.ID, "qualification_complete"); err != nil {
		t.Fatal(err)
	}
}

func assertOwnerConsoleMeanReversionShadowSelection(t *testing.T, ctx context.Context, pool *pgxpool.Pool,
	store *OwnerConsoleStore, runtimeStore *PublicShadowStore, principal authentication.Principal,
) {
	t.Helper()
	claim := ownerConsoleMeanReversionShadowClaim(t, ctx, pool, store, runtimeStore, principal)
	now := time.Now().UTC().Truncate(time.Microsecond)
	metadata := ownerConsoleMeanReversionMetadata(t, ctx, pool, runtimeStore, now)
	assertOwnerConsoleMeanReversionActivity(t, ctx, store, runtimeStore, claim, now)
	assertOwnerConsoleMeanReversionDecision(t, ctx, pool, store, runtimeStore, claim, metadata.ID, now)
	if err := runtimeStore.Fail(ctx, claim.ID, "qualification_complete"); err != nil {
		t.Fatal(err)
	}
}

func ownerConsoleMeanReversionShadowClaim(t *testing.T, ctx context.Context, pool *pgxpool.Pool,
	store *OwnerConsoleStore, runtimeStore *PublicShadowStore, principal authentication.Principal,
) PublicShadowClaim {
	t.Helper()
	created, err := store.CreateRun(ctx, principal, "shadow-mean-bybit-owner_console", generated.RunCreateRequest{
		StrategyId: "mean-reversion", StrategyVersion: "mean-reversion@1.0.0",
		Mode:       generated.RunCreateRequestModeShadow,
		Exchanges:  []generated.RunCreateRequestExchanges{generated.RunCreateRequestExchangesBybit},
		Instrument: "ETH/USDT", Preset: generated.LatestQualifiedInputs,
	})
	if err != nil || created.StrategyId != "mean-reversion" || created.Exchanges == nil ||
		len(*created.Exchanges) != 1 || (*created.Exchanges)[0] != generated.RunResourceExchangesBybit ||
		created.Instrument == nil || *created.Instrument != "ETH/USDT" {
		t.Fatalf("Mean Reversion shadow create=%#v error=%v", created, err)
	}
	claim, found, err := runtimeStore.Claim(ctx)
	if err != nil || !found || claim.ID != created.Id || claim.StrategyID != "mean-reversion-1-0-0" ||
		claim.StrategyVersion != "mean-reversion@1.0.0" || claim.ExchangeID != "bybit" ||
		claim.InstrumentID != "ETHUSDT" || !claim.MarketScopeRequired || len(claim.MarketScopes) != 1 ||
		claim.MarketScopes[0].ExchangeID != "bybit" || claim.MarketScopes[0].InstrumentID != "ETHUSDT" ||
		claim.MarketScopes[0].Purpose != "primary" {
		t.Fatalf("Mean Reversion shadow claim=%#v found=%t error=%v", claim, found, err)
	}
	var scopeCount int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM shadow_session_market_scopes WHERE session_id=$1`,
		claim.ID).Scan(&scopeCount); err != nil || scopeCount != 1 {
		t.Fatalf("Mean Reversion exact market scope count=%d error=%v", scopeCount, err)
	}
	if _, err = pool.Exec(ctx, `UPDATE shadow_sessions SET claim_expires_at=CURRENT_TIMESTAMP+interval '1 minute'
		WHERE id=$1`, claim.ID); err != nil {
		t.Fatal(err)
	}
	return claim
}

func ownerConsoleMeanReversionMetadata(t *testing.T, ctx context.Context, pool *pgxpool.Pool,
	runtimeStore *PublicShadowStore, now time.Time,
) PublicShadowMetadataEvidence {
	t.Helper()
	base, _ := domain.ParseAssetSymbol("ETH")
	quote, _ := domain.ParseAssetSymbol("USDT")
	instrument, _ := domain.NewSpotInstrument(base, quote)
	priceTick, _ := domain.ParsePrice("0.01")
	quantityStep, _ := domain.ParseQuantity("0.0001")
	minimumQuantity, _ := domain.ParseQuantity("0.0001")
	maximumQuantity, _ := domain.ParseQuantity("230")
	minimumNotional, _ := domain.ParseNotional("5")
	publicRecord := exchangecontracts.InstrumentRecord{Exchange: "bybit", NativeSymbol: "ETHUSDT",
		NativeStatus: "Trading", MaximumQuantity: maximumQuantity, RawPayloadHash: strings.Repeat("a", 64),
		Metadata: domain.InstrumentMetadata{Instrument: instrument, Version: 1, EffectiveAt: now,
			PriceTick: priceTick, QuantityStep: quantityStep, MinimumQuantity: minimumQuantity,
			MinimumNotional: minimumNotional}}
	metadataEvidence, err := runtimeStore.RegisterPublicInstrument(ctx, publicRecord)
	if err != nil || metadataEvidence.ID == "" || metadataEvidence.MaximumQuantity.String() != "230" {
		t.Fatalf("exact public instrument metadata=%#v error=%v", metadataEvidence, err)
	}
	replayedMetadata, err := runtimeStore.RegisterPublicInstrument(ctx, publicRecord)
	if err != nil || replayedMetadata.ID != metadataEvidence.ID {
		t.Fatalf("exact public instrument metadata replay=%#v error=%v", replayedMetadata, err)
	}
	var persistedMaximum string
	if err = pool.QueryRow(ctx, `SELECT maximum_quantity::text FROM instrument_metadata_versions WHERE id=$1`,
		metadataEvidence.ID).Scan(&persistedMaximum); err != nil {
		t.Fatalf("persisted public maximum quantity=%q error=%v", persistedMaximum, err)
	}
	persistedMaximumQuantity, err := domain.ParseQuantity(persistedMaximum)
	if err != nil || persistedMaximumQuantity.Compare(maximumQuantity) != 0 {
		t.Fatalf("persisted public maximum quantity=%q error=%v", persistedMaximum, err)
	}
	return metadataEvidence
}

func assertOwnerConsoleMeanReversionActivity(t *testing.T, ctx context.Context,
	store *OwnerConsoleStore, runtimeStore *PublicShadowStore, claim PublicShadowClaim, now time.Time,
) {
	t.Helper()
	nextEvaluation := now.Add(time.Hour)
	waitingReason := "No Mean Reversion decision is due yet; waiting for the next finalized one-hour candle."
	if err := runtimeStore.RecordActivity(ctx, claim, PublicShadowActivity{
		State: "waiting", ReasonCode: "waiting_for_finalized_1h_candle", Summary: waitingReason,
		NextEvaluationAt: &nextEvaluation,
		TriggerCondition: "After the next finalized one-hour signal candle with a healthy four-hour regime view.",
		ObservedAt:       now,
		Inputs: []PublicShadowInputHealth{{ExchangeID: "bybit", InstrumentID: "ETHUSDT",
			State: "HEALTHY", Reason: "The selected production-public order book is healthy and fresh.",
			Fresh: true, BookVersion: 7, Age: 25 * time.Millisecond, ObservedAt: now}},
	}); err != nil {
		t.Fatalf("Mean Reversion shadow activity rejected: %v", err)
	}
	activityProjection, err := store.Shadow(ctx, claim.ID)
	if err != nil || activityProjection.ActivityState != generated.ShadowSessionResourceActivityStateWaiting ||
		activityProjection.WaitingReasonCode != "waiting_for_finalized_1h_candle" ||
		activityProjection.WaitingReason != waitingReason || activityProjection.NextEvaluationAt == nil ||
		!time.Time(*activityProjection.NextEvaluationAt).Equal(nextEvaluation) ||
		len(activityProjection.InputHealth) != 1 || activityProjection.InputHealth[0].Exchange != "bybit" ||
		activityProjection.InputHealth[0].Instrument != "ETH/USDT" || !activityProjection.InputHealth[0].Fresh {
		t.Fatalf("Mean Reversion shadow activity projection=%#v error=%v", activityProjection, err)
	}
	unifiedProjection, err := store.Run(ctx, claim.ID)
	if err != nil || unifiedProjection.WaitingReason == nil || *unifiedProjection.WaitingReason != waitingReason ||
		unifiedProjection.NextEvaluationAt == nil ||
		!time.Time(*unifiedProjection.NextEvaluationAt).Equal(nextEvaluation) {
		t.Fatalf("Mean Reversion unified waiting projection=%#v error=%v", unifiedProjection, err)
	}
}

func assertOwnerConsoleMeanReversionDecision(t *testing.T, ctx context.Context, pool *pgxpool.Pool,
	store *OwnerConsoleStore, runtimeStore *PublicShadowStore, claim PublicShadowClaim,
	metadataID string, now time.Time,
) {
	t.Helper()
	decisionID, err := domain.NewDecisionID("mean-shadow-decision-owner_console")
	if err != nil {
		t.Fatal(err)
	}
	input := meanreversion.Input{Ordinal: 1, LogicalTime: 1, Now: now,
		Evidence: meanreversion.InputEvidence{PrimaryCandleViewID: "bybit-ETHUSDT-1h",
			PrimaryCandleViewRevision: 1, HigherCandleViewID: "bybit-ETHUSDT-4h",
			HigherCandleViewRevision: 1, MarketViewID: "bybit-ETHUSDT-book", MarketViewRevision: 1,
			CoherentViewID: strings.Repeat("3", 64), CoherentVersionVectorHash: strings.Repeat("3", 64),
			InstrumentMetadataID: metadataID,
			CorrelationID:        claim.ID, CausationID: "mean-shadow-candle-owner_console"}}
	decision := meanreversion.Decision{ID: decisionID, Ordinal: 1, Action: meanreversion.ActionNone,
		ReasonCode: meanreversion.ReasonWarmUp, Explanation: meanreversion.Explanation{ReasonCode: meanreversion.ReasonWarmUp}}
	decisionPayload, _ := json.Marshal(decision)
	result := backtest.EventResult{Ordinal: 1, Decision: decisionPayload, Orders: json.RawMessage("[]"),
		ExecutionEvents: json.RawMessage("[]"), Balances: json.RawMessage(`{"Balances":{},"Positions":[],"Revision":1}`)}
	if err = runtimeStore.RecordMeanReversionShadowDecision(ctx, claim, input, result); err != nil {
		t.Fatalf("Mean Reversion shadow evidence rejected: %v", err)
	}
	var evidenceCount int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM shadow_strategy_decision_evidence evidence
		JOIN decisions decision ON decision.id=evidence.decision_id
		WHERE decision.run_id=$1 AND evidence.input_kind='mean_reversion_input'`, claim.ID).Scan(&evidenceCount); err != nil || evidenceCount != 1 {
		t.Fatalf("Mean Reversion semantic evidence count=%d error=%v", evidenceCount, err)
	}
	shadow, err := store.Shadow(ctx, claim.ID)
	if err != nil || shadow.StrategyVersion != "mean-reversion@1.0.0" || shadow.Decisions == nil ||
		len(*shadow.Decisions) != 1 || (*shadow.Decisions)[0].ReasonCode != meanreversion.ReasonWarmUp {
		t.Fatalf("Mean Reversion shadow projection=%#v error=%v", shadow, err)
	}
}

func assertPublicShadowImmutability(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var err error
	if _, err = pool.Exec(ctx, `UPDATE command_requests SET reason='tampered' WHERE id=(SELECT id FROM command_requests ORDER BY created_at LIMIT 1)`); err == nil {
		t.Fatal("immutable durable command mutated")
	}
	if _, err = pool.Exec(ctx, `DELETE FROM audit_events WHERE event_type='owner_bootstrapped'`); err == nil {
		t.Fatal("immutable bootstrap audit deleted")
	}
	if _, err = pool.Exec(ctx, `UPDATE shadow_session_activity_observations SET summary='tampered'
		WHERE session_id=(SELECT session_id FROM shadow_session_activity_observations LIMIT 1)`); err == nil {
		t.Fatal("immutable shadow activity mutated")
	}
	if _, err = pool.Exec(ctx, `DELETE FROM shadow_session_input_health_observations
		WHERE session_id=(SELECT session_id FROM shadow_session_input_health_observations LIMIT 1)`); err == nil {
		t.Fatal("immutable shadow input health deleted")
	}
	var unsafe int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM shadow_sessions
		WHERE public_exchange NOT IN ('binance-production-public','bybit-production-public')
		   OR public_exchange<>exchange_id || '-production-public' OR NOT simulation_only`).Scan(&unsafe); err != nil || unsafe != 0 {
		t.Fatalf("unsafe shadow records = %d %v", unsafe, err)
	}
}

func assertOwnerConsoleSessionLimitAndHistoricalAuthorizationGuard(t *testing.T, ctx context.Context, pool *pgxpool.Pool, service *authentication.Service, clock *domain.ReplayClock, first authentication.LoginResult, password string, now time.Time) {
	t.Helper()
	for index := 0; index < 5; index++ {
		if err := clock.Advance(time.Second); err != nil {
			t.Fatal(err)
		}
		if _, err := service.Login(ctx, "owner@example.test", password, "127.0.0.1", "extra-login"); err != nil {
			t.Fatalf("extra login %d failed: %v", index, err)
		}
	}
	var active int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE user_id=$1 AND revoked_at IS NULL`, first.Principal.UserID).Scan(&active); err != nil || active != authentication.MaximumSessions {
		t.Fatalf("active session cap = %d %v", active, err)
	}
	if _, err := service.Authenticate(ctx, first.SessionToken); !errors.Is(err, authentication.ErrSessionInvalid) {
		t.Fatalf("oldest excess session remained active: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO user_roles(user_id,role_id,granted_at) VALUES($1,'viewer',$2)`, first.Principal.UserID, now); err == nil || !strings.Contains(err.Error(), "legacy_authorization_records_are_historical") {
		t.Fatalf("historical role mutation was not rejected: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE user_id=$1 AND revoked_at IS NULL`, first.Principal.UserID).Scan(&active); err != nil || active != authentication.MaximumSessions {
		t.Fatalf("rejected historical mutation changed owner sessions = %d %v", active, err)
	}
}
