package postgres

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"axiom/internal/authentication"
	"axiom/internal/storage/postgres/generated"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// OwnerAuthenticationStore persists the one owner, credentials, sessions, and rate limits.
type OwnerAuthenticationStore struct{ pool *pgxpool.Pool }

// NewOwnerAuthenticationStore constructs the least-privilege authentication repository.
func NewOwnerAuthenticationStore(pool *pgxpool.Pool) (*OwnerAuthenticationStore, error) {
	if pool == nil {
		return nil, fmt.Errorf("owner_console_authentication_pool_missing")
	}
	return &OwnerAuthenticationStore{pool: pool}, nil
}

// UserCount reports whether the semantic owner account has been bootstrapped.
func (store *OwnerAuthenticationStore) UserCount(ctx context.Context) (int64, error) {
	var count int64
	err := store.pool.QueryRow(ctx, `SELECT count(*) FROM owner_accounts`).Scan(&count)
	return count, err
}

// BootstrapOwner creates the first owner account and audit event atomically.
func (store *OwnerAuthenticationStore) BootstrapOwner(ctx context.Context, owner authentication.BootstrapOwner) (bool, error) {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return false, fmt.Errorf("owner_console_bootstrap_begin_failed")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err = tx.Exec(ctx, "LOCK TABLE users, owner_accounts IN SHARE ROW EXCLUSIVE MODE"); err != nil {
		return false, fmt.Errorf("owner_console_bootstrap_lock_failed")
	}
	var count int64
	err = tx.QueryRow(ctx, "SELECT count(*) FROM owner_accounts").Scan(&count)
	if err != nil || count > 0 {
		return false, err
	}
	if err = insertOwnerAccount(ctx, tx, owner); err != nil {
		return false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("owner_console_bootstrap_commit_failed")
	}
	return true, nil
}

func insertOwnerAccount(ctx context.Context, tx pgx.Tx, owner authentication.BootstrapOwner) error {
	now := ownerConsoleTimestamp(owner.OccurredAt)
	if _, err := tx.Exec(ctx, `INSERT INTO users(
id,email,normalized_email,password_hash,status,created_at,password_changed_at
) VALUES($1,$2,$3,$4,'active',$5,$5)`, owner.ID, owner.Email, owner.NormalizedEmail, owner.PasswordHash, now); err != nil {
		return fmt.Errorf("owner_console_bootstrap_user_failed")
	}
	if _, err := tx.Exec(ctx, `INSERT INTO owner_accounts(singleton,user_id,established_at)
VALUES(true,$1,$2)`, owner.ID, now); err != nil {
		return fmt.Errorf("owner_bootstrap_account_failed")
	}
	_, err := generated.New(tx).InsertOwnerConsoleAuditEvent(ctx, generated.InsertOwnerConsoleAuditEventParams{ID: owner.AuditID,
		EventType: "owner_bootstrapped", Actor: owner.ID, CausationID: owner.AuditID, CorrelationID: owner.AuditID,
		EventHash: owner.EventHash, RecordedAt: now})
	if err != nil {
		return fmt.Errorf("owner_console_bootstrap_audit_failed")
	}
	return nil
}

// UserForLogin returns only the active owner credential projection.
func (store *OwnerAuthenticationStore) UserForLogin(ctx context.Context, normalizedEmail string) (authentication.User, error) {
	var user authentication.User
	err := store.pool.QueryRow(ctx, `SELECT u.id,u.email,u.normalized_email,u.password_hash,u.status
FROM owner_accounts owner
JOIN users u ON u.id=owner.user_id
WHERE u.normalized_email=$1`, normalizedEmail).Scan(
		&user.ID, &user.Email, &user.NormalizedEmail, &user.PasswordHash, &user.Status)
	if err != nil {
		return authentication.User{}, err
	}
	return user, nil
}

// UpdatePasswordHash upgrades an obsolete profile only if the verified hash is unchanged.
func (store *OwnerAuthenticationStore) UpdatePasswordHash(ctx context.Context, userID, prior, updated string, now time.Time) error {
	_, err := generated.New(store.pool).UpdateUserPasswordHash(ctx, generated.UpdateUserPasswordHashParams{ID: userID,
		PasswordHash: updated, PasswordHash_2: prior, PasswordChangedAt: ownerConsoleTimestamp(now)})
	return err
}

// CountFailures reads the durable email/source rate-limit window.
func (store *OwnerAuthenticationStore) CountFailures(ctx context.Context, emailHash, sourceHash string, since time.Time) (int64, error) {
	return generated.New(store.pool).CountRecentAuthenticationFailures(ctx, generated.CountRecentAuthenticationFailuresParams{
		NormalizedEmailHash: emailHash, SourceScopeHash: sourceHash, OccurredAt: ownerConsoleTimestamp(since)})
}

// RecordFailure appends one non-enumerating authentication failure.
func (store *OwnerAuthenticationStore) RecordFailure(ctx context.Context, emailHash, sourceHash, correlationID string, now time.Time) error {
	id, err := ownerConsoleRandomID("auth-failure")
	if err != nil {
		return err
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	queries := generated.New(tx)
	if _, err = queries.RecordAuthenticationFailure(ctx, generated.RecordAuthenticationFailureParams{ID: id,
		NormalizedEmailHash: emailHash, SourceScopeHash: sourceHash, OccurredAt: ownerConsoleTimestamp(now), CorrelationID: correlationID}); err != nil {
		return err
	}
	_, err = queries.InsertOwnerConsoleAuditEvent(ctx, generated.InsertOwnerConsoleAuditEventParams{
		ID: "audit-" + id, EventType: "authentication_failed", Actor: "anonymous",
		CausationID: id, CorrelationID: correlationID,
		EventHash:  ownerConsoleHash([]byte(emailHash + "\x00" + sourceHash + "\x00" + now.UTC().Format(time.RFC3339Nano))),
		RecordedAt: ownerConsoleTimestamp(now),
	})
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// CreateSession inserts a fresh session and revokes active sessions beyond the cap atomically.
func (store *OwnerAuthenticationStore) CreateSession(ctx context.Context, session authentication.NewSession, maximum int) error {
	if maximum != authentication.MaximumSessions {
		return authentication.ErrConfiguration
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	queries := generated.New(tx)
	if _, err = queries.InsertOwnerConsoleSession(ctx, generated.InsertOwnerConsoleSessionParams{ID: session.ID, UserID: session.UserID,
		TokenHash: session.TokenHash, CsrfTokenHash: session.CSRFTokenHash, CreatedAt: ownerConsoleTimestamp(session.CreatedAt),
		ExpiresAt: ownerConsoleTimestamp(session.ExpiresAt), IdleExpiresAt: ownerConsoleTimestamp(session.IdleExpiresAt)}); err != nil {
		return err
	}
	if _, err = queries.RevokeOldestExcessSessions(ctx, generated.RevokeOldestExcessSessionsParams{
		UserID: session.UserID, Now: ownerConsoleTimestamp(session.CreatedAt)}); err != nil {
		return err
	}
	if _, err = queries.InsertOwnerConsoleAuditEvent(ctx, generated.InsertOwnerConsoleAuditEventParams{
		ID: "audit-login-" + session.ID, EventType: "authentication_succeeded", Actor: session.UserID,
		CausationID: session.ID, CorrelationID: session.ID,
		EventHash:  ownerConsoleHash([]byte(session.UserID + "\x00" + session.ID + "\x00" + session.CreatedAt.UTC().Format(time.RFC3339Nano))),
		RecordedAt: ownerConsoleTimestamp(session.CreatedAt),
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// SessionByTokenHash returns the server-side session for the one active owner.
func (store *OwnerAuthenticationStore) SessionByTokenHash(ctx context.Context, hash string) (authentication.Session, error) {
	var session authentication.Session
	var revoked pgtype.Timestamptz
	err := store.pool.QueryRow(ctx, `SELECT s.id,s.user_id,s.token_hash,s.csrf_token_hash,
s.created_at,s.expires_at,s.last_seen_at,s.idle_expires_at,s.reauthenticated_at,
s.revision,s.revoked_at,u.email,u.status
FROM sessions s
JOIN owner_accounts owner ON owner.user_id=s.user_id
JOIN users u ON u.id=s.user_id
WHERE s.token_hash=$1`, hash).Scan(&session.ID, &session.UserID, &session.TokenHash,
		&session.CSRFTokenHash, &session.CreatedAt, &session.ExpiresAt, &session.LastSeenAt,
		&session.IdleExpiresAt, &session.ReauthenticatedAt, &session.Revision, &revoked,
		&session.Email, &session.Status)
	if err != nil {
		return authentication.Session{}, err
	}
	if revoked.Valid {
		value := revoked.Time
		session.RevokedAt = &value
	}
	return session, nil
}

// TouchSession advances idle activity without extending absolute lifetime.
func (store *OwnerAuthenticationStore) TouchSession(ctx context.Context, id string, seen, idle time.Time) (authentication.Session, error) {
	row, err := generated.New(store.pool).TouchSession(ctx, generated.TouchSessionParams{ID: id,
		LastSeenAt: ownerConsoleTimestamp(seen), IdleExpiresAt: ownerConsoleTimestamp(idle)})
	if err != nil {
		return authentication.Session{}, err
	}
	return ownerConsoleSessionRow(row), nil
}

// RevokeSession idempotently closes one session.
func (store *OwnerAuthenticationStore) RevokeSession(ctx context.Context, id, reason string, now time.Time) error {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	queries := generated.New(tx)
	row, err := queries.RevokeSession(ctx, generated.RevokeSessionParams{ID: id,
		RevokedAt: ownerConsoleTimestamp(now), RevokedReason: &reason})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err = queries.InsertOwnerConsoleAuditEvent(ctx, generated.InsertOwnerConsoleAuditEventParams{
		ID: "audit-revoke-" + id, EventType: "session_revoked", Actor: row.UserID,
		CausationID: id, CorrelationID: id,
		EventHash:  ownerConsoleHash([]byte(row.UserID + "\x00" + id + "\x00" + reason + "\x00" + now.UTC().Format(time.RFC3339Nano))),
		RecordedAt: ownerConsoleTimestamp(now),
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func ownerConsoleSessionRow(row *generated.Session) authentication.Session {
	return authentication.Session{ID: row.ID, UserID: row.UserID, TokenHash: hashText(row.TokenHash),
		CSRFTokenHash: hashText(row.CsrfTokenHash), CreatedAt: row.CreatedAt.Time, ExpiresAt: row.ExpiresAt.Time,
		LastSeenAt: row.LastSeenAt.Time, IdleExpiresAt: row.IdleExpiresAt.Time,
		ReauthenticatedAt: row.ReauthenticatedAt.Time, Revision: row.Revision}
}

func ownerConsoleTimestamp(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value.UTC(), Valid: !value.IsZero()}
}

func ownerConsoleRandomID(prefix string) (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", authentication.ErrConfiguration
	}
	return prefix + "-" + hex.EncodeToString(value), nil
}

var _ authentication.Store = (*OwnerAuthenticationStore)(nil)
