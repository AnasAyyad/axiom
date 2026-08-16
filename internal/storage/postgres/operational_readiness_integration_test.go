package postgres

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"axiom/internal/api/generated"
	"axiom/internal/domain"
	"axiom/internal/storage/pressure"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestOperationalReadinessPostgresOperationalReadinessQualification(t *testing.T) {
	ctx, pool := openOperationalReadinessTestDatabase(t, "AXIOM_OPERATIONAL_READINESS_TEST_DSN")
	defer pool.Close()
	assertPostgres18(t, ctx, pool)
	assertEmptyTestDatabase(t, ctx, pool)
	migrations, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	if applied, applyErr := ApplyMigrations(ctx, pool); applyErr != nil || applied != len(migrations) {
		t.Fatalf("operational readiness clean migration applied=%d want=%d error=%v", applied, len(migrations), applyErr)
	}
	assertOperationalReadinessPressureLifecycle(t, ctx, pool)
	assertOperationalReadinessArtifactLifecycle(t, ctx, pool)
	assertOperationalReadinessObserverRoleBoundary(t, ctx, pool)
}

func assertOperationalReadinessObserverRoleBoundary(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	role := fmt.Sprintf("axiom_d5_observer_%d", time.Now().UnixNano())
	identifier := pgx.Identifier{role}.Sanitize()
	if _, err := pool.Exec(ctx, "CREATE ROLE "+identifier+" NOLOGIN"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DROP OWNED BY "+identifier)
		_, _ = pool.Exec(context.Background(), "DROP ROLE "+identifier)
	})
	if err := ApplyOperationalReadinessObserverRoleGrants(
		ctx, pool, role,
		"axiom_app", "axiom_recorder", "axiom_readonly", "axiom_binance_engine",
		"axiom_bybit_engine", "axiom_sandbox_qualification",
	); err != nil {
		t.Fatal(err)
	}
	var granted int
	if err := pool.QueryRow(ctx, `
SELECT count(*)
FROM information_schema.role_table_grants
WHERE grantee=$1 AND privilege_type='SELECT'`, role).Scan(&granted); err != nil ||
		granted != len(operationalReadinessObserverTables) {
		t.Fatalf("observer SELECT grants=%d want=%d error=%v", granted, len(operationalReadinessObserverTables), err)
	}
	var unsafe int
	if err := pool.QueryRow(ctx, `
SELECT count(*)
FROM information_schema.role_table_grants
WHERE grantee=$1 AND (
  privilege_type<>'SELECT' OR table_name IN (
    'users','sessions','sandbox_runtime_private_inbox',
    'sandbox_runtime_sandbox_authorizations','sandbox_runtime_credential_generations'
  )
)`, role).Scan(&unsafe); err != nil || unsafe != 0 {
		t.Fatalf("observer unsafe grants=%d error=%v", unsafe, err)
	}
}

func assertOperationalReadinessArtifactLifecycle(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	fixture := newOwnerControlIntegrationFixture(t, ctx, pool)
	at := time.Date(2030, 8, 3, 12, 0, 0, 0, time.UTC)
	var revision int64
	if err := pool.QueryRow(ctx, `INSERT INTO owner_console_activity_projection(
id,view_kind,source_type,source_id,source_revision,reason_code,outcome,
correlation_id,occurred_at,details,projected_at
) VALUES('activity-operational_readiness-lifecycle','system_events','operational_readiness_test','source-operational_readiness','1',
'unknown','observed','operational_readiness-lifecycle',$1,'{}'::jsonb,$1) RETURNING activity_revision`, at).Scan(&revision); err != nil {
		t.Fatal(err)
	}
	request := generated.ExportRequest{ExpectedRevision: generated.Revision(strconv.FormatInt(revision, 10)),
		Format: generated.ExportRequestFormatJson, Reason: "verify automatic operational readiness lifecycle expiry",
		ResourceId: "activity-operational_readiness-lifecycle", ResourceType: generated.ExportRequestResourceTypeActivity}
	first, err := fixture.store.CreateOwnerControlExport(ctx, fixture.principal, "operational_readiness-lifecycle-export-1", request)
	if err != nil {
		t.Fatal(err)
	}
	held, err := fixture.store.CreateOwnerControlExport(ctx, fixture.principal, "operational_readiness-lifecycle-export-2", request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO incidents(id,severity,state,reason_code,owner_user_id,opened_at,updated_at)
VALUES('incident-operational_readiness-hold','warning','open','retention_review',$1,$2,$2)`, fixture.userID, at); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO owner_console_artifact_holds(
id,artifact_id,hold_type,reference_id,reason,actor_user_id,created_at
) VALUES('hold-operational_readiness-lifecycle',$1,'incident','incident-operational_readiness-hold','preserve operational readiness lifecycle evidence',$2,$3)`, held.Id, fixture.userID, at); err != nil {
		t.Fatal(err)
	}
	clock, _ := domain.NewReplayClock(at.Add(8 * 24 * time.Hour))
	worker, _ := NewOperationalReadinessLifecycleWorker(pool, clock)
	worked, err := worker.RunOne(ctx)
	if err != nil || !worked {
		t.Fatalf("lifecycle worked=%t error=%v", worked, err)
	}
	var firstDeleted *time.Time
	var heldDeleted *time.Time
	if err = pool.QueryRow(ctx, `SELECT deleted_at FROM owner_console_export_artifacts WHERE id=$1`, first.Id).Scan(&firstDeleted); err != nil || firstDeleted == nil {
		t.Fatalf("expired artifact deletion=%v error=%v", firstDeleted, err)
	}
	if err = pool.QueryRow(ctx, `SELECT deleted_at FROM owner_console_export_artifacts WHERE id=$1`, held.Id).Scan(&heldDeleted); err != nil || heldDeleted != nil {
		t.Fatalf("held artifact deletion=%v error=%v", heldDeleted, err)
	}
}

func TestOperationalReadinessPostgresOperationalEvidenceToOperationalReadinessUpgradeQualification(t *testing.T) {
	ctx, pool := openOperationalReadinessTestDatabase(t, "AXIOM_OPERATIONAL_READINESS_UPGRADE_TEST_DSN")
	defer pool.Close()
	assertPostgres18(t, ctx, pool)
	assertEmptyTestDatabase(t, ctx, pool)
	applyTriangularArbitrageMigrationPrefix(t, ctx, pool, 26)
	migrations, err := Migrations()
	if err != nil || len(migrations) < 27 {
		t.Fatalf("operational readiness migration catalog=%d error=%v", len(migrations), err)
	}
	wantApplied := len(migrations) - 26
	if applied, applyErr := ApplyMigrations(ctx, pool); applyErr != nil || applied != wantApplied {
		t.Fatalf("operational-evidence-to-current migration applied=%d want=%d error=%v",
			applied, wantApplied, applyErr)
	}
	var level, source string
	if err = pool.QueryRow(ctx, `SELECT level,source_instance FROM owner_console_storage_pressure_state
WHERE scope_id='market-data'`).Scan(&level, &source); err != nil || level != "CRITICAL" || source != "migration-bootstrap" {
		t.Fatalf("operational readiness bootstrap posture=%s/%s error=%v", level, source, err)
	}
}

func assertOperationalReadinessPressureLifecycle(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	store, err := NewOperationalReadinessStoragePressureStore(pool, "operational_readiness-recorder-01")
	if err != nil {
		t.Fatal(err)
	}
	policy := pressure.Policy{HighFreeBytes: 10 << 30, CriticalFreeBytes: 5 << 30, SampleInterval: 15 * time.Second}
	now := time.Now().UTC().Add(time.Second)
	high, _ := policy.Classify(8<<30, 100<<30, now)
	state, transitioned, err := store.Observe(ctx, high, policy)
	if err != nil || !transitioned || state.Level != pressure.LevelHigh {
		t.Fatalf("high state=%+v transitioned=%t error=%v", state, transitioned, err)
	}
	assertOperationalReadinessHeavyWorkAllowed(t, ctx, pool, false, "at high")
	normal, _ := policy.Classify(20<<30, 100<<30, now.Add(time.Second))
	state, transitioned, err = store.Observe(ctx, normal, policy)
	if err != nil || !transitioned || state.Level != pressure.LevelNormal {
		t.Fatalf("normal state=%+v transitioned=%t error=%v", state, transitioned, err)
	}
	assertOperationalReadinessHeavyWorkAllowed(t, ctx, pool, true, "after recovery")
	if _, err = pool.Exec(ctx, `UPDATE owner_console_storage_pressure_state
SET observed_at=CURRENT_TIMESTAMP-interval '3 minutes' WHERE scope_id='market-data'`); err != nil {
		t.Fatal(err)
	}
	assertOperationalReadinessHeavyWorkAllowed(t, ctx, pool, false, "with stale pressure")
	critical, _ := policy.Classify(4<<30, 100<<30, now.Add(2*time.Second))
	state, _, err = store.Observe(ctx, critical, policy)
	if err != nil || state.Level != pressure.LevelCritical {
		t.Fatalf("critical state=%+v error=%v", state, err)
	}
	var severity, reason string
	if err = pool.QueryRow(ctx, `SELECT severity,reason_code FROM alerts WHERE id='storage-pressure-market-data'`).Scan(&severity, &reason); err != nil || severity != "critical" || reason != "storage.pressure.critical" {
		t.Fatalf("pressure alert=%s/%s error=%v", severity, reason, err)
	}
	if _, err = pool.Exec(ctx, `DELETE FROM owner_console_storage_pressure_events WHERE revision=$1`, state.Revision); err == nil {
		t.Fatal("immutable pressure evidence deleted")
	}
}

func assertOperationalReadinessHeavyWorkAllowed(t *testing.T, ctx context.Context, pool *pgxpool.Pool,
	want bool, posture string) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	allowed, gateErr := operationalReadinessHeavyWorkAllowed(ctx, tx)
	_ = tx.Rollback(ctx)
	if gateErr != nil || allowed != want {
		t.Fatalf("heavy work %s allowed=%t want=%t error=%v", posture, allowed, want, gateErr)
	}
}

func openOperationalReadinessTestDatabase(t *testing.T, environment string) (context.Context, *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv(environment)
	if dsn == "" {
		t.Skip(environment + " is not set")
	}
	configuration, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(configuration.ConnConfig.Database, "_operational_readiness_test") {
		t.Fatal("operational readiness integration requires a dedicated database ending _operational_readiness_test")
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
