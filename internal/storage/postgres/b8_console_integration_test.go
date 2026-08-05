package postgres

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"axiom/internal/api/console"
	"axiom/internal/api/generated"
	"axiom/internal/authentication"
	"axiom/internal/domain"
	"axiom/internal/replay"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestB8PostgresCleanInstallQualification(t *testing.T) {
	ctx, pool := openB8TestDatabase(t, "AXIOM_B8_TEST_DSN")
	defer pool.Close()
	assertPostgres18(t, ctx, pool)
	assertEmptyTestDatabase(t, ctx, pool)
	applyB4MigrationPrefix(t, ctx, pool, 20)
	assertB8SchemaAndCommands(t, ctx, pool)
}

func TestB8PostgresB7ToB8UpgradeQualification(t *testing.T) {
	ctx, pool := openB8TestDatabase(t, "AXIOM_B8_UPGRADE_TEST_DSN")
	defer pool.Close()
	assertPostgres18(t, ctx, pool)
	assertEmptyTestDatabase(t, ctx, pool)
	applyB4MigrationPrefix(t, ctx, pool, 19)
	if _, err := pool.Exec(ctx, `INSERT INTO assets(symbol) VALUES ('B8UPGRADE')`); err != nil {
		t.Fatal(err)
	}
	connection, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Release()
	migrations, err := Migrations()
	if err != nil || len(migrations) < 20 {
		t.Fatalf("migration catalog=%d error=%v", len(migrations), err)
	}
	changed, err := applyMigration(ctx, connection, migrations[19])
	if err != nil || !changed {
		t.Fatalf("B7-to-B8 migration changed=%t error=%v", changed, err)
	}
	var sentinel int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM assets WHERE symbol='B8UPGRADE'`).
		Scan(&sentinel); err != nil || sentinel != 1 {
		t.Fatalf("B7 upgrade sentinel=%d error=%v", sentinel, err)
	}
	assertB8SchemaAndCommands(t, ctx, pool)
}

func openB8TestDatabase(t *testing.T, environment string) (context.Context, *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv(environment)
	if dsn == "" {
		t.Skip(environment + " is not set")
	}
	configuration, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(configuration.ConnConfig.Database, "_b8_test") {
		t.Fatal("B8 integration requires a dedicated database ending _b8_test")
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

func assertB8SchemaAndCommands(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) {
	t.Helper()
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	clock, _ := domain.NewReplayClock(now)
	principal := b8QualificationPrincipal(t, ctx, pool, now)
	store, err := NewA11ConsoleStore(pool, []byte(strings.Repeat("8", 32)), clock)
	if err != nil {
		t.Fatal(err)
	}
	assertB8ReadProjections(t, ctx, store)
	assertB8MissingExport(t, ctx, store, principal)
	created := assertB8FaultCommands(t, ctx, pool, store, principal, now)
	assertB8FaultInjection(t, ctx, pool, created)
	assertB8Tables(t, ctx, pool)
}

func b8QualificationPrincipal(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	now time.Time,
) authentication.Principal {
	t.Helper()
	const userID = "b8-qualification-owner"
	const sessionID = "b8-qualification-session"
	if _, err := pool.Exec(ctx, `INSERT INTO users(
id,email,password_hash,status,created_at,normalized_email,password_changed_at
) VALUES ($1,'b8-owner@example.test','qualification-hash','active',$2,
  'b8-owner@example.test',$2)`, userID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO user_roles(user_id,role_id,granted_at) VALUES ($1,'owner',$2)`,
		userID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO sessions(
  id,user_id,token_hash,created_at,expires_at,csrf_token_hash,last_seen_at,
  idle_expires_at,reauthenticated_at,revision
) VALUES ($2,$1,$4,$3::timestamptz,$3::timestamptz+interval '1 hour',$5,
  $3::timestamptz,$3::timestamptz+interval '30 minutes',$3::timestamptz,1)`,
		userID, sessionID, now, strings.Repeat("8", 64), strings.Repeat("9", 64)); err != nil {
		t.Fatal(err)
	}
	return authentication.Principal{
		UserID: userID, Email: "b8-owner@example.test", SessionID: sessionID,
		Roles: []string{"owner"}, Permissions: []string{
			"operations.read", "commands.write", "incident.raw", "audit.raw", "research.promote",
		},
		ReauthenticatedAt: now, SessionRevision: 1,
	}
}

func assertB8MissingExport(
	t *testing.T, ctx context.Context, store *A11ConsoleStore,
	principal authentication.Principal,
) {
	t.Helper()
	_, err := store.ExportReport(ctx, principal, "missing-b8-report",
		"missing-b8-export", generated.ReportExportRequest{
			Format: generated.ReportExportRequestFormatJson,
		})
	if !errors.Is(err, console.ErrNotFound) {
		t.Fatalf("missing B8 report export error=%v", err)
	}
}

func assertB8FaultCommands(
	t *testing.T, ctx context.Context, pool *pgxpool.Pool, store *A11ConsoleStore,
	principal authentication.Principal, now time.Time,
) generated.ReplayFaultResource {
	t.Helper()
	var err error
	hash := strings.Repeat("8", 64)
	if _, err = pool.Exec(ctx, `INSERT INTO jobs(
	  id,job_type,idempotency_key,state,payload_hash,created_at,updated_at,
	  owner_user_id,request_payload
	) VALUES ('replay-b8-qualification','replay','replay-b8-qualification','QUEUED',
	  $1,$2,$2,$3,'{}')`, hash, now, principal.UserID); err != nil {
		t.Fatal(err)
	}
	body := generated.ReplayFaultRequest{Fault: generated.Latency, Ordinal: "7",
		DelayNanos: "1000000", ExpectedRevision: "0",
		Reason: "qualify deterministic latency injection"}
	created, err := store.ScheduleReplayFault(
		ctx, principal, "replay-b8-qualification", "fault-b8-qualification", body,
	)
	if err != nil || created.Revision != "1" || !created.SimulationOnly {
		t.Fatalf("B8 fault created=%#v error=%v", created, err)
	}
	repeated, err := store.ScheduleReplayFault(
		ctx, principal, "replay-b8-qualification", "fault-b8-qualification", body,
	)
	if err != nil || repeated.Id != created.Id {
		t.Fatalf("B8 idempotent replay=%#v error=%v", repeated, err)
	}
	changed := body
	changed.Ordinal = "8"
	if _, err = store.ScheduleReplayFault(
		ctx, principal, "replay-b8-qualification", "fault-b8-qualification", changed,
	); !errors.Is(err, console.ErrIdempotencyConflict) {
		t.Fatalf("B8 idempotency conflict=%v", err)
	}
	page, err := store.ReplayFaults(ctx, "replay-b8-qualification")
	if err != nil || len(page.Items) != 1 || page.Revision != "1" ||
		!page.SimulationOnly {
		t.Fatalf("B8 fault page=%#v error=%v", page, err)
	}
	if _, err = pool.Exec(ctx, `UPDATE b8_replay_fault_schedules
	  SET delay_nanos=2000000 WHERE id=$1`, created.Id); err == nil {
		t.Fatal("immutable B8 fault schedule updated")
	}
	return created
}

func assertB8FaultInjection(
	t *testing.T, ctx context.Context, pool *pgxpool.Pool,
	created generated.ReplayFaultResource,
) {
	t.Helper()
	source, err := (&a11Materializer{pool: pool}).b8FaultSource(
		ctx, "replay-b8-qualification", &b8QualificationSource{},
	)
	if err != nil {
		t.Fatal(err)
	}
	event, ok, err := source.Next()
	if err != nil || !ok || event.Ordinal != 7 || event.LogicalTime != 1_000_001 {
		t.Fatalf("B8 materialized fault event=%#v ok=%t error=%v", event, ok, err)
	}
	var injected int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM audit_events
	  WHERE event_type='replay.fault.injected' AND causation_id=$1`, created.Id).
		Scan(&injected); err != nil || injected != 1 {
		t.Fatalf("B8 injected fault audit count=%d error=%v", injected, err)
	}
}

func assertB8Tables(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	for _, table := range []string{
		"b8_replay_fault_schedule_states", "b8_replay_fault_schedules", "b8_report_exports",
	} {
		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.tables
		  WHERE table_schema='public' AND table_name=$1`, table).Scan(&count); err != nil ||
			count != 1 {
			t.Fatalf("B8 table %s count=%d error=%v", table, count, err)
		}
	}
}

func assertB8ReadProjections(
	t *testing.T,
	ctx context.Context,
	store *A11ConsoleStore,
) {
	t.Helper()
	status, err := store.SystemStatus(ctx)
	if err != nil || status.Release != generated.V1B ||
		status.Phase != generated.SystemStatusPhaseB8 || bool(status.RealTradingEnabled) {
		t.Fatalf("B8 system status=%#v error=%v", status, err)
	}
	exchanges, err := store.Exchanges(ctx, "", 20)
	if err != nil || exchanges.SnapshotRevision == "" {
		t.Fatalf("B8 exchanges=%#v error=%v", exchanges, err)
	}
	opportunities, err := store.Opportunities(ctx, "", 20, "")
	if err != nil || opportunities.SnapshotRevision == "" {
		t.Fatalf("B8 opportunities=%#v error=%v", opportunities, err)
	}
	strategies, err := store.Strategies(ctx, "", 20)
	if err != nil || strategies.SnapshotRevision == "" {
		t.Fatalf("B8 strategies=%#v error=%v", strategies, err)
	}
	inventory, err := store.Inventory(ctx, "", 20, console.InventoryFilters{})
	if err != nil || inventory.SnapshotRevision == "" || inventory.CombinedBalance {
		t.Fatalf("B8 inventory=%#v error=%v", inventory, err)
	}
	rebalancing, err := store.Rebalancing(ctx, "", 20)
	if err != nil || rebalancing.SnapshotRevision == "" || rebalancing.ExecutionAvailable {
		t.Fatalf("B8 rebalancing=%#v error=%v", rebalancing, err)
	}
	research, err := store.ChampionChallenger(ctx, "", 20)
	if err != nil || research.SnapshotRevision == "" {
		t.Fatalf("B8 research=%#v error=%v", research, err)
	}
	if _, err = store.Opportunity(ctx, "missing-b8-opportunity"); !errors.Is(err, console.ErrNotFound) {
		t.Fatalf("missing B8 opportunity error=%v", err)
	}
	if _, err = store.RebalancingDetail(ctx, "missing-b8-rebalancing"); !errors.Is(err, console.ErrNotFound) {
		t.Fatalf("missing B8 rebalancing error=%v", err)
	}
	if _, err = store.ReplayFaults(ctx, "missing-b8-replay"); !errors.Is(err, console.ErrNotFound) {
		t.Fatalf("missing B8 replay error=%v", err)
	}
}

type b8QualificationSource struct {
	delivered bool
}

func (source *b8QualificationSource) Next() (replay.Event, bool, error) {
	if source.delivered {
		return replay.Event{}, false, nil
	}
	source.delivered = true
	return replay.Event{LogicalTime: 1, Ordinal: 7, Canonical: []byte("b8")}, true, nil
}

func (source *b8QualificationSource) SeekOrdinal(uint64) error {
	source.delivered = false
	return nil
}
