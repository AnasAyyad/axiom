package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"axiom/internal/authentication"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SandboxRuntimeAuthenticationStore owns atomic TOTP replay prevention, one-use
// authorizations, high-risk audit linkage, and revoke-all session control.
type SandboxRuntimeAuthenticationStore struct{ pool *pgxpool.Pool }

// NewSandboxRuntimeAuthenticationStore constructs the durable high-risk authorization store.
func NewSandboxRuntimeAuthenticationStore(pool *pgxpool.Pool) (*SandboxRuntimeAuthenticationStore, error) {
	if pool == nil {
		return nil, fmt.Errorf("sandbox_runtime_authentication_pool_missing")
	}
	return &SandboxRuntimeAuthenticationStore{pool: pool}, nil
}

// CreateSandboxAuthorization atomically advances TOTP replay state, grant, and audit.
func (store *SandboxRuntimeAuthenticationStore) CreateSandboxAuthorization(
	ctx context.Context,
	write authentication.NewSandboxAuthorization,
) error {
	if err := validateSandboxRuntimeAuthorizationWrite(write); err != nil {
		return err
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return fmt.Errorf("sandbox_runtime_authorization_begin_failed")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err = persistSandboxRuntimeAuthorization(ctx, tx, write); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("sandbox_runtime_authorization_commit_failed")
	}
	return nil
}

func persistSandboxRuntimeAuthorization(
	ctx context.Context,
	tx pgx.Tx,
	write authentication.NewSandboxAuthorization,
) error {
	if _, err := activeSandboxRuntimeSessionRevision(
		ctx,
		tx,
		write.UserID,
		write.SessionID,
		write.SessionRevision,
		write.CreatedAt,
	); err != nil {
		return err
	}
	var counter int64
	err := tx.QueryRow(ctx, `
INSERT INTO sandbox_runtime_totp_replay_state(user_id,last_used_counter,updated_at)
VALUES ($1,$2,$3)
ON CONFLICT (user_id) DO UPDATE
SET last_used_counter=EXCLUDED.last_used_counter,updated_at=EXCLUDED.updated_at
WHERE sandbox_runtime_totp_replay_state.last_used_counter < EXCLUDED.last_used_counter
RETURNING last_used_counter`, write.UserID, write.TOTPCounter, write.CreatedAt).Scan(&counter)
	if err != nil || counter != write.TOTPCounter {
		return fmt.Errorf("sandbox_runtime_totp_replay_rejected")
	}
	if _, err = tx.Exec(ctx, `
INSERT INTO sandbox_runtime_sandbox_authorizations(
  id,token_hash,user_id,session_id,purpose,totp_counter,session_revision,
  source_hash,reason_hash,target_revision,created_at,expires_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		write.ID, write.TokenHash, write.UserID, write.SessionID, write.Purpose,
		write.TOTPCounter, write.SessionRevision, write.SourceHash, write.ReasonHash,
		write.TargetRevision, write.CreatedAt, write.ExpiresAt); err != nil {
		return fmt.Errorf("sandbox_runtime_authorization_insert_failed")
	}
	if err = appendHighRiskAudit(ctx, tx, write.Audit); err != nil {
		return err
	}
	return nil
}

// ConsumeSandboxAuthorization consumes one unexpired session/purpose-bound grant.
func (store *SandboxRuntimeAuthenticationStore) ConsumeSandboxAuthorization(
	ctx context.Context,
	tokenHash, sessionID string,
	purpose authentication.AuthorizationPurpose,
	now time.Time,
) (authentication.ConsumedAuthorization, error) {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return authentication.ConsumedAuthorization{}, fmt.Errorf("sandbox_runtime_authorization_consume_begin_failed")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	consumed, err := consumeSandboxRuntimeAuthorization(
		ctx, tx, tokenHash, sessionID, purpose, now,
	)
	if err != nil {
		return authentication.ConsumedAuthorization{}, err
	}
	revision, err := activeSandboxRuntimeSessionRevision(
		ctx,
		tx,
		consumed.UserID,
		consumed.SessionID,
		0,
		now,
	)
	if err != nil {
		return authentication.ConsumedAuthorization{}, err
	}
	if err = appendConsumedAuthorizationAudit(ctx, tx, consumed, revision, now); err != nil {
		return authentication.ConsumedAuthorization{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return authentication.ConsumedAuthorization{}, fmt.Errorf("sandbox_runtime_authorization_consume_commit_failed")
	}
	consumed.ConsumedAt = consumed.ConsumedAt.UTC()
	return consumed, nil
}

func consumeSandboxRuntimeAuthorization(
	ctx context.Context,
	tx pgx.Tx,
	tokenHash, sessionID string,
	purpose authentication.AuthorizationPurpose,
	now time.Time,
) (authentication.ConsumedAuthorization, error) {
	var consumed authentication.ConsumedAuthorization
	err := tx.QueryRow(ctx, `
UPDATE sandbox_runtime_sandbox_authorizations
SET consumed_at=$4
WHERE token_hash=$1 AND session_id=$2 AND purpose=$3
  AND consumed_at IS NULL AND expires_at>$4
RETURNING id,user_id,session_id,purpose,source_hash,reason_hash,target_revision,consumed_at`,
		tokenHash, sessionID, purpose, now,
	).Scan(
		&consumed.ID,
		&consumed.UserID,
		&consumed.SessionID,
		&consumed.Purpose,
		&consumed.SourceHash,
		&consumed.ReasonHash,
		&consumed.TargetRevision,
		&consumed.ConsumedAt,
	)
	if err != nil {
		return authentication.ConsumedAuthorization{}, fmt.Errorf("sandbox_runtime_authorization_consume_failed")
	}
	return consumed, nil
}

func activeSandboxRuntimeSessionRevision(
	ctx context.Context,
	tx pgx.Tx,
	userID, sessionID string,
	expectedRevision int64,
	now time.Time,
) (int64, error) {
	var revision int64
	if err := tx.QueryRow(ctx, `
SELECT session.revision
FROM sessions session
JOIN users actor ON actor.id=session.user_id
WHERE session.id=$1 AND session.user_id=$2
  AND ($3=0 OR session.revision=$3)
  AND actor.status='active'
  AND session.revoked_at IS NULL
  AND session.expires_at>$4
  AND session.idle_expires_at>$4
FOR SHARE OF session,actor`,
		sessionID,
		userID,
		expectedRevision,
		now,
	).Scan(&revision); err != nil {
		return 0, fmt.Errorf("sandbox_runtime_authorization_session_invalid")
	}
	return revision, nil
}

func validateSandboxRuntimeAuthorizationWrite(write authentication.NewSandboxAuthorization) error {
	audit := write.Audit
	if write.ID == "" || write.TokenHash == "" || write.UserID == "" || write.SessionID == "" ||
		write.TOTPCounter < 0 || write.SessionRevision <= 0 ||
		write.CreatedAt.IsZero() || write.CreatedAt.Location() != time.UTC ||
		write.ExpiresAt != write.CreatedAt.Add(authentication.SandboxReauthorizationLifetime) ||
		!validHash(write.TokenHash) || !validHash(write.SourceHash) || !validHash(write.ReasonHash) ||
		audit.ID == "" || audit.ActorUserID != write.UserID || audit.SessionID != write.SessionID ||
		audit.Purpose != write.Purpose || audit.Outcome != "authorization_issued" ||
		audit.SourceHash != write.SourceHash || audit.ReasonHash != write.ReasonHash ||
		!equalOptionalRevision(audit.TargetRevision, write.TargetRevision) ||
		(authentication.RevisionBoundAuthorizationPurpose(write.Purpose) != (write.TargetRevision != nil)) ||
		audit.Revision != write.SessionRevision || !audit.OccurredAt.Equal(write.CreatedAt) {
		return fmt.Errorf("sandbox_runtime_authorization_invalid")
	}
	return nil
}

func equalOptionalRevision(left, right *int64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right && *left > 0
}

func appendConsumedAuthorizationAudit(
	ctx context.Context,
	tx pgx.Tx,
	consumed authentication.ConsumedAuthorization,
	revision int64,
	now time.Time,
) error {
	return appendHighRiskAudit(ctx, tx, authentication.HighRiskAudit{
		ID:             consumed.ID + "-consumed-audit",
		ActorUserID:    consumed.UserID,
		SessionID:      consumed.SessionID,
		Purpose:        consumed.Purpose,
		Outcome:        "authorization_consumed",
		SourceHash:     consumed.SourceHash,
		ReasonHash:     consumed.ReasonHash,
		Revision:       revision,
		TargetRevision: consumed.TargetRevision,
		OccurredAt:     now,
	})
}

// RevokeAllUserSessions revokes every active session for one user.
func (store *SandboxRuntimeAuthenticationStore) RevokeAllUserSessions(
	ctx context.Context,
	authorizationID, userID, actorSessionID, sourceHash, reasonHash string,
	now time.Time,
) (int64, error) {
	if authorizationID == "" || userID == "" || actorSessionID == "" ||
		!validHash(sourceHash) || !validHash(reasonHash) ||
		now.IsZero() || now.Location() != time.UTC {
		return 0, fmt.Errorf("sandbox_runtime_revoke_all_invalid")
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return 0, fmt.Errorf("sandbox_runtime_revoke_all_begin_failed")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	count, err := revokeAllSandboxRuntimeSessions(
		ctx, tx, authorizationID, userID, actorSessionID, sourceHash, reasonHash, now,
	)
	if err != nil {
		return 0, err
	}
	if err = tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("sandbox_runtime_revoke_all_commit_failed")
	}
	return count, nil
}

func revokeAllSandboxRuntimeSessions(
	ctx context.Context,
	tx pgx.Tx,
	authorizationID, userID, actorSessionID, sourceHash, reasonHash string,
	now time.Time,
) (int64, error) {
	revision, err := lockSandboxRuntimeActorSession(ctx, tx, userID, actorSessionID, now)
	if err != nil {
		return 0, err
	}
	beforeHash, err := sandboxRuntimeSessionStateHash(ctx, tx, userID)
	if err != nil {
		return 0, err
	}
	count, err := revokeSandboxRuntimeSessions(ctx, tx, userID, now)
	if err != nil {
		return 0, err
	}
	afterHash, err := sandboxRuntimeSessionStateHash(ctx, tx, userID)
	if err != nil {
		return 0, err
	}
	controlID := "revoke-all-" + actorSessionID + "-" + fmt.Sprint(now.UnixNano())
	if err = insertSandboxRuntimeSessionControl(
		ctx, tx, controlID, authorizationID, userID, actorSessionID,
		sourceHash, reasonHash, count, now,
	); err != nil {
		return 0, err
	}
	audit := authentication.HighRiskAudit{
		ID:          controlID + "-audit",
		ActorUserID: userID,
		SessionID:   actorSessionID,
		Purpose:     authentication.PurposeRevokeAllSessions,
		Outcome:     "sessions_revoked",
		SourceHash:  sourceHash,
		ReasonHash:  reasonHash,
		Revision:    revision + 1,
		BeforeHash:  beforeHash,
		AfterHash:   afterHash,
		OccurredAt:  now,
	}
	if err = appendHighRiskAudit(ctx, tx, audit); err != nil {
		return 0, err
	}
	return count, nil
}

func sandboxRuntimeSessionStateHash(
	ctx context.Context,
	tx pgx.Tx,
	userID string,
) (string, error) {
	rows, err := tx.Query(ctx, `
SELECT id,revision,revoked_at,revoked_reason
FROM sessions WHERE user_id=$1 ORDER BY id`, userID)
	if err != nil {
		return "", fmt.Errorf("sandbox_runtime_session_state_hash_failed")
	}
	defer rows.Close()
	hasher := sha256.New()
	count := 0
	for rows.Next() {
		var id string
		var revision int64
		var revokedAt *time.Time
		var revokedReason *string
		if err = rows.Scan(&id, &revision, &revokedAt, &revokedReason); err != nil {
			return "", fmt.Errorf("sandbox_runtime_session_state_hash_failed")
		}
		revoked := ""
		if revokedAt != nil {
			revoked = revokedAt.UTC().Format(time.RFC3339Nano)
		}
		_, _ = hasher.Write([]byte(strings.Join([]string{
			id,
			fmt.Sprint(revision),
			revoked,
			valueOrEmpty(revokedReason),
		}, "\x00") + "\x00"))
		count++
	}
	if rows.Err() != nil || count == 0 {
		return "", fmt.Errorf("sandbox_runtime_session_state_hash_failed")
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}
