package authentication

import (
	"context"
	"encoding/base32"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type sandboxAuthorizationTestStore struct {
	*authenticationTestStore
	mutex          sync.Mutex
	counter        int64
	authorizations map[string]NewSandboxAuthorization
	consumed       map[string]bool
}

func newSandboxAuthorizationTestStore() *sandboxAuthorizationTestStore {
	return &sandboxAuthorizationTestStore{
		authenticationTestStore: newAuthenticationTestStore(),
		counter:                 -1, authorizations: map[string]NewSandboxAuthorization{}, consumed: map[string]bool{},
	}
}

func (store *sandboxAuthorizationTestStore) CreateSandboxAuthorization(
	_ context.Context,
	write NewSandboxAuthorization,
) error {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	if write.TOTPCounter <= store.counter {
		return errors.New("totp_replay")
	}
	store.counter = write.TOTPCounter
	store.authorizations[write.TokenHash] = write
	return nil
}

func (store *sandboxAuthorizationTestStore) ConsumeSandboxAuthorization(
	_ context.Context,
	hash, session string,
	purpose AuthorizationPurpose,
	now time.Time,
) (ConsumedAuthorization, error) {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	write, ok := store.authorizations[hash]
	if !ok || store.consumed[hash] || write.SessionID != session || write.Purpose != purpose ||
		!now.Before(write.ExpiresAt) {
		return ConsumedAuthorization{}, errors.New("invalid")
	}
	store.consumed[hash] = true
	return ConsumedAuthorization{
		ID: write.ID, UserID: write.UserID, SessionID: write.SessionID,
		Purpose: write.Purpose, SourceHash: write.SourceHash,
		ReasonHash: write.ReasonHash, TargetRevision: write.TargetRevision, ConsumedAt: now,
	}, nil
}

func (store *sandboxAuthorizationTestStore) RevokeAllUserSessions(
	_ context.Context,
	authorizationID, userID, actorSessionID, sourceHash, reasonHash string,
	now time.Time,
) (int64, error) {
	store.mutex.Lock()
	authorized := false
	for hash, write := range store.authorizations {
		if write.ID == authorizationID && store.consumed[hash] &&
			write.UserID == userID && write.SessionID == actorSessionID &&
			write.Purpose == PurposeRevokeAllSessions &&
			write.SourceHash == sourceHash && write.ReasonHash == reasonHash {
			authorized = true
			break
		}
	}
	store.mutex.Unlock()
	if !authorized {
		return 0, errors.New("authorization_invalid")
	}
	store.authenticationTestStore.mutex.Lock()
	defer store.authenticationTestStore.mutex.Unlock()
	var count int64
	for hash, session := range store.sessions {
		if session.UserID == userID && session.RevokedAt == nil {
			session.RevokedAt = &now
			store.sessions[hash] = session
			count++
		}
	}
	return count, nil
}

func (*sandboxAuthorizationTestStore) AppendHighRiskAudit(context.Context, HighRiskAudit) error {
	return nil
}

func TestRFC6238SixDigitWindowAndReplay(t *testing.T) {
	seed := []byte("12345678901234567890")
	at := time.Unix(59, 0).UTC()
	code := totpCode(seed, 1)
	counter, ok := validateTOTP(seed, code, at, 1)
	if !ok || counter != 1 || len(code) != 6 {
		t.Fatalf("TOTP failed: code=%q counter=%d valid=%t", code, counter, ok)
	}
	if _, ok := validateTOTP(seed, code, at.Add(2*TOTPStep), 1); ok {
		t.Fatal("code outside +/-1 window accepted")
	}
}

func TestPasswordTOTPGrantIsPurposeSessionBoundAndOneUse(t *testing.T) {
	service, principal, code := sandboxAuthorizationFixture(t)
	grant, err := service.Reauthenticate(
		context.Background(), principal, "owner-password", code,
		PurposeSandboxArm, "127.0.0.1", "arm bounded sandbox",
	)
	if err != nil {
		t.Fatalf("reauthentication failed: %v", err)
	}
	assertSandboxGrantBinding(t, service, principal, grant)
	if _, err = service.Reauthenticate(
		context.Background(), principal, "owner-password", code,
		PurposeSandboxArm, "127.0.0.1", "replay",
	); !errors.Is(err, ErrReauthorizationFailed) {
		t.Fatalf("TOTP replay accepted: %v", err)
	}
}

func TestD1HighRiskGrantRequiresAndPreservesExactRevision(t *testing.T) {
	service, principal, code := sandboxAuthorizationFixture(t)
	reason := "activate exact configuration revision"
	grant, err := service.ReauthenticateForRevision(
		context.Background(), principal, "owner-password", code,
		PurposeConfigurationActivation, 7, "127.0.0.1", reason,
	)
	if err != nil {
		t.Fatalf("revision-bound reauthentication failed: %v", err)
	}
	consumed, err := service.Consume(
		context.Background(), principal, grant.Token, PurposeConfigurationActivation,
	)
	if err != nil || consumed.TargetRevision == nil || *consumed.TargetRevision != 7 {
		t.Fatalf("consumed target revision = %v error=%v", consumed.TargetRevision, err)
	}
	if _, err = service.ReauthenticateForRevision(
		context.Background(), principal, "owner-password", code,
		PurposeSandboxArm, 7, "127.0.0.1", reason,
	); !errors.Is(err, ErrReauthorizationFailed) {
		t.Fatalf("non-revision purpose accepted a target revision: %v", err)
	}
}

func TestRevokeAllRequiresConsumedDedicatedAuthorization(t *testing.T) {
	service, principal, code := sandboxAuthorizationFixture(t)
	store := service.store.(*sandboxAuthorizationTestStore)
	now := service.clock.Now().UTC
	store.sessions["actor-token-hash"] = Session{
		ID: principal.SessionID, UserID: principal.UserID, TokenHash: "actor-token-hash",
		CreatedAt: now, ExpiresAt: now.Add(time.Hour), LastSeenAt: now,
		IdleExpiresAt: now.Add(time.Hour), Revision: 1,
	}
	store.sessions["other-token-hash"] = Session{
		ID: "session-2", UserID: principal.UserID, TokenHash: "other-token-hash",
		CreatedAt: now, ExpiresAt: now.Add(time.Hour), LastSeenAt: now,
		IdleExpiresAt: now.Add(time.Hour), Revision: 1,
	}
	grant, err := service.Reauthenticate(
		context.Background(),
		principal,
		"owner-password",
		code,
		PurposeRevokeAllSessions,
		"127.0.0.1",
		"revoke every owner session",
	)
	if err != nil {
		t.Fatal(err)
	}
	count, err := service.RevokeAll(context.Background(), principal, grant.Token)
	if err != nil || count != 2 {
		t.Fatalf("revoke all count=%d error=%v", count, err)
	}
	for hash, session := range store.sessions {
		if session.RevokedAt == nil {
			t.Fatalf("session %s remained active", hash)
		}
	}
	if _, err = service.RevokeAll(
		context.Background(),
		principal,
		grant.Token,
	); !errors.Is(err, ErrAuthorizationInvalid) {
		t.Fatalf("revoke-all authorization replay accepted: %v", err)
	}
}

func sandboxAuthorizationFixture(
	t *testing.T,
) (*SandboxAuthorizationService, Principal, string) {
	t.Helper()
	store := newSandboxAuthorizationTestStore()
	clock := testAuthenticationClock()
	hash := currentTestHash(t)
	store.users["owner@example.com"] = User{
		ID: "owner-1", Email: "owner@example.com", NormalizedEmail: "owner@example.com",
		PasswordHash: hash, Status: "active", Roles: []string{"owner"},
		Permissions: []string{
			PermissionSandboxRead, PermissionSandboxArm, PermissionSandboxCancel,
			PermissionSandboxAdmin, "configuration.admin",
		},
		RoleRevision: 1,
	}
	seed := []byte("12345678901234567890")
	seedFile := filepath.Join(t.TempDir(), "totp")
	if err := os.WriteFile(seedFile, []byte(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(seed)), 0o400); err != nil {
		t.Fatal(err)
	}
	service, err := NewSandboxAuthorizationService(store, store, clock, seedFile)
	if err != nil {
		t.Fatal(err)
	}
	principal := Principal{
		UserID: "owner-1", Email: "owner@example.com", SessionID: "session-1",
		Permissions: store.users["owner@example.com"].Permissions, SessionRevision: 1,
	}
	now := clock.Now().UTC
	clock.mutex.Lock()
	clock.now = now
	clock.mutex.Unlock()
	code := totpCode(seed, uint64(now.Unix()/30))
	return service, principal, code
}

func assertSandboxGrantBinding(
	t *testing.T,
	service *SandboxAuthorizationService,
	principal Principal,
	grant AuthorizationGrant,
) {
	t.Helper()
	if _, err := service.Consume(context.Background(), principal, grant.Token, PurposeRiskUnlock); !errors.Is(err, ErrAuthorizationInvalid) {
		t.Fatal("wrong purpose accepted")
	}
	if _, err := service.Consume(context.Background(), principal, grant.Token, PurposeSandboxArm); err != nil {
		t.Fatalf("valid consume failed: %v", err)
	}
	if _, err := service.Consume(context.Background(), principal, grant.Token, PurposeSandboxArm); !errors.Is(err, ErrAuthorizationInvalid) {
		t.Fatal("one-use grant replay accepted")
	}
}
