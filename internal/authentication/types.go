package authentication

import (
	"context"
	"time"

	"axiom/internal/domain"
)

// Session-policy durations and quotas are fixed A11 security boundaries.
const (
	AbsoluteLifetime       = 12 * time.Hour
	IdleLifetime           = 30 * time.Minute
	RecentReauthentication = 10 * time.Minute
	FailureWindow          = 15 * time.Minute
	MaximumFailures        = int64(5)
	MaximumSessions        = 5
)

// User is the minimal credential and authorization record needed at login.
type User struct {
	ID, Email, NormalizedEmail, PasswordHash, Status string
	Roles, Permissions                               []string
	RoleRevision                                     int64
}

// Session is the server-side opaque-session record.
type Session struct {
	ID, UserID, TokenHash, CSRFTokenHash string
	Email, Status                        string
	Roles, Permissions                   []string
	CreatedAt, ExpiresAt, LastSeenAt     time.Time
	IdleExpiresAt, ReauthenticatedAt     time.Time
	Revision, RoleRevision               int64
	RevokedAt                            *time.Time
}

// Principal is safe authenticated request context.
type Principal struct {
	UserID, Email, SessionID string
	Roles, Permissions       []string
	ReauthenticatedAt        time.Time
	SessionRevision          int64
}

// LoginResult contains opaque values that are only sent in protected cookies/responses.
type LoginResult struct {
	Principal    Principal
	SessionToken string
	CSRFToken    string
	ExpiresAt    time.Time
}

// BootstrapOwner is an already-hashed one-time owner input.
type BootstrapOwner struct {
	ID, Email, NormalizedEmail, PasswordHash, AuditID, EventHash string
	OccurredAt                                                   time.Time
}

// NewSession is one atomic session-creation request.
type NewSession struct {
	ID, UserID, TokenHash, CSRFTokenHash string
	CreatedAt, ExpiresAt, IdleExpiresAt  time.Time
}

// Store is the durable authentication boundary.
type Store interface {
	UserCount(context.Context) (int64, error)
	BootstrapOwner(context.Context, BootstrapOwner) (bool, error)
	UserForLogin(context.Context, string) (User, error)
	UpdatePasswordHash(context.Context, string, string, string, time.Time) error
	CountFailures(context.Context, string, string, time.Time) (int64, error)
	RecordFailure(context.Context, string, string, string, time.Time) error
	CreateSession(context.Context, NewSession, int) error
	SessionByTokenHash(context.Context, string) (Session, error)
	TouchSession(context.Context, string, time.Time, time.Time) (Session, error)
	RevokeSession(context.Context, string, string, time.Time) error
}

// Service owns authentication policy and deterministic time access.
type Service struct {
	store     Store
	clock     domain.Clock
	hasher    PasswordHasher
	csrfKey   []byte
	dummyHash string
}
