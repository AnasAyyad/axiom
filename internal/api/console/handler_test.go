package console

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"axiom/internal/authentication"
	"axiom/internal/domain"
)

func TestLoginCookiesOriginCSRFAndLogout(t *testing.T) {
	handler, store := a11HTTPTestHandler(t, []string{"operations.read", "commands.write"})
	rejected := httptest.NewRequest(http.MethodPost, "/api/v1/session/login", strings.NewReader(`{"email":"owner@example.test","password":"console-password"}`))
	rejected.Header.Set("Origin", "http://localhost:4173.evil.test")
	rejectedResponse := httptest.NewRecorder()
	handler.ServeHTTP(rejectedResponse, rejected)
	if rejectedResponse.Code != http.StatusForbidden {
		t.Fatalf("non-exact login origin = %d", rejectedResponse.Code)
	}
	session, csrf := assertA11LoginCookies(t, handler, store)
	assertA11CSRFBoundary(t, handler, session, csrf)
	assertA11LogoutRevokes(t, handler, session, csrf)
}

func assertA11LoginCookies(t *testing.T, handler http.Handler, store *a11HTTPStore) (*http.Cookie, *http.Cookie) {
	t.Helper()
	login := httptest.NewRequest(http.MethodPost, "/api/v1/session/login", strings.NewReader(`{"email":"owner@example.test","password":"console-password"}`))
	login.Header.Set("Origin", "http://localhost:4173")
	login.Header.Set("Content-Type", "application/json")
	loginResponse := httptest.NewRecorder()
	handler.ServeHTTP(loginResponse, login)
	if loginResponse.Code != http.StatusCreated {
		t.Fatalf("login = %d %s", loginResponse.Code, loginResponse.Body.String())
	}
	if strings.Contains(loginResponse.Body.String(), "console-password") || strings.Contains(loginResponse.Body.String(), "axiom_session") {
		t.Fatal("login response exposed credential or opaque session token")
	}
	var sessionCookie, csrfCookie *http.Cookie
	for _, cookie := range loginResponse.Result().Cookies() {
		switch cookie.Name {
		case sessionCookieName:
			sessionCookie = cookie
		case csrfCookieName:
			csrfCookie = cookie
		}
	}
	if sessionCookie == nil || csrfCookie == nil || !sessionCookie.HttpOnly || csrfCookie.HttpOnly ||
		sessionCookie.SameSite != http.SameSiteStrictMode || sessionCookie.Path != "/" || sessionCookie.Domain != "" {
		t.Fatalf("cookie policy = session %#v csrf %#v", sessionCookie, csrfCookie)
	}
	storedSession, found := store.sessions[a11HTTPTokenHash(sessionCookie.Value)]
	if sessionCookie.Value == csrfCookie.Value || !found || storedSession.CSRFTokenHash != a11HTTPTokenHash(csrfCookie.Value) {
		t.Fatal("session and CSRF values were not independently bound and hashed")
	}
	return sessionCookie, csrfCookie
}

func assertA11CSRFBoundary(t *testing.T, handler http.Handler, sessionCookie, csrfCookie *http.Cookie) {
	t.Helper()
	mutationBody := []byte(`{"expected_revision":"1","reason":"operator qualification pause"}`)
	missingCSRF := httptest.NewRequest(http.MethodPost, "/api/v1/risk/pause", bytes.NewReader(mutationBody))
	missingCSRF.Header.Set("Origin", "http://localhost:4173")
	missingCSRF.Header.Set("Idempotency-Key", "qualification-pause-01")
	missingCSRF.AddCookie(sessionCookie)
	missingCSRF.AddCookie(csrfCookie)
	missingCSRFResponse := httptest.NewRecorder()
	handler.ServeHTTP(missingCSRFResponse, missingCSRF)
	if missingCSRFResponse.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF header = %d", missingCSRFResponse.Code)
	}

	validCSRF := httptest.NewRequest(http.MethodPost, "/api/v1/risk/pause", bytes.NewReader(mutationBody))
	validCSRF.Header.Set("Origin", "http://localhost:4173")
	validCSRF.Header.Set("Content-Type", "application/json")
	validCSRF.Header.Set("Idempotency-Key", "qualification-pause-01")
	validCSRF.Header.Set("X-CSRF-Token", csrfCookie.Value)
	validCSRF.AddCookie(sessionCookie)
	validCSRF.AddCookie(csrfCookie)
	validCSRFResponse := httptest.NewRecorder()
	handler.ServeHTTP(validCSRFResponse, validCSRF)
	if validCSRFResponse.Code != http.StatusServiceUnavailable {
		t.Fatalf("verified mutation boundary = %d %s", validCSRFResponse.Code, validCSRFResponse.Body.String())
	}
}

func assertA11LogoutRevokes(t *testing.T, handler http.Handler, sessionCookie, csrfCookie *http.Cookie) {
	t.Helper()
	logout := httptest.NewRequest(http.MethodPost, "/api/v1/session/logout", nil)
	logout.Header.Set("Origin", "http://localhost:4173")
	logout.Header.Set("X-CSRF-Token", csrfCookie.Value)
	logout.AddCookie(sessionCookie)
	logout.AddCookie(csrfCookie)
	logoutResponse := httptest.NewRecorder()
	handler.ServeHTTP(logoutResponse, logout)
	if logoutResponse.Code != http.StatusNoContent {
		t.Fatalf("logout = %d %s", logoutResponse.Code, logoutResponse.Body.String())
	}

	me := httptest.NewRequest(http.MethodGet, "/api/v1/session/me", nil)
	me.AddCookie(sessionCookie)
	meResponse := httptest.NewRecorder()
	handler.ServeHTTP(meResponse, me)
	if meResponse.Code != http.StatusUnauthorized {
		t.Fatalf("revoked session remained accepted = %d", meResponse.Code)
	}
}

func TestViewerCannotMutateAndBoundaryValidationFailsClosed(t *testing.T) {
	handler, _ := a11HTTPTestHandler(t, []string{"operations.read"})
	session, csrf := a11HTTPLogin(t, handler)

	viewerRequest := httptest.NewRequest(http.MethodPost, "/api/v1/risk/pause", strings.NewReader(`{"expected_revision":"1","reason":"operator qualification pause"}`))
	viewerRequest.Header.Set("Origin", "http://localhost:4173")
	viewerRequest.Header.Set("Idempotency-Key", "qualification-pause-01")
	viewerRequest.Header.Set("X-CSRF-Token", csrf.Value)
	viewerRequest.AddCookie(session)
	viewerRequest.AddCookie(csrf)
	viewerResponse := httptest.NewRecorder()
	handler.ServeHTTP(viewerResponse, viewerRequest)
	if viewerResponse.Code != http.StatusForbidden {
		t.Fatalf("read-only viewer mutation = %d", viewerResponse.Code)
	}
}

func TestLoginSourceScopeDropsEphemeralPorts(t *testing.T) {
	for input, expected := range map[string]string{
		"127.0.0.1:54231":      "127.0.0.1",
		"[2001:db8::1]:443":    "2001:db8::1",
		"203.0.113.9":          "203.0.113.9",
		"untrusted-proxy-text": "invalid-source",
	} {
		if actual := loginSourceScope(input); actual != expected {
			t.Errorf("source scope %q = %q, want %q", input, actual, expected)
		}
	}
}

func a11HTTPLogin(t *testing.T, handler http.Handler) (*http.Cookie, *http.Cookie) {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/session/login", strings.NewReader(`{"email":"owner@example.test","password":"console-password"}`))
	request.Header.Set("Origin", "http://localhost:4173")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("login = %d %s", response.Code, response.Body.String())
	}
	var session, csrf *http.Cookie
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == sessionCookieName {
			session = cookie
		}
		if cookie.Name == csrfCookieName {
			csrf = cookie
		}
	}
	return session, csrf
}

func a11HTTPTestHandler(t *testing.T, permissions []string) (http.Handler, *a11HTTPStore) {
	t.Helper()
	hash, err := (authentication.PasswordHasher{}).Hash("console-password")
	if err != nil {
		t.Fatal(err)
	}
	store := &a11HTTPStore{user: authentication.User{ID: "user-a11", Email: "owner@example.test", NormalizedEmail: "owner@example.test",
		PasswordHash: hash, Status: "active", Roles: []string{"owner"}, Permissions: permissions, RoleRevision: 1},
		sessions: map[string]authentication.Session{}}
	clock, _ := domain.NewReplayClock(time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC))
	service, err := authentication.NewService(store, clock, []byte(strings.Repeat("c", 32)))
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	Register(mux, Options{Authentication: service, AllowedOrigins: []string{"http://localhost:4173"}})
	return mux, store
}

type a11HTTPStore struct {
	user     authentication.User
	sessions map[string]authentication.Session
}

func (*a11HTTPStore) UserCount(context.Context) (int64, error) { return 1, nil }
func (*a11HTTPStore) BootstrapOwner(context.Context, authentication.BootstrapOwner) (bool, error) {
	return false, nil
}
func (store *a11HTTPStore) UserForLogin(_ context.Context, normalized string) (authentication.User, error) {
	if normalized != store.user.NormalizedEmail {
		return authentication.User{}, errors.New("not_found")
	}
	return store.user, nil
}
func (*a11HTTPStore) UpdatePasswordHash(context.Context, string, string, string, time.Time) error {
	return nil
}
func (*a11HTTPStore) CountFailures(context.Context, string, string, time.Time) (int64, error) {
	return 0, nil
}
func (*a11HTTPStore) RecordFailure(context.Context, string, string, string, time.Time) error {
	return nil
}
func (store *a11HTTPStore) CreateSession(_ context.Context, value authentication.NewSession, _ int) error {
	store.sessions[value.TokenHash] = authentication.Session{ID: value.ID, UserID: value.UserID,
		TokenHash: value.TokenHash, CSRFTokenHash: value.CSRFTokenHash, Email: store.user.Email, Status: store.user.Status,
		Roles: store.user.Roles, Permissions: store.user.Permissions, CreatedAt: value.CreatedAt, ExpiresAt: value.ExpiresAt,
		LastSeenAt: value.CreatedAt, IdleExpiresAt: value.IdleExpiresAt, ReauthenticatedAt: value.CreatedAt, Revision: 1}
	return nil
}
func (store *a11HTTPStore) SessionByTokenHash(_ context.Context, hash string) (authentication.Session, error) {
	value, ok := store.sessions[hash]
	if !ok {
		return authentication.Session{}, errors.New("not_found")
	}
	return value, nil
}
func (store *a11HTTPStore) TouchSession(_ context.Context, id string, seen, idle time.Time) (authentication.Session, error) {
	for hash, value := range store.sessions {
		if value.ID == id && value.RevokedAt == nil {
			value.LastSeenAt, value.IdleExpiresAt, value.Revision = seen, idle, value.Revision+1
			store.sessions[hash] = value
			return value, nil
		}
	}
	return authentication.Session{}, errors.New("not_found")
}
func (store *a11HTTPStore) RevokeSession(_ context.Context, id, _ string, now time.Time) error {
	for hash, value := range store.sessions {
		if value.ID == id && value.RevokedAt == nil {
			value.RevokedAt, value.Revision = &now, value.Revision+1
			store.sessions[hash] = value
		}
	}
	return nil
}

func a11HTTPTokenHash(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

var _ authentication.Store = (*a11HTTPStore)(nil)
