package console

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"axiom/internal/api/generated"
	"axiom/internal/authentication"
)

type a11FilterReadStub struct {
	ReadService
	incidentState string
	auditEvent    string
	auditRaw      bool
	jobID         string
	eventOrdinal  string
	incidentRaw   bool
}

func (stub *a11FilterReadStub) Job(_ context.Context, id, eventOrdinal string) (generated.JobResource, error) {
	stub.jobID, stub.eventOrdinal = id, eventOrdinal
	return generated.JobResource{Id: id, Kind: generated.JobResourceKind("replay"),
		State: generated.JobResourceState("PAUSED"), ModeLabel: generated.REPLAY, Revision: "1"}, nil
}

func (stub *a11FilterReadStub) Incident(_ context.Context, id string, raw bool) (generated.IncidentDetail, error) {
	stub.incidentRaw = raw
	return generated.IncidentDetail{Id: id, ReasonCode: "test", Revision: "1",
		Severity: generated.IncidentDetailSeverity("warning"), State: generated.IncidentDetailState("resolved"),
		Timeline: []generated.TimelineEvent{}}, nil
}

func (stub *a11FilterReadStub) Incidents(_ context.Context, _ string, _ int, state string) (generated.IncidentPage, error) {
	stub.incidentState = state
	return generated.IncidentPage{Items: []generated.IncidentSummary{}, Revision: "0"}, nil
}

func (stub *a11FilterReadStub) Audit(_ context.Context, _ string, _ int, eventType string, raw bool) (generated.AuditEventPage, error) {
	stub.auditEvent, stub.auditRaw = eventType, raw
	return generated.AuditEventPage{Items: []generated.AuditEvent{}, Revision: "0"}, nil
}

func TestReadFiltersReachAuthoritativeProjection(t *testing.T) {
	stub := &a11FilterReadStub{}
	handler := &handler{options: Options{Read: stub}}

	incidentResponse := httptest.NewRecorder()
	handler.incidents(incidentResponse, httptest.NewRequest(http.MethodGet,
		"/api/v1/incidents?state=acknowledged&page_size=25", nil), authentication.Principal{})
	if incidentResponse.Code != http.StatusOK || stub.incidentState != "acknowledged" {
		t.Fatalf("incident filter = %d %q", incidentResponse.Code, stub.incidentState)
	}

	auditResponse := httptest.NewRecorder()
	handler.audit(auditResponse, httptest.NewRequest(http.MethodGet,
		"/api/v1/audit-events?event_type=command_completed&include_detail=true&page_size=25", nil),
		authentication.Principal{Permissions: []string{"audit.raw"}})
	if auditResponse.Code != http.StatusOK || stub.auditEvent != "command_completed" || !stub.auditRaw {
		t.Fatalf("audit filter = %d %q raw=%t", auditResponse.Code, stub.auditEvent, stub.auditRaw)
	}
}

func TestRawEvidenceRequiresExplicitPermission(t *testing.T) {
	stub := &a11FilterReadStub{}
	handler := &handler{options: Options{Read: stub}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/incidents/incident-a11?include_raw=true", nil)
	request.SetPathValue("id", "incident-a11")
	response := httptest.NewRecorder()
	handler.incident(response, request, authentication.Principal{})
	if response.Code != http.StatusForbidden || stub.incidentRaw {
		t.Fatalf("raw incident permission = %d forwarded=%t", response.Code, stub.incidentRaw)
	}

	allowed := httptest.NewRecorder()
	handler.incident(allowed, request, authentication.Principal{Permissions: []string{"incident.raw"}})
	if allowed.Code != http.StatusOK || !stub.incidentRaw {
		t.Fatalf("authorized raw incident = %d forwarded=%t", allowed.Code, stub.incidentRaw)
	}

	invalidRequest := httptest.NewRequest(http.MethodGet, "/api/v1/incidents/incident-a11?include_raw=1", nil)
	invalidRequest.SetPathValue("id", "incident-a11")
	invalid := httptest.NewRecorder()
	handler.incident(invalid, invalidRequest, authentication.Principal{Permissions: []string{"incident.raw"}})
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("noncanonical boolean accepted = %d", invalid.Code)
	}
}

func TestIncidentFilterRejectsUnknownState(t *testing.T) {
	stub := &a11FilterReadStub{}
	handler := &handler{options: Options{Read: stub}}
	response := httptest.NewRecorder()
	handler.incidents(response, httptest.NewRequest(http.MethodGet,
		"/api/v1/incidents?state=deleted", nil), authentication.Principal{})
	if response.Code != http.StatusBadRequest || stub.incidentState != "" {
		t.Fatalf("unknown state = %d forwarded=%q", response.Code, stub.incidentState)
	}
}

func TestReplayEventOrdinalReachesAuthoritativeProjection(t *testing.T) {
	stub := &a11FilterReadStub{}
	handler := &handler{options: Options{Read: stub}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/replays/replay-a11?event_ordinal=42", nil)
	request.SetPathValue("id", "replay-a11")
	response := httptest.NewRecorder()
	handler.job(response, request, authentication.Principal{})
	if response.Code != http.StatusOK || stub.jobID != "replay-a11" || stub.eventOrdinal != "42" {
		t.Fatalf("replay event selection = %d %q/%q", response.Code, stub.jobID, stub.eventOrdinal)
	}

	backtestRequest := httptest.NewRequest(http.MethodGet, "/api/v1/backtests/backtest-a11?event_ordinal=42", nil)
	backtestRequest.SetPathValue("id", "backtest-a11")
	backtestResponse := httptest.NewRecorder()
	handler.job(backtestResponse, backtestRequest, authentication.Principal{})
	if backtestResponse.Code != http.StatusBadRequest || stub.jobID != "replay-a11" {
		t.Fatalf("backtest accepted replay inspection = %d forwarded=%q", backtestResponse.Code, stub.jobID)
	}
}
