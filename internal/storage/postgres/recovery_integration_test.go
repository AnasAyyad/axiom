package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func assertTransactionalRecovery(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	applied, err := applyRecoveryUnit(ctx, pool, false)
	if err != nil || !applied {
		t.Fatalf("pre-commit recovery injection failed: %t, %v", applied, err)
	}
	assertRecoveryUnitState(t, ctx, pool, 0, "2.000000000000000000", "0.000000000000000000")
	applied, err = applyRecoveryUnit(ctx, pool, true)
	if err != nil || !applied {
		t.Fatalf("recovery unit commit failed: %t, %v", applied, err)
	}
	applied, err = applyRecoveryUnit(ctx, pool, true)
	if err != nil || applied {
		t.Fatalf("recovery unit retry was not idempotent: %t, %v", applied, err)
	}
	assertRecoveryUnitState(t, ctx, pool, 4, "1.000000000000000000", "1.000000000000000000")
}

func applyRecoveryUnit(ctx context.Context, pool *pgxpool.Pool, commit bool) (bool, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	now := time.Date(2026, 7, 14, 10, 3, 0, 0, time.UTC)
	var identity string
	err = tx.QueryRow(ctx, `INSERT INTO inbox_events (consumer,message_id,payload_hash,consumed_at)
      VALUES ('recovery-test','message-unit',$1,$2) ON CONFLICT DO NOTHING RETURNING message_id`,
		fixedHash("c"), now).Scan(&identity)
	if err == pgx.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err = writeRecoveryFacts(ctx, tx, now); err != nil {
		return false, err
	}
	if !commit {
		return true, tx.Rollback(ctx)
	}
	return true, tx.Commit(ctx)
}

func writeRecoveryFacts(ctx context.Context, tx pgx.Tx, now time.Time) error {
	statements := []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO outbox_events (id,topic,payload_hash,created_at)
          VALUES ('outbox-unit','accounting.committed',$1,$2)`, []any{fixedHash("d"), now}},
		{`UPDATE virtual_balances SET available=available-1,reserved=reserved+1,revision=revision+1,updated_at=$1
          WHERE account_id='account-a' AND asset_symbol='BTC' AND available>=1`, []any{now}},
		{`INSERT INTO reservations (id,account_id,asset_symbol,quantity,state,fencing_token,revision,created_at,updated_at)
          VALUES ('reservation-unit','account-a','BTC',1,'active',2,1,$1,$1)`, []any{now}},
		{`INSERT INTO journal_transactions
          (id,transaction_type,run_id,portfolio_id,configuration_id,causation_id,correlation_id,recorded_at,ingest_ordinal)
          VALUES ('journal-unit','reserve','run-a','portfolio-a','configuration-a','message-unit','recovery-test',$1,6)`, []any{now}},
		{`INSERT INTO ledger_entries
          (transaction_id,line_number,account_class,account_owner,asset_symbol,direction,quantity) VALUES
          ('journal-unit',1,'reserved_asset','account-a','BTC','debit',1),
          ('journal-unit',2,'available_asset','account-a','BTC','credit',1)`, nil},
	}
	for _, statement := range statements {
		result, err := tx.Exec(ctx, statement.sql, statement.args...)
		if err != nil || result.RowsAffected() != 1 && statement.sql[0] == 'U' {
			return fmt.Errorf("recovery_unit_write_failed")
		}
	}
	return nil
}

func assertRecoveryUnitState(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	wantFacts int,
	wantAvailable, wantReserved string,
) {
	t.Helper()
	var facts int
	err := pool.QueryRow(ctx, `SELECT
      (SELECT count(*) FROM inbox_events WHERE message_id='message-unit') +
      (SELECT count(*) FROM outbox_events WHERE id='outbox-unit') +
      (SELECT count(*) FROM reservations WHERE id='reservation-unit') +
      (SELECT count(*) FROM journal_transactions WHERE id='journal-unit' AND sealed)`).Scan(&facts)
	if err != nil || facts != wantFacts {
		t.Fatalf("recovery facts = %d, want %d: %v", facts, wantFacts, err)
	}
	var available, reserved string
	err = pool.QueryRow(ctx, `SELECT available::text,reserved::text FROM virtual_balances
      WHERE account_id='account-a' AND asset_symbol='BTC'`).Scan(&available, &reserved)
	if err != nil || available != wantAvailable || reserved != wantReserved {
		t.Fatalf("recovery balance = %s/%s, want %s/%s: %v", available, reserved, wantAvailable, wantReserved, err)
	}
}

func fixedHash(character string) string {
	value := ""
	for range 64 {
		value += character
	}
	return value
}
