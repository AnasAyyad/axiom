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

func TestOwnerControlPostgresCleanInstallQualification(t *testing.T) {
	ctx, pool := openOwnerControlTestDatabase(t, "AXIOM_OWNER_CONTROL_TEST_DSN")
	defer pool.Close()
	assertPostgres18(t, ctx, pool)
	assertEmptyTestDatabase(t, ctx, pool)
	migrations, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	if applied, applyErr := ApplyMigrations(ctx, pool); applyErr != nil || applied != len(migrations) {
		t.Fatalf("owner control clean migration applied=%d want=%d error=%v", applied, len(migrations), applyErr)
	}
	assertOwnerControlSchemaAndFailClosedProjection(t, ctx, pool, "clean")
	assertOwnerControlIdempotencyRevisionAndExportBoundary(t, ctx, pool)
}

func assertOwnerControlIdempotencyRevisionAndExportBoundary(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) {
	t.Helper()
	fixture := newOwnerControlIntegrationFixture(t, ctx, pool)
	store, principal := fixture.store, fixture.principal
	userID := fixture.userID

	assertOwnerControlCommandAndExportBoundary(t, ctx, pool, store, principal)
	assertOwnerControlExportAndHoldBoundary(t, ctx, pool, store, principal, userID)
}

type ownerControlIntegrationFixture struct {
	store     *OwnerConsoleStore
	principal authentication.Principal
	userID    string
}

func newOwnerControlIntegrationFixture(
	t *testing.T, ctx context.Context, pool *pgxpool.Pool,
) ownerControlIntegrationFixture {
	t.Helper()
	// Keep deterministic command time after migration-created timestamps so the
	// lifecycle monotonicity checks do not depend on the wall date of the test.
	at := time.Date(2030, 8, 3, 12, 0, 0, 0, time.UTC)
	seedOperationalReadinessNormalPressure(t, ctx, pool, at)
	userID, sessionID := "owner_control-command-owner", "owner_control-command-session"
	if _, err := pool.Exec(ctx, `INSERT INTO users(
	    id,email,password_hash,status,created_at,normalized_email,password_changed_at
	  ) VALUES ($1,'owner_control-owner@example.test','not-a-real-secret','active',$2,
	    'owner_control-owner@example.test',$2)`, userID, at); err != nil {
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
	store, err := NewOwnerConsoleStore(pool, []byte(strings.Repeat("k", 32)), clock)
	if err != nil {
		t.Fatal(err)
	}
	principal := authentication.Principal{UserID: userID, SessionID: sessionID, SessionRevision: 1}
	return ownerControlIntegrationFixture{store: store, principal: principal, userID: userID}
}

func seedOperationalReadinessNormalPressure(t *testing.T, ctx context.Context, pool *pgxpool.Pool, at time.Time) {
	t.Helper()
	if _, err := pool.Exec(ctx, `UPDATE owner_console_storage_pressure_state SET level='NORMAL',
available_bytes=21474836480,total_bytes=107374182400,revision=revision+1,
observed_at=$1,source_instance='qualification-recorder' WHERE scope_id='market-data'`, at); err != nil {
		t.Fatal(err)
	}
}

func assertOwnerControlCommandAndExportBoundary(
	t *testing.T, ctx context.Context, pool *pgxpool.Pool, store *OwnerConsoleStore,
	principal authentication.Principal,
) {
	t.Helper()
	command := console.OwnerControlCommand{
		Kind: "risk_control", TargetID: "global:global", Action: "global", State: "paused",
		IdempotencyKey: "owner_control-risk-pause-0001", Reason: "pause all owner control entries safely",
		ExpectedRevision: 1, Payload: map[string]any{},
	}
	first, err := store.ExecuteOwnerControl(ctx, principal, command)
	if err != nil || first.State != generated.CommandAcceptedStateApplied {
		t.Fatalf("owner control command first=%+v error=%v", first, err)
	}
	replayed, err := store.ExecuteOwnerControl(ctx, principal, command)
	if err != nil || replayed.Id != first.Id {
		t.Fatalf("owner control idempotent replay=%+v error=%v", replayed, err)
	}
	conflicting := command
	conflicting.State = "locked"
	if _, err = store.ExecuteOwnerControl(ctx, principal, conflicting); !errors.Is(err, console.ErrIdempotencyConflict) {
		t.Fatalf("owner control changed idempotency payload error=%v", err)
	}
	stale := command
	stale.IdempotencyKey = "owner_control-risk-stale-0002"
	if _, err = store.ExecuteOwnerControl(ctx, principal, stale); !errors.Is(err, console.ErrConflict) {
		t.Fatalf("owner control stale revision error=%v", err)
	}
	var state string
	var revision int64
	if err = pool.QueryRow(ctx, `SELECT state,revision FROM owner_console_risk_controls
	    WHERE scope_type='global' AND scope_id='global'`).Scan(&state, &revision); err != nil || state != "paused" || revision != 2 {
		t.Fatalf("owner control risk state=%s revision=%d error=%v", state, revision, err)
	}
	var riskEvents int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM outbox_events
	    WHERE topic='risk_control' AND stream='risk'`).Scan(&riskEvents); err != nil || riskEvents != 1 {
		t.Fatalf("owner control risk events=%d error=%v", riskEvents, err)
	}
}

func assertOwnerControlExportAndHoldBoundary(
	t *testing.T, ctx context.Context, pool *pgxpool.Pool, store *OwnerConsoleStore,
	principal authentication.Principal, userID string,
) {
	t.Helper()
	var activityID string
	var activityRevision int64
	if err := pool.QueryRow(ctx, `SELECT id,activity_revision FROM owner_console_activity_projection
	    WHERE source_id='known-clean'`).Scan(&activityID, &activityRevision); err != nil {
		t.Fatal(err)
	}
	request := generated.ExportRequest{
		ExpectedRevision: generated.Revision(strconv.FormatInt(activityRevision, 10)),
		Format:           generated.ExportRequestFormatJson,
		Reason:           "export redacted owner control activity evidence",
		ResourceId:       activityID,
		ResourceType:     generated.ExportRequestResourceTypeActivity,
	}
	artifact, err := store.CreateOwnerControlExport(ctx, principal, "owner_control-export-0001", request)
	if err != nil {
		t.Fatal(err)
	}
	if artifact.Content == nil || artifact.ContentHash != ownerConsoleHash([]byte(*artifact.Content)) ||
		artifact.ExpiresAt.Sub(artifact.CreatedAt) != 7*24*time.Hour {
		t.Fatalf("owner control artifact seal/retention invalid: %+v", artifact)
	}
	assertOwnerControlExportJob(t, ctx, pool, artifact)
	lower := strings.ToLower(*artifact.Content)
	for _, forbidden := range []string{"not-a-real-secret", "authorization_token", "request_signature", "private_payload"} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("owner control artifact exposed forbidden value %q", forbidden)
		}
	}
	replayedArtifact, err := store.CreateOwnerControlExport(ctx, principal, "owner_control-export-0001", request)
	if err != nil || replayedArtifact.Id != artifact.Id {
		t.Fatalf("owner control export replay=%+v error=%v", replayedArtifact, err)
	}
	changedExport := request
	changedExport.Format = generated.ExportRequestFormatCsv
	if _, err = store.CreateOwnerControlExport(ctx, principal, "owner_control-export-0001", changedExport); !errors.Is(err, console.ErrIdempotencyConflict) {
		t.Fatalf("owner control export idempotency conflict error=%v", err)
	}
	assertOwnerControlHoldAndQuota(t, ctx, pool, store, principal, userID, request, artifact)
}

func assertOwnerControlExportJob(
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
		t.Fatalf("owner control export job id=%q type=%s state=%s revision=%d", artifact.JobId, jobType, state, revision)
	}
}

func assertOwnerControlHoldAndQuota(
	t *testing.T, ctx context.Context, pool *pgxpool.Pool, store *OwnerConsoleStore,
	principal authentication.Principal, userID string, request generated.ExportRequest,
	artifact generated.ExportArtifact,
) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO incidents(
id,severity,state,reason_code,opened_at,resolved_at,updated_at
) VALUES ('incident-owner_control','warning','open','owner_control_artifact_review',$1,NULL,$1)`,
		artifact.CreatedAt); err != nil {
		t.Fatal(err)
	}
	holdRevision := int64(1)
	holdReason := "hold owner control artifact for incident review"
	hold := console.OwnerControlCommand{
		Kind: "artifact_hold", TargetID: artifact.Id, Action: "incident", State: "held",
		IdempotencyKey: "owner_control-artifact-hold-0002", Reason: holdReason, ExpectedRevision: 1,
		Payload: map[string]any{"hold_type": "incident", "reference_id": "incident-owner_control"},
		Authorization: &authentication.ConsumedAuthorization{
			UserID: userID, Purpose: authentication.PurposeArtifactHold,
			ReasonHash:     authentication.AuthorizationBindingHash(holdReason),
			TargetRevision: &holdRevision,
		},
	}
	if _, err := store.ExecuteOwnerControl(ctx, principal, hold); err != nil {
		t.Fatalf("owner control artifact hold failed: %v", err)
	}
	deleteCommand := console.OwnerControlCommand{
		Kind: "export", TargetID: artifact.Id, Action: "delete", State: "deleted",
		IdempotencyKey: "owner_control-artifact-delete-0003", Reason: "delete expired owner control export artifact",
		ExpectedRevision: 1, Payload: map[string]any{},
	}
	if _, err := store.ExecuteOwnerControl(ctx, principal, deleteCommand); !errors.Is(err, console.ErrPrecondition) {
		t.Fatalf("held owner control artifact deletion error=%v", err)
	}
	var deletedAt *time.Time
	if err := pool.QueryRow(ctx, `SELECT deleted_at FROM owner_console_export_artifacts WHERE id=$1`, artifact.Id).Scan(&deletedAt); err != nil || deletedAt != nil {
		t.Fatalf("held owner control artifact deleted_at=%v error=%v", deletedAt, err)
	}
	for index := 2; index <= 20; index++ {
		key := "owner_control-export-quota-" + strconv.Itoa(index)
		if _, err := store.CreateOwnerControlExport(ctx, principal, key, request); err != nil {
			t.Fatalf("owner control export quota fill %d failed: %v", index, err)
		}
	}
	if _, err := store.CreateOwnerControlExport(ctx, principal, "owner_control-export-quota-21", request); !errors.Is(err, console.ErrQuota) {
		t.Fatalf("owner control per-user export quota error=%v", err)
	}
}

func TestOwnerControlPostgresSandboxRuntimeToOwnerControlUpgradeQualification(t *testing.T) {
	ctx, pool := openOwnerControlTestDatabase(t, "AXIOM_OWNER_CONTROL_UPGRADE_TEST_DSN")
	defer pool.Close()
	assertPostgres18(t, ctx, pool)
	assertEmptyTestDatabase(t, ctx, pool)
	applyTriangularArbitrageMigrationPrefix(t, ctx, pool, 24)
	insertOwnerControlAuditSource(t, ctx, pool, "upgrade-backfill")
	connection, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Release()
	migrations, err := Migrations()
	if err != nil || len(migrations) < 25 {
		t.Fatalf("owner control migration catalog=%d error=%v", len(migrations), err)
	}
	changed, err := applyMigration(ctx, connection, migrations[24])
	if err != nil || !changed {
		t.Fatalf("sandbox-to-owner-control migration changed=%t error=%v", changed, err)
	}
	assertOwnerControlProjectedReason(t, ctx, pool, "upgrade-backfill", false)
	assertOwnerControlSchemaAndFailClosedProjection(t, ctx, pool, "upgrade")
}

func openOwnerControlTestDatabase(t *testing.T, environment string) (context.Context, *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv(environment)
	if dsn == "" {
		t.Skip(environment + " is not set")
	}
	configuration, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(configuration.ConnConfig.Database, "_owner_control_test") {
		t.Fatal("owner control integration requires a dedicated database ending _owner_control_test")
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

func assertOwnerControlSchemaAndFailClosedProjection(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	suffix string,
) {
	t.Helper()
	assertOwnerControlRelations(t, ctx, pool)
	assertOwnerControlStrategySeed(t, ctx, pool, suffix)
	assertOwnerControlProjectionBoundaries(t, ctx, pool, suffix)
	assertOwnerControlRevisionConstraint(t, ctx, pool)
}

func assertOwnerControlRelations(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	for _, relation := range []string{
		"owner_console_reason_catalogue", "owner_console_activity_projection", "owner_console_activity_explanations",
		"owner_console_strategy_controls", "owner_console_risk_controls", "owner_console_export_artifacts",
		"owner_console_artifact_holds", "owner_console_artifact_access_events",
		"owner_console_qualification_catalogue", "owner_console_qualification_runs", "owner_console_role_change_events",
	} {
		var exists bool
		if err := pool.QueryRow(ctx, `SELECT to_regclass('public.'||$1) IS NOT NULL`, relation).Scan(&exists); err != nil || !exists {
			t.Fatalf("owner control relation %s exists=%t error=%v", relation, exists, err)
		}
	}
}

func assertOwnerControlStrategySeed(t *testing.T, ctx context.Context, pool *pgxpool.Pool, suffix string) {
	t.Helper()
	strategyID := "owner_control-strategy-" + suffix
	if _, err := pool.Exec(ctx, `INSERT INTO strategy_definitions(id,name,family) VALUES ($1,$2,'owner_control-test')`,
		strategyID, "owner control strategy "+suffix); err != nil {
		t.Fatal(err)
	}
	var configured, runtime string
	var revision int64
	if err := pool.QueryRow(ctx, `SELECT configured_state,runtime_state,revision
	    FROM owner_console_strategy_controls WHERE strategy_id=$1`, strategyID).Scan(
		&configured, &runtime, &revision,
	); err != nil || configured != "disabled" || runtime != "blocked" || revision != 1 {
		t.Fatalf("owner control strategy seed=%s/%s/%d error=%v", configured, runtime, revision, err)
	}
}

func assertOwnerControlProjectionBoundaries(t *testing.T, ctx context.Context, pool *pgxpool.Pool, suffix string) {
	t.Helper()
	knownID := "known-" + suffix
	insertOwnerControlAuditSource(t, ctx, pool, knownID)
	assertOwnerControlProjectedReason(t, ctx, pool, knownID, false)
	unknownID := "unknown-" + suffix
	insertOwnerControlUnknownAlert(t, ctx, pool, unknownID)
	assertOwnerControlProjectedReason(t, ctx, pool, unknownID, true)

	if _, err := pool.Exec(ctx, `UPDATE owner_console_activity_projection SET outcome='tampered'
	    WHERE source_id=$1`, knownID); err == nil {
		t.Fatal("immutable owner control activity projection accepted mutation")
	}
}

func assertOwnerControlRevisionConstraint(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var targetConstraint string
	if err := pool.QueryRow(ctx, `SELECT pg_get_constraintdef(oid)
	    FROM pg_constraint WHERE conname='owner_console_authorization_target_revision_required'`).Scan(
		&targetConstraint,
	); err != nil || !strings.Contains(targetConstraint, "target_revision IS NOT NULL") {
		t.Fatalf("owner control target-revision constraint=%q error=%v", targetConstraint, err)
	}
}

func insertOwnerControlAuditSource(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO audit_events(
	    id,event_type,actor,causation_id,correlation_id,event_hash,recorded_at
	  ) VALUES ($1,'owner_control_test','owner_control-test',$1,$1,$2,CURRENT_TIMESTAMP)`,
		id, strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
}

func insertOwnerControlUnknownAlert(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO alerts(
	    id,alert_type,state,created_at,severity,reason_code,deduplication_key,
	    correlation_id,last_seen_at,occurrences,revision
	  ) VALUES ($1,'owner_control_unknown','open',CURRENT_TIMESTAMP,'warning','unpublished.reason',$2,
	    $1,CURRENT_TIMESTAMP,1,1)`, id, strings.Repeat("b", 64)); err != nil {
		t.Fatal(err)
	}
}

func assertOwnerControlProjectedReason(
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
	    FROM owner_console_activity_explanations WHERE source_id=$1`, sourceID).Scan(
		&summary, &gotUnknown,
	); err != nil || gotUnknown != unknown {
		t.Fatalf("owner control projection %s unknown=%t summary=%q error=%v", sourceID, gotUnknown, summary, err)
	}
	if unknown && summary != "Activity recorded" {
		t.Fatalf("unknown owner control reason summary=%q", summary)
	}
}
