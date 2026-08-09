package console

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOperationalEvidenceOwnerRoutesReachOperationalServiceBoundary(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, method, path, body, permission string
	}{
		{"audit verification", http.MethodGet, "/api/v1/audit-verification", "", "operations.read"},
		{"create incident", http.MethodPost, "/api/v1/incidents", `{"severity":"warning","reason_code":"operator_review","owner_user_id":"owner-1","expected_revision":"1","reason":"open reviewed operational incident"}`, "incident.write"},
		{"create report schedule", http.MethodPost, "/api/v1/report-schedules", `{"report_type":"risk","frequency":"hourly","minute_utc":5,"expected_revision":"1","reason":"create reviewed UTC report schedule"}`, "research.control"},
		{"escalate alert", http.MethodPost, "/api/v1/alerts/alert-1/escalate", `{"expected_revision":"1","reason":"escalate after operator impact review"}`, "alert.write"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			allowed, _ := ownerConsoleHTTPTestHandler(t, []string{test.permission})
			allowedSession, allowedCSRF := ownerConsoleHTTPLogin(t, allowed)
			if status := operationalEvidencePermissionRequest(allowed, allowedSession, allowedCSRF,
				test.method, test.path, test.body); status != http.StatusServiceUnavailable {
				t.Fatalf("authorized boundary status=%d", status)
			}
			secondOwner, _ := ownerConsoleHTTPTestHandler(t, []string{"operations.read"})
			secondSession, secondCSRF := ownerConsoleHTTPLogin(t, secondOwner)
			if status := operationalEvidencePermissionRequest(secondOwner, secondSession, secondCSRF,
				test.method, test.path, test.body); status != http.StatusServiceUnavailable {
				t.Fatalf("owner boundary status=%d", status)
			}
		})
	}
}

func operationalEvidencePermissionRequest(
	handler http.Handler, session, csrf *http.Cookie, method, path, body string,
) int {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.AddCookie(session)
	if method != http.MethodGet {
		request.Header.Set("Origin", "http://localhost:4173")
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Idempotency-Key", "operational_evidence-permission-check-0001")
		request.Header.Set("X-CSRF-Token", csrf.Value)
		request.AddCookie(csrf)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response.Code
}
