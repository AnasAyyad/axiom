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

func TestOwnerControlOwnerRoutesExposeNoRoleChangeEndpoint(t *testing.T) {
	operations, _ := ownerConsoleHTTPTestHandler(t, []string{"operations.read"})
	session, _ := ownerConsoleHTTPLogin(t, operations)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/activity", nil)
	request.AddCookie(session)
	response := httptest.NewRecorder()
	operations.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("owner activity projection absence = %d", response.Code)
	}

	activity, _ := ownerConsoleHTTPTestHandler(t, []string{"activity.read"})
	activitySession, _ := ownerConsoleHTTPLogin(t, activity)
	request = httptest.NewRequest(http.MethodGet, "/api/v1/activity", nil)
	request.AddCookie(activitySession)
	response = httptest.NewRecorder()
	activity.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("authorized activity projection absence = %d", response.Code)
	}

	request = httptest.NewRequest(http.MethodPost, "/api/v1/users/user-2/roles",
		strings.NewReader(`{"roles":["auditor"],"expected_revision":"1","reason":"assign owner control audit access","authorization_token":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`))
	request.AddCookie(session)
	response = httptest.NewRecorder()
	operations.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("role mutation endpoint = %d", response.Code)
	}
}

func TestOwnerControlHighRiskAuthorizationIsBoundToExactRevision(t *testing.T) {
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	seed := []byte("12345678901234567890")
	mux, commands, clock := newOwnerControlAuthorizationHTTPFixture(t, now, seed)
	session, csrf := ownerConsoleHTTPLogin(t, mux)
	reason := "activate approved owner control configuration revision"
	token := issueOwnerControlAuthorization(t, mux, session, csrf, seed, now, reason, "7")
	status := submitOwnerControlConfigurationActivation(
		t, mux, session, csrf, token, reason, "8", "owner_control-config-mismatch-0001",
	)
	if status != http.StatusForbidden || commands.calls != 0 {
		t.Fatalf("revision-mismatched owner control authorization status=%d calls=%d", status, commands.calls)
	}
	if err := clock.Advance(authentication.TOTPStep); err != nil {
		t.Fatal(err)
	}
	token = issueOwnerControlAuthorization(t, mux, session, csrf, seed, now.Add(authentication.TOTPStep), reason, "7")
	status = submitOwnerControlConfigurationActivation(
		t, mux, session, csrf, token, reason, "7", "owner_control-config-match-0002",
	)
	if status != http.StatusAccepted || commands.calls != 1 ||
		commands.command.Authorization == nil || commands.command.Authorization.TargetRevision == nil ||
		*commands.command.Authorization.TargetRevision != 7 {
		t.Fatalf("revision-matched owner control authorization status=%d calls=%d command=%+v",
			status, commands.calls, commands.command)
	}
}

func newOwnerControlAuthorizationHTTPFixture(
	t *testing.T,
	now time.Time,
	seed []byte,
) (http.Handler, *ownerControlHTTPCommands, *domain.ReplayClock) {
	t.Helper()
	clock, err := domain.NewReplayClock(now)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := (authentication.PasswordHasher{}).Hash("console-password")
	if err != nil {
		t.Fatal(err)
	}
	store := &sandboxQualificationHTTPAuthorizationStore{
		ownerConsoleHTTPStore: &ownerConsoleHTTPStore{
			user: authentication.User{
				ID: "user-owner_control", Email: "owner@example.test", NormalizedEmail: "owner@example.test",
				PasswordHash: hash, Status: "active",
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
		store, store, clock, writeSandboxQualificationTOTPSeed(t, seed),
	)
	if err != nil {
		t.Fatal(err)
	}
	commands := &ownerControlHTTPCommands{}
	mux := http.NewServeMux()
	Register(mux, Options{
		Authentication: auth, SandboxAuthorizations: authorizations, OwnerControlCommands: commands,
		AllowedOrigins: []string{"http://localhost:4173"},
	})
	return mux, commands, clock
}

func issueOwnerControlAuthorization(
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
		Reason:  reason, Totp: sandboxQualificationTOTPCode(seed, uint64(at.Unix()/30)),
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
		t.Fatalf("owner control authorization = %d %s", response.Code, response.Body.String())
	}
	var grant generated.HighRiskAuthorizationGrant
	if err := json.Unmarshal(response.Body.Bytes(), &grant); err != nil || grant.TargetRevision != generated.Revision(revision) {
		t.Fatalf("owner control authorization grant=%+v error=%v", grant, err)
	}
	return grant.Token
}

func submitOwnerControlConfigurationActivation(
	t *testing.T,
	handler http.Handler,
	session, csrf *http.Cookie,
	token, reason, revision, idempotencyKey string,
) int {
	t.Helper()
	payload, _ := json.Marshal(generated.ConfigurationActivationRequest{
		AuthorizationToken: token, ConfigurationId: "configuration-owner_control",
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

type ownerControlHTTPCommands struct {
	calls   int
	command OwnerControlCommand
}

func (commands *ownerControlHTTPCommands) ExecuteOwnerControl(
	_ context.Context,
	_ authentication.Principal,
	command OwnerControlCommand,
) (generated.CommandAccepted, error) {
	commands.calls++
	commands.command = command
	return generated.CommandAccepted{
		Id: "command-owner_control", TargetId: command.TargetID, CorrelationId: "command-owner_control",
		CreatedAt: time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC),
		Revision:  "2", State: generated.CommandAcceptedStateApplied,
	}, nil
}

func (*ownerControlHTTPCommands) CreateOwnerControlExport(
	context.Context,
	authentication.Principal,
	string,
	generated.ExportRequest,
) (generated.ExportArtifact, error) {
	return generated.ExportArtifact{}, nil
}
