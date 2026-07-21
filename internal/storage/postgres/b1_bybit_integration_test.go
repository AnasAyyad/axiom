package postgres

import (
	"context"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestB1PostgresCleanInstallQualification(t *testing.T) {
	ctx, pool := openB1TestDatabase(t, "AXIOM_B1_TEST_DSN")
	defer pool.Close()
	assertPostgres18(t, ctx, pool)
	assertEmptyTestDatabase(t, ctx, pool)
	migrations, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	type result struct {
		applied int
		err     error
	}
	results := make(chan result, 2)
	var started sync.WaitGroup
	started.Add(2)
	for index := 0; index < 2; index++ {
		go func() {
			started.Done()
			started.Wait()
			applied, applyErr := ApplyMigrations(ctx, pool)
			results <- result{applied: applied, err: applyErr}
		}()
	}
	total := 0
	for index := 0; index < 2; index++ {
		outcome := <-results
		if outcome.err != nil {
			t.Fatal(outcome.err)
		}
		total += outcome.applied
	}
	if total != len(migrations) {
		t.Fatalf("concurrent clean migrations=%d want=%d", total, len(migrations))
	}
	if applied, applyErr := ApplyMigrations(ctx, pool); applyErr != nil || applied != 0 {
		t.Fatalf("idempotent migration=%d error=%v", applied, applyErr)
	}
	assertB1Schema(t, ctx, pool)
	assertB1ImmutableEvidence(t, ctx, pool)
	assertB1RoleGrants(t, ctx, pool)
}

func TestB1PostgresV1AToB1UpgradeQualification(t *testing.T) {
	ctx, pool := openB1TestDatabase(t, "AXIOM_B1_UPGRADE_TEST_DSN")
	defer pool.Close()
	assertPostgres18(t, ctx, pool)
	assertEmptyTestDatabase(t, ctx, pool)
	migrations, err := Migrations()
	if err != nil || len(migrations) < 13 {
		t.Fatalf("migration catalog=%d error=%v", len(migrations), err)
	}
	connection, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = ensureMigrationTable(ctx, connection); err != nil {
		connection.Release()
		t.Fatal(err)
	}
	for _, migration := range migrations[:11] {
		changed, applyErr := applyMigration(ctx, connection, migration)
		if applyErr != nil || !changed {
			connection.Release()
			t.Fatalf("V1A migration %s changed=%t error=%v", migration.Name, changed, applyErr)
		}
	}
	connection.Release()
	var bybitReferences int
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM exchanges WHERE id='bybit'").Scan(&bybitReferences); err != nil || bybitReferences != 0 {
		t.Fatalf("pre-upgrade Bybit references=%d error=%v", bybitReferences, err)
	}
	if applied, applyErr := ApplyMigrations(ctx, pool); applyErr != nil || applied != len(migrations)-11 {
		t.Fatalf("V1A-to-B1 migrations=%d/%d error=%v", applied, len(migrations)-11, applyErr)
	}
	assertB1Schema(t, ctx, pool)
	assertB1ImmutableEvidence(t, ctx, pool)
}

func openB1TestDatabase(t *testing.T, environment string) (context.Context, *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv(environment)
	if dsn == "" {
		t.Skip(environment + " is not set")
	}
	configuration, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(configuration.ConnConfig.Database, "_b1_test") {
		t.Fatal("B1 integration requires a dedicated database ending _b1_test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
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

func assertPostgres18(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var raw string
	err := pool.QueryRow(ctx, "SHOW server_version_num").Scan(&raw)
	version, parseErr := strconv.Atoi(raw)
	if err != nil || parseErr != nil || version < 180000 || version >= 190000 {
		t.Fatalf("PostgreSQL version=%d error=%v", version, err)
	}
}

func assertB1Schema(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM exchanges
WHERE id='bybit' AND environment='production_public'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("Bybit reference=%d error=%v", count, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns
WHERE table_schema='public' AND table_name='shadow_sessions' AND column_name='exchange_id' AND is_nullable='NO'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("shadow exchange FK column=%d error=%v", count, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_constraint constraint_row
JOIN pg_class table_row ON table_row.oid=constraint_row.conrelid
WHERE table_row.relname='shadow_sessions' AND constraint_row.contype='f'
AND pg_get_constraintdef(constraint_row.oid) LIKE 'FOREIGN KEY (exchange_id) REFERENCES exchanges(id)%'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("shadow exchange FK=%d error=%v", count, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_constraint constraint_row
JOIN pg_class table_row ON table_row.oid=constraint_row.conrelid
WHERE table_row.relname IN ('portfolio_ownership','shadow_sessions')
AND constraint_row.contype='c' AND (
  lower(pg_get_constraintdef(constraint_row.oid)) LIKE '%strategy_key%trend%' OR
  lower(pg_get_constraintdef(constraint_row.oid)) LIKE '%public_exchange%binance%'
)`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("legacy single-strategy/exchange checks=%d error=%v", count, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_trigger
WHERE NOT tgisinternal AND tgname IN (
  'portfolio_ownership_strategy_reference','shadow_public_exchange_reference',
  'public_clock_samples_immutable','public_connection_events_immutable'
)`).Scan(&count); err != nil || count != 4 {
		t.Fatalf("B1 relational/immutable triggers=%d error=%v", count, err)
	}
}

func assertB1ImmutableEvidence(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	statements := []struct {
		sql  string
		args []any
	}{
		{sql: "INSERT INTO assets(symbol) VALUES ('BTC'),('USDT') ON CONFLICT DO NOTHING"},
		{sql: "INSERT INTO instruments(id,base_asset,quote_asset,product) VALUES ('BTCUSDT','BTC','USDT','spot') ON CONFLICT DO NOTHING"},
		{sql: `INSERT INTO public_connection_events(id,exchange_id,instrument_id,recorder_session,connection_id,
connection_generation,state,reason,observed_at,ingest_ordinal)
VALUES ('event-1','bybit','BTCUSDT','session-1','connection-1',1,'HEALTHY','fixture',clock_timestamp(),1)`},
		{sql: `INSERT INTO public_clock_samples(id,exchange_id,instrument_id,recorder_session,connection_id,
connection_generation,observed_at,offset_nanoseconds,uncertainty_nanoseconds,eligible,raw_payload_hash)
			VALUES ('clock-1','bybit','BTCUSDT','session-1','connection-1',1,clock_timestamp(),0,1,true,$1)`,
			args: []any{strings.Repeat("a", 64)}},
	}
	for _, statement := range statements {
		if _, err := pool.Exec(ctx, statement.sql, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, "UPDATE public_connection_events SET reason='mutated' WHERE id='event-1'"); err == nil {
		t.Fatal("immutable public connection evidence accepted update")
	}
	if _, err := pool.Exec(ctx, "DELETE FROM public_clock_samples WHERE id='clock-1'"); err == nil {
		t.Fatal("immutable clock evidence accepted delete")
	}
}

func assertB1RoleGrants(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	roles := []string{"axiom_b1_runtime", "axiom_b1_recorder", "axiom_b1_readonly"}
	for _, role := range roles {
		if _, err := pool.Exec(ctx, "CREATE ROLE "+role); err != nil {
			t.Fatal(err)
		}
	}
	if err := ApplyRoleGrants(ctx, pool, roles[0], roles[1], roles[2]); err != nil {
		t.Fatal(err)
	}
	checks := []struct {
		role, table, privilege string
		want                   bool
	}{
		{roles[1], "public_clock_samples", "INSERT", true},
		{roles[1], "public_connection_events", "INSERT", true},
		{roles[1], "orders", "SELECT", false},
		{roles[2], "public_connection_events", "SELECT", true},
		{roles[2], "public_connection_events", "UPDATE", false},
	}
	for _, check := range checks {
		var allowed bool
		if err := pool.QueryRow(ctx, "SELECT has_table_privilege($1,$2,$3)", check.role, check.table, check.privilege).Scan(&allowed); err != nil || allowed != check.want {
			t.Fatalf("role=%s table=%s privilege=%s allowed=%t error=%v", check.role, check.table, check.privilege, allowed, err)
		}
	}
}
