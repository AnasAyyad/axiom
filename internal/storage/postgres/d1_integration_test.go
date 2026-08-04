package postgres

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"axiom/internal/api/console"
	"axiom/internal/api/generated"
	"axiom/internal/authentication"
	"axiom/internal/domain"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestV1DD1PostgresCleanInstallQualification(t *testing.T) {
	ctx, pool := openD1TestDatabase(t, "AXIOM_D1_TEST_DSN")
	defer pool.Close()
	assertPostgres18(t, ctx, pool)
	assertEmptyTestDatabase(t, ctx, pool)
	migrations, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	if applied, applyErr := ApplyMigrations(ctx, pool); applyErr != nil || applied != len(migrations) {
		t.Fatalf("D1 clean migration applied=%d want=%d error=%v", applied, len(migrations), applyErr)
	}
	assertD1SchemaAndFailClosedProjection(t, ctx, pool, "clean")
	assertD1IdempotencyRevisionAndExportBoundary(t, ctx, pool)
}

func assertD1IdempotencyRevisionAndExportBoundary(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) {
	t.Helper()
	fixture := newD1IntegrationFixture(t, ctx, pool)
	store, principal := fixture.store, fixture.principal
	userID := fixture.userID

	assertD1CommandAndExportBoundary(t, ctx, pool, store, principal)
	assertD1ExportAndHoldBoundary(t, ctx, pool, store, principal, userID)
}

type d1IntegrationFixture struct {
	store     *A11ConsoleStore
	principal authentication.Principal
	userID    string
}

func newD1IntegrationFixture(
	t *testing.T, ctx context.Context, pool *pgxpool.Pool,
) d1IntegrationFixture {
	t.Helper()
	// Keep deterministic command time after migration-created timestamps so the
	// lifecycle monotonicity checks do not depend on the wall date of the test.
	at := time.Date(2030, 8, 3, 12, 0, 0, 0, time.UTC)
	seedD5NormalPressure(t, ctx, pool, at)
	userID, sessionID := "d1-command-owner", "d1-command-session"
	if _, err := pool.Exec(ctx, `INSERT INTO users(
	    id,email,password_hash,status,created_at,normalized_email,password_changed_at
	  ) VALUES ($1,'d1-owner@example.test','not-a-real-secret','active',$2,
	    'd1-owner@example.test',$2)`, userID, at); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO user_roles(user_id,role_id,granted_at)
	    VALUES ($1,'owner',$2)`, userID, at); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO sessions(
	    id,user_id,token_hash,created_at,expires_at,csrf_token_hash,last_seen_at,
	    idle_expires_at,reauthenticated_at,revision
	  ) VALUES ($2,$1,$4,$3::timestamptz,$3::timestamptz+interval '1 hour',$5,
	    $3::timestamptz,$3::timestamptz+interval '30 minutes',$3::timestamptz,1)`,
		userID, sessionID, at, strings.Repeat("c", 64), strings.Repeat("d", 64)); err != nil {
		t.Fatal(err)
	}
	clock, err := domain.NewReplayClock(at)
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewA11ConsoleStore(pool, []byte(strings.Repeat("k", 32)), clock)
	if err != nil {
		t.Fatal(err)
	}
	principal := authentication.Principal{UserID: userID, SessionID: sessionID, SessionRevision: 1}
	return d1IntegrationFixture{store: store, principal: principal, userID: userID}
}

func seedD5NormalPressure(t *testing.T, ctx context.Context, pool *pgxpool.Pool, at time.Time) {
	t.Helper()
	if _, err := pool.Exec(ctx, `UPDATE v1d_storage_pressure_state SET level='NORMAL',
available_bytes=21474836480,total_bytes=107374182400,revision=revision+1,
observed_at=$1,source_instance='qualification-recorder' WHERE scope_id='market-data'`, at); err != nil {
		t.Fatal(err)
	}
}

func assertD1CommandAndExportBoundary(
	t *testing.T, ctx context.Context, pool *pgxpool.Pool, store *A11ConsoleStore,
	principal authentication.Principal,
) {
	t.Helper()
	command := console.D1Command{
		Kind: "risk_control", TargetID: "global:global", Action: "global", State: "paused",
		IdempotencyKey: "d1-risk-pause-0001", Reason: "pause all D1 entries safely",
		ExpectedRevision: 1, Payload: map[string]any{},
	}
	first, err := store.ExecuteD1(ctx, principal, command)
	if err != nil || first.State != generated.CommandAcceptedStateApplied {
		t.Fatalf("D1 command first=%+v error=%v", first, err)
	}
	replayed, err := store.ExecuteD1(ctx, principal, command)
	if err != nil || replayed.Id != first.Id {
		t.Fatalf("D1 idempotent replay=%+v error=%v", replayed, err)
	}
	conflicting := command
	conflicting.State = "locked"
	if _, err = store.ExecuteD1(ctx, principal, conflicting); !errors.Is(err, console.ErrIdempotencyConflict) {
		t.Fatalf("D1 changed idempotency payload error=%v", err)
	}
	stale := command
	stale.IdempotencyKey = "d1-risk-stale-0002"
	if _, err = store.ExecuteD1(ctx, principal, stale); !errors.Is(err, console.ErrConflict) {
		t.Fatalf("D1 stale revision error=%v", err)
	}
	var state string
	var revision int64
	if err = pool.QueryRow(ctx, `SELECT state,revision FROM v1d_risk_controls
	    WHERE scope_type='global' AND scope_id='global'`).Scan(&state, &revision); err != nil || state != "paused" || revision != 2 {
		t.Fatalf("D1 risk state=%s revision=%d error=%v", state, revision, err)
	}
	var riskEvents int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM outbox_events
	    WHERE topic='risk_control' AND stream='risk'`).Scan(&riskEvents); err != nil || riskEvents != 1 {
		t.Fatalf("D1 risk events=%d error=%v", riskEvents, err)
	}
}

func assertD1ExportAndHoldBoundary(
	t *testing.T, ctx context.Context, pool *pgxpool.Pool, store *A11ConsoleStore,
	principal authentication.Principal, userID string,
) {
	t.Helper()
	var activityID string
	var activityRevision int64
	if err := pool.QueryRow(ctx, `SELECT id,activity_revision FROM v1d_activity_projection
	    WHERE source_id='known-clean'`).Scan(&activityID, &activityRevision); err != nil {
		t.Fatal(err)
	}
	request := generated.ExportRequest{
		ExpectedRevision: generated.Revision(strconv.FormatInt(activityRevision, 10)),
		Format:           generated.ExportRequestFormatJson,
		Reason:           "export redacted D1 activity evidence",
		ResourceId:       activityID,
		ResourceType:     generated.ExportRequestResourceTypeActivity,
	}
	artifact, err := store.CreateD1Export(ctx, principal, "d1-export-0001", request)
	if err != nil {
		t.Fatal(err)
	}
	if artifact.Content == nil || artifact.ContentHash != a11Hash([]byte(*artifact.Content)) ||
		artifact.ExpiresAt.Sub(artifact.CreatedAt) != 7*24*time.Hour {
		t.Fatalf("D1 artifact seal/retention invalid: %+v", artifact)
	}
	assertD1ExportJob(t, ctx, pool, artifact)
	lower := strings.ToLower(*artifact.Content)
	for _, forbidden := range []string{"not-a-real-secret", "authorization_token", "request_signature", "private_payload"} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("D1 artifact exposed forbidden value %q", forbidden)
		}
	}
	replayedArtifact, err := store.CreateD1Export(ctx, principal, "d1-export-0001", request)
	if err != nil || replayedArtifact.Id != artifact.Id {
		t.Fatalf("D1 export replay=%+v error=%v", replayedArtifact, err)
	}
	changedExport := request
	changedExport.Format = generated.ExportRequestFormatCsv
	if _, err = store.CreateD1Export(ctx, principal, "d1-export-0001", changedExport); !errors.Is(err, console.ErrIdempotencyConflict) {
		t.Fatalf("D1 export idempotency conflict error=%v", err)
	}
	assertD1HoldAndQuota(t, ctx, pool, store, principal, userID, request, artifact)
}

func assertD1ExportJob(
	t *testing.T, ctx context.Context, pool *pgxpool.Pool, artifact generated.ExportArtifact,
) {
	t.Helper()
	var jobType, state string
	var revision int64
	if err := pool.QueryRow(ctx, `SELECT job_type,state,progress_revision FROM jobs WHERE id=$1`,
		artifact.JobId).Scan(&jobType, &state, &revision); err != nil {
		t.Fatal(err)
	}
	if artifact.JobId == "" || jobType != "export" || state != "SUCCEEDED" || revision != 3 {
		t.Fatalf("D1 export job id=%q type=%s state=%s revision=%d", artifact.JobId, jobType, state, revision)
	}
}

func assertD1HoldAndQuota(
	t *testing.T, ctx context.Context, pool *pgxpool.Pool, store *A11ConsoleStore,
	principal authentication.Principal, userID string, request generated.ExportRequest,
	artifact generated.ExportArtifact,
) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO incidents(
id,severity,state,reason_code,opened_at,resolved_at,updated_at
) VALUES ('incident-d1','warning','open','d1_artifact_review',$1,NULL,$1)`,
		artifact.CreatedAt); err != nil {
		t.Fatal(err)
	}
	holdRevision := int64(1)
	holdReason := "hold D1 artifact for incident review"
	hold := console.D1Command{
		Kind: "artifact_hold", TargetID: artifact.Id, Action: "incident", State: "held",
		IdempotencyKey: "d1-artifact-hold-0002", Reason: holdReason, ExpectedRevision: 1,
		Payload: map[string]any{"hold_type": "incident", "reference_id": "incident-d1"},
		Authorization: &authentication.ConsumedAuthorization{
			UserID: userID, Purpose: authentication.PurposeArtifactHold,
			ReasonHash:     authentication.AuthorizationBindingHash(holdReason),
			TargetRevision: &holdRevision,
		},
	}
	if _, err := store.ExecuteD1(ctx, principal, hold); err != nil {
		t.Fatalf("D1 artifact hold failed: %v", err)
	}
	deleteCommand := console.D1Command{
		Kind: "export", TargetID: artifact.Id, Action: "delete", State: "deleted",
		IdempotencyKey: "d1-artifact-delete-0003", Reason: "delete expired D1 export artifact",
		ExpectedRevision: 1, Payload: map[string]any{},
	}
	if _, err := store.ExecuteD1(ctx, principal, deleteCommand); !errors.Is(err, console.ErrPrecondition) {
		t.Fatalf("held D1 artifact deletion error=%v", err)
	}
	var deletedAt *time.Time
	if err := pool.QueryRow(ctx, `SELECT deleted_at FROM v1d_export_artifacts WHERE id=$1`, artifact.Id).Scan(&deletedAt); err != nil || deletedAt != nil {
		t.Fatalf("held D1 artifact deleted_at=%v error=%v", deletedAt, err)
	}
	for index := 2; index <= 20; index++ {
		key := "d1-export-quota-" + strconv.Itoa(index)
		if _, err := store.CreateD1Export(ctx, principal, key, request); err != nil {
			t.Fatalf("D1 export quota fill %d failed: %v", index, err)
		}
	}
	if _, err := store.CreateD1Export(ctx, principal, "d1-export-quota-21", request); !errors.Is(err, console.ErrQuota) {
		t.Fatalf("D1 per-user export quota error=%v", err)
	}
}

func TestV1DD1PostgresV1CToD1UpgradeQualification(t *testing.T) {
	ctx, pool := openD1TestDatabase(t, "AXIOM_D1_UPGRADE_TEST_DSN")
	defer pool.Close()
	assertPostgres18(t, ctx, pool)
	assertEmptyTestDatabase(t, ctx, pool)
	applyB4MigrationPrefix(t, ctx, pool, 24)
	insertD1AuditSource(t, ctx, pool, "upgrade-backfill")
	connection, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Release()
	migrations, err := Migrations()
	if err != nil || len(migrations) < 25 {
		t.Fatalf("D1 migration catalog=%d error=%v", len(migrations), err)
	}
	changed, err := applyMigration(ctx, connection, migrations[24])
	if err != nil || !changed {
		t.Fatalf("V1C-to-D1 migration changed=%t error=%v", changed, err)
	}
	assertD1ProjectedReason(t, ctx, pool, "upgrade-backfill", false)
	assertD1SchemaAndFailClosedProjection(t, ctx, pool, "upgrade")
}

func openD1TestDatabase(t *testing.T, environment string) (context.Context, *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv(environment)
	if dsn == "" {
		t.Skip(environment + " is not set")
	}
	configuration, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(configuration.ConnConfig.Database, "_d1_test") {
		t.Fatal("D1 integration requires a dedicated database ending _d1_test")
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

func assertD1SchemaAndFailClosedProjection(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	suffix string,
) {
	t.Helper()
	assertD1Relations(t, ctx, pool)
	assertD1StrategySeed(t, ctx, pool, suffix)
	assertD1ProjectionBoundaries(t, ctx, pool, suffix)
	assertD1RevisionConstraint(t, ctx, pool)
}

func assertD1Relations(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	for _, relation := range []string{
		"v1d_reason_catalogue", "v1d_activity_projection", "v1d_activity_explanations",
		"v1d_strategy_controls", "v1d_risk_controls", "v1d_export_artifacts",
		"v1d_artifact_holds", "v1d_artifact_access_events",
		"v1d_qualification_catalogue", "v1d_qualification_runs", "v1d_role_change_events",
	} {
		var exists bool
		if err := pool.QueryRow(ctx, `SELECT to_regclass('public.'||$1) IS NOT NULL`, relation).Scan(&exists); err != nil || !exists {
			t.Fatalf("D1 relation %s exists=%t error=%v", relation, exists, err)
		}
	}
}

func assertD1StrategySeed(t *testing.T, ctx context.Context, pool *pgxpool.Pool, suffix string) {
	t.Helper()
	strategyID := "d1-strategy-" + suffix
	if _, err := pool.Exec(ctx, `INSERT INTO strategy_definitions(id,name,family) VALUES ($1,$2,'d1-test')`,
		strategyID, "D1 strategy "+suffix); err != nil {
		t.Fatal(err)
	}
	var configured, runtime string
	var revision int64
	if err := pool.QueryRow(ctx, `SELECT configured_state,runtime_state,revision
	    FROM v1d_strategy_controls WHERE strategy_id=$1`, strategyID).Scan(
		&configured, &runtime, &revision,
	); err != nil || configured != "disabled" || runtime != "blocked" || revision != 1 {
		t.Fatalf("D1 strategy seed=%s/%s/%d error=%v", configured, runtime, revision, err)
	}
}

func assertD1ProjectionBoundaries(t *testing.T, ctx context.Context, pool *pgxpool.Pool, suffix string) {
	t.Helper()
	knownID := "known-" + suffix
	insertD1AuditSource(t, ctx, pool, knownID)
	assertD1ProjectedReason(t, ctx, pool, knownID, false)
	unknownID := "unknown-" + suffix
	insertD1UnknownAlert(t, ctx, pool, unknownID)
	assertD1ProjectedReason(t, ctx, pool, unknownID, true)

	if _, err := pool.Exec(ctx, `UPDATE v1d_activity_projection SET outcome='tampered'
	    WHERE source_id=$1`, knownID); err == nil {
		t.Fatal("immutable D1 activity projection accepted mutation")
	}
}

func assertD1RevisionConstraint(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var targetConstraint string
	if err := pool.QueryRow(ctx, `SELECT pg_get_constraintdef(oid)
	    FROM pg_constraint WHERE conname='v1d_authorization_target_revision_required'`).Scan(
		&targetConstraint,
	); err != nil || !strings.Contains(targetConstraint, "target_revision IS NOT NULL") {
		t.Fatalf("D1 target-revision constraint=%q error=%v", targetConstraint, err)
	}
}

func insertD1AuditSource(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO audit_events(
	    id,event_type,actor,causation_id,correlation_id,event_hash,recorded_at
	  ) VALUES ($1,'d1_test','d1-test',$1,$1,$2,CURRENT_TIMESTAMP)`,
		id, strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
}

func insertD1UnknownAlert(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO alerts(
	    id,alert_type,state,created_at,severity,reason_code,deduplication_key,
	    correlation_id,last_seen_at,occurrences,revision
	  ) VALUES ($1,'d1_unknown','open',CURRENT_TIMESTAMP,'warning','unpublished.reason',$2,
	    $1,CURRENT_TIMESTAMP,1,1)`, id, strings.Repeat("b", 64)); err != nil {
		t.Fatal(err)
	}
}

func assertD1ProjectedReason(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	sourceID string,
	unknown bool,
) {
	t.Helper()
	var summary string
	var gotUnknown bool
	if err := pool.QueryRow(ctx, `SELECT reason_summary,unknown_reason
	    FROM v1d_activity_explanations WHERE source_id=$1`, sourceID).Scan(
		&summary, &gotUnknown,
	); err != nil || gotUnknown != unknown {
		t.Fatalf("D1 projection %s unknown=%t summary=%q error=%v", sourceID, gotUnknown, summary, err)
	}
	if unknown && summary != "Activity recorded" {
		t.Fatalf("unknown D1 reason summary=%q", summary)
	}
}
