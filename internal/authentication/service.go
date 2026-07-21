package authentication

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"axiom/internal/domain"

	"golang.org/x/crypto/argon2"
)

// NewService constructs the fail-closed authentication boundary.
func NewService(store Store, clock domain.Clock, csrfKey []byte) (*Service, error) {
	if store == nil || clock == nil || len(csrfKey) < 32 {
		return nil, ErrConfiguration
	}
	hasher := PasswordHasher{}
	dummyHash, err := fixedDummyHash()
	if err != nil {
		return nil, err
	}
	return &Service{store: store, clock: clock, hasher: hasher, csrfKey: append([]byte(nil), csrfKey...), dummyHash: dummyHash}, nil
}

// Bootstrap creates the first owner transactionally from an encoded hash only.
func (service *Service) Bootstrap(ctx context.Context, email, encodedHash string) (bool, error) {
	count, err := service.store.UserCount(ctx)
	if err != nil {
		return false, ErrBootstrapRequired
	}
	if count > 0 {
		return false, nil
	}
	normalized := normalizeEmail(email)
	if normalized == "" || service.hasher.ValidateBootstrapHash(encodedHash) != nil {
		return false, ErrBootstrapRequired
	}
	now := service.clock.Now().UTC
	userID, err := newIdentifier("user")
	if err != nil {
		return false, err
	}
	auditID, err := newIdentifier("audit")
	if err != nil {
		return false, err
	}
	eventHash := stableHash("owner_bootstrap", userID, normalized, now.Format(time.RFC3339Nano))
	return service.store.BootstrapOwner(ctx, BootstrapOwner{ID: userID, Email: strings.TrimSpace(email),
		NormalizedEmail: normalized, PasswordHash: encodedHash, AuditID: auditID, EventHash: eventHash, OccurredAt: now})
}

// Login applies durable rate limiting, generic failures, rehash, and session rotation.
func (service *Service) Login(ctx context.Context, email, password, sourceScope, correlationID string) (LoginResult, error) {
	now := service.clock.Now().UTC
	normalized, emailHash, sourceHash := loginScope(email, sourceScope)
	limited, err := service.rateLimited(ctx, emailHash, sourceHash, now)
	if err != nil {
		return LoginResult{}, ErrAuthenticationFailed
	}
	user, userErr := service.store.UserForLogin(ctx, normalized)
	encoded := service.dummyHash
	if userErr == nil {
		encoded = user.PasswordHash
	}
	valid, rehash, verifyErr := service.hasher.Verify(password, encoded)
	if limited || userErr != nil || verifyErr != nil || !valid || user.Status != "active" {
		_ = service.store.RecordFailure(ctx, emailHash, sourceHash, correlationID, now)
		if limited {
			return LoginResult{}, ErrRateLimited
		}
		return LoginResult{}, ErrAuthenticationFailed
	}
	if rehash && service.rehash(ctx, user, password, now) != nil {
		return LoginResult{}, ErrAuthenticationFailed
	}
	return service.createLoginSession(ctx, user, now)
}

func (service *Service) rateLimited(ctx context.Context, emailHash, sourceHash string, now time.Time) (bool, error) {
	count, err := service.store.CountFailures(ctx, emailHash, sourceHash, now.Add(-FailureWindow))
	return count >= MaximumFailures, err
}

func (service *Service) rehash(ctx context.Context, user User, password string, now time.Time) error {
	updated, err := service.hasher.Hash(password)
	if err != nil {
		return err
	}
	return service.store.UpdatePasswordHash(ctx, user.ID, user.PasswordHash, updated, now)
}

func (service *Service) createLoginSession(ctx context.Context, user User, now time.Time) (LoginResult, error) {
	sessionID, token, csrf, err := service.newSessionValues()
	if err != nil {
		return LoginResult{}, err
	}
	expires := now.Add(AbsoluteLifetime)
	write := NewSession{ID: sessionID, UserID: user.ID, TokenHash: tokenHash(token),
		CSRFTokenHash: tokenHash(csrf), CreatedAt: now, ExpiresAt: expires, IdleExpiresAt: now.Add(IdleLifetime)}
	if err = service.store.CreateSession(ctx, write, MaximumSessions); err != nil {
		return LoginResult{}, ErrAuthenticationFailed
	}
	principal := Principal{UserID: user.ID, Email: user.Email, SessionID: sessionID, Roles: append([]string(nil), user.Roles...),
		Permissions: append([]string(nil), user.Permissions...), ReauthenticatedAt: now, SessionRevision: 1}
	return LoginResult{Principal: principal, SessionToken: token, CSRFToken: csrf, ExpiresAt: expires}, nil
}

func (service *Service) newSessionValues() (string, string, string, error) {
	id, err := newIdentifier("session")
	if err != nil {
		return "", "", "", err
	}
	token, err := newOpaqueToken()
	if err != nil {
		return "", "", "", err
	}
	csrf, err := signedCSRF(id, service.csrfKey)
	return id, token, csrf, err
}

// Authenticate validates, expires, and idly rotates one opaque session record.
func (service *Service) Authenticate(ctx context.Context, token string) (Principal, error) {
	if len(token) < 32 {
		return Principal{}, ErrSessionInvalid
	}
	now := service.clock.Now().UTC
	session, err := service.store.SessionByTokenHash(ctx, tokenHash(token))
	if err != nil || session.Status != "active" || session.RevokedAt != nil || !now.Before(session.ExpiresAt) || !now.Before(session.IdleExpiresAt) {
		if err == nil && session.ID != "" && session.RevokedAt == nil {
			_ = service.store.RevokeSession(ctx, session.ID, "expired", now)
		}
		return Principal{}, ErrSessionInvalid
	}
	updated, err := service.store.TouchSession(ctx, session.ID, now, minTime(session.ExpiresAt, now.Add(IdleLifetime)))
	if err != nil {
		return Principal{}, ErrSessionInvalid
	}
	updated.Email, updated.Status = session.Email, session.Status
	updated.Roles, updated.Permissions = session.Roles, session.Permissions
	return principalFromSession(updated), nil
}

// ValidateCSRF binds the readable signed token to the authenticated session and stored hash.
func (service *Service) ValidateCSRF(session Session, cookieToken, headerToken string) error {
	if cookieToken == "" || headerToken == "" || cookieToken != headerToken ||
		tokenHash(cookieToken) != session.CSRFTokenHash || !validateSignedCSRF(cookieToken, session.ID, service.csrfKey) {
		return ErrCSRFInvalid
	}
	return nil
}

// ValidateRequestCSRF reloads the bound server-side session for one mutation.
func (service *Service) ValidateRequestCSRF(ctx context.Context, sessionToken, cookieToken, headerToken string) error {
	session, err := service.store.SessionByTokenHash(ctx, tokenHash(sessionToken))
	if err != nil || session.RevokedAt != nil {
		return ErrCSRFInvalid
	}
	return service.ValidateCSRF(session, cookieToken, headerToken)
}

// Logout revokes the current server-side session.
func (service *Service) Logout(ctx context.Context, sessionID string) error {
	if sessionID == "" {
		return ErrSessionInvalid
	}
	if err := service.store.RevokeSession(ctx, sessionID, "logout", service.clock.Now().UTC); err != nil {
		return ErrSessionInvalid
	}
	return nil
}

// RequirePermission checks explicit authorization without role-name shortcuts.
func RequirePermission(principal Principal, permission string) error {
	for _, candidate := range principal.Permissions {
		if candidate == permission {
			return nil
		}
	}
	return ErrForbidden
}

// RequireRecentReauthentication gates policy-loosening recovery.
func (service *Service) RequireRecentReauthentication(principal Principal) error {
	if service.clock.Now().UTC.Sub(principal.ReauthenticatedAt) > RecentReauthentication {
		return ErrForbidden
	}
	return nil
}

func principalFromSession(session Session) Principal {
	return Principal{UserID: session.UserID, Email: session.Email, SessionID: session.ID,
		Roles: append([]string(nil), session.Roles...), Permissions: append([]string(nil), session.Permissions...),
		ReauthenticatedAt: session.ReauthenticatedAt, SessionRevision: session.Revision}
}

func loginScope(email, source string) (string, string, string) {
	normalized := normalizeEmail(email)
	return normalized, stableHash(normalized), stableHash(strings.TrimSpace(source))
}

func normalizeEmail(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) < 3 || len(value) > 254 || !strings.Contains(value, "@") || strings.ContainsAny(value, "\r\n\x00") {
		return ""
	}
	return value
}

func stableHash(values ...string) string {
	digest := sha256.Sum256([]byte(strings.Join(values, "\x00")))
	return hex.EncodeToString(digest[:])
}

func minTime(left, right time.Time) time.Time {
	if left.Before(right) {
		return left
	}
	return right
}

func fixedDummyHash() (string, error) {
	salt := []byte("axiom-a11-dummy!")
	output := argon2Dummy([]byte("not-a-user-password"), salt)
	return encodePasswordProfile(passwordProfile{CurrentMemoryKiB, CurrentIterations, CurrentParallelism, salt, output}), nil
}

func argon2Dummy(password, salt []byte) []byte {
	return argon2.IDKey(password, salt, CurrentIterations, CurrentMemoryKiB, CurrentParallelism, CurrentOutputBytes)
}
