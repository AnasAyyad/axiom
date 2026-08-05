package console

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"axiom/internal/api/generated"
	"axiom/internal/authentication"
	"axiom/internal/domain"
)

func TestD1OwnerRoutesExposeNoRoleChangeEndpoint(t *testing.T) {
	operations, _ := a11HTTPTestHandler(t, []string{"operations.read"})
	session, _ := a11HTTPLogin(t, operations)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/activity", nil)
	request.AddCookie(session)
	response := httptest.NewRecorder()
	operations.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("owner activity projection absence = %d", response.Code)
	}

	activity, _ := a11HTTPTestHandler(t, []string{"activity.read"})
	activitySession, _ := a11HTTPLogin(t, activity)
	request = httptest.NewRequest(http.MethodGet, "/api/v1/activity", nil)
	request.AddCookie(activitySession)
	response = httptest.NewRecorder()
	activity.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("authorized activity projection absence = %d", response.Code)
	}

	request = httptest.NewRequest(http.MethodPost, "/api/v1/users/user-2/roles",
		strings.NewReader(`{"roles":["auditor"],"expected_revision":"1","reason":"assign D1 audit access","authorization_token":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`))
	request.AddCookie(session)
	response = httptest.NewRecorder()
	operations.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("role mutation endpoint = %d", response.Code)
	}
}

func TestD1HighRiskAuthorizationIsBoundToExactRevision(t *testing.T) {
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	seed := []byte("12345678901234567890")
	mux, commands, clock := newD1AuthorizationHTTPFixture(t, now, seed)
	session, csrf := a11HTTPLogin(t, mux)
	reason := "activate approved D1 configuration revision"
	token := issueD1Authorization(t, mux, session, csrf, seed, now, reason, "7")
	status := submitD1ConfigurationActivation(
		t, mux, session, csrf, token, reason, "8", "d1-config-mismatch-0001",
	)
	if status != http.StatusForbidden || commands.calls != 0 {
		t.Fatalf("revision-mismatched D1 authorization status=%d calls=%d", status, commands.calls)
	}
	if err := clock.Advance(authentication.TOTPStep); err != nil {
		t.Fatal(err)
	}
	token = issueD1Authorization(t, mux, session, csrf, seed, now.Add(authentication.TOTPStep), reason, "7")
	status = submitD1ConfigurationActivation(
		t, mux, session, csrf, token, reason, "7", "d1-config-match-0002",
	)
	if status != http.StatusAccepted || commands.calls != 1 ||
		commands.command.Authorization == nil || commands.command.Authorization.TargetRevision == nil ||
		*commands.command.Authorization.TargetRevision != 7 {
		t.Fatalf("revision-matched D1 authorization status=%d calls=%d command=%+v",
			status, commands.calls, commands.command)
	}
}

func newD1AuthorizationHTTPFixture(
	t *testing.T,
	now time.Time,
	seed []byte,
) (http.Handler, *d1HTTPCommands, *domain.ReplayClock) {
	t.Helper()
	clock, err := domain.NewReplayClock(now)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := (authentication.PasswordHasher{}).Hash("console-password")
	if err != nil {
		t.Fatal(err)
	}
	store := &c6HTTPAuthorizationStore{
		a11HTTPStore: &a11HTTPStore{
			user: authentication.User{
				ID: "user-d1", Email: "owner@example.test", NormalizedEmail: "owner@example.test",
				PasswordHash: hash, Status: "active", Roles: []string{"owner"},
				Permissions: []string{"operations.read", "configuration.admin"}, RoleRevision: 1,
			},
			sessions: map[string]authentication.Session{},
		},
		grants: make(map[string]authentication.NewSandboxAuthorization),
	}
	auth, err := authentication.NewService(store, clock, []byte(strings.Repeat("c", 32)))
	if err != nil {
		t.Fatal(err)
	}
	authorizations, err := authentication.NewSandboxAuthorizationService(
		store, store, clock, writeC6TOTPSeed(t, seed),
	)
	if err != nil {
		t.Fatal(err)
	}
	commands := &d1HTTPCommands{}
	mux := http.NewServeMux()
	Register(mux, Options{
		Authentication: auth, SandboxAuthorizations: authorizations, D1Commands: commands,
		AllowedOrigins: []string{"http://localhost:4173"},
	})
	return mux, commands, clock
}

func issueD1Authorization(
	t *testing.T,
	handler http.Handler,
	session, csrf *http.Cookie,
	seed []byte,
	at time.Time,
	reason, revision string,
) string {
	t.Helper()
	payload, _ := json.Marshal(generated.HighRiskAuthorizationRequest{
		ExpectedRevision: generated.Revision(revision), Password: "console-password",
		Purpose: generated.HighRiskAuthorizationRequestPurposeConfigurationActivation,
		Reason:  reason, Totp: c6TOTPCode(seed, uint64(at.Unix()/30)),
	})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/authorizations", bytes.NewReader(payload))
	request.RemoteAddr = "127.0.0.1:51515"
	request.Header.Set("Origin", "http://localhost:4173")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", csrf.Value)
	request.AddCookie(session)
	request.AddCookie(csrf)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("D1 authorization = %d %s", response.Code, response.Body.String())
	}
	var grant generated.HighRiskAuthorizationGrant
	if err := json.Unmarshal(response.Body.Bytes(), &grant); err != nil || grant.TargetRevision != generated.Revision(revision) {
		t.Fatalf("D1 authorization grant=%+v error=%v", grant, err)
	}
	return grant.Token
}

func submitD1ConfigurationActivation(
	t *testing.T,
	handler http.Handler,
	session, csrf *http.Cookie,
	token, reason, revision, idempotencyKey string,
) int {
	t.Helper()
	payload, _ := json.Marshal(generated.ConfigurationActivationRequest{
		AuthorizationToken: token, ConfigurationId: "configuration-d1",
		ExpectedRevision: generated.Revision(revision), Reason: reason,
	})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/configuration-revisions", bytes.NewReader(payload))
	request.RemoteAddr = "127.0.0.1:51515"
	request.Header.Set("Origin", "http://localhost:4173")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", csrf.Value)
	request.Header.Set("Idempotency-Key", idempotencyKey)
	request.AddCookie(session)
	request.AddCookie(csrf)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response.Code
}

type d1HTTPCommands struct {
	calls   int
	command D1Command
}

func (commands *d1HTTPCommands) ExecuteD1(
	_ context.Context,
	_ authentication.Principal,
	command D1Command,
) (generated.CommandAccepted, error) {
	commands.calls++
	commands.command = command
	return generated.CommandAccepted{
		Id: "command-d1", TargetId: command.TargetID, CorrelationId: "command-d1",
		CreatedAt: time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC),
		Revision:  "2", State: generated.CommandAcceptedStateApplied,
	}, nil
}

func (*d1HTTPCommands) CreateD1Export(
	context.Context,
	authentication.Principal,
	string,
	generated.ExportRequest,
) (generated.ExportArtifact, error) {
	return generated.ExportArtifact{}, nil
}
