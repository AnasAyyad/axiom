package postgres

import (
	"context"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestA4PostgresMigrationJournalAndReservationIntegration(t *testing.T) {
	dsn := os.Getenv("AXIOM_A4_TEST_DSN")
	if dsn == "" {
		t.Skip("AXIOM_A4_TEST_DSN is not set")
	}
	configuration, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(configuration.ConnConfig.Database, "_a4_test") {
		t.Fatal("A4 integration requires a dedicated database ending _a4_test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, configuration)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	assertEmptyTestDatabase(t, ctx, pool)
	migrations, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	if applied, applyErr := ApplyMigrations(ctx, pool); applyErr != nil || applied != len(migrations) {
		t.Fatalf("initial migration = %d, %v", applied, applyErr)
	}
	if applied, applyErr := ApplyMigrations(ctx, pool); applyErr != nil || applied != 0 {
		t.Fatalf("idempotent migration = %d, %v", applied, applyErr)
	}
	runtimeRole := testRole("AXIOM_A4_RUNTIME_ROLE", "axiom_runtime")
	recorderRole := testRole("AXIOM_A4_RECORDER_ROLE", "axiom_recorder")
	readOnlyRole := testRole("AXIOM_A4_READONLY_ROLE", "axiom_readonly")
	if err = ApplyRoleGrants(ctx, pool, runtimeRole, recorderRole, readOnlyRole); err != nil {
		t.Fatalf("role grants failed: %v", err)
	}
	assertRoleMatrix(t, ctx, pool, runtimeRole, recorderRole, readOnlyRole)
	seedAccountingReferences(t, ctx, pool)
	assertSchemaHistoryGuards(t, ctx, pool)
	assertRunLifecycle(t, ctx, pool)
	assertJournalConstraint(t, ctx, pool)
	assertTransactionalRecovery(t, ctx, pool)
	assertCoordinationGuards(t, ctx, pool)
	assertConcurrentReservation(t, ctx, pool)
	assertReservationGuards(t, ctx, pool)
	assertOrderGuards(t, ctx, pool)
}

func testRole(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func assertRoleMatrix(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	runtimeRole, recorderRole, readOnlyRole string,
) {
	t.Helper()
	checks := []struct {
		role, table, privilege string
		want                   bool
	}{
		{runtimeRole, "ledger_entries", "INSERT", true},
		{runtimeRole, "ledger_entries", "UPDATE", false},
		{runtimeRole, "virtual_balances", "UPDATE", true},
		{runtimeRole, "execution_leases", "DELETE", true},
		{recorderRole, "assets", "SELECT", true},
		{recorderRole, "market_data_segments", "INSERT", true},
		{recorderRole, "orders", "SELECT", false},
		{recorderRole, "users", "SELECT", false},
		{readOnlyRole, "journal_transactions", "SELECT", true},
		{readOnlyRole, "sessions", "SELECT", false},
		{readOnlyRole, "journal_transactions", "INSERT", false},
	}
	for _, check := range checks {
		var allowed bool
		err := pool.QueryRow(ctx, "SELECT has_table_privilege($1,$2,$3)", check.role, check.table, check.privilege).Scan(&allowed)
		if err != nil || allowed != check.want {
			t.Fatalf("role privilege %s %s %s = %t, %v", check.role, check.privilege, check.table, allowed, err)
		}
	}
}

func assertSchemaHistoryGuards(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	now := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)
	_, err := pool.Exec(ctx, `INSERT INTO asset_screening_versions
      (id,asset_symbol,version,prior_status,status,actor,reason,causation_id,configuration_id,effective_at,recorded_at)
      VALUES ('screening-1','BTC',1,NULL,'approved','test','initial','cause','configuration-a',$1,$1)`, now)
	if err != nil {
		t.Fatalf("valid screening history rejected: %v", err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO asset_screening_versions
      (id,asset_symbol,version,prior_status,status,actor,reason,causation_id,configuration_id,effective_at,recorded_at)
      VALUES ('screening-3','BTC',3,'approved','blocked','test','invalid jump','cause','configuration-a',$1,$1)`, now); err == nil {
		t.Fatal("non-contiguous screening history accepted")
	}
	if _, err = pool.Exec(ctx, "UPDATE strategy_versions SET promotion_status='candidate' WHERE id='strategy-version-a'"); err != nil {
		t.Fatalf("valid pre-use promotion rejected: %v", err)
	}
	if _, err = pool.Exec(ctx, "UPDATE strategy_versions SET used_at=$1 WHERE id='strategy-version-a'", now); err != nil {
		t.Fatalf("first-use lock rejected: %v", err)
	}
	if _, err = pool.Exec(ctx, "UPDATE strategy_versions SET promotion_status='locked_test' WHERE id='strategy-version-a'"); err == nil {
		t.Fatal("used strategy version mutated")
	}
	if _, err = pool.Exec(ctx, `INSERT INTO positions
      (account_id,instrument_id,quantity,weighted_average_cost,realized_pnl,revision,updated_at)
      VALUES ('account-a','instrument-a',-1,1,0,1,$1)`, now); err == nil {
		t.Fatal("negative spot position accepted")
	}
	hash := strings.Repeat("b", 64)
	if _, err = pool.Exec(ctx, `INSERT INTO dataset_manifests
      (id,dataset_hash,schema_compatibility,coverage_start,coverage_end,state,created_at)
      VALUES ('dataset-a',$1,'market-wire.v1',$2,$3,'building',$2)`, hash, now, now.Add(time.Hour)); err != nil {
		t.Fatalf("dataset seed failed: %v", err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO dataset_gaps
      (id,dataset_id,first_ordinal,last_ordinal,reason_code,detected_at)
      VALUES ('gap-a','dataset-a',10,20,'loss',$1)`, now); err != nil {
		t.Fatalf("valid dataset gap rejected: %v", err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO dataset_gaps
      (id,dataset_id,first_ordinal,last_ordinal,reason_code,detected_at)
      VALUES ('gap-b','dataset-a',20,30,'overlap',$1)`, now); err == nil {
		t.Fatal("overlapping dataset gap accepted")
	}
}

func assertEmptyTestDatabase(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var tables int
	if err := pool.QueryRow(ctx,
		"SELECT count(*) FROM pg_tables WHERE schemaname = 'public'").Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if tables != 0 {
		t.Fatal("dedicated A4 integration database is not empty")
	}
}

func assertRunLifecycle(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	now := time.Date(2026, 7, 14, 10, 1, 0, 0, time.UTC)
	if _, err := pool.Exec(ctx, "UPDATE runs SET root_seed_hash=$1 WHERE id='run-a'", strings.Repeat("b", 64)); err == nil {
		t.Fatal("run reproducibility identity mutated")
	}
	if _, err := pool.Exec(ctx, "UPDATE runs SET state='running',started_at=$1 WHERE id='run-a'", now); err != nil {
		t.Fatalf("legal run start rejected: %v", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE runs SET state='created' WHERE id='run-a'"); err == nil {
		t.Fatal("run moved backward to created")
	}
	if _, err := pool.Exec(ctx, "UPDATE runs SET state='completed',completed_at=$1 WHERE id='run-a'", now.Add(time.Minute)); err != nil {
		t.Fatalf("legal run completion rejected: %v", err)
	}
}

func seedAccountingReferences(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	now := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)
	hash := strings.Repeat("a", 64)
	statements := []struct {
		sql  string
		args []any
	}{
		{"INSERT INTO configuration_versions VALUES ($1,1,$2,$3,$4,$5)", []any{"configuration-a", hash, []byte("{}"), "test", now}},
		{"INSERT INTO assets(symbol) VALUES ('USDT'),('BTC')", nil},
		{"INSERT INTO exchanges VALUES ('exchange-a','emulator','emulator')", nil},
		{"INSERT INTO instruments VALUES ('instrument-a','BTC','USDT','spot')", nil},
		{"INSERT INTO strategy_definitions VALUES ('strategy-a','trend','trend')", nil},
		{"INSERT INTO strategy_versions VALUES ('strategy-version-a','strategy-a',1,$1,'research',NULL,$2)", []any{hash, now}},
		{"INSERT INTO runs VALUES ('run-a','shadow','configuration-a','strategy-version-a',NULL,$1,$1,'created',$2,NULL,NULL)", []any{hash, now}},
		{"INSERT INTO portfolios VALUES ('portfolio-a','portfolio','USDT',$1)", []any{now}},
		{"INSERT INTO virtual_accounts VALUES ('account-a','portfolio-a','run-a','main',$1)", []any{now}},
		{"INSERT INTO virtual_balances VALUES ('account-a','USDT',500,0,1,$1),('account-a','BTC',2,0,1,$1)", []any{now}},
	}
	for _, statement := range statements {
		if _, err := pool.Exec(ctx, statement.sql, statement.args...); err != nil {
			t.Fatalf("seed failed: %v", err)
		}
	}
}

func assertJournalConstraint(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	now := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = tx.Exec(ctx, `INSERT INTO journal_transactions
      (id,transaction_type,run_id,portfolio_id,configuration_id,causation_id,correlation_id,recorded_at,ingest_ordinal)
      VALUES ('journal-unbalanced','test','run-a','portfolio-a','configuration-a','cause','correlation',$1,1)`, now)
	_, _ = tx.Exec(ctx, `INSERT INTO ledger_entries
      (transaction_id,line_number,account_class,account_owner,asset_symbol,direction,quantity)
      VALUES ('journal-unbalanced',1,'available_asset','account-a','USDT','debit',1)`)
	if err = tx.Commit(ctx); err == nil {
		t.Fatal("unbalanced database journal committed")
	}
	tx, _ = pool.Begin(ctx)
	_, err = tx.Exec(ctx, `INSERT INTO journal_transactions
      (id,transaction_type,run_id,portfolio_id,configuration_id,causation_id,correlation_id,recorded_at,ingest_ordinal)
      VALUES ('journal-balanced','test','run-a','portfolio-a','configuration-a','cause','correlation',$1,2)`, now)
	if err == nil {
		_, err = tx.Exec(ctx, `INSERT INTO ledger_entries
        (transaction_id,line_number,account_class,account_owner,asset_symbol,direction,quantity) VALUES
        ('journal-balanced',1,'available_asset','account-a','USDT','debit',1),
        ('journal-balanced',2,'external_equity','market','USDT','credit',1)`)
	}
	if err != nil || tx.Commit(ctx) != nil {
		t.Fatalf("balanced database journal rejected: %v", err)
	}
	assertJournalSealed(t, ctx, pool)
	assertJournalReversals(t, ctx, pool, now)
}

func assertJournalSealed(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var sealed bool
	if err := pool.QueryRow(ctx, "SELECT sealed FROM journal_transactions WHERE id='journal-balanced'").Scan(&sealed); err != nil || !sealed {
		t.Fatalf("committed journal was not sealed: %t, %v", sealed, err)
	}
	_, err := pool.Exec(ctx, `INSERT INTO ledger_entries
      (transaction_id,line_number,account_class,account_owner,asset_symbol,direction,quantity) VALUES
      ('journal-balanced',3,'available_asset','account-a','USDT','debit',1),
      ('journal-balanced',4,'external_equity','market','USDT','credit',1)`)
	if err == nil {
		t.Fatal("lines appended to committed journal")
	}
	if _, err = pool.Exec(ctx, "UPDATE journal_transactions SET transaction_type='rewritten' WHERE id='journal-balanced'"); err == nil {
		t.Fatal("committed journal header mutated")
	}
}

func assertJournalReversals(t *testing.T, ctx context.Context, pool *pgxpool.Pool, now time.Time) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO journal_transactions
      (id,transaction_type,run_id,portfolio_id,configuration_id,causation_id,correlation_id,reversal_of,recorded_at,ingest_ordinal)
      VALUES ('journal-reversal','reversal','run-a','portfolio-a','configuration-a','cause','correlation','journal-balanced',$1,3)`, now)
	if err == nil {
		_, err = tx.Exec(ctx, `INSERT INTO ledger_entries
        (transaction_id,line_number,account_class,account_owner,asset_symbol,direction,quantity) VALUES
        ('journal-reversal',1,'available_asset','account-a','USDT','credit',1),
        ('journal-reversal',2,'external_equity','market','USDT','debit',1)`)
	}
	if err != nil || tx.Commit(ctx) != nil {
		t.Fatalf("exact database reversal rejected: %v", err)
	}
	tx, _ = pool.Begin(ctx)
	_, err = tx.Exec(ctx, `INSERT INTO journal_transactions
      (id,transaction_type,run_id,portfolio_id,configuration_id,causation_id,correlation_id,recorded_at,ingest_ordinal)
      VALUES ('journal-second','test','run-a','portfolio-a','configuration-a','cause','correlation',$1,4)`, now)
	if err == nil {
		_, err = tx.Exec(ctx, `INSERT INTO ledger_entries
        (transaction_id,line_number,account_class,account_owner,asset_symbol,direction,quantity) VALUES
        ('journal-second',1,'available_asset','account-a','USDT','debit',2),
        ('journal-second',2,'external_equity','market','USDT','credit',2)`)
	}
	if err != nil || tx.Commit(ctx) != nil {
		t.Fatalf("second database journal rejected: %v", err)
	}
	tx, _ = pool.Begin(ctx)
	_, err = tx.Exec(ctx, `INSERT INTO journal_transactions
      (id,transaction_type,run_id,portfolio_id,configuration_id,causation_id,correlation_id,reversal_of,recorded_at,ingest_ordinal)
      VALUES ('journal-bad-reversal','reversal','run-a','portfolio-a','configuration-a','cause','correlation','journal-second',$1,5)`, now)
	if err == nil {
		_, err = tx.Exec(ctx, `INSERT INTO ledger_entries
        (transaction_id,line_number,account_class,account_owner,asset_symbol,direction,quantity) VALUES
        ('journal-bad-reversal',1,'available_asset','account-a','USDT','credit',1),
        ('journal-bad-reversal',2,'external_equity','market','USDT','debit',1)`)
	}
	if err == nil && tx.Commit(ctx) == nil {
		t.Fatal("balanced but non-opposite database reversal committed")
	}
}

func assertConcurrentReservation(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var successes atomic.Int32
	var group sync.WaitGroup
	for _, reservationID := range []string{"reservation-a", "reservation-b"} {
		group.Add(1)
		go func() {
			defer group.Done()
			if tryDatabaseReservation(ctx, pool, reservationID) == nil {
				successes.Add(1)
			}
		}()
	}
	group.Wait()
	if successes.Load() != 1 {
		t.Fatalf("successful database reservations = %d", successes.Load())
	}
	var available, reserved string
	if err := pool.QueryRow(ctx,
		"SELECT available::text,reserved::text FROM virtual_balances WHERE account_id='account-a' AND asset_symbol='USDT'").Scan(&available, &reserved); err != nil {
		t.Fatal(err)
	}
	if available != "100.000000000000000000" || reserved != "400.000000000000000000" {
		t.Fatalf("database balance = %s/%s", available, reserved)
	}
}

func tryDatabaseReservation(ctx context.Context, pool *pgxpool.Pool, reservationID string) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	result, err := tx.Exec(ctx, `UPDATE virtual_balances
      SET available=available-400,reserved=reserved+400,revision=revision+1,updated_at=clock_timestamp()
      WHERE account_id='account-a' AND asset_symbol='USDT' AND available>=400`)
	if err != nil || result.RowsAffected() != 1 {
		return errReservationRejected
	}
	_, err = tx.Exec(ctx, `INSERT INTO reservations
      (id,account_id,asset_symbol,quantity,state,fencing_token,revision,created_at,updated_at)
      VALUES ($1,'account-a','USDT',400,'active',1,1,clock_timestamp(),clock_timestamp())`, reservationID)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

var errReservationRejected = &reservationRejectedError{}

type reservationRejectedError struct{}

func (*reservationRejectedError) Error() string { return "reservation_rejected" }

func assertReservationGuards(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var reservationID string
	if err := pool.QueryRow(ctx, "SELECT id FROM reservations WHERE state='active' AND asset_symbol='USDT'").Scan(&reservationID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "UPDATE reservations SET quantity=399 WHERE id=$1", reservationID); err == nil {
		t.Fatal("reservation quantity mutated")
	}
	if _, err := pool.Exec(ctx, `UPDATE reservations
		SET state='quarantined',revision=2,updated_at=updated_at + interval '1 microsecond' WHERE id=$1`, reservationID); err != nil {
		t.Fatalf("reservation quarantine rejected: %v", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE reservations SET state='released',revision=3 WHERE id=$1", reservationID); err == nil {
		t.Fatal("quarantined reservation released")
	}
}

func assertOrderGuards(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	now := time.Date(2026, 7, 14, 10, 2, 0, 0, time.UTC)
	_, err := pool.Exec(ctx, `INSERT INTO orders
      (id,account_id,client_order_id,account_epoch,instrument_id,side,quantity,state,revision,created_at,updated_at)
      VALUES ('order-a','account-a','client-a',1,'instrument-a','buy',1,'created',1,$1,$1)`, now)
	if err != nil {
		t.Fatalf("order seed failed: %v", err)
	}
	if _, err = pool.Exec(ctx, "UPDATE orders SET quantity=2 WHERE id='order-a'"); err == nil {
		t.Fatal("order identity mutated")
	}
	if _, err = pool.Exec(ctx, "UPDATE orders SET state='validating',revision=2,last_event_ordinal=1,updated_at=$1 WHERE id='order-a'", now.Add(time.Second)); err != nil {
		t.Fatalf("legal order transition rejected: %v", err)
	}
	if _, err = pool.Exec(ctx, "UPDATE orders SET state='reserved',revision=4,last_event_ordinal=2,updated_at=$1 WHERE id='order-a'", now.Add(2*time.Second)); err == nil {
		t.Fatal("order revision skipped")
	}
}
