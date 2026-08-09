package postgres

import (
	"context"

	"fmt"

	"time"

	"github.com/jackc/pgx/v5"
)

func lockSandboxRuntimeActorSession(
	ctx context.Context,
	tx pgx.Tx,
	userID, actorSessionID string,
	now time.Time,
) (int64, error) {
	var revision int64
	if err := tx.QueryRow(ctx, `
SELECT session.revision
FROM sessions session
JOIN users actor ON actor.id=session.user_id
WHERE session.id=$1 AND session.user_id=$2
  AND actor.status='active'
  AND session.revoked_at IS NULL
  AND session.expires_at>$3
  AND session.idle_expires_at>$3
FOR UPDATE OF session`, actorSessionID, userID, now).Scan(&revision); err != nil {
		return 0, fmt.Errorf("sandbox_runtime_revoke_all_actor_invalid")
	}
	return revision, nil
}

func revokeSandboxRuntimeSessions(
	ctx context.Context,
	tx pgx.Tx,
	userID string,
	now time.Time,
) (int64, error) {
	tag, err := tx.Exec(ctx, `
UPDATE sessions
SET revoked_at=$2,revoked_reason='owner_revoke_all',revision=revision+1
WHERE user_id=$1 AND revoked_at IS NULL`, userID, now)
	if err != nil {
		return 0, fmt.Errorf("sandbox_runtime_revoke_all_failed")
	}
	return tag.RowsAffected(), nil
}

func insertSandboxRuntimeSessionControl(
	ctx context.Context,
	tx pgx.Tx,
	id, authorizationID, userID, actorSessionID, sourceHash, reasonHash string,
	count int64,
	now time.Time,
) error {
	if _, err := tx.Exec(ctx, `
INSERT INTO sandbox_runtime_session_control_events(
 id,actor_user_id,actor_session_id,authorization_id,control_kind,
 source_hash,reason_hash,affected_sessions,occurred_at
) VALUES ($1,$2,$3,$4,'revoke_all',$5,$6,$7,$8)`,
		id, userID, actorSessionID, authorizationID, sourceHash, reasonHash, count, now,
	); err != nil {
		return fmt.Errorf("sandbox_runtime_revoke_all_control_failed")
	}
	return nil
}
