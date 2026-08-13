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

func TestMultiExchangeConsolePostgresCleanInstallQualification(t *testing.T) {
	ctx, pool := openMultiExchangeConsoleTestDatabase(t, "AXIOM_MULTI_EXCHANGE_CONSOLE_TEST_DSN")
	defer pool.Close()
	assertPostgres18(t, ctx, pool)
	assertEmptyTestDatabase(t, ctx, pool)
	applyTriangularArbitrageMigrationPrefix(t, ctx, pool, 20)
	assertMultiExchangeConsoleSchemaAndCommands(t, ctx, pool)
}

func TestMultiExchangeConsolePostgresResearchPromotionToMultiExchangeConsoleUpgradeQualification(t *testing.T) {
	ctx, pool := openMultiExchangeConsoleTestDatabase(t, "AXIOM_MULTI_EXCHANGE_CONSOLE_UPGRADE_TEST_DSN")
	defer pool.Close()
	assertPostgres18(t, ctx, pool)
	assertEmptyTestDatabase(t, ctx, pool)
	applyTriangularArbitrageMigrationPrefix(t, ctx, pool, 19)
	if _, err := pool.Exec(ctx, `INSERT INTO assets(symbol) VALUES ('MultiExchangeConsoleUPGRADE')`); err != nil {
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
		t.Fatalf("research-promotion-to-console migration changed=%t error=%v", changed, err)
	}
	var sentinel int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM assets WHERE symbol='MultiExchangeConsoleUPGRADE'`).
		Scan(&sentinel); err != nil || sentinel != 1 {
		t.Fatalf("research promotion upgrade sentinel=%d error=%v", sentinel, err)
	}
	assertMultiExchangeConsoleSchemaAndCommands(t, ctx, pool)
}

func openMultiExchangeConsoleTestDatabase(t *testing.T, environment string) (context.Context, *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv(environment)
	if dsn == "" {
		t.Skip(environment + " is not set")
	}
	configuration, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(configuration.ConnConfig.Database, "_multi_exchange_console_test") {
		t.Fatal("multi-exchange console integration requires a dedicated database ending _multi_exchange_console_test")
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

func assertMultiExchangeConsoleSchemaAndCommands(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) {
	t.Helper()
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	clock, _ := domain.NewReplayClock(now)
	principal := multiExchangeConsoleQualificationPrincipal(t, ctx, pool, now)
	store, err := NewOwnerConsoleStore(pool, []byte(strings.Repeat("8", 32)), clock)
	if err != nil {
		t.Fatal(err)
	}
	assertMultiExchangeConsoleReadProjections(t, ctx, store)
	assertMultiExchangeConsoleMissingExport(t, ctx, store, principal)
	created := assertMultiExchangeConsoleFaultCommands(t, ctx, pool, store, principal, now)
	assertMultiExchangeConsoleFaultInjection(t, ctx, pool, created)
	assertMultiExchangeConsoleTables(t, ctx, pool)
}

func multiExchangeConsoleQualificationPrincipal(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	now time.Time,
) authentication.Principal {
	t.Helper()
	const userID = "multi_exchange_console-qualification-owner"
	const sessionID = "multi_exchange_console-qualification-session"
	if _, err := pool.Exec(ctx, `INSERT INTO users(
id,email,password_hash,status,created_at,normalized_email,password_changed_at
) VALUES ($1,'multi_exchange_console-owner@example.test','qualification-hash','active',$2,
  'multi_exchange_console-owner@example.test',$2)`, userID, now); err != nil {
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
		UserID: userID, Email: "multi_exchange_console-owner@example.test", SessionID: sessionID,
		ReauthenticatedAt: now, SessionRevision: 1,
	}
}

func assertMultiExchangeConsoleMissingExport(
	t *testing.T, ctx context.Context, store *OwnerConsoleStore,
	principal authentication.Principal,
) {
	t.Helper()
	_, err := store.ExportReport(ctx, principal, "missing-multi_exchange_console-report",
		"missing-multi_exchange_console-export", generated.ReportExportRequest{
			Format: generated.ReportExportRequestFormatJson,
		})
	if !errors.Is(err, console.ErrNotFound) {
		t.Fatalf("missing multi-exchange console report export error=%v", err)
	}
}

func assertMultiExchangeConsoleFaultCommands(
	t *testing.T, ctx context.Context, pool *pgxpool.Pool, store *OwnerConsoleStore,
	principal authentication.Principal, now time.Time,
) generated.ReplayFaultResource {
	t.Helper()
	var err error
	hash := strings.Repeat("8", 64)
	if _, err = pool.Exec(ctx, `INSERT INTO jobs(
	  id,job_type,idempotency_key,state,payload_hash,created_at,updated_at,
	  owner_user_id,request_payload
	) VALUES ('replay-multi_exchange_console-qualification','replay','replay-multi_exchange_console-qualification','QUEUED',
	  $1,$2,$2,$3,'{}')`, hash, now, principal.UserID); err != nil {
		t.Fatal(err)
	}
	body := generated.ReplayFaultRequest{Fault: generated.Latency, Ordinal: "7",
		DelayNanos: "1000000", ExpectedRevision: "0",
		Reason: "qualify deterministic latency injection"}
	created, err := store.ScheduleReplayFault(
		ctx, principal, "replay-multi_exchange_console-qualification", "fault-multi_exchange_console-qualification", body,
	)
	if err != nil || created.Revision != "1" || !created.SimulationOnly {
		t.Fatalf("multi-exchange console fault created=%#v error=%v", created, err)
	}
	repeated, err := store.ScheduleReplayFault(
		ctx, principal, "replay-multi_exchange_console-qualification", "fault-multi_exchange_console-qualification", body,
	)
	if err != nil || repeated.Id != created.Id {
		t.Fatalf("multi-exchange console idempotent replay=%#v error=%v", repeated, err)
	}
	changed := body
	changed.Ordinal = "8"
	if _, err = store.ScheduleReplayFault(
		ctx, principal, "replay-multi_exchange_console-qualification", "fault-multi_exchange_console-qualification", changed,
	); !errors.Is(err, console.ErrIdempotencyConflict) {
		t.Fatalf("multi-exchange console idempotency conflict=%v", err)
	}
	page, err := store.ReplayFaults(ctx, "replay-multi_exchange_console-qualification")
	if err != nil || len(page.Items) != 1 || page.Revision != "1" ||
		!page.SimulationOnly {
		t.Fatalf("multi-exchange console fault page=%#v error=%v", page, err)
	}
	if _, err = pool.Exec(ctx, `UPDATE multi_exchange_console_replay_fault_schedules
	  SET delay_nanos=2000000 WHERE id=$1`, created.Id); err == nil {
		t.Fatal("immutable multi-exchange console fault schedule updated")
	}
	return created
}

func assertMultiExchangeConsoleFaultInjection(
	t *testing.T, ctx context.Context, pool *pgxpool.Pool,
	created generated.ReplayFaultResource,
) {
	t.Helper()
	source, err := (&ownerConsoleMaterializer{pool: pool}).multiExchangeConsoleFaultSource(
		ctx, "replay-multi_exchange_console-qualification", &multiExchangeConsoleQualificationSource{},
	)
	if err != nil {
		t.Fatal(err)
	}
	event, ok, err := source.Next()
	if err != nil || !ok || event.Ordinal != 7 || event.LogicalTime != 1_000_001 {
		t.Fatalf("multi-exchange console materialized fault event=%#v ok=%t error=%v", event, ok, err)
	}
	var injected int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM audit_events
	  WHERE event_type='replay.fault.injected' AND causation_id=$1`, created.Id).
		Scan(&injected); err != nil || injected != 1 {
		t.Fatalf("multi-exchange console injected fault audit count=%d error=%v", injected, err)
	}
}

func assertMultiExchangeConsoleTables(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	for _, table := range []string{
		"multi_exchange_console_replay_fault_schedule_states", "multi_exchange_console_replay_fault_schedules", "multi_exchange_console_report_exports",
	} {
		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.tables
		  WHERE table_schema='public' AND table_name=$1`, table).Scan(&count); err != nil ||
			count != 1 {
			t.Fatalf("multi-exchange console table %s count=%d error=%v", table, count, err)
		}
	}
}

func assertMultiExchangeConsoleReadProjections(
	t *testing.T,
	ctx context.Context,
	store *OwnerConsoleStore,
) {
	t.Helper()
	status, err := store.SystemStatus(ctx)
	if err != nil || status.ApplicationVersion == "" || status.BuildCommit == "" ||
		status.ConfigurationIdentity == "" || status.ReadinessState == "" || bool(status.RealTradingEnabled) {
		t.Fatalf("multi-exchange console system status=%#v error=%v", status, err)
	}
	exchanges, err := store.Exchanges(ctx, "", 20)
	if err != nil || exchanges.SnapshotRevision == "" {
		t.Fatalf("multi-exchange console exchanges=%#v error=%v", exchanges, err)
	}
	opportunities, err := store.Opportunities(ctx, "", 20, "")
	if err != nil || opportunities.SnapshotRevision == "" {
		t.Fatalf("multi-exchange console opportunities=%#v error=%v", opportunities, err)
	}
	strategies, err := store.Strategies(ctx, "", 20)
	if err != nil || strategies.SnapshotRevision == "" {
		t.Fatalf("multi-exchange console strategies=%#v error=%v", strategies, err)
	}
	inventory, err := store.Inventory(ctx, "", 20, console.InventoryFilters{})
	if err != nil || inventory.SnapshotRevision == "" || inventory.CombinedBalance {
		t.Fatalf("multi-exchange console inventory=%#v error=%v", inventory, err)
	}
	rebalancing, err := store.Rebalancing(ctx, "", 20)
	if err != nil || rebalancing.SnapshotRevision == "" || rebalancing.ExecutionAvailable {
		t.Fatalf("multi-exchange console rebalancing=%#v error=%v", rebalancing, err)
	}
	research, err := store.ChampionChallenger(ctx, "", 20)
	if err != nil || research.SnapshotRevision == "" {
		t.Fatalf("multi-exchange console research=%#v error=%v", research, err)
	}
	if _, err = store.Opportunity(ctx, "missing-multi_exchange_console-opportunity"); !errors.Is(err, console.ErrNotFound) {
		t.Fatalf("missing multi-exchange console opportunity error=%v", err)
	}
	if _, err = store.RebalancingDetail(ctx, "missing-multi_exchange_console-rebalancing"); !errors.Is(err, console.ErrNotFound) {
		t.Fatalf("missing multi-exchange console rebalancing error=%v", err)
	}
	if _, err = store.ReplayFaults(ctx, "missing-multi_exchange_console-replay"); !errors.Is(err, console.ErrNotFound) {
		t.Fatalf("missing multi-exchange console replay error=%v", err)
	}
}

type multiExchangeConsoleQualificationSource struct {
	delivered bool
}

func (source *multiExchangeConsoleQualificationSource) Next() (replay.Event, bool, error) {
	if source.delivered {
		return replay.Event{}, false, nil
	}
	source.delivered = true
	return replay.Event{LogicalTime: 1, Ordinal: 7, Canonical: []byte("multi_exchange_console")}, true, nil
}

func (source *multiExchangeConsoleQualificationSource) SeekOrdinal(uint64) error {
	source.delivered = false
	return nil
}
