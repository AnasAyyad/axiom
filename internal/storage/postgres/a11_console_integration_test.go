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
	seedA11RuntimeEvidence(t, ctx, pool, now)
	consoleStore, err := NewA11ConsoleStore(pool, []byte(strings.Repeat("s", 32)), clock)
	if err != nil {
		t.Fatal(err)
	}
	assertA11StablePagination(t, ctx, pool, consoleStore, now)
	assertA11RiskRecovery(t, ctx, consoleStore, login.Principal)
	assertA11DurableJobs(t, ctx, pool, consoleStore, login.Principal)
	assertA11WorkerLeasesAndRecovery(t, ctx, pool, consoleStore, clock)
	assertA11ShadowAndAudit(t, ctx, pool, consoleStore, login.Principal)
	assertA11ResumableStream(t, ctx, pool, consoleStore, login.Principal)
	assertA11SessionLimitAndPrivilegeRotation(t, ctx, pool, authService, clock, login, password, now)
}

func assertA11WorkerLeasesAndRecovery(t *testing.T, ctx context.Context, pool *pgxpool.Pool, consoleStore *A11ConsoleStore, clock *domain.ReplayClock) {
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
	projection, err := consoleStore.Job(ctx, claim.ID)
	if err != nil || projection.State != generated.JobResourceState("SUCCEEDED") || projection.Result == nil || projection.Result.ResultHash != result.ResultHash {
		t.Fatalf("completed projection = %#v %v", projection, err)
	}
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
}

func a11QualificationClaim(id, kind string) backtest.JobClaim {
	hash := strings.Repeat("d", 64)
	runID, _ := domain.NewRunID(id)
	return backtest.JobClaim{Manifest: backtest.RunManifest{RunID: runID, Mode: kind,
		CodeCommit: strings.Repeat("c", 40), Build: backtest.CurrentBuildIdentity([]string{"trimpath"}, hash, hash),
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
		Metrics: metrics, ResultHash: strings.Repeat("a", 64)}
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
	first, err := store.Incidents(ctx, "", 1)
	if err != nil || len(first.Items) != 1 || first.NextCursor == nil || first.Items[0].Id != "incident-m" {
		t.Fatalf("first incident page = %#v %v", first, err)
	}
	second, err := store.Incidents(ctx, *first.NextCursor, 1)
	if err != nil || len(second.Items) != 1 || second.Items[0].Id != "incident-a" || second.Items[0].Id == first.Items[0].Id {
		t.Fatalf("second incident page = %#v %v", second, err)
	}
	if _, err = store.TrendDecisions(ctx, *first.NextCursor, 1); !errors.Is(err, console.ErrInvalidRequest) {
		t.Fatalf("filter/sort-bound cursor crossed resource scope: %v", err)
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
		  VALUES('namespace-a11',$1,'production-public','combined-a11','fee-a10','latency-a10','fill-a10',$1,'{}',$2)`, []any{hash, now}},
		{`INSERT INTO virtual_accounts(id,portfolio_id,run_id,name,created_at) VALUES('account-a11','portfolio-a11','run-a11-shadow','main',$1)`, []any{now}},
		{`INSERT INTO virtual_balances(account_id,asset_symbol,available,reserved,revision,updated_at)
          VALUES('account-a11','USDT',1000,0,1,$1),('account-a11','BTC',0,0,1,$1)`, []any{now}},
		{`INSERT INTO instrument_metadata_versions(id,exchange_id,instrument_id,version,price_tick,quantity_step,
		  minimum_quantity,minimum_notional,effective_at,recorded_at)
		  VALUES('metadata-a11-binance','binance','instrument-a10',1,0.01,0.00001,0.00001,10,$1,$1)`, []any{now}},
		{`INSERT INTO market_data_segments(id,recorder_session,exchange_id,instrument_id,event_type,schema_version,parser_version,normalization_version,compression,path,checksum,ordered_content_hash,record_count,first_ordinal,last_ordinal,started_at,ended_at,state,finalized_at)
		  VALUES('segment-a11','recorder-a11','binance','instrument-a10','book','market-wire.v1','parser-a11','normalizer-a11','zstd','a11/segment.zst',$1,$1,1,1,1,$2,$2,'ready',$2)`, []any{hash, now}},
		{`INSERT INTO market_data_segments(id,recorder_session,exchange_id,instrument_id,event_type,schema_version,parser_version,normalization_version,compression,path,checksum,ordered_content_hash,record_count,first_ordinal,last_ordinal,started_at,ended_at,state,finalized_at)
		  VALUES('segment-a11-candle','recorder-a11','binance','instrument-a10','candle','market-wire.v1','parser-a11','normalizer-a11','zstd','a11/candle.zst',$1,$1,1,2,2,$2,$2,'ready',$2)`, []any{hash, now}},
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
		RootSeedHash: strings.Repeat("8", 64), StrategyVersion: generated.OfflineJobRequestStrategyVersionTrendV1a1}
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
	var commands, jobs, outbox int
	if err = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM command_requests),(SELECT count(*) FROM jobs),(SELECT count(*) FROM outbox_events)`).Scan(&commands, &jobs, &outbox); err != nil {
		t.Fatal(err)
	}
	if commands != 5 || jobs != 4 || outbox != 5 {
		t.Fatalf("durable command/job/outbox counts = %d/%d/%d", commands, jobs, outbox)
	}
}

func assertA11ShadowAndAudit(t *testing.T, ctx context.Context, pool *pgxpool.Pool, store *A11ConsoleStore, principal authentication.Principal) {
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
