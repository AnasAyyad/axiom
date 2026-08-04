package postgres

import (
	"context"
	"fmt"

	"axiom/internal/domain"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// D5LifecycleWorker removes only expired generated content with no active hold.
// Metadata and immutable access/audit evidence are retained.
type D5LifecycleWorker struct {
	pool  *pgxpool.Pool
	clock domain.Clock
}

// NewD5LifecycleWorker constructs the generated-artifact expiry worker.
func NewD5LifecycleWorker(pool *pgxpool.Pool, clock domain.Clock) (*D5LifecycleWorker, error) {
	if pool == nil || clock == nil {
		return nil, fmt.Errorf("d5_lifecycle_dependencies_missing")
	}
	return &D5LifecycleWorker{pool: pool, clock: clock}, nil
}

// RunOne expires at most one unheld artifact while preserving metadata and audit evidence.
func (worker *D5LifecycleWorker) RunOne(ctx context.Context) (bool, error) {
	tx, err := worker.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	now := worker.clock.Now().UTC
	var id, owner string
	err = tx.QueryRow(ctx, `SELECT artifact.id,artifact.owner_user_id
FROM v1d_export_artifacts artifact
WHERE artifact.deleted_at IS NULL AND artifact.expires_at<=$1
  AND NOT EXISTS(SELECT 1 FROM v1d_artifact_holds hold
    WHERE hold.artifact_id=artifact.id AND hold.released_at IS NULL)
ORDER BY artifact.expires_at,artifact.id FOR UPDATE OF artifact SKIP LOCKED LIMIT 1`, now).Scan(&id, &owner)
	if err == pgx.ErrNoRows {
		return false, tx.Commit(ctx)
	}
	if err != nil {
		return false, err
	}
	if _, err = tx.Exec(ctx, `UPDATE v1d_export_artifacts SET content=NULL,deleted_at=$2,
deletion_reason='automatic_expiry_unheld' WHERE id=$1`, id, now); err != nil {
		return false, err
	}
	eventID, err := a11Identifier("artifact-access")
	if err != nil {
		return false, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO v1d_artifact_access_events(
id,artifact_id,actor_user_id,action,reason,correlation_id,occurred_at
) VALUES($1,$2,$3,'deleted','automatic seven-day expiry with no active hold',$2,$4)`, eventID, id, owner, now); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}
