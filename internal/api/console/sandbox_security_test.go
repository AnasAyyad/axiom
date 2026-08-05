package console

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"axiom/internal/api/generated"
	"axiom/internal/authentication"
	"axiom/internal/domain"
)

func TestC6RoutesRequireSessionExactPermissionOriginCSRFAndIdempotency(
	t *testing.T,
) {
	readOnly, _ := a11HTTPTestHandler(
		t,
		[]string{authentication.PermissionSandboxRead},
	)
	unauthorized := httptest.NewRecorder()
	readOnly.ServeHTTP(
		unauthorized,
		httptest.NewRequest(http.MethodGet, "/api/v1/sandbox/overview", nil),
	)
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized read = %d", unauthorized.Code)
	}
	session, csrf := a11HTTPLogin(t, readOnly)
	assertC6MutationStatus(
		t, readOnly, session, csrf, "", "", http.StatusForbidden,
	)

	armed, _ := a11HTTPTestHandler(
		t,
		[]string{
			authentication.PermissionSandboxRead,
			authentication.PermissionSandboxArm,
		},
	)
	armSession, armCSRF := a11HTTPLogin(t, armed)
	assertC6MutationStatus(
		t, armed, armSession, armCSRF,
		"http://attacker.example", "key-c6", http.StatusForbidden,
	)
	assertC6MutationStatus(
		t, armed, armSession, armCSRF,
		"http://localhost:4173", "", http.StatusBadRequest,
	)
}

func assertC6MutationStatus(
	t *testing.T,
	handler http.Handler,
	session, csrf *http.Cookie,
	origin, idempotency string,
	expected int,
) {
	t.Helper()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/sandbox/sessions/session-c6/arms",
		bytes.NewBufferString(
			`{"authorization_token":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","expected_revision":"1","account_ids":["binance-c6"],"reason":"operator bounded arm"}`,
		),
	)
	request.Header.Set("Origin", origin)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", csrf.Value)
	if idempotency != "" {
		request.Header.Set("Idempotency-Key", idempotency)
	}
	request.AddCookie(session)
	request.AddCookie(csrf)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != expected {
		t.Fatalf(
			"C6 mutation = %d, want %d: %s",
			response.Code,
			expected,
			response.Body.String(),
		)
	}
}

func TestC6OneUseAuthorizationRejectsReasonTamperingAndReplay(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	seed := []byte("12345678901234567890")
	mux, commands := newC6AuthorizationHTTPFixture(t, now, seed)
	session, csrf := a11HTTPLogin(t, mux)
	reason := "arm bounded sandbox qualification"
	token := issueC6Authorization(
		t,
		mux,
		session,
		csrf,
		c6TOTPCode(seed, uint64(now.Unix()/30)),
		reason,
	)
	status, body := submitC6Arm(
		t, mux, session, csrf, token, reason+" tampered", "arm-key-tampered-0001",
	)
	if status != http.StatusForbidden ||
		!bytes.Contains(body, []byte("authorization_binding_mismatch")) {
		t.Fatalf("tampered binding = %d %s", status, body)
	}
	status, body = submitC6Arm(
		t, mux, session, csrf, token, reason, "arm-key-replay-000002",
	)
	if status != http.StatusForbidden ||
		!bytes.Contains(body, []byte("authorization_invalid")) ||
		commands.armCalls != 0 {
		t.Fatalf("authorization replay = %d %s calls=%d", status, body, commands.armCalls)
	}
}

func TestSandboxStrategyStartRequiresItsOwnBoundOneUseAuthorization(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	seed := []byte("12345678901234567890")
	mux, commands := newC6AuthorizationHTTPFixture(t, now, seed)
	session, csrf := a11HTTPLogin(t, mux)
	reason := "start bounded strategy session"
	token := issueC6Authorization(t, mux, session, csrf,
		c6TOTPCode(seed, uint64(now.Unix()/30)), reason)
	status, body := submitSandboxStrategyStart(t, mux, session, csrf, token, reason, "strategy-start-key-0001")
	if status != http.StatusAccepted || commands.strategyStartCalls != 1 {
		t.Fatalf("strategy start = %d %s calls=%d", status, body, commands.strategyStartCalls)
	}
	status, body = submitSandboxStrategyStart(t, mux, session, csrf, token, reason, "strategy-start-key-0002")
	if status != http.StatusForbidden || !bytes.Contains(body, []byte("authorization_invalid")) || commands.strategyStartCalls != 1 {
		t.Fatalf("strategy start replay = %d %s calls=%d", status, body, commands.strategyStartCalls)
	}
}

func newC6AuthorizationHTTPFixture(
	t *testing.T,
	now time.Time,
	seed []byte,
) (http.Handler, *c6HTTPCommands) {
	t.Helper()
	clock, err := domain.NewReplayClock(now)
	if err != nil {
		t.Fatal(err)
	}
	store := newC6HTTPAuthorizationStore(t)
	auth, err := authentication.NewService(
		store, clock, []byte("cccccccccccccccccccccccccccccccc"),
	)
	if err != nil {
		t.Fatal(err)
	}
	authorization, err := authentication.NewSandboxAuthorizationService(
		store, store, clock, writeC6TOTPSeed(t, seed),
	)
	if err != nil {
		t.Fatal(err)
	}
	commands := &c6HTTPCommands{}
	mux := http.NewServeMux()
	Register(mux, Options{
		Authentication: auth, SandboxAuthorizations: authorization,
		SandboxCommands: commands,
		AllowedOrigins:  []string{"http://localhost:4173"},
	})
	return mux, commands
}

func newC6HTTPAuthorizationStore(t *testing.T) *c6HTTPAuthorizationStore {
	t.Helper()
	hash, err := (authentication.PasswordHasher{}).Hash("console-password")
	if err != nil {
		t.Fatal(err)
	}
	return &c6HTTPAuthorizationStore{
		a11HTTPStore: &a11HTTPStore{
			user: authentication.User{
				ID: "user-c6", Email: "owner@example.test",
				NormalizedEmail: "owner@example.test",
				PasswordHash:    hash, Status: "active", Roles: []string{"owner"},
				Permissions: []string{
					authentication.PermissionSandboxRead,
					authentication.PermissionSandboxArm,
				},
				RoleRevision: 1,
			},
			sessions: map[string]authentication.Session{},
		},
		grants: make(map[string]authentication.NewSandboxAuthorization),
	}
}

func writeC6TOTPSeed(t *testing.T, seed []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "totp")
	encoded := base32.StdEncoding.WithPadding(base32.NoPadding).
		EncodeToString(seed)
	if err := os.WriteFile(path, []byte(encoded), 0o400); err != nil {
		t.Fatal(err)
	}
	return path
}

func issueC6Authorization(
	t *testing.T,
	handler http.Handler,
	session, csrf *http.Cookie,
	code, reason string,
) string {
	t.Helper()
	payload, _ := json.Marshal(generated.SandboxAuthorizationRequest{
		Password: "console-password",
		Totp:     code,
		Purpose:  generated.SandboxAuthorizationRequestPurposeSandboxArm,
		Reason:   reason,
	})
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/sandbox/authorizations",
		bytes.NewReader(payload),
	)
	request.RemoteAddr = "127.0.0.1:51515"
	request.Header.Set("Origin", "http://localhost:4173")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", csrf.Value)
	request.AddCookie(session)
	request.AddCookie(csrf)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("authorization = %d %s", response.Code, response.Body.String())
	}
	var grant generated.SandboxAuthorizationGrant
	if json.Unmarshal(response.Body.Bytes(), &grant) != nil || len(grant.Token) < 32 {
		t.Fatalf("invalid grant response")
	}
	if bytes.Contains(response.Body.Bytes(), []byte(code)) ||
		bytes.Contains(response.Body.Bytes(), []byte("console-password")) {
		t.Fatal("authorization response exposed a credential factor")
	}
	return grant.Token
}

func submitC6Arm(
	t *testing.T,
	handler http.Handler,
	session, csrf *http.Cookie,
	token, reason, key string,
) (int, []byte) {
	t.Helper()
	payload, _ := json.Marshal(generated.SandboxArmRequest{
		AuthorizationToken: token,
		ExpectedRevision:   "1",
		AccountIds:         []string{"binance-c6"},
		Reason:             reason,
	})
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/sandbox/sessions/session-c6/arms",
		bytes.NewReader(payload),
	)
	request.RemoteAddr = "127.0.0.1:61616"
	request.Header.Set("Origin", "http://localhost:4173")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", csrf.Value)
	request.Header.Set("Idempotency-Key", key)
	request.AddCookie(session)
	request.AddCookie(csrf)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response.Code, response.Body.Bytes()
}

func submitSandboxStrategyStart(
	t *testing.T,
	handler http.Handler,
	session, csrf *http.Cookie,
	token, reason, key string,
) (int, []byte) {
	t.Helper()
	payload, _ := json.Marshal(generated.SandboxStrategySessionStartRequest{
		AuthorizationToken: token, ExpectedRevision: "1", Reason: reason,
	})
	request := httptest.NewRequest(http.MethodPost,
		"/api/v1/sandbox/strategy-sessions/strategy-session-1/start", bytes.NewReader(payload))
	request.RemoteAddr = "127.0.0.1:51515"
	request.Header.Set("Origin", "http://localhost:4173")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", csrf.Value)
	request.Header.Set("Idempotency-Key", key)
	request.AddCookie(session)
	request.AddCookie(csrf)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response.Code, response.Body.Bytes()
}

type c6HTTPAuthorizationStore struct {
	*a11HTTPStore
	grants  map[string]authentication.NewSandboxAuthorization
	used    map[string]bool
	counter int64
}

func (store *c6HTTPAuthorizationStore) CreateSandboxAuthorization(
	_ context.Context,
	value authentication.NewSandboxAuthorization,
) error {
	if value.TOTPCounter <= store.counter {
		return errors.New("totp_replay")
	}
	store.counter = value.TOTPCounter
	store.grants[value.TokenHash] = value
	return nil
}

func (store *c6HTTPAuthorizationStore) ConsumeSandboxAuthorization(
	_ context.Context,
	hash, session string,
	purpose authentication.AuthorizationPurpose,
	now time.Time,
) (authentication.ConsumedAuthorization, error) {
	value, ok := store.grants[hash]
	if !ok || store.used[hash] || value.SessionID != session ||
		value.Purpose != purpose || !now.Before(value.ExpiresAt) {
		return authentication.ConsumedAuthorization{}, errors.New("invalid")
	}
	if store.used == nil {
		store.used = make(map[string]bool)
	}
	store.used[hash] = true
	return authentication.ConsumedAuthorization{
		ID: value.ID, UserID: value.UserID, SessionID: value.SessionID,
		Purpose: value.Purpose, SourceHash: value.SourceHash,
		ReasonHash: value.ReasonHash, TargetRevision: value.TargetRevision, ConsumedAt: now,
	}, nil
}

func (*c6HTTPAuthorizationStore) RevokeAllUserSessions(
	context.Context,
	string,
	string,
	string,
	string,
	string,
	time.Time,
) (int64, error) {
	return 0, nil
}

func (*c6HTTPAuthorizationStore) AppendHighRiskAudit(
	context.Context,
	authentication.HighRiskAudit,
) error {
	return nil
}

type c6HTTPCommands struct{ armCalls, strategyStartCalls, strategyStopCalls int }

func (commands *c6HTTPCommands) CreateSandboxArm(
	context.Context,
	authentication.Principal,
	string,
	string,
	generated.SandboxArmRequest,
	authentication.ConsumedAuthorization,
) (generated.SandboxArm, error) {
	commands.armCalls++
	return generated.SandboxArm{}, nil
}

func (commands *c6HTTPCommands) StartSandboxStrategySession(
	context.Context,
	authentication.Principal,
	string,
	string,
	generated.SandboxStrategySessionStartRequest,
	authentication.ConsumedAuthorization,
) (generated.CommandAccepted, error) {
	commands.strategyStartCalls++
	return generated.CommandAccepted{}, nil
}

func (commands *c6HTTPCommands) StopSandboxStrategySession(
	context.Context,
	authentication.Principal,
	string,
	string,
	generated.RevisionCommandRequest,
) (generated.CommandAccepted, error) {
	commands.strategyStopCalls++
	return generated.CommandAccepted{}, nil
}

func (*c6HTTPCommands) RevokeSandboxArm(
	context.Context,
	authentication.Principal,
	string,
	string,
	generated.RevisionCommandRequest,
) (generated.CommandAccepted, error) {
	return generated.CommandAccepted{}, nil
}

func (*c6HTTPCommands) UnlockSandboxAccount(
	context.Context,
	authentication.Principal,
	string,
	string,
	generated.SandboxUnlockRequest,
	authentication.ConsumedAuthorization,
) (generated.CommandAccepted, error) {
	return generated.CommandAccepted{}, nil
}

func (*c6HTTPCommands) CreateSandboxTestOrder(
	context.Context,
	authentication.Principal,
	string,
	generated.SandboxTestOrderRequest,
) (generated.CommandAccepted, error) {
	return generated.CommandAccepted{}, nil
}

func (*c6HTTPCommands) QueueSandboxOrderCommand(
	context.Context,
	authentication.Principal,
	string,
	string,
	string,
	generated.RevisionCommandRequest,
) (generated.CommandAccepted, error) {
	return generated.CommandAccepted{}, nil
}

func (*c6HTTPCommands) QueueSandboxAccountReconciliation(
	context.Context,
	authentication.Principal,
	string,
	string,
	generated.RevisionCommandRequest,
) (generated.CommandAccepted, error) {
	return generated.CommandAccepted{}, nil
}

func c6TOTPCode(seed []byte, counter uint64) string {
	message := make([]byte, 8)
	binary.BigEndian.PutUint64(message, counter)
	mac := hmac.New(sha1.New, seed)
	_, _ = mac.Write(message)
	digest := mac.Sum(nil)
	offset := digest[len(digest)-1] & 0x0f
	number := binary.BigEndian.Uint32(digest[offset:offset+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", number%1_000_000)
}

var _ authentication.SandboxAuthorizationStore = (*c6HTTPAuthorizationStore)(nil)
var _ SandboxCommandService = (*c6HTTPCommands)(nil)
