package postgres

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"axiom/internal/api/generated"
	"axiom/internal/domain"
	"axiom/internal/storage/pressure"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestV1DD5PostgresOperationalReadinessQualification(t *testing.T) {
	ctx, pool := openD5TestDatabase(t, "AXIOM_D5_TEST_DSN")
	defer pool.Close()
	assertPostgres18(t, ctx, pool)
	assertEmptyTestDatabase(t, ctx, pool)
	migrations, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	if applied, applyErr := ApplyMigrations(ctx, pool); applyErr != nil || applied != len(migrations) {
		t.Fatalf("D5 clean migration applied=%d want=%d error=%v", applied, len(migrations), applyErr)
	}
	assertD5PressureLifecycle(t, ctx, pool)
	assertD5ArtifactLifecycle(t, ctx, pool)
}

func assertD5ArtifactLifecycle(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	fixture := newD1IntegrationFixture(t, ctx, pool)
	at := time.Date(2030, 8, 3, 12, 0, 0, 0, time.UTC)
	var revision int64
	if err := pool.QueryRow(ctx, `INSERT INTO v1d_activity_projection(
id,view_kind,source_type,source_id,source_revision,reason_code,outcome,
correlation_id,occurred_at,details,projected_at
) VALUES('activity-d5-lifecycle','system_events','d5_test','source-d5','1',
'unknown','observed','d5-lifecycle',$1,'{}'::jsonb,$1) RETURNING activity_revision`, at).Scan(&revision); err != nil {
		t.Fatal(err)
	}
	request := generated.ExportRequest{ExpectedRevision: generated.Revision(strconv.FormatInt(revision, 10)),
		Format: generated.ExportRequestFormatJson, Reason: "verify automatic D5 lifecycle expiry",
		ResourceId: "activity-d5-lifecycle", ResourceType: generated.ExportRequestResourceTypeActivity}
	first, err := fixture.store.CreateD1Export(ctx, fixture.principal, "d5-lifecycle-export-1", request)
	if err != nil {
		t.Fatal(err)
	}
	held, err := fixture.store.CreateD1Export(ctx, fixture.principal, "d5-lifecycle-export-2", request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO incidents(id,severity,state,reason_code,owner_user_id,opened_at,updated_at)
VALUES('incident-d5-hold','warning','open','retention_review',$1,$2,$2)`, fixture.userID, at); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO v1d_artifact_holds(
id,artifact_id,hold_type,reference_id,reason,actor_user_id,created_at
) VALUES('hold-d5-lifecycle',$1,'incident','incident-d5-hold','preserve D5 lifecycle evidence',$2,$3)`, held.Id, fixture.userID, at); err != nil {
		t.Fatal(err)
	}
	clock, _ := domain.NewReplayClock(at.Add(8 * 24 * time.Hour))
	worker, _ := NewD5LifecycleWorker(pool, clock)
	worked, err := worker.RunOne(ctx)
	if err != nil || !worked {
		t.Fatalf("lifecycle worked=%t error=%v", worked, err)
	}
	var firstDeleted *time.Time
	var heldDeleted *time.Time
	if err = pool.QueryRow(ctx, `SELECT deleted_at FROM v1d_export_artifacts WHERE id=$1`, first.Id).Scan(&firstDeleted); err != nil || firstDeleted == nil {
		t.Fatalf("expired artifact deletion=%v error=%v", firstDeleted, err)
	}
	if err = pool.QueryRow(ctx, `SELECT deleted_at FROM v1d_export_artifacts WHERE id=$1`, held.Id).Scan(&heldDeleted); err != nil || heldDeleted != nil {
		t.Fatalf("held artifact deletion=%v error=%v", heldDeleted, err)
	}
}

func TestV1DD5PostgresD4ToD5UpgradeQualification(t *testing.T) {
	ctx, pool := openD5TestDatabase(t, "AXIOM_D5_UPGRADE_TEST_DSN")
	defer pool.Close()
	assertPostgres18(t, ctx, pool)
	assertEmptyTestDatabase(t, ctx, pool)
	applyB4MigrationPrefix(t, ctx, pool, 26)
	migrations, err := Migrations()
	if err != nil || len(migrations) != 27 {
		t.Fatalf("D5 migration catalog=%d error=%v", len(migrations), err)
	}
	connection, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Release()
	if changed, applyErr := applyMigration(ctx, connection, migrations[26]); applyErr != nil || !changed {
		t.Fatalf("D4-to-D5 migration changed=%t error=%v", changed, applyErr)
	}
	var level, source string
	if err = pool.QueryRow(ctx, `SELECT level,source_instance FROM v1d_storage_pressure_state
WHERE scope_id='market-data'`).Scan(&level, &source); err != nil || level != "CRITICAL" || source != "migration-bootstrap" {
		t.Fatalf("D5 bootstrap posture=%s/%s error=%v", level, source, err)
	}
}

func assertD5PressureLifecycle(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	store, err := NewD5StoragePressureStore(pool, "d5-recorder-01")
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
	assertD5HeavyWorkAllowed(t, ctx, pool, false, "at high")
	normal, _ := policy.Classify(20<<30, 100<<30, now.Add(time.Second))
	state, transitioned, err = store.Observe(ctx, normal, policy)
	if err != nil || !transitioned || state.Level != pressure.LevelNormal {
		t.Fatalf("normal state=%+v transitioned=%t error=%v", state, transitioned, err)
	}
	assertD5HeavyWorkAllowed(t, ctx, pool, true, "after recovery")
	if _, err = pool.Exec(ctx, `UPDATE v1d_storage_pressure_state
SET observed_at=CURRENT_TIMESTAMP-interval '3 minutes' WHERE scope_id='market-data'`); err != nil {
		t.Fatal(err)
	}
	assertD5HeavyWorkAllowed(t, ctx, pool, false, "with stale pressure")
	critical, _ := policy.Classify(4<<30, 100<<30, now.Add(2*time.Second))
	state, _, err = store.Observe(ctx, critical, policy)
	if err != nil || state.Level != pressure.LevelCritical {
		t.Fatalf("critical state=%+v error=%v", state, err)
	}
	var severity, reason string
	if err = pool.QueryRow(ctx, `SELECT severity,reason_code FROM alerts WHERE id='storage-pressure-market-data'`).Scan(&severity, &reason); err != nil || severity != "critical" || reason != "storage.pressure.critical" {
		t.Fatalf("pressure alert=%s/%s error=%v", severity, reason, err)
	}
	if _, err = pool.Exec(ctx, `DELETE FROM v1d_storage_pressure_events WHERE revision=$1`, state.Revision); err == nil {
		t.Fatal("immutable pressure evidence deleted")
	}
}

func assertD5HeavyWorkAllowed(t *testing.T, ctx context.Context, pool *pgxpool.Pool,
	want bool, posture string) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	allowed, gateErr := d5HeavyWorkAllowed(ctx, tx)
	_ = tx.Rollback(ctx)
	if gateErr != nil || allowed != want {
		t.Fatalf("heavy work %s allowed=%t want=%t error=%v", posture, allowed, want, gateErr)
	}
}

func openD5TestDatabase(t *testing.T, environment string) (context.Context, *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv(environment)
	if dsn == "" {
		t.Skip(environment + " is not set")
	}
	configuration, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(configuration.ConnConfig.Database, "_d5_test") {
		t.Fatal("D5 integration requires a dedicated database ending _d5_test")
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
