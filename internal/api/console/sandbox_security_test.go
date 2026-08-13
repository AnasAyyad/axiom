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

func TestSandboxQualificationRoutesRequireSessionExactPermissionOriginCSRFAndIdempotency(
	t *testing.T,
) {
	readOnly, _ := ownerConsoleHTTPTestHandler(
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
	session, csrf := ownerConsoleHTTPLogin(t, readOnly)
	assertSandboxQualificationMutationStatus(
		t, readOnly, session, csrf, "", "", http.StatusForbidden,
	)

	armed, _ := ownerConsoleHTTPTestHandler(
		t,
		[]string{
			authentication.PermissionSandboxRead,
			authentication.PermissionSandboxArm,
		},
	)
	armSession, armCSRF := ownerConsoleHTTPLogin(t, armed)
	assertSandboxQualificationMutationStatus(
		t, armed, armSession, armCSRF,
		"http://attacker.example", "key-sandbox_qualification", http.StatusForbidden,
	)
	assertSandboxQualificationMutationStatus(
		t, armed, armSession, armCSRF,
		"http://localhost:4173", "", http.StatusBadRequest,
	)
}

func assertSandboxQualificationMutationStatus(
	t *testing.T,
	handler http.Handler,
	session, csrf *http.Cookie,
	origin, idempotency string,
	expected int,
) {
	t.Helper()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/sandbox/sessions/session-sandbox_qualification/arms",
		bytes.NewBufferString(
			`{"authorization_token":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","expected_revision":"1","account_ids":["binance-sandbox_qualification"],"reason":"operator bounded arm"}`,
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
			"sandbox qualification mutation = %d, want %d: %s",
			response.Code,
			expected,
			response.Body.String(),
		)
	}
}

func TestSandboxQualificationOneUseAuthorizationRejectsReasonTamperingAndReplay(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	seed := []byte("12345678901234567890")
	mux, commands := newSandboxQualificationAuthorizationHTTPFixture(t, now, seed)
	session, csrf := ownerConsoleHTTPLogin(t, mux)
	reason := "arm bounded sandbox qualification"
	token := issueSandboxQualificationAuthorization(
		t,
		mux,
		session,
		csrf,
		sandboxQualificationTOTPCode(seed, uint64(now.Unix()/30)),
		reason,
	)
	status, body := submitSandboxQualificationArm(
		t, mux, session, csrf, token, reason+" tampered", "arm-key-tampered-0001",
	)
	if status != http.StatusForbidden ||
		!bytes.Contains(body, []byte("authorization_binding_mismatch")) {
		t.Fatalf("tampered binding = %d %s", status, body)
	}
	status, body = submitSandboxQualificationArm(
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
	mux, commands := newSandboxQualificationAuthorizationHTTPFixture(t, now, seed)
	session, csrf := ownerConsoleHTTPLogin(t, mux)
	reason := "start bounded strategy session"
	token := issueSandboxQualificationAuthorization(t, mux, session, csrf,
		sandboxQualificationTOTPCode(seed, uint64(now.Unix()/30)), reason)
	status, body := submitSandboxStrategyStart(t, mux, session, csrf, token, reason, "strategy-start-key-0001")
	if status != http.StatusAccepted || commands.strategyStartCalls != 1 {
		t.Fatalf("strategy start = %d %s calls=%d", status, body, commands.strategyStartCalls)
	}
	status, body = submitSandboxStrategyStart(t, mux, session, csrf, token, reason, "strategy-start-key-0002")
	if status != http.StatusForbidden || !bytes.Contains(body, []byte("authorization_invalid")) || commands.strategyStartCalls != 1 {
		t.Fatalf("strategy start replay = %d %s calls=%d", status, body, commands.strategyStartCalls)
	}
}

func TestSandboxStrategyPrepareRequiresExactOwnerMutationGuards(t *testing.T) {
	mux, commands := newSandboxQualificationAuthorizationHTTPFixture(t,
		time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC),
		[]byte("12345678901234567890"))
	session, csrf := ownerConsoleHTTPLogin(t, mux)
	payload, err := json.Marshal(generated.SandboxStrategySessionCreateRequest{
		StrategyId: generated.SandboxStrategySessionCreateRequestStrategyIdTrendFollowing,
		Exchanges:  []generated.SandboxExchange{generated.SandboxExchangeBinance},
		Instrument: generated.SandboxStrategySessionCreateRequestInstrumentBTCUSDT,
		Preset:     generated.SandboxStrategySessionCreateRequestPresetLatestQualifiedInputs,
		Reason:     "prepare bounded Testnet strategy session",
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost,
		"/api/v1/sandbox/strategy-sessions", bytes.NewReader(payload))
	request.Header.Set("Origin", "http://localhost:4173")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", csrf.Value)
	request.Header.Set("Idempotency-Key", "strategy-prepare-key-0001")
	request.AddCookie(session)
	request.AddCookie(csrf)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted || commands.strategyPrepareCalls != 1 {
		t.Fatalf("strategy prepare = %d %s calls=%d", response.Code,
			response.Body.String(), commands.strategyPrepareCalls)
	}

	badPayload := bytes.NewBufferString(`{"strategy_id":"cross-exchange-arbitrage","exchanges":["binance"],"instrument":"BTCUSDT","preset":"latest-qualified-inputs","reason":"prepare malformed topology"}`)
	bad := httptest.NewRequest(http.MethodPost,
		"/api/v1/sandbox/strategy-sessions", badPayload)
	bad.Header.Set("Origin", "http://localhost:4173")
	bad.Header.Set("Content-Type", "application/json")
	bad.Header.Set("X-CSRF-Token", csrf.Value)
	bad.Header.Set("Idempotency-Key", "strategy-prepare-key-0002")
	bad.AddCookie(session)
	bad.AddCookie(csrf)
	badResponse := httptest.NewRecorder()
	mux.ServeHTTP(badResponse, bad)
	if badResponse.Code != http.StatusAccepted || commands.strategyPrepareCalls != 2 {
		t.Fatalf("topology ownership belongs to service = %d %s calls=%d", badResponse.Code,
			badResponse.Body.String(), commands.strategyPrepareCalls)
	}
}

func newSandboxQualificationAuthorizationHTTPFixture(
	t *testing.T,
	now time.Time,
	seed []byte,
) (http.Handler, *sandboxQualificationHTTPCommands) {
	t.Helper()
	clock, err := domain.NewReplayClock(now)
	if err != nil {
		t.Fatal(err)
	}
	store := newSandboxQualificationHTTPAuthorizationStore(t)
	auth, err := authentication.NewService(
		store, clock, []byte("cccccccccccccccccccccccccccccccc"),
	)
	if err != nil {
		t.Fatal(err)
	}
	authorization, err := authentication.NewSandboxAuthorizationService(
		store, store, clock, writeSandboxQualificationTOTPSeed(t, seed),
	)
	if err != nil {
		t.Fatal(err)
	}
	commands := &sandboxQualificationHTTPCommands{}
	mux := http.NewServeMux()
	Register(mux, Options{
		Authentication: auth, SandboxAuthorizations: authorization,
		SandboxCommands: commands,
		AllowedOrigins:  []string{"http://localhost:4173"},
	})
	return mux, commands
}

func newSandboxQualificationHTTPAuthorizationStore(t *testing.T) *sandboxQualificationHTTPAuthorizationStore {
	t.Helper()
	hash, err := (authentication.PasswordHasher{}).Hash("console-password")
	if err != nil {
		t.Fatal(err)
	}
	return &sandboxQualificationHTTPAuthorizationStore{
		ownerConsoleHTTPStore: &ownerConsoleHTTPStore{
			user: authentication.User{
				ID: "user-sandbox_qualification", Email: "owner@example.test",
				NormalizedEmail: "owner@example.test",
				PasswordHash:    hash, Status: "active",
			},
			sessions: map[string]authentication.Session{},
		},
		grants: make(map[string]authentication.NewSandboxAuthorization),
	}
}

func writeSandboxQualificationTOTPSeed(t *testing.T, seed []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "totp")
	encoded := base32.StdEncoding.WithPadding(base32.NoPadding).
		EncodeToString(seed)
	if err := os.WriteFile(path, []byte(encoded), 0o400); err != nil {
		t.Fatal(err)
	}
	return path
}

func issueSandboxQualificationAuthorization(
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

func submitSandboxQualificationArm(
	t *testing.T,
	handler http.Handler,
	session, csrf *http.Cookie,
	token, reason, key string,
) (int, []byte) {
	t.Helper()
	payload, _ := json.Marshal(generated.SandboxArmRequest{
		AuthorizationToken: token,
		ExpectedRevision:   "1",
		AccountIds:         []string{"binance-sandbox_qualification"},
		Reason:             reason,
	})
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/sandbox/sessions/session-sandbox_qualification/arms",
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

type sandboxQualificationHTTPAuthorizationStore struct {
	*ownerConsoleHTTPStore
	grants  map[string]authentication.NewSandboxAuthorization
	used    map[string]bool
	counter int64
}

func (store *sandboxQualificationHTTPAuthorizationStore) CreateSandboxAuthorization(
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

func (store *sandboxQualificationHTTPAuthorizationStore) ConsumeSandboxAuthorization(
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

func (*sandboxQualificationHTTPAuthorizationStore) RevokeAllUserSessions(
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

func (*sandboxQualificationHTTPAuthorizationStore) AppendHighRiskAudit(
	context.Context,
	authentication.HighRiskAudit,
) error {
	return nil
}

type sandboxQualificationHTTPCommands struct {
	armCalls, strategyPrepareCalls, strategyStartCalls, strategyStopCalls int
}

func (commands *sandboxQualificationHTTPCommands) CreateSandboxStrategySession(
	context.Context,
	authentication.Principal,
	string,
	generated.SandboxStrategySessionCreateRequest,
) (generated.CommandAccepted, error) {
	commands.strategyPrepareCalls++
	return generated.CommandAccepted{}, nil
}

func (commands *sandboxQualificationHTTPCommands) CreateSandboxArm(
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

func (commands *sandboxQualificationHTTPCommands) StartSandboxStrategySession(
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

func (commands *sandboxQualificationHTTPCommands) StopSandboxStrategySession(
	context.Context,
	authentication.Principal,
	string,
	string,
	generated.RevisionCommandRequest,
) (generated.CommandAccepted, error) {
	commands.strategyStopCalls++
	return generated.CommandAccepted{}, nil
}

func (*sandboxQualificationHTTPCommands) RevokeSandboxArm(
	context.Context,
	authentication.Principal,
	string,
	string,
	generated.RevisionCommandRequest,
) (generated.CommandAccepted, error) {
	return generated.CommandAccepted{}, nil
}

func (*sandboxQualificationHTTPCommands) UnlockSandboxAccount(
	context.Context,
	authentication.Principal,
	string,
	string,
	generated.SandboxUnlockRequest,
	authentication.ConsumedAuthorization,
) (generated.CommandAccepted, error) {
	return generated.CommandAccepted{}, nil
}

func (*sandboxQualificationHTTPCommands) CreateSandboxTestOrder(
	context.Context,
	authentication.Principal,
	string,
	generated.SandboxTestOrderRequest,
) (generated.CommandAccepted, error) {
	return generated.CommandAccepted{}, nil
}

func (*sandboxQualificationHTTPCommands) QueueSandboxOrderCommand(
	context.Context,
	authentication.Principal,
	string,
	string,
	string,
	generated.RevisionCommandRequest,
) (generated.CommandAccepted, error) {
	return generated.CommandAccepted{}, nil
}

func (*sandboxQualificationHTTPCommands) QueueSandboxAccountReconciliation(
	context.Context,
	authentication.Principal,
	string,
	string,
	generated.RevisionCommandRequest,
) (generated.CommandAccepted, error) {
	return generated.CommandAccepted{}, nil
}

func sandboxQualificationTOTPCode(seed []byte, counter uint64) string {
	message := make([]byte, 8)
	binary.BigEndian.PutUint64(message, counter)
	mac := hmac.New(sha1.New, seed)
	_, _ = mac.Write(message)
	digest := mac.Sum(nil)
	offset := digest[len(digest)-1] & 0x0f
	number := binary.BigEndian.Uint32(digest[offset:offset+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", number%1_000_000)
}

var _ authentication.SandboxAuthorizationStore = (*sandboxQualificationHTTPAuthorizationStore)(nil)
var _ SandboxCommandService = (*sandboxQualificationHTTPCommands)(nil)
