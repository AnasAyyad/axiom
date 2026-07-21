package authentication

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"axiom/internal/domain"

	"golang.org/x/crypto/argon2"
)

type authenticationTestClock struct {
	mutex    sync.Mutex
	now      time.Time
	sequence uint64
}

func (clock *authenticationTestClock) Now() domain.EventTime {
	clock.mutex.Lock()
	defer clock.mutex.Unlock()
	clock.sequence++
	return domain.EventTime{UTC: clock.now, Sequence: clock.sequence}
}

func (clock *authenticationTestClock) advance(duration time.Duration) {
	clock.mutex.Lock()
	defer clock.mutex.Unlock()
	clock.now = clock.now.Add(duration)
}

type authenticationFailure struct {
	email, source string
	at            time.Time
}

type authenticationTestStore struct {
	mutex          sync.Mutex
	users          map[string]User
	sessions       map[string]Session
	failures       []authenticationFailure
	bootstrapCount int
}

func newAuthenticationTestStore() *authenticationTestStore {
	return &authenticationTestStore{users: map[string]User{}, sessions: map[string]Session{}}
}

func (store *authenticationTestStore) UserCount(context.Context) (int64, error) {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	return int64(len(store.users)), nil
}

func (store *authenticationTestStore) BootstrapOwner(_ context.Context, owner BootstrapOwner) (bool, error) {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	if len(store.users) > 0 {
		return false, nil
	}
	store.users[owner.NormalizedEmail] = User{ID: owner.ID, Email: owner.Email, NormalizedEmail: owner.NormalizedEmail,
		PasswordHash: owner.PasswordHash, Status: "active", Roles: []string{"owner"},
		Permissions: []string{"operations.read", "commands.write", "incident.raw", "audit.raw"}, RoleRevision: 1}
	store.bootstrapCount++
	return true, nil
}

func (store *authenticationTestStore) UserForLogin(_ context.Context, email string) (User, error) {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	user, ok := store.users[email]
	if !ok {
		return User{}, errors.New("not_found")
	}
	return user, nil
}

func (store *authenticationTestStore) UpdatePasswordHash(_ context.Context, id, prior, updated string, _ time.Time) error {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	for email, user := range store.users {
		if user.ID == id && user.PasswordHash == prior {
			user.PasswordHash = updated
			store.users[email] = user
			return nil
		}
	}
	return errors.New("not_found")
}

func (store *authenticationTestStore) CountFailures(_ context.Context, email, source string, since time.Time) (int64, error) {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	var count int64
	for _, failure := range store.failures {
		if failure.email == email && failure.source == source && !failure.at.Before(since) {
			count++
		}
	}
	return count, nil
}

func (store *authenticationTestStore) RecordFailure(_ context.Context, email, source, _ string, now time.Time) error {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	store.failures = append(store.failures, authenticationFailure{email: email, source: source, at: now})
	return nil
}

func (store *authenticationTestStore) CreateSession(_ context.Context, write NewSession, maximum int) error {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	store.sessions[write.TokenHash] = Session{ID: write.ID, UserID: write.UserID, TokenHash: write.TokenHash,
		CSRFTokenHash: write.CSRFTokenHash, CreatedAt: write.CreatedAt, ExpiresAt: write.ExpiresAt,
		LastSeenAt: write.CreatedAt, IdleExpiresAt: write.IdleExpiresAt, ReauthenticatedAt: write.CreatedAt, Revision: 1}
	active := make([]string, 0)
	for hash, session := range store.sessions {
		if session.UserID == write.UserID && session.RevokedAt == nil {
			active = append(active, hash)
		}
	}
	for len(active) > maximum {
		session := store.sessions[active[0]]
		now := write.CreatedAt
		session.RevokedAt = &now
		store.sessions[active[0]] = session
		active = active[1:]
	}
	return nil
}

func (store *authenticationTestStore) SessionByTokenHash(_ context.Context, hash string) (Session, error) {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	session, ok := store.sessions[hash]
	if !ok {
		return Session{}, errors.New("not_found")
	}
	for _, user := range store.users {
		if user.ID == session.UserID {
			session.Email, session.Status = user.Email, user.Status
			session.Roles, session.Permissions = user.Roles, user.Permissions
		}
	}
	return session, nil
}

func (store *authenticationTestStore) TouchSession(_ context.Context, id string, seen, idle time.Time) (Session, error) {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	for hash, session := range store.sessions {
		if session.ID == id && session.RevokedAt == nil && seen.Before(session.ExpiresAt) && seen.Before(session.IdleExpiresAt) {
			session.LastSeenAt, session.IdleExpiresAt, session.Revision = seen, idle, session.Revision+1
			store.sessions[hash] = session
			return session, nil
		}
	}
	return Session{}, errors.New("not_found")
}

func (store *authenticationTestStore) RevokeSession(_ context.Context, id, _ string, now time.Time) error {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	for hash, session := range store.sessions {
		if session.ID == id && session.RevokedAt == nil {
			session.RevokedAt = &now
			session.Revision++
			store.sessions[hash] = session
		}
	}
	return nil
}

func TestPasswordHasherProfileAndFloor(t *testing.T) {
	hasher := PasswordHasher{}
	encoded, err := hasher.Hash("correct horse battery staple")
	if err != nil || hasher.ValidateBootstrapHash(encoded) != nil {
		t.Fatal("current profile rejected")
	}
	valid, rehash, err := hasher.Verify("correct horse battery staple", encoded)
	if err != nil || !valid || rehash {
		t.Fatalf("verification = %v %v %v", valid, rehash, err)
	}
	if valid, _, _ = hasher.Verify("wrong", encoded); valid {
		t.Fatal("wrong password accepted")
	}
	weak := passwordProfile{memory: MinimumMemoryKiB - 1, iterations: MinimumIterations,
		parallelism: MinimumParallelism, salt: []byte("0123456789abcdef"), output: make([]byte, CurrentOutputBytes)}
	if _, _, err = hasher.Verify("wrong", encodePasswordProfile(weak)); !errors.Is(err, ErrAuthenticationFailed) {
		t.Fatal("below-floor hash accepted")
	}
}

func TestArgon2idDeploymentProfileUnderOneSecond(t *testing.T) {
	hasher := PasswordHasher{}
	encoded, err := hasher.Hash("deployment-profile-benchmark")
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	valid, _, err := hasher.Verify("deployment-profile-benchmark", encoded)
	if err != nil || !valid {
		t.Fatal("profile verification failed")
	}
	if elapsed := time.Since(started); elapsed >= time.Second {
		t.Fatalf("verification took %s", elapsed)
	}
}

func TestBootstrapRequiresEncodedOwnerOnlyWhenEmpty(t *testing.T) {
	store, clock := newAuthenticationTestStore(), testAuthenticationClock()
	service, err := NewService(store, clock, []byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.Bootstrap(context.Background(), "owner@example.com", "plaintext"); !errors.Is(err, ErrBootstrapRequired) {
		t.Fatal("plaintext bootstrap accepted")
	}
	hash, _ := (PasswordHasher{}).Hash("owner-password")
	created, err := service.Bootstrap(context.Background(), "Owner@Example.com", hash)
	if err != nil || !created || store.bootstrapCount != 1 {
		t.Fatalf("bootstrap = %v %v", created, err)
	}
	created, err = service.Bootstrap(context.Background(), "", "")
	if err != nil || created || store.bootstrapCount != 1 {
		t.Fatal("existing user overwritten or inputs required")
	}
}

func TestLoginSessionCSRFLogoutAndGenericFailures(t *testing.T) {
	service, store, clock := authenticatedTestService(t, currentTestHash(t))
	result, err := service.Login(context.Background(), "OWNER@example.com", "owner-password", "127.0.0.1", "correlation-1")
	if err != nil || len(result.SessionToken) < 32 || len(result.CSRFToken) < 32 {
		t.Fatalf("login failed: %v", err)
	}
	principal, err := service.Authenticate(context.Background(), result.SessionToken)
	if err != nil || principal.UserID == "" || RequirePermission(principal, "commands.write") != nil {
		t.Fatal("session not authenticated")
	}
	session, _ := store.SessionByTokenHash(context.Background(), tokenHash(result.SessionToken))
	if err = service.ValidateCSRF(session, result.CSRFToken, result.CSRFToken); err != nil {
		t.Fatal(err)
	}
	if err = service.ValidateCSRF(session, result.CSRFToken, "different"); !errors.Is(err, ErrCSRFInvalid) {
		t.Fatal("csrf mismatch accepted")
	}
	if err = service.Logout(context.Background(), principal.SessionID); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Authenticate(context.Background(), result.SessionToken); !errors.Is(err, ErrSessionInvalid) {
		t.Fatal("revoked session accepted")
	}
	if _, err = service.Login(context.Background(), "missing@example.com", "wrong", "127.0.0.1", "correlation-2"); !errors.Is(err, ErrAuthenticationFailed) {
		t.Fatal("unknown-user detail leaked")
	}
	if _, err = service.Login(context.Background(), "owner@example.com", "wrong", "127.0.0.1", "correlation-3"); !errors.Is(err, ErrAuthenticationFailed) {
		t.Fatal("bad-password detail leaked")
	}
	clock.advance(time.Minute)
}

func TestLoginRehashRateLimitIdleAndRecentAuthentication(t *testing.T) {
	legacy := legacyTestHash("owner-password")
	service, store, clock := authenticatedTestService(t, legacy)
	result, err := service.Login(context.Background(), "owner@example.com", "owner-password", "source", "correlation")
	if err != nil {
		t.Fatal(err)
	}
	user, _ := store.UserForLogin(context.Background(), "owner@example.com")
	if user.PasswordHash == legacy {
		t.Fatal("obsolete profile not rehashed")
	}
	if err = service.RequireRecentReauthentication(result.Principal); err != nil {
		t.Fatal(err)
	}
	clock.advance(RecentReauthentication + time.Second)
	if err = service.RequireRecentReauthentication(result.Principal); !errors.Is(err, ErrForbidden) {
		t.Fatal("stale reauthentication accepted")
	}
	clock.advance(IdleLifetime)
	if _, err = service.Authenticate(context.Background(), result.SessionToken); !errors.Is(err, ErrSessionInvalid) {
		t.Fatal("idle session accepted")
	}
	for index := 0; index < int(MaximumFailures); index++ {
		_, _ = service.Login(context.Background(), "owner@example.com", "wrong", "limited", "failure")
	}
	if _, err = service.Login(context.Background(), "owner@example.com", "owner-password", "limited", "limited"); !errors.Is(err, ErrRateLimited) {
		t.Fatal("rate limit not durable")
	}
}

func authenticatedTestService(t *testing.T, hash string) (*Service, *authenticationTestStore, *authenticationTestClock) {
	t.Helper()
	store, clock := newAuthenticationTestStore(), testAuthenticationClock()
	store.users["owner@example.com"] = User{ID: "user-owner", Email: "owner@example.com", NormalizedEmail: "owner@example.com",
		PasswordHash: hash, Status: "active", Roles: []string{"owner"}, Permissions: []string{"operations.read", "commands.write"}, RoleRevision: 1}
	service, err := NewService(store, clock, []byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	return service, store, clock
}

func currentTestHash(t *testing.T) string {
	t.Helper()
	hash, err := (PasswordHasher{}).Hash("owner-password")
	if err != nil {
		t.Fatal(err)
	}
	return hash
}

func legacyTestHash(password string) string {
	salt := []byte("0123456789abcdef")
	output := argon2.IDKey([]byte(password), salt, MinimumIterations, MinimumMemoryKiB, MinimumParallelism, CurrentOutputBytes)
	return encodePasswordProfile(passwordProfile{MinimumMemoryKiB, MinimumIterations, MinimumParallelism, salt, output})
}

func testAuthenticationClock() *authenticationTestClock {
	return &authenticationTestClock{now: time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)}
}
