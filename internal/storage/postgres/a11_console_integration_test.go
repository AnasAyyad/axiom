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

	"axiom/internal/api/console"
	"axiom/internal/api/generated"
	"axiom/internal/authentication"
	"axiom/internal/backtest"
	"axiom/internal/domain"
	"axiom/internal/replay"
	"axiom/internal/research"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestA11PostgresAuthenticationCommandsAndConsoleQualification(t *testing.T) {
	ctx, cancel, pool, now := a11QualificationDatabase(t)
	defer cancel()
	defer pool.Close()
	clock, _ := domain.NewReplayClock(now)
	authService, password, login := a11QualificationAuthentication(t, ctx, pool, clock)
	seedA10References(t, ctx, pool)
	repository, _ := NewA10Repository(pool)
	if err := repository.Register(ctx, a10RegistrationFixture()); err != nil {
		t.Fatal(err)
	}
	seedA11RegisteredReport(t, ctx, pool, now)
	seedA11RuntimeEvidence(t, ctx, pool, now)
	consoleStore, err := NewA11ConsoleStore(pool, []byte(strings.Repeat("s", 32)), clock)
	if err != nil {
		t.Fatal(err)
	}
	trendStatus, err := consoleStore.Trend(ctx)
	if err != nil || trendStatus.Version != generated.TrendV1a1 || len(trendStatus.Parameters) != 16 {
		t.Fatalf("registered Trend projection = %#v %v", trendStatus, err)
	}
	assertA11StablePagination(t, ctx, pool, consoleStore, now)
	assertA11IncidentReplayWindow(t, ctx, pool, consoleStore)
	assertA11RiskRecovery(t, ctx, consoleStore, login.Principal)
	assertA11DurableJobs(t, ctx, pool, consoleStore, login.Principal)
	assertA11WorkerLeasesAndRecovery(t, ctx, pool, consoleStore, login.Principal, clock)
	assertA11ShadowAndAudit(t, ctx, pool, consoleStore, login.Principal, clock)
	assertA11ResumableStream(t, ctx, pool, consoleStore, login.Principal)
	assertA11SessionLimitAndPrivilegeRotation(t, ctx, pool, authService, clock, login, password, now)
}

func assertA11WorkerLeasesAndRecovery(t *testing.T, ctx context.Context, pool *pgxpool.Pool,
	consoleStore *A11ConsoleStore, principal authentication.Principal, clock *domain.ReplayClock) {
	t.Helper()
	materialize := func(_ context.Context, id string, kind string, payload json.RawMessage) (backtest.JobClaim, error) {
		if kind != "backtest" || !json.Valid(payload) {
			return backtest.JobClaim{}, errors.New("invalid qualification job")
		}
		return a11QualificationClaim(id, kind), nil
	}
	first, err := NewA11JobStore(pool, "qualification-worker-one", clock, materialize)
	if err != nil {
		t.Fatal(err)
	}
	claim, ok, err := first.Claim(ctx)
	if err != nil || !ok || claim.ID == "" {
		t.Fatalf("durable claim = %#v %t %v", claim, ok, err)
	}
	result := a11QualificationJobResult()
	canonical, _ := json.Marshal(result)
	if err = first.Complete(ctx, claim.ID, result, canonical); err != nil {
		t.Fatalf("durable completion failed: %v", err)
	}
	assertA11CanonicalOutputs(t, ctx, pool, claim.Manifest.RunID.Value())
	assertA11CompletedJob(t, ctx, consoleStore, claim, result)
	failed, ok, err := first.Claim(ctx)
	if err != nil || !ok || first.Fail(ctx, failed.ID, "qualification_failure") != nil {
		t.Fatalf("durable failure = %#v %t %v", failed, ok, err)
	}
	expired, ok, err := first.Claim(ctx)
	if err != nil || !ok {
		t.Fatalf("crash lease claim = %#v %t %v", expired, ok, err)
	}
	if err = clock.Advance(a11JobLease + time.Second); err != nil {
		t.Fatal(err)
	}
	second, _ := NewA11JobStore(pool, "qualification-worker-two", clock, materialize)
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
	assertA11PauseDuringClaimMaterialization(t, ctx, pool, consoleStore, principal, clock)
}

func assertA11PauseDuringClaimMaterialization(t *testing.T, ctx context.Context, pool *pgxpool.Pool,
	consoleStore *A11ConsoleStore, principal authentication.Principal, clock *domain.ReplayClock) {
	t.Helper()
	speed := generated.Maximum
	request := generated.ReplayJobRequest{ConfigurationId: "configuration-a10", DatasetId: "dataset-a7-formal-pending",
		ResearchGenerationId: "generation-a10-1", RootSeedHash: strings.Repeat("8", 64),
		StrategyVersion: generated.ReplayJobRequestStrategyVersionTrendV1a1, Speed: &speed}
	job, err := consoleStore.CreateJob(ctx, principal, "replay", "replay-claim-race-a11", request)
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
		return a11QualificationClaim(id, kind), updateErr
	}
	store, err := NewA11JobStore(pool, "qualification-claim-race", clock, materialize)
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

func assertA11CanonicalOutputs(t *testing.T, ctx context.Context, pool *pgxpool.Pool, runID string) {
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

func assertA11CompletedJob(t *testing.T, ctx context.Context, consoleStore *A11ConsoleStore, claim backtest.JobClaim, result backtest.CanonicalResult) {
	t.Helper()
	projection, err := consoleStore.Job(ctx, claim.ID, "")
	if err != nil || projection.State != generated.JobResourceState("SUCCEEDED") || projection.Result == nil || projection.Result.ResultHash != result.ResultHash {
		t.Fatalf("completed projection = %#v %v", projection, err)
	}
	if projection.RegisteredReport == nil || projection.RegisteredReport.ResearchGenerationId != "generation-a10-1" ||
		len(projection.RegisteredReport.Benchmarks) != 3 || len(projection.RegisteredReport.Stress) != 6 ||
		len(projection.RegisteredReport.Capacity) != 2 || projection.RegisteredReport.ConfidenceLabel != "local_tier_b" {
		t.Fatalf("registered report projection = %#v", projection.RegisteredReport)
	}
	inspection, err := consoleStore.replayInspection(ctx, claim.Manifest.RunID.Value(), "1")
	if err != nil || inspection == nil || inspection.Ordinal != "1" || inspection.EventCount != "1" ||
		inspection.EventHash == "" || inspection.CanonicalDecision != `{"outcome":"rejected"}` {
		t.Fatalf("replay inspection = %#v %v", inspection, err)
	}
}

func a11QualificationClaim(id, kind string) backtest.JobClaim {
	hash := strings.Repeat("d", 64)
	runID, _ := domain.NewRunID(id)
	return backtest.JobClaim{TimingMode: replay.MaximumTiming, Acceleration: 1,
		Manifest: backtest.RunManifest{RunID: runID, Mode: kind,
			ResearchGenerationID: "generation-a10-1",
			CodeCommit:           strings.Repeat("c", 40), Build: backtest.CurrentBuildIdentity([]string{"trimpath"}, hash, hash),
			Dataset: backtest.DatasetDescriptor{DatasetID: "recorder-dataset-a11", ManifestHash: hash, Revision: 1,
				SourceCommit: strings.Repeat("c", 40), SchemaVersion: "dataset.v1", ParserVersion: "parser-v1",
				NormalizationVersion: "normalizer-v1", SegmentHashes: []string{hash}, RecordCount: 1,
				Complete: true, Confidence: backtest.ConfidenceB}, ConfigurationHash: hash, Seed: strings.Repeat("8", 64),
			SchedulerVersion: "scheduler-v1", SerializationVersion: "canonical-json-v1",
			Models: backtest.ModelNamespace{ID: "namespace-a11", MarketContext: "production-public",
				LiquidityDomain: "combined-a11", FeeDomain: "fee-a10", LatencyDomain: "latency-a10", FillDomain: "fill-a10"},
			StartingBalanceHash: hash}}
}

func a11QualificationJobResult() backtest.CanonicalResult {
	metrics := backtest.Metrics{TotalNetReturn: "0", MaximumDrawdown: "0", CurrentDrawdown: "0",
		SharpeRatio: "0", SortinoRatio: "0", ProfitFactor: "0", Expectancy: "0", WinRate: "0",
		Turnover: "0", Exposure: "0"}
	return backtest.CanonicalResult{ManifestHash: strings.Repeat("b", 64), Confidence: backtest.ConfidenceB,
		Events: []backtest.EventResult{{Ordinal: 1, Decision: json.RawMessage(`{"outcome":"rejected"}`),
			Orders: json.RawMessage(`[]`), ExecutionEvents: json.RawMessage(`[]`), Balances: json.RawMessage(`{"USDT":"1000"}`)}},
		Metrics: metrics, ResultHash: strings.Repeat("a", 64)}
}

func seedA11RegisteredReport(t *testing.T, ctx context.Context, pool *pgxpool.Pool, now time.Time) {
	t.Helper()
	interval, err := research.BlockBootstrapMean([]string{"0.01", "0", "0.02", "-0.01"}, 2, 100, "a11-suite-seed")
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
	manifest, err := research.BuildReport(a11RegisteredReportInput(start, now, interval, result, stress))
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
	  viability_disposition,disclaimer_policy,created_at) VALUES('registered-report-a11','generation-a10-1',$1,$2,$3,$4,$5,$6,$7,$8,
	  'no_production_profitability_claim',$9)`, manifest.ManifestHash, strings.Repeat("f", 64), canonical, runReferences,
		manifest.ConfidenceLabel, manifest.PlatformCorrectness, manifest.StrategyEvidence, manifest.ViabilityDisposition, manifest.CreatedAt); err != nil {
		t.Fatal(err)
	}
}

func a11RegisteredReportInput(start, now time.Time, interval research.ConfidenceInterval,
	result func(string) research.ResultSlice, stress []research.ResultSlice) research.ReportInput {
	return research.ReportInput{
		ResearchGenerationID: "generation-a10-1", Hypothesis: "Strict breakouts may retain positive net expectancy after costs.",
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

func a11QualificationDatabase(t *testing.T) (context.Context, context.CancelFunc, *pgxpool.Pool, time.Time) {
	t.Helper()
	dsn := os.Getenv("AXIOM_A11_TEST_DSN")
	if dsn == "" {
		t.Skip("AXIOM_A11_TEST_DSN is not set")
	}
	configuration, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(configuration.ConnConfig.Database, "_a11_test") {
		t.Fatal("A11 integration requires a dedicated database ending _a11_test")
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
		t.Fatalf("A11 migrations = %d %v", applied, applyErr)
	}
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	return ctx, cancel, pool, now
}

func a11QualificationAuthentication(t *testing.T, ctx context.Context, pool *pgxpool.Pool, clock *domain.ReplayClock) (*authentication.Service, string, authentication.LoginResult) {
	t.Helper()
	now := clock.Now().UTC
	authStore, err := NewA11AuthenticationStore(pool)
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

	login, err := authService.Login(ctx, "OWNER@example.test", password, "127.0.0.1", "login-a11")
	if err != nil {
		t.Fatal(err)
	}
	assertA11SecretsAreHashed(t, ctx, pool, password, passwordHash, login)
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

func assertA11ResumableStream(t *testing.T, ctx context.Context, pool *pgxpool.Pool, store *A11ConsoleStore, principal authentication.Principal) {
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
      count(*) FILTER (WHERE stream NOT IN ('system','exchange','portfolio','risk','trend','job','shadow','incident','alert','order','fill')),
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

func assertA11StablePagination(t *testing.T, ctx context.Context, pool *pgxpool.Pool, store *A11ConsoleStore, now time.Time) {
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

func assertA11IncidentReplayWindow(t *testing.T, ctx context.Context, pool *pgxpool.Pool, store *A11ConsoleStore) {
	t.Helper()
	detail, err := store.Incident(ctx, "incident-z", false)
	if err != nil || detail.ReplayWindow.DatasetId != "dataset-a7-formal-pending" ||
		detail.ReplayWindow.FirstOrdinal != "1" || detail.ReplayWindow.LastOrdinal != "1" {
		t.Fatalf("qualified incident replay window = %#v %v", detail.ReplayWindow, err)
	}
	incidentID, first, last := "incident-z", generated.Revision("1"), generated.Revision("1")
	request := generated.ReplayJobRequest{ConfigurationId: "configuration-a10", DatasetId: "dataset-a7-formal-pending",
		ResearchGenerationId: "generation-a10-1",
		RootSeedHash:         strings.Repeat("8", 64), StrategyVersion: generated.ReplayJobRequestStrategyVersionTrendV1a1,
		IncidentId: &incidentID, FirstOrdinal: &first, LastOrdinal: &last}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err = validateA11IncidentReplay(ctx, tx, request); err != nil {
		t.Fatalf("exact incident replay rejected: %v", err)
	}
	changed := generated.Revision("2")
	request.LastOrdinal = &changed
	if err = validateA11IncidentReplay(ctx, tx, request); !errors.Is(err, console.ErrPrecondition) {
		t.Fatalf("altered incident replay accepted: %v", err)
	}
}

func assertA11SecretsAreHashed(t *testing.T, ctx context.Context, pool *pgxpool.Pool, password, passwordHash string, login authentication.LoginResult) {
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

func seedA11RuntimeEvidence(t *testing.T, ctx context.Context, pool *pgxpool.Pool, now time.Time) {
	t.Helper()
	hash := strings.Repeat("7", 64)
	statements := []struct {
		sql  string
		args []any
	}{
		{`UPDATE dataset_manifests SET dataset_kind='decision_inputs' WHERE id='dataset-a7-formal-pending'`, nil},
		{`INSERT INTO runs(id,mode,configuration_id,strategy_version_id,dataset_id,root_seed_hash,reproducibility_hash,state,created_at)
          VALUES('run-a11-shadow','shadow','configuration-a10','trend-v1a-1','dataset-a7-formal-pending',$1,$1,'created',$2)`, []any{hash, now}},
		{`INSERT INTO portfolios(id,name,reporting_asset,created_at) VALUES('portfolio-a11','A11 virtual portfolio','USDT',$1)`, []any{now}},
		{`INSERT INTO model_namespaces(id,namespace_hash,market_context,liquidity_domain,fee_model_id,latency_model_id,
		  fill_model_id,price_model_hash,canonical_payload,created_at)
		  VALUES('namespace-a11',$1,'production-public','combined-a11','fixed-bps-v1','fixed-zero-v1','fill-v1',$1,'{}',$2)`, []any{hash, now}},
		{`INSERT INTO virtual_accounts(id,portfolio_id,run_id,name,created_at) VALUES('account-a11','portfolio-a11','run-a11-shadow','main',$1)`, []any{now}},
		{`INSERT INTO virtual_balances(account_id,asset_symbol,available,reserved,revision,updated_at)
          VALUES('account-a11','USDT',1000,0,1,$1),('account-a11','BTC',0,0,1,$1)`, []any{now}},
		{`INSERT INTO instrument_metadata_versions(id,exchange_id,instrument_id,version,price_tick,quantity_step,
		  minimum_quantity,minimum_notional,effective_at,recorded_at)
		  VALUES('metadata-a11-binance','binance','instrument-a10',1,0.01,0.00001,0.00001,10,$1,$1),
		        ('metadata-a11-binance-eth','binance','instrument-eth-a10',1,0.01,0.00001,0.00001,10,$1,$1)`, []any{now}},
		{`INSERT INTO market_data_segments(id,recorder_session,exchange_id,instrument_id,event_type,schema_version,parser_version,normalization_version,compression,path,checksum,ordered_content_hash,record_count,first_ordinal,last_ordinal,started_at,ended_at,state,finalized_at)
		  VALUES('segment-a11','recorder-a11','binance','instrument-a10','book','market-wire.v1','parser-a11','normalizer-a11','zstd','a11/segment.zst',$1,$1,1,1,1,$2,$2,'ready',$2)`, []any{hash, now}},
		{`INSERT INTO market_data_segments(id,recorder_session,exchange_id,instrument_id,event_type,schema_version,parser_version,normalization_version,compression,path,checksum,ordered_content_hash,record_count,first_ordinal,last_ordinal,started_at,ended_at,state,finalized_at)
		  VALUES('segment-a11-candle','recorder-a11','binance','instrument-a10','candle','market-wire.v1','parser-a11','normalizer-a11','zstd','a11/candle.zst',$1,$1,1,2,2,$2,$2,'ready',$2)`, []any{hash, now}},
		{`INSERT INTO dataset_segments(dataset_id,segment_id,ordinal) VALUES('dataset-a7-formal-pending','segment-a11',0)`, nil},
		{`INSERT INTO startup_recovery_attempts(id,run_id,state,build_hash,configuration_hash,started_at,completed_at)
          VALUES('recovery-a11','run-a11-shadow','ready_paused',$1,$1,$2,$2)`, []any{hash, now}},
	}
	for index, statement := range statements {
		if _, err := pool.Exec(ctx, statement.sql, statement.args...); err != nil {
			t.Fatalf("A11 seed %d failed: %v", index+1, err)
		}
	}
	stages := []string{"database_prerequisites", "fenced_ownership", "build_safety_manifest", "configuration_graph",
		"schema_and_durability", "checkpoint_and_cursor", "protected_state", "committed_event_replay",
		"journal_and_projections", "simulator_reconciliation", "recorder_segments", "public_market_state",
		"operational_invariants", "administrative_readiness"}
	for ordinal, stage := range stages {
		if _, err := pool.Exec(ctx, `INSERT INTO startup_recovery_evidence(attempt_id,ordinal,stage,evidence_hash,recorded_at) VALUES('recovery-a11',$1,$2,$3,$4)`, ordinal, stage, hash, now); err != nil {
			t.Fatalf("recovery evidence %d failed: %v", ordinal, err)
		}
	}
}

func assertA11RiskRecovery(t *testing.T, ctx context.Context, store *A11ConsoleStore, principal authentication.Principal) {
	t.Helper()
	risk, err := store.Risk(ctx)
	if err != nil || risk.State != generated.RiskStatusState("PAUSED") || risk.Revision != "1" || !risk.RecoveryReady {
		t.Fatalf("initial fail-closed risk = %#v %v", risk, err)
	}
	stale := generated.RevisionCommandRequest{ExpectedRevision: "2", Reason: "qualification recovery"}
	if _, err = store.RiskCommand(ctx, principal, "resume", "risk-resume-stale-a11", stale); !errors.Is(err, console.ErrConflict) {
		t.Fatalf("stale recovery revision accepted: %v", err)
	}
	request := generated.RevisionCommandRequest{ExpectedRevision: "1", Reason: "qualification recovery"}
	accepted, err := store.RiskCommand(ctx, principal, "resume", "risk-resume-valid-a11", request)
	if err != nil || accepted.State != generated.CommandAcceptedStateApplied {
		t.Fatalf("safe recovery rejected: %#v %v", accepted, err)
	}
	replayed, err := store.RiskCommand(ctx, principal, "resume", "risk-resume-valid-a11", request)
	if err != nil || replayed.Id != accepted.Id {
		t.Fatalf("idempotent risk command changed: %#v %v", replayed, err)
	}
	conflict := request
	conflict.Reason = "different qualification recovery"
	if _, err = store.RiskCommand(ctx, principal, "resume", "risk-resume-valid-a11", conflict); !errors.Is(err, console.ErrIdempotencyConflict) {
		t.Fatalf("idempotency conflict accepted: %v", err)
	}
	risk, err = store.Risk(ctx)
	if err != nil || risk.State != generated.RiskStatusState("NORMAL") || risk.Revision != "2" {
		t.Fatalf("resumed risk projection = %#v %v", risk, err)
	}
}

func assertA11DurableJobs(t *testing.T, ctx context.Context, pool *pgxpool.Pool, store *A11ConsoleStore, principal authentication.Principal) {
	t.Helper()
	request := generated.OfflineJobRequest{ConfigurationId: "configuration-a10", DatasetId: "dataset-a7-formal-pending",
		ResearchGenerationId: "generation-a10-1",
		RootSeedHash:         strings.Repeat("8", 64), StrategyVersion: generated.OfflineJobRequestStrategyVersionTrendV1a1}
	first, err := store.CreateJob(ctx, principal, "backtest", "backtest-idempotent-a11", request)
	if err != nil || first.State != generated.JobResourceState("QUEUED") {
		t.Fatalf("backtest create = %#v %v", first, err)
	}
	replayed, err := store.CreateJob(ctx, principal, "backtest", "backtest-idempotent-a11", request)
	if err != nil || replayed.Id != first.Id {
		t.Fatalf("backtest replay changed identity = %#v %v", replayed, err)
	}
	changed := request
	changed.RootSeedHash = strings.Repeat("9", 64)
	if _, err = store.CreateJob(ctx, principal, "backtest", "backtest-idempotent-a11", changed); !errors.Is(err, console.ErrIdempotencyConflict) {
		t.Fatalf("job idempotency conflict accepted: %v", err)
	}
	for index := 2; index <= 4; index++ {
		if _, err = store.CreateJob(ctx, principal, "backtest", "backtest-queued-a11-"+string(rune('0'+index)), request); err != nil {
			t.Fatalf("queued job %d rejected: %v", index, err)
		}
	}
	if _, err = store.CreateJob(ctx, principal, "backtest", "backtest-queued-a11-5", request); !errors.Is(err, console.ErrQuota) {
		t.Fatalf("fifth queued job accepted: %v", err)
	}
	retriedAtQuota, err := store.CreateJob(ctx, principal, "backtest", "backtest-idempotent-a11", request)
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

func assertA11ShadowAndAudit(t *testing.T, ctx context.Context, pool *pgxpool.Pool, store *A11ConsoleStore, principal authentication.Principal, clock *domain.ReplayClock) {
	t.Helper()
	request := generated.ShadowSessionRequest{ConfigurationId: "configuration-a10", PortfolioId: "portfolio-a11",
		StrategyVersion: generated.ShadowSessionRequestStrategyVersionTrendV1a1}
	shadow, err := store.CreateShadow(ctx, principal, "shadow-create-a11", request)
	if err != nil || !bool(shadow.PublicOnly) || !bool(shadow.SimulationOnly) || shadow.EntriesEnabled || shadow.State != generated.ShadowSessionResourceState("QUEUED") {
		t.Fatalf("shadow safety projection = %#v %v", shadow, err)
	}
	if _, err = store.CreateShadow(ctx, principal, "shadow-create-second-a11", request); !errors.Is(err, console.ErrConflict) {
		t.Fatalf("second active shadow accepted: %v", err)
	}
	stop := generated.RevisionCommandRequest{ExpectedRevision: shadow.Revision, Reason: "qualification stop"}
	if _, err = store.StopShadow(ctx, principal, shadow.Id, "shadow-stop-a11", stop); err != nil {
		t.Fatalf("shadow stop rejected: %v", err)
	}
	runtimeShadow, err := store.CreateShadow(ctx, principal, "shadow-runtime-a11", request)
	if err != nil {
		t.Fatal(err)
	}
	runtimeStore, err := NewA11ShadowStore(pool, "qualification-shadow-runtime", clock)
	if err != nil {
		t.Fatal(err)
	}
	claim, found, err := runtimeStore.Claim(ctx)
	if err != nil || !found || claim.ID != runtimeShadow.Id || claim.Models.ID != "namespace-a11" ||
		claim.SlippageModelID != "slippage-v1" || claim.GapModelID != "gap-v1" {
		t.Fatalf("shadow runtime claim = %#v %t %v", claim, found, err)
	}
	if err = runtimeStore.Fail(ctx, claim.ID, "qualification_complete"); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE command_requests SET reason='tampered' WHERE id=(SELECT id FROM command_requests ORDER BY created_at LIMIT 1)`); err == nil {
		t.Fatal("immutable durable command mutated")
	}
	if _, err = pool.Exec(ctx, `DELETE FROM audit_events WHERE event_type='owner_bootstrapped'`); err == nil {
		t.Fatal("immutable bootstrap audit deleted")
	}
	var unsafe int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM shadow_sessions WHERE public_exchange<>'binance-production-public' OR NOT simulation_only`).Scan(&unsafe); err != nil || unsafe != 0 {
		t.Fatalf("unsafe shadow records = %d %v", unsafe, err)
	}
}

func assertA11SessionLimitAndPrivilegeRotation(t *testing.T, ctx context.Context, pool *pgxpool.Pool, service *authentication.Service, clock *domain.ReplayClock, first authentication.LoginResult, password string, now time.Time) {
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
	if _, err := pool.Exec(ctx, `INSERT INTO user_roles(user_id,role_id,granted_at) VALUES($1,'viewer',$2)`, first.Principal.UserID, now); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE user_id=$1 AND revoked_at IS NULL`, first.Principal.UserID).Scan(&active); err != nil || active != 0 {
		t.Fatalf("privilege change did not rotate sessions = %d %v", active, err)
	}
}
