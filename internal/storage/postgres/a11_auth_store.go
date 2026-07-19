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

// A11AuthenticationStore persists owner bootstrap, credentials, sessions, and rate limits.
type A11AuthenticationStore struct{ pool *pgxpool.Pool }

// NewA11AuthenticationStore constructs the least-privilege authentication repository.
func NewA11AuthenticationStore(pool *pgxpool.Pool) (*A11AuthenticationStore, error) {
	if pool == nil {
		return nil, fmt.Errorf("a11_authentication_pool_missing")
	}
	return &A11AuthenticationStore{pool: pool}, nil
}

// UserCount reports whether bootstrap inputs are required.
func (store *A11AuthenticationStore) UserCount(ctx context.Context) (int64, error) {
	return generated.New(store.pool).CountUsersForBootstrap(ctx)
}

// BootstrapOwner creates the first user, roles, permissions, and audit event atomically.
func (store *A11AuthenticationStore) BootstrapOwner(ctx context.Context, owner authentication.BootstrapOwner) (bool, error) {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return false, fmt.Errorf("a11_bootstrap_begin_failed")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err = tx.Exec(ctx, "LOCK TABLE users IN SHARE ROW EXCLUSIVE MODE"); err != nil {
		return false, fmt.Errorf("a11_bootstrap_lock_failed")
	}
	queries := generated.New(tx)
	count, err := queries.CountUsersForBootstrap(ctx)
	if err != nil || count > 0 {
		return false, err
	}
	if err = insertA11Owner(ctx, queries, owner); err != nil {
		return false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("a11_bootstrap_commit_failed")
	}
	return true, nil
}

func insertA11Owner(ctx context.Context, queries *generated.Queries, owner authentication.BootstrapOwner) error {
	now := a11Timestamp(owner.OccurredAt)
	if _, err := queries.BootstrapOwnerUser(ctx, generated.BootstrapOwnerUserParams{ID: owner.ID, Email: owner.Email,
		NormalizedEmail: owner.NormalizedEmail, PasswordHash: owner.PasswordHash, CreatedAt: now}); err != nil {
		return fmt.Errorf("a11_bootstrap_user_failed")
	}
	for _, role := range []string{"owner", "viewer"} {
		if _, err := queries.GetBootstrapAuthorizationRole(ctx,
			generated.GetBootstrapAuthorizationRoleParams{ID: role, Name: role}); err != nil {
			return fmt.Errorf("a11_bootstrap_role_failed")
		}
	}
	if _, err := queries.GrantUserRole(ctx, generated.GrantUserRoleParams{UserID: owner.ID, RoleID: "owner", GrantedAt: now}); err != nil {
		return fmt.Errorf("a11_bootstrap_role_assignment_failed")
	}
	if err := grantA11Permissions(ctx, queries, now); err != nil {
		return err
	}
	_, err := queries.InsertA11AuditEvent(ctx, generated.InsertA11AuditEventParams{ID: owner.AuditID,
		EventType: "owner_bootstrapped", Actor: owner.ID, CausationID: owner.AuditID, CorrelationID: owner.AuditID,
		EventHash: owner.EventHash, RecordedAt: now})
	if err != nil {
		return fmt.Errorf("a11_bootstrap_audit_failed")
	}
	return nil
}

func grantA11Permissions(ctx context.Context, queries *generated.Queries, now pgtype.Timestamptz) error {
	for _, permission := range []string{"operations.read", "commands.write", "incident.raw", "audit.raw"} {
		if _, err := queries.GrantRolePermission(ctx, generated.GrantRolePermissionParams{RoleID: "owner", PermissionID: permission, GrantedAt: now}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("a11_bootstrap_permission_failed")
		}
	}
	if _, err := queries.GrantRolePermission(ctx, generated.GrantRolePermissionParams{RoleID: "viewer", PermissionID: "operations.read", GrantedAt: now}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("a11_bootstrap_permission_failed")
	}
	return nil
}

// UserForLogin returns only the credential and explicit authorization projection.
func (store *A11AuthenticationStore) UserForLogin(ctx context.Context, normalizedEmail string) (authentication.User, error) {
	row, err := generated.New(store.pool).GetUserForAuthentication(ctx, normalizedEmail)
	if err != nil {
		return authentication.User{}, err
	}
	return authentication.User{ID: row.ID, Email: row.Email, NormalizedEmail: row.NormalizedEmail,
		PasswordHash: row.PasswordHash, Status: row.Status, Roles: row.Roles,
		Permissions: row.Permissions, RoleRevision: row.RoleRevision}, nil
}

// UpdatePasswordHash upgrades an obsolete profile only if the verified hash is unchanged.
func (store *A11AuthenticationStore) UpdatePasswordHash(ctx context.Context, userID, prior, updated string, now time.Time) error {
	_, err := generated.New(store.pool).UpdateUserPasswordHash(ctx, generated.UpdateUserPasswordHashParams{ID: userID,
		PasswordHash: updated, PasswordHash_2: prior, PasswordChangedAt: a11Timestamp(now)})
	return err
}

// CountFailures reads the durable email/source rate-limit window.
func (store *A11AuthenticationStore) CountFailures(ctx context.Context, emailHash, sourceHash string, since time.Time) (int64, error) {
	return generated.New(store.pool).CountRecentAuthenticationFailures(ctx, generated.CountRecentAuthenticationFailuresParams{
		NormalizedEmailHash: emailHash, SourceScopeHash: sourceHash, OccurredAt: a11Timestamp(since)})
}

// RecordFailure appends one non-enumerating authentication failure.
func (store *A11AuthenticationStore) RecordFailure(ctx context.Context, emailHash, sourceHash, correlationID string, now time.Time) error {
	id, err := a11RandomID("auth-failure")
	if err != nil {
		return err
	}
	_, err = generated.New(store.pool).RecordAuthenticationFailure(ctx, generated.RecordAuthenticationFailureParams{ID: id,
		NormalizedEmailHash: emailHash, SourceScopeHash: sourceHash, OccurredAt: a11Timestamp(now), CorrelationID: correlationID})
	return err
}

// CreateSession inserts a fresh session and revokes active sessions beyond the cap atomically.
func (store *A11AuthenticationStore) CreateSession(ctx context.Context, session authentication.NewSession, maximum int) error {
	if maximum != authentication.MaximumSessions {
		return authentication.ErrConfiguration
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	queries := generated.New(tx)
	if _, err = queries.InsertA11Session(ctx, generated.InsertA11SessionParams{ID: session.ID, UserID: session.UserID,
		TokenHash: session.TokenHash, CsrfTokenHash: session.CSRFTokenHash, CreatedAt: a11Timestamp(session.CreatedAt),
		ExpiresAt: a11Timestamp(session.ExpiresAt), IdleExpiresAt: a11Timestamp(session.IdleExpiresAt)}); err != nil {
		return err
	}
	if _, err = queries.RevokeOldestExcessSessions(ctx, generated.RevokeOldestExcessSessionsParams{
		UserID: session.UserID, Now: a11Timestamp(session.CreatedAt)}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// SessionByTokenHash returns the server-side session and current authorization projection.
func (store *A11AuthenticationStore) SessionByTokenHash(ctx context.Context, hash string) (authentication.Session, error) {
	row, err := generated.New(store.pool).GetSessionByTokenHash(ctx, hash)
	if err != nil {
		return authentication.Session{}, err
	}
	return a11AuthenticationSession(row), nil
}

// TouchSession advances idle activity without extending absolute lifetime.
func (store *A11AuthenticationStore) TouchSession(ctx context.Context, id string, seen, idle time.Time) (authentication.Session, error) {
	row, err := generated.New(store.pool).TouchSession(ctx, generated.TouchSessionParams{ID: id,
		LastSeenAt: a11Timestamp(seen), IdleExpiresAt: a11Timestamp(idle)})
	if err != nil {
		return authentication.Session{}, err
	}
	return a11SessionRow(row), nil
}

// RevokeSession idempotently closes one session.
func (store *A11AuthenticationStore) RevokeSession(ctx context.Context, id, reason string, now time.Time) error {
	_, err := generated.New(store.pool).RevokeSession(ctx, generated.RevokeSessionParams{ID: id,
		RevokedAt: a11Timestamp(now), RevokedReason: &reason})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	return err
}

func a11AuthenticationSession(row *generated.GetSessionByTokenHashRow) authentication.Session {
	session := authentication.Session{ID: row.ID, UserID: row.UserID, TokenHash: hashText(row.TokenHash),
		CSRFTokenHash: hashText(row.CsrfTokenHash), Email: row.Email, Status: row.UserStatus,
		Roles: row.Roles, Permissions: row.Permissions, RoleRevision: row.RoleRevision,
		CreatedAt: row.CreatedAt.Time, ExpiresAt: row.ExpiresAt.Time, LastSeenAt: row.LastSeenAt.Time,
		IdleExpiresAt: row.IdleExpiresAt.Time, ReauthenticatedAt: row.ReauthenticatedAt.Time, Revision: row.Revision}
	if row.RevokedAt.Valid {
		revoked := row.RevokedAt.Time
		session.RevokedAt = &revoked
	}
	return session
}

func a11SessionRow(row *generated.Session) authentication.Session {
	return authentication.Session{ID: row.ID, UserID: row.UserID, TokenHash: hashText(row.TokenHash),
		CSRFTokenHash: hashText(row.CsrfTokenHash), CreatedAt: row.CreatedAt.Time, ExpiresAt: row.ExpiresAt.Time,
		LastSeenAt: row.LastSeenAt.Time, IdleExpiresAt: row.IdleExpiresAt.Time,
		ReauthenticatedAt: row.ReauthenticatedAt.Time, Revision: row.Revision}
}

func a11Timestamp(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value.UTC(), Valid: !value.IsZero()}
}

func a11RandomID(prefix string) (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", authentication.ErrConfiguration
	}
	return prefix + "-" + hex.EncodeToString(value), nil
}

var _ authentication.Store = (*A11AuthenticationStore)(nil)
