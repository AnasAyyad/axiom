package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
)

func d5HeavyWorkAllowed(ctx context.Context, tx pgx.Tx) (bool, error) {
	var allowed bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(
  SELECT 1 FROM v1d_storage_pressure_state
  WHERE scope_id='market-data' AND level='NORMAL' AND source_instance<>'migration-bootstrap'
    AND observed_at>=CURRENT_TIMESTAMP-interval '2 minutes'
)`).Scan(&allowed)
	return allowed, err
}
